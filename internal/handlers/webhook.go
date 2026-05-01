package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/cache"
	"github.com/TeleginSergey/hozdacha/internal/db"
	"github.com/TeleginSergey/hozdacha/internal/moysklad"
)

type WebhookHandler struct {
	productQuery   db.ProductQuery
	moyskladClient *moysklad.Client
	stockCache     *cache.StockCache
	webhookSecret  string
	logger         *zap.Logger
}

func NewWebhookHandler(
	productQuery db.ProductQuery,
	moyskladClient *moysklad.Client,
	stockCache *cache.StockCache,
	webhookSecret string,
	logger *zap.Logger,
) *WebhookHandler {
	return &WebhookHandler{
		productQuery:   productQuery,
		moyskladClient: moyskladClient,
		stockCache:     stockCache,
		webhookSecret:  webhookSecret,
		logger:         logger,
	}
}

// MoyskladWebhookEvent структура события от МойСклад
type MoyskladWebhookEvent struct {
	Events []MoyskladEvent `json:"events"`
}

type MoyskladEvent struct {
	Meta struct {
		Type string `json:"type"`
		Href string `json:"href"`
	} `json:"meta"`
	Action    string `json:"action"` // CREATE, UPDATE, DELETE
	AccountID string `json:"accountId"`
}

// MoyskladStockWebhookEvent структура события webhookstock
type MoyskladStockWebhookEvent struct {
	AccountID  string `json:"accountId"`
	StockType  string `json:"stockType"`
	ReportType string `json:"reportType"`
	ReportURL  string `json:"reportUrl"`
}

// HandleWebhook обрабатывает webhook от МойСклад
func (h *WebhookHandler) HandleWebhook(c *gin.Context) {
	// Проверка подписи (если настроен секрет)
	if h.webhookSecret != "" {
		signature := c.GetHeader("X-Moysklad-Signature")
		if signature == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing signature"})
			return
		}

		// Читаем тело запроса
		body, err := c.GetRawData()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read body"})
			return
		}

		// Проверяем подпись
		if !h.verifySignature(body, signature) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid signature"})
			return
		}

		// Восстанавливаем тело для дальнейшей обработки
		c.Request.Body = http.NoBody
		c.Set("webhook_body", body)
	}

	var event MoyskladWebhookEvent
	if err := c.ShouldBindJSON(&event); err != nil {
		h.logger.Warn("Failed to parse webhook event", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}

	// Обрабатываем каждое событие
	for _, evt := range event.Events {
		if err := h.processEvent(c.Request.Context(), evt); err != nil {
			h.logger.Error("Failed to process webhook event",
				zap.String("type", evt.Meta.Type),
				zap.String("action", evt.Action),
				zap.Error(err))
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// HandleStockWebhook обрабатывает webhook на изменение остатков (webhookstock)
func (h *WebhookHandler) HandleStockWebhook(c *gin.Context) {
	// Проверка подписи (если настроен секрет)
	if h.webhookSecret != "" {
		signature := c.GetHeader("X-Moysklad-Signature")
		if signature == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing signature"})
			return
		}

		// Читаем тело запроса
		body, err := c.GetRawData()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read body"})
			return
		}

		// Проверяем подпись
		if !h.verifySignature(body, signature) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid signature"})
			return
		}

		// Восстанавливаем тело для дальнейшей обработки
		c.Request.Body = http.NoBody
		c.Set("webhook_body", body)
	}

	var stockEvent MoyskladStockWebhookEvent
	if err := c.ShouldBindJSON(&stockEvent); err != nil {
		h.logger.Warn("Failed to parse stock webhook event", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}

	// Обрабатываем событие изменения остатков
	if err := h.processStockEvent(c.Request.Context(), stockEvent); err != nil {
		h.logger.Error("Failed to process stock webhook event",
			zap.String("reportType", stockEvent.ReportType),
			zap.String("reportUrl", stockEvent.ReportURL),
			zap.Error(err))
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *WebhookHandler) processEvent(ctx context.Context, event MoyskladEvent) error {
	// Обрабатываем только события товаров
	if event.Meta.Type != "product" {
		return nil
	}

	// Извлекаем ID товара из href
	moyskladID := h.extractIDFromHref(event.Meta.Href)
	if moyskladID == "" {
		return nil
	}

	switch event.Action {
	case "CREATE", "UPDATE":
		// Обновляем товар из МойСклад
		return h.syncProduct(ctx, moyskladID)
	case "DELETE":
		// Помечаем товар как неактивный
		return h.deactivateProduct(ctx, moyskladID)
	}

	return nil
}

func (h *WebhookHandler) syncProduct(ctx context.Context, moyskladID string) error {
	// Получаем товар из МойСклад
	products, err := h.moyskladClient.GetProductsByID(ctx, moyskladID)
	if err != nil {
		return err
	}

	if len(products) == 0 {
		return nil
	}

	msProduct := products[0]

	// Находим товар в БД
	existing, err := h.productQuery.GetByMoyskladID(ctx, moyskladID)
	if err != nil {
		return err
	}

	// Обновляем или создаем товар
	product := &db.Product{
		Name:        msProduct.Name,
		Description: msProduct.Description,
		MoyskladID:  &moyskladID,
		Active:      true, // не удалён
	}

	// Парсим цену
	if len(msProduct.SalePrices) > 0 && msProduct.SalePrices[0].Value > 0 {
		product.Price = msProduct.SalePrices[0].Value / 100.0
	}

	// Обновляем остаток (из вложенного объекта stock)
	if msProduct.Stock != nil {
		if msProduct.Stock.Stock > 0 {
			product.Stock = int(msProduct.Stock.Stock)
		} else {
			product.Stock = 0
		}
	}

	// Статус по остатку
	if product.Stock > 0 {
		product.Status = "active"
	} else {
		product.Status = "out_of_stock"
	}

	// Обновляем изображение
	if msProduct.Images != nil && len(msProduct.Images.Rows) > 0 {
		imageURL := msProduct.Images.Rows[0].Meta.Href
		product.ImageURL = &imageURL
	}

	if existing == nil {
		// Создаем новый товар
		created, err := h.productQuery.Insert(ctx, product)
		if err != nil {
			return err
		}
		// Кэшируем остаток
		if created != nil {
			h.stockCache.SetStock(ctx, created.ID, moyskladID, created.Stock)
		}
	} else {
		// Обновляем существующий товар
		product.ID = existing.ID
		updated, err := h.productQuery.Update(ctx, product, existing.ID)
		if err != nil {
			return err
		}
		// Обновляем кэш остатка
		if updated != nil {
			h.stockCache.SetStock(ctx, updated.ID, moyskladID, updated.Stock)
		}
		// Инвалидируем старый кэш
		h.stockCache.InvalidateByMoyskladID(ctx, moyskladID)
	}

	h.logger.Info("Product synced from webhook",
		zap.String("moysklad_id", moyskladID),
		zap.String("action", "sync"))

	return nil
}

func (h *WebhookHandler) deactivateProduct(ctx context.Context, moyskladID string) error {
	product, err := h.productQuery.GetByMoyskladID(ctx, moyskladID)
	if err != nil {
		return err
	}
	if product == nil {
		return nil
	}

	product.Active = false
	product.Status = "deleted"
	product.Stock = 0
	_, err = h.productQuery.Update(ctx, product, product.ID)
	if err != nil {
		return err
	}

	// Инвалидируем кэш
	h.stockCache.InvalidateStock(ctx, product.ID)

	h.logger.Info("Product deactivated from webhook",
		zap.String("moysklad_id", moyskladID))

	return nil
}

func (h *WebhookHandler) extractIDFromHref(href string) string {
	// Извлекаем ID из href типа: https://api.moysklad.ru/api/remap/1.2/entity/product/12345678-1234-1234-1234-123456789012
	// Или просто возвращаем последнюю часть пути
	parts := []rune(href)
	lastSlash := -1
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] == '/' {
			lastSlash = i
			break
		}
	}
	if lastSlash >= 0 && lastSlash < len(parts)-1 {
		return string(parts[lastSlash+1:])
	}
	return ""
}

func (h *WebhookHandler) processStockEvent(ctx context.Context, event MoyskladStockWebhookEvent) error {
	h.logger.Info("Processing stock webhook event",
		zap.String("reportType", event.ReportType),
		zap.String("reportUrl", event.ReportURL))

	// Получаем отчет по остаткам из предоставленного URL
	stockReport, err := h.moyskladClient.GetStockReportFromURL(ctx, event.ReportURL)
	if err != nil {
		return fmt.Errorf("failed to get stock report from URL: %w", err)
	}

	// Обновляем кэш остатков
	for moyskladID, stock := range stockReport {
		// Находим товар в БД по moysklad_id
		product, err := h.productQuery.GetByMoyskladID(ctx, moyskladID)
		if err != nil {
			h.logger.Warn("Failed to find product for stock update",
				zap.String("moysklad_id", moyskladID),
				zap.Error(err))
			continue
		}
		if product == nil {
			continue
		}

		// Обновляем остаток в БД
		updatedStock := int(stock)
		product.Stock = updatedStock
		if updatedStock > 0 {
			product.Status = "active"
		} else {
			product.Status = "out_of_stock"
		}

		_, err = h.productQuery.Update(ctx, product, product.ID)
		if err != nil {
			h.logger.Warn("Failed to update product stock",
				zap.String("moysklad_id", moyskladID),
				zap.Int("stock", updatedStock),
				zap.Error(err))
			continue
		}

		// Обновляем кэш
		err = h.stockCache.SetStock(ctx, product.ID, moyskladID, updatedStock)
		if err != nil {
			h.logger.Warn("Failed to update stock cache",
				zap.String("moysklad_id", moyskladID),
				zap.Int("stock", updatedStock),
				zap.Error(err))
		}
	}

	h.logger.Info("Stock webhook processed successfully",
		zap.String("reportType", event.ReportType),
		zap.Int("stockUpdates", len(stockReport)))

	return nil
}

func (h *WebhookHandler) verifySignature(body []byte, signature string) bool {
	if h.webhookSecret == "" {
		return true // Если секрет не настроен, пропускаем проверку
	}

	mac := hmac.New(sha256.New, []byte(h.webhookSecret))
	mac.Write(body)
	expectedSignature := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(expectedSignature), []byte(signature))
}
