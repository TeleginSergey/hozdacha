package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/db"
	"github.com/TeleginSergey/hozdacha/internal/middleware"
	"github.com/TeleginSergey/hozdacha/internal/usecase"
)

type ProductHandler struct {
	uc     *usecase.ProductUsecase
	db     *db.DB
	logger *zap.Logger
}

// RenderProductPage рендерит SSR-страницу товара через usecase,
// чтобы применялись кэш остатков и stock buffer (как в API/каталоге).
func (h *ProductHandler) RenderProductPage(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.HTML(http.StatusNotFound, "error.html", gin.H{
			"title":   "Товар не найден",
			"message": "Товар не найден",
		})
		return
	}

	product, err := h.uc.GetProductByID(c.Request.Context(), id)
	if err != nil || product == nil || !product.Active {
		c.HTML(http.StatusNotFound, "error.html", gin.H{
			"title":   "Товар не найден",
			"message": "Товар не найден или недоступен",
		})
		return
	}

	c.HTML(http.StatusOK, "product.html", gin.H{
		"title":   product.Name,
		"product": product,
	})
}

// NewProductHandlerWithUsecase — конструктор поверх usecase-слоя.
func NewProductHandlerWithUsecase(uc *usecase.ProductUsecase, database *db.DB, logger *zap.Logger) *ProductHandler {
	return &ProductHandler{
		uc:     uc,
		db:     database,
		logger: logger,
	}
}

func (h *ProductHandler) GetProducts(c *gin.Context) {
	// Валидация и санитизация параметров
	limitStr := c.DefaultQuery("limit", "20")
	offsetStr := c.DefaultQuery("offset", "0")
	categoryID := c.Query("category_id")

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 || limit > 500 {
		limit = 50 // Увеличиваем дефолтный лимит
	}

	offset, err2 := strconv.Atoi(offsetStr)
	if err2 != nil || offset < 0 {
		offset = 0
	}

	var categoryPtr *int64
	if categoryID != "" {
		categoryIDInt, err3 := strconv.ParseInt(categoryID, 10, 64)
		if err3 != nil || categoryIDInt <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid category_id"})
			return
		}
		categoryPtr = &categoryIDInt
	}

	products, err := h.uc.GetCatalogProducts(c.Request.Context(), limit, offset, categoryPtr)
	if err != nil {
		h.logger.Error("Failed to get products", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get products"})
		return
	}

	// Получаем общее количество для пагинации
	total, err := h.uc.GetActiveProductsCount(c.Request.Context(), categoryPtr)
	if err != nil {
		h.logger.Warn("Failed to get total count", zap.Error(err))
		total = 0
	}

	c.JSON(http.StatusOK, gin.H{
		"products": products,
		"total":    total,
		"count":    len(products),
	})
}

func (h *ProductHandler) GetProduct(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid product ID"})
		return
	}

	product, err := h.uc.GetProductByID(c.Request.Context(), id)
	if err != nil {
		h.logger.Error("Failed to get product", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get product"})
		return
	}

	if product == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Product not found"})
		return
	}

	c.JSON(http.StatusOK, product)
}

func (h *ProductHandler) SearchProducts(c *gin.Context) {
	query := c.Query("q")
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Search query is required"})
		return
	}

	// Санитизация поискового запроса
	query = middleware.SanitizeString(query, 100)
	if len(query) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Search query must be at least 2 characters"})
		return
	}

	limit, err := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if err != nil || limit <= 0 || limit > 100 {
		limit = 20
	}

	offset, err2 := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err2 != nil || offset < 0 {
		offset = 0
	}

	// Опциональный фильтр по категории (включая подкатегории).
	var categoryID *int64
	if cidStr := c.Query("category_id"); cidStr != "" {
		if cid, err := strconv.ParseInt(cidStr, 10, 64); err == nil && cid > 0 {
			categoryID = &cid
		}
	}

	products, err := h.uc.SearchProducts(c.Request.Context(), query, categoryID, limit, offset)
	if err != nil {
		h.logger.Error("Failed to search products", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to search products"})
		return
	}

	// Получаем общее количество найденных товаров
	total, err := h.uc.GetSearchProductsCount(c.Request.Context(), query, categoryID)
	if err != nil {
		h.logger.Warn("Failed to get search count", zap.Error(err))
		total = 0
	}

	c.JSON(http.StatusOK, gin.H{
		"products": products,
		"total":    total,
		"count":    len(products),
	})
}

// ListCategories — GET /api/categories
func (h *ProductHandler) ListCategories(c *gin.Context) {
	categories, err := (&db.CategoryQuery{DB: h.db}).ListAll(c.Request.Context())
	if err != nil {
		h.logger.Error("Failed to list categories", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load categories"})
		return
	}
	if categories == nil {
		categories = []*db.Category{}
	}
	c.JSON(http.StatusOK, gin.H{"categories": categories})
}
