package services

import (
	"context"
	"encoding/json"
	"fmt"

	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/cache"
	"github.com/TeleginSergey/hozdacha/internal/db"
	"github.com/TeleginSergey/hozdacha/internal/moysklad"
)

// WebhookProcessor выполняет бизнес-логику вебхуков (из HTTP или из очереди inbox).
type WebhookProcessor struct {
	productQuery   db.ProductQuery
	moyskladClient *moysklad.Client
	stockCache     *cache.StockCache
	logger         *zap.Logger
}

func NewWebhookProcessor(
	productQuery db.ProductQuery,
	moyskladClient *moysklad.Client,
	stockCache *cache.StockCache,
	logger *zap.Logger,
) *WebhookProcessor {
	return &WebhookProcessor{
		productQuery:   productQuery,
		moyskladClient: moyskladClient,
		stockCache:     stockCache,
		logger:         logger,
	}
}

func (p *WebhookProcessor) ProcessEntityPayload(ctx context.Context, payload []byte) error {
	var env moysklad.WebhookEntityEnvelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return fmt.Errorf("decode entity webhook: %w", err)
	}
	for _, evt := range env.Events {
		if err := p.processEntityEvent(ctx, evt); err != nil {
			return err
		}
	}
	return nil
}

func (p *WebhookProcessor) processEntityEvent(ctx context.Context, event moysklad.WebhookEntityEvent) error {
	if event.Meta.Type != "product" {
		return nil
	}
	moyskladID := extractIDFromHref(event.Meta.Href)
	if moyskladID == "" {
		return nil
	}
	switch event.Action {
	case "CREATE", "UPDATE":
		return p.syncProduct(ctx, moyskladID)
	case "DELETE":
		return p.deactivateProduct(ctx, moyskladID)
	}
	return nil
}

func (p *WebhookProcessor) ProcessStockPayload(ctx context.Context, payload []byte) error {
	var stockEvent moysklad.WebhookStockPayload
	if err := json.Unmarshal(payload, &stockEvent); err != nil {
		return fmt.Errorf("decode stock webhook: %w", err)
	}
	return p.processStockEvent(ctx, stockEvent)
}

func (p *WebhookProcessor) syncProduct(ctx context.Context, moyskladID string) error {
	products, err := p.moyskladClient.GetProductsByID(ctx, moyskladID)
	if err != nil {
		return err
	}
	if len(products) == 0 {
		return nil
	}
	msProduct := products[0]

	existing, err := p.productQuery.GetByMoyskladID(ctx, moyskladID)
	if err != nil {
		return err
	}

	product := &db.Product{
		Name:        msProduct.Name,
		Description: msProduct.Description,
		MoyskladID:  &moyskladID,
		Active:      true,
	}
	if len(msProduct.SalePrices) > 0 && msProduct.SalePrices[0].Value > 0 {
		product.Price = msProduct.SalePrices[0].Value / 100.0
	}
	if msProduct.Stock != nil {
		if msProduct.Stock.Stock > 0 {
			product.Stock = int(msProduct.Stock.Stock)
		} else {
			product.Stock = 0
		}
	}
	if product.Stock > 0 {
		product.Status = "active"
	} else {
		product.Status = "out_of_stock"
	}
	if msProduct.Images != nil && len(msProduct.Images.Rows) > 0 {
		imageURL := msProduct.Images.Rows[0].Meta.Href
		product.ImageURL = &imageURL
	}

	if existing == nil {
		created, err := p.productQuery.Insert(ctx, product)
		if err != nil {
			return err
		}
		if created != nil && p.stockCache != nil {
			_ = p.stockCache.SetStock(ctx, created.ID, moyskladID, created.Stock)
		}
	} else {
		product.ID = existing.ID
		updated, err := p.productQuery.Update(ctx, product, existing.ID)
		if err != nil {
			return err
		}
		if updated != nil && p.stockCache != nil {
			_ = p.stockCache.SetStock(ctx, updated.ID, moyskladID, updated.Stock)
			p.stockCache.InvalidateByMoyskladID(ctx, moyskladID)
		}
	}

	p.logger.Info("Product synced from webhook queue",
		zap.String("moysklad_id", moyskladID))
	return nil
}

func (p *WebhookProcessor) deactivateProduct(ctx context.Context, moyskladID string) error {
	product, err := p.productQuery.GetByMoyskladID(ctx, moyskladID)
	if err != nil {
		return err
	}
	if product == nil {
		return nil
	}
	product.Active = false
	product.Status = "deleted"
	product.Stock = 0
	_, err = p.productQuery.Update(ctx, product, product.ID)
	if err != nil {
		return err
	}
	if p.stockCache != nil {
		p.stockCache.InvalidateStock(ctx, product.ID)
	}
	p.logger.Info("Product deactivated from webhook queue", zap.String("moysklad_id", moyskladID))
	return nil
}

func (p *WebhookProcessor) processStockEvent(ctx context.Context, event moysklad.WebhookStockPayload) error {
	p.logger.Info("Processing stock webhook (queued)",
		zap.String("reportType", event.ReportType),
		zap.String("reportUrl", event.ReportURL))

	stockReport, err := p.moyskladClient.GetStockReportFromURL(ctx, event.ReportURL)
	if err != nil {
		return fmt.Errorf("stock report: %w", err)
	}

	for moyskladID, stock := range stockReport {
		product, err := p.productQuery.GetByMoyskladID(ctx, moyskladID)
		if err != nil {
			p.logger.Warn("find product for stock", zap.String("moysklad_id", moyskladID), zap.Error(err))
			continue
		}
		if product == nil {
			continue
		}
		updatedStock := int(stock)
		product.Stock = updatedStock
		if updatedStock > 0 {
			product.Status = "active"
		} else {
			product.Status = "out_of_stock"
		}
		_, err = p.productQuery.Update(ctx, product, product.ID)
		if err != nil {
			p.logger.Warn("update stock", zap.String("moysklad_id", moyskladID), zap.Error(err))
			continue
		}
		if p.stockCache != nil {
			if err := p.stockCache.SetStock(ctx, product.ID, moyskladID, updatedStock); err != nil {
				p.logger.Warn("stock cache", zap.String("moysklad_id", moyskladID), zap.Error(err))
			}
		}
	}
	return nil
}

func extractIDFromHref(href string) string {
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
