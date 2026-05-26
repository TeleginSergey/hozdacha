package db

import (
	"context"
	"fmt"
	"time"

	"github.com/georgysavva/scany/v2/pgxscan"
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
