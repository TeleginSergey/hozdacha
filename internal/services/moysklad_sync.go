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

type MoyskladSyncService struct {
	moyskladClient *moysklad.Client
	productQuery   db.ProductQuery
	stockCache     *cache.StockCache
	stockBuffer    float64
	logger         *zap.Logger
}

func NewMoyskladSyncService(
	moyskladClient *moysklad.Client,
	productQuery db.ProductQuery,
	stockCache *cache.StockCache,
	stockBuffer float64,
	logger *zap.Logger,
) *MoyskladSyncService {
	return &MoyskladSyncService{
		moyskladClient: moyskladClient,
		productQuery:   productQuery,
		stockCache:     stockCache,
		stockBuffer:    stockBuffer,
		logger:         logger,
	}
}

type SyncResult struct {
	Created int `json:"created"`
	Updated int `json:"updated"`
	Errors  int `json:"errors"`
}

// SyncProducts выполняет полную синхронизацию товаров
func (s *MoyskladSyncService) SyncProducts(ctx context.Context) (*SyncResult, error) {
	return s.syncProducts(ctx, false, nil)
}

// SyncProductsDelta выполняет дельта-синхронизацию (только измененные товары)
func (s *MoyskladSyncService) SyncProductsDelta(ctx context.Context) (*SyncResult, error) {
	if s.stockCache == nil {
		// Если кэш не настроен, делаем полную синхронизацию
		return s.SyncProducts(ctx)
	}

	// Получаем время последней синхронизации
	lastSync, err := s.stockCache.GetLastSyncTime(ctx)
	if err != nil {
		s.logger.Warn("Failed to get last sync time, doing full sync", zap.Error(err))
		return s.SyncProducts(ctx)
	}

	var since *time.Time
	if lastSync != nil {
		since = lastSync
	} else {
		// Если нет времени последней синхронизации, синхронизируем за последний час
		t := time.Now().Add(-1 * time.Hour)
		since = &t
	}

	return s.syncProducts(ctx, true, since)
}

func (s *MoyskladSyncService) syncProducts(ctx context.Context, delta bool, since *time.Time) (*SyncResult, error) {
	// Защищаемся от паник
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("syncProducts panicked",
				zap.Any("panic", r),
				zap.Bool("delta", delta))
		}
	}()

	if s.moyskladClient == nil {
		return nil, fmt.Errorf("moysklad client not initialized")
	}

	// Берем остатки отдельным отчетом: entity/product не содержит stock.
	s.logger.Info("Getting stock report from Moysklad")
	stockMap, err := s.moyskladClient.GetStockReport(ctx)
	if err != nil {
		// Не делаем синк "в ноль" при проблемах с остатками — лучше упасть явно.
		s.logger.Error("Failed to get stock report from Moysklad", zap.Error(err))
		return nil, fmt.Errorf("failed to get stock report from Moysklad: %w", err)
	}
	s.logger.Info("Stock report retrieved", zap.Int("stock_count", len(stockMap)))

	var moyskladProducts []moysklad.MoyskladProduct
	if delta && since != nil {
		// Дельта-синхронизация
		s.logger.Info("Starting delta sync", zap.Time("since", *since))
		moyskladProducts, err = s.moyskladClient.GetProductsDelta(ctx, *since)
	} else {
		// Полная синхронизация
		s.logger.Info("Starting full sync")
		moyskladProducts, err = s.moyskladClient.GetProducts(ctx)
	}

	if err != nil {
		s.logger.Error("Failed to get products from Moysklad", zap.Error(err))
		return nil, fmt.Errorf("failed to get products from Moysklad: %w", err)
	}

	s.logger.Info("Products retrieved from Moysklad", zap.Int("total", len(moyskladProducts)))
	result := &SyncResult{}

	// Синхронизируем каждый товар с прогрессом - очень агрессивный размер батча
	batchSize := 5 // Очень маленький батч для минимальной нагрузки
	totalProducts := len(moyskladProducts)

	// Экстремально уменьшаем размер батча для больших объемов
	if totalProducts > 1000 {
		batchSize = 3
	}
	if totalProducts > 5000 {
		batchSize = 2
	}
	if totalProducts > 10000 {
		batchSize = 1 // По одному товару для максимальной стабильности
	}

	s.logger.Info("Starting ultra-optimized batch processing",
		zap.Int("batch_size", batchSize),
		zap.Int("total_products", totalProducts),
		zap.Int("estimated_batches", (totalProducts+batchSize-1)/batchSize))
	for i := 0; i < len(moyskladProducts); i += batchSize {
		end := i + batchSize
		if end > len(moyskladProducts) {
			end = len(moyskladProducts)
		}

		batch := moyskladProducts[i:end]
		progressPercent := float64(i+batchSize) * 100 / float64(totalProducts)
		s.logger.Info("Processing ultra-small batch",
			zap.Int("batch_start", i),
			zap.Int("batch_end", end),
			zap.Int("batch_size", len(batch)),
			zap.Float64("progress_percent", progressPercent),
			zap.Int("current_batch", (i/batchSize)+1),
			zap.Int("total_batches", (totalProducts+batchSize-1)/batchSize),
			zap.Int("total_products", len(moyskladProducts)))

		for _, msProduct := range batch {
			// Защищаемся от паник для каждого товара
			func() {
				defer func() {
					if r := recover(); r != nil {
						s.logger.Error("Product sync panicked",
							zap.Any("panic", r),
							zap.String("moysklad_id", msProduct.ID),
							zap.String("product_name", msProduct.Name))
						result.Errors++
					}
				}()

				// Проверяем, существует ли товар с таким moysklad_id
				existing, err := s.productQuery.GetByMoyskladID(ctx, msProduct.ID)
				if err != nil {
					s.logger.Warn("Failed to check existing product",
						zap.String("moysklad_id", msProduct.ID),
						zap.Error(err))
					result.Errors++
					return
				}

				// Преобразуем товар из МойСклад в наш формат
				now := time.Now()
				product := &db.Product{
					Name:        msProduct.Name,
					Description: msProduct.Description,
					MoyskladID:  &msProduct.ID,
					Active:      true, // Active = true для не удалённых товаров
					CreatedAt:   now,
					UpdatedAt:   now,
				}

				// Парсим цену (в МойСклад это массив salePrices, цена в копейках)
				if len(msProduct.SalePrices) > 0 && msProduct.SalePrices[0].Value > 0 {
					// Цена в МойСклад хранится в копейках, делим на 100 для получения рублей
					product.Price = msProduct.SalePrices[0].Value / 100.0
				} else {
					// Если цены нет, устанавливаем 0
					product.Price = 0
				}

				// Устанавливаем остаток (из вложенного объекта stock)
				stock := stockMap[msProduct.ID]

				if stock > 0 {
					product.Stock = int(stock)
				} else {
					// Если остаток не пришел, оставляем 0 (можно будет обновить через SyncStockOnly)
					product.Stock = 0
				}

				// Статус товара в зависимости от остатка
				if product.Stock > 0 {
					product.Status = "active"
				} else {
					// Оставляем карточку, но помечаем как out_of_stock
					product.Status = "out_of_stock"
				}

				// Обновляем время последнего updated из МойСклад, если доступно
				if msProduct.Updated != "" {
					if t, err := time.Parse(time.RFC3339Nano, msProduct.Updated); err == nil {
						product.LastSyncUpdated = &t
					}
				}

				// Устанавливаем изображение (берем первое изображение если есть)
				if msProduct.Images != nil && len(msProduct.Images.Rows) > 0 {
					imageURL := msProduct.Images.Rows[0].Meta.Href
					product.ImageURL = &imageURL
				}

				if existing == nil {
					// Создаем новый товар
					created, err := s.productQuery.Insert(ctx, product)
					if err != nil {
						s.logger.Warn("Failed to create product",
							zap.String("moysklad_id", msProduct.ID),
							zap.Error(err))
						result.Errors++
						return
					}
					result.Created++

					// Кэшируем остаток
					if s.stockCache != nil && created != nil {
						s.stockCache.SetStock(ctx, created.ID, msProduct.ID, created.Stock)
					}

					s.logger.Info("Product created from Moysklad",
						zap.String("moysklad_id", msProduct.ID),
						zap.String("name", msProduct.Name))
				} else {
					// Обновляем существующий товар
					product.ID = existing.ID
					// Устанавливаем UpdatedAt для обновления
					product.UpdatedAt = time.Now()
					updated, err := s.productQuery.Update(ctx, product, existing.ID)
					if err != nil {
						s.logger.Warn("Failed to update product",
							zap.String("moysklad_id", msProduct.ID),
							zap.Error(err))
						result.Errors++
						return
					}
					result.Updated++

					// Обновляем кэш остатка
					if s.stockCache != nil && updated != nil {
						s.stockCache.SetStock(ctx, updated.ID, msProduct.ID, updated.Stock)
					}

					s.logger.Info("Product updated from Moysklad",
						zap.String("moysklad_id", msProduct.ID),
						zap.String("name", msProduct.Name))
				}
			}()
		}

		// Экстремально длинная пауза между батчами для максимальной стабильности
		if i+batchSize < len(moyskladProducts) {
			// Очень длинные паузы для больших объемов данных
			pauseDuration := 2 * time.Second // 2 секунды по умолчанию
			if len(moyskladProducts) > 1000 {
				pauseDuration = 3 * time.Second
			}
			if len(moyskladProducts) > 5000 {
				pauseDuration = 5 * time.Second // 5 секунд для 4000+ товаров
			}
			if len(moyskladProducts) > 10000 {
				pauseDuration = 10 * time.Second // 10 секунд для очень больших объемов
			}

			s.logger.Info("Long pause between batches for stability",
				zap.Duration("pause", pauseDuration),
				zap.Int("processed", i+batchSize),
				zap.Int("total", len(moyskladProducts)),
				zap.Float64("progress_percent", float64(i+batchSize)*100/float64(len(moyskladProducts))))

			time.Sleep(pauseDuration)
		}
	}

	// Сохраняем время последней синхронизации
	if s.stockCache != nil {
		s.stockCache.SetLastSyncTime(ctx, time.Now())
	}

	// Если остатки не пришли, пытаемся синхронизировать их отдельно
	// (это может быть медленно для большого количества товаров, поэтому опционально)
	if result.Created > 0 || result.Updated > 0 {
		s.logger.Info("Syncing stock separately if needed")
		// Можно вызвать SyncStockOnly, но это может быть долго для 1000+ товаров
		// Лучше делать это отдельной задачей или по требованию
	}

	return result, nil
}

// SyncStockOnly синхронизирует только остатки (быстрее чем полная синхронизация)
func (s *MoyskladSyncService) SyncStockOnly(ctx context.Context) error {
	if s.moyskladClient == nil {
		return fmt.Errorf("moysklad client not initialized")
	}

	if s.stockCache == nil {
		return fmt.Errorf("stock cache not initialized")
	}

	// Получаем отчет по остаткам
	stockMap, err := s.moyskladClient.GetStockReport(ctx)
	if err != nil {
		return fmt.Errorf("failed to get stock report: %w", err)
	}

	// Обновляем кэш для каждого товара
	for moyskladID, stock := range stockMap {
		product, err := s.productQuery.GetByMoyskladID(ctx, moyskladID)
		if err != nil {
			s.logger.Warn("Failed to get product by moysklad_id",
				zap.String("moysklad_id", moyskladID),
				zap.Error(err))
			continue
		}

		if product != nil {
			// Обновляем остаток в БД
			product.Stock = int(stock)
			// Обновляем статус по остатку
			if product.Stock > 0 {
				product.Status = "active"
			} else {
				product.Status = "out_of_stock"
			}
			_, err = s.productQuery.Update(ctx, product, product.ID)
			if err != nil {
				s.logger.Warn("Failed to update product stock",
					zap.String("moysklad_id", moyskladID),
					zap.Error(err))
				continue
			}

			// Обновляем кэш
			s.stockCache.SetStock(ctx, product.ID, moyskladID, product.Stock)
		}
	}

	// Сохраняем время последней синхронизации
	s.stockCache.SetLastSyncTime(ctx, time.Now())

	s.logger.Info("Stock sync completed", zap.Int("products_updated", len(stockMap)))
	return nil
}
