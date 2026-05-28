package services

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

type Scheduler struct {
	syncService        *MoyskladSyncService
	workerPool         *SyncWorkerPool
	imageSync          *ImageSyncService
	maxWorkers         int
	interval           time.Duration
	reseedFullInterval time.Duration
	imageSyncInterval  time.Duration
	imageSyncTime      string
	logger             *zap.Logger
	stopChan           chan struct{}
	syncRunning        int32
}

func NewScheduler(syncService *MoyskladSyncService, interval time.Duration, maxWorkers int, reseedFullInterval time.Duration, imageSync *ImageSyncService, imageSyncInterval time.Duration, imageSyncTime string, logger *zap.Logger) *Scheduler {
	if maxWorkers < 1 {
		maxWorkers = 1
	}
	workerPool := NewSyncWorkerPool(syncService, maxWorkers, logger)

	return &Scheduler{
		syncService:        syncService,
		workerPool:         workerPool,
		imageSync:          imageSync,
		maxWorkers:         maxWorkers,
		interval:           interval,
		reseedFullInterval: reseedFullInterval,
		imageSyncInterval:  imageSyncInterval,
		imageSyncTime:      imageSyncTime,
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

	// Запускаем периодическую синхронизацию изображений
	if s.imageSync != nil && (s.imageSyncInterval > 0 || s.imageSyncTime != "") {
		go s.runImageSyncLoop(ctx)
	}

	// Лёгкая периодическая синхронизация акций (specialpricediscount).
	// Дёргает только entity/specialpricediscount + апдейт связей — отдельно от
	// тяжёлой синхронизации товаров.
	go s.runPromotionsSyncLoop(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	s.logger.Info("Scheduler started with worker pool",
		zap.Duration("interval", s.interval),
		zap.Int("max_workers", s.maxWorkers),
		zap.Duration("reseed_full_interval", s.reseedFullInterval),
		zap.String("note", "initial sync is triggered by App; scheduler only handles periodic delta sync"))

	// Синхронизируем по расписанию (initial full sync делается из app.Run)
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

// runPromotionsSyncLoop запускает периодическую синхронизацию акций из МойСклад.
// Интервал — фиксированные 10 минут: акции редко меняются, а API дешёвый.
// Первый запуск — через 30 секунд после старта, чтобы дать товарам/категориям загрузиться.
func (s *Scheduler) runPromotionsSyncLoop(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("runPromotionsSyncLoop panicked", zap.Any("panic", r))
		}
	}()

	const interval = 10 * time.Minute

	select {
	case <-ctx.Done():
		return
	case <-time.After(30 * time.Second):
	}
	s.runPromotionsSyncOnce(ctx)

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopChan:
			return
		case <-t.C:
			s.runPromotionsSyncOnce(ctx)
		}
	}
}

func (s *Scheduler) runPromotionsSyncOnce(parent context.Context) {
	if s.syncService == nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()
	if _, err := s.syncService.SyncPromotions(ctx); err != nil {
		s.logger.Warn("Periodic promotions sync failed", zap.Error(err))
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

func (s *Scheduler) runImageSyncLoop(ctx context.Context) {
	// Первый запуск при деплое — не ждём расписания.
	s.logger.Info("Running initial image sync on startup")
	rctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	if err := s.imageSync.SyncImages(rctx); err != nil {
		s.logger.Error("Initial image sync failed", zap.Error(err))
	}
	cancel()

	// Определяем режим: точное время суток или интервал.
	if s.imageSyncTime != "" {
		s.runImageSyncAtTime(ctx)
	} else if s.imageSyncInterval > 0 {
		s.runImageSyncByInterval(ctx)
	}
}

// runImageSyncAtTime запускает синхронизацию ежедневно в заданное время (например "04:00").
func (s *Scheduler) runImageSyncAtTime(ctx context.Context) {
	s.logger.Info("Image sync scheduled daily", zap.String("at", s.imageSyncTime))

	for {
		now := time.Now()
		next := nextTimeOfDay(now, s.imageSyncTime)
		wait := next.Sub(now)

		s.logger.Info("Next image sync", zap.Time("at", next), zap.Duration("in", wait))

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}

		rctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		s.logger.Info("Starting daily image sync with cleanup")
		if err := s.imageSync.SyncImagesWithCleanup(rctx); err != nil {
			s.logger.Error("Image sync failed", zap.Error(err))
		}
		cancel()
	}
}

// runImageSyncByInterval запускает синхронизацию с фиксированным интервалом.
func (s *Scheduler) runImageSyncByInterval(ctx context.Context) {
	time.Sleep(30 * time.Second) // даём серверу прогреться

	t := time.NewTicker(s.imageSyncInterval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			rctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			s.logger.Info("Starting periodic image sync with cleanup")
			if err := s.imageSync.SyncImagesWithCleanup(rctx); err != nil {
				s.logger.Error("Image sync failed", zap.Error(err))
			}
			cancel()
		}
	}
}

// nextTimeOfDay возвращает ближайший момент времени с заданным временем суток (HH:MM).
func nextTimeOfDay(from time.Time, timeStr string) time.Time {
	parts := strings.SplitN(timeStr, ":", 2)
	hour := 0
	minute := 0
	if len(parts) >= 1 {
		fmt.Sscanf(parts[0], "%d", &hour)
	}
	if len(parts) >= 2 {
		fmt.Sscanf(parts[1], "%d", &minute)
	}

	loc := from.Location()
	next := time.Date(from.Year(), from.Month(), from.Day(), hour, minute, 0, 0, loc)
	if !next.After(from) {
		next = next.Add(24 * time.Hour)
	}
	return next
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

	// Ждём освобождения блокировки до 2 минут (на случай, если ticker delta-sync захватил её первым).
	waitDeadline := time.Now().Add(2 * time.Minute)
	for !atomic.CompareAndSwapInt32(&s.syncRunning, 0, 1) {
		if time.Now().After(waitDeadline) {
			return fmt.Errorf("another sync is already running (waited 2m)")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	defer atomic.StoreInt32(&s.syncRunning, 0)

	s.logger.Info("Starting full product sync with worker pool")

	// Сначала обновляем кэш категорий (moysklad_uuid → DB id) — worker pool
	// будет читать его при сохранении каждого товара (SyncSingleProduct).
	s.syncService.RefreshCategories(ctx)

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

	// Разбиваем на батчи и отправляем в worker pool.
	// Worker'ы обрабатывают параллельно через семафор — батчи по 20 товаров.
	const batchSize = 20
	totalProducts := len(moyskladProducts)

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

	// Логируем статистику привязки товаров к категориям с задержкой,
	// чтобы worker pool успел обработать большую часть батчей.
	// Заодно — синхронизируем акции: они зависят от уже импортированных
	// товаров (по moysklad_id) и категорий (по folder UUID), которые к этому
	// моменту уже доступны.
	go func() {
		time.Sleep(2 * time.Minute)
		s.syncService.LogCategoryLinkStats()

		pctx, pcancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer pcancel()
		if _, err := s.syncService.SyncPromotions(pctx); err != nil {
			s.logger.Warn("Promotions sync failed", zap.Error(err))
		}
	}()

	return nil
}

func (s *Scheduler) Stop() {
	s.logger.Info("Stopping scheduler and worker pool")
	close(s.stopChan)
	if s.workerPool != nil {
		s.workerPool.Stop()
	}
}
