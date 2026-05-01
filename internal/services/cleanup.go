package services

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/db"
)

type CleanupService struct {
	userQuery db.UserQuery
	logger    *zap.Logger
}

func NewCleanupService(userQuery db.UserQuery, logger *zap.Logger) *CleanupService {
	return &CleanupService{
		userQuery: userQuery,
		logger:    logger,
	}
}

// CleanPendingUsers - cleans up users who didn't verify email within 24 hours
func (s *CleanupService) CleanPendingUsers(ctx context.Context) error {
	// Since we removed email verification, no cleanup needed
	s.logger.Info("Email verification disabled - no cleanup needed")
	return nil
}

// StartCleanupScheduler - runs cleanup every 6 hours
func (s *CleanupService) StartCleanupScheduler(ctx context.Context) {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()

	s.logger.Info("Starting cleanup scheduler for pending users")

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("Cleanup scheduler stopped")
			return
		case <-ticker.C:
			if err := s.CleanPendingUsers(ctx); err != nil {
				s.logger.Error("Scheduled cleanup failed", zap.Error(err))
			}
		}
	}
}
