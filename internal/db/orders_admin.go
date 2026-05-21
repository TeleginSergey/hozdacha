package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"
)

// OrderListFilter — параметры выборки заказов в админке.
// Все поля опциональны: пустой фильтр возвращает все заказы.
type OrderListFilter struct {
	Statuses       []string   // если задан, фильтруем по списку статусов
	UserID         *int64     // конкретный пользователь
	DateFrom       *time.Time // orders_created_at >= DateFrom
	DateTo         *time.Time // orders_created_at <= DateTo
	PickupDateFrom *time.Time // orders_pickup_at >= PickupDateFrom
	PickupDateTo   *time.Time // orders_pickup_at <= PickupDateTo
	Search         string     // ILIKE по customer_name, phone и точное по orders_id_pk если число
	Limit          int
	Offset         int
	SortBy         string // "created_at" (default), "total_price", "reserved_until", "pickup_at"
	SortOrder      string // "asc" / "desc" (default desc)
}

// ListOrders возвращает заказы по фильтру + общее количество (для пагинации).
func (o *orderQuery) ListOrders(ctx context.Context, f OrderListFilter) ([]*Order, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if f.Limit <= 0 {
		f.Limit = 50
	}
	if f.Limit > 500 {
		f.Limit = 500 // защита от слишком тяжёлых запросов
	}

	where := squirrel.And{}
	if len(f.Statuses) > 0 {
		where = append(where, squirrel.Eq{OrdersStatus: f.Statuses})
	}
	if f.UserID != nil {
		where = append(where, squirrel.Eq{OrdersUserID: *f.UserID})
	}
	if f.DateFrom != nil {
		where = append(where, squirrel.GtOrEq{OrdersCreatedAt: *f.DateFrom})
	}
	if f.DateTo != nil {
		where = append(where, squirrel.LtOrEq{OrdersCreatedAt: *f.DateTo})
	}
	if f.PickupDateFrom != nil {
		where = append(where, squirrel.GtOrEq{OrdersPickupAt: *f.PickupDateFrom})
	}
	if f.PickupDateTo != nil {
		where = append(where, squirrel.Lt{OrdersPickupAt: *f.PickupDateTo})
	}
	if s := strings.TrimSpace(f.Search); s != "" {
		// ILIKE по имени и телефону + точный матч по ID если поиск это число.
		// Используем %s параметр через squirrel.Expr, потому что squirrel.Like не делает ILIKE.
		searchClauses := squirrel.Or{
			squirrel.Expr(OrdersCustomerName+" ILIKE ?", "%"+s+"%"),
			squirrel.Expr(OrdersPhone+" ILIKE ?", "%"+s+"%"),
		}
		// Если строка содержит только цифры — добавим точный матч по id.
		if onlyDigits(s) {
			searchClauses = append(searchClauses, squirrel.Expr(OrdersID+" = ?", s))
		}
		where = append(where, searchClauses)
	}

	orderBy := orderBySafe(f.SortBy, f.SortOrder)

	// Запрос данных
	order := &Order{}
	listSQL, listArgs, err := o.sq.Select(order.columns("")...).
		From(OrdersTable).
		Where(where).
		OrderBy(orderBy).
		Limit(uint64(f.Limit)).
		Offset(uint64(max0(f.Offset))).
		ToSql()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to build list query: %w", err)
	}

	var orders []*Order
	if err := pgxscan.Select(ctx, o.runner, &orders, listSQL, listArgs...); err != nil {
		return nil, 0, fmt.Errorf("failed to list orders: %w", err)
	}

	// Запрос count'а
	countSQL, countArgs, err := o.sq.Select("COUNT(*)").
		From(OrdersTable).
		Where(where).
		ToSql()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to build count query: %w", err)
	}
	var total int
	if err := o.runner.QueryRow(ctx, countSQL, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count orders: %w", err)
	}
	return orders, total, nil
}

// StatsByStatus возвращает количество заказов в каждом статусе за период.
// Если from/to == nil — без ограничения сверху/снизу.
func (o *orderQuery) StatsByStatus(ctx context.Context, from, to *time.Time) (map[string]int, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	where := squirrel.And{}
	if from != nil {
		where = append(where, squirrel.GtOrEq{OrdersCreatedAt: *from})
	}
	if to != nil {
		where = append(where, squirrel.LtOrEq{OrdersCreatedAt: *to})
	}

	qb := o.sq.Select(OrdersStatus, "COUNT(*)").From(OrdersTable).GroupBy(OrdersStatus)
	if len(where) > 0 {
		qb = qb.Where(where)
	}
	sqlText, args, err := qb.ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build stats query: %w", err)
	}

	rows, err := o.runner.Query(ctx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute stats: %w", err)
	}
	defer rows.Close()

	stats := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("failed to scan stats: %w", err)
		}
		stats[status] = count
	}
	return stats, rows.Err()
}

// UserOrderStats — агрегаты по заказам конкретного клиента.
type UserOrderStats struct {
	TotalOrders    int     `json:"total_orders"`
	CompletedCount int     `json:"completed_count"`
	CancelledCount int     `json:"cancelled_count"`
	ExpiredCount   int     `json:"expired_count"`
	PendingCount   int     `json:"pending_count"`
	TotalRevenue   float64 `json:"total_revenue"` // сумма по completed
	AvgCheck       float64 `json:"avg_check"`     // средний чек по completed
	NoShowRate     float64 `json:"no_show_rate"`  // (expired+cancelled) / total, 0..1
}

// GetUserStats возвращает суммарную статистику клиента.
func (o *orderQuery) GetUserStats(ctx context.Context, userID int64) (*UserOrderStats, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	sqlText := `
		SELECT
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE ` + OrdersStatus + ` = 'completed') AS completed,
			COUNT(*) FILTER (WHERE ` + OrdersStatus + ` = 'cancelled') AS cancelled,
			COUNT(*) FILTER (WHERE ` + OrdersStatus + ` = 'expired') AS expired,
			COUNT(*) FILTER (WHERE ` + OrdersStatus + ` = 'pending') AS pending,
			COALESCE(SUM(` + OrdersTotalPrice + `) FILTER (WHERE ` + OrdersStatus + ` = 'completed'), 0) AS revenue,
			COALESCE(AVG(` + OrdersTotalPrice + `) FILTER (WHERE ` + OrdersStatus + ` = 'completed'), 0) AS avg_check
		FROM ` + OrdersTable + `
		WHERE ` + OrdersUserID + ` = $1
	`
	stats := &UserOrderStats{}
	err := o.runner.QueryRow(ctx, sqlText, userID).Scan(
		&stats.TotalOrders, &stats.CompletedCount, &stats.CancelledCount,
		&stats.ExpiredCount, &stats.PendingCount, &stats.TotalRevenue, &stats.AvgCheck,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get user stats: %w", err)
	}
	if stats.TotalOrders > 0 {
		stats.NoShowRate = float64(stats.ExpiredCount+stats.CancelledCount) / float64(stats.TotalOrders)
	}
	return stats, nil
}

// UserTopProduct — топ-товар клиента по числу заказов.
type UserTopProduct struct {
	ProductID   int64   `json:"product_id"`
	ProductName string  `json:"product_name"`
	OrderCount  int     `json:"order_count"`
	TotalQty    int     `json:"total_qty"`
	TotalSpent  float64 `json:"total_spent"`
}

// GetUserTopProducts — топ-N товаров клиента по completed-заказам.
func (o *orderQuery) GetUserTopProducts(ctx context.Context, userID int64, limit int) ([]*UserTopProduct, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if limit <= 0 || limit > 50 {
		limit = 10
	}

	sqlText := `
		SELECT
			oi.` + OrderItemsProductID + ` AS product_id,
			COALESCE(p.` + ProductsName + `, '') AS product_name,
			COUNT(DISTINCT oi.` + OrderItemsOrderID + `) AS order_count,
			COALESCE(SUM(oi.` + OrderItemsQuantity + `), 0) AS total_qty,
			COALESCE(SUM(oi.` + OrderItemsQuantity + ` * oi.` + OrderItemsPrice + `), 0) AS total_spent
		FROM ` + OrderItemsTable + ` oi
		JOIN ` + OrdersTable + ` o ON o.` + OrdersID + ` = oi.` + OrderItemsOrderID + `
		LEFT JOIN ` + ProductsTable + ` p ON p.` + ProductsID + ` = oi.` + OrderItemsProductID + `
		WHERE o.` + OrdersUserID + ` = $1
		  AND o.` + OrdersStatus + ` = 'completed'
		GROUP BY oi.` + OrderItemsProductID + `, p.` + ProductsName + `
		ORDER BY order_count DESC, total_spent DESC
		LIMIT $2
	`
	rows, err := o.runner.Query(ctx, sqlText, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query top products: %w", err)
	}
	defer rows.Close()

	out := make([]*UserTopProduct, 0)
	for rows.Next() {
		tp := &UserTopProduct{}
		if err := rows.Scan(&tp.ProductID, &tp.ProductName, &tp.OrderCount, &tp.TotalQty, &tp.TotalSpent); err != nil {
			return nil, fmt.Errorf("failed to scan top product: %w", err)
		}
		out = append(out, tp)
	}
	return out, rows.Err()
}

// GetItemsByOrderID возвращает позиции заказа (для админ-карточки).
func (o *orderQuery) GetItemsByOrderID(ctx context.Context, orderID int64) ([]*OrderItem, error) {
	items, err := o.getOrderItems(ctx, orderID)
	if err != nil {
		return nil, err
	}
	out := make([]*OrderItem, len(items))
	for i := range items {
		v := items[i]
		out[i] = &v
	}
	return out, nil
}

// --- helpers ---

func onlyDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

func orderBySafe(by, order string) string {
	col := OrdersCreatedAt
	switch by {
	case "total_price":
		col = OrdersTotalPrice
	case "reserved_until":
		col = OrdersReservedUntil
	case "pickup_at":
		col = OrdersPickupAt
	}
	dir := "DESC"
	if strings.EqualFold(order, "asc") {
		dir = "ASC"
	}
	return col + " " + dir
}
