package main

import (
	"log"
	"os"

	"github.com/TeleginSergey/hozdacha/internal/app"
	"github.com/TeleginSergey/hozdacha/internal/config"
	"github.com/TeleginSergey/hozdacha/internal/db"
	"go.uber.org/zap"
)

func main() {
	// Проверяем аргументы командной строки
	if len(os.Args) > 1 && os.Args[1] == "migrate-up" {
		runMigrations()
		return
	}

	// Обычный запуск приложения
	appInstance, err := app.NewApp()
	if err != nil {
		log.Fatalf("failed to initialize app: %v", err)
	}
	appInstance.Run()
}

// runMigrations запускает миграции базы данных
func runMigrations() {
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("failed to init logger: %v", err)
	}
	defer logger.Sync()

	// Загружаем конфигурацию
	cfg, err := config.Load()
	if err != nil {
		logger.Fatal("Failed to load config", zap.Error(err))
	}

	// Подключаемся к БД
	database, err := db.New(&cfg.DB, logger)
	if err != nil {
		logger.Fatal("Failed to connect to database", zap.Error(err))
	}
	defer database.Close()

	// Создаём runner и запускаем миграции
	runner := db.NewMigrateRunner(database.Pool, logger)

	migrationsDir := "./migrations"
	if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
		// Пробуем альтернативный путь (для Docker)
		migrationsDir = "/root/migrations"
	}

	logger.Info("Running migrations", zap.String("dir", migrationsDir))

	if err := runner.RunMigrations(migrationsDir); err != nil {
		logger.Fatal("Migration failed", zap.Error(err))
	}

	logger.Info("Migrations completed successfully")
}
