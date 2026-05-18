package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/TeleginSergey/hozdacha/internal/db"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type StockCache struct {
	client *redis.Client
	logger *zap.Logger
}

type StockInfo struct {
	Stock      int       `json:"stock"`
	CachedAt   time.Time `json:"cached_at"`
	MoyskladID string    `json:"moysklad_id"`
}

func NewStockCache(host, port, password string, db int, logger *zap.Logger) (*StockCache, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", host, port),
		Password: password,
		DB:       db,
	})

	// Проверяем подключение
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	logger.Info("Redis connected successfully")

	return &StockCache{
		client: client,
		logger: logger,
	}, nil
}

func (c *StockCache) Close() error {
	return c.client.Close()
}

// GetRedisClient возвращает Redis клиент для использования в других сервисах
func (c *StockCache) GetRedisClient() *redis.Client {
	return c.client
}

// SetStock кэширует остаток товара
func (c *StockCache) SetStock(ctx context.Context, productID int64, moyskladID string, stock int) error {
	key := fmt.Sprintf("stock:%d", productID)

	info := StockInfo{
		Stock:      stock,
		CachedAt:   time.Now(),
		MoyskladID: moyskladID,
	}

	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("failed to marshal stock info: %w", err)
	}

	// TTL 30 минут
	return c.client.Set(ctx, key, data, 30*time.Minute).Err()
}

// GetStock получает остаток из кэша
func (c *StockCache) GetStock(ctx context.Context, productID int64) (*StockInfo, error) {
	key := fmt.Sprintf("stock:%d", productID)

	data, err := c.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil // Кэш не найден
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get stock from cache: %w", err)
	}

	var info StockInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("failed to unmarshal stock info: %w", err)
	}

	return &info, nil
}

// GetStockWithBuffer получает остаток с учетом буфера
func (c *StockCache) GetStockWithBuffer(ctx context.Context, productID int64, bufferPercent float64) (int, error) {
	info, err := c.GetStock(ctx, productID)
	if err != nil {
		return 0, err
	}
	if info == nil {
		// Важно: отсутствие кэша НЕ должно затирать остаток из БД нулем.
		// Возвращаем -1 как маркер cache-miss.
		return -1, nil
	}

	// Применяем буфер: stock - (stock * bufferPercent / 100)
	bufferedStock := float64(info.Stock) * (1 - bufferPercent/100)
	return int(bufferedStock), nil
}

// InvalidateStock удаляет остаток из кэша
func (c *StockCache) InvalidateStock(ctx context.Context, productID int64) error {
	key := fmt.Sprintf("stock:%d", productID)
	return c.client.Del(ctx, key).Err()
}

// InvalidateStockCache псевдоним для InvalidateStock (для совместимости с интерфейсом)
func (c *StockCache) InvalidateStockCache(ctx context.Context, productID int64) error {
	return c.InvalidateStock(ctx, productID)
}

// InvalidateByMoyskladID удаляет кэш по moysklad_id
func (c *StockCache) InvalidateByMoyskladID(ctx context.Context, moyskladID string) error {
	// Ищем все ключи с префиксом stock:*
	keys, err := c.client.Keys(ctx, "stock:*").Result()
	if err != nil {
		return fmt.Errorf("failed to get keys: %w", err)
	}

	// Проверяем каждый ключ и удаляем если moysklad_id совпадает
	for _, key := range keys {
		data, err := c.client.Get(ctx, key).Bytes()
		if err != nil {
			continue
		}

		var info StockInfo
		if err := json.Unmarshal(data, &info); err != nil {
			continue
		}

		if info.MoyskladID == moyskladID {
			if err := c.client.Del(ctx, key).Err(); err != nil {
				c.logger.Warn("Failed to delete cache key", zap.String("key", key), zap.Error(err))
			}
		}
	}

	return nil
}

// SetLastSyncTime сохраняет время последней синхронизации
func (c *StockCache) SetLastSyncTime(ctx context.Context, t time.Time) error {
	key := "sync:last_time"
	return c.client.Set(ctx, key, t.Format(time.RFC3339), 0).Err() // Без TTL
}

// GetLastSyncTime получает время последней синхронизации
func (c *StockCache) GetLastSyncTime(ctx context.Context) (*time.Time, error) {
	key := "sync:last_time"

	val, err := c.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil // Не найдено
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get last sync time: %w", err)
	}

	t, err := time.Parse(time.RFC3339, val)
	if err != nil {
		return nil, fmt.Errorf("failed to parse time: %w", err)
	}

	return &t, nil
}

// ReserveStock резервирует товар для корзины (временно уменьшает доступный остаток)
func (c *StockCache) ReserveStock(ctx context.Context, productID int64, quantity int) error {
	key := fmt.Sprintf("reserved:%d", productID)
	if quantity <= 0 {
		return nil
	}

	return c.client.IncrBy(ctx, key, int64(quantity)).Err()
}

// ReleaseStock освобождает зарезервированный товар
func (c *StockCache) ReleaseStock(ctx context.Context, productID int64, quantity int) error {
	key := fmt.Sprintf("reserved:%d", productID)
	if quantity <= 0 {
		return nil
	}

	script := redis.NewScript(`
local current = tonumber(redis.call("GET", KEYS[1]) or "0")
local next = current - tonumber(ARGV[1])
if next <= 0 then
	redis.call("DEL", KEYS[1])
	return 0
end
redis.call("SET", KEYS[1], next)
return next
`)
	return script.Run(ctx, c.client, []string{key}, quantity).Err()
}

// GetReservedStock получает количество зарезервированного товара
func (c *StockCache) GetReservedStock(ctx context.Context, productID int64) (int, error) {
	key := fmt.Sprintf("reserved:%d", productID)

	val, err := c.client.Get(ctx, key).Int()
	if err == redis.Nil {
		return 0, nil // Нет резервирований
	}
	if err != nil {
		return 0, fmt.Errorf("failed to get reserved stock: %w", err)
	}

	return val, nil
}

// GetAvailableStock получает доступный остаток (фактический - зарезервированный)
func (c *StockCache) GetAvailableStock(ctx context.Context, productID int64, productQuery db.ProductQuery) (int, error) {
	info, err := c.GetStock(ctx, productID)
	if err != nil {
		return 0, err
	}
	if info == nil {
		product, err := productQuery.GetByID(ctx, productID)
		if err != nil {
			return 0, fmt.Errorf("failed to get product stock from db: %w", err)
		}
		if product == nil {
			return 0, fmt.Errorf("product %d not found", productID)
		}
		if err := c.SetStock(ctx, product.ID, "", product.Stock); err != nil {
			c.logger.Warn("Failed to warm stock cache from db", zap.Int64("product_id", productID), zap.Error(err))
		}
		info = &StockInfo{Stock: product.Stock}
	}

	reserved, err := c.GetReservedStock(ctx, productID)
	if err != nil {
		return 0, err
	}

	available := info.Stock - reserved
	if available < 0 {
		available = 0
	}

	return available, nil
}

func (c *StockCache) ConsumeReservedStock(ctx context.Context, productID int64, quantity int) error {
	return c.ReleaseStock(ctx, productID, quantity)
}

// SetStockToCache устанавливает остаток в кэш (для совместимости с интерфейсом)
func (c *StockCache) SetStockToCache(ctx context.Context, productID int64, stock int) error {
	return c.SetStock(ctx, productID, "", stock)
}

// BatchGetAvailableStocks получает доступные остатки для списка товаров одним пайплайном.
// Возвращает map[productID]availableStock. Товары с холодным кэшем получают stock из БД.
func (c *StockCache) BatchGetAvailableStocks(ctx context.Context, productIDs []int64, productQuery db.ProductQuery) (map[int64]int, error) {
	if len(productIDs) == 0 {
		return map[int64]int{}, nil
	}

	// 1. Пачкой читаем stock из Redis через pipeline.
	pipe := c.client.Pipeline()
	stockCmds := make([]*redis.StringCmd, len(productIDs))
	reservedCmds := make([]*redis.StringCmd, len(productIDs))
	for i, pid := range productIDs {
		stockCmds[i] = pipe.Get(ctx, fmt.Sprintf("stock:%d", pid))
		reservedCmds[i] = pipe.Get(ctx, fmt.Sprintf("reserved:%d", pid))
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		// Pipeline error — не фатально, но логируем.
		c.logger.Warn("Batch stock pipeline failed", zap.Error(err))
	}

	// 2. Собираем холодные ID (те, которых нет в Redis).
	coldIDs := make([]int64, 0)
	stockMap := make(map[int64]int, len(productIDs))
	reservedMap := make(map[int64]int, len(productIDs))
	for i, pid := range productIDs {
		val, err := stockCmds[i].Result()
		if err == redis.Nil {
			coldIDs = append(coldIDs, pid)
			continue
		}
		if err != nil {
			coldIDs = append(coldIDs, pid)
			continue
		}
		var info StockInfo
		if err := json.Unmarshal([]byte(val), &info); err != nil {
			coldIDs = append(coldIDs, pid)
			continue
		}
		stockMap[pid] = info.Stock

		// Reserved.
		rval, err := reservedCmds[i].Result()
		if err == nil {
			var r int
			fmt.Sscanf(rval, "%d", &r)
			reservedMap[pid] = r
		}
	}

	// 3. Холодные — тянем из БД и греем кэш.
	if len(coldIDs) > 0 {
		dbProducts, err := productQuery.GetByIDs(ctx, coldIDs)
		if err != nil {
			c.logger.Warn("Batch DB fetch for cold cache failed", zap.Error(err))
		} else {
			for _, p := range dbProducts {
				if p != nil {
					stockMap[p.ID] = p.Stock
					msID := ""
					if p.MoyskladID != nil {
						msID = *p.MoyskladID
					}
					_ = c.SetStock(ctx, p.ID, msID, p.Stock)
				}
			}
		}
		// Для товаров, которых нет в БД — stock = 0.
		for _, pid := range coldIDs {
			if _, ok := stockMap[pid]; !ok {
				stockMap[pid] = 0
			}
		}
	}

	// 4. Вычисляем available = stock - reserved.
	result := make(map[int64]int, len(productIDs))
	for _, pid := range productIDs {
		s := stockMap[pid]
		r := reservedMap[pid]
		avail := s - r
		if avail < 0 {
			avail = 0
		}
		result[pid] = avail
	}
	return result, nil
}

// BatchSetStocks массово устанавливает остатки в кэш
func (c *StockCache) BatchSetStocks(ctx context.Context, products []*db.Product) error {
	for _, product := range products {
		moyskladID := ""
		if product.MoyskladID != nil {
			moyskladID = *product.MoyskladID
		}
		if err := c.SetStock(ctx, product.ID, moyskladID, product.Stock); err != nil {
			c.logger.Warn("Failed to set stock in batch",
				zap.Int64("product_id", product.ID),
				zap.Error(err))
		}
	}
	return nil
}
