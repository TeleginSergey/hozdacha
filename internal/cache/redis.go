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

	// Увеличиваем зарезервированное количество
	return c.client.IncrBy(ctx, key, int64(quantity)).Err()
}

// ReleaseStock освобождает зарезервированный товар
func (c *StockCache) ReleaseStock(ctx context.Context, productID int64, quantity int) error {
	key := fmt.Sprintf("reserved:%d", productID)

	// Уменьшаем зарезервированное количество
	return c.client.IncrBy(ctx, key, -int64(quantity)).Err()
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
		// Cache miss - возвращаем специальное значение чтобы использовать остаток из БД
		return -2, nil // -2 означает "использовать остаток из БД"
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

// SetStockToCache устанавливает остаток в кэш (для совместимости с интерфейсом)
func (c *StockCache) SetStockToCache(ctx context.Context, productID int64, stock int) error {
	return c.SetStock(ctx, productID, "", stock)
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
