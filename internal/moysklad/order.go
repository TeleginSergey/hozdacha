package moysklad

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/db"
)

type OrderMeta struct {
	Href string `json:"href"`
	Type string `json:"type"`
}

// CreateOrderFromDB создаёт CustomerOrder в МойСклад на основе локального заказа из БД.
// Цены в МойСклад — в копейках (×100), поле reserve на каждой позиции бронирует товар.
// Возвращает ID созданного заказа в МойСклад (для последующего удаления при экспирации).
func (c *Client) CreateOrderFromDB(ctx context.Context, order *db.Order, productQuery db.ProductQuery) (*string, error) {
	if !c.HasOrderDefaults() {
		return nil, fmt.Errorf("moysklad organization/agent IDs are not configured; set MOYSKLAD_ORGANIZATION_ID and MOYSKLAD_AGENT_ID")
	}

	description := fmt.Sprintf("Заказ #%d (онлайн). Покупатель: %s, тел: %s",
		order.ID, order.CustomerName, order.Phone)
	if order.Comment != nil && *order.Comment != "" {
		description += "\nКомментарий: " + *order.Comment
	}

	moyskladOrder := &MoyskladOrder{
		Name:      fmt.Sprintf("Online-%d", order.ID),
		Positions: make([]MoyskladPosition, 0, len(order.Items)),
		Organization: &OrderMeta{
			Href: fmt.Sprintf("%s/entity/organization/%s", c.baseURL, c.organizationID),
			Type: "organization",
		},
		Agent: &OrderMeta{
			Href: fmt.Sprintf("%s/entity/counterparty/%s", c.baseURL, c.agentID),
			Type: "counterparty",
		},
		Description: description,
	}

	for _, item := range order.Items {
		product, err := productQuery.GetByID(ctx, item.ProductID)
		if err != nil || product == nil {
			c.logger.Warn("Failed to get product for Moysklad order",
				zap.Int64("product_id", item.ProductID), zap.Error(err))
			continue
		}
		if product.MoyskladID == nil || *product.MoyskladID == "" {
			c.logger.Warn("Product has no Moysklad ID, skipping",
				zap.Int64("product_id", item.ProductID))
			continue
		}

		moyskladOrder.Positions = append(moyskladOrder.Positions, MoyskladPosition{
			Quantity: item.Quantity,
			Reserve:  item.Quantity, // ← это и делает заказ "бронью": товар уйдёт в резерв на складе
			// API МойСклад принимает цену в копейках.
			Price: item.Price * 100,
			Assortment: &OrderMeta{
				Href: fmt.Sprintf("%s/entity/product/%s", c.baseURL, *product.MoyskladID),
				Type: "product",
			},
		})
	}

	if len(moyskladOrder.Positions) == 0 {
		return nil, fmt.Errorf("no valid positions for Moysklad order")
	}

	created, err := c.CreateCustomerOrder(ctx, moyskladOrder)
	if err != nil {
		return nil, fmt.Errorf("failed to create customer order in Moysklad: %w", err)
	}
	if created.ID == "" {
		return nil, fmt.Errorf("moysklad returned empty order id")
	}
	return &created.ID, nil
}
