-- 008_order_events.sql
-- Audit log: лог изменений статусов заказа (created, shipped, cancelled, expired).
-- Хранит "кто и когда" для каждой смены состояния — для админки и разбора инцидентов.

CREATE TABLE IF NOT EXISTS order_events (
    order_events_id_pk SERIAL PRIMARY KEY,
    order_events_order_id_fk INTEGER NOT NULL REFERENCES orders(orders_id_pk) ON DELETE CASCADE,
    -- Типы событий: 'created', 'shipped', 'cancelled', 'expired', 'moysklad_synced', 'moysklad_failed'
    order_events_type VARCHAR(32) NOT NULL,
    -- NULL для системных событий (cron expiry, sync); иначе id админа/пользователя, инициировавшего изменение.
    order_events_actor_user_id_fk INTEGER REFERENCES users(users_id_pk) ON DELETE SET NULL,
    -- Произвольная метаинформация: для cancelled — причина, для shipped — moysklad_id, etc.
    order_events_payload JSONB,
    order_events_created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_order_events_order_id ON order_events (order_events_order_id_fk, order_events_created_at DESC);
CREATE INDEX IF NOT EXISTS idx_order_events_created_at ON order_events (order_events_created_at DESC);

-- Индексы под фильтры в админке.
CREATE INDEX IF NOT EXISTS idx_orders_status_created ON orders (orders_status, orders_created_at DESC);
CREATE INDEX IF NOT EXISTS idx_orders_phone ON orders (orders_phone);
-- Триграммный индекс для LIKE-поиска по имени и телефону (если расширение доступно).
-- Если pg_trgm не установлен — индексы не создадутся, но LIKE будет работать через seq scan.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = 'pg_trgm') THEN
        CREATE EXTENSION IF NOT EXISTS pg_trgm;
        CREATE INDEX IF NOT EXISTS idx_orders_customer_name_trgm ON orders USING gin (orders_customer_name gin_trgm_ops);
        CREATE INDEX IF NOT EXISTS idx_users_username_trgm ON users USING gin (users_username gin_trgm_ops);
        CREATE INDEX IF NOT EXISTS idx_users_email_trgm ON users USING gin (users_email gin_trgm_ops);
    END IF;
END $$;
