package services

import (
	"context"
	"errors"
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

// errNoImage возвращается когда у товара нет изображений в МойСклад (не ошибка, просто пропуск).
var errNoImage = errors.New("product has no images in MoySklad")

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

	// Фильтруем: нужны товары с Moysklad ID, у которых ещё нет локального изображения.
	// image_sync работает независимо от product_sync — не требует image_url с MoySklad URL.
	var toDownload []*db.Product
	for _, p := range products {
		if p.MoyskladID == nil || *p.MoyskladID == "" {
			continue
		}
		// Если уже есть локальное изображение — пропускаем
		if p.ImageURL != nil && strings.HasPrefix(*p.ImageURL, "/static") {
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
	var downloaded, skipped, failed int

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
				if errors.Is(err, errNoImage) {
					skipped++
				} else {
					s.logger.Warn("Failed to download image",
						zap.Int64("product_id", product.ID),
						zap.String("name", product.Name),
						zap.Error(err))
					failed++
				}
			} else {
				downloaded++
			}
		}

		s.logger.Info("Image batch completed",
			zap.Int("batch_start", i),
			zap.Int("downloaded", downloaded),
			zap.Int("errors", failed))

		if i+batchSize < len(toDownload) {
			time.Sleep(pauseBetweenBatches)
		}
	}

	s.logger.Info("Image sync finished",
		zap.Int("downloaded", downloaded),
		zap.Int("skipped", skipped),
		zap.Int("errors", failed))
	return nil
}

func (s *ImageSyncService) downloadProductImage(ctx context.Context, product *db.Product) error {
	// Получаем href первого изображения товара из МойСклад по его moysklad_id.
	imageHref, err := s.moyskladClient.GetProductFirstImageHref(ctx, *product.MoyskladID)
	if err != nil {
		return fmt.Errorf("get image href failed: %w", err)
	}
	if imageHref == "" {
		return errNoImage
	}

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
