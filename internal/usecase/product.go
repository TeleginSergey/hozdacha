package usecase

import (
	"context"

	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/cache"
	"github.com/TeleginSergey/hozdacha/internal/db"
)

// ProductRepository описывает минимальный контракт, который нужен бизнес-логике продуктов.
type ProductRepository interface {
	GetByID(ctx context.Context, id int64) (*db.Product, error)
	GetByIDs(ctx context.Context, ids []int64) ([]*db.Product, error)
	GetByMoyskladID(ctx context.Context, moyskladID string) (*db.Product, error)
	GetAll(ctx context.Context, limit, offset int) ([]*db.Product, error)
	GetActive(ctx context.Context, limit, offset int) ([]*db.Product, error)
	Search(ctx context.Context, query string, categoryID *int64, limit, offset int) ([]*db.Product, error)
	GetByCategory(ctx context.Context, categoryID int64, limit, offset int) ([]*db.Product, error)
	CountActive(ctx context.Context) (int, error)
	CountByCategory(ctx context.Context, categoryID int64) (int, error)
	CountSearch(ctx context.Context, query string, categoryID *int64) (int, error)
	Insert(ctx context.Context, product *db.Product) (*db.Product, error)
	Update(ctx context.Context, product *db.Product, id int64) (*db.Product, error)
	Delete(ctx context.Context, id int64) error
}

type ProductUsecase struct {
	repo       ProductRepository
	stockCache interface {
		GetAvailableStock(ctx context.Context, productID int64, productQuery db.ProductQuery) (int, error)
		BatchGetAvailableStocks(ctx context.Context, productIDs []int64, productQuery db.ProductQuery) (map[int64]int, error)
		SetStockToCache(ctx context.Context, productID int64, stock int) error
		InvalidateStockCache(ctx context.Context, productID int64) error
		BatchSetStocks(ctx context.Context, products []*db.Product) error
	}
	stockBuffer float64
	pricer      *PromotionPricer
	logger      *zap.Logger
}

func NewProductUsecase(
	repo ProductRepository,
	stockCache *cache.StockCache,
	stockBuffer float64,
	logger *zap.Logger,
) *ProductUsecase {
	return &ProductUsecase{
		repo:        repo,
		stockCache:  stockCache,
		stockBuffer: stockBuffer,
		logger:      logger,
	}
}

// SetPromotionPricer подключает калькулятор акций. Безопасен для nil — тогда цены остаются базовыми.
func (u *ProductUsecase) SetPromotionPricer(p *PromotionPricer) {
	u.pricer = p
}

// applyPromotions — точка интеграции акций. Если pricer не задан, ничего не делает.
func (u *ProductUsecase) applyPromotions(ctx context.Context, products []*db.Product) {
	if u.pricer == nil || len(products) == 0 {
		return
	}
	u.pricer.Apply(ctx, products)
}

func (u *ProductUsecase) GetCatalogProducts(ctx context.Context, limit, offset int, categoryID *int64) ([]*db.Product, error) {
	if limit <= 0 {
		return []*db.Product{}, nil
	}

	// Из-за stock buffer часть товаров может отфильтроваться уже после выборки из БД.
	// Чтобы пагинация оставалась “плотной”, добираем страницу несколькими батчами.
	const maxBatches = 20
	collected := make([]*db.Product, 0, limit)

	batchLimit := limit
	batchOffset := offset

	for batches := 0; batches < maxBatches && len(collected) < limit; batches++ {
		var products []*db.Product
		var err error

		if categoryID != nil && *categoryID > 0 {
			products, err = u.repo.GetByCategory(ctx, *categoryID, batchLimit, batchOffset)
		} else {
			products, err = u.repo.GetActive(ctx, batchLimit, batchOffset)
		}
		if err != nil {
			return nil, err
		}
		if len(products) == 0 {
			break
		}

		// Собираем ID для батчевого запроса в Redis.
		ids := make([]int64, 0, len(products))
		for _, p := range products {
			if p != nil {
				ids = append(ids, p.ID)
			}
		}
		// Один пайплайн в Redis вместо N отдельных вызовов.
		availableMap, _ := u.stockCache.BatchGetAvailableStocks(ctx, ids, u.repo)
		u.applyStockBuffer(ctx, products, availableMap)

		for _, p := range products {
			if p == nil {
				continue
			}
			if avail, ok := availableMap[p.ID]; ok {
				p.Stock = avail
			}
			if p.Stock > 0 {
				collected = append(collected, p)
				if len(collected) >= limit {
					break
				}
			}
		}

		// Если БД вернула меньше лимита — это конец списка.
		if len(products) < batchLimit {
			break
		}
		batchOffset += batchLimit
	}

	u.applyPromotions(ctx, collected)
	return collected, nil
}

func (u *ProductUsecase) GetProductByID(ctx context.Context, id int64) (*db.Product, error) {
	product, err := u.repo.GetByID(ctx, id)
	if err != nil || product == nil {
		return product, err
	}

	// Кэшируем остатки товаров
	stock, err := u.stockCache.GetAvailableStock(ctx, id, u.repo)
	if err != nil {
		u.logger.Error("Failed to get available stock", zap.Int64("product_id", id), zap.Error(err))
	} else if stock >= 0 {
		product.Stock = stock
		u.logger.Info("Using available stock from cache", zap.Int64("product_id", id), zap.Int("available_stock", stock))
	} else if stock == -2 {
		// Cache miss - используем остаток из БД
		u.logger.Info("Cache miss, using DB stock", zap.Int64("product_id", id), zap.Int("db_stock", product.Stock))
		// product.Stock уже содержит остаток из БД
	} else {
		u.logger.Error("Unexpected availableStock value", zap.Int64("product_id", id), zap.Int("available_stock", stock))
	}

	// Убедимся что Stock всегда установлено
	if product.Stock == 0 && (stock == -2) {
		// Если в БД тоже 0, оставляем 0
		product.Stock = 0
	}

	u.applyPromotions(ctx, []*db.Product{product})
	return product, nil
}

func (u *ProductUsecase) SearchProducts(ctx context.Context, q string, categoryID *int64, limit, offset int) ([]*db.Product, error) {
	if limit <= 0 {
		return []*db.Product{}, nil
	}

	const maxBatches = 20
	collected := make([]*db.Product, 0, limit)

	batchLimit := limit
	batchOffset := offset

	for batches := 0; batches < maxBatches && len(collected) < limit; batches++ {
		products, err := u.repo.Search(ctx, q, categoryID, batchLimit, batchOffset)
		if err != nil {
			return nil, err
		}
		if len(products) == 0 {
			break
		}

		// Батчевый запрос в Redis (как в каталоге).
		ids := make([]int64, 0, len(products))
		for _, p := range products {
			if p != nil {
				ids = append(ids, p.ID)
			}
		}
		availableMap, _ := u.stockCache.BatchGetAvailableStocks(ctx, ids, u.repo)
		u.applyStockBuffer(ctx, products, availableMap)

		for _, p := range products {
			if p == nil {
				continue
			}
			if avail, ok := availableMap[p.ID]; ok {
				p.Stock = avail
			}
			if p.Stock > 0 {
				collected = append(collected, p)
				if len(collected) >= limit {
					break
				}
			}
		}

		if len(products) < batchLimit {
			break
		}
		batchOffset += batchLimit
	}

	u.applyPromotions(ctx, collected)
	return collected, nil
}

// GetActiveProductsCount возвращает количество активных товаров
func (u *ProductUsecase) GetActiveProductsCount(ctx context.Context, categoryID *int64) (int, error) {
	if categoryID != nil {
		return u.repo.CountByCategory(ctx, *categoryID)
	}
	return u.repo.CountActive(ctx)
}

// GetSearchProductsCount возвращает количество товаров по поисковому запросу
func (u *ProductUsecase) GetSearchProductsCount(ctx context.Context, query string, categoryID *int64) (int, error) {
	return u.repo.CountSearch(ctx, query, categoryID)
}

// applyStockBuffer повторно использует уже существующую логику по смещению остатков.
func (u *ProductUsecase) applyStockBuffer(ctx context.Context, products []*db.Product, availableMap map[int64]int) {
	if u.stockBuffer <= 0 {
		return
	}
	for _, product := range products {
		if product == nil {
			continue
		}
		if avail, ok := availableMap[product.ID]; ok && avail >= 0 {
			product.Stock = avail
		} else {
			bufferedStock := float64(product.Stock) * (1 - u.stockBuffer/100)
			product.Stock = int(bufferedStock)
		}
	}
}
