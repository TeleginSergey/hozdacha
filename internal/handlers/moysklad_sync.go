package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/services"
)

type MoyskladSyncHandler struct {
	syncService *services.MoyskladSyncService
	logger      *zap.Logger
}

func NewMoyskladSyncHandler(syncService *services.MoyskladSyncService, logger *zap.Logger) *MoyskladSyncHandler {
	return &MoyskladSyncHandler{
		syncService: syncService,
		logger:      logger,
	}
}

func (h *MoyskladSyncHandler) SyncProducts(c *gin.Context) {
	// Дельта-синхронизация (резервный механизм)
	result, err := h.syncService.SyncProductsDelta(c.Request.Context())
	if err != nil {
		h.logger.Error("Failed to sync products (delta)", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to sync products",
			"message": err.Error(),
		})
		return
	}

	h.logger.Info("Products delta-sync completed",
		zap.Int("created", result.Created),
		zap.Int("updated", result.Updated),
		zap.Int("errors", result.Errors))

	c.JSON(http.StatusOK, gin.H{
		"message": "Products delta-sync completed (webhook backup)",
		"result":  result,
	})
}

func (h *MoyskladSyncHandler) SyncProductsFull(c *gin.Context) {
	// Полная синхронизация (для первого запуска или восстановления)
	h.logger.Info("Starting full product sync (initial setup or recovery)")

	result, err := h.syncService.SyncProducts(c.Request.Context())
	if err != nil {
		h.logger.Error("Failed to sync products (full)", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to sync products",
			"message": err.Error(),
		})
		return
	}

	h.logger.Info("Products full-sync completed",
		zap.Int("created", result.Created),
		zap.Int("updated", result.Updated),
		zap.Int("errors", result.Errors))

	c.JSON(http.StatusOK, gin.H{
		"message": "Products full-sync completed (all products synchronized)",
		"result":  result,
	})
}
