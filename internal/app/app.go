package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/cache"
	"github.com/TeleginSergey/hozdacha/internal/config"
	"github.com/TeleginSergey/hozdacha/internal/db"
	"github.com/TeleginSergey/hozdacha/internal/handlers"
	"github.com/TeleginSergey/hozdacha/internal/middleware"
	"github.com/TeleginSergey/hozdacha/internal/moysklad"
	"github.com/TeleginSergey/hozdacha/internal/services"
	"github.com/TeleginSergey/hozdacha/internal/usecase"
)

// App инкапсулирует все зависимости и HTTP-сервер.
type App struct {
	Config    *config.Config
	Logger    *zap.Logger
	DB        *db.DB
	Cache     *cache.StockCache
	Router    *gin.Engine
	Server    *http.Server
	Scheduler *services.Scheduler
}

// NewApp строит все зависимости и возвращает готовое приложение.
func NewApp() (*App, error) {
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to init logger: %w", err)
	}

	cfg, err := config.Load()
	if err != nil {
		logger.Fatal("Failed to load config", zap.Error(err))
	}

	// DB
	database, err := db.New(&cfg.DB, logger)
	if err != nil {
		logger.Fatal("Failed to initialize database", zap.Error(err))
	}

	// Redis
	var stockCache *cache.StockCache
	if cfg.Redis.Host != "" {
		var err error
		stockCache, err = cache.NewStockCache(cfg.Redis.Host, cfg.Redis.Port, cfg.Redis.Password, cfg.Redis.DB, logger)
		if err != nil {
			logger.Fatal("Failed to initialize Redis cache", zap.Error(err))
		}
		logger.Info("Redis cache initialized")
	} else {
		logger.Info("Redis not configured, running without cache")
	}

	// Repositories
	userRepo := db.NewUserQuery(database.Pool, database.SQ, logger)
	productRepo := db.NewProductQuery(database.Pool, database.SQ, logger)
	promotionRepo := db.NewPromotionQuery(database.Pool, database.SQ, logger)
	orderRepo := db.NewOrderQuery(database.Pool, database.SQ, logger)
	cartRepo := db.NewCartItemQuery(database.Pool, logger)

	// Moysklad client
	var moyskladClient *moysklad.Client
	if cfg.Moysklad.Token != "" {
		moyskladClient = moysklad.NewClient(cfg.Moysklad.BaseURL, cfg.Moysklad.Token, logger, 2.0, 3)
		logger.Info("Moysklad client initialized")
	} else {
		logger.Warn("Moysklad token not provided, integration disabled")
	}

	// Usecases / services
	authService := services.NewAuthService(userRepo, cfg.JWT.Secret, logger)
	userUC := usecase.NewUserUsecase(userRepo, logger, cfg.JWT.Secret)
	productUC := usecase.NewProductUsecase(productRepo, stockCache, cfg.Moysklad.StockBuffer, logger)
	orderUC := usecase.NewOrderUsecase(orderRepo, productRepo, stockCache, moyskladClient, nil, logger)
	moyskladSyncService := services.NewMoyskladSyncService(
		moyskladClient,
		productRepo, // реализует ProductRepository
		stockCache,
		cfg.Moysklad.StockBuffer,
		logger,
	)

	// Scheduler
	var scheduler *services.Scheduler
	if cfg.Moysklad.AutoSync && moyskladClient != nil {
		scheduler = services.NewScheduler(moyskladSyncService, cfg.Moysklad.SyncInterval, logger)
	}

	// Email Service
	emailService := services.NewEmailService(cfg.SMTP, logger)

	// Blacklist Service (для отзыва токенов)
	var blacklistService *services.TokenBlacklistService
	if cfg.Redis.Host != "" && stockCache != nil {
		// Используем тот же Redis клиент
		blacklistService = services.NewTokenBlacklistService(stockCache.GetRedisClient(), logger)
		middleware.SetBlacklistChecker(blacklistService)
		logger.Info("Token blacklist service initialized")
	}

	// Handlers поверх usecase // Handlers
	authHandler := handlers.NewAuthHandler(authService, logger)
	userHandler := handlers.NewUserHandler(userUC, emailService, blacklistService, logger)
	productHandler := handlers.NewProductHandlerWithUsecase(productUC, logger)
	promotionHandler := handlers.NewPromotionHandler(promotionRepo, logger)
	orderHandler := handlers.NewOrderHandlerWithUsecase(orderUC, logger)
	moyskladSyncHandler := handlers.NewMoyskladSyncHandler(moyskladSyncService, logger)

	// Создаем cartHandler
	cartUC := usecase.NewCartUsecase(cartRepo, productRepo, stockCache, logger)
	cartHandler := handlers.NewCartHandler(cartUC, logger)

	var webhookHandler *handlers.WebhookHandler
	if moyskladClient != nil {
		webhookHandler = handlers.NewWebhookHandler(
			productRepo,
			moyskladClient,
			stockCache,
			cfg.Moysklad.WebhookSecret,
			logger,
		)
	}

	// Router
	router := handlers.SetupRouter(
		authHandler,
		userHandler,
		productHandler,
		promotionHandler,
		orderHandler,
		moyskladSyncHandler,
		webhookHandler,
		cartHandler,
		productRepo,
		cfg.JWT.Secret,
		logger,
	)

	addr := fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	return &App{
		Config:    cfg,
		Logger:    logger,
		DB:        database,
		Cache:     stockCache,
		Router:    router,
		Server:    srv,
		Scheduler: scheduler,
	}, nil
}

// Run запускает HTTP-сервер и планировщик, и делает graceful shutdown.
func (a *App) Run() {
	// Выполняем полную синхронизацию с МойСклад при запуске
	if a.Scheduler != nil {
		a.Logger.Info("Starting initial full sync with Moysklad on startup")

		// Защищаемся от паник при синхронизации
		func() {
			defer func() {
				if r := recover(); r != nil {
					a.Logger.Error("Initial full sync panicked",
						zap.Any("panic", r),
						zap.String("stack", fmt.Sprintf("%+v", r)))
					a.Logger.Warn("Application will continue despite sync panic")
				}
			}()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			err := a.Scheduler.FullSync(ctx)
			if err != nil {
				a.Logger.Error("Initial full sync failed",
					zap.Error(err),
					zap.String("note", "Application will continue without initial sync"))
			} else {
				a.Logger.Info("Initial full sync completed successfully")
			}
		}()

		// Запускаем планировщик для периодической синхронизации
		go func() {
			defer func() {
				if r := recover(); r != nil {
					a.Logger.Error("Scheduler panicked",
						zap.Any("panic", r),
						zap.String("stack", fmt.Sprintf("%+v", r)))
				}
			}()

			a.Scheduler.Start(context.Background())
		}()

		a.Logger.Info("Auto-sync scheduler started",
			zap.Duration("interval", a.Config.Moysklad.SyncInterval),
			zap.Float64("stock_buffer", a.Config.Moysklad.StockBuffer))
	}

	go func() {
		a.Logger.Info("Server starting", zap.String("address", a.Server.Addr))
		if err := a.Server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.Logger.Fatal("Failed to start server", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	a.Logger.Info("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := a.Server.Shutdown(ctx); err != nil {
		a.Logger.Fatal("Server forced to shutdown", zap.Error(err))
	}

	if a.Cache != nil {
		_ = a.Cache.Close()
	}
	if a.DB != nil {
		a.DB.Close()
	}

	a.Logger.Info("Server exited")
}
