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

func (c *Client) CreateOrderFromDB(ctx context.Context, order *db.Order, productQuery db.ProductQuery) (*string, error) {
	moyskladOrder := &MoyskladOrder{
		Name:      fmt.Sprintf("Заказ #%d от %s", order.ID, order.CustomerName),
		Positions: make([]MoyskladPosition, 0, len(order.Items)),
	}

	// Преобразуем товары заказа в позиции МойСклад
	for _, item := range order.Items {
		// Получаем товар для получения MoyskladID
		product, err := productQuery.GetByID(ctx, item.ProductID)
		if err != nil || product == nil {
			c.logger.Warn("Failed to get product for Moysklad order",
				zap.Int64("product_id", item.ProductID), zap.Error(err))
			continue
		}

		position := MoyskladPosition{
			Quantity: item.Quantity,
			Price:    item.Price,
		}

		// Устанавливаем meta ссылку на товар в МойСклад, если есть MoyskladID
		if product.MoyskladID != nil && *product.MoyskladID != "" {
			position.Assortment = &OrderMeta{
				Href: fmt.Sprintf("%s/entity/product/%s", c.baseURL, *product.MoyskladID),
				Type: "product",
			}
		} else {
			c.logger.Warn("Product has no Moysklad ID, skipping", zap.Int64("product_id", item.ProductID))
			continue
		}

		moyskladOrder.Positions = append(moyskladOrder.Positions, position)
	}

	if len(moyskladOrder.Positions) == 0 {
		return nil, fmt.Errorf("no valid positions for Moysklad order")
	}

	return c.CreateOrder(ctx, moyskladOrder)
}
