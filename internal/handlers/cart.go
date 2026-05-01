package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/TeleginSergey/hozdacha/internal/usecase"
	"go.uber.org/zap"
)

type CartHandler struct {
	cartUsecase *usecase.CartUsecase
	logger       *zap.Logger
}

func NewCartHandler(cartUsecase *usecase.CartUsecase, logger *zap.Logger) *CartHandler {
	return &CartHandler{
		cartUsecase: cartUsecase,
		logger:       logger,
	}
}

func (h *CartHandler) AddToCart(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req usecase.AddToCartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	req.UserID = userID.(int64)

	if err := h.cartUsecase.AddToCart(c.Request.Context(), &req); err != nil {
		h.logger.Error("Failed to add to cart", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add to cart"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Product added to cart"})
}

func (h *CartHandler) GetCart(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	cart, err := h.cartUsecase.GetCart(c.Request.Context(), userID.(int64))
	if err != nil {
		h.logger.Error("Failed to get cart", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get cart"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"cart": cart})
}

func (h *CartHandler) UpdateCart(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req usecase.UpdateCartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	req.UserID = userID.(int64)

	if err := h.cartUsecase.UpdateCart(c.Request.Context(), &req); err != nil {
		h.logger.Error("Failed to update cart", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update cart"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Cart updated"})
}

func (h *CartHandler) RemoveFromCart(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	productIDStr := c.Param("id")
	productID, err := strconv.ParseInt(productIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid product ID"})
		return
	}

	if err := h.cartUsecase.RemoveFromCart(c.Request.Context(), userID.(int64), productID); err != nil {
		h.logger.Error("Failed to remove from cart", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove from cart"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Product removed from cart"})
}

func (h *CartHandler) ClearCart(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	if err := h.cartUsecase.ClearCart(c.Request.Context(), userID.(int64)); err != nil {
		h.logger.Error("Failed to clear cart", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clear cart"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Cart cleared"})
}

func (h *CartHandler) GetCartTotal(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	total, err := h.cartUsecase.GetCartTotal(c.Request.Context(), userID.(int64))
	if err != nil {
		h.logger.Error("Failed to get cart total", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get cart total"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"total": total})
}
