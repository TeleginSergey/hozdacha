package handlers

import (
	"context"
	"net/http"
	"sync/atomic"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/services"
)

type MoyskladSyncHandler struct {
	syncService     *services.MoyskladSyncService
	logger          *zap.Logger
	fullSyncRunning int32 // atomic: 1 = running
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
	// Защита от параллельного запуска — один sync за раз.
	if !atomic.CompareAndSwapInt32(&h.fullSyncRunning, 0, 1) {
		c.JSON(http.StatusConflict, gin.H{"message": "Full sync already running, please wait"})
		return
	}

	h.logger.Info("Starting full product sync in background")

	// Запускаем в горутине с независимым контекстом — HTTP-таймаут не прерывает sync.
	go func() {
		defer atomic.StoreInt32(&h.fullSyncRunning, 0)
		ctx := context.Background()
		result, err := h.syncService.SyncProducts(ctx)
		if err != nil {
			h.logger.Error("Full sync failed", zap.Error(err))
			return
		}
		h.logger.Info("Full sync completed",
			zap.Int("created", result.Created),
			zap.Int("updated", result.Updated),
			zap.Int("errors", result.Errors))
	}()

	c.JSON(http.StatusAccepted, gin.H{
		"message": "Full sync started in background. Check server logs for progress.",
	})
}
