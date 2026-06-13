package db

import (
	"context"
	"fmt"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// Таблицы и колонки для связей акций.
const (
	PromotionProductsTable   = "promotion_products"
	PromotionCategoriesTable = "promotion_categories"

	PromotionLinkPromotionID = "promotion_id"
	PromotionLinkProductID   = "product_id"
	PromotionLinkCategoryID  = "category_id"
)

// PromotionLinkQuery управляет связями акция-товар и акция-категория.
// Логика приоритета (product > category > base) применяется в usecase, здесь —
// только хранение и выборка.
type PromotionLinkQuery interface {
	// Управление связями.
	ReplaceProductLinks(ctx context.Context, promotionID int64, productIDs []int64) error
	ReplaceCategoryLinks(ctx context.Context, promotionID int64, categoryIDs []int64) error

	// Чтение связей конкретной акции.
	ListProductIDs(ctx context.Context, promotionID int64) ([]int64, error)
	ListCategoryIDs(ctx context.Context, promotionID int64) ([]int64, error)

	// Чтение связей пакета акций одним запросом — для публичной выдачи с фронтом
	// (нужно построить ссылку «к товару» / «в категорию»).
	ListProductIDsForPromotions(ctx context.Context, promotionIDs []int64) (map[int64][]int64, error)
	ListCategoryIDsForPromotions(ctx context.Context, promotionIDs []int64) (map[int64][]int64, error)

	// Активные акции для пакета товаров/категорий — для расчёта эффективной цены.
	// Возвращает максимальный (лучший для покупателя) discount по product_id / category_id.
	BestActiveProductPromotions(ctx context.Context, productIDs []int64) (map[int64]*Promotion, error)
	BestActiveCategoryPromotions(ctx context.Context, categoryIDs []int64) (map[int64]*Promotion, error)
}

type promotionLinkQuery struct {
	runner *pgxpool.Pool
	sq     squirrel.StatementBuilderType
	logger *zap.Logger
}

func NewPromotionLinkQuery(runner *pgxpool.Pool, sq squirrel.StatementBuilderType, logger *zap.Logger) PromotionLinkQuery {
	return &promotionLinkQuery{runner: runner, sq: sq, logger: logger}
}

// replaceLinks — общая логика для product/category связей: атомарно удалить старые и вставить новые.
func (p *promotionLinkQuery) replaceLinks(
	ctx context.Context,
	table, fkCol string,
	promotionID int64,
	ids []int64,
) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	tx, err := p.runner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	delQB, delArgs, err := p.sq.Delete(table).
		Where(squirrel.Eq{PromotionLinkPromotionID: promotionID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build delete query: %w", err)
	}
	if _, err := tx.Exec(ctx, delQB, delArgs...); err != nil {
		return fmt.Errorf("failed to delete old links: %w", err)
	}

	if len(ids) > 0 {
		ins := p.sq.Insert(table).Columns(PromotionLinkPromotionID, fkCol)
		seen := make(map[int64]struct{}, len(ids))
		for _, id := range ids {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ins = ins.Values(promotionID, id)
		}
		insQB, insArgs, err := ins.Suffix("ON CONFLICT DO NOTHING").ToSql()
		if err != nil {
			return fmt.Errorf("failed to build insert query: %w", err)
		}
		if _, err := tx.Exec(ctx, insQB, insArgs...); err != nil {
			return fmt.Errorf("failed to insert new links: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit tx: %w", err)
	}
	p.logger.Debug("promotion links replaced",
		zap.String("table", table),
		zap.Int64("promotion_id", promotionID),
		zap.Int("count", len(ids)),
	)
	return nil
}

func (p *promotionLinkQuery) ReplaceProductLinks(ctx context.Context, promotionID int64, productIDs []int64) error {
	return p.replaceLinks(ctx, PromotionProductsTable, PromotionLinkProductID, promotionID, productIDs)
}

func (p *promotionLinkQuery) ReplaceCategoryLinks(ctx context.Context, promotionID int64, categoryIDs []int64) error {
	return p.replaceLinks(ctx, PromotionCategoriesTable, PromotionLinkCategoryID, promotionID, categoryIDs)
}

func (p *promotionLinkQuery) listIDs(ctx context.Context, table, fkCol string, promotionID int64) ([]int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	qb, args, err := p.sq.Select(fkCol).
		From(table).
		Where(squirrel.Eq{PromotionLinkPromotionID: promotionID}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}
	var ids []int64
	if err := pgxscan.Select(ctx, p.runner, &ids, qb, args...); err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	return ids, nil
}

func (p *promotionLinkQuery) ListProductIDs(ctx context.Context, promotionID int64) ([]int64, error) {
	return p.listIDs(ctx, PromotionProductsTable, PromotionLinkProductID, promotionID)
}

func (p *promotionLinkQuery) ListCategoryIDs(ctx context.Context, promotionID int64) ([]int64, error) {
	return p.listIDs(ctx, PromotionCategoriesTable, PromotionLinkCategoryID, promotionID)
}

// ListProductIDsForPromotions возвращает одним запросом мапу promotion_id -> []product_id
// для переданного набора акций. Используется, чтобы отдавать клиенту достаточно
// информации для построения корректной ссылки на товар/категорию.
func (p *promotionLinkQuery) ListProductIDsForPromotions(ctx context.Context, promotionIDs []int64) (map[int64][]int64, error) {
	result := make(map[int64][]int64)
	if len(promotionIDs) == 0 {
		return result, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	qb, args, err := p.sq.Select(PromotionLinkPromotionID, PromotionLinkProductID).
		From(PromotionProductsTable).
		Where(squirrel.Eq{PromotionLinkPromotionID: promotionIDs}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}
	type row struct {
		PromotionID int64 `db:"promotion_id"`
		ProductID   int64 `db:"product_id"`
	}
	var rows []row
	if err := pgxscan.Select(ctx, p.runner, &rows, qb, args...); err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	for _, r := range rows {
		result[r.PromotionID] = append(result[r.PromotionID], r.ProductID)
	}
	return result, nil
}

// ListCategoryIDsForPromotions — категорийный аналог ListProductIDsForPromotions.
func (p *promotionLinkQuery) ListCategoryIDsForPromotions(ctx context.Context, promotionIDs []int64) (map[int64][]int64, error) {
	result := make(map[int64][]int64)
	if len(promotionIDs) == 0 {
		return result, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	qb, args, err := p.sq.Select(PromotionLinkPromotionID, PromotionLinkCategoryID).
		From(PromotionCategoriesTable).
		Where(squirrel.Eq{PromotionLinkPromotionID: promotionIDs}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}
	type row struct {
		PromotionID int64 `db:"promotion_id"`
		CategoryID  int64 `db:"category_id"`
	}
	var rows []row
	if err := pgxscan.Select(ctx, p.runner, &rows, qb, args...); err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	for _, r := range rows {
		result[r.PromotionID] = append(result[r.PromotionID], r.CategoryID)
	}
	return result, nil
}

// bestActivePromotionsByLink — общая логика для product- и category-уровневых выборок.
// Берёт лучшую (максимальную по discount) активную акцию для каждого fk_id из переданного списка.
func (p *promotionLinkQuery) bestActivePromotionsByLink(
	ctx context.Context,
	table, fkCol string,
	ids []int64,
) (map[int64]*Promotion, error) {
	result := make(map[int64]*Promotion)
	if len(ids) == 0 {
		return result, nil
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	now := time.Now()

	// Выбираем все (fk_id, promotion) пары для активных акций; разруливаем "лучшую" в Go.
	promotion := &Promotion{}
	promoCols := promotion.columns("p")
	cols := append([]string{fmt.Sprintf("l.%s AS link_fk_id", fkCol)}, promoCols...)

	qb, args, err := p.sq.Select(cols...).
		From(fmt.Sprintf("%s l", table)).
		Join(fmt.Sprintf("%s p ON p.%s = l.%s", PromotionsTable, PromotionsID, PromotionLinkPromotionID)).
		Where(squirrel.Eq{fmt.Sprintf("l.%s", fkCol): ids}).
		Where(squirrel.Eq{fmt.Sprintf("p.%s", PromotionsActive): true}).
		Where(squirrel.Or{
			squirrel.LtOrEq{fmt.Sprintf("p.%s", PromotionsStartDate): now},
			squirrel.Eq{fmt.Sprintf("p.%s", PromotionsStartDate): nil},
		}).
		Where(squirrel.Or{
			squirrel.GtOrEq{fmt.Sprintf("p.%s", PromotionsEndDate): now},
			squirrel.Eq{fmt.Sprintf("p.%s", PromotionsEndDate): nil},
		}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	type row struct {
		LinkFkID int64 `db:"link_fk_id"`
		Promotion
	}
	var rows []*row
	if err := pgxscan.Select(ctx, p.runner, &rows, qb, args...); err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	for _, r := range rows {
		existing, ok := result[r.LinkFkID]
		if !ok || r.Discount > existing.Discount {
			promo := r.Promotion
			result[r.LinkFkID] = &promo
		}
	}
	return result, nil
}

func (p *promotionLinkQuery) BestActiveProductPromotions(ctx context.Context, productIDs []int64) (map[int64]*Promotion, error) {
	return p.bestActivePromotionsByLink(ctx, PromotionProductsTable, PromotionLinkProductID, productIDs)
}

func (p *promotionLinkQuery) BestActiveCategoryPromotions(ctx context.Context, categoryIDs []int64) (map[int64]*Promotion, error) {
	return p.bestActivePromotionsByLink(ctx, PromotionCategoriesTable, PromotionLinkCategoryID, categoryIDs)
}
