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
	"github.com/TeleginSergey/hozdacha/internal/resilience"
	"github.com/TeleginSergey/hozdacha/internal/services"
	"github.com/TeleginSergey/hozdacha/internal/usecase"
	"github.com/redis/go-redis/v9"
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
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// DB
	database, err := db.New(&cfg.DB, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
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
	promotionLinkRepo := db.NewPromotionLinkQuery(database.Pool, database.SQ, logger)
	orderRepo := db.NewOrderQuery(database.Pool, database.SQ, logger)
	orderEventRepo := db.NewOrderEventQuery(database.Pool, logger)
	cartRepo := db.NewCartItemQuery(database.Pool, logger)

	// Moysklad client (circuit breaker + лимит параллельных запросов)
	var moyskladClient *moysklad.Client
	if cfg.Moysklad.Token != "" {
		maxConc := cfg.Moysklad.MaxConcurrentRequests
		if maxConc < 1 {
			maxConc = 8
		}
		cb := resilience.NewCircuitBreaker(cfg.Moysklad.CircuitFailThreshold, cfg.Moysklad.CircuitOpenTimeout)
		moyskladClient = moysklad.NewClient(
			cfg.Moysklad.BaseURL,
			cfg.Moysklad.Token,
			logger,
			cfg.Moysklad.RequestsPerSecond,
			cfg.Moysklad.MaxRetries,
			moysklad.WithOutboundConcurrency(maxConc),
			moysklad.WithCircuitBreaker(cb),
			moysklad.WithOrderDefaults(cfg.Moysklad.OrganizationID, cfg.Moysklad.AgentID),
		)
		if cfg.Moysklad.OrganizationID == "" || cfg.Moysklad.AgentID == "" {
			logger.Warn("Moysklad organization/agent ID not set — orders will NOT be sent to Moysklad",
				zap.String("hint", "set MOYSKLAD_ORGANIZATION_ID and MOYSKLAD_AGENT_ID env vars"))
		}
		logger.Info("Moysklad client initialized",
			zap.Int("max_concurrent_requests", maxConc),
			zap.Int("circuit_fail_threshold", cfg.Moysklad.CircuitFailThreshold))
	} else {
		logger.Warn("Moysklad token not provided, integration disabled")
	}

	// Usecases / services
	authService := services.NewAuthService(userRepo, cfg.JWT.Secret, logger)
	userUC := usecase.NewUserUsecase(userRepo, logger, cfg.JWT.Secret)
	productUC := usecase.NewProductUsecase(productRepo, stockCache, cfg.Moysklad.StockBuffer, logger)
	categoryRepoForPricer := &db.CategoryQuery{DB: database}
	productUC.SetPromotionPricer(usecase.NewPromotionPricer(promotionLinkRepo, categoryRepoForPricer, logger))
	orderUC := usecase.NewOrderUsecase(orderRepo, productRepo, stockCache, moyskladClient, orderEventRepo, logger)
	categoryRepo := &db.CategoryQuery{DB: database}
	moyskladSyncService := services.NewMoyskladSyncService(
		moyskladClient,
		productRepo, // реализует ProductRepository
		categoryRepo,
		stockCache,
		cfg.Moysklad.StockBuffer,
		logger,
	)

	// Scheduler
	var scheduler *services.Scheduler
	if cfg.Moysklad.AutoSync && moyskladClient != nil {
		syncWorkers := cfg.Moysklad.SyncWorkers
		if syncWorkers < 1 {
			syncWorkers = 3
		}
		if syncWorkers > 32 {
			syncWorkers = 32
		}

		var imageSync *services.ImageSyncService
		if cfg.Moysklad.ImageSyncInterval > 0 || cfg.Moysklad.ImageSyncTime != "" {
			imageSync = services.NewImageSyncService(moyskladClient, productRepo, "./web/static/images/products", logger)
		}

		scheduler = services.NewScheduler(moyskladSyncService, cfg.Moysklad.SyncInterval, syncWorkers, cfg.Moysklad.ReseedFullInterval, imageSync, cfg.Moysklad.ImageSyncInterval, cfg.Moysklad.ImageSyncTime, logger)
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

	// Health check service
	var healthCheckService *services.HealthCheckService
	if database != nil {
		var redisClient *redis.Client
		if stockCache != nil {
			redisClient = stockCache.GetRedisClient()
		}
		healthCheckService = services.NewHealthCheckService(database.Pool, redisClient, logger)
	}

	// Создаем cartUC перед использованием в других handlers
	cartUC := usecase.NewCartUsecase(cartRepo, productRepo, logger)

	// Handlers поверх usecase // Handlers
	authHandler := handlers.NewAuthHandler(authService, logger)
	userHandler := handlers.NewUserHandler(userUC, emailService, blacklistService, logger)
	productHandler := handlers.NewProductHandlerWithUsecase(productUC, database, logger)
	promotionHandler := handlers.NewPromotionHandler(promotionRepo, logger)
	orderHandler := handlers.NewOrderHandlerWithUsecase(orderUC, cartUC, userRepo, logger)
	moyskladSyncHandler := handlers.NewMoyskladSyncHandler(moyskladSyncService, logger)
	cartHandler := handlers.NewCartHandler(cartUC, logger)
	adminOrdersHandler := handlers.NewAdminOrdersHandler(orderRepo, userRepo, orderEventRepo, logger)

	var webhookHandler *handlers.WebhookHandler
	webhookInbox := db.NewWebhookInboxRepo(database.Pool, logger)
	webhookProcessor := services.NewWebhookProcessor(productRepo, moyskladClient, stockCache, logger)
	if moyskladClient != nil {
		maxWhAttempts := cfg.Moysklad.WebhookMaxAttempts
		if maxWhAttempts < 1 {
			maxWhAttempts = 15
		}
		webhookHandler = handlers.NewWebhookHandler(
			cfg.Moysklad.WebhookSecret,
			webhookInbox,
			maxWhAttempts,
			logger,
		)
		whBatch := cfg.Moysklad.WebhookInboxBatch
		if whBatch < 1 {
			whBatch = 10
		}
		whWorker := services.NewWebhookInboxWorker(webhookInbox, webhookProcessor, cfg.Moysklad.WebhookWorkerInterval, whBatch, logger)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("webhook inbox worker panicked", zap.Any("panic", r))
				}
			}()
			whWorker.Run(context.Background())
		}()
		logger.Info("Webhook inbox worker started")
	}

	// Reservation expiry: проверяем истёкшие брони (orders.reserved_until < now) каждые 5 минут.
	expirySvc := services.NewReservationExpiryService(
		orderRepo,
		orderEventRepo,
		moyskladClient,
		stockCache,
		5*time.Minute,
		100,
		logger,
	)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("reservation expiry service panicked", zap.Any("panic", r))
			}
		}()
		expirySvc.Run(context.Background())
	}()

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
		adminOrdersHandler,
		productRepo,
		cfg.JWT.Secret,
		cfg.Server.CORSOrigins,
		healthCheckService,
		logger,
	)

	addr := fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
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
	// СНАЧАЛА запускаем HTTP сервер
	go func() {
		a.Logger.Info("Server starting", zap.String("address", a.Server.Addr))
		if err := a.Server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.Logger.Fatal("Failed to start server", zap.Error(err))
		}
	}()

	// ПОТОМ запускаем синхронизацию с МойСклад в фоне
	if a.Scheduler != nil {
		a.Logger.Info("Starting Moysklad sync services in background")

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

		// Выполняем полную синхронизацию с МойСклад в фоне
		go func() {
			defer func() {
				if r := recover(); r != nil {
					a.Logger.Error("Initial full sync panicked",
						zap.Any("panic", r),
						zap.String("stack", fmt.Sprintf("%+v", r)))
					a.Logger.Warn("Application will continue despite sync panic")
				}
			}()

			// Даем серверу время на запуск
			time.Sleep(5 * time.Second)

			a.Logger.Info("Starting initial full sync with 30 minute timeout")
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()

			err := a.Scheduler.FullSync(ctx)
			if err != nil {
				a.Logger.Error("Initial full sync failed",
					zap.Error(err),
					zap.String("note", "Application will continue without initial sync, periodic sync will handle updates"))
			} else {
				a.Logger.Info("Initial full sync completed successfully")
			}
		}()

		a.Logger.Info("Auto-sync scheduler started",
			zap.Duration("interval", a.Config.Moysklad.SyncInterval),
			zap.Float64("stock_buffer", a.Config.Moysklad.StockBuffer))
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	a.Logger.Info("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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
