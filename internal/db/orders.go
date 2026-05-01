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

const (
	OrdersID           = "orders_id_pk"
	OrdersUserID       = "orders_user_id_fk"
	OrdersStatus       = "orders_status"
	OrdersTotalPrice   = "orders_total_price"
	OrdersCustomerName = "orders_customer_name"
	OrdersPhone        = "orders_phone"
	OrdersAddress      = "orders_address"
	OrdersComment      = "orders_comment"
	OrdersMoyskladID   = "orders_moysklad_id"
	OrdersCreatedAt    = "orders_created_at"
	OrdersUpdatedAt    = "orders_updated_at"
)

const (
	OrderItemsID        = "order_items_id_pk"
	OrderItemsOrderID   = "order_items_order_id_fk"
	OrderItemsProductID = "order_items_product_id_fk"
	OrderItemsQuantity  = "order_items_quantity"
	OrderItemsPrice     = "order_items_price"
)

type Order struct {
	ID           int64       `db:"orders_id_pk"`
	UserID       *int64      `db:"orders_user_id_fk" insert:"orders_user_id_fk" update:"orders_user_id_fk"`
	Status       string      `db:"orders_status" insert:"orders_status" update:"orders_status"`
	TotalPrice   float64     `db:"orders_total_price" insert:"orders_total_price" update:"orders_total_price"`
	CustomerName string      `db:"orders_customer_name" insert:"orders_customer_name" update:"orders_customer_name"`
	Phone        string      `db:"orders_phone" insert:"orders_phone" update:"orders_phone"`
	Address      *string     `db:"orders_address" insert:"orders_address" update:"orders_address"`
	Comment      *string     `db:"orders_comment" insert:"orders_comment" update:"orders_comment"`
	MoyskladID   *string     `db:"orders_moysklad_id" insert:"orders_moysklad_id" update:"orders_moysklad_id"`
	CreatedAt    time.Time   `db:"orders_created_at"`
	UpdatedAt    time.Time   `db:"orders_updated_at" update:"orders_updated_at"`
	Items        []OrderItem `db:"-"`
}

type OrderItem struct {
	ID        int64   `db:"order_items_id_pk"`
	OrderID   int64   `db:"order_items_order_id_fk" insert:"order_items_order_id_fk"`
	ProductID int64   `db:"order_items_product_id_fk" insert:"order_items_product_id_fk"`
	Quantity  int     `db:"order_items_quantity" insert:"order_items_quantity"`
	Price     float64 `db:"order_items_price" insert:"order_items_price"`
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
