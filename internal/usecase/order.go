package usecase

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/cache"
	"github.com/TeleginSergey/hozdacha/internal/db"
	"github.com/TeleginSergey/hozdacha/internal/moysklad"
	"github.com/TeleginSergey/hozdacha/internal/telegram"
)

// DefaultReservationTTL — на сколько товар бронируется для пользователя
// после создания заказа. Если клиент не выкупил в магазине за это время,
// бронь автоматически снимается, сток возвращается, и CustomerOrder в МойСклад удаляется.
const DefaultReservationTTL = 48 * time.Hour

// OrderRepository — контракт работы с заказами.
type OrderRepository interface {
	InsertWithItems(ctx context.Context, order *db.Order, items []db.OrderItem) (*db.Order, error)
	CreateOrderAtomic(ctx context.Context, order *db.Order, items []db.OrderItem, clearCartForUserID int64) (*db.Order, []*db.Product, error)
	Update(ctx context.Context, order *db.Order, id int64) (*db.Order, error)
	GetByUserID(ctx context.Context, userID int64) ([]*db.Order, error)
}

// ProductReadRepository — контракт чтения товаров; совместим с db.ProductQuery.
type ProductReadRepository interface {
	db.ProductQuery
}

type OrderUsecase struct {
	orders      OrderRepository
	products    ProductReadRepository
	stockCache  *cache.StockCache
	moysklad    *moysklad.Client
	telegramBot *telegram.Bot
	logger      *zap.Logger
}

func NewOrderUsecase(
	orderRepo OrderRepository,
	productRepo ProductReadRepository,
	stockCache *cache.StockCache,
	moyskladClient *moysklad.Client,
	telegramBot *telegram.Bot,
	logger *zap.Logger,
) *OrderUsecase {
	return &OrderUsecase{
		orders:      orderRepo,
		products:    productRepo,
		stockCache:  stockCache,
		moysklad:    moyskladClient,
		telegramBot: telegramBot,
		logger:      logger,
	}
}

// Локальные типы запроса повторяют структуру services.CreateOrderRequest,
// чтобы не тянуть сервисный слой внутрь usecase и избежать циклов импорта.
type CreateOrderItem struct {
	ProductID int64 `json:"product_id"`
	Quantity  int   `json:"quantity"`
}

type CreateOrderRequest struct {
	UserID       int64             `json:"user_id"`
	CustomerName string            `json:"customer_name"`
	Phone        string            `json:"phone"`
	Address      *string           `json:"address"`
	Comment      *string           `json:"comment"`
	Items        []CreateOrderItem `json:"items"`
}

func (u *OrderUsecase) CreateOrder(ctx context.Context, req CreateOrderRequest) (*db.Order, error) {
	// Простая локальная валидация (чтобы не тянуть сервисы и не создавать циклы импорта)
	if req.CustomerName == "" {
		return nil, fmt.Errorf("customer name is required")
	}
	if req.Phone == "" {
		return nil, fmt.Errorf("phone is required")
	}
	if len(req.Items) == 0 {
		return nil, fmt.Errorf("at least one item is required")
	}

	var totalPrice float64
	items := make([]db.OrderItem, 0, len(req.Items))

	// Предварительно подгружаем товары только чтобы зафиксировать цену.
	// Финальная проверка стока и его атомарное списание происходят в БД.
	for _, item := range req.Items {
		if item.Quantity <= 0 {
			return nil, fmt.Errorf("invalid quantity for product %d", item.ProductID)
		}
		product, err := u.products.GetByID(ctx, item.ProductID)
		if err != nil {
			return nil, fmt.Errorf("failed to get product %d: %w", item.ProductID, err)
		}
		if product == nil {
			return nil, fmt.Errorf("product %d not found", item.ProductID)
		}
		if !product.Active {
			return nil, fmt.Errorf("product %d is not active", item.ProductID)
		}

		itemPrice := product.Price * float64(item.Quantity)
		totalPrice += itemPrice

		items = append(items, db.OrderItem{
			ProductID: item.ProductID,
			Quantity:  item.Quantity,
			Price:     product.Price,
		})
	}

	reservedUntil := time.Now().Add(DefaultReservationTTL)
	order := &db.Order{
		UserID:        &req.UserID,
		Status:        "pending",
		TotalPrice:    totalPrice,
		CustomerName:  req.CustomerName,
		Phone:         req.Phone,
		Address:       req.Address,
		Comment:       req.Comment,
		ReservedUntil: &reservedUntil,
	}

	order, updatedProducts, err := u.orders.CreateOrderAtomic(ctx, order, items, req.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to create order: %w", err)
	}

	// После коммита обновляем Redis-кэш остатков и сбрасываем резервы
	// best-effort: если Redis не отвечает — заказ всё равно создан корректно.
	if u.stockCache != nil {
		for i, p := range updatedProducts {
			moyskladID := ""
			if p.MoyskladID != nil {
				moyskladID = *p.MoyskladID
			}
			if err := u.stockCache.SetStock(ctx, p.ID, moyskladID, p.Stock); err != nil {
				u.logger.Warn("Failed to refresh stock cache after order", zap.Int64("product_id", p.ID), zap.Error(err))
			}
			if err := u.stockCache.ReleaseStock(ctx, p.ID, items[i].Quantity); err != nil {
				u.logger.Warn("Failed to release reservation after order", zap.Int64("product_id", p.ID), zap.Error(err))
			}
		}
	}

	// Telegram-уведомление
	if u.telegramBot != nil {
		if err := u.telegramBot.SendOrderNotification(ctx, order); err != nil {
			u.logger.Warn("Failed to send telegram notification", zap.Error(err))
		}
	}

	// Создание заказа в МойСклад
	if u.moysklad != nil {
		moyskladID, err := u.moysklad.CreateOrderFromDB(ctx, order, u.products)
		if err != nil {
			u.logger.Warn("Failed to create order in Moysklad", zap.Error(err))
		} else if moyskladID != nil {
			order.MoyskladID = moyskladID
			if _, err := u.orders.Update(ctx, order, order.ID); err != nil {
				u.logger.Warn("Failed to update order with Moysklad ID", zap.Error(err))
			}
		}
	}

	return order, nil
}

func (u *OrderUsecase) GetUserOrders(ctx context.Context, userID int64) ([]*db.Order, error) {
	orders, err := u.orders.GetByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user orders: %w", err)
	}
	return orders, nil
}
