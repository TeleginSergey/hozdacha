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
	CategoriesParentID    = "categories_parent_id_fk"
	CategoriesCreatedAt   = "categories_created_at"
)

type Category struct {
	ID          int64      `db:"categories_id_pk" json:"id"`
	Name        string     `db:"categories_name" json:"name"`
	Description *string    `db:"categories_description" json:"description,omitempty"`
	ParentID    *int64     `db:"categories_parent_id_fk" json:"parent_id"`
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

// SetParent устанавливает родительскую категорию по ID. Если parentID == nil — очищает связь.
func (q *CategoryQuery) SetParent(ctx context.Context, id int64, parentID *int64) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	sql := `UPDATE ` + CategoriesTable + ` SET ` + CategoriesParentID + ` = $1 WHERE ` + CategoriesID + ` = $2`
	_, err := q.Pool.Exec(ctx, sql, parentID, id)
	if err != nil {
		return fmt.Errorf("failed to set parent for category %d: %w", id, err)
	}
	return nil
}

// AncestorMap возвращает для каждой категории список её ID-предков (включая саму категорию).
// Используется для разрешения акций на родительские категории: если акция привязана к
// «Посуда», то все её подкатегории её получают.
func (q *CategoryQuery) AncestorMap(ctx context.Context) (map[int64][]int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	sql := `SELECT ` + CategoriesID + `, ` + CategoriesParentID + ` FROM ` + CategoriesTable
	rows, err := q.Pool.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("failed to load categories: %w", err)
	}
	defer rows.Close()

	parent := make(map[int64]*int64)
	for rows.Next() {
		var id int64
		var pid *int64
		if err := rows.Scan(&id, &pid); err != nil {
			return nil, fmt.Errorf("failed to scan category: %w", err)
		}
		parent[id] = pid
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	// Защита от циклов на случай битых данных.
	const maxDepth = 32
	result := make(map[int64][]int64, len(parent))
	for id := range parent {
		chain := []int64{id}
		cur := parent[id]
		for depth := 0; cur != nil && depth < maxDepth; depth++ {
			chain = append(chain, *cur)
			cur = parent[*cur]
		}
		result[id] = chain
	}
	return result, nil
}

func (q *CategoryQuery) ListAll(ctx context.Context) ([]*Category, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var categories []*Category
	sql := `SELECT ` + CategoriesID + `, ` + CategoriesName + `, ` + CategoriesDescription + `, ` + CategoriesParentID + `, ` + CategoriesCreatedAt + `
		FROM ` + CategoriesTable + `
		ORDER BY ` + CategoriesName + ` ASC`

	if err := pgxscan.Select(ctx, q.Pool, &categories, sql); err != nil {
		return nil, fmt.Errorf("failed to list categories: %w", err)
	}
	return categories, nil
}
