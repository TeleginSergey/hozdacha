package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/services"
	"github.com/TeleginSergey/hozdacha/internal/usecase"
)

type OrderHandler struct {
	orderUC *usecase.OrderUsecase
	logger  *zap.Logger
}

// Временный конструктор для старого сервиса (backward compatibility).
func NewOrderHandler(orderService *services.OrderService, logger *zap.Logger) *OrderHandler {
	uc := usecase.NewOrderUsecase(
		orderService.OrderQuery(),
		orderService.ProductQuery(),
		orderService.StockCache(),
		orderService.MoyskladClient(),
		orderService.TelegramBot(),
		logger,
	)
	return &OrderHandler{
		orderUC: uc,
		logger:  logger,
	}
}

// Новый конструктор поверх usecase.
func NewOrderHandlerWithUsecase(orderUC *usecase.OrderUsecase, logger *zap.Logger) *OrderHandler {
	return &OrderHandler{
		orderUC: orderUC,
		logger:  logger,
	}
}

func (h *OrderHandler) CreateOrder(c *gin.Context) {
	// Проверяем авторизацию
	userID, exists := c.Get("user_id")
	if !exists {
		// Для API запросов возвращаем ошибку
		if c.Request.Header.Get("Content-Type") == "application/json" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":        "Authentication required",
				"redirect_url": "/login?redirect=checkout",
			})
			return
		}

		// Для HTML запросов редиректим на страницу логина
		redirectURL := "/login?redirect=checkout"
		c.Redirect(http.StatusFound, redirectURL)
		return
	}

	var req usecase.CreateOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}

	// Получаем данные пользователя из профиля, если не указаны в запросе
	if req.CustomerName == "" || req.Phone == "" {
		// Получаем информацию о пользователе
		username, _ := c.Get("username")
		email, _ := c.Get("email")

		// Если имя не указано, используем username или email
		if req.CustomerName == "" {
			if name, ok := username.(string); ok && name != "" {
				req.CustomerName = name
			} else if mail, ok := email.(string); ok && mail != "" {
				req.CustomerName = mail
			} else {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Customer name is required"})
				return
			}
		}

		// Телефон должен быть указан в профиле
		if req.Phone == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Phone number is required. Please update your profile."})
			return
		}
	}

	order, err := h.orderUC.CreateOrder(c.Request.Context(), usecase.CreateOrderRequest{
		UserID:       userID.(int64),
		CustomerName: req.CustomerName,
		Phone:        req.Phone,
		Address:      req.Address,
		Comment:      req.Comment,
		Items:        req.Items,
	})
	if err != nil {
		h.logger.Error("Failed to create order", zap.Error(err))
		// Не раскрываем детали ошибки клиенту
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create order"})
		return
	}

	c.JSON(http.StatusCreated, order)
}
