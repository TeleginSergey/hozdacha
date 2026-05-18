package usecase

import (
	"context"
	"fmt"
	"time"

	"github.com/TeleginSergey/hozdacha/internal/db"
	"go.uber.org/zap"
)

// CartUsecase реализует корзину как wish-list: добавление в корзину НЕ резервирует товар.
// Резервация (атомарный декремент стока) происходит только при создании заказа.
// Это устраняет проблемы:
//   - "висячих" резервов от пользователей, которые не оформили заказ
//   - рассинхрона Redis/Postgres при падении любого из них
//   - двойного учёта остатка
//
// Гонка двух одновременных заказов решается атомарным UPDATE в БД (см. CreateOrderAtomic).
type CartUsecase struct {
	cartQuery    db.CartItemQuery
	productQuery db.ProductQuery
	logger       *zap.Logger
}

func NewCartUsecase(cartQuery db.CartItemQuery, productQuery db.ProductQuery, logger *zap.Logger) *CartUsecase {
	return &CartUsecase{
		cartQuery:    cartQuery,
		productQuery: productQuery,
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

// AddToCart добавляет товар в корзину. Проверяет только наличие/активность товара
// и грубое наличие стока для UX. Финальная атомарная проверка — при создании заказа.
func (c *CartUsecase) AddToCart(ctx context.Context, req *AddToCartRequest) error {
	product, err := c.productQuery.GetByID(ctx, req.ProductID)
	if err != nil {
		return fmt.Errorf("failed to get product: %w", err)
	}
	if product == nil {
		return fmt.Errorf("product not found")
	}
	if !product.Active {
		return fmt.Errorf("product is not active")
	}

	// Грубая проверка: товар вообще есть в наличии. Реальная гарантия — при оформлении заказа.
	if product.Stock <= 0 {
		return fmt.Errorf("product %s is out of stock", product.Name)
	}

	existingItem, err := c.cartQuery.GetByUserIDAndProductID(ctx, req.UserID, req.ProductID)
	if err != nil {
		return fmt.Errorf("failed to check existing cart item: %w", err)
	}
	if existingItem != nil {
		newQuantity := existingItem.Quantity + req.Quantity
		if err := c.cartQuery.UpdateQuantity(ctx, req.UserID, req.ProductID, newQuantity); err != nil {
			return fmt.Errorf("failed to update cart item: %w", err)
		}
		return nil
	}

	cartItem := &db.CartItem{
		UserID:    req.UserID,
		ProductID: req.ProductID,
		Quantity:  req.Quantity,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := c.cartQuery.Create(ctx, cartItem); err != nil {
		return fmt.Errorf("failed to create cart item: %w", err)
	}

	c.logger.Info("Product added to cart",
		zap.Int64("user_id", req.UserID),
		zap.Int64("product_id", req.ProductID),
		zap.Int("quantity", req.Quantity))
	return nil
}

func (c *CartUsecase) GetCart(ctx context.Context, userID int64) ([]*CartResponse, error) {
	cartItems, err := c.cartQuery.GetByUserID(ctx, userID)
	if err != nil {
		c.logger.Error("Failed to get cart items", zap.Error(err))
		return nil, fmt.Errorf("failed to get cart items: %w", err)
	}

	// Собираем все productID и тянем одним запросом.
	productIDs := make([]int64, 0, len(cartItems))
	for _, item := range cartItems {
		productIDs = append(productIDs, item.ProductID)
	}
	productMap := make(map[int64]*db.Product, len(productIDs))
	if len(productIDs) > 0 {
		products, err := c.productQuery.GetByIDs(ctx, productIDs)
		if err != nil {
			c.logger.Warn("Batch product fetch failed, falling back to individual", zap.Error(err))
		} else {
			for _, p := range products {
				if p != nil {
					productMap[p.ID] = p
				}
			}
		}
	}

	responses := make([]*CartResponse, 0, len(cartItems))
	for _, item := range cartItems {
		product := productMap[item.ProductID]
		if product == nil {
			// Fallback: индивидуальный запрос если батч не сработал.
			product, err = c.productQuery.GetByID(ctx, item.ProductID)
			if err != nil {
				c.logger.Error("Failed to get product for cart item", zap.Error(err), zap.Int64("product_id", item.ProductID))
				continue
			}
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
	product, err := c.productQuery.GetByID(ctx, req.ProductID)
	if err != nil {
		return fmt.Errorf("failed to get product: %w", err)
	}
	if product == nil {
		return fmt.Errorf("product not found")
	}

	existingItem, err := c.cartQuery.GetByUserIDAndProductID(ctx, req.UserID, req.ProductID)
	if err != nil {
		return fmt.Errorf("failed to check existing cart item: %w", err)
	}
	if existingItem == nil {
		return fmt.Errorf("item not found in cart")
	}

	if err := c.cartQuery.UpdateQuantity(ctx, req.UserID, req.ProductID, req.Quantity); err != nil {
		return fmt.Errorf("failed to update cart item: %w", err)
	}
	return nil
}

func (c *CartUsecase) RemoveFromCart(ctx context.Context, userID, productID int64) error {
	if err := c.cartQuery.Delete(ctx, userID, productID); err != nil {
		c.logger.Error("Failed to remove from cart", zap.Error(err))
		return fmt.Errorf("failed to remove from cart: %w", err)
	}
	return nil
}

func (c *CartUsecase) ClearCart(ctx context.Context, userID int64) error {
	if err := c.cartQuery.Clear(ctx, userID); err != nil {
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
