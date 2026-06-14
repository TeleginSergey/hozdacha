package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/db"
)

type WebhookHandler struct {
	webhookSecret string
	inbox         *db.WebhookInboxRepo
	maxAttempts   int
	logger        *zap.Logger
}

func NewWebhookHandler(
	webhookSecret string,
	inbox *db.WebhookInboxRepo,
	maxAttempts int,
	logger *zap.Logger,
) *WebhookHandler {
	if maxAttempts < 1 {
		maxAttempts = 15
	}
	return &WebhookHandler{
		webhookSecret: webhookSecret,
		inbox:         inbox,
		maxAttempts:   maxAttempts,
		logger:        logger,
	}
}

func (h *WebhookHandler) idempotencyKey(c *gin.Context, kind string, body []byte) string {
	if id := c.GetHeader("X-Request-Id"); id != "" {
		return kind + ":" + id
	}
	sum := sha256.Sum256(append([]byte(kind+":"), body...))
	return kind + ":" + hex.EncodeToString(sum[:])
}

func (h *WebhookHandler) readVerifiedBody(c *gin.Context) ([]byte, error) {
	// Fail-closed: без настроенного секрета вебхуки не принимаем — иначе это открытый
	// эндпоинт, который может менять остатки/заказы. Требуется MOYSKLAD_WEBHOOK_SECRET.
	if h.webhookSecret == "" {
		h.logger.Error("MOYSKLAD_WEBHOOK_SECRET не задан — вебхуки отклоняются (fail-closed)")
		return nil, errHTTP(http.StatusServiceUnavailable, "webhook secret not configured")
	}
	signature := c.GetHeader("X-Moysklad-Signature")
	if signature == "" {
		return nil, errHTTP(http.StatusUnauthorized, "missing signature")
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, err
	}
	if !h.verifySignature(body, signature) {
		return nil, errHTTP(http.StatusUnauthorized, "invalid signature")
	}
	return body, nil
}

type httpStatusError struct {
	code int
	msg  string
}

func (e *httpStatusError) Error() string { return e.msg }

func errHTTP(code int, msg string) error {
	return &httpStatusError{code: code, msg: msg}
}

func writeWebhookReadError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	var hs *httpStatusError
	if errors.As(err, &hs) {
		c.JSON(hs.code, gin.H{"error": hs.msg})
		return true
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	return true
}

// HandleWebhook — быстрый приём: валидация + inbox, 200 без синхронной обработки МойСклад.
func (h *WebhookHandler) HandleWebhook(c *gin.Context) {
	body, err := h.readVerifiedBody(c)
	if writeWebhookReadError(c, err) {
		return
	}

	if h.inbox == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "webhook inbox not configured"})
		return
	}

	key := h.idempotencyKey(c, db.WebhookKindEntity, body)
	inserted, err := h.inbox.Enqueue(c.Request.Context(), db.WebhookKindEntity, key, body, h.maxAttempts)
	if err != nil {
		h.logger.Error("webhook enqueue failed", zap.Error(err))
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "queue unavailable"})
		return
	}
	if !inserted {
		h.logger.Debug("webhook duplicate ignored", zap.String("key", key))
	}
	c.Status(http.StatusNoContent)
}

// HandleStockWebhook — то же для webhookstock.
func (h *WebhookHandler) HandleStockWebhook(c *gin.Context) {
	body, err := h.readVerifiedBody(c)
	if writeWebhookReadError(c, err) {
		return
	}

	if h.inbox == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "webhook inbox not configured"})
		return
	}

	key := h.idempotencyKey(c, db.WebhookKindStock, body)
	inserted, err := h.inbox.Enqueue(c.Request.Context(), db.WebhookKindStock, key, body, h.maxAttempts)
	if err != nil {
		h.logger.Error("stock webhook enqueue failed", zap.Error(err))
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "queue unavailable"})
		return
	}
	if !inserted {
		h.logger.Debug("stock webhook duplicate ignored", zap.String("key", key))
	}
	c.Status(http.StatusNoContent)
}

// ListDeadLetter — админ: последние записи DLQ (краткий обзор).
func (h *WebhookHandler) ListDeadLetter(c *gin.Context) {
	if h.inbox == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "inbox not configured"})
		return
	}
	limit := 50
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	rows, err := h.inbox.ListDeadLetter(c.Request.Context(), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		lastErr := ""
		if r.LastError != nil {
			lastErr = *r.LastError
		}
		out = append(out, gin.H{
			"id":              r.ID,
			"idempotency_key": r.IdempotencyKey,
			"webhook_kind":    r.WebhookKind,
			"attempts":        r.Attempts,
			"last_error":      lastErr,
			"created_at":      r.CreatedAt,
			"updated_at":      r.UpdatedAt,
			"payload_preview": previewPayload(r.Payload, 400),
		})
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func previewPayload(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "…"
}

// ReplayDeadLetter — админ: вернуть запись из DLQ в pending.
func (h *WebhookHandler) ReplayDeadLetter(c *gin.Context) {
	if h.inbox == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "inbox not configured"})
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.inbox.Replay(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "requeued", "id": id})
}

func (h *WebhookHandler) verifySignature(body []byte, signature string) bool {
	if h.webhookSecret == "" {
		// Fail-closed: без секрета подпись считаем невалидной.
		return false
	}
	mac := hmac.New(sha256.New, []byte(h.webhookSecret))
	mac.Write(body)
	expectedSignature := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expectedSignature), []byte(signature))
}
