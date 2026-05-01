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
		InvalidateStockCache(ctx context.Context, productID int64) error
	}
	logger *zap.Logger
}

func NewCartUsecase(cartQuery db.CartItemQuery, productQuery db.ProductQuery, stockCache interface {
	GetAvailableStock(ctx context.Context, productID int64, productQuery db.ProductQuery) (int, error)
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
		// Если товар уже в корзине, обновляем количество
		newQuantity := existingItem.Quantity + req.Quantity
		if newQuantity > availableStock {
			return fmt.Errorf("insufficient stock for product %s (available: %d, requested: %d)", product.Name, availableStock, newQuantity)
		}
		return c.cartQuery.UpdateQuantity(ctx, req.UserID, req.ProductID, newQuantity)
	}

	// Добавляем товар в корзину
	cartItem := &db.CartItem{
		UserID:    req.UserID,
		ProductID: req.ProductID,
		Quantity:  req.Quantity,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	err = c.cartQuery.Create(ctx, cartItem)
	if err != nil {
		return fmt.Errorf("failed to add item to cart: %w", err)
	}

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

	// Обновляем товар в корзине
	newQuantity := req.Quantity
	if newQuantity > availableStock {
		return fmt.Errorf("insufficient stock for product %s (available: %d, requested: %d)", product.Name, availableStock, newQuantity)
	}

	return c.cartQuery.UpdateQuantity(ctx, existingItem.UserID, existingItem.ProductID, newQuantity)
}

func (c *CartUsecase) RemoveFromCart(ctx context.Context, userID, productID int64) error {
	err := c.cartQuery.Delete(ctx, userID, productID)
	if err != nil {
		c.logger.Error("Failed to remove from cart", zap.Error(err))
		return fmt.Errorf("failed to remove from cart: %w", err)
	}
	return nil
}

func (c *CartUsecase) ClearCart(ctx context.Context, userID int64) error {
	err := c.cartQuery.Clear(ctx, userID)
	if err != nil {
		c.logger.Error("Failed to clear cart", zap.Error(err))
		return fmt.Errorf("failed to clear cart: %w", err)
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
