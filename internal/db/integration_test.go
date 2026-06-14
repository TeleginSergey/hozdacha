//go:build integration

package db

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// Интеграционный тест с реальной PostgreSQL (через сервис-контейнер в CI).
// Поднимает чистую БД, применяет миграции и проверяет наличие ключевых таблиц.
// Запуск: go test -tags=integration ./internal/db/...  (нужен DATABASE_URL)
func TestIntegration_MigrationsAndSchema(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — пропускаем интеграционный тест")
	}
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}

	mr := NewMigrateRunner(pool, zap.NewNop())
	if err := mr.RunMigrations("../../migrations"); err != nil {
		t.Fatalf("migrations: %v", err)
	}

	// Повторный запуск миграций должен быть идемпотентным.
	if err := mr.RunMigrations("../../migrations"); err != nil {
		t.Fatalf("migrations are not idempotent: %v", err)
	}

	for _, table := range []string{"users", "products", "orders", "cart_items", "order_events", "webhook_inbox"} {
		var n int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			t.Errorf("table %q not usable after migrations: %v", table, err)
		}
	}
}
