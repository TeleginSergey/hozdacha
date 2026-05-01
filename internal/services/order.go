package services

import (
	"context"
	"fmt"

	"github.com/TeleginSergey/hozdacha/internal/cache"
	"github.com/TeleginSergey/hozdacha/internal/db"
	"github.com/TeleginSergey/hozdacha/internal/moysklad"
	"github.com/TeleginSergey/hozdacha/internal/telegram"
	"go.uber.org/zap"
)

type OrderService struct {
	orderQuery     db.OrderQuery
	productQuery   db.ProductQuery
	stockCache     *cache.StockCache
	moyskladClient *moysklad.Client
	telegramBot    *telegram.Bot
	logger         *zap.Logger
}

func NewOrderService(
	orderQuery db.OrderQuery,
	productQuery db.ProductQuery,
	stockCache *cache.StockCache,
	moyskladClient *moysklad.Client,
	telegramBot *telegram.Bot,
	logger *zap.Logger,
) *OrderService {
	return &OrderService{
		orderQuery:     orderQuery,
		productQuery:   productQuery,
		stockCache:     stockCache,
		moyskladClient: moyskladClient,
		telegramBot:    telegramBot,
		logger:         logger,
	}
}

func (s *OrderService) StockCache() *cache.StockCache {
	return s.stockCache
}

type CreateOrderRequest struct {
	CustomerName string            `json:"customer_name" binding:"required"`
	Phone        string            `json:"phone" binding:"required"`
	Address      *string           `json:"address"`
	Comment      *string           `json:"comment"`
	Items        []CreateOrderItem `json:"items" binding:"required,min=1"`
}

type CreateOrderItem struct {
	ProductID int64 `json:"product_id" binding:"required"`
	Quantity  int   `json:"quantity" binding:"required,min=1"`
}

func (s *OrderService) CreateOrder(ctx context.Context, req CreateOrderRequest) (*db.Order, error) {
	// Валидация уже выполнена в handler, но проверяем еще раз для безопасности
	if err := ValidateOrderRequest(req); err != nil {
		return nil, err
	}

	// Проверяем товары и считаем общую стоимость
	var totalPrice float64
	items := make([]db.OrderItem, 0, len(req.Items))

	for _, item := range req.Items {
		product, err := s.productQuery.GetByID(ctx, item.ProductID)
		if err != nil {
			return nil, fmt.Errorf("failed to get product %d: %w", item.ProductID, err)
		}
		if product == nil {
			return nil, fmt.Errorf("product %d not found", item.ProductID)
		}
		if !product.Active {
			return nil, fmt.Errorf("product %d is not active", item.ProductID)
		}
		if product.Stock < item.Quantity {
			return nil, fmt.Errorf("insufficient stock for product %d", item.ProductID)
		}

		itemPrice := product.Price * float64(item.Quantity)
		totalPrice += itemPrice

		items = append(items, db.OrderItem{
			ProductID: item.ProductID,
			Quantity:  item.Quantity,
			Price:     product.Price,
		})
	}

	// Создаем заказ
	order := &db.Order{
		Status:       "pending",
		TotalPrice:   totalPrice,
		CustomerName: req.CustomerName,
		Phone:        req.Phone,
		Address:      req.Address,
		Comment:      req.Comment,
	}

	order, err := s.orderQuery.InsertWithItems(ctx, order, items)
	if err != nil {
		return nil, fmt.Errorf("failed to create order: %w", err)
	}

	// Отправляем уведомление в Telegram
	if s.telegramBot != nil {
		err = s.telegramBot.SendOrderNotification(ctx, order)
		if err != nil {
			s.logger.Warn("Failed to send telegram notification", zap.Error(err))
		}
	}

	// Пытаемся создать заказ в МойСклад
	if s.moyskladClient != nil {
		moyskladID, err := s.moyskladClient.CreateOrderFromDB(ctx, order, s.productQuery)
		if err != nil {
			s.logger.Warn("Failed to create order in Moysklad", zap.Error(err))
		} else if moyskladID != nil {
			order.MoyskladID = moyskladID
			_, err = s.orderQuery.Update(ctx, order, order.ID)
			if err != nil {
				s.logger.Warn("Failed to update order with Moysklad ID", zap.Error(err))
			}
		}
	}

	return order, nil
}

// Небольшие геттеры, чтобы OrderHandler мог построить usecase поверх сервиса (переходный этап).
func (s *OrderService) OrderQuery() db.OrderQuery {
	return s.orderQuery
}

func (s *OrderService) ProductQuery() db.ProductQuery {
	return s.productQuery
}

func (s *OrderService) MoyskladClient() *moysklad.Client {
	return s.moyskladClient
}

func (s *OrderService) TelegramBot() *telegram.Bot {
	return s.telegramBot
}
