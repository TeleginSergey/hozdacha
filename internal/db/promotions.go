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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

const PromotionsTable = "promotions"

const (
	PromotionsID          = "promotions_id_pk"
	PromotionsTitle       = "promotions_title"
	PromotionsDescription = "promotions_description"
	PromotionsDiscount    = "promotions_discount"
	PromotionsImageURL    = "promotions_image_url"
	PromotionsActive      = "promotions_active"
	PromotionsStartDate   = "promotions_start_date"
	PromotionsEndDate     = "promotions_end_date"
	PromotionsCreatedAt   = "promotions_created_at"
	PromotionsUpdatedAt   = "promotions_updated_at"
)

type Promotion struct {
	ID          int64      `db:"promotions_id_pk"`
	Title       string     `db:"promotions_title" insert:"promotions_title" update:"promotions_title"`
	Description *string    `db:"promotions_description" insert:"promotions_description" update:"promotions_description"`
	Discount    float64    `db:"promotions_discount" insert:"promotions_discount" update:"promotions_discount"`
	ImageURL    *string    `db:"promotions_image_url" insert:"promotions_image_url" update:"promotions_image_url"`
	Active      bool       `db:"promotions_active" insert:"promotions_active" update:"promotions_active"`
	StartDate   *time.Time `db:"promotions_start_date" insert:"promotions_start_date" update:"promotions_start_date"`
	EndDate     *time.Time `db:"promotions_end_date" insert:"promotions_end_date" update:"promotions_end_date"`
	CreatedAt   time.Time  `db:"promotions_created_at"`
	UpdatedAt   time.Time  `db:"promotions_updated_at" update:"promotions_updated_at"`
}

var (
	stomPromotionSelect = stom.MustNewStom(Promotion{}).SetTag(selectTag)
	stomPromotionInsert = stom.MustNewStom(Promotion{}).SetTag(insertTag)
	stomPromotionUpdate = stom.MustNewStom(Promotion{}).SetTag(updateTag)
)

func (p *Promotion) columns(pref string) []string {
	return colNamesWithPref(stomPromotionSelect.TagValues(), pref)
}

type PromotionQuery interface {
	GetByID(ctx context.Context, id int64) (*Promotion, error)
	GetActive(ctx context.Context) ([]*Promotion, error)
	GetAll(ctx context.Context) ([]*Promotion, error)
	Insert(ctx context.Context, promotion *Promotion) (*Promotion, error)
	Update(ctx context.Context, promotion *Promotion, id int64) (*Promotion, error)
	Delete(ctx context.Context, id int64) error
}

type promotionQuery struct {
	runner *pgxpool.Pool
	sq     squirrel.StatementBuilderType
	logger *zap.Logger
}

func NewPromotionQuery(runner *pgxpool.Pool, sq squirrel.StatementBuilderType, logger *zap.Logger) PromotionQuery {
	return &promotionQuery{
		runner: runner,
		sq:     sq,
		logger: logger,
	}
}

func (p *promotionQuery) GetByID(ctx context.Context, id int64) (*Promotion, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	promotion := &Promotion{}
	qb, args, err := p.sq.Select(promotion.columns("")...).
		From(PromotionsTable).
		Where(squirrel.Eq{PromotionsID: id}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	err = pgxscan.Get(ctx, p.runner, promotion, qb, args...)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			p.logger.Warn("Database error",
				zap.Int64("promotion_id", id),
				zap.String("pg_error_code", pgErr.Code),
				zap.Error(err),
			)
		}
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	return promotion, nil
}

func (p *promotionQuery) GetActive(ctx context.Context) ([]*Promotion, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	now := time.Now()
	var promotions []*Promotion
	promotion := &Promotion{}
	qb, args, err := p.sq.Select(promotion.columns("")...).
		From(PromotionsTable).
		Where(squirrel.And{
			squirrel.Eq{PromotionsActive: true},
			squirrel.Or{
				squirrel.LtOrEq{PromotionsStartDate: now},
				squirrel.Eq{PromotionsStartDate: nil},
			},
			squirrel.Or{
				squirrel.GtOrEq{PromotionsEndDate: now},
				squirrel.Eq{PromotionsEndDate: nil},
			},
		}).
		OrderBy(PromotionsCreatedAt + " DESC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	err = pgxscan.Select(ctx, p.runner, &promotions, qb, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	return promotions, nil
}

func (p *promotionQuery) GetAll(ctx context.Context) ([]*Promotion, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var promotions []*Promotion
	promotion := &Promotion{}
	qb, args, err := p.sq.Select(promotion.columns("")...).
		From(PromotionsTable).
		OrderBy(PromotionsCreatedAt + " DESC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	err = pgxscan.Select(ctx, p.runner, &promotions, qb, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	return promotions, nil
}

func (p *promotionQuery) Insert(ctx context.Context, promotion *Promotion) (*Promotion, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	insertMap, err := stomPromotionInsert.ToMap(promotion)
	if err != nil {
		return nil, fmt.Errorf("failed to map struct: %w", err)
	}
	qb, args, err := p.sq.Insert(PromotionsTable).
		SetMap(insertMap).
		Suffix("RETURNING *").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}
	err = pgxscan.Get(ctx, p.runner, promotion, qb, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	p.logger.Info("Promotion inserted successfully", zap.Int64("promotion_id", promotion.ID))
	return promotion, nil
}

func (p *promotionQuery) Update(ctx context.Context, promotion *Promotion, id int64) (*Promotion, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	updateMap, err := stomPromotionUpdate.ToMap(promotion)
	if err != nil {
		return nil, fmt.Errorf("failed to map struct: %w", err)
	}
	qb, args, err := p.sq.Update(PromotionsTable).
		SetMap(updateMap).
		Where(squirrel.Eq{PromotionsID: id}).
		Suffix("RETURNING *").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}
	err = pgxscan.Get(ctx, p.runner, promotion, qb, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	p.logger.Info("Promotion updated successfully", zap.Int64("promotion_id", promotion.ID))
	return promotion, nil
}

func (p *promotionQuery) Delete(ctx context.Context, id int64) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	qb, args, err := p.sq.Delete(PromotionsTable).
		Where(squirrel.Eq{PromotionsID: id}).
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build query: %w", err)
	}

	result, err := p.runner.Exec(ctx, qb, args...)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("no promotion found with id %d", id)
	}

	p.logger.Info("Promotion deleted successfully", zap.Int64("promotion_id", id))
	return nil
}



