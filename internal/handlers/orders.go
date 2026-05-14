package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/db"
	"github.com/TeleginSergey/hozdacha/internal/usecase"
)

type OrderHandler struct {
	orderUC *usecase.OrderUsecase
	cartUC  *usecase.CartUsecase
	logger  *zap.Logger
}

// Новый конструктор поверх usecase.
func NewOrderHandlerWithUsecase(orderUC *usecase.OrderUsecase, cartUC *usecase.CartUsecase, logger *zap.Logger) *OrderHandler {
	return &OrderHandler{
		orderUC: orderUC,
		cartUC:  cartUC,
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
		// Недостаточно стока — сообщаем клиенту по делу.
		if errors.Is(err, db.ErrInsufficientStock) {
			h.logger.Info("Order rejected: insufficient stock",
				zap.Int64("user_id", userID.(int64)),
				zap.Error(err))
			c.JSON(http.StatusConflict, gin.H{
				"error":   "insufficient_stock",
				"message": "Один из товаров в корзине уже недоступен в нужном количестве. Обновите корзину.",
			})
			return
		}
		h.logger.Error("Failed to create order", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create order"})
		return
	}

	c.JSON(http.StatusCreated, order)
}

func (h *OrderHandler) GetUserOrders(c *gin.Context) {
	// Проверяем авторизацию
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required"})
		return
	}

	orders, err := h.orderUC.GetUserOrders(c.Request.Context(), userID.(int64))
	if err != nil {
		h.logger.Error("Failed to get user orders", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get orders"})
		return
	}

	c.JSON(http.StatusOK, orders)
}
