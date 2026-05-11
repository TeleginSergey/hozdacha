package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

type SyncTask struct {
	ID         string
	Type       string // "full", "delta", "batch"
	Products   []MoyskladProduct
	BatchIndex int
	TotalBatches int
}

type SyncWorkerPool struct {
	syncService *MoyskladSyncService
	logger       *zap.Logger
	taskQueue    chan SyncTask
	workers     int
	semaphore    chan struct{}
	stopChan     chan struct{}
	wg          sync.WaitGroup
}

func NewSyncWorkerPool(syncService *MoyskladSyncService, workers int, logger *zap.Logger) *SyncWorkerPool {
	return &SyncWorkerPool{
		syncService: syncService,
		logger:       logger,
		taskQueue:    make(chan SyncTask, 100), // Буфер на 100 задач
		workers:     workers,
		semaphore:    make(chan struct{}, workers), // Ограничение параллелизма
		stopChan:     make(chan struct{}),
	}
}

func (p *SyncWorkerPool) Start(ctx context.Context) {
	p.logger.Info("Starting sync worker pool", zap.Int("workers", p.workers))
	
	// Запускаем worker'ов
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(ctx, i)
	}
	
	// Ожидаем сигнала остановки
	<-ctx.Done()
	p.Stop()
}

func (p *SyncWorkerPool) Stop() {
	p.logger.Info("Stopping sync worker pool")
	close(p.stopChan)
	close(p.taskQueue)
	p.wg.Wait()
}

func (p *SyncWorkerPool) PushTask(task SyncTask) {
	select {
	case p.taskQueue <- task:
		p.logger.Debug("Task queued", 
			zap.String("task_id", task.ID),
			zap.String("type", task.Type),
			zap.Int("batch_index", task.BatchIndex))
	default:
		p.logger.Warn("Task queue full, dropping task", 
			zap.String("task_id", task.ID))
	}
}

func (p *SyncWorkerPool) worker(ctx context.Context, workerID int) {
	defer p.wg.Done()
	
	p.logger.Info("Worker started", zap.Int("worker_id", workerID))
	
	for {
		select {
		case <-ctx.Done():
			p.logger.Info("Worker stopped by context", zap.Int("worker_id", workerID))
			return
			
		case <-p.stopChan:
			p.logger.Info("Worker stopped by signal", zap.Int("worker_id", workerID))
			return
			
		case task, ok := <-p.taskQueue:
			if !ok {
				p.logger.Info("Task queue closed, worker exiting", zap.Int("worker_id", workerID))
				return
			}
			p.processTask(ctx, task, workerID)
		}
	}
}

func (p *SyncWorkerPool) processTask(ctx context.Context, task SyncTask, workerID int) {
	// Получаем слот в семафоре (ограничение параллелизма)
	p.semaphore <- struct{}{}
	defer func() { <-p.semaphore }()
	
	// Timeout для обработки одного батча
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	
	p.logger.Info("Processing batch",
		zap.String("task_id", task.ID),
		zap.String("type", task.Type),
		zap.Int("worker_id", workerID),
		zap.Int("batch_index", task.BatchIndex),
		zap.Int("total_batches", task.TotalBatches),
		zap.Int("products_count", len(task.Products)))
	
	// Защита от паник
	defer func() {
		if r := recover(); r != nil {
			p.logger.Error("Batch processing panicked",
				zap.Any("panic", r),
				zap.String("task_id", task.ID),
				zap.Int("batch_index", task.BatchIndex),
				zap.Int("worker_id", workerID))
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
		zap.Int("worker_id", workerID))
}
