package services

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// TokenBlacklistService управляет чёрным списком отозванных JWT токенов
type TokenBlacklistService struct {
	redis  *redis.Client
	logger *zap.Logger
}

// NewTokenBlacklistService создаёт сервис для работы с чёрным списком токенов
func NewTokenBlacklistService(redisClient *redis.Client, logger *zap.Logger) *TokenBlacklistService {
	return &TokenBlacklistService{
		redis:  redisClient,
		logger: logger,
	}
}

// AddToBlacklist добавляет токен в чёрный список
// ttl - время жизни записи (обычно оставшееся время жизни токена)
func (s *TokenBlacklistService) AddToBlacklist(ctx context.Context, jti string, ttl time.Duration) error {
	key := fmt.Sprintf("blacklist:jti:%s", jti)
	err := s.redis.Set(ctx, key, "1", ttl).Err()
	if err != nil {
		s.logger.Error("Failed to add token to blacklist",
			zap.String("jti", jti),
			zap.Duration("ttl", ttl),
			zap.Error(err))
		return fmt.Errorf("failed to add token to blacklist: %w", err)
	}

	s.logger.Info("Token added to blacklist",
		zap.String("jti", jti),
		zap.Duration("ttl", ttl))
	return nil
}

// IsBlacklisted проверяет, находится ли токен в чёрном списке
func (s *TokenBlacklistService) IsBlacklisted(ctx context.Context, jti string) (bool, error) {
	key := fmt.Sprintf("blacklist:jti:%s", jti)
	exists, err := s.redis.Exists(ctx, key).Result()
	if err != nil {
		s.logger.Error("Failed to check blacklist",
			zap.String("jti", jti),
			zap.Error(err))
		return false, fmt.Errorf("failed to check blacklist: %w", err)
	}

	return exists > 0, nil
}

// BlacklistAllUserTokens добавляет все активные токены пользователя в чёрный список
// Используется при смене пароля или удалении пользователя
func (s *TokenBlacklistService) BlacklistAllUserTokens(ctx context.Context, userID int64, expiration time.Duration) error {
	// Используем pattern для поиска всех токенов пользователя
	// На практике мы просто добавляем флаг в Redis, что все токены до текущего момента отозваны
	key := fmt.Sprintf("blacklist:user:%d", userID)
	err := s.redis.Set(ctx, key, time.Now().Unix(), expiration).Err()
	if err != nil {
		s.logger.Error("Failed to blacklist user tokens",
			zap.Int64("user_id", userID),
			zap.Error(err))
		return fmt.Errorf("failed to blacklist user tokens: %w", err)
	}

	s.logger.Info("All user tokens blacklisted",
		zap.Int64("user_id", userID))
	return nil
}

// IsUserBlacklisted проверяет, были ли отозваны все токены пользователя
// beforeTime - время создания текущего токена (iat из claims)
func (s *TokenBlacklistService) IsUserBlacklisted(ctx context.Context, userID int64, tokenIssuedAt int64) (bool, error) {
	key := fmt.Sprintf("blacklist:user:%d", userID)
	revokedAt, err := s.redis.Get(ctx, key).Int64()
	if err == redis.Nil {
		return false, nil // Нет записи - пользователь не в чёрном списке
	}
	if err != nil {
		s.logger.Error("Failed to check user blacklist",
			zap.Int64("user_id", userID),
			zap.Error(err))
		return false, fmt.Errorf("failed to check user blacklist: %w", err)
	}

	// Если токен был выпущен до отзыва всех токенов - он недействителен
	return tokenIssuedAt < revokedAt, nil
}
