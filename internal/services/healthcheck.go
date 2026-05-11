package services

import (
	"context"
	"database/sql"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type HealthCheckService struct {
	db    *sql.DB
	redis *redis.Client
	logger *zap.Logger
}

func NewHealthCheckService(db *sql.DB, redis *redis.Client, logger *zap.Logger) *HealthCheckService {
	return &HealthCheckService{
		db:    db,
		redis: redis,
		logger: logger,
	}
}

// Check выполняет быструю проверку здоровья системы
func (h *HealthCheckService) Check(ctx context.Context) error {
	// Timeout для healthcheck
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	// Проверяем подключение к БД
	if err := h.checkDatabase(ctx); err != nil {
		h.logger.Error("Database health check failed", zap.Error(err))
		return err
	}

	// Проверяем подключение к Redis
	if err := h.checkRedis(ctx); err != nil {
		h.logger.Error("Redis health check failed", zap.Error(err))
		return err
	}

	h.logger.Debug("Health check passed")
	return nil
}

// checkDatabase проверяет доступность БД
func (h *HealthCheckService) checkDatabase(ctx context.Context) error {
	if h.db == nil {
		return nil // БД может быть не инициализирована в некоторых конфигурациях
	}

	// Простой ping с timeout
	if err := h.db.PingContext(ctx); err != nil {
		return err
	}

	return nil
}

// checkRedis проверяет доступность Redis
func (h *HealthCheckService) checkRedis(ctx context.Context) error {
	if h.redis == nil {
		return nil // Redis может быть не инициализирован в некоторых конфигурациях
	}

	// Простой ping с timeout
	if err := h.redis.Ping(ctx).Err(); err != nil {
		return err
	}

	return nil
}

// IsReady проверяет готовность системы к работе
func (h *HealthCheckService) IsReady(ctx context.Context) bool {
	if err := h.Check(ctx); err != nil {
		h.logger.Warn("System not ready", zap.Error(err))
		return false
	}
	
	h.logger.Debug("System is ready")
	return true
}

// GetStatus возвращает детальный статус системы
func (h *HealthCheckService) GetStatus(ctx context.Context) map[string]interface{} {
	status := make(map[string]interface{})
	
	// Проверяем БД
	if h.db != nil {
		if err := h.checkDatabase(ctx); err != nil {
			status["database"] = map[string]string{
				"status": "unhealthy",
				"error":  err.Error(),
			}
		} else {
			status["database"] = map[string]string{
				"status": "healthy",
			}
		}
	} else {
		status["database"] = map[string]string{
			"status": "disabled",
		}
	}
	
	// Проверяем Redis
	if h.redis != nil {
		if err := h.checkRedis(ctx); err != nil {
			status["redis"] = map[string]string{
				"status": "unhealthy",
				"error":  err.Error(),
			}
		} else {
			status["redis"] = map[string]string{
				"status": "healthy",
			}
		}
	} else {
		status["redis"] = map[string]string{
			"status": "disabled",
		}
	}
	
	// Общий статус
	allHealthy := true
	if dbStatus, ok := status["database"].(map[string]string); ok && dbStatus["status"] == "unhealthy" {
		allHealthy = false
	}
	if redisStatus, ok := status["redis"].(map[string]string); ok && redisStatus["status"] == "unhealthy" {
		allHealthy = false
	}
	
	status["overall"] = map[string]interface{}{
		"status":    func() string { if allHealthy { return "healthy" } else { return "unhealthy" } }(),
		"timestamp": time.Now().Unix(),
	}
	
	return status
}
