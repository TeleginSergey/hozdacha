package services

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

type Scheduler struct {
	syncService        *MoyskladSyncService
	workerPool         *SyncWorkerPool
	maxWorkers         int
	interval           time.Duration
	reseedFullInterval time.Duration
	logger             *zap.Logger
	stopChan           chan struct{}
	syncRunning        int32
}

func NewScheduler(syncService *MoyskladSyncService, interval time.Duration, maxWorkers int, reseedFullInterval time.Duration, logger *zap.Logger) *Scheduler {
	if maxWorkers < 1 {
		maxWorkers = 1
	}
	workerPool := NewSyncWorkerPool(syncService, maxWorkers, logger)

	return &Scheduler{
		syncService:        syncService,
		workerPool:         workerPool,
		maxWorkers:         maxWorkers,
		interval:           interval,
		reseedFullInterval: reseedFullInterval,
		logger:             logger,
		stopChan:           make(chan struct{}),
	}
}

func (s *Scheduler) Start(ctx context.Context) {
	if s.syncService == nil {
		s.logger.Warn("Sync service not initialized, scheduler disabled")
		return
	}

	// Запускаем worker pool в отдельной goroutine
	go s.workerPool.Start(ctx)

	if s.reseedFullInterval > 0 {
		go s.runReseedFullLoop(ctx)
	}

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	s.logger.Info("Scheduler started with worker pool",
		zap.Duration("interval", s.interval),
		zap.Int("max_workers", s.maxWorkers),
		zap.Duration("reseed_full_interval", s.reseedFullInterval))

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

func (s *Scheduler) runReseedFullLoop(ctx context.Context) {
	t := time.NewTicker(s.reseedFullInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			rctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
			s.logger.Info("Scheduled reseed: full Moysklad sync")
			if err := s.FullSync(rctx); err != nil {
				s.logger.Error("Reseed full sync failed", zap.Error(err))
			} else {
				s.logger.Info("Reseed full sync finished")
			}
			cancel()
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
	if !atomic.CompareAndSwapInt32(&s.syncRunning, 0, 1) {
		s.logger.Warn("Skipping backup sync because another sync is already running")
		return
	}
	defer atomic.StoreInt32(&s.syncRunning, 0)

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

// FullSync выполняет полную синхронизацию всех продуктов с МойСклад через worker pool
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
	if !atomic.CompareAndSwapInt32(&s.syncRunning, 0, 1) {
		return fmt.Errorf("another sync is already running")
	}
	defer atomic.StoreInt32(&s.syncRunning, 0)

	s.logger.Info("Starting full product sync with worker pool")

	stockMap, err := s.syncService.GetStockMapForSync(ctx)
	if err != nil {
		s.logger.Error("Failed to get stock report from Moysklad", zap.Error(err))
		return fmt.Errorf("failed to get stock report from Moysklad: %w", err)
	}

	// Получаем все товары из МойСклад
	moyskladProducts, err := s.syncService.GetProductsForSync(ctx, false, nil)
	if err != nil {
		s.logger.Error("Failed to get products from Moysklad", zap.Error(err))
		return fmt.Errorf("failed to get products from Moysklad: %w", err)
	}

	s.logger.Info("Products retrieved from Moysklad", zap.Int("total", len(moyskladProducts)))

	// Разбиваем на маленькие батчи и отправляем в worker pool
	batchSize := 3 // Очень маленький батч
	totalProducts := len(moyskladProducts)

	if totalProducts > 1000 {
		batchSize = 2
	}
	if totalProducts > 5000 {
		batchSize = 1
	}

	totalBatches := (totalProducts + batchSize - 1) / batchSize
	s.logger.Info("Splitting products into batches for worker pool",
		zap.Int("batch_size", batchSize),
		zap.Int("total_products", totalProducts),
		zap.Int("total_batches", totalBatches))

	// Отправляем все батчи в очередь worker pool
	for i := 0; i < totalProducts; i += batchSize {
		end := i + batchSize
		if end > totalProducts {
			end = totalProducts
		}

		batch := moyskladProducts[i:end]
		task := SyncTask{
			ID:           fmt.Sprintf("full-sync-%d", i/batchSize),
			Type:         "batch",
			Products:     batch,
			StockByID:    stockMap,
			BatchIndex:   i / batchSize,
			TotalBatches: totalBatches,
		}

		s.workerPool.PushTask(task)
	}

	s.logger.Info("All batches queued for worker pool",
		zap.Int("total_batches", totalBatches),
		zap.String("note", "workers will process batches in parallel"))

	return nil
}

func (s *Scheduler) Stop() {
	s.logger.Info("Stopping scheduler and worker pool")
	close(s.stopChan)
	if s.workerPool != nil {
		s.workerPool.Stop()
	}
}
