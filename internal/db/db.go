package db

import (
	"context"
	"fmt"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

const (
	selectTag     = "db"
	insertTag     = "insert"
	updateTag     = "update"
	updateAuthTag = "updateAuth"
)

type DB struct {
	Pool   *pgxpool.Pool
	SQ     squirrel.StatementBuilderType
	Logger *zap.Logger
}

func New(cfg interface{ DSN() string }, logger *zap.Logger) (*DB, error) {
	config, err := pgxpool.ParseConfig(cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("failed to parse database config: %w", err)
	}

	config.MaxConns = 25
	config.MinConns = 5
	config.MaxConnLifetime = 5 * time.Minute
	config.MaxConnIdleTime = 1 * time.Minute

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	logger.Info("Database connection pool created successfully")

	return &DB{
		Pool:   pool,
		SQ:     squirrel.StatementBuilderType{}.PlaceholderFormat(squirrel.Dollar),
		Logger: logger,
	}, nil
}

func (d *DB) Close() {
	d.Pool.Close()
}

func colNamesWithPref(cols []string, pref string) []string {
	if pref == "" {
		return cols
	}
	result := make([]string, len(cols))
	for i, col := range cols {
		result[i] = pref + "." + col
	}
	return result
}
