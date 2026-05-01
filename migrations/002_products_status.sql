-- Добавление статуса товара и времени последней синхронизации из МойСклад

ALTER TABLE products
    ADD COLUMN IF NOT EXISTS products_status VARCHAR(20) NOT NULL DEFAULT 'active',
    ADD COLUMN IF NOT EXISTS products_last_sync_updated TIMESTAMP NULL;

-- Опциональное ограничение по допустимым значениям статуса
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'products_status_check'
    ) THEN
        ALTER TABLE products
        ADD CONSTRAINT products_status_check
        CHECK (products_status IN ('active', 'out_of_stock', 'deleted'));
    END IF;
END$$;


