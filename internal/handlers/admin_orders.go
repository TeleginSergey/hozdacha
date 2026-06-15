package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/db"
)

// AdminOrdersHandler — endpoints админки для управления заказами и просмотра аналитики.
// Все роуты этого хендлера защищены AuthMiddleware + RequireAdmin.
type AdminOrdersHandler struct {
	orders db.OrderQuery
	users  db.UserQuery
	events db.OrderEventQuery
	logger *zap.Logger
}

func NewAdminOrdersHandler(orders db.OrderQuery, users db.UserQuery, events db.OrderEventQuery, logger *zap.Logger) *AdminOrdersHandler {
	return &AdminOrdersHandler{orders: orders, users: users, events: events, logger: logger}
}

// ListOrders — GET /api/admin/orders
//
// Query params:
//   - status: csv ("pending,completed"); по умолчанию все
//   - user_id: int
//   - from, to: RFC3339 даты (orders_created_at)
//   - search: подстрока (имя, телефон) или цифры (id)
//   - limit (default 50, max 500), offset
//   - sort: created_at|total_price|reserved_until (default created_at)
//   - order: asc|desc (default desc)
func (h *AdminOrdersHandler) ListOrders(c *gin.Context) {
	f := db.OrderListFilter{}

	if s := c.Query("status"); s != "" {
		for _, v := range strings.Split(s, ",") {
			v = strings.TrimSpace(v)
			if v != "" {
				f.Statuses = append(f.Statuses, v)
			}
		}
	}
	if v := c.Query("user_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil && id > 0 {
			f.UserID = &id
		}
	}
	if v := c.Query("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.DateFrom = &t
		}
	}
	if v := c.Query("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.DateTo = &t
		}
	}
	f.Search = c.Query("search")
	f.SortBy = c.Query("sort")
	f.SortOrder = c.Query("order")
	f.Limit, _ = strconv.Atoi(c.Query("limit"))
	f.Offset, _ = strconv.Atoi(c.Query("offset"))

	orders, total, err := h.orders.ListOrders(c.Request.Context(), f)
	if err != nil {
		h.logger.Error("Failed to list admin orders", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list orders"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"items":  orders,
		"total":  total,
		"limit":  f.Limit,
		"offset": f.Offset,
	})
}

// ListToday — GET /api/admin/orders/today
// Заказы с указанным временем самовывоза сегодня (orders_pickup_at в пределах текущего дня).
func (h *AdminOrdersHandler) ListToday(c *gin.Context) {
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	endOfDay := startOfDay.Add(24 * time.Hour)

	f := db.OrderListFilter{
		Statuses:       []string{"pending"},
		PickupDateFrom: &startOfDay,
		PickupDateTo:   &endOfDay,
		Limit:          500,
		SortBy:         "pickup_at",
		SortOrder:      "asc",
	}
	orders, total, err := h.orders.ListOrders(c.Request.Context(), f)
	if err != nil {
		h.logger.Error("Failed to list today's orders", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list orders"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": orders, "total": total})
}

// Stats — GET /api/admin/orders/stats?from=...&to=...
// Счётчики заказов по статусам + процент no-show.
func (h *AdminOrdersHandler) Stats(c *gin.Context) {
	var from, to *time.Time
	if v := c.Query("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			from = &t
		}
	}
	if v := c.Query("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			to = &t
		}
	}
	stats, err := h.orders.StatsByStatus(c.Request.Context(), from, to)
	if err != nil {
		h.logger.Error("Failed to compute stats", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get stats"})
		return
	}

	total := 0
	for _, v := range stats {
		total += v
	}
	noShow := stats["expired"] + stats["cancelled"]
	noShowRate := 0.0
	if total > 0 {
		noShowRate = float64(noShow) / float64(total)
	}

	// Денежные метрики (выручка/средний чек по выкупленным) — не валим весь
	// дашборд, если этот запрос не удался: отдаём нули.
	revenue, err := h.orders.RevenueByPeriod(c.Request.Context(), from, to)
	if err != nil {
		h.logger.Warn("Failed to compute revenue stats", zap.Error(err))
		revenue = &db.RevenueStats{}
	}

	c.JSON(http.StatusOK, gin.H{
		"by_status":    stats,
		"total":        total,
		"no_show":      noShow,
		"no_show_rate": noShowRate,
		"revenue":      revenue.Revenue,
		"avg_check":    revenue.AvgCheck,
	})
}

// GetOrderDetails — GET /api/admin/orders/:id
// Карточка заказа: order + items + audit-events.
func (h *AdminOrdersHandler) GetOrderDetails(c *gin.Context) {
	orderID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || orderID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid order id"})
		return
	}
	ctx := c.Request.Context()

	order, err := h.orders.GetByID(ctx, orderID)
	if err != nil {
		h.logger.Error("Failed to get order", zap.Int64("order_id", orderID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get order"})
		return
	}
	if order == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Order not found"})
		return
	}

	items, err := h.orders.GetItemsByOrderID(ctx, orderID)
	if err != nil {
		h.logger.Error("Failed to get order items", zap.Int64("order_id", orderID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get items"})
		return
	}

	var events []*db.OrderEvent
	if h.events != nil {
		events, err = h.events.ListByOrderID(ctx, orderID)
		if err != nil {
			h.logger.Warn("Failed to load order events",
				zap.Int64("order_id", orderID), zap.Error(err))
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"order":  order,
		"items":  items,
		"events": events,
	})
}

// SearchUsers — GET /api/admin/users?search=...&limit=&offset=
func (h *AdminOrdersHandler) SearchUsers(c *gin.Context) {
	q := c.Query("search")
	limit, _ := strconv.Atoi(c.Query("limit"))
	offset, _ := strconv.Atoi(c.Query("offset"))

	users, total, err := h.users.SearchUsers(c.Request.Context(), q, limit, offset)
	if err != nil {
		h.logger.Error("Failed to search users", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to search users"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": users, "total": total})
}

// UserStats — GET /api/admin/users/:id/stats
// Карточка клиента: профиль + агрегаты + топ-товары.
func (h *AdminOrdersHandler) UserStats(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid user id"})
		return
	}
	ctx := c.Request.Context()

	user, err := h.users.GetByID(ctx, id)
	if err != nil {
		h.logger.Error("Failed to get user", zap.Int64("user_id", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get user"})
		return
	}
	if user == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	stats, err := h.orders.GetUserStats(ctx, id)
	if err != nil {
		h.logger.Error("Failed to get user stats", zap.Int64("user_id", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get stats"})
		return
	}
	topLimit, _ := strconv.Atoi(c.Query("top_limit"))
	if topLimit <= 0 {
		topLimit = 10
	}
	topProducts, err := h.orders.GetUserTopProducts(ctx, id, topLimit)
	if err != nil {
		h.logger.Warn("Failed to get user top products", zap.Error(err))
	}

	// Не отдаём приватные поля (password_hash, токены).
	c.JSON(http.StatusOK, gin.H{
		"user": gin.H{
			"id":             user.ID,
			"username":       user.Username,
			"email":          user.Email,
			"full_name":      user.FullName,
			"phone":          user.Phone,
			"email_verified": user.EmailVerified,
			"created_at":     user.CreatedAt,
			"role_id":        user.RoleID,
		},
		"stats":        stats,
		"top_products": topProducts,
	})
}

// Lookup — GET /api/admin/orders/lookup?phone=...&order_id=...
// Быстрый поиск брони по номеру или телефону.
func (h *AdminOrdersHandler) Lookup(c *gin.Context) {
	phone := strings.TrimSpace(c.Query("phone"))
	orderIDStr := strings.TrimSpace(c.Query("order_id"))
	if phone == "" && orderIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Either phone or order_id required"})
		return
	}

	f := db.OrderListFilter{
		Limit:     20,
		SortBy:    "created_at",
		SortOrder: "desc",
	}
	if orderIDStr != "" {
		f.Search = orderIDStr
	} else {
		f.Search = phone
	}
	orders, total, err := h.orders.ListOrders(c.Request.Context(), f)
	if err != nil {
		h.logger.Error("Failed lookup", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Lookup failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": orders, "total": total})
}
