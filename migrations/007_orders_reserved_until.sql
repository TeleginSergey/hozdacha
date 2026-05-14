-- 007_orders_reserved_until.sql
-- Бронирование с TTL: каждый заказ держит резерв 48 часов от создания.
-- Cron-задача возвращает остаток и удаляет CustomerOrder в МойСклад, если клиент не пришёл.

ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS orders_reserved_until TIMESTAMPTZ;

-- Для уже существующих заказов считаем дедлайн от created_at, чтобы можно было корректно очистить старые брони.
UPDATE orders
SET orders_reserved_until = orders_created_at + INTERVAL '48 hours'
WHERE orders_reserved_until IS NULL
  AND orders_status = 'pending';

-- Частичный индекс по pending-заказам с дедлайном — основная нагрузка приходит на cron expiry.
CREATE INDEX IF NOT EXISTS idx_orders_reserved_until_pending
    ON orders (orders_reserved_until)
    WHERE orders_status = 'pending';

-- Допустимые статусы заказа: pending → completed (выкупили в магазине), expired (TTL вышел), cancelled (отменил пользователь/админ).
-- Снимаем старый CHECK, если он был, и ставим новый.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.check_constraints
        WHERE constraint_name = 'orders_status_check'
    ) THEN
        ALTER TABLE orders DROP CONSTRAINT orders_status_check;
    END IF;
END $$;

ALTER TABLE orders
    ADD CONSTRAINT orders_status_check
    CHECK (orders_status IN ('pending', 'completed', 'expired', 'cancelled'));
