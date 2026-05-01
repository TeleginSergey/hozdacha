package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

const CartItemsTable = "cart_items"

const (
	CartItemsID        = "cart_items_id_pk"
	CartItemsUserID    = "cart_items_user_id_fk"
	CartItemsProductID = "cart_items_product_id_fk"
	CartItemsQuantity  = "cart_items_quantity"
	CartItemsCreatedAt = "cart_items_created_at"
	CartItemsUpdatedAt = "cart_items_updated_at"
)

type CartItem struct {
	ID        int64     `db:"cart_items_id_pk"`
	UserID    int64     `db:"cart_items_user_id_fk" insert:"cart_items_user_id_fk" update:"cart_items_user_id_fk"`
	ProductID int64     `db:"cart_items_product_id_fk" insert:"cart_items_product_id_fk" update:"cart_items_product_id_fk"`
	Quantity  int       `db:"cart_items_quantity" insert:"cart_items_quantity" update:"cart_items_quantity"`
	CreatedAt time.Time `db:"cart_items_created_at"`
	UpdatedAt time.Time `db:"cart_items_updated_at" update:"cart_items_updated_at"`

	// Joined fields for API responses
	Product *Product `db:"product,omitempty"`
}

type CartItemQuery interface {
	Create(ctx context.Context, item *CartItem) error
	GetByUserID(ctx context.Context, userID int64) ([]*CartItem, error)
	GetByUserIDAndProductID(ctx context.Context, userID, productID int64) (*CartItem, error)
	UpdateQuantity(ctx context.Context, userID, productID int64, quantity int) error
	Delete(ctx context.Context, userID, productID int64) error
	Clear(ctx context.Context, userID int64) error
	GetTotal(ctx context.Context, userID int64) (float64, error)
}

type cartItemQuery struct {
	db     *pgxpool.Pool
	sq     squirrel.StatementBuilderType
	logger *zap.Logger
}

func NewCartItemQuery(db *pgxpool.Pool, logger *zap.Logger) CartItemQuery {
	return &cartItemQuery{
		db:     db,
		sq:     squirrel.StatementBuilderType{}.PlaceholderFormat(squirrel.Dollar),
		logger: logger,
	}
}

func (c *cartItemQuery) Create(ctx context.Context, item *CartItem) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	now := time.Now()
	item.CreatedAt = now
	item.UpdatedAt = now

	qb, args, err := c.sq.Insert(CartItemsTable).
		Columns(CartItemsUserID, CartItemsProductID, CartItemsQuantity, CartItemsCreatedAt, CartItemsUpdatedAt).
		Values(item.UserID, item.ProductID, item.Quantity, item.CreatedAt, item.UpdatedAt).
		Suffix("RETURNING *").
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build query: %w", err)
	}

	err = pgxscan.Get(ctx, c.db, item, qb, args...)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	return nil
}

func (c *cartItemQuery) GetByUserID(ctx context.Context, userID int64) ([]*CartItem, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var items []*CartItem
	qb, args, err := c.sq.Select(
		CartItemsID,
		CartItemsUserID,
		CartItemsProductID,
		CartItemsQuantity,
		CartItemsCreatedAt,
		CartItemsUpdatedAt,
	).
		From(CartItemsTable).
		Where(squirrel.Eq{CartItemsUserID: userID}).
		OrderBy(CartItemsCreatedAt + " DESC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	err = pgxscan.Select(ctx, c.db, &items, qb, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	return items, nil
}

func (c *cartItemQuery) GetByUserIDAndProductID(ctx context.Context, userID, productID int64) (*CartItem, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var item CartItem
	qb, args, err := c.sq.Select(
		CartItemsID,
		CartItemsUserID,
		CartItemsProductID,
		CartItemsQuantity,
		CartItemsCreatedAt,
		CartItemsUpdatedAt,
	).
		From(CartItemsTable).
		Where(squirrel.And{
			squirrel.Eq{CartItemsUserID: userID},
			squirrel.Eq{CartItemsProductID: productID},
		}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	err = pgxscan.Get(ctx, c.db, &item, qb, args...)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	return &item, nil
}

func (c *cartItemQuery) UpdateQuantity(ctx context.Context, userID, productID int64, quantity int) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	qb, args, err := c.sq.Update(CartItemsTable).
		Set(CartItemsQuantity, quantity).
		Set(CartItemsUpdatedAt, time.Now()).
		Where(squirrel.And{
			squirrel.Eq{CartItemsUserID: userID},
			squirrel.Eq{CartItemsProductID: productID},
		}).
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build query: %w", err)
	}

	_, err = c.db.Exec(ctx, qb, args...)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	return nil
}

func (c *cartItemQuery) Delete(ctx context.Context, userID, productID int64) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	qb, args, err := c.sq.Delete(CartItemsTable).
		Where(squirrel.And{
			squirrel.Eq{CartItemsUserID: userID},
			squirrel.Eq{CartItemsProductID: productID},
		}).
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build query: %w", err)
	}

	_, err = c.db.Exec(ctx, qb, args...)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	return nil
}

func (c *cartItemQuery) Clear(ctx context.Context, userID int64) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	qb, args, err := c.sq.Delete(CartItemsTable).
		Where(squirrel.Eq{CartItemsUserID: userID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build query: %w", err)
	}

	_, err = c.db.Exec(ctx, qb, args...)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	return nil
}

func (c *cartItemQuery) GetTotal(ctx context.Context, userID int64) (float64, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	qb, args, err := c.sq.Select(
		"SUM(" + CartItemsQuantity + " * " + ProductsTable + "." + ProductsPrice + ")",
	).
		From(CartItemsTable).
		Join(ProductsTable + " ON " + CartItemsProductID + " = " + ProductsID).
		Where(squirrel.Eq{CartItemsUserID: userID}).
		ToSql()
	if err != nil {
		return 0, fmt.Errorf("failed to build query: %w", err)
	}

	var total float64
	err = c.db.QueryRow(ctx, qb, args...).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("failed to execute query: %w", err)
	}

	return total, nil
}
