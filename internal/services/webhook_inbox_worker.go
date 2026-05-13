package services

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/db"
	"github.com/TeleginSergey/hozdacha/internal/resilience"
)

// WebhookInboxWorker забирает записи из webhook_inbox и обрабатывает их с backoff и DLQ.
type WebhookInboxWorker struct {
	inbox     *db.WebhookInboxRepo
	processor *WebhookProcessor
	logger    *zap.Logger
	interval  time.Duration
	batch     int
}

func NewWebhookInboxWorker(inbox *db.WebhookInboxRepo, processor *WebhookProcessor, interval time.Duration, batch int, logger *zap.Logger) *WebhookInboxWorker {
	if interval < time.Second {
		interval = 2 * time.Second
	}
	if batch < 1 {
		batch = 10
	}
	return &WebhookInboxWorker{
		inbox:     inbox,
		processor: processor,
		logger:    logger,
		interval:  interval,
		batch:     batch,
	}
}

func (w *WebhookInboxWorker) Run(ctx context.Context) {
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.processBatch(ctx)
		}
	}
}

func (w *WebhookInboxWorker) processBatch(ctx context.Context) {
	jobs, err := w.inbox.ClaimBatch(ctx, w.batch)
	if err != nil {
		w.logger.Error("webhook inbox claim", zap.Error(err))
		return
	}
	for _, job := range jobs {
		w.handleOne(ctx, job)
	}
}

func (w *WebhookInboxWorker) handleOne(ctx context.Context, job db.WebhookInboxJob) {
	pctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	var err error
	switch job.WebhookKind {
	case db.WebhookKindEntity:
		err = w.processor.ProcessEntityPayload(pctx, job.Payload)
	case db.WebhookKindStock:
		err = w.processor.ProcessStockPayload(pctx, job.Payload)
	default:
		err = fmt.Errorf("unknown webhook_kind %q", job.WebhookKind)
	}

	nextAttempts := job.Attempts + 1
	if err == nil {
		if e := w.inbox.MarkCompleted(pctx, job.ID); e != nil {
			w.logger.Error("webhook mark completed", zap.Int64("id", job.ID), zap.Error(e))
		}
		return
	}

	msg := err.Error()
	if nextAttempts >= job.MaxAttempts {
		if e := w.inbox.MarkDeadLetter(pctx, job.ID, msg); e != nil {
			w.logger.Error("webhook mark dlq", zap.Int64("id", job.ID), zap.Error(e))
		}
		w.logger.Warn("webhook moved to dead_letter",
			zap.Int64("id", job.ID), zap.String("kind", job.WebhookKind), zap.Error(err))
		return
	}

	nextAt := time.Now().Add(resilience.WebhookRetryDelay(nextAttempts))
	if e := w.inbox.MarkRetry(pctx, job.ID, nextAttempts, msg, nextAt); e != nil {
		w.logger.Error("webhook mark retry", zap.Int64("id", job.ID), zap.Error(e))
	}
	w.logger.Warn("webhook scheduled retry",
		zap.Int64("id", job.ID), zap.Int("attempt", nextAttempts), zap.Time("next", nextAt), zap.Error(err))
}
