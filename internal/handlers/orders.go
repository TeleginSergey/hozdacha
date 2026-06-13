package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/db"
	"github.com/TeleginSergey/hozdacha/internal/usecase"
)

type OrderHandler struct {
	orderUC  *usecase.OrderUsecase
	cartUC   *usecase.CartUsecase
	userRepo db.UserQuery
	logger   *zap.Logger
}

// Новый конструктор поверх usecase.
func NewOrderHandlerWithUsecase(orderUC *usecase.OrderUsecase, cartUC *usecase.CartUsecase, userRepo db.UserQuery, logger *zap.Logger) *OrderHandler {
	return &OrderHandler{
		orderUC:  orderUC,
		cartUC:   cartUC,
		userRepo: userRepo,
		logger:   logger,
	}
}

func (h *OrderHandler) CreateOrder(c *gin.Context) {
	// Проверяем авторизацию
	userID, exists := c.Get("user_id")
	if !exists {
		// Для API запросов возвращаем ошибку
		if c.Request.Header.Get("Content-Type") == "application/json" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":        "Нужно войти",
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный формат запроса"})
		return
	}

	// Если имя или телефон не указаны в запросе — берём из профиля пользователя.
	if req.CustomerName == "" || req.Phone == "" {
		username, _ := c.Get("username")
		email, _ := c.Get("email")

		if req.CustomerName == "" {
			if name, ok := username.(string); ok && name != "" {
				req.CustomerName = name
			} else if mail, ok := email.(string); ok && mail != "" {
				req.CustomerName = mail
			} else {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Нужно указать имя"})
				return
			}
		}

		if req.Phone == "" {
			user, err := h.userRepo.GetByID(c.Request.Context(), userID.(int64))
			if err != nil || user == nil || user.Phone == nil || *user.Phone == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Нужен номер телефона. Укажите его в профиле."})
				return
			}
			req.Phone = *user.Phone
		}
	}

	order, err := h.orderUC.CreateOrder(c.Request.Context(), usecase.CreateOrderRequest{
		UserID:       userID.(int64),
		CustomerName: req.CustomerName,
		Phone:        req.Phone,
		Address:      req.Address,
		Comment:      req.Comment,
		PickupAt:     req.PickupAt,
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
		// Окно акций закрыто — дневная акция уже не действует.
		if strings.HasPrefix(err.Error(), "promotion_window_closed:") {
			msg := strings.TrimPrefix(err.Error(), "promotion_window_closed: ")
			h.logger.Info("Order rejected: promotion window closed",
				zap.Int64("user_id", userID.(int64)),
				zap.Error(err))
			c.JSON(http.StatusConflict, gin.H{
				"error":   "promotion_window_closed",
				"message": "Окно акции закрыто. " + msg + ". Акционные товары можно бронировать только в рабочее время: по будням до 18:00, по выходным до 16:00.",
			})
			return
		}
		// МойСклад не настроен — не создаём заказ вообще.
		if strings.Contains(err.Error(), "moysklad integration is not fully configured") {
			h.logger.Error("Order rejected: Moysklad not configured", zap.Error(err))
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Система приёма заказов временно недоступна. Попробуйте позже."})
			return
		}
		h.logger.Error("Failed to create order", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось создать заказ"})
		return
	}

	c.JSON(http.StatusCreated, order)
}

// CancelOrder — пользователь отменяет свой pending-заказ.
// POST /api/orders/:id/cancel
func (h *OrderHandler) CancelOrder(c *gin.Context) {
	userIDRaw, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Нужно войти"})
		return
	}
	userID, ok := userIDRaw.(int64)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Неверный пользователь"})
		return
	}

	orderID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || orderID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный номер заказа"})
		return
	}

	if err := h.orderUC.CancelOrderByUser(c.Request.Context(), userID, orderID); err != nil {
		if errors.Is(err, db.ErrOrderForbidden) {
			c.JSON(http.StatusForbidden, gin.H{"error": "Это не ваш заказ"})
			return
		}
		h.logger.Error("Failed to cancel order",
			zap.Int64("order_id", orderID),
			zap.Int64("user_id", userID),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось отменить заказ"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "cancelled"})
}

// ShipOrder — продавец отметил что клиент пришёл и выкупил товар.
// POST /api/admin/orders/:id/ship
func (h *OrderHandler) ShipOrder(c *gin.Context) {
	orderID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || orderID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный номер заказа"})
		return
	}

	if err := h.orderUC.CompleteOrder(c.Request.Context(), orderID, actorUserID(c)); err != nil {
		h.logger.Error("Failed to ship order",
			zap.Int64("order_id", orderID),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось отгрузить заказ"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "completed"})
}

// CancelOrderByAdmin — админ отменяет любой pending-заказ: товары возвращаются,
// заказ удаляется в МойСклад, статус становится 'cancelled'.
// POST /api/admin/orders/:id/cancel
func (h *OrderHandler) CancelOrderByAdmin(c *gin.Context) {
	orderID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || orderID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный номер заказа"})
		return
	}

	if err := h.orderUC.CancelOrderByAdmin(c.Request.Context(), orderID, actorUserID(c)); err != nil {
		h.logger.Error("Failed to cancel order by admin",
			zap.Int64("order_id", orderID),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось отменить заказ"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "cancelled"})
}

// ExpireOrder — админ форсирует истечение брони (то же что делает cron автоматически).
// POST /api/admin/orders/:id/expire
func (h *OrderHandler) ExpireOrder(c *gin.Context) {
	orderID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || orderID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный номер заказа"})
		return
	}

	if err := h.orderUC.ExpireOrderByAdmin(c.Request.Context(), orderID, actorUserID(c)); err != nil {
		h.logger.Error("Failed to expire order",
			zap.Int64("order_id", orderID),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось отменить просроченный заказ"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "expired"})
}

// actorUserID — достаёт id залогиненного админа/пользователя из контекста (для audit log).
// Возвращает nil если не залогинен.
func actorUserID(c *gin.Context) *int64 {
	raw, exists := c.Get("user_id")
	if !exists {
		return nil
	}
	if id, ok := raw.(int64); ok {
		return &id
	}
	return nil
}

func (h *OrderHandler) GetUserOrders(c *gin.Context) {
	// Проверяем авторизацию
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Нужно войти"})
		return
	}

	orders, err := h.orderUC.GetUserOrders(c.Request.Context(), userID.(int64))
	if err != nil {
		h.logger.Error("Failed to get user orders", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось загрузить заказы"})
		return
	}

	c.JSON(http.StatusOK, orders)
}
