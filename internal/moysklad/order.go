package moysklad

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

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
		Organization: &MoyskladEntity{
			Meta: &OrderMeta{
				Href: fmt.Sprintf("%s/entity/organization/%s", c.baseURL, c.organizationID),
				Type: "organization",
			},
		},
		Agent: &MoyskladEntity{
			Meta: &OrderMeta{
				Href: fmt.Sprintf("%s/entity/counterparty/%s", c.baseURL, c.agentID),
				Type: "counterparty",
			},
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
			Assortment: &MoyskladEntity{
				Meta: &OrderMeta{
					Href: fmt.Sprintf("%s/entity/product/%s", c.baseURL, *product.MoyskladID),
					Type: "product",
				},
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

// CreateOrderFromRequest создаёт CustomerOrder в МойСклад на основе данных запроса (до сохранения в БД).
func (c *Client) CreateOrderFromRequest(ctx context.Context, name, customerName, phone string, comment *string, items []db.OrderItem, productQuery db.ProductQuery) (*string, error) {
	if !c.HasOrderDefaults() {
		return nil, fmt.Errorf("moysklad organization/agent IDs are not configured")
	}

	description := fmt.Sprintf("Покупатель: %s, тел: %s", customerName, phone)
	if comment != nil && *comment != "" {
		description += "\nКомментарий: " + *comment
	}

	moyskladOrder := &MoyskladOrder{
		Name:      name,
		Positions: make([]MoyskladPosition, 0, len(items)),
		Organization: &MoyskladEntity{
			Meta: &OrderMeta{
				Href: fmt.Sprintf("%s/entity/organization/%s", c.baseURL, c.organizationID),
				Type: "organization",
			},
		},
		Agent: &MoyskladEntity{
			Meta: &OrderMeta{
				Href: fmt.Sprintf("%s/entity/counterparty/%s", c.baseURL, c.agentID),
				Type: "counterparty",
			},
		},
		Description: description,
	}

	for _, item := range items {
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
			Reserve:  item.Quantity,
			Price:    item.Price * 100,
			Assortment: &MoyskladEntity{
				Meta: &OrderMeta{
					Href: fmt.Sprintf("%s/entity/product/%s", c.baseURL, *product.MoyskladID),
					Type: "product",
				},
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

// UpdateOrderName обновляет имя заказа в МойСклад.
func (c *Client) UpdateOrderName(ctx context.Context, moyskladID, name string) error {
	url := fmt.Sprintf("%s/entity/customerorder/%s", c.baseURL, moyskladID)
	body := fmt.Sprintf(`{"name":"%s"}`, name)
	req, err := http.NewRequestWithContext(ctx, "PUT", url, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to update order name: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("moysklad API error: %s, body: %s", resp.Status, string(respBody))
	}
	return nil
}
