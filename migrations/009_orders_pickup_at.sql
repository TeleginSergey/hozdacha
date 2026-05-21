-- 009_orders_pickup_at.sql
-- Желаемое время самовывоза: клиент указывает когда придёт.
-- Ограничения: > момента создания, <= orders_reserved_until (бронь ещё жива).

ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS orders_pickup_at TIMESTAMPTZ;

-- Индекс для страницы "визиты сегодня" в админке:
-- быстро достать все заказы с pickup_at в заданном диапазоне.
CREATE INDEX IF NOT EXISTS idx_orders_pickup_at
    ON orders (orders_pickup_at)
    WHERE orders_pickup_at IS NOT NULL;
