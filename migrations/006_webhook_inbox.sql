-- Очередь вебхуков МойСклад: быстрый приём HTTP, асинхронная обработка, DLQ, replay.
CREATE TABLE IF NOT EXISTS webhook_inbox (
    id BIGSERIAL PRIMARY KEY,
    idempotency_key TEXT NOT NULL,
    webhook_kind TEXT NOT NULL CHECK (webhook_kind IN ('entity', 'stock')),
    payload JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'completed', 'dead_letter')),
    attempts INT NOT NULL DEFAULT 0,
    max_attempts INT NOT NULL DEFAULT 15,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMPTZ,
    CONSTRAINT webhook_inbox_idempotency_key_unique UNIQUE (idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_webhook_inbox_pending ON webhook_inbox (next_attempt_at, id)
    WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS idx_webhook_inbox_dlq ON webhook_inbox (updated_at DESC)
    WHERE status = 'dead_letter';
