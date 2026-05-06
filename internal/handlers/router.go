package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/db"
	"github.com/TeleginSergey/hozdacha/internal/middleware"
)

// SetupRouter — общий конструктор роутера, переиспользуемый из app.NewApp().
func SetupRouter(
	authHandler *AuthHandler,
	userHandler *UserHandler,
	productHandler *ProductHandler,
	promotionHandler *PromotionHandler,
	orderHandler *OrderHandler,
	moyskladSyncHandler *MoyskladSyncHandler,
	webhookHandler *WebhookHandler,
	cartHandler *CartHandler,
	productQuery db.ProductQuery,
	jwtSecret string,
	logger *zap.Logger,
) *gin.Engine {
	router := gin.Default()

	// Глобальные middleware безопасности
	router.Use(middleware.SecurityHeaders())
	router.Use(middleware.CORS())
	router.Use(middleware.RateLimit())

	// Статические файлы
	router.Static("/static", "./web/static")
	router.LoadHTMLGlob("web/templates/*")

	// Главная страница
	router.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", gin.H{
			"title": "Телегинс Шоп",
		})
	})

	// Каталог
	router.GET("/catalog", func(c *gin.Context) {
		c.HTML(http.StatusOK, "catalog.html", gin.H{
			"title": "Каталог товаров",
		})
	})

	// Детальная страница товара (используем общую функцию)
	router.GET("/product/:id", func(c *gin.Context) {
		productHandler.RenderProductPage(c)
	})

	// Корзина (требует авторизации)
	router.GET("/cart", middleware.RequireAuth(), func(c *gin.Context) {
		c.HTML(http.StatusOK, "cart.html", gin.H{
			"title": "Корзина - Телегинс Шоп",
		})
	})

	// Страницы аутентификации
	router.GET("/login", func(c *gin.Context) {
		c.HTML(http.StatusOK, "login.html", gin.H{
			"title": "Вход - Телегинс Шоп",
		})
	})

	router.GET("/register", func(c *gin.Context) {
		c.HTML(http.StatusOK, "register.html", gin.H{
			"title": "Регистрация - Телегинс Шоп",
		})
	})

	// Verify email page removed

	// Профиль (требует авторизации)
	router.GET("/profile", middleware.RequireAuth(), func(c *gin.Context) {
		c.HTML(http.StatusOK, "profile.html", gin.H{
			"title": "Личный кабинет - Телегинс Шоп",
		})
	})

	// Админ-панель (требует авторизации)
	router.GET("/admin", middleware.RequireAuth(), func(c *gin.Context) {
		c.HTML(http.StatusOK, "admin.html", gin.H{
			"title": "Админ-панель",
		})
	})

	// Health check endpoint для Traefik/Docker
	healthHandler := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":    "ok",
			"timestamp": time.Now().Unix(),
		})
	}
	router.GET("/health", healthHandler)
	router.HEAD("/health", healthHandler)

	// Публичные API
	api := router.Group("/api")
	{
		// Аутентификация
		auth := api.Group("/auth")
		{
			auth.POST("/register", userHandler.Register)
			auth.POST("/login", userHandler.Login)
			auth.POST("/logout", middleware.AuthMiddleware(jwtSecret), userHandler.Logout)
			auth.POST("/verify-email", userHandler.VerifyEmail)
			auth.POST("/resend-code", userHandler.ResendVerificationCode)
			auth.POST("/forgot-password", userHandler.ForgotPassword)
			auth.POST("/reset-password", userHandler.ResetPassword)
			// Profile endpoints require authentication
			authProfile := auth.Group("/profile")
			authProfile.Use(middleware.AuthMiddleware(jwtSecret))
			authProfile.Use(middleware.RequireAuth())
			{
				authProfile.PUT("", userHandler.UpdateProfile)
				authProfile.GET("", userHandler.GetProfile)
			}

			// Token verification endpoint
			auth.GET("/verify", func(c *gin.Context) {
				middleware.AuthMiddleware(jwtSecret)(c)
				if c.IsAborted() {
					return
				}
				userHandler.VerifyToken(c)
			})
		}

		// Товары (без защиты - доступны всем)
		products := api.Group("/products")
		products.Use(middleware.ProductRateLimit())
		{
			products.GET("", productHandler.GetProducts)
			products.GET("/search", productHandler.SearchProducts)
			products.GET("/:id", productHandler.GetProduct)
		}

		// Акции
		api.GET("/promotions", promotionHandler.GetActivePromotions)

		// Корзина (с защитой)
		cart := api.Group("/cart")
		cart.Use(middleware.RequireAuth())
		{
			cart.POST("", cartHandler.AddToCart)
			cart.GET("", cartHandler.GetCart)
			cart.PUT("", cartHandler.UpdateCart)
			cart.DELETE("", cartHandler.ClearCart)
			cart.DELETE("/:id", cartHandler.RemoveFromCart)
			cart.GET("/total", cartHandler.GetCartTotal)
		}

		// Заказы (требует авторизации)
		orders := api.Group("/orders")
		orders.Use(middleware.RequireAuth())
		{
			orders.POST("", orderHandler.CreateOrder)
		}

		// Webhook для МойСклад (публичный endpoint, но защищен подписью)
		if webhookHandler != nil {
			api.POST("/webhooks/moysklad", webhookHandler.HandleWebhook)
			api.POST("/webhooks/stock", webhookHandler.HandleStockWebhook)
		}

		// Авторизация (публичный endpoint) - строгий rate limit
		api.POST("/admin/login", middleware.StrictRateLimit(), authHandler.Login)
	}

	// Админ API (требуют авторизации)
	admin := router.Group("/api/admin")
	admin.Use(middleware.StrictRateLimit()) // Строгий лимит для админки
	admin.Use(middleware.AuthMiddleware(jwtSecret))
	{
		// Акции
		admin.GET("/promotions", promotionHandler.GetAllPromotions)
		admin.GET("/promotions/:id", promotionHandler.GetPromotion)
		admin.POST("/promotions", promotionHandler.CreatePromotion)
		admin.PUT("/promotions/:id", promotionHandler.UpdatePromotion)
		admin.DELETE("/promotions/:id", promotionHandler.DeletePromotion)

		// Синхронизация с МойСклад
		admin.POST("/products/sync", moyskladSyncHandler.SyncProducts)
		admin.POST("/products/sync/full", moyskladSyncHandler.SyncProductsFull)
	}

	return router
}
