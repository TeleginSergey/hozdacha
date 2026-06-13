package handlers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/db"
	"github.com/TeleginSergey/hozdacha/internal/middleware"
	"github.com/TeleginSergey/hozdacha/internal/services"
)

type PromotionHandler struct {
	promotionQuery     db.PromotionQuery
	promotionLinkQuery db.PromotionLinkQuery
	logger             *zap.Logger
}

func NewPromotionHandler(promotionQuery db.PromotionQuery, logger *zap.Logger) *PromotionHandler {
	return &PromotionHandler{
		promotionQuery: promotionQuery,
		logger:         logger,
	}
}

// SetPromotionLinkQuery опционально подключает репозиторий связей акций.
// Без него ручки GetActivePromotions / GetAllPromotions отдают только поля самой акции.
func (h *PromotionHandler) SetPromotionLinkQuery(links db.PromotionLinkQuery) {
	h.promotionLinkQuery = links
}

// promotionCard — DTO для публичной выдачи акций: добавляет first_product_id /
// first_category_id и счётчики связей, чтобы фронт мог построить корректную ссылку
// на товар/категорию/страницу акций.
type promotionCard struct {
	db.Promotion
	FirstProductID  *int64 `json:"first_product_id,omitempty"`
	FirstCategoryID *int64 `json:"first_category_id,omitempty"`
	ProductCount    int    `json:"product_count"`
	CategoryCount   int    `json:"category_count"`
}

func (h *PromotionHandler) GetActivePromotions(c *gin.Context) {
	promotions, err := h.promotionQuery.GetActive(c.Request.Context())
	if err != nil {
		h.logger.Error("Failed to get active promotions", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось загрузить акции"})
		return
	}

	cards := h.buildCards(c.Request.Context(), promotions)
	c.JSON(http.StatusOK, gin.H{"promotions": cards})
}

func (h *PromotionHandler) GetAllPromotions(c *gin.Context) {
	promotions, err := h.promotionQuery.GetAll(c.Request.Context())
	if err != nil {
		h.logger.Error("Failed to get promotions", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось загрузить акции"})
		return
	}

	cards := h.buildCards(c.Request.Context(), promotions)
	c.JSON(http.StatusOK, gin.H{"promotions": cards})
}

// buildCards навешивает на каждую акцию first_product_id / first_category_id, чтобы
// фронт мог построить ссылку «перейти к товару» или «перейти в категорию».
// Если репозиторий связей не подключён, акции всё равно отдаются (с пустыми доп. полями).
func (h *PromotionHandler) buildCards(ctx context.Context, promotions []*db.Promotion) []promotionCard {
	cards := make([]promotionCard, 0, len(promotions))
	if len(promotions) == 0 {
		return cards
	}

	ids := make([]int64, 0, len(promotions))
	for _, p := range promotions {
		if p == nil {
			continue
		}
		ids = append(ids, p.ID)
	}

	var productMap map[int64][]int64
	var categoryMap map[int64][]int64
	if h.promotionLinkQuery != nil && len(ids) > 0 {
		if pm, err := h.promotionLinkQuery.ListProductIDsForPromotions(ctx, ids); err == nil {
			productMap = pm
		} else {
			h.logger.Warn("failed to load promotion product links", zap.Error(err))
		}
		if cm, err := h.promotionLinkQuery.ListCategoryIDsForPromotions(ctx, ids); err == nil {
			categoryMap = cm
		} else {
			h.logger.Warn("failed to load promotion category links", zap.Error(err))
		}
	}

	for _, p := range promotions {
		if p == nil {
			continue
		}
		card := promotionCard{Promotion: *p}
		if plist, ok := productMap[p.ID]; ok && len(plist) > 0 {
			first := plist[0]
			card.FirstProductID = &first
			card.ProductCount = len(plist)
		}
		if clist, ok := categoryMap[p.ID]; ok && len(clist) > 0 {
			first := clist[0]
			card.FirstCategoryID = &first
			card.CategoryCount = len(clist)
		}
		cards = append(cards, card)
	}
	return cards
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
