package usecase

import (
	"context"
	"fmt"
	"time"

	"github.com/TeleginSergey/hozdacha/internal/db"
	"go.uber.org/zap"
)

type CartUsecase struct {
	cartQuery    db.CartItemQuery
	productQuery db.ProductQuery
	stockCache   interface {
		GetAvailableStock(ctx context.Context, productID int64, productQuery db.ProductQuery) (int, error)
		ReserveStock(ctx context.Context, productID int64, quantity int) error
		ReleaseStock(ctx context.Context, productID int64, quantity int) error
		ConsumeReservedStock(ctx context.Context, productID int64, quantity int) error
		SetStock(ctx context.Context, productID int64, moyskladID string, stock int) error
		InvalidateStockCache(ctx context.Context, productID int64) error
	}
	logger *zap.Logger
}

func NewCartUsecase(cartQuery db.CartItemQuery, productQuery db.ProductQuery, stockCache interface {
	GetAvailableStock(ctx context.Context, productID int64, productQuery db.ProductQuery) (int, error)
	ReserveStock(ctx context.Context, productID int64, quantity int) error
	ReleaseStock(ctx context.Context, productID int64, quantity int) error
	ConsumeReservedStock(ctx context.Context, productID int64, quantity int) error
	SetStock(ctx context.Context, productID int64, moyskladID string, stock int) error
	InvalidateStockCache(ctx context.Context, productID int64) error
}, logger *zap.Logger) *CartUsecase {
	return &CartUsecase{
		cartQuery:    cartQuery,
		productQuery: productQuery,
		stockCache:   stockCache,
		logger:       logger,
	}
}

type AddToCartRequest struct {
	UserID    int64 `json:"user_id" binding:"required"`
	ProductID int64 `json:"product_id" binding:"required"`
	Quantity  int   `json:"quantity" binding:"required,min=1"`
}

type UpdateCartRequest struct {
	UserID    int64 `json:"user_id" binding:"required"`
	ProductID int64 `json:"product_id" binding:"required"`
	Quantity  int   `json:"quantity" binding:"required,min=1"`
}

type CartResponse struct {
	ID        int64       `json:"id"`
	ProductID int64       `json:"product_id"`
	Quantity  int         `json:"quantity"`
	Product   *db.Product `json:"product,omitempty"`
	CreatedAt string      `json:"created_at"`
}

func (c *CartUsecase) AddToCart(ctx context.Context, req *AddToCartRequest) error {
	// Проверяем существует ли товар
	product, err := c.productQuery.GetByID(ctx, req.ProductID)
	if err != nil {
		return fmt.Errorf("failed to get product: %w", err)
	}
	if product == nil {
		return fmt.Errorf("product not found")
	}

	// Проверяем доступный остаток через кэш
	availableStock, err := c.stockCache.GetAvailableStock(ctx, req.ProductID, c.productQuery)
	if err != nil {
		return fmt.Errorf("failed to get available stock: %w", err)
	}

	// Проверяем остаток
	if availableStock < req.Quantity {
		return fmt.Errorf("insufficient stock for product %s (available: %d, requested: %d)", product.Name, availableStock, req.Quantity)
	}

	// Проверяем есть ли уже товар в корзине
	existingItem, err := c.cartQuery.GetByUserIDAndProductID(ctx, req.UserID, req.ProductID)
	if err != nil {
		return fmt.Errorf("failed to check existing cart item: %w", err)
	}
	if existingItem != nil {
		newQuantity := existingItem.Quantity + req.Quantity
		if availableStock < req.Quantity {
			return fmt.Errorf("insufficient stock for product %s (available: %d, requested: %d)", product.Name, availableStock, req.Quantity)
		}
		if err := c.stockCache.ReserveStock(ctx, req.ProductID, req.Quantity); err != nil {
			return fmt.Errorf("failed to reserve additional stock: %w", err)
		}
		if err := c.cartQuery.UpdateQuantity(ctx, req.UserID, req.ProductID, newQuantity); err != nil {
			c.stockCache.ReleaseStock(ctx, req.ProductID, req.Quantity)
			return fmt.Errorf("failed to update cart item: %w", err)
		}
		return nil
	}

	// Добавляем товар в корзину с резервированием
	cartItem := &db.CartItem{
		UserID:    req.UserID,
		ProductID: req.ProductID,
		Quantity:  req.Quantity,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Резервируем товар в кэше остатков
	if err := c.stockCache.ReserveStock(ctx, req.ProductID, req.Quantity); err != nil {
		return fmt.Errorf("failed to reserve stock for product %d: %w", req.ProductID, err)
	}

	err = c.cartQuery.Create(ctx, cartItem)
	if err != nil {
		// При ошибке освобождаем резервирование
		c.stockCache.ReleaseStock(ctx, req.ProductID, req.Quantity)
		return fmt.Errorf("failed to create cart item: %w", err)
	}

	c.logger.Info("Product added to cart with reservation",
		zap.Int64("user_id", req.UserID),
		zap.Int64("product_id", req.ProductID),
		zap.Int("quantity", req.Quantity),
		zap.Int("available_stock", availableStock))

	return nil
}

func (c *CartUsecase) GetCart(ctx context.Context, userID int64) ([]*CartResponse, error) {
	cartItems, err := c.cartQuery.GetByUserID(ctx, userID)
	if err != nil {
		c.logger.Error("Failed to get cart items", zap.Error(err))
		return nil, fmt.Errorf("failed to get cart items: %w", err)
	}

	var responses []*CartResponse
	for _, item := range cartItems {
		// Получаем информацию о товаре
		product, err := c.productQuery.GetByID(ctx, item.ProductID)
		if err != nil {
			c.logger.Error("Failed to get product for cart item", zap.Error(err), zap.Int64("product_id", item.ProductID))
			continue
		}

		responses = append(responses, &CartResponse{
			ID:        item.ID,
			ProductID: item.ProductID,
			Quantity:  item.Quantity,
			Product:   product,
			CreatedAt: item.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}

	return responses, nil
}

func (c *CartUsecase) UpdateCart(ctx context.Context, req *UpdateCartRequest) error {
	// Проверяем существует ли товар
	product, err := c.productQuery.GetByID(ctx, req.ProductID)
	if err != nil {
		return fmt.Errorf("failed to get product: %w", err)
	}
	if product == nil {
		return fmt.Errorf("product not found")
	}

	// Проверяем доступный остаток через кэш
	availableStock, err := c.stockCache.GetAvailableStock(ctx, req.ProductID, c.productQuery)
	if err != nil {
		return fmt.Errorf("failed to get available stock: %w", err)
	}

	// Проверяем остаток
	if availableStock < req.Quantity {
		return fmt.Errorf("insufficient stock for product %s (available: %d, requested: %d)", product.Name, availableStock, req.Quantity)
	}

	// Проверяем есть ли уже товар в корзине
	existingItem, err := c.cartQuery.GetByUserIDAndProductID(ctx, req.UserID, req.ProductID)
	if err != nil {
		return fmt.Errorf("failed to check existing cart item: %w", err)
	}
	if existingItem == nil {
		return fmt.Errorf("item not found in cart")
	}

	// Обновляем товар в корзине с резервированием
	newQuantity := req.Quantity
	maxAllowed := availableStock + existingItem.Quantity
	if newQuantity > maxAllowed {
		return fmt.Errorf("insufficient stock for product %s (available: %d, requested: %d)", product.Name, maxAllowed, newQuantity)
	}

	// Вычисляем разницу для резервирования/освобождения
	quantityDiff := newQuantity - existingItem.Quantity

	if quantityDiff > 0 {
		// Нужно зарезервировать дополнительно
		if err := c.stockCache.ReserveStock(ctx, req.ProductID, quantityDiff); err != nil {
			return fmt.Errorf("failed to reserve additional stock: %w", err)
		}
	} else if quantityDiff < 0 {
		// Нужно освободить часть резерва
		if err := c.stockCache.ReleaseStock(ctx, req.ProductID, -quantityDiff); err != nil {
			return fmt.Errorf("failed to release stock: %w", err)
		}
	}

	err = c.cartQuery.UpdateQuantity(ctx, existingItem.UserID, existingItem.ProductID, newQuantity)
	if err != nil {
		// При ошибке откатываем резервирование
		if quantityDiff > 0 {
			c.stockCache.ReleaseStock(ctx, req.ProductID, quantityDiff)
		} else if quantityDiff < 0 {
			c.stockCache.ReserveStock(ctx, req.ProductID, -quantityDiff)
		}
		return fmt.Errorf("failed to update cart item: %w", err)
	}

	c.logger.Info("Cart item updated with stock adjustment",
		zap.Int64("user_id", req.UserID),
		zap.Int64("product_id", req.ProductID),
		zap.Int("old_quantity", existingItem.Quantity),
		zap.Int("new_quantity", newQuantity),
		zap.Int("stock_diff", quantityDiff))

	return nil
}

func (c *CartUsecase) RemoveFromCart(ctx context.Context, userID, productID int64) error {
	// Получаем информацию о товаре в корзине для освобождения резерва
	cartItem, err := c.cartQuery.GetByUserIDAndProductID(ctx, userID, productID)
	if err != nil {
		return fmt.Errorf("failed to get cart item: %w", err)
	}
	if cartItem == nil {
		return fmt.Errorf("item not found in cart")
	}

	err = c.cartQuery.Delete(ctx, userID, productID)
	if err != nil {
		c.logger.Error("Failed to remove from cart", zap.Error(err))
		return fmt.Errorf("failed to remove from cart: %w", err)
	}

	// Освобождаем зарезервированный товар
	if err := c.stockCache.ReleaseStock(ctx, productID, cartItem.Quantity); err != nil {
		c.logger.Error("Failed to release stock on cart removal",
			zap.Error(err),
			zap.Int64("product_id", productID),
			zap.Int("quantity", cartItem.Quantity))
		// Не возвращаем ошибку, так как товар уже удален из корзины
	}

	c.logger.Info("Cart item removed with stock release",
		zap.Int64("user_id", userID),
		zap.Int64("product_id", productID),
		zap.Int("released_quantity", cartItem.Quantity))

	return nil
}

func (c *CartUsecase) ClearCart(ctx context.Context, userID int64) error {
	// Получаем все товары в корзине для освобождения резервов
	cartItems, err := c.cartQuery.GetByUserID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get cart items: %w", err)
	}

	err = c.cartQuery.Clear(ctx, userID)
	if err != nil {
		c.logger.Error("Failed to clear cart", zap.Error(err))
		return fmt.Errorf("failed to clear cart: %w", err)
	}

	// Освобождаем все зарезервированные товары
	for _, item := range cartItems {
		if releaseErr := c.stockCache.ReleaseStock(ctx, item.ProductID, item.Quantity); releaseErr != nil {
			c.logger.Error("Failed to release stock on cart clear",
				zap.Error(releaseErr),
				zap.Int64("product_id", item.ProductID),
				zap.Int("quantity", item.Quantity))
			// Продолжаем освобождать остальные товары
		}
	}

	c.logger.Info("Cart cleared with stock releases",
		zap.Int64("user_id", userID),
		zap.Int("items_cleared", len(cartItems)))

	return nil
}

func (c *CartUsecase) CommitCartReservation(ctx context.Context, userID int64) error {
	cartItems, err := c.cartQuery.GetByUserID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get cart items: %w", err)
	}

	for _, item := range cartItems {
		product, err := c.productQuery.GetByID(ctx, item.ProductID)
		if err != nil {
			return fmt.Errorf("failed to get product %d: %w", item.ProductID, err)
		}
		if product == nil {
			return fmt.Errorf("product %d not found", item.ProductID)
		}
		product.Stock -= item.Quantity
		if product.Stock < 0 {
			product.Stock = 0
		}
		if product.Stock > 0 {
			product.Status = "active"
		} else {
			product.Status = "out_of_stock"
		}
		updated, err := c.productQuery.Update(ctx, product, item.ProductID)
		if err != nil {
			return fmt.Errorf("failed to decrease product stock %d: %w", item.ProductID, err)
		}
		if err := c.stockCache.ConsumeReservedStock(ctx, item.ProductID, item.Quantity); err != nil {
			return fmt.Errorf("failed to consume reserved stock for product %d: %w", item.ProductID, err)
		}
		moyskladID := ""
		if updated.MoyskladID != nil {
			moyskladID = *updated.MoyskladID
		}
		if err := c.stockCache.SetStock(ctx, updated.ID, moyskladID, updated.Stock); err != nil {
			c.logger.Warn("Failed to update stock cache after order", zap.Int64("product_id", updated.ID), zap.Error(err))
		}
	}

	return nil
}

func (c *CartUsecase) ClearCartAfterOrder(ctx context.Context, userID int64) error {
	if err := c.cartQuery.Clear(ctx, userID); err != nil {
		c.logger.Error("Failed to clear cart after order", zap.Error(err))
		return fmt.Errorf("failed to clear cart after order: %w", err)
	}

	return nil
}

func (c *CartUsecase) GetCartTotal(ctx context.Context, userID int64) (float64, error) {
	total, err := c.cartQuery.GetTotal(ctx, userID)
	if err != nil {
		c.logger.Error("Failed to get cart total", zap.Error(err))
		return 0, fmt.Errorf("failed to get cart total: %w", err)
	}
	return total, nil
}
