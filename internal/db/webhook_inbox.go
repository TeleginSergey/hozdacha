package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

const (
	WebhookKindEntity = "entity"
	WebhookKindStock  = "stock"

	WebhookStatusPending    = "pending"
	WebhookStatusProcessing = "processing"
	WebhookStatusCompleted  = "completed"
	WebhookStatusDeadLetter = "dead_letter"
)

type WebhookInboxJob struct {
	ID              int64
	IdempotencyKey  string
	WebhookKind     string
	Payload         []byte
	Status          string
	Attempts        int
	MaxAttempts     int
	NextAttemptAt   time.Time
	LastError       *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ProcessedAt     *time.Time
}

type WebhookInboxRepo struct {
	pool   *pgxpool.Pool
	logger *zap.Logger
}

func NewWebhookInboxRepo(pool *pgxpool.Pool, logger *zap.Logger) *WebhookInboxRepo {
	return &WebhookInboxRepo{pool: pool, logger: logger}
}

// Enqueue вставляет задачу; duplicate idempotency_key → inserted=false.
func (r *WebhookInboxRepo) Enqueue(ctx context.Context, kind, idempotencyKey string, payload []byte, maxAttempts int) (inserted bool, err error) {
	if maxAttempts < 1 {
		maxAttempts = 15
	}
	const q = `
INSERT INTO webhook_inbox (idempotency_key, webhook_kind, payload, status, max_attempts, next_attempt_at)
VALUES ($1, $2, $3::jsonb, 'pending', $4, now())
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING id`
	var id int64
	err = r.pool.QueryRow(ctx, q, idempotencyKey, kind, payload, maxAttempts).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *WebhookInboxRepo) ClaimBatch(ctx context.Context, limit int) ([]WebhookInboxJob, error) {
	if limit < 1 {
		limit = 10
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	const q = `
WITH picked AS (
  SELECT id FROM webhook_inbox
  WHERE status = 'pending' AND next_attempt_at <= now()
  ORDER BY id
  FOR UPDATE SKIP LOCKED
  LIMIT $1
)
UPDATE webhook_inbox w
SET status = 'processing', updated_at = now()
FROM picked p
WHERE w.id = p.id
RETURNING w.id, w.idempotency_key, w.webhook_kind, w.payload::text, w.status, w.attempts, w.max_attempts,
          w.next_attempt_at, w.last_error, w.created_at, w.updated_at, w.processed_at`

	rows, err := tx.Query(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []WebhookInboxJob
	for rows.Next() {
		var j WebhookInboxJob
		var payloadStr string
		if err := rows.Scan(&j.ID, &j.IdempotencyKey, &j.WebhookKind, &payloadStr, &j.Status, &j.Attempts, &j.MaxAttempts,
			&j.NextAttemptAt, &j.LastError, &j.CreatedAt, &j.UpdatedAt, &j.ProcessedAt); err != nil {
			return nil, err
		}
		j.Payload = []byte(payloadStr)
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (r *WebhookInboxRepo) MarkCompleted(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `
UPDATE webhook_inbox
SET status = 'completed', processed_at = now(), updated_at = now(), last_error = NULL
WHERE id = $1`, id)
	return err
}

func (r *WebhookInboxRepo) MarkRetry(ctx context.Context, id int64, attempts int, lastErr string, nextAt time.Time) error {
	_, err := r.pool.Exec(ctx, `
UPDATE webhook_inbox
SET status = 'pending', attempts = $2, last_error = $3, next_attempt_at = $4, updated_at = now()
WHERE id = $1`, id, attempts, lastErr, nextAt)
	return err
}

func (r *WebhookInboxRepo) MarkDeadLetter(ctx context.Context, id int64, lastErr string) error {
	_, err := r.pool.Exec(ctx, `
UPDATE webhook_inbox
SET status = 'dead_letter', last_error = $2, updated_at = now()
WHERE id = $1`, id, lastErr)
	return err
}

func (r *WebhookInboxRepo) ListDeadLetter(ctx context.Context, limit int) ([]WebhookInboxJob, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
SELECT id, idempotency_key, webhook_kind, payload::text, status, attempts, max_attempts, next_attempt_at, last_error, created_at, updated_at, processed_at
FROM webhook_inbox
WHERE status = 'dead_letter'
ORDER BY updated_at DESC
LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []WebhookInboxJob
	for rows.Next() {
		var j WebhookInboxJob
		var payloadStr string
		if err := rows.Scan(&j.ID, &j.IdempotencyKey, &j.WebhookKind, &payloadStr, &j.Status, &j.Attempts, &j.MaxAttempts,
			&j.NextAttemptAt, &j.LastError, &j.CreatedAt, &j.UpdatedAt, &j.ProcessedAt); err != nil {
			return nil, err
		}
		j.Payload = []byte(payloadStr)
		out = append(out, j)
	}
	return out, rows.Err()
}

// Replay сбрасывает запись в pending (ручной повтор из админки).
func (r *WebhookInboxRepo) Replay(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE webhook_inbox
SET status = 'pending', next_attempt_at = now(), updated_at = now(), last_error = NULL, processed_at = NULL
WHERE id = $1 AND status = 'dead_letter'`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no dead_letter row with id %d", id)
	}
	return nil
}
