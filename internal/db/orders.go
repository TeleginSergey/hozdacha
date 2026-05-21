package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/elgris/stom"
	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

const OrdersTable = "orders"
const OrderItemsTable = "order_items"

// ErrInsufficientStock возвращается, если на момент создания заказа в БД
// фактически меньше товара, чем требуется. Это финальная атомарная проверка
// и она имеет приоритет над любыми кэшами.
var ErrInsufficientStock = errors.New("insufficient stock")

// ErrOrderForbidden возвращается, если пользователь пытается изменить не свой заказ.
var ErrOrderForbidden = errors.New("order does not belong to user")

const (
	OrdersID            = "orders_id_pk"
	OrdersUserID        = "orders_user_id_fk"
	OrdersStatus        = "orders_status"
	OrdersTotalPrice    = "orders_total_price"
	OrdersCustomerName  = "orders_customer_name"
	OrdersPhone         = "orders_phone"
	OrdersAddress       = "orders_address"
	OrdersComment       = "orders_comment"
	OrdersMoyskladID    = "orders_moysklad_id"
	OrdersCreatedAt     = "orders_created_at"
	OrdersUpdatedAt     = "orders_updated_at"
	OrdersReservedUntil = "orders_reserved_until"
	OrdersPickupAt      = "orders_pickup_at"
)

const (
	OrderItemsID        = "order_items_id_pk"
	OrderItemsOrderID   = "order_items_order_id_fk"
	OrderItemsProductID = "order_items_product_id_fk"
	OrderItemsQuantity  = "order_items_quantity"
	OrderItemsPrice     = "order_items_price"
)

type Order struct {
	ID            int64       `db:"orders_id_pk" json:"id"`
	UserID        *int64      `db:"orders_user_id_fk" insert:"orders_user_id_fk" update:"orders_user_id_fk" json:"user_id"`
	Status        string      `db:"orders_status" insert:"orders_status" update:"orders_status" json:"status"`
	TotalPrice    float64     `db:"orders_total_price" insert:"orders_total_price" update:"orders_total_price" json:"total_price"`
	CustomerName  string      `db:"orders_customer_name" insert:"orders_customer_name" update:"orders_customer_name" json:"customer_name"`
	Phone         string      `db:"orders_phone" insert:"orders_phone" update:"orders_phone" json:"phone"`
	Address       *string     `db:"orders_address" insert:"orders_address" update:"orders_address" json:"address"`
	Comment       *string     `db:"orders_comment" insert:"orders_comment" update:"orders_comment" json:"comment"`
	MoyskladID    *string     `db:"orders_moysklad_id" insert:"orders_moysklad_id" update:"orders_moysklad_id" json:"moysklad_id"`
	CreatedAt     time.Time   `db:"orders_created_at" json:"created_at"`
	UpdatedAt     time.Time   `db:"orders_updated_at" update:"orders_updated_at" json:"updated_at"`
	ReservedUntil *time.Time  `db:"orders_reserved_until" insert:"orders_reserved_until" update:"orders_reserved_until" json:"reserved_until"`
	PickupAt      *time.Time  `db:"orders_pickup_at" insert:"orders_pickup_at" update:"orders_pickup_at" json:"pickup_at"`
	Items         []OrderItem `db:"-" json:"items,omitempty"`
}

type OrderItem struct {
	ID          int64   `db:"order_items_id_pk" json:"id"`
	OrderID     int64   `db:"order_items_order_id_fk" insert:"order_items_order_id_fk" json:"order_id"`
	ProductID   int64   `db:"order_items_product_id_fk" insert:"order_items_product_id_fk" json:"product_id"`
	Quantity    int     `db:"order_items_quantity" insert:"order_items_quantity" json:"quantity"`
	Price       float64 `db:"order_items_price" insert:"order_items_price" json:"price"`
	ProductName string  `db:"-" json:"product_name"`
}

var (
	stomOrderSelect     = stom.MustNewStom(Order{}).SetTag(selectTag)
	stomOrderInsert     = stom.MustNewStom(Order{}).SetTag(insertTag)
	stomOrderUpdate     = stom.MustNewStom(Order{}).SetTag(updateTag)
	stomOrderItemInsert = stom.MustNewStom(OrderItem{}).SetTag(insertTag)
)

func (o *Order) columns(pref string) []string {
	return colNamesWithPref(stomOrderSelect.TagValues(), pref)
}

type OrderQuery interface {
	GetByID(ctx context.Context, id int64) (*Order, error)
	GetByUserID(ctx context.Context, userID int64) ([]*Order, error)
	GetAll(ctx context.Context, limit, offset int) ([]*Order, error)
	Insert(ctx context.Context, order *Order) (*Order, error)
	Update(ctx context.Context, order *Order, id int64) (*Order, error)
	InsertWithItems(ctx context.Context, order *Order, items []OrderItem) (*Order, error)
	// ListExpiredPendingIDs возвращает id pending-заказов, у которых reserved_until < now (TTL истёк).
	// Используется cron-задачей для автоматической отмены брони.
	ListExpiredPendingIDs(ctx context.Context, now time.Time, limit int) ([]int64, error)
	// CancelPendingOrderAtomic атомарно отменяет pending-заказ: переводит его в targetStatus
	// (например, 'expired' для cron или 'cancelled' для ручной отмены пользователем),
	// и возвращает товары на склад в той же транзакции.
	// Возвращает moysklad_id (если был) — чтобы вызывающий мог удалить CustomerOrder в МойСклад.
	// alreadyHandled = true, если заказ уже не pending (другой воркер обработал / уже отменён).
	// ownerUserID != nil — дополнительно проверяем что заказ принадлежит этому юзеру (для cancel юзером).
	CancelPendingOrderAtomic(ctx context.Context, orderID int64, targetStatus string, ownerUserID *int64) (moyskladID *string, alreadyHandled bool, restored []*Product, err error)
	// CompleteOrder помечает pending-заказ как completed. Сток не возвращается
	// (товар выкуплен в магазине). Резерв в МойСклад продавец снимает сам отгрузкой.
	CompleteOrder(ctx context.Context, orderID int64) (alreadyHandled bool, err error)
	// CreateOrderAtomic выполняет в одной транзакции:
	//  1. UPDATE products SET stock = stock - qty WHERE id = ? AND stock >= qty (для каждого item)
	//  2. INSERT INTO orders
	//  3. INSERT INTO order_items
	//  4. DELETE FROM cart_items WHERE user_id = ?
	// Если у любого товара недостаточно стока — возвращает ErrInsufficientStock и транзакция откатывается.
	// updatedProducts содержит новые остатки товаров после списания (для обновления кэша после коммита).
	CreateOrderAtomic(ctx context.Context, order *Order, items []OrderItem, clearCartForUserID int64) (createdOrder *Order, updatedProducts []*Product, err error)

	// --- Админка ---
	// ListOrders — список заказов с фильтрами + общий count для пагинации.
	ListOrders(ctx context.Context, f OrderListFilter) ([]*Order, int, error)
	// StatsByStatus — счётчики заказов по статусам за период (для дашборда).
	StatsByStatus(ctx context.Context, from, to *time.Time) (map[string]int, error)
	// GetUserStats — агрегаты по конкретному клиенту.
	GetUserStats(ctx context.Context, userID int64) (*UserOrderStats, error)
	// GetUserTopProducts — топ-N товаров клиента по completed-заказам.
	GetUserTopProducts(ctx context.Context, userID int64, limit int) ([]*UserTopProduct, error)
	// GetItemsByOrderID — позиции заказа (для карточки).
	GetItemsByOrderID(ctx context.Context, orderID int64) ([]*OrderItem, error)
}

type orderQuery struct {
	runner *pgxpool.Pool
	sq     squirrel.StatementBuilderType
	logger *zap.Logger
}

func NewOrderQuery(runner *pgxpool.Pool, sq squirrel.StatementBuilderType, logger *zap.Logger) OrderQuery {
	return &orderQuery{
		runner: runner,
		sq:     sq,
		logger: logger,
	}
}

func (o *orderQuery) GetByID(ctx context.Context, id int64) (*Order, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	order := &Order{}
	qb, args, err := o.sq.Select(order.columns("")...).
		From(OrdersTable).
		Where(squirrel.Eq{OrdersID: id}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	err = pgxscan.Get(ctx, o.runner, order, qb, args...)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	// Загружаем items
	items, err := o.getOrderItems(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get order items: %w", err)
	}
	order.Items = items

	return order, nil
}

func (o *orderQuery) getOrderItems(ctx context.Context, orderID int64) ([]OrderItem, error) {
	var items []OrderItem
	qb, args, err := o.sq.Select(
		OrderItemsID,
		OrderItemsOrderID,
		OrderItemsProductID,
		OrderItemsQuantity,
		OrderItemsPrice,
	).
		From(OrderItemsTable).
		Where(squirrel.Eq{OrderItemsOrderID: orderID}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	err = pgxscan.Select(ctx, o.runner, &items, qb, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	return items, nil
}

func (o *orderQuery) GetByUserID(ctx context.Context, userID int64) ([]*Order, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var orders []*Order
	order := &Order{}
	qb, args, err := o.sq.Select(order.columns("")...).
		From(OrdersTable).
		Where(squirrel.Eq{OrdersUserID: userID}).
		OrderBy(OrdersCreatedAt + " DESC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	err = pgxscan.Select(ctx, o.runner, &orders, qb, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	return orders, nil
}

func (o *orderQuery) GetAll(ctx context.Context, limit, offset int) ([]*Order, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var orders []*Order
	order := &Order{}
	qb, args, err := o.sq.Select(order.columns("")...).
		From(OrdersTable).
		OrderBy(OrdersCreatedAt + " DESC").
		Limit(uint64(limit)).
		Offset(uint64(offset)).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	err = pgxscan.Select(ctx, o.runner, &orders, qb, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	return orders, nil
}

func (o *orderQuery) Insert(ctx context.Context, order *Order) (*Order, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	insertMap, err := stomOrderInsert.ToMap(order)
	if err != nil {
		return nil, fmt.Errorf("failed to map struct: %w", err)
	}
	qb, args, err := o.sq.Insert(OrdersTable).
		SetMap(insertMap).
		Suffix("RETURNING *").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}
	err = pgxscan.Get(ctx, o.runner, order, qb, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	o.logger.Info("Order inserted successfully", zap.Int64("order_id", order.ID))
	return order, nil
}

func (o *orderQuery) InsertWithItems(ctx context.Context, order *Order, items []OrderItem) (*Order, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	tx, err := o.runner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Вставляем заказ
	insertMap, err := stomOrderInsert.ToMap(order)
	if err != nil {
		return nil, fmt.Errorf("failed to map struct: %w", err)
	}
	qb, args, err := o.sq.Insert(OrdersTable).
		SetMap(insertMap).
		Suffix("RETURNING *").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}
	err = pgxscan.Get(ctx, tx, order, qb, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	// Вставляем items
	for i := range items {
		items[i].OrderID = order.ID
		itemMap, err := stomOrderItemInsert.ToMap(&items[i])
		if err != nil {
			return nil, fmt.Errorf("failed to map item: %w", err)
		}
		itemQb, itemArgs, err := o.sq.Insert(OrderItemsTable).
			SetMap(itemMap).
			ToSql()
		if err != nil {
			return nil, fmt.Errorf("failed to build item query: %w", err)
		}
		_, err = tx.Exec(ctx, itemQb, itemArgs...)
		if err != nil {
			return nil, fmt.Errorf("failed to insert item: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	o.logger.Info("Order with items inserted successfully", zap.Int64("order_id", order.ID))
	order.Items = items
	return order, nil
}

func (o *orderQuery) CreateOrderAtomic(ctx context.Context, order *Order, items []OrderItem, clearCartForUserID int64) (*Order, []*Product, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	tx, err := o.runner.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Атомарно списываем сток у каждого товара. Если у кого-то stock < quantity —
	// UPDATE вернёт 0 строк и мы откатываем транзакцию с ErrInsufficientStock.
	updatedProducts := make([]*Product, 0, len(items))
	for _, item := range items {
		updateSQL := `
			UPDATE ` + ProductsTable + `
			SET ` + ProductsStock + ` = ` + ProductsStock + ` - $1,
			    ` + ProductsStatus + ` = CASE WHEN ` + ProductsStock + ` - $1 > 0 THEN 'active' ELSE 'out_of_stock' END,
			    ` + ProductsUpdatedAt + ` = NOW()
			WHERE ` + ProductsID + ` = $2
			  AND ` + ProductsStock + ` >= $1
			  AND ` + ProductsActive + ` = TRUE
			RETURNING *
		`
		var updatedProduct Product
		err := pgxscan.Get(ctx, tx, &updatedProduct, updateSQL, item.Quantity, item.ProductID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// Либо товара нет, либо стока недостаточно, либо товар неактивен.
				o.logger.Warn("Order atomic decrement failed",
					zap.Int64("product_id", item.ProductID),
					zap.Int("quantity", item.Quantity))
				return nil, nil, fmt.Errorf("%w: product %d", ErrInsufficientStock, item.ProductID)
			}
			return nil, nil, fmt.Errorf("failed to decrement product stock %d: %w", item.ProductID, err)
		}
		updatedProducts = append(updatedProducts, &updatedProduct)
	}

	// 2. Вставляем заказ
	insertMap, err := stomOrderInsert.ToMap(order)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to map order: %w", err)
	}
	qb, args, err := o.sq.Insert(OrdersTable).
		SetMap(insertMap).
		Suffix("RETURNING *").
		ToSql()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build order insert query: %w", err)
	}
	if err := pgxscan.Get(ctx, tx, order, qb, args...); err != nil {
		return nil, nil, fmt.Errorf("failed to insert order: %w", err)
	}

	// 3. Вставляем order_items
	for i := range items {
		items[i].OrderID = order.ID
		itemMap, err := stomOrderItemInsert.ToMap(&items[i])
		if err != nil {
			return nil, nil, fmt.Errorf("failed to map order item: %w", err)
		}
		itemQb, itemArgs, err := o.sq.Insert(OrderItemsTable).
			SetMap(itemMap).
			ToSql()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to build order item query: %w", err)
		}
		if _, err := tx.Exec(ctx, itemQb, itemArgs...); err != nil {
			return nil, nil, fmt.Errorf("failed to insert order item: %w", err)
		}
	}

	// 4. Чистим корзину пользователя (только указанные товары, чтобы не удалить
	// что-то, что пользователь добавил параллельно после старта оформления).
	if clearCartForUserID != 0 && len(items) > 0 {
		productIDs := make([]int64, 0, len(items))
		for _, it := range items {
			productIDs = append(productIDs, it.ProductID)
		}
		clearQb, clearArgs, err := o.sq.Delete(CartItemsTable).
			Where(squirrel.Eq{CartItemsUserID: clearCartForUserID}).
			Where(squirrel.Eq{CartItemsProductID: productIDs}).
			ToSql()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to build cart cleanup query: %w", err)
		}
		if _, err := tx.Exec(ctx, clearQb, clearArgs...); err != nil {
			return nil, nil, fmt.Errorf("failed to clear cart items: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	order.Items = items
	o.logger.Info("Order created atomically",
		zap.Int64("order_id", order.ID),
		zap.Int("items_count", len(items)))
	return order, updatedProducts, nil
}

func (o *orderQuery) ListExpiredPendingIDs(ctx context.Context, now time.Time, limit int) ([]int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if limit <= 0 {
		limit = 100
	}

	sqlText := `
		SELECT ` + OrdersID + `
		FROM ` + OrdersTable + `
		WHERE ` + OrdersStatus + ` = 'pending'
		  AND ` + OrdersReservedUntil + ` IS NOT NULL
		  AND ` + OrdersReservedUntil + ` < $1
		ORDER BY ` + OrdersReservedUntil + ` ASC
		LIMIT $2
	`
	rows, err := o.runner.Query(ctx, sqlText, now, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list expired orders: %w", err)
	}
	defer rows.Close()

	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan expired order id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// allowedCancelStatuses — куда можно перевести pending-заказ.
var allowedCancelStatuses = map[string]struct{}{
	"expired":   {},
	"cancelled": {},
}

func (o *orderQuery) CancelPendingOrderAtomic(ctx context.Context, orderID int64, targetStatus string, ownerUserID *int64) (*string, bool, []*Product, error) {
	if _, ok := allowedCancelStatuses[targetStatus]; !ok {
		return nil, false, nil, fmt.Errorf("invalid target status %q (expected 'expired' or 'cancelled')", targetStatus)
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	tx, err := o.runner.Begin(ctx)
	if err != nil {
		return nil, false, nil, fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Захватываем заказ с FOR UPDATE SKIP LOCKED, чтобы параллельные воркеры не брали одно и то же.
	var order Order
	lockSQL := `
		SELECT ` + OrdersID + `, ` + OrdersUserID + `, ` + OrdersStatus + `, ` + OrdersMoyskladID + `
		FROM ` + OrdersTable + `
		WHERE ` + OrdersID + ` = $1
		FOR UPDATE SKIP LOCKED
	`
	row := tx.QueryRow(ctx, lockSQL, orderID)
	if err := row.Scan(&order.ID, &order.UserID, &order.Status, &order.MoyskladID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Либо заказа нет, либо его уже кто-то залочил — пропустим.
			return nil, true, nil, nil
		}
		return nil, false, nil, fmt.Errorf("failed to lock order: %w", err)
	}
	if order.Status != "pending" {
		return nil, true, nil, nil
	}
	// Проверка владельца — для cancel от пользователя.
	if ownerUserID != nil {
		if order.UserID == nil || *order.UserID != *ownerUserID {
			return nil, false, nil, ErrOrderForbidden
		}
	}

	// Возвращаем сток по каждой позиции и собираем итоговые остатки для обновления кэша.
	restoreSQL := `
		UPDATE ` + ProductsTable + ` p
		SET ` + ProductsStock + ` = p.` + ProductsStock + ` + oi.` + OrderItemsQuantity + `,
		    ` + ProductsStatus + ` = CASE WHEN p.` + ProductsStock + ` + oi.` + OrderItemsQuantity + ` > 0 THEN 'active' ELSE 'out_of_stock' END,
		    ` + ProductsUpdatedAt + ` = NOW()
		FROM ` + OrderItemsTable + ` oi
		WHERE oi.` + OrderItemsOrderID + ` = $1
		  AND p.` + ProductsID + ` = oi.` + OrderItemsProductID + `
		RETURNING p.*
	`
	rows, err := tx.Query(ctx, restoreSQL, orderID)
	if err != nil {
		return nil, false, nil, fmt.Errorf("failed to restore stock: %w", err)
	}
	restored := make([]*Product, 0)
	if err := pgxscan.ScanAll(&restored, rows); err != nil {
		return nil, false, nil, fmt.Errorf("failed to scan restored products: %w", err)
	}

	updateSQL := `
		UPDATE ` + OrdersTable + `
		SET ` + OrdersStatus + ` = $2, ` + OrdersUpdatedAt + ` = NOW()
		WHERE ` + OrdersID + ` = $1
	`
	if _, err := tx.Exec(ctx, updateSQL, orderID, targetStatus); err != nil {
		return nil, false, nil, fmt.Errorf("failed to mark order %s: %w", targetStatus, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, false, nil, fmt.Errorf("failed to commit cancel tx: %w", err)
	}

	o.logger.Info("Order cancelled and stock restored",
		zap.Int64("order_id", orderID),
		zap.String("target_status", targetStatus),
		zap.Int("products_restored", len(restored)))
	return order.MoyskladID, false, restored, nil
}

func (o *orderQuery) CompleteOrder(ctx context.Context, orderID int64) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Меняем статус только если заказ ещё pending.
	tag, err := o.runner.Exec(ctx, `
		UPDATE `+OrdersTable+`
		SET `+OrdersStatus+` = 'completed', `+OrdersUpdatedAt+` = NOW()
		WHERE `+OrdersID+` = $1 AND `+OrdersStatus+` = 'pending'
	`, orderID)
	if err != nil {
		return false, fmt.Errorf("failed to complete order: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return true, nil // уже не pending
	}
	o.logger.Info("Order marked completed", zap.Int64("order_id", orderID))
	return false, nil
}

func (o *orderQuery) Update(ctx context.Context, order *Order, id int64) (*Order, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	updateMap, err := stomOrderUpdate.ToMap(order)
	if err != nil {
		return nil, fmt.Errorf("failed to map struct: %w", err)
	}
	qb, args, err := o.sq.Update(OrdersTable).
		SetMap(updateMap).
		Where(squirrel.Eq{OrdersID: id}).
		Suffix("RETURNING *").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}
	err = pgxscan.Get(ctx, o.runner, order, qb, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	o.logger.Info("Order updated successfully", zap.Int64("order_id", order.ID))
	return order, nil
}
