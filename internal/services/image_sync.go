package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/TeleginSergey/hozdacha/internal/db"
	"github.com/TeleginSergey/hozdacha/internal/moysklad"
	"go.uber.org/zap"
)

// ImageSyncService скачивает изображения товаров из МойСклад и сохраняет локально.
// Рассчитан на слабый сервер: батчи по 5 товаров, паузы 500ms между батчами,
// только миниатюры (небольшой размер).
type ImageSyncService struct {
	moyskladClient *moysklad.Client
	productQuery   db.ProductQuery
	imagesDir      string
	logger         *zap.Logger
	running        int32
}

func NewImageSyncService(moyskladClient *moysklad.Client, productQuery db.ProductQuery, imagesDir string, logger *zap.Logger) *ImageSyncService {
	return &ImageSyncService{
		moyskladClient: moyskladClient,
		productQuery:   productQuery,
		imagesDir:      imagesDir,
		logger:         logger,
	}
}

// SyncImages проходит по всем товарам, у которых image_url указывает на МойСклад,
// скачивает изображения и сохраняет локально.
func (s *ImageSyncService) SyncImages(ctx context.Context) error {
	if s.moyskladClient == nil {
		return fmt.Errorf("moysklad client not initialized")
	}

	if !atomic.CompareAndSwapInt32(&s.running, 0, 1) {
		return fmt.Errorf("image sync already running")
	}
	defer atomic.StoreInt32(&s.running, 0)

	// Создаём директорию для изображений
	if err := os.MkdirAll(s.imagesDir, 0755); err != nil {
		return fmt.Errorf("failed to create images directory: %w", err)
	}

	// Получаем все активные товары с Moysklad ID (у них потенциально есть изображения)
	products, err := s.productQuery.GetAll(ctx, 100000, 0)
	if err != nil {
		return fmt.Errorf("failed to get products: %w", err)
	}

	// Фильтруем: нужны товары с Moysklad ID и image_url, указывающим на МойСклад
	var toDownload []*db.Product
	for _, p := range products {
		if p.MoyskladID == nil || *p.MoyskladID == "" {
			continue
		}
		if p.ImageURL == nil || *p.ImageURL == "" {
			continue
		}
		// Если image_url уже локальный — пропускаем
		if !strings.Contains(*p.ImageURL, "moysklad") {
			continue
		}
		toDownload = append(toDownload, p)
	}

	if len(toDownload) == 0 {
		s.logger.Info("No products need image download")
		return nil
	}

	s.logger.Info("Starting image download",
		zap.Int("total_products", len(toDownload)))

	const batchSize = 5
	const pauseBetweenBatches = 500 * time.Millisecond
	var downloaded, skipped, errors int

	for i := 0; i < len(toDownload); i += batchSize {
		end := i + batchSize
		if end > len(toDownload) {
			end = len(toDownload)
		}

		batch := toDownload[i:end]
		for _, product := range batch {
			select {
			case <-ctx.Done():
				s.logger.Info("Image sync cancelled", zap.Int("downloaded", downloaded))
				return ctx.Err()
			default:
			}

			if err := s.downloadProductImage(ctx, product); err != nil {
				s.logger.Warn("Failed to download image",
					zap.Int64("product_id", product.ID),
					zap.String("name", product.Name),
					zap.Error(err))
				errors++
			} else {
				downloaded++
			}
		}

		s.logger.Info("Image batch completed",
			zap.Int("batch_start", i),
			zap.Int("downloaded", downloaded),
			zap.Int("errors", errors))

		if i+batchSize < len(toDownload) {
			time.Sleep(pauseBetweenBatches)
		}
	}

	s.logger.Info("Image sync finished",
		zap.Int("downloaded", downloaded),
		zap.Int("skipped", skipped),
		zap.Int("errors", errors))
	return nil
}

func (s *ImageSyncService) downloadProductImage(ctx context.Context, product *db.Product) error {
	imageHref := *product.ImageURL

	data, contentType, err := s.moyskladClient.DownloadImage(ctx, imageHref)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	// Определяем расширение файла
	ext := ".jpg"
	switch {
	case strings.Contains(contentType, "png"):
		ext = ".png"
	case strings.Contains(contentType, "gif"):
		ext = ".gif"
	case strings.Contains(contentType, "webp"):
		ext = ".webp"
	}

	filename := fmt.Sprintf("%d%s", product.ID, ext)
	filePath := filepath.Join(s.imagesDir, filename)

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	// Обновляем image_url на локальный путь
	localURL := "/static/images/products/" + filename
	product.ImageURL = &localURL
	product.UpdatedAt = time.Now()

	if _, err := s.productQuery.Update(ctx, product, product.ID); err != nil {
		return fmt.Errorf("failed to update product image_url: %w", err)
	}

	s.logger.Debug("Image downloaded",
		zap.Int64("product_id", product.ID),
		zap.String("name", product.Name),
		zap.String("local_url", localURL),
		zap.Int("size_bytes", len(data)))
	return nil
}
