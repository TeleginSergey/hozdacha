package services

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

type SyncTask struct {
	ID           string
	Type         string // "full", "delta", "batch", "preload"
	Products     []MoyskladProduct
	BatchIndex   int
	TotalBatches int
	Priority     int // 1=высокий, 2=средний, 3=низкий
}

type WorkerStats struct {
	TotalTasks     int64
	ProcessedTasks int64
	FailedTasks    int64
	ActiveWorkers  int32
	QueuedTasks    int32
}

type SyncWorkerPool struct {
	syncService *MoyskladSyncService
	logger      *zap.Logger
	taskQueue   chan SyncTask
	workers     int32
	maxWorkers  int32
	semaphore   chan struct{}
	stopChan    chan struct{}
	wg          sync.WaitGroup
	stats       *WorkerStats
	ctx         context.Context
	cancel      context.CancelFunc
}

func NewSyncWorkerPool(syncService *MoyskladSyncService, workers int, logger *zap.Logger) *SyncWorkerPool {
	ctx, cancel := context.WithCancel(context.Background())

	// Определяем максимальное количество воркеров на основе CPU
	maxWorkers := int32(workers)
	if maxWorkers > int32(runtime.NumCPU()) {
		maxWorkers = int32(runtime.NumCPU())
	}

	return &SyncWorkerPool{
		syncService: syncService,
		logger:      logger,
		taskQueue:   make(chan SyncTask, 200), // Увеличенный буфер
		workers:     0,
		maxWorkers:  maxWorkers,
		semaphore:   make(chan struct{}, maxWorkers),
		stopChan:    make(chan struct{}),
		wg:          sync.WaitGroup{},
		stats:       &WorkerStats{},
		ctx:         ctx,
		cancel:      cancel,
	}
}

func (p *SyncWorkerPool) Start(ctx context.Context) {
	p.logger.Info("Starting dynamic sync worker pool",
		zap.Int32("max_workers", p.maxWorkers),
		zap.Int("cpu_cores", runtime.NumCPU()))

	// Запускаем воркеров динамически по мере необходимости
	go p.workerManager(ctx)

	// Запускаем мониторинг статистики
	go p.statsMonitor(ctx)

	// Ожидаем сигнала остановки
	<-ctx.Done()
	p.Stop()
}

func (p *SyncWorkerPool) Stop() {
	p.logger.Info("Stopping sync worker pool")
	p.cancel() // Отменяем контекст
	close(p.stopChan)
	close(p.taskQueue)
	p.wg.Wait()
}

// workerManager управляет количеством воркеров динамически
func (p *SyncWorkerPool) workerManager(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.adjustWorkerCount()
		}
	}
}

// adjustWorkerCount динамически регулирует количество воркеров
func (p *SyncWorkerPool) adjustWorkerCount() {
	queuedTasks := int(atomic.LoadInt32(&p.stats.QueuedTasks))
	activeWorkers := atomic.LoadInt32(&p.stats.ActiveWorkers)

	// Если много задач в очереди и есть свободные слоты - добавляем воркеров
	if queuedTasks > 10 && activeWorkers < p.maxWorkers {
		p.startWorker()
	}

	// Если мало задач и много воркеров - останавливаем лишних
	if queuedTasks < 5 && activeWorkers > 1 {
		p.stopWorker()
	}
}

// startWorker запускает нового воркера
func (p *SyncWorkerPool) startWorker() {
	if atomic.LoadInt32(&p.workers) >= p.maxWorkers {
		return
	}

	atomic.AddInt32(&p.workers, 1)
	atomic.AddInt32(&p.stats.ActiveWorkers, 1)

	p.wg.Add(1)
	go func() {
		defer func() {
			atomic.AddInt32(&p.workers, -1)
			atomic.AddInt32(&p.stats.ActiveWorkers, -1)
			p.wg.Done()
		}()

		workerID := atomic.LoadInt32(&p.workers)
		p.worker(p.ctx, int(workerID))
	}()

	p.logger.Info("Started new worker",
		zap.Int32("total_workers", p.workers),
		zap.Int32("active_workers", p.stats.ActiveWorkers))
}

// stopWorker останавливает одного воркера (путем отправки специальной задачи)
func (p *SyncWorkerPool) stopWorker() {
	if atomic.LoadInt32(&p.workers) <= 1 {
		return
	}

	// Отправляем специальную задачу для остановки
	task := SyncTask{
		ID:       "stop-worker",
		Type:     "stop",
		Priority: 1,
	}

	select {
	case p.taskQueue <- task:
	default:
		// Очередь переполнена, игнорируем
	}
}

// statsMonitor отслеживает и логирует статистику
func (p *SyncWorkerPool) statsMonitor(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.logStats()
		}
	}
}

// logStats выводит текущую статистику
func (p *SyncWorkerPool) logStats() {
	total := atomic.LoadInt64(&p.stats.TotalTasks)
	processed := atomic.LoadInt64(&p.stats.ProcessedTasks)
	failed := atomic.LoadInt64(&p.stats.FailedTasks)
	active := atomic.LoadInt32(&p.stats.ActiveWorkers)
	queued := atomic.LoadInt32(&p.stats.QueuedTasks)

	p.logger.Info("Worker pool stats",
		zap.Int64("total_tasks", total),
		zap.Int64("processed_tasks", processed),
		zap.Int64("failed_tasks", failed),
		zap.Int32("active_workers", active),
		zap.Int32("queued_tasks", queued),
		zap.Float64("success_rate", float64(processed)/float64(total)*100))
}

func (p *SyncWorkerPool) PushTask(task SyncTask) {
	atomic.AddInt64(&p.stats.TotalTasks, 1)
	atomic.AddInt32(&p.stats.QueuedTasks, 1)

	select {
	case p.taskQueue <- task:
		p.logger.Debug("Task queued",
			zap.String("task_id", task.ID),
			zap.String("type", task.Type),
			zap.Int("batch_index", task.BatchIndex),
			zap.Int("priority", task.Priority))
	default:
		atomic.AddInt32(&p.stats.QueuedTasks, -1)
		atomic.AddInt64(&p.stats.FailedTasks, 1)
		p.logger.Warn("Task queue full, dropping task",
			zap.String("task_id", task.ID))
	}
}

// GetStats возвращает текущую статистику воркеров
func (p *SyncWorkerPool) GetStats() WorkerStats {
	return WorkerStats{
		TotalTasks:     atomic.LoadInt64(&p.stats.TotalTasks),
		ProcessedTasks: atomic.LoadInt64(&p.stats.ProcessedTasks),
		FailedTasks:    atomic.LoadInt64(&p.stats.FailedTasks),
		ActiveWorkers:  atomic.LoadInt32(&p.stats.ActiveWorkers),
		QueuedTasks:    atomic.LoadInt32(&p.stats.QueuedTasks),
	}
}

func (p *SyncWorkerPool) worker(ctx context.Context, workerID int) {
	defer func() {
		p.logger.Info("Worker stopped", zap.Int("worker_id", workerID))
	}()

	p.logger.Info("Worker started", zap.Int("worker_id", workerID))

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("Worker stopped by context", zap.Int("worker_id", workerID))
			return

		case task, ok := <-p.taskQueue:
			if !ok {
				p.logger.Info("Task queue closed, worker exiting", zap.Int("worker_id", workerID))
				return
			}

			// Обработка специальной задачи остановки
			if task.Type == "stop" {
				p.logger.Info("Worker received stop signal", zap.Int("worker_id", workerID))
				return
			}

			p.processTask(ctx, task, workerID)
		}
	}
}

func (p *SyncWorkerPool) processTask(ctx context.Context, task SyncTask, workerID int) {
	// Получаем слот в семафоре (ограничение параллелизма)
	p.semaphore <- struct{}{}
	defer func() {
		<-p.semaphore
		atomic.AddInt32(&p.stats.QueuedTasks, -1)
	}()

	// Timeout для обработки одного батча
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	p.logger.Info("Processing batch",
		zap.String("task_id", task.ID),
		zap.String("type", task.Type),
		zap.Int("worker_id", workerID),
		zap.Int("batch_index", task.BatchIndex),
		zap.Int("total_batches", task.TotalBatches),
		zap.Int("products_count", len(task.Products)),
		zap.Int("priority", task.Priority))

	// Защита от паник и учет статистики
	success := true
	defer func() {
		if r := recover(); r != nil {
			success = false
			p.logger.Error("Batch processing panicked",
				zap.Any("panic", r),
				zap.String("task_id", task.ID),
				zap.Int("batch_index", task.BatchIndex),
				zap.Int("worker_id", workerID))
		}

		if success {
			atomic.AddInt64(&p.stats.ProcessedTasks, 1)
		} else {
			atomic.AddInt64(&p.stats.FailedTasks, 1)
		}
	}()

	// Обрабатываем батч продуктов
	for _, product := range task.Products {
		select {
		case <-ctx.Done():
			p.logger.Info("Batch processing cancelled",
				zap.String("task_id", task.ID),
				zap.Int("batch_index", task.BatchIndex))
			return
		default:
			// Timeout для каждого API запроса
			apiCtx, apiCancel := context.WithTimeout(ctx, 30*time.Second)

			err := p.syncService.SyncSingleProduct(apiCtx, product)
			apiCancel()

			if err != nil {
				success = false
				p.logger.Error("Failed to sync product",
					zap.Error(err),
					zap.String("product_id", product.ID),
					zap.String("product_name", product.Name),
					zap.String("task_id", task.ID),
					zap.Int("batch_index", task.BatchIndex),
					zap.Int("worker_id", workerID))
			}
		}
	}

	p.logger.Info("Batch completed",
		zap.String("task_id", task.ID),
		zap.Int("batch_index", task.BatchIndex),
		zap.Int("worker_id", workerID),
		zap.Bool("success", success))
}
