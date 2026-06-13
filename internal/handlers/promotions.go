package handlers

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/db"
	"github.com/TeleginSergey/hozdacha/internal/middleware"
	"github.com/TeleginSergey/hozdacha/internal/services"
	"github.com/TeleginSergey/hozdacha/internal/usecase"
)

type PromotionHandler struct {
	promotionQuery     db.PromotionQuery
	promotionLinkQuery db.PromotionLinkQuery
	productUC          *usecase.ProductUsecase
	catalogUC          *usecase.PromotionCatalogUsecase
	logger             *zap.Logger
}

func NewPromotionHandler(promotionQuery db.PromotionQuery, logger *zap.Logger) *PromotionHandler {
	return &PromotionHandler{
		promotionQuery: promotionQuery,
		logger:         logger,
	}
}

// SetPromotionLinkQuery опционально подключает репозиторий связей акций.
// Без него ручки GetActivePromotions / GetAllPromotions / GetPromotionsFeed
// отдают только поля самой акции.
func (h *PromotionHandler) SetPromotionLinkQuery(links db.PromotionLinkQuery) {
	h.promotionLinkQuery = links
}

func (h *PromotionHandler) SetProductUsecase(uc *usecase.ProductUsecase) {
	h.productUC = uc
}

func (h *PromotionHandler) SetPromotionCatalogUsecase(uc *usecase.PromotionCatalogUsecase) {
	h.catalogUC = uc
}

// promotionCard — DTO для публичной выдачи акций: добавляет first_product_id /
// first_category_id, счётчики связей и подсказку по окну бронирования
// (target: "today"/"tomorrow" + note для UI).
type promotionCard struct {
	db.Promotion
	FirstProductID    *int64 `json:"first_product_id,omitempty"`
	FirstCategoryID   *int64 `json:"first_category_id,omitempty"`
	ProductCount      int    `json:"product_count"`
	CategoryCount     int    `json:"category_count"`
	ReservationTarget string `json:"reservation_target"`
	ReservationNote   string `json:"reservation_note"`
}

func (h *PromotionHandler) GetActivePromotions(c *gin.Context) {
	promotions, err := h.promotionQuery.GetActive(c.Request.Context())
	if err != nil {
		h.logger.Error("Failed to get active promotions", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось загрузить акции"})
		return
	}

	now := time.Now()
	promotions = services.FilterPromotionsForReservation(promotions, now)
	cards := h.buildCards(c.Request.Context(), promotions, now)
	c.JSON(http.StatusOK, gin.H{"promotions": cards})
}

func (h *PromotionHandler) GetAllPromotions(c *gin.Context) {
	promotions, err := h.promotionQuery.GetAll(c.Request.Context())
	if err != nil {
		h.logger.Error("Failed to get promotions", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось загрузить акции"})
		return
	}

	now := time.Now()
	promotions = services.FilterPromotionsForReservation(promotions, now)
	cards := h.buildCards(c.Request.Context(), promotions, now)
	c.JSON(http.StatusOK, gin.H{"promotions": cards})
}

// buildCards навешивает на каждую акцию first_product_id / first_category_id, чтобы
// фронт мог построить ссылку «перейти к товару» или «перейти в категорию».
// Также рассчитывает окно бронирования (ReservationTarget/Note) на момент now.
// Если репозиторий связей не подключён, акции всё равно отдаются (с пустыми доп. полями).
func (h *PromotionHandler) buildCards(ctx context.Context, promotions []*db.Promotion, now time.Time) []promotionCard {
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

	target, note := services.ReservationTarget(now)

	for _, p := range promotions {
		if p == nil {
			continue
		}
		card := promotionCard{
			Promotion:         *p,
			ReservationTarget: target,
			ReservationNote:   note,
		}
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

// ---------- /api/promotions/feed ----------

// GetPromotionsFeed возвращает ленту секций-акций для страницы /promotions.
// Каждая секция содержит шапку акции (с target/note), а фронт сам подгружает
// товары конкретной секции отдельным запросом /api/products с фильтром.
//
// Параметры:
//   - page, page_size — пагинация по секциям (акциям);
//
// Ответ: { items: [ {promotion, product_count} ], page, page_size, has_more, reservation_target, reservation_note }
func (h *PromotionHandler) GetPromotionsFeed(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageSize <= 0 || pageSize > 60 {
		pageSize = 20
	}

	ctx := c.Request.Context()
	promotions, err := h.promotionQuery.GetActive(ctx)
	if err != nil {
		h.logger.Error("Failed to get active promotions", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось загрузить акции"})
		return
	}

	now := time.Now()
	promotions = services.FilterPromotionsForReservation(promotions, now)
	cards := h.buildCards(ctx, promotions, now)

	// Оставляем только секции с товарами (акции-категории на feed'е не показываем —
	// для них отдельный сценарий с /catalog?category_id=...).
	filtered := make([]promotionCard, 0, len(cards))
	for _, card := range cards {
		if card.ProductCount > 0 {
			filtered = append(filtered, card)
		}
	}

	// Сортируем по убыванию скидки: самые «жирные» акции сверху.
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].Discount > filtered[j].Discount
	})

	pageStart := (page - 1) * pageSize
	pageEnd := pageStart + pageSize
	if pageStart > len(filtered) {
		pageStart = len(filtered)
	}
	if pageEnd > len(filtered) {
		pageEnd = len(filtered)
	}
	items := filtered[pageStart:pageEnd]
	hasMore := pageEnd < len(filtered)

	target, note := services.ReservationTarget(now)

	c.JSON(http.StatusOK, gin.H{
		"items":             items,
		"page":              page,
		"page_size":         pageSize,
		"has_more":          hasMore,
		"reservation_target": target,
		"reservation_note":  note,
		"total":             len(filtered),
	})
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

// GetPromotionProducts возвращает товары конкретной акции пачкой (limit/offset).
// Используется feed'ом /promotions для догрузки внутри одной секции.
func (h *PromotionHandler) GetPromotionProducts(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный номер акции"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if limit <= 0 || limit > 60 {
		limit = 20
	}
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if offset < 0 {
		offset = 0
	}

	ctx := c.Request.Context()
	if h.promotionLinkQuery == nil {
		c.JSON(http.StatusOK, gin.H{"products": []*db.Product{}, "count": 0, "has_more": false})
		return
	}
	productIDs, err := h.promotionLinkQuery.ListProductIDs(ctx, id)
	if err != nil {
		h.logger.Warn("failed to load product ids for promotion", zap.Int64("promotion_id", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось загрузить товары акции"})
		return
	}
	// Обрезаем по limit/offset.
	total := len(productIDs)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	pageIDs := productIDs[offset:end]

	products := make([]*db.Product, 0, len(pageIDs))
	if h.productUC != nil && len(pageIDs) > 0 {
		loaded, err := h.productUC.GetProductsByIDs(ctx, pageIDs)
		if err != nil {
			h.logger.Warn("failed to load promotion products", zap.Int64("promotion_id", id), zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось загрузить товары акции"})
			return
		}
		products = loaded
	}

	c.JSON(http.StatusOK, gin.H{
		"products": products,
		"count":    len(products),
		"total":    total,
		"has_more": end < total,
	})
}

// GetPromotionalProducts — GET /api/promotions/products
// Единый каталог акционных товаров (прямые + из категорий) с поиском и фильтром.
func (h *PromotionHandler) GetPromotionalProducts(c *gin.Context) {
	if h.catalogUC == nil {
		c.JSON(http.StatusOK, gin.H{
			"products":          []*db.Product{},
			"total":             0,
			"has_more":          false,
			"categories":        []usecase.PromotionCatalogCategory{},
			"promotions":        []usecase.PromotionCatalogPromo{},
			"reservation_note":  "",
			"reservation_target": "",
		})
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "24"))
	if limit <= 0 || limit > 60 {
		limit = 24
	}
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if offset < 0 {
		offset = 0
	}

	search := middleware.SanitizeString(c.Query("q"), 100)

	var categoryID *int64
	if cidStr := c.Query("category_id"); cidStr != "" {
		if cid, err := strconv.ParseInt(cidStr, 10, 64); err == nil && cid > 0 {
			categoryID = &cid
		}
	}
	kind := c.DefaultQuery("kind", "all")

	result, err := h.catalogUC.ListProducts(c.Request.Context(), search, categoryID, kind, limit, offset)
	if err != nil {
		h.logger.Error("Failed to list promotional products", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось загрузить акционные товары"})
		return
	}

	hasMore := offset+len(result.Products) < result.Total

	c.JSON(http.StatusOK, gin.H{
		"products":           result.Products,
		"total":              result.Total,
		"count":              len(result.Products),
		"has_more":           hasMore,
		"categories":         result.Categories,
		"promotions":         result.Promotions,
		"reservation_note":   result.ReservationNote,
		"reservation_target": result.ReservationTarget,
	})
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
