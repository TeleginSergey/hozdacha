-- Добавление статуса товара и времени последней синхронизации из МойСклад

ALTER TABLE products
    ADD COLUMN IF NOT EXISTS products_status VARCHAR(20) NOT NULL DEFAULT 'active',
    ADD COLUMN IF NOT EXISTS products_last_sync_updated TIMESTAMP NULL;

-- Удаляем constraint если существует (для чистой переустановки)
ALTER TABLE products DROP CONSTRAINT IF EXISTS products_status_check;

-- Добавляем constraint заново
ALTER TABLE products ADD CONSTRAINT products_status_check CHECK (products_status IN ('active', 'out_of_stock', 'deleted'));


