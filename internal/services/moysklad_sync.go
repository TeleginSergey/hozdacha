package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/cache"
	"github.com/TeleginSergey/hozdacha/internal/db"
	"github.com/TeleginSergey/hozdacha/internal/moysklad"
)

type MoyskladSyncService struct {
	moyskladClient *moysklad.Client
	productQuery   db.ProductQuery
	categoryQuery  *db.CategoryQuery
	stockCache     *cache.StockCache
	stockBuffer    float64
	logger         *zap.Logger
}

func NewMoyskladSyncService(
	moyskladClient *moysklad.Client,
	productQuery db.ProductQuery,
	categoryQuery *db.CategoryQuery,
	stockCache *cache.StockCache,
	stockBuffer float64,
	logger *zap.Logger,
) *MoyskladSyncService {
	return &MoyskladSyncService{
		moyskladClient: moyskladClient,
		productQuery:   productQuery,
		categoryQuery:  categoryQuery,
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

	// Для дельта-синхронизации не тянем stock report — webhook'и уже обновляют сток
	// в реальном времени. Stock report нужен только для полной синхронизации.
	var stockMap map[string]float64
	if !delta {
		s.logger.Info("Full sync: getting stock report from Moysklad")
		var err error
		stockMap, err = s.moyskladClient.GetStockReport(ctx)
		if err != nil {
			s.logger.Error("Failed to get stock report from Moysklad", zap.Error(err))
			return nil, fmt.Errorf("failed to get stock report from Moysklad: %w", err)
		}
		s.logger.Info("Stock report retrieved", zap.Int("stock_count", len(stockMap)))
	}

	var moyskladProducts []moysklad.MoyskladProduct
	var err error
	if delta && since != nil {
		s.logger.Info("Starting delta sync", zap.Time("since", *since))
		moyskladProducts, err = s.moyskladClient.GetProductsDelta(ctx, *since)
	} else {
		s.logger.Info("Starting full sync")
		moyskladProducts, err = s.moyskladClient.GetProducts(ctx)
	}

	if err != nil {
		if delta && since != nil && errors.Is(err, moysklad.ErrResyncRequired) {
			s.logger.Warn("Delta sync rejected by Moysklad, falling back to full product list", zap.Error(err))
			moyskladProducts, err = s.moyskladClient.GetProducts(ctx)
		}
	}

	if err != nil {
		s.logger.Error("Failed to get products from Moysklad", zap.Error(err))
		return nil, fmt.Errorf("failed to get products from Moysklad: %w", err)
	}

	s.logger.Info("Products retrieved from Moysklad", zap.Int("total", len(moyskladProducts)))

	// Загружаем группы товаров (категории) из отдельного endpoint /entity/productfolder.
	// Строим карту UUID→name для быстрого поиска при обработке каждого товара.
	folderNameByID := make(map[string]string)
	if s.categoryQuery == nil {
		s.logger.Warn("categoryQuery is nil — categories will not be synced")
	} else {
		folders, fErr := s.moyskladClient.GetProductFolders(ctx)
		if fErr != nil {
			s.logger.Warn("Failed to fetch product folders, categories will not be synced", zap.Error(fErr))
		} else {
			for _, f := range folders {
				folderNameByID[f.ID] = f.Name
			}
			s.logger.Info("Product folders loaded from Moysklad",
				zap.Int("count", len(folderNameByID)))
			// Покажем первые несколько для отладки.
			if len(folders) > 0 {
				sample := folders
				if len(sample) > 5 {
					sample = sample[:5]
				}
				for _, f := range sample {
					s.logger.Info("Folder sample",
						zap.String("id", f.ID),
						zap.String("name", f.Name),
						zap.String("path", f.PathName))
				}
			}
		}
	}

	// Счётчики для диагностики синхронизации категорий.
	var (
		productsWithFolder int32
		folderLookupHits   int32
		folderLookupMisses int32
		categoriesUpserted int32
	)

	result := &SyncResult{}

	// Разумные батчи: 20 товаров за раз, пауза 100ms между батчами.
	// МС API даёт 800 запросов/минуту — мы используем малую долю.
	const batchSize = 20
	const pauseBetweenBatches = 100 * time.Millisecond

	for i := 0; i < len(moyskladProducts); i += batchSize {
		end := i + batchSize
		if end > len(moyskladProducts) {
			end = len(moyskladProducts)
		}

		batch := moyskladProducts[i:end]
		for _, msProduct := range batch {
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

				existing, err := s.productQuery.GetByMoyskladID(ctx, msProduct.ID)
				if err != nil {
					s.logger.Warn("Failed to check existing product",
						zap.String("moysklad_id", msProduct.ID),
						zap.Error(err))
					result.Errors++
					return
				}

				now := time.Now()
				product := &db.Product{
					Name:        msProduct.Name,
					Description: msProduct.Description,
					MoyskladID:  &msProduct.ID,
					Active:      true,
					CreatedAt:   now,
					UpdatedAt:   now,
				}

				if len(msProduct.SalePrices) > 0 && msProduct.SalePrices[0].Value > 0 {
					product.Price = msProduct.SalePrices[0].Value / 100.0
				}

				// Сток: для full sync из stockMap, для delta — не трогаем (webhook'и обновляют).
				if !delta && stockMap != nil {
					if stock, ok := stockMap[msProduct.ID]; ok && stock > 0 {
						product.Stock = int(stock)
					}
				} else if existing != nil {
					// Сохраняем существующий сток (webhook'и его уже обновили).
					product.Stock = existing.Stock
				}

				if product.Stock > 0 {
					product.Status = "active"
				} else {
					product.Status = "out_of_stock"
				}

				// Синхронизируем группу товара → categories через folderNameByID.
				if s.categoryQuery != nil && msProduct.ProductFolder != nil && msProduct.ProductFolder.Meta.Href != "" {
					atomic.AddInt32(&productsWithFolder, 1)
					// href вида https://.../entity/productfolder/UUID — берём последний сегмент.
					href := strings.TrimSuffix(msProduct.ProductFolder.Meta.Href, "/")
					parts := strings.Split(href, "/")
					folderUUID := parts[len(parts)-1]
					if folderName, ok := folderNameByID[folderUUID]; ok && folderName != "" {
						atomic.AddInt32(&folderLookupHits, 1)
						catID, catErr := s.categoryQuery.UpsertByName(ctx, folderName)
						if catErr != nil {
							s.logger.Warn("Failed to upsert category",
								zap.String("folder", folderName),
								zap.Error(catErr))
						} else {
							product.CategoryID = &catID
							atomic.AddInt32(&categoriesUpserted, 1)
						}
					} else {
						atomic.AddInt32(&folderLookupMisses, 1)
						s.logger.Debug("Folder UUID not found in map",
							zap.String("uuid", folderUUID),
							zap.String("href", msProduct.ProductFolder.Meta.Href))
					}
				}
				if product.CategoryID == nil && existing != nil {
					product.CategoryID = existing.CategoryID
				}

				if msProduct.Updated != "" {
					if t, err := time.Parse(time.RFC3339Nano, msProduct.Updated); err == nil {
						product.LastSyncUpdated = &t
					}
				}

				// Сохраняем существующий image_url (локальные скачанные изображения).
				// ImageSyncService отвечает за загрузку и обновление изображений независимо.
				if existing != nil && existing.ImageURL != nil {
					product.ImageURL = existing.ImageURL
				} else if msProduct.Images != nil && len(msProduct.Images.Rows) > 0 {
					imageURL := msProduct.Images.Rows[0].Meta.Href
					product.ImageURL = &imageURL
				}

				if existing == nil {
					created, err := s.productQuery.Insert(ctx, product)
					if err != nil {
						s.logger.Warn("Failed to create product",
							zap.String("moysklad_id", msProduct.ID),
							zap.Error(err))
						result.Errors++
						return
					}
					result.Created++
					if s.stockCache != nil && created != nil {
						s.stockCache.SetStock(ctx, created.ID, msProduct.ID, created.Stock)
					}
				} else {
					product.ID = existing.ID
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
					if s.stockCache != nil && updated != nil {
						s.stockCache.SetStock(ctx, updated.ID, msProduct.ID, updated.Stock)
					}
				}
			}()
		}

		// Короткая пауза между батчами чтобы не давить на API МС.
		if i+batchSize < len(moyskladProducts) {
			time.Sleep(pauseBetweenBatches)
		}
	}

	// Сохраняем время последней синхронизации для следующей дельты.
	if s.stockCache != nil {
		s.stockCache.SetLastSyncTime(ctx, time.Now())
	}

	s.logger.Info("Sync completed",
		zap.Int("created", result.Created),
		zap.Int("updated", result.Updated),
		zap.Int("errors", result.Errors),
		zap.Bool("delta", delta))

	s.logger.Info("Category sync stats",
		zap.Int("folders_loaded", len(folderNameByID)),
		zap.Int32("products_with_folder", atomic.LoadInt32(&productsWithFolder)),
		zap.Int32("folder_lookup_hits", atomic.LoadInt32(&folderLookupHits)),
		zap.Int32("folder_lookup_misses", atomic.LoadInt32(&folderLookupMisses)),
		zap.Int32("categories_upserted", atomic.LoadInt32(&categoriesUpserted)))

	return result, nil
}

// GetProductsForSync получает товары для синхронизации (полная или дельта)
func (s *MoyskladSyncService) GetProductsForSync(ctx context.Context, delta bool, since *time.Time) ([]moysklad.MoyskladProduct, error) {
	var moyskladProducts []moysklad.MoyskladProduct
	var err error

	if delta && since != nil {
		// Дельта-синхронизация
		s.logger.Info("Getting delta products from Moysklad", zap.Time("since", *since))
		moyskladProducts, err = s.moyskladClient.GetProductsDelta(ctx, *since)
	} else {
		// Полная синхронизация
		s.logger.Info("Getting all products from Moysklad")
		moyskladProducts, err = s.moyskladClient.GetProducts(ctx)
	}

	if err != nil {
		s.logger.Error("Failed to get products from Moysklad", zap.Error(err))
		return nil, fmt.Errorf("failed to get products from Moysklad: %w", err)
	}

	return moyskladProducts, nil
}

func (s *MoyskladSyncService) GetStockMapForSync(ctx context.Context) (map[string]float64, error) {
	if s.moyskladClient == nil {
		return nil, fmt.Errorf("moysklad client not initialized")
	}

	stockMap, err := s.moyskladClient.GetStockReport(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get stock report from Moysklad: %w", err)
	}

	return stockMap, nil
}

// getPrice извлекает цену из MoyskladProduct
func getPrice(product moysklad.MoyskladProduct) float64 {
	if len(product.SalePrices) > 0 {
		// Берем первую цену (обычно базовая цена)
		return product.SalePrices[0].Value / 100.0 // Конвертируем из копеек в рубли
	}
	return 0.0
}

// getStock извлекает остаток из MoyskladProduct
func getStock(product moysklad.MoyskladProduct) int {
	if product.Stock != nil {
		return int(product.Stock.Stock)
	}
	return 0
}

// SyncSingleProduct синхронизирует один продукт с timeout
func (s *MoyskladSyncService) SyncSingleProduct(ctx context.Context, product moysklad.MoyskladProduct) error {
	// Проверяем существует ли товар
	existingProduct, err := s.productQuery.GetByMoyskladID(ctx, product.ID)
	if err != nil {
		return fmt.Errorf("failed to check existing product: %w", err)
	}

	if existingProduct != nil {
		// Обновляем существующий товар
		stock := getStock(product)
		var status string

		if stock > 0 {
			status = "active"
		} else {
			status = "out_of_stock"
		}

		updated := &db.Product{
			ID:          existingProduct.ID,
			MoyskladID:  &product.ID,
			Name:        product.Name,
			Description: product.Description,
			Price:       getPrice(product),
			Stock:       stock,
			Status:      status,
			Active:      true,
			UpdatedAt:   time.Now(),
			ImageURL:    existingProduct.ImageURL, // Сохраняем существующий image_url
		}

		if _, err := s.productQuery.Update(ctx, updated, existingProduct.ID); err != nil {
			return fmt.Errorf("failed to update product: %w", err)
		}

		// Обновляем кэш остатков
		if s.stockCache != nil {
			s.stockCache.SetStock(ctx, updated.ID, product.ID, updated.Stock)
		}

		s.logger.Debug("Product updated from Moysklad",
			zap.String("moysklad_id", product.ID),
			zap.String("name", product.Name),
			zap.String("status", status))
	} else {
		// Создаем новый товар
		stock := getStock(product)
		var status string

		if stock > 0 {
			status = "active"
		} else {
			status = "out_of_stock"
		}

		newProduct := &db.Product{
			MoyskladID:  &product.ID,
			Name:        product.Name,
			Description: product.Description,
			Price:       getPrice(product),
			Stock:       stock,
			Status:      status, // Добавляем статус
			Active:      true,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}

		if _, err := s.productQuery.Insert(ctx, newProduct); err != nil {
			return fmt.Errorf("failed to create product: %w", err)
		}

		// Устанавливаем кэш остатков
		if s.stockCache != nil {
			s.stockCache.SetStock(ctx, newProduct.ID, product.ID, newProduct.Stock)
		}

		s.logger.Debug("Product created from Moysklad",
			zap.String("moysklad_id", product.ID),
			zap.String("name", product.Name),
			zap.String("status", status))
	}

	return nil
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
