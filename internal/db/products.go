package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/elgris/stom"
	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

const ProductsTable = "products"

const (
	ProductsID              = "products_id_pk"
	ProductsName            = "products_name"
	ProductsDescription     = "products_description"
	ProductsPrice           = "products_price"
	ProductsImageURL        = "products_image_url"
	ProductsMoyskladID      = "products_moysklad_id"
	ProductsCategoryID      = "products_category_id_fk"
	ProductsStock           = "products_stock"
	ProductsActive          = "products_active"
	ProductsCreatedAt       = "products_created_at"
	ProductsUpdatedAt       = "products_updated_at"
	ProductsStatus          = "products_status"
	ProductsLastSyncUpdated = "products_last_sync_updated"
)

type Product struct {
	ID              int64      `db:"products_id_pk"`
	Name            string     `db:"products_name" insert:"products_name" update:"products_name"`
	Description     *string    `db:"products_description" insert:"products_description" update:"products_description"`
	Price           float64    `db:"products_price" insert:"products_price" update:"products_price"`
	ImageURL        *string    `db:"products_image_url" insert:"products_image_url" update:"products_image_url"`
	MoyskladID      *string    `db:"products_moysklad_id" insert:"products_moysklad_id" update:"products_moysklad_id"`
	CategoryID      *int64     `db:"products_category_id_fk" insert:"products_category_id_fk" update:"products_category_id_fk"`
	Stock           int        `db:"products_stock" insert:"products_stock" update:"products_stock"`
	Active          bool       `db:"products_active" insert:"products_active" update:"products_active"`
	Status          string     `db:"products_status" insert:"products_status" update:"products_status"`
	LastSyncUpdated *time.Time `db:"products_last_sync_updated" insert:"products_last_sync_updated" update:"products_last_sync_updated"`
	CreatedAt       time.Time  `db:"products_created_at"`
	UpdatedAt       time.Time  `db:"products_updated_at" update:"products_updated_at"`
}

var (
	stomProductSelect = stom.MustNewStom(Product{}).SetTag(selectTag)
	stomProductInsert = stom.MustNewStom(Product{}).SetTag(insertTag)
	stomProductUpdate = stom.MustNewStom(Product{}).SetTag(updateTag)
)

func (p *Product) columns(pref string) []string {
	return colNamesWithPref(stomProductSelect.TagValues(), pref)
}

type ProductQuery interface {
	GetByID(ctx context.Context, id int64) (*Product, error)
	GetByIDs(ctx context.Context, ids []int64) ([]*Product, error)
	GetByMoyskladID(ctx context.Context, moyskladID string) (*Product, error)
	GetAll(ctx context.Context, limit, offset int) ([]*Product, error)
	GetActive(ctx context.Context, limit, offset int) ([]*Product, error)
	Search(ctx context.Context, query string, categoryID *int64, limit, offset int) ([]*Product, error)
	GetByCategory(ctx context.Context, categoryID int64, limit, offset int) ([]*Product, error)
	CountActive(ctx context.Context) (int, error)
	CountByCategory(ctx context.Context, categoryID int64) (int, error)
	CountSearch(ctx context.Context, query string, categoryID *int64) (int, error)
	Insert(ctx context.Context, product *Product) (*Product, error)
	Update(ctx context.Context, product *Product, id int64) (*Product, error)
	Delete(ctx context.Context, id int64) error
}

type productQuery struct {
	runner *pgxpool.Pool
	sq     squirrel.StatementBuilderType
	logger *zap.Logger
}

func NewProductQuery(runner *pgxpool.Pool, sq squirrel.StatementBuilderType, logger *zap.Logger) ProductQuery {
	return &productQuery{
		runner: runner,
		sq:     sq,
		logger: logger,
	}
}

func (p *productQuery) GetByID(ctx context.Context, id int64) (*Product, error) {
	p.logger.Debug("Fetching product by ID", zap.Int64("product_id", id))
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	product := &Product{}
	qb, args, err := p.sq.Select(product.columns("")...).
		From(ProductsTable).
		Where(squirrel.Eq{ProductsID: id}).
		ToSql()
	if err != nil {
		p.logger.Error("Failed to build query", zap.Error(err))
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	err = pgxscan.Get(ctx, p.runner, product, qb, args...)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			p.logger.Warn("Database error",
				zap.Int64("product_id", id),
				zap.String("pg_error_code", pgErr.Code),
				zap.Error(err),
			)
		} else {
			p.logger.Warn("Failed to fetch product", zap.Int64("product_id", id), zap.Error(err))
		}
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	return product, nil
}

// GetByIDs возвращает товары по списку ID (один SQL-запрос с IN).
func (p *productQuery) GetByIDs(ctx context.Context, ids []int64) ([]*Product, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	products := make([]*Product, 0, len(ids))
	qb, args, err := p.sq.Select((&Product{}).columns("")...).
		From(ProductsTable).
		Where(squirrel.Eq{ProductsID: ids}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build GetByIDs: %w", err)
	}
	if err := pgxscan.Select(ctx, p.runner, &products, qb, args...); err != nil {
		return nil, fmt.Errorf("GetByIDs: %w", err)
	}
	return products, nil
}

func (p *productQuery) GetByMoyskladID(ctx context.Context, moyskladID string) (*Product, error) {
	p.logger.Debug("Fetching product by Moysklad ID", zap.String("moysklad_id", moyskladID))
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	product := &Product{}
	qb, args, err := p.sq.Select(product.columns("")...).
		From(ProductsTable).
		Where(squirrel.Eq{ProductsMoyskladID: moyskladID}).
		ToSql()
	if err != nil {
		p.logger.Error("Failed to build query", zap.Error(err))
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	err = pgxscan.Get(ctx, p.runner, product, qb, args...)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	return product, nil
}

func (p *productQuery) GetAll(ctx context.Context, limit, offset int) ([]*Product, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var products []*Product
	product := &Product{}
	qb, args, err := p.sq.Select(product.columns("")...).
		From(ProductsTable).
		OrderBy(ProductsCreatedAt + " DESC").
		Limit(uint64(limit)).
		Offset(uint64(offset)).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	err = pgxscan.Select(ctx, p.runner, &products, qb, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	return products, nil
}

func (p *productQuery) GetActive(ctx context.Context, limit, offset int) ([]*Product, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var products []*Product
	product := &Product{}
	qb, args, err := p.sq.Select(product.columns("")...).
		From(ProductsTable).
		Where(squirrel.And{
			squirrel.Eq{ProductsActive: true},
			// Показываем в каталоге только товары со статусом active и положительным остатком
			squirrel.Eq{ProductsStatus: "active"},
			squirrel.Gt{ProductsStock: 0},
		}).
		OrderBy(ProductsCreatedAt + " DESC").
		Limit(uint64(limit)).
		Offset(uint64(offset)).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	err = pgxscan.Select(ctx, p.runner, &products, qb, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	return products, nil
}

func (p *productQuery) Search(ctx context.Context, query string, categoryID *int64, limit, offset int) ([]*Product, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var products []*Product
	product := &Product{}
	searchPattern := "%" + query + "%"

	if categoryID != nil {
		// Поиск внутри категории + подкатегорий (рекурсивный CTE).
		cols := strings.Join(product.columns(""), ", ")
		sql := `WITH RECURSIVE category_tree AS (
			SELECT ` + CategoriesID + ` AS id FROM ` + CategoriesTable + ` WHERE ` + CategoriesID + ` = $1
			UNION ALL
			SELECT c.` + CategoriesID + ` FROM ` + CategoriesTable + ` c
			JOIN category_tree ct ON c.` + CategoriesParentID + ` = ct.id
		)
		SELECT ` + cols + ` FROM ` + ProductsTable + `
		WHERE ` + ProductsActive + ` = true
		  AND ` + ProductsStatus + ` = 'active'
		  AND ` + ProductsStock + ` > 0
		  AND ` + ProductsName + ` ILIKE $2
		  AND ` + ProductsCategoryID + ` IN (SELECT id FROM category_tree)
		ORDER BY ` + ProductsCreatedAt + ` DESC
		LIMIT $3 OFFSET $4`
		if err := pgxscan.Select(ctx, p.runner, &products, sql, *categoryID, searchPattern, limit, offset); err != nil {
			return nil, fmt.Errorf("failed to execute search by category: %w", err)
		}
		return products, nil
	}

	qb, args, err := p.sq.Select(product.columns("")...).
		From(ProductsTable).
		Where(squirrel.And{
			squirrel.Eq{ProductsActive: true},
			squirrel.Eq{ProductsStatus: "active"},
			squirrel.Gt{ProductsStock: 0},
			squirrel.ILike{ProductsName: searchPattern},
		}).
		OrderBy(ProductsCreatedAt + " DESC").
		Limit(uint64(limit)).
		Offset(uint64(offset)).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	err = pgxscan.Select(ctx, p.runner, &products, qb, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	return products, nil
}

// GetByCategory — выборка товаров из категории И всех её подкатегорий (рекурсивный CTE).
// Это позволяет показывать все товары при клике на главную категорию (и из подкатегорий тоже).
func (p *productQuery) GetByCategory(ctx context.Context, categoryID int64, limit, offset int) ([]*Product, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	product := &Product{}
	cols := strings.Join(product.columns(""), ", ")
	sql := `WITH RECURSIVE category_tree AS (
		SELECT ` + CategoriesID + ` AS id FROM ` + CategoriesTable + ` WHERE ` + CategoriesID + ` = $1
		UNION ALL
		SELECT c.` + CategoriesID + ` FROM ` + CategoriesTable + ` c
		JOIN category_tree ct ON c.` + CategoriesParentID + ` = ct.id
	)
	SELECT ` + cols + ` FROM ` + ProductsTable + `
	WHERE ` + ProductsActive + ` = true
	  AND ` + ProductsStatus + ` = 'active'
	  AND ` + ProductsStock + ` > 0
	  AND ` + ProductsCategoryID + ` IN (SELECT id FROM category_tree)
	ORDER BY ` + ProductsCreatedAt + ` DESC
	LIMIT $2 OFFSET $3`

	var products []*Product
	if err := pgxscan.Select(ctx, p.runner, &products, sql, categoryID, limit, offset); err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	return products, nil
}

func (p *productQuery) Insert(ctx context.Context, product *Product) (*Product, error) {
	p.logger.Debug("Inserting product", zap.String("name", product.Name))
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Устанавливаем временные метки если не установлены
	now := time.Now()
	if product.CreatedAt.IsZero() {
		product.CreatedAt = now
	}
	if product.UpdatedAt.IsZero() {
		product.UpdatedAt = now
	}

	insertMap, err := stomProductInsert.ToMap(product)
	if err != nil {
		return nil, fmt.Errorf("failed to map struct: %w", err)
	}
	qb, args, err := p.sq.Insert(ProductsTable).
		SetMap(insertMap).
		Suffix("RETURNING *").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}
	err = pgxscan.Get(ctx, p.runner, product, qb, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	p.logger.Info("Product inserted successfully", zap.Int64("product_id", product.ID))
	return product, nil
}

func (p *productQuery) Update(ctx context.Context, product *Product, id int64) (*Product, error) {
	p.logger.Debug("Updating product", zap.Int64("product_id", id))
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Всегда устанавливаем UpdatedAt при обновлении
	product.UpdatedAt = time.Now()

	updateMap, err := stomProductUpdate.ToMap(product)
	if err != nil {
		return nil, fmt.Errorf("failed to map struct: %w", err)
	}
	qb, args, err := p.sq.Update(ProductsTable).
		SetMap(updateMap).
		Where(squirrel.Eq{ProductsID: id}).
		Suffix("RETURNING *").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}
	err = pgxscan.Get(ctx, p.runner, product, qb, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	p.logger.Info("Product updated successfully", zap.Int64("product_id", product.ID))
	return product, nil
}

func (p *productQuery) Delete(ctx context.Context, id int64) error {
	p.logger.Debug("Deleting product", zap.Int64("product_id", id))
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	qb, args, err := p.sq.Delete(ProductsTable).
		Where(squirrel.Eq{ProductsID: id}).
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build query: %w", err)
	}

	result, err := p.runner.Exec(ctx, qb, args...)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("no product found with id %d", id)
	}

	p.logger.Info("Product deleted successfully", zap.Int64("product_id", id))
	return nil
}

// CountActive возвращает количество активных товаров
func (p *productQuery) CountActive(ctx context.Context) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var count int
	qb, args, err := p.sq.Select("COUNT(*)").
		From(ProductsTable).
		Where(squirrel.And{
			squirrel.Eq{ProductsActive: true},
			squirrel.Eq{ProductsStatus: "active"},
			squirrel.Gt{ProductsStock: 0},
		}).
		ToSql()
	if err != nil {
		return 0, fmt.Errorf("failed to build query: %w", err)
	}

	err = p.runner.QueryRow(ctx, qb, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to execute query: %w", err)
	}
	return count, nil
}

// CountByCategory возвращает количество товаров в категории и всех её подкатегориях.
func (p *productQuery) CountByCategory(ctx context.Context, categoryID int64) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	sql := `WITH RECURSIVE category_tree AS (
		SELECT ` + CategoriesID + ` AS id FROM ` + CategoriesTable + ` WHERE ` + CategoriesID + ` = $1
		UNION ALL
		SELECT c.` + CategoriesID + ` FROM ` + CategoriesTable + ` c
		JOIN category_tree ct ON c.` + CategoriesParentID + ` = ct.id
	)
	SELECT COUNT(*) FROM ` + ProductsTable + `
	WHERE ` + ProductsActive + ` = true
	  AND ` + ProductsStatus + ` = 'active'
	  AND ` + ProductsStock + ` > 0
	  AND ` + ProductsCategoryID + ` IN (SELECT id FROM category_tree)`

	var count int
	if err := p.runner.QueryRow(ctx, sql, categoryID).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to execute query: %w", err)
	}
	return count, nil
}

// CountSearch возвращает количество товаров по поисковому запросу
func (p *productQuery) CountSearch(ctx context.Context, query string, categoryID *int64) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var count int
	searchPattern := "%" + query + "%"

	if categoryID != nil {
		sql := `WITH RECURSIVE category_tree AS (
			SELECT ` + CategoriesID + ` AS id FROM ` + CategoriesTable + ` WHERE ` + CategoriesID + ` = $1
			UNION ALL
			SELECT c.` + CategoriesID + ` FROM ` + CategoriesTable + ` c
			JOIN category_tree ct ON c.` + CategoriesParentID + ` = ct.id
		)
		SELECT COUNT(*) FROM ` + ProductsTable + `
		WHERE ` + ProductsActive + ` = true
		  AND ` + ProductsStatus + ` = 'active'
		  AND ` + ProductsStock + ` > 0
		  AND ` + ProductsName + ` ILIKE $2
		  AND ` + ProductsCategoryID + ` IN (SELECT id FROM category_tree)`
		if err := p.runner.QueryRow(ctx, sql, *categoryID, searchPattern).Scan(&count); err != nil {
			return 0, fmt.Errorf("failed to execute search count by category: %w", err)
		}
		return count, nil
	}

	qb, args, err := p.sq.Select("COUNT(*)").
		From(ProductsTable).
		Where(squirrel.And{
			squirrel.Eq{ProductsActive: true},
			squirrel.Eq{ProductsStatus: "active"},
			squirrel.Gt{ProductsStock: 0},
			squirrel.ILike{ProductsName: searchPattern},
		}).
		ToSql()
	if err != nil {
		return 0, fmt.Errorf("failed to build query: %w", err)
	}

	err = p.runner.QueryRow(ctx, qb, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to execute query: %w", err)
	}
	return count, nil
}
