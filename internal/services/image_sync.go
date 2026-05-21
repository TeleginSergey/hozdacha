package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
		// Если уже есть локальное изображение — проверяем что файл реально существует на диске.
		// После редеплоя DB хранит старый путь, но файл может отсутствовать.
		if p.ImageURL != nil && strings.HasPrefix(*p.ImageURL, "/static") {
			filename := filepath.Base(*p.ImageURL)
			filePath := filepath.Join(s.imagesDir, filename)
			if _, err := os.Stat(filePath); err == nil {
				continue // файл есть — пропускаем
			}
			// файл исчез (редеплой/смена тома) — скачиваем заново
		}
		toDownload = append(toDownload, p)
	}

	if len(toDownload) == 0 {
		s.logger.Info("No products need image download")
		return nil
	}

	s.logger.Info("Starting image download",
		zap.Int("total_products", len(toDownload)))

	// Размер батча: 5 параллельных горутин. Rate limiter клиента МойСклад
	// сам сериализует исходящие запросы по 10/сек — дополнительный sleep не нужен.
	const batchSize = 5
	var downloaded, skipped, failed int
	var mu sync.Mutex

	for i := 0; i < len(toDownload); i += batchSize {
		select {
		case <-ctx.Done():
			s.logger.Info("Image sync cancelled", zap.Int("downloaded", downloaded))
			return ctx.Err()
		default:
		}

		end := i + batchSize
		if end > len(toDownload) {
			end = len(toDownload)
		}

		var wg sync.WaitGroup
		batch := toDownload[i:end]
		for _, product := range batch {
			wg.Add(1)
			go func(p *db.Product) {
				defer wg.Done()
				err := s.downloadProductImage(ctx, p)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					if errors.Is(err, errNoImage) {
						skipped++
					} else {
						s.logger.Warn("Failed to download image",
							zap.Int64("product_id", p.ID),
							zap.String("name", p.Name),
							zap.Error(err))
						failed++
					}
				} else {
					downloaded++
				}
			}(product)
		}
		wg.Wait()

		s.logger.Info("Image batch completed",
			zap.Int("batch_start", i),
			zap.Int("downloaded", downloaded),
			zap.Int("errors", failed))
	}

	s.logger.Info("Image sync finished",
		zap.Int("downloaded", downloaded),
		zap.Int("skipped", skipped),
		zap.Int("errors", failed))
	return nil
}

// SyncImagesWithCleanup скачивает отсутствующие изображения И удаляет осиротевшие файлы
// (для товаров, удалённых из МойСклад). Предназначен для ночного запуска.
func (s *ImageSyncService) SyncImagesWithCleanup(ctx context.Context) error {
	// Сначала скачиваем недостающие
	if err := s.SyncImages(ctx); err != nil {
		return err
	}

	// Затем чистим осиротевшие файлы
	return s.cleanupOrphanedFiles(ctx)
}

// cleanupOrphanedFiles удаляет файлы изображений, которым нет соответствующего товара в БД.
func (s *ImageSyncService) cleanupOrphanedFiles(ctx context.Context) error {
	// Получаем все image_url из БД
	products, err := s.productQuery.GetAll(ctx, 100000, 0)
	if err != nil {
		return fmt.Errorf("failed to get products for cleanup: %w", err)
	}

	// Строим множество файлов, на которые есть ссылки из БД
	known := make(map[string]struct{}, len(products))
	for _, p := range products {
		if p.ImageURL != nil && strings.HasPrefix(*p.ImageURL, "/static") {
			known[filepath.Base(*p.ImageURL)] = struct{}{}
		}
	}

	// Читаем все файлы в директории
	entries, err := os.ReadDir(s.imagesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // папки нет — нечего чистить
		}
		return fmt.Errorf("failed to read images dir: %w", err)
	}

	var deleted int
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if _, ok := known[name]; !ok {
			path := filepath.Join(s.imagesDir, name)
			if err := os.Remove(path); err != nil {
				s.logger.Warn("Failed to remove orphaned image", zap.String("file", path), zap.Error(err))
			} else {
				deleted++
				s.logger.Debug("Removed orphaned image", zap.String("file", name))
			}
		}
	}

	if deleted > 0 {
		s.logger.Info("Cleaned up orphaned images", zap.Int("deleted", deleted))
	} else {
		s.logger.Info("No orphaned images to clean up")
	}
	return nil
}

func (s *ImageSyncService) downloadProductImage(ctx context.Context, product *db.Product) error {
	// Получаем URL оригинального изображения (meta.downloadHref) из коллекции МойСклад.
	// Это 1 запрос вместо 2 (старый подход через JSON-метаданные изображения).
	downloadURL, err := s.moyskladClient.GetProductFirstDownloadURL(ctx, *product.MoyskladID)
	if err != nil {
		return fmt.Errorf("get download url failed: %w", err)
	}
	if downloadURL == "" {
		return errNoImage
	}

	data, contentType, err := s.moyskladClient.DownloadRawFile(ctx, downloadURL)
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
