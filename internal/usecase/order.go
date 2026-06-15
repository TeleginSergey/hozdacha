package usecase

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/cache"
	"github.com/TeleginSergey/hozdacha/internal/db"
	"github.com/TeleginSergey/hozdacha/internal/moysklad"
)

// reservedUntilFor возвращает момент истечения брони — 03:00 через 3 календарных дня.
// Пример: заказ создан 21 мая в 22:30 → бронь до 24 мая 03:00.
func reservedUntilFor(now time.Time) time.Time {
	d := now.AddDate(0, 0, 3)
	return time.Date(d.Year(), d.Month(), d.Day(), 3, 0, 0, 0, now.Location())
}

// OrderRepository — контракт работы с заказами.
type OrderRepository interface {
	InsertWithItems(ctx context.Context, order *db.Order, items []db.OrderItem) (*db.Order, error)
	CreateOrderAtomic(ctx context.Context, order *db.Order, items []db.OrderItem, clearCartForUserID int64) (*db.Order, []*db.Product, error)
	CancelPendingOrderAtomic(ctx context.Context, orderID int64, targetStatus string, ownerUserID *int64) (*string, bool, []*db.Product, error)
	CompleteOrder(ctx context.Context, orderID int64) (bool, error)
	GetByID(ctx context.Context, id int64) (*db.Order, error)
	Update(ctx context.Context, order *db.Order, id int64) (*db.Order, error)
	GetByUserID(ctx context.Context, userID int64) ([]*db.Order, error)
	GetItemsByOrderID(ctx context.Context, orderID int64) ([]*db.OrderItem, error)
}

// ProductReadRepository — контракт чтения товаров; совместим с db.ProductQuery.
type ProductReadRepository interface {
	db.ProductQuery
}

type OrderUsecase struct {
	orders     OrderRepository
	products   ProductReadRepository
	stockCache *cache.StockCache
	moysklad   *moysklad.Client
	events     db.OrderEventQuery
	pricer     *PromotionPricer
	logger     *zap.Logger
}

func NewOrderUsecase(
	orderRepo OrderRepository,
	productRepo ProductReadRepository,
	stockCache *cache.StockCache,
	moyskladClient *moysklad.Client,
	events db.OrderEventQuery,
	logger *zap.Logger,
) *OrderUsecase {
	return &OrderUsecase{
		orders:     orderRepo,
		products:   productRepo,
		stockCache: stockCache,
		moysklad:   moyskladClient,
		events:     events,
		logger:     logger,
	}
}

func (u *OrderUsecase) SetPromotionPricer(p *PromotionPricer) {
	u.pricer = p
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
	PickupAt     *time.Time        `json:"pickup_at"`
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

	// Если МойСклад настроен но не сконфигурирован до конца — не создаём заказ.
	// Локальный заказ без синхронизации с МойСклад приводит к расхождению остатков.
	if u.moysklad != nil && !u.moysklad.HasOrderDefaults() {
		return nil, fmt.Errorf("moysklad integration is not fully configured (missing organization/agent IDs)")
	}

	now := time.Now()

	productIDs := make([]int64, 0, len(req.Items))
	for _, item := range req.Items {
		productIDs = append(productIDs, item.ProductID)
	}

	// Окно акций: если в заказе есть дневная акция, уже не актуальная для
	// текущего окна бронирования — блокируем заказ целиком.
	// dayPromoDeadline != nil — в заказе есть актуальная дневная акция, и забрать
	// товар нужно до закрытия магазина в день брони (нельзя «на 2 дня вперёд»).
	var dayPromoDeadline *time.Time
	if u.pricer != nil {
		if err := u.pricer.CheckDayPromotionWindow(ctx, productIDs, now); err != nil {
			return nil, err
		}
		if hasDay, deadline, err := u.pricer.DayPromotionDeadline(ctx, productIDs, now); err == nil && hasDay {
			dayPromoDeadline = &deadline
		}
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

	// Обычная бронь живёт 3 дня. Для дневной акции она ограничена днём акции:
	// после закрытия магазина сток возвращается, а забрать товар нужно в этот день.
	reservedUntil := reservedUntilFor(now)
	if dayPromoDeadline != nil {
		reservedUntil = *dayPromoDeadline
	}

	// Валидация желаемого времени визита.
	if req.PickupAt != nil {
		if !req.PickupAt.After(now) {
			return nil, fmt.Errorf("pickup time must be in the future")
		}
		if req.PickupAt.After(reservedUntil) {
			if dayPromoDeadline != nil {
				return nil, fmt.Errorf("promotion_pickup_window: акционные товары нужно забрать в день акции, до %s",
					dayPromoDeadline.Format("15:04"))
			}
			return nil, fmt.Errorf("pickup time must be before reservation expires (%s)", reservedUntil.Format(time.RFC3339))
		}
	}

	order := &db.Order{
		UserID:        &req.UserID,
		Status:        "pending",
		TotalPrice:    totalPrice,
		CustomerName:  req.CustomerName,
		Phone:         req.Phone,
		Address:       req.Address,
		Comment:       req.Comment,
		ReservedUntil: &reservedUntil,
		PickupAt:      req.PickupAt,
	}

	// Сначала создаём заказ в МойСклад. Если не получилось — не сохраняем локально ничего.
	var moyskladID *string
	if u.moysklad != nil {
		// Временный ID для имени заказа в МойСклад (заменим после сохранения в БД)
		tmpName := fmt.Sprintf("Online-%d", time.Now().UnixNano()%100000)
		mid, err := u.moysklad.CreateOrderFromRequest(ctx, tmpName, req.CustomerName, req.Phone, req.Comment, items, u.products)
		if err != nil {
			return nil, fmt.Errorf("failed to create order in Moysklad: %w", err)
		}
		moyskladID = mid
	}

	order.MoyskladID = moyskladID
	order, updatedProducts, err := u.orders.CreateOrderAtomic(ctx, order, items, req.UserID)
	if err != nil {
		// Если локальное сохранение не удалось — пытаемся удалить заказ из МойСклад (best-effort)
		if moyskladID != nil && u.moysklad != nil {
			if delErr := u.moysklad.DeleteCustomerOrder(ctx, *moyskladID); delErr != nil {
				u.logger.Error("Failed to rollback Moysklad order after local save failure",
					zap.Stringp("moysklad_id", moyskladID), zap.Error(delErr))
			}
		}
		return nil, fmt.Errorf("failed to create order: %w", err)
	}

	// Обновляем имя заказа в МойСклад на реальный ID
	if moyskladID != nil && u.moysklad != nil {
		newName := fmt.Sprintf("Online-%d", order.ID)
		if err := u.moysklad.UpdateOrderName(ctx, *moyskladID, newName); err != nil {
			u.logger.Warn("Failed to update Moysklad order name", zap.Stringp("moysklad_id", moyskladID), zap.Error(err))
		}
	}
	// Audit log.
	u.recordEvent(ctx, order.ID, db.OrderEventCreated, &req.UserID, map[string]any{
		"items_count": len(items),
		"total_price": order.TotalPrice,
	})

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

	// МойСклад уже создан на предыдущем шаге — логируем успех.
	if moyskladID != nil {
		u.logger.Info("Order synced to Moysklad",
			zap.Int64("order_id", order.ID),
			zap.Stringp("moysklad_id", moyskladID))
		u.recordEvent(ctx, order.ID, db.OrderEventMoyskladSynced, nil, map[string]any{
			"moysklad_id": *moyskladID,
		})
	}

	return order, nil
}

// CancelOrderByUser отменяет pending-заказ пользователя: возвращает товары на склад
// и удаляет CustomerOrder в МойСклад. Заказ остаётся в БД со статусом 'cancelled' для истории.
func (u *OrderUsecase) CancelOrderByUser(ctx context.Context, userID, orderID int64) error {
	return u.cancelOrder(ctx, orderID, "cancelled", &userID, &userID)
}

// CancelOrderByAdmin отменяет заказ от имени администратора: возвращает товары,
// удаляет CustomerOrder в МойСклад, ставит статус 'cancelled'.
func (u *OrderUsecase) CancelOrderByAdmin(ctx context.Context, orderID int64, adminUserID *int64) error {
	return u.cancelOrder(ctx, orderID, "cancelled", nil, adminUserID)
}

// ExpireOrderByAdmin — ручной триггер истечения брони (форсирует то, что обычно делает cron).
// adminUserID опционально — если задан, фиксируется в audit log как actor.
func (u *OrderUsecase) ExpireOrderByAdmin(ctx context.Context, orderID int64, adminUserID *int64) error {
	return u.cancelOrder(ctx, orderID, "expired", nil, adminUserID)
}

// recordEvent — best-effort запись в audit log. Не должна ломать основной поток.
func (u *OrderUsecase) recordEvent(ctx context.Context, orderID int64, eventType string, actorUserID *int64, payload any) {
	if u.events == nil {
		return
	}
	if err := u.events.Insert(ctx, orderID, eventType, actorUserID, payload); err != nil {
		u.logger.Warn("Failed to record order event",
			zap.Int64("order_id", orderID),
			zap.String("event_type", eventType),
			zap.Error(err))
	}
}

func (u *OrderUsecase) cancelOrder(ctx context.Context, orderID int64, targetStatus string, ownerUserID, actorUserID *int64) error {
	moyskladID, alreadyHandled, restored, err := u.orders.CancelPendingOrderAtomic(ctx, orderID, targetStatus, ownerUserID)
	if err != nil {
		return fmt.Errorf("failed to cancel order: %w", err)
	}
	if alreadyHandled {
		// Заказ уже не pending — считаем идемпотентной операцией.
		return nil
	}

	// Audit log: фиксируем кто и почему отменил.
	eventType := db.OrderEventCancelled
	if targetStatus == "expired" {
		eventType = db.OrderEventExpired
	}
	u.recordEvent(ctx, orderID, eventType, actorUserID, map[string]any{
		"target_status": targetStatus,
	})

	// Обновляем Redis-кэш остатков (best effort).
	if u.stockCache != nil {
		for _, p := range restored {
			msID := ""
			if p.MoyskladID != nil {
				msID = *p.MoyskladID
			}
			if err := u.stockCache.SetStock(ctx, p.ID, msID, p.Stock); err != nil {
				u.logger.Warn("Failed to refresh stock cache after order cancel",
					zap.Int64("product_id", p.ID), zap.Error(err))
			}
		}
	}

	// Удаляем заказ в МойСклад (best effort). Если упадёт — БД уже консистентна,
	// админ почистит вручную или MoyskladID останется висеть.
	if moyskladID != nil && *moyskladID != "" && u.moysklad != nil {
		delCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		if err := u.moysklad.DeleteCustomerOrder(delCtx, *moyskladID); err != nil {
			u.logger.Warn("Failed to delete order in Moysklad",
				zap.Int64("order_id", orderID),
				zap.Stringp("moysklad_id", moyskladID),
				zap.Error(err))
		}
	}

	u.logger.Info("Order cancelled",
		zap.Int64("order_id", orderID),
		zap.String("status", targetStatus))
	return nil
}

// CompleteOrder помечает заказ как выкупленный в магазине.
// Сток в БД не возвращается (товар продан). Резерв в МойСклад снимает продавец отгрузкой.
// adminUserID опционально — для записи в audit log кто из админов нажал кнопку.
func (u *OrderUsecase) CompleteOrder(ctx context.Context, orderID int64, adminUserID *int64) error {
	alreadyHandled, err := u.orders.CompleteOrder(ctx, orderID)
	if err != nil {
		return fmt.Errorf("failed to complete order: %w", err)
	}
	if alreadyHandled {
		// Уже не pending — идемпотентно.
		return nil
	}
	u.recordEvent(ctx, orderID, db.OrderEventShipped, adminUserID, nil)
	u.logger.Info("Order completed by admin", zap.Int64("order_id", orderID))
	return nil
}

func (u *OrderUsecase) GetUserOrders(ctx context.Context, userID int64) ([]*db.Order, error) {
	orders, err := u.orders.GetByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user orders: %w", err)
	}
	for _, o := range orders {
		items, err := u.orders.GetItemsByOrderID(ctx, o.ID)
		if err != nil {
			u.logger.Warn("Failed to load items for order", zap.Int64("order_id", o.ID), zap.Error(err))
			continue
		}
		for _, it := range items {
			product, err := u.products.GetByID(ctx, it.ProductID)
			if err == nil && product != nil {
				it.ProductName = product.Name
			}
			o.Items = append(o.Items, *it)
		}
	}
	return orders, nil
}
