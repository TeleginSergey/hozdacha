package handlers

import (
	"html/template"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/db"
	"github.com/TeleginSergey/hozdacha/internal/metrics"
	"github.com/TeleginSergey/hozdacha/internal/middleware"
	"github.com/TeleginSergey/hozdacha/internal/services"
)

// trustedProxies возвращает список доверенных прокси для router.SetTrustedProxies.
// Берётся из TRUSTED_PROXIES (CIDR через запятую); по умолчанию — приватные диапазоны,
// где живёт reverse-proxy (Traefik/Dokploy). Это заставляет c.ClientIP() брать реальный
// IP из X-Forwarded-For, но игнорировать подделку заголовка клиентом.
func trustedProxies() []string {
	if raw := strings.TrimSpace(os.Getenv("TRUSTED_PROXIES")); raw != "" {
		var out []string
		for _, p := range strings.Split(raw, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	return []string{"127.0.0.1/32", "::1/128", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}
}

// metricsTokenOK проверяет токен доступа к /metrics: через заголовок
// Authorization: Bearer <token> или query-параметр ?token=<token>.
func metricsTokenOK(c *gin.Context, want string) bool {
	if c.Query("token") == want {
		return true
	}
	const prefix = "Bearer "
	if h := c.GetHeader("Authorization"); strings.HasPrefix(h, prefix) {
		return strings.TrimSpace(h[len(prefix):]) == want
	}
	return false
}

// templateFuncs — общие функции для всех HTML-шаблонов.
var templateFuncs = template.FuncMap{
	// deref безопасно разыменовывает указатель на float64. Для nil возвращает 0.
	"deref": func(v *float64) float64 {
		if v == nil {
			return 0
		}
		return *v
	},
}

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
	adminOrdersHandler *AdminOrdersHandler,
	productQuery db.ProductQuery,
	jwtSecret string,
	corsOrigins []string,
	healthCheckService *services.HealthCheckService,
	logger *zap.Logger,
) *gin.Engine {
	router := gin.Default()

	// Доверяем только reverse-proxy (Traefik/Dokploy), иначе Gin доверяет всем и
	// c.ClientIP() можно подменить через X-Forwarded-For, обойдя rate-limit.
	if err := router.SetTrustedProxies(trustedProxies()); err != nil {
		logger.Warn("failed to set trusted proxies", zap.Error(err))
	}

	// Добавляем health check service в контекст для middleware
	router.Use(func(c *gin.Context) {
		c.Set("health_service", healthCheckService)
		c.Next()
	})

	// Глобальные middleware безопасности
	router.Use(middleware.SecurityHeaders())
	router.Use(middleware.CORS(corsOrigins))
	router.Use(middleware.RateLimit())
	// Сбор HTTP-метрик (RED) по каждому запросу — после CORS/RateLimit, чтобы
	// учитывать в т.ч. отклонённые запросы.
	router.Use(middleware.Metrics())

	// Статические файлы
	router.Static("/static", "./web/static")
	// Иконки в корне (браузеры/краулеры запрашивают /favicon.ico).
	router.StaticFile("/favicon.ico", "./web/static/images/favicon-48.png")
	router.StaticFile("/favicon.svg", "./web/static/images/favicon.svg")
	router.SetFuncMap(templateFuncs)
	// Подключаем страничные шаблоны + partials (общий хедер и т.п.).
	// Чтобы partials/header.html был доступен через {{ template "header.html" . }}
	// внутри страничных шаблонов, оба глоба регистрируются на одном наборе шаблонов.
	pageGlob := template.New("pages").Funcs(templateFuncs)
	pageGlob, errGlob := pageGlob.ParseGlob("web/templates/*.html")
	if errGlob != nil {
		logger.Fatal("failed to parse page templates", zap.Error(errGlob))
	}
	pageGlob, errGlob = pageGlob.ParseGlob("web/templates/**/*.html")
	if errGlob != nil {
		logger.Fatal("failed to parse partial templates", zap.Error(errGlob))
	}
	router.SetHTMLTemplate(pageGlob)

	// Главная страница
	router.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", gin.H{
			"title": "ХозДача",
		})
	})

	// Каталог
	router.GET("/catalog", func(c *gin.Context) {
		c.HTML(http.StatusOK, "catalog.html", gin.H{
			"title": "Каталог товаров",
		})
	})

	// Отдельная страница со всеми акциями.
	router.GET("/promotions", func(c *gin.Context) {
		c.HTML(http.StatusOK, "promotions.html", gin.H{
			"title": "Акции — ХозДача",
		})
	})

	// Контакты и как нас найти.
	router.GET("/contacts", func(c *gin.Context) {
		c.HTML(http.StatusOK, "contacts.html", gin.H{
			"title": "Контакты — ХозДача",
		})
	})

	// Детальная страница товара (используем общую функцию)
	router.GET("/product/:id", func(c *gin.Context) {
		productHandler.RenderProductPage(c)
	})

	// Корзина: как /profile — отдаём HTML без RequireAuth; проверка JWT в JS (checkAuth + Bearer в API).
	router.GET("/cart", middleware.AuthMiddleware(jwtSecret), func(c *gin.Context) {
		c.HTML(http.StatusOK, "cart.html", gin.H{
			"title": "Корзина - ХозДача",
		})
	})

	// Страницы аутентификации
	router.GET("/login", func(c *gin.Context) {
		c.HTML(http.StatusOK, "login.html", gin.H{
			"title": "Вход - ХозДача",
		})
	})

	router.GET("/register", func(c *gin.Context) {
		c.HTML(http.StatusOK, "register.html", gin.H{
			"title": "Регистрация - ХозДача",
		})
	})

	router.GET("/forgot-password", func(c *gin.Context) {
		c.HTML(http.StatusOK, "forgot-password.html", gin.H{
			"title": "Сброс пароля - ХозДача",
		})
	})

	// Verify email page removed

	// Профиль (требует авторизации)
	router.GET("/profile", middleware.AuthMiddleware(jwtSecret), func(c *gin.Context) {
		c.HTML(http.StatusOK, "profile.html", gin.H{
			"title": "Личный кабинет - ХозДача",
		})
	})

	// Мои заказы (требует авторизации)
	router.GET("/orders", middleware.AuthMiddleware(jwtSecret), func(c *gin.Context) {
		c.HTML(http.StatusOK, "orders.html", gin.H{
			"title": "Мои заказы - ХозДача",
		})
	})

	// Админ-панель: страница публичная (содержит форму логина), сами API под
	// /api/admin защищены AuthMiddleware + RequireAdmin. JS проверяет токен
	// в localStorage и переключает loginSection ↔ adminSection.
	router.GET("/admin", func(c *gin.Context) {
		c.HTML(http.StatusOK, "admin.html", gin.H{
			"title": "Админ-панель",
		})
	})

	// Ultra lightweight health check для Dokploy/Traefik (работает под нагрузкой)
	healthHandler := func(c *gin.Context) {
		// Максимально легкий ответ - без JSON, без логирования, без задержек
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.String(http.StatusOK, "ok")
	}
	router.GET("/health", healthHandler)
	router.HEAD("/health", healthHandler)

	// Самый легкий healthcheck - только статус код
	healthPingHandler := func(c *gin.Context) {
		// Только статус 200, без тела ответа
		c.Status(http.StatusOK)
	}
	router.GET("/ping", healthPingHandler)
	router.HEAD("/ping", healthPingHandler)

	// Детальный health check с использованием HealthCheckService
	healthDetailHandler := func(c *gin.Context) {
		// Используем HealthCheckService если доступен
		if healthService, exists := c.Get("health_service"); exists {
			if hs, ok := healthService.(*services.HealthCheckService); ok {
				status := hs.GetStatus(c.Request.Context())
				c.JSON(http.StatusOK, status)
				return
			}
		}

		// Fallback если HealthCheckService не инициализирован
		c.JSON(http.StatusOK, gin.H{
			"status":    "ok",
			"timestamp": time.Now().Unix(),
			"note":      "fallback mode - health check service not available",
		})
	}
	router.GET("/health/detail", healthDetailHandler)

	// Prometheus scrape endpoint. По умолчанию выключен; включается заданием
	// METRICS_TOKEN — тогда требует Authorization: Bearer <token> или ?token=.
	// Сами метрики дублируются в админ-панель через /api/admin/metrics (под JWT).
	if metricsToken := strings.TrimSpace(os.Getenv("METRICS_TOKEN")); metricsToken != "" {
		promHandler := promhttp.HandlerFor(metrics.Registry, promhttp.HandlerOpts{})
		router.GET("/metrics", func(c *gin.Context) {
			if !metricsTokenOK(c, metricsToken) {
				c.AbortWithStatus(http.StatusUnauthorized)
				return
			}
			promHandler.ServeHTTP(c.Writer, c.Request)
		})
	}

	// Публичные API
	api := router.Group("/api")
	{
		// Аутентификация. Жёсткий rate-limit вешаем точечно на чувствительные
		// эндпоинты (перебор кодов / спам), не трогая /profile и /verify, чтобы не
		// блокировать легитимных пользователей за общим IP (CGNAT).
		auth := api.Group("/auth")
		authRL := middleware.AuthRateLimit()
		{
			auth.POST("/register", authRL, userHandler.Register)
			auth.POST("/login", authRL, userHandler.Login)
			auth.POST("/logout", middleware.AuthMiddleware(jwtSecret), userHandler.Logout)
			auth.POST("/verify-email", authRL, middleware.AuthMiddleware(jwtSecret), userHandler.VerifyEmail)
			auth.POST("/resend-code", authRL, userHandler.ResendVerificationCode)
			auth.POST("/send-verification", authRL, userHandler.ResendVerificationCode) // Alias для совместимости с фронтом
			auth.POST("/forgot-password", authRL, userHandler.ForgotPassword)
			auth.POST("/reset-password", authRL, userHandler.ResetPassword)
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

		// Категории
		api.GET("/categories", productHandler.ListCategories)

		// Акции (feed и products — до /:id, чтобы gin не перехватывал как id)
		api.GET("/promotions/feed", promotionHandler.GetPromotionsFeed)
		api.GET("/promotions/products", promotionHandler.GetPromotionalProducts)
		api.GET("/promotions/:id/products", promotionHandler.GetPromotionProducts)
		api.GET("/promotions", promotionHandler.GetActivePromotions)

		// Корзина (с защитой): сначала парсим JWT, иначе RequireAuth не видит user_id.
		cart := api.Group("/cart")
		cart.Use(middleware.AuthMiddleware(jwtSecret))
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
		orders.Use(middleware.AuthMiddleware(jwtSecret))
		orders.Use(middleware.RequireAuth())
		{
			orders.POST("", orderHandler.CreateOrder)
			orders.GET("", orderHandler.GetUserOrders)
			// Пользователь отменяет свою же бронь — возврат стока и удаление в МойСклад.
			orders.POST("/:id/cancel", orderHandler.CancelOrder)
		}

		// Webhook для МойСклад (публичный endpoint, но защищен подписью)
		if webhookHandler != nil {
			api.POST("/webhooks/moysklad", webhookHandler.HandleWebhook)
			api.POST("/webhooks/stock", webhookHandler.HandleStockWebhook)
		}

		// Авторизация (публичный endpoint) - строгий rate limit
		api.POST("/admin/login", middleware.StrictRateLimit(), authHandler.Login)
	}

	// Админ API (требуют авторизации + роль admin)
	admin := router.Group("/api/admin")
	admin.Use(middleware.StrictRateLimit())
	admin.Use(middleware.AuthMiddleware(jwtSecret))
	admin.Use(middleware.RequireAdmin())
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

		// Управление заказами в магазине
		// /ship  — клиент пришёл и выкупил → status=completed
		// /expire — ручное истечение брони → возврат стока + удаление в МойСклад
		adminOrders := admin.Group("/orders")
		{
			// Списки и аналитика. Lookup объявляется до /:id, чтобы gin не путал
			// "lookup" / "today" / "stats" с параметром :id.
			adminOrders.GET("", adminOrdersHandler.ListOrders)
			adminOrders.GET("/today", adminOrdersHandler.ListToday)
			adminOrders.GET("/stats", adminOrdersHandler.Stats)
			adminOrders.GET("/lookup", adminOrdersHandler.Lookup)
			adminOrders.GET("/:id", adminOrdersHandler.GetOrderDetails)

			// Изменение статусов.
			adminOrders.POST("/:id/ship", orderHandler.ShipOrder)
			adminOrders.POST("/:id/cancel", orderHandler.CancelOrderByAdmin)
			adminOrders.POST("/:id/expire", orderHandler.ExpireOrder)
		}

		// Метрики приложения для админ-панели (снимок Prometheus-реестра в JSON).
		admin.GET("/metrics", func(c *gin.Context) {
			c.JSON(http.StatusOK, metrics.GetSnapshot())
		})

		// Клиенты — поиск и статистика.
		adminUsers := admin.Group("/users")
		{
			adminUsers.GET("", adminOrdersHandler.SearchUsers)
			adminUsers.GET("/:id/stats", adminOrdersHandler.UserStats)
		}

		if webhookHandler != nil {
			admin.GET("/webhooks/dead-letter", webhookHandler.ListDeadLetter)
			admin.POST("/webhooks/replay/:id", webhookHandler.ReplayDeadLetter)
		}
	}

	return router
}
