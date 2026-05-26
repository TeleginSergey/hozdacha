package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"
)

const CategoriesTable = "categories"

const (
	CategoriesID          = "categories_id_pk"
	CategoriesName        = "categories_name"
	CategoriesDescription = "categories_description"
	CategoriesCreatedAt   = "categories_created_at"
)

type Category struct {
	ID          int64      `db:"categories_id_pk" json:"id"`
	Name        string     `db:"categories_name" json:"name"`
	Description *string    `db:"categories_description" json:"description,omitempty"`
	CreatedAt   *time.Time `db:"categories_created_at" json:"created_at,omitempty"`
}

type CategoryQuery struct {
	*DB
}

// UpsertByName возвращает ID категории по имени, создавая её если не существует.
// Работает без уникального индекса: SELECT → INSERT с fallback SELECT на race condition.
func (q *CategoryQuery) UpsertByName(ctx context.Context, name string) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	selectSQL := `SELECT ` + CategoriesID + ` FROM ` + CategoriesTable + ` WHERE ` + CategoriesName + ` = $1`
	insertSQL := `INSERT INTO ` + CategoriesTable + ` (` + CategoriesName + `) VALUES ($1) RETURNING ` + CategoriesID

	var id int64
	err := q.Pool.QueryRow(ctx, selectSQL, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("failed to find category %q: %w", name, err)
	}

	// Категория не найдена — создаём.
	err = q.Pool.QueryRow(ctx, insertSQL, name).Scan(&id)
	if err != nil {
		// Race condition: другой процесс вставил раньше — повторяем SELECT.
		if err2 := q.Pool.QueryRow(ctx, selectSQL, name).Scan(&id); err2 == nil {
			return id, nil
		}
		return 0, fmt.Errorf("failed to insert category %q: %w", name, err)
	}
	return id, nil
}

func (q *CategoryQuery) ListAll(ctx context.Context) ([]*Category, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var categories []*Category
	sql := `SELECT ` + CategoriesID + `, ` + CategoriesName + `, ` + CategoriesDescription + `, ` + CategoriesCreatedAt + `
		FROM ` + CategoriesTable + `
		ORDER BY ` + CategoriesName + ` ASC`

	if err := pgxscan.Select(ctx, q.Pool, &categories, sql); err != nil {
		return nil, fmt.Errorf("failed to list categories: %w", err)
	}
	return categories, nil
}
