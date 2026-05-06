package services

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
)

type Scheduler struct {
	syncService *MoyskladSyncService
	interval    time.Duration
	logger      *zap.Logger
	stopChan    chan struct{}
}

func NewScheduler(syncService *MoyskladSyncService, interval time.Duration, logger *zap.Logger) *Scheduler {
	return &Scheduler{
		syncService: syncService,
		interval:    interval,
		logger:      logger,
		stopChan:    make(chan struct{}),
	}
}

func (s *Scheduler) Start(ctx context.Context) {
	if s.syncService == nil {
		s.logger.Warn("Sync service not initialized, scheduler disabled")
		return
	}

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	s.logger.Info("Scheduler started", zap.Duration("interval", s.interval))

	// Выполняем первую синхронизацию сразу при старте
	go s.syncOnce(ctx)

	// Затем синхронизируем по расписанию
	for {
		select {
		case <-ticker.C:
			s.syncOnce(ctx)
		case <-s.stopChan:
			s.logger.Info("Scheduler stopped")
			return
		case <-ctx.Done():
			s.logger.Info("Scheduler stopped due to context cancellation")
			return
		}
	}
}

func (s *Scheduler) syncOnce(ctx context.Context) {
	// Защищаемся от паник
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("syncOnce panicked",
				zap.Any("panic", r),
				zap.String("stack", fmt.Sprintf("%+v", r)))
		}
	}()

	s.logger.Info("Starting backup product sync (webhooks handle real-time updates)")

	// Используем дельта-синхронизацию как резервный механизм
	result, err := s.syncService.SyncProductsDelta(ctx)
	if err != nil {
		s.logger.Error("Backup sync failed, webhooks should handle most updates", zap.Error(err))
		return
	}

	// Логируем только если были изменения (вебхуки должны обрабатывать основную нагрузку)
	if result.Created > 0 || result.Updated > 0 {
		s.logger.Info("Backup sync found missed updates",
			zap.Int("created", result.Created),
			zap.Int("updated", result.Updated),
			zap.Int("errors", result.Errors),
			zap.String("note", "consider checking webhook configuration"))
	} else {
		s.logger.Debug("Backup sync completed - no changes (webhooks working correctly)")
	}
}

// FullSync выполняет полную синхронизацию всех продуктов с МойСклад
func (s *Scheduler) FullSync(ctx context.Context) error {
	// Защищаемся от паник
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("FullSync panicked",
				zap.Any("panic", r),
				zap.String("stack", fmt.Sprintf("%+v", r)))
		}
	}()

	if s.syncService == nil {
		return fmt.Errorf("sync service not initialized")
	}

	s.logger.Info("Starting full product sync on startup")

	// Выполняем полную синхронизацию
	result, err := s.syncService.SyncProducts(ctx)
	if err != nil {
		s.logger.Error("Full sync failed", zap.Error(err))
		return err
	}

	s.logger.Info("Full sync completed successfully",
		zap.Int("processed", result.Created+result.Updated),
		zap.Int("created", result.Created),
		zap.Int("updated", result.Updated),
		zap.Int("errors", result.Errors))

	return nil
}

func (s *Scheduler) Stop() {
	close(s.stopChan)
}
