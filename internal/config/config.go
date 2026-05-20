package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	DB       DBConfig
	Server   ServerConfig
	JWT      JWTConfig
	Moysklad MoyskladConfig
	Redis    RedisConfig
	SMTP     SMTPConfig
}

type DBConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	Name     string
	SSLMode  string
}

type ServerConfig struct {
	Port        string
	Host        string
	CORSOrigins []string
}

type JWTConfig struct {
	Secret     string
	Expiration time.Duration
}

type MoyskladConfig struct {
	Token             string
	BaseURL           string
	SyncInterval      time.Duration // Интервал автоматической синхронизации
	AutoSync          bool          // Включить автоматическую синхронизацию
	WebhookSecret     string        // Секрет для проверки webhook'ов
	StockBuffer       float64       // Буфер остатков в процентах (5-10%)
	RequestsPerSecond float64       // Лимит запросов в секунду к API МойСклад
	MaxRetries        int           // Максимальное количество попыток при ошибках
	// SyncWorkers — параллельные воркеры пула синхронизации (сеть/IO к МойСклад; не обязано совпадать с числом CPU).
	SyncWorkers int
	// ReseedFullInterval — редкая полная пересинхронизация (0 = отключено).
	ReseedFullInterval time.Duration
	// ImageSyncInterval — периодичность скачивания изображений товаров из МойСклад (0 = отключено).
	ImageSyncInterval time.Duration
	// ImageSyncTime — точное время суток для запуска (например "04:00"). Имеет приоритет над ImageSyncInterval.
	ImageSyncTime string
	// Webhook inbox / worker
	WebhookWorkerInterval time.Duration
	WebhookInboxBatch     int
	WebhookMaxAttempts    int
	// Circuit breaker исходящих запросов к API МойСклад
	CircuitFailThreshold  int
	CircuitOpenTimeout    time.Duration
	MaxConcurrentRequests int
	// OrganizationID — UUID организации в МойСклад, от имени которой создаются CustomerOrder.
	// AgentID — UUID контрагента-покупателя по умолчанию (например, "Розничный покупатель").
	// Узнать UUID можно через GET /entity/organization и /entity/counterparty.
	OrganizationID string
	AgentID        string
}

type RedisConfig struct {
	Host     string
	Port     string
	Password string
	DB       int
}

type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	UseTLS   bool
}

func Load() (*Config, error) {
	if err := godotenv.Load(); err != nil {
		// Не критично, если .env не найден
	}

	dbPassword, err := requireEnv("DB_PASSWORD")
	if err != nil {
		return nil, err
	}
	jwtSecret, err := requireEnv("JWT_SECRET")
	if err != nil {
		return nil, err
	}

	port, _ := strconv.Atoi(getEnv("DB_PORT", "5432"))
	expiration, _ := time.ParseDuration(getEnv("JWT_EXPIRATION", "24h"))

	// CORS: читаем список разрешённых origins из переменной (через запятую)
	var corsOrigins []string
	if raw := os.Getenv("CORS_ALLOWED_ORIGINS"); raw != "" {
		for _, o := range strings.Split(raw, ",") {
			if o = strings.TrimSpace(o); o != "" {
				corsOrigins = append(corsOrigins, o)
			}
		}
	}

	return &Config{
		DB: DBConfig{
			Host:     getEnv("DB_HOST", "postgres"),
			Port:     port,
			User:     getEnv("DB_USER", "postgres"),
			Password: dbPassword,
			Name:     getEnv("DB_NAME", "hozdacha"),
			SSLMode:  getEnv("DB_SSLMODE", "disable"),
		},
		Server: ServerConfig{
			Port:        getEnv("SERVER_PORT", "8080"),
			Host:        getEnv("SERVER_HOST", "0.0.0.0"),
			CORSOrigins: corsOrigins,
		},
		JWT: JWTConfig{
			Secret:     jwtSecret,
			Expiration: expiration,
		},
		Moysklad: MoyskladConfig{
			Token:                 os.Getenv("MOYSKLAD_TOKEN"),
			BaseURL:               getEnv("MOYSKLAD_BASE_URL", "https://api.moysklad.ru/api/remap/1.2"),
			AutoSync:              getEnv("MOYSKLAD_AUTO_SYNC", "true") == "true",
			SyncInterval:          parseDuration(getEnv("MOYSKLAD_SYNC_INTERVAL", "1h")), // Резервная синхронизация каждые 1 час
			WebhookSecret:         os.Getenv("MOYSKLAD_WEBHOOK_SECRET"),
			StockBuffer:           parseFloat(getEnv("MOYSKLAD_STOCK_BUFFER", "3.0")),         // Уменьшили буфер до 3%
			RequestsPerSecond:     parseFloat(getEnv("MOYSKLAD_REQUESTS_PER_SECOND", "10.0")), // 10/сек = 600/минуту (75% от 800 лимита)
			MaxRetries:            parseInt(getEnv("MOYSKLAD_MAX_RETRIES", "5")),              // в т.ч. повторы при 503
			SyncWorkers:           parseInt(getEnv("MOYSKLAD_SYNC_WORKERS", "3")),
			ReseedFullInterval:    parseDurationOrZero(getEnv("MOYSKLAD_RESEED_FULL_INTERVAL", "24h")),
			ImageSyncInterval:     parseDurationOrZero(getEnv("MOYSKLAD_IMAGE_SYNC_INTERVAL", "6h")),
			ImageSyncTime:         getEnv("MOYSKLAD_IMAGE_SYNC_TIME", ""),
			WebhookWorkerInterval: parseDuration(getEnv("WEBHOOK_INBOX_POLL_INTERVAL", "2s")),
			WebhookInboxBatch:     parseInt(getEnv("WEBHOOK_INBOX_BATCH", "10")),
			WebhookMaxAttempts:    parseInt(getEnv("WEBHOOK_MAX_ATTEMPTS", "15")),
			CircuitFailThreshold:  parseInt(getEnv("MOYSKLAD_CIRCUIT_FAIL_THRESHOLD", "5")),
			CircuitOpenTimeout:    parseDuration(getEnv("MOYSKLAD_CIRCUIT_OPEN_TIMEOUT", "60s")),
			MaxConcurrentRequests: parseInt(getEnv("MOYSKLAD_MAX_CONCURRENT_REQUESTS", "8")),
			OrganizationID:        os.Getenv("MOYSKLAD_ORGANIZATION_ID"),
			AgentID:               os.Getenv("MOYSKLAD_AGENT_ID"),
		},
		Redis: RedisConfig{
			Host:     getEnv("REDIS_HOST", "redis"),
			Port:     getEnv("REDIS_PORT", "6379"),
			Password: os.Getenv("REDIS_PASSWORD"),
			DB:       parseInt(getEnv("REDIS_DB", "0")),
		},
		SMTP: SMTPConfig{
			Host:     getEnv("SMTP_HOST", "smtp.gmail.com"),
			Port:     parseInt(getEnv("SMTP_PORT", "587")),
			Username: os.Getenv("SMTP_USERNAME"),
			Password: os.Getenv("SMTP_PASSWORD"),
			From:     getEnv("SMTP_FROM", "noreply@hozdacha.ru"),
			UseTLS:   getEnv("SMTP_USE_TLS", "true") == "true",
		},
	}, nil
}

func (c *DBConfig) DSN() string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.Name, c.SSLMode)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func requireEnv(key string) (string, error) {
	if value := os.Getenv(key); value != "" {
		return value, nil
	}
	return "", fmt.Errorf("required environment variable %s is not set", key)
}

func parseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		// По умолчанию 10 минут
		return 10 * time.Minute
	}
	return d
}

// parseDurationOrZero — как parseDuration, но "0"/"off" отключают фичу.
func parseDurationOrZero(s string) time.Duration {
	if s == "" || s == "0" || s == "off" || s == "false" {
		return 0
	}
	return parseDuration(s)
}

func parseFloat(s string) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 5.0 // По умолчанию 5%
	}
	return f
}

func parseInt(s string) int {
	i, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return i
}
