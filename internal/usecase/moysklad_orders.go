package usecase

import (
	"context"
	"fmt"
	"time"

	"github.com/TeleginSergey/hozdacha/internal/db"
	"github.com/TeleginSergey/hozdacha/internal/moysklad"
	"go.uber.org/zap"
)

type MoyskladOrderUsecase struct {
	cartQuery      db.CartItemQuery
	productQuery   db.ProductQuery
	orderQuery     db.OrderQuery
	moyskladClient *moysklad.Client
	logger         *zap.Logger
}

func NewMoyskladOrderUsecase(
	cartQuery db.CartItemQuery,
	productQuery db.ProductQuery,
	orderQuery db.OrderQuery,
	moyskladClient *moysklad.Client,
	logger *zap.Logger,
) *MoyskladOrderUsecase {
	return &MoyskladOrderUsecase{
		cartQuery:      cartQuery,
		productQuery:   productQuery,
		orderQuery:     orderQuery,
		moyskladClient: moyskladClient,
		logger:         logger,
	}
}

type MoyskladMoyskladCreateOrderRequest struct {
	UserID int64 `json:"user_id" binding:"required"`
}

type MoyskladOrderResponse struct {
	OrderID    string `json:"order_id"`
	MoyskladID string `json:"moysklad_id"`
	Status     string `json:"status"`
	Message    string `json:"message"`
}

func (m *MoyskladOrderUsecase) CreateOrder(ctx context.Context, req *CreateOrderRequest) (*MoyskladOrderResponse, error) {
	// Получаем товары из корзины
	cartItems, err := m.cartQuery.GetByUserID(ctx, req.UserID)
	if err != nil {
		m.logger.Error("Failed to get cart items", zap.Error(err))
		return nil, fmt.Errorf("failed to get cart items: %w", err)
	}

	if len(cartItems) == 0 {
		return nil, fmt.Errorf("cart is empty")
	}

	// Проверяем наличие товаров
	for _, item := range cartItems {
		product, err := m.productQuery.GetByID(ctx, item.ProductID)
		if err != nil {
			m.logger.Error("Failed to get product", zap.Error(err), zap.Int64("product_id", item.ProductID))
			return nil, fmt.Errorf("failed to get product: %w", err)
		}
		if product == nil {
			return nil, fmt.Errorf("product %d not found", item.ProductID)
		}
		if product.Stock < item.Quantity {
			return nil, fmt.Errorf("insufficient stock for product %s", product.Name)
		}
	}

	// Создаем заказ в нашей базе
	order := &db.Order{
		UserID:       &req.UserID,
		Status:       "pending",
		TotalPrice:   0,                    // Заполним ниже
		CustomerName: "Покупатель с сайта", // Временно, можно будет брать из профиля
		Phone:        "",                   // Временно, можно будет брать из профиля
		Address:      nil,                  // Временно, можно будет брать из профиля
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	// Рассчитываем общую сумму
	var totalPrice float64
	for _, item := range cartItems {
		product, _ := m.productQuery.GetByID(ctx, item.ProductID)
		if product != nil {
			totalPrice += float64(item.Quantity) * product.Price
		}
	}
	order.TotalPrice = totalPrice

	// Сохраняем заказ в базу
	createdOrder, err := m.orderQuery.Insert(ctx, order)
	if err != nil {
		m.logger.Error("Failed to create order", zap.Error(err))
		return nil, fmt.Errorf("failed to create order: %w", err)
	}

	// Создаем заказ в МойСклад
	moyskladOrder, err := m.createMoyskladOrder(ctx, cartItems, createdOrder.ID)
	if err != nil {
		m.logger.Error("Failed to create Moysklad order", zap.Error(err))
		// Отмечаем заказ как ошибочный, но не удаляем
		createdOrder.Status = "error"
		_, err = m.orderQuery.Update(ctx, createdOrder, createdOrder.ID)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("failed to create Moysklad order: %w", err)
	}

	// Обновляем статус заказа
	createdOrder.Status = "processing"
	createdOrder.MoyskladID = &moyskladOrder.ID
	_, err = m.orderQuery.Update(ctx, createdOrder, createdOrder.ID)
	if err != nil {
		m.logger.Error("Failed to update order with Moysklad ID", zap.Error(err))
		return nil, fmt.Errorf("failed to update order: %w", err)
	}

	// Очищаем корзину
	err = m.cartQuery.Clear(ctx, req.UserID)
	if err != nil {
		m.logger.Error("Failed to clear cart", zap.Error(err))
		// Не возвращаем ошибку, так как заказ уже создан
	}

	return &MoyskladOrderResponse{
		OrderID:    fmt.Sprintf("%d", createdOrder.ID),
		MoyskladID: moyskladOrder.ID,
		Status:     "processing",
		Message:    "Заказ успешно создан и отправлен в МойСклад",
	}, nil
}

func (m *MoyskladOrderUsecase) createMoyskladOrder(ctx context.Context, cartItems []*db.CartItem, orderID int64) (*moysklad.MoyskladOrder, error) {
	// Получаем информацию об организации и контрагенте
	orgs, err := m.moyskladClient.GetOrganizations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get organizations: %w", err)
	}
	if len(orgs.Rows) == 0 {
		return nil, fmt.Errorf("no organizations found")
	}

	agents, err := m.moyskladClient.GetCounterparties(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get counterparties: %w", err)
	}
	if len(agents.Rows) == 0 {
		return nil, fmt.Errorf("no counterparties found")
	}

	// Создаем позиции заказа
	var positions []moysklad.MoyskladPosition
	for _, item := range cartItems {
		product, err := m.productQuery.GetByID(ctx, item.ProductID)
		if err != nil || product == nil {
			continue
		}

		position := moysklad.MoyskladPosition{
			Quantity: item.Quantity,
			Price:    product.Price * 100, // В МойСклад цена в копейках
			Assortment: &moysklad.MoyskladEntity{
				Meta: &moysklad.OrderMeta{
					Href: fmt.Sprintf("%s/entity/product/%s", m.moyskladClient.GetBaseURL(), *product.MoyskladID),
					Type: "product",
				},
			},
		}
		positions = append(positions, position)
	}

	// Создаем заказ в МойСклад
	moyskladOrder := &moysklad.MoyskladOrder{
		Name: fmt.Sprintf("Заказ №%d от %s", orderID, time.Now().Format("2006-01-02 15:04:05")),
		Organization: &moysklad.MoyskladEntity{
			Meta: &moysklad.OrderMeta{
				Href: orgs.Rows[0].Href,
				Type: "organization",
			},
		},
		Agent: &moysklad.MoyskladEntity{
			Meta: &moysklad.OrderMeta{
				Href: agents.Rows[0].Href,
				Type: "counterparty",
			},
		},
		Positions: positions,
	}

	createdOrder, err := m.moyskladClient.CreateCustomerOrder(ctx, moyskladOrder)
	if err != nil {
		return nil, fmt.Errorf("failed to create customer order: %w", err)
	}

	return createdOrder, nil
}

func (m *MoyskladOrderUsecase) GetOrderStatus(ctx context.Context, userID, orderID int64) (*db.Order, error) {
	order, err := m.orderQuery.GetByID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("failed to get order: %w", err)
	}

	// Проверяем что заказ принадлежит пользователю
	if *order.UserID != userID {
		return nil, fmt.Errorf("access denied")
	}

	return order, nil
}
