package db

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// MigrateRunner управляет применением SQL миграций
type MigrateRunner struct {
	db     *pgxpool.Pool
	logger *zap.Logger
}

// NewMigrateRunner создаёт новый runner для миграций
func NewMigrateRunner(db *pgxpool.Pool, logger *zap.Logger) *MigrateRunner {
	return &MigrateRunner{
		db:     db,
		logger: logger,
	}
}

// RunMigrations применяет все pending миграции
func (m *MigrateRunner) RunMigrations(migrationsDir string) error {
	// Создаём таблицу для отслеживания миграций, если её нет
	err := m.createMigrationsTable()
	if err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// Получаем список применённых миграций
	applied, err := m.getAppliedMigrations()
	if err != nil {
		return fmt.Errorf("failed to get applied migrations: %w", err)
	}

	// Читаем файлы миграций
	files, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("failed to read migrations dir: %w", err)
	}

	// Сортируем файлы
	sort.Slice(files, func(i, j int) bool {
		return files[i].Name() < files[j].Name()
	})

	// Применяем каждую миграцию
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".sql") {
			continue
		}

		// Проверяем, была ли уже применена
		if _, ok := applied[file.Name()]; ok {
			m.logger.Info("Migration already applied", zap.String("file", file.Name()))
			continue
		}

		// Читаем содержимое файла
		path := filepath.Join(migrationsDir, file.Name())
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read migration %s: %w", file.Name(), err)
		}

		// Выполняем миграцию в транзакции
		err = m.runMigration(file.Name(), string(content))
		if err != nil {
			return fmt.Errorf("failed to run migration %s: %w", file.Name(), err)
		}

		m.logger.Info("Migration applied successfully", zap.String("file", file.Name()))
	}

	m.logger.Info("All migrations completed successfully")
	return nil
}

// createMigrationsTable создаёт таблицу для отслеживания миграций
func (m *MigrateRunner) createMigrationsTable() error {
	query := `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			id SERIAL PRIMARY KEY,
			filename VARCHAR(255) UNIQUE NOT NULL,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`
	_, err := m.db.Exec(context.Background(), query)
	return err
}

// getAppliedMigrations возвращает список уже применённых миграций
func (m *MigrateRunner) getAppliedMigrations() (map[string]bool, error) {
	query := `SELECT filename FROM schema_migrations`
	rows, err := m.db.Query(context.Background(), query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var filename string
		if err := rows.Scan(&filename); err != nil {
			return nil, err
		}
		applied[filename] = true
	}

	return applied, rows.Err()
}

// runMigration выполняет одну миграцию и записывает её в историю
func (m *MigrateRunner) runMigration(filename string, content string) error {
	ctx := context.Background()

	// Начинаем транзакцию
	tx, err := m.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Разделяем контент на отдельные команды (по точке с запятой)
	statements := splitStatements(content)

	// Выполняем каждую команду
	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}

		_, err = tx.Exec(ctx, stmt)
		if err != nil {
			return fmt.Errorf("failed to execute statement: %w", err)
		}
	}

	// Записываем миграцию в историю
	_, err = tx.Exec(ctx, 
		`INSERT INTO schema_migrations (filename, applied_at) VALUES ($1, $2)`,
		filename, time.Now())
	if err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}

	// Коммитим транзакцию
	return tx.Commit(ctx)
}

// splitStatements разделяет SQL скрипт на отдельные команды
func splitStatements(content string) []string {
	// Простое разделение по точке с запятой
	// В реальном production коде нужно учитывать строковые литералы и комментарии
	var statements []string
	var current strings.Builder

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		// Пропускаем комментарии
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") || strings.HasPrefix(trimmed, "/*") {
			continue
		}

		current.WriteString(line)
		current.WriteString("\n")

		// Если строка заканчивается на точку с запятой - это конец команды
		if strings.HasSuffix(trimmed, ";") {
			statements = append(statements, current.String())
			current.Reset()
		}
	}

	// Добавляем остаток, если есть
	if current.Len() > 0 {
		statements = append(statements, current.String())
	}

	return statements
}
