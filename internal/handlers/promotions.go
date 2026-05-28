package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/db"
	"github.com/TeleginSergey/hozdacha/internal/middleware"
	"github.com/TeleginSergey/hozdacha/internal/services"
)

type PromotionHandler struct {
	promotionQuery db.PromotionQuery
	logger         *zap.Logger
}

func NewPromotionHandler(promotionQuery db.PromotionQuery, logger *zap.Logger) *PromotionHandler {
	return &PromotionHandler{
		promotionQuery: promotionQuery,
		logger:         logger,
	}
}

func (h *PromotionHandler) GetActivePromotions(c *gin.Context) {
	promotions, err := h.promotionQuery.GetActive(c.Request.Context())
	if err != nil {
		h.logger.Error("Failed to get active promotions", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось загрузить акции"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"promotions": promotions})
}

func (h *PromotionHandler) GetAllPromotions(c *gin.Context) {
	promotions, err := h.promotionQuery.GetAll(c.Request.Context())
	if err != nil {
		h.logger.Error("Failed to get promotions", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось загрузить акции"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"promotions": promotions})
}

func (h *PromotionHandler) GetPromotion(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный номер акции"})
		return
	}

	promotion, err := h.promotionQuery.GetByID(c.Request.Context(), id)
	if err != nil {
		h.logger.Error("Failed to get promotion", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось загрузить акцию"})
		return
	}

	if promotion == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Акция не найдена"})
		return
	}

	c.JSON(http.StatusOK, promotion)
}

func (h *PromotionHandler) CreatePromotion(c *gin.Context) {
	var promotion db.Promotion
	if err := c.ShouldBindJSON(&promotion); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный формат запроса"})
		return
	}

	// Валидация
	if err := services.ValidatePromotion(promotion.Title, promotion.Description, promotion.Discount); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Санитизация
	promotion.Title = middleware.SanitizeString(promotion.Title, 255)
	if promotion.Description != nil {
		sanitized := middleware.SanitizeString(*promotion.Description, 2000)
		promotion.Description = &sanitized
	}

	created, err := h.promotionQuery.Insert(c.Request.Context(), &promotion)
	if err != nil {
		h.logger.Error("Failed to create promotion", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось создать акцию"})
		return
	}

	c.JSON(http.StatusCreated, created)
}

func (h *PromotionHandler) UpdatePromotion(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный номер акции"})
		return
	}

	var promotion db.Promotion
	if err := c.ShouldBindJSON(&promotion); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный формат запроса"})
		return
	}

	// Валидация
	if err := services.ValidatePromotion(promotion.Title, promotion.Description, promotion.Discount); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Санитизация
	promotion.Title = middleware.SanitizeString(promotion.Title, 255)
	if promotion.Description != nil {
		sanitized := middleware.SanitizeString(*promotion.Description, 2000)
		promotion.Description = &sanitized
	}

	updated, err := h.promotionQuery.Update(c.Request.Context(), &promotion, id)
	if err != nil {
		h.logger.Error("Failed to update promotion", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось обновить акцию"})
		return
	}

	c.JSON(http.StatusOK, updated)
}

func (h *PromotionHandler) DeletePromotion(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный номер акции"})
		return
	}

	if err := h.promotionQuery.Delete(c.Request.Context(), id); err != nil {
		h.logger.Error("Failed to delete promotion", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось удалить акцию"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Promotion deleted successfully"})
}
