package services

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/cache"
	"github.com/TeleginSergey/hozdacha/internal/db"
	"github.com/TeleginSergey/hozdacha/internal/moysklad"
)

// ReservationExpiryService периодически отменяет pending-заказы, у которых истёк TTL брони:
//   - помечает заказ как expired
//   - возвращает товары на склад в БД (атомарно с пунктом выше)
//   - удаляет CustomerOrder в МойСклад
//   - обновляет Redis-кэш остатков
//
// Запускается одной горутиной — несколько инстансов сервиса безопасно работают параллельно
// благодаря FOR UPDATE SKIP LOCKED в ExpireOrderAtomic.
type ReservationExpiryService struct {
	orderQuery     db.OrderQuery
	moyskladClient *moysklad.Client
	stockCache     *cache.StockCache
	logger         *zap.Logger

	interval  time.Duration
	batchSize int
}

func NewReservationExpiryService(
	orderQuery db.OrderQuery,
	moyskladClient *moysklad.Client,
	stockCache *cache.StockCache,
	interval time.Duration,
	batchSize int,
	logger *zap.Logger,
) *ReservationExpiryService {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	if batchSize <= 0 {
		batchSize = 100
	}
	return &ReservationExpiryService{
		orderQuery:     orderQuery,
		moyskladClient: moyskladClient,
		stockCache:     stockCache,
		logger:         logger,
		interval:       interval,
		batchSize:      batchSize,
	}
}

// Run блокирует горутину до отмены ctx.
func (s *ReservationExpiryService) Run(ctx context.Context) {
	s.logger.Info("Reservation expiry service started",
		zap.Duration("interval", s.interval),
		zap.Int("batch_size", s.batchSize))

	// Первый прогон сразу, чтобы не ждать interval после рестарта.
	s.tick(ctx)

	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			s.logger.Info("Reservation expiry service stopped")
			return
		case <-t.C:
			s.tick(ctx)
		}
	}
}

func (s *ReservationExpiryService) tick(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("reservation expiry tick panicked", zap.Any("panic", r))
		}
	}()

	ids, err := s.orderQuery.ListExpiredPendingIDs(ctx, time.Now(), s.batchSize)
	if err != nil {
		s.logger.Error("Failed to list expired orders", zap.Error(err))
		return
	}
	if len(ids) == 0 {
		return
	}

	s.logger.Info("Expiring reservations", zap.Int("count", len(ids)))

	expiredCount := 0
	for _, id := range ids {
		if err := s.expireOne(ctx, id); err != nil {
			s.logger.Error("Failed to expire order", zap.Int64("order_id", id), zap.Error(err))
			continue
		}
		expiredCount++
	}

	s.logger.Info("Reservation expiry batch finished",
		zap.Int("total", len(ids)),
		zap.Int("expired", expiredCount))
}

func (s *ReservationExpiryService) expireOne(ctx context.Context, orderID int64) error {
	moyskladID, alreadyHandled, restored, err := s.orderQuery.ExpireOrderAtomic(ctx, orderID)
	if err != nil {
		return fmt.Errorf("expire order atomic: %w", err)
	}
	if alreadyHandled {
		return nil
	}

	// Обновляем Redis-кэш остатков (best effort).
	if s.stockCache != nil {
		for _, p := range restored {
			msID := ""
			if p.MoyskladID != nil {
				msID = *p.MoyskladID
			}
			if err := s.stockCache.SetStock(ctx, p.ID, msID, p.Stock); err != nil {
				s.logger.Warn("Failed to refresh stock cache after expiry",
					zap.Int64("product_id", p.ID), zap.Error(err))
			}
		}
	}

	// Удаляем заказ в МойСклад (best effort: если не удалось, сохраняем заказ в БД,
	// чтобы админ мог увидеть и удалить вручную через дашборд МойСклад).
	if moyskladID != nil && *moyskladID != "" && s.moyskladClient != nil {
		delCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		if err := s.moyskladClient.DeleteCustomerOrder(delCtx, *moyskladID); err != nil {
			s.logger.Warn("Failed to delete order in Moysklad",
				zap.Int64("order_id", orderID),
				zap.Stringp("moysklad_id", moyskladID),
				zap.Error(err))
			// Не возвращаем ошибку — БД уже консистентна.
		}
	}

	return nil
}
