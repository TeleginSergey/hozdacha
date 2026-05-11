-- Исправление constraint для статусов продуктов
-- Добавляем статус 'archived' для товаров из МойСклад

-- Удаляем старый constraint
ALTER TABLE products DROP CONSTRAINT IF EXISTS products_status_check;

-- Добавляем обновленный constraint с поддержкой всех возможных статусов
ALTER TABLE products ADD CONSTRAINT products_status_check 
CHECK (products_status IN ('active', 'out_of_stock', 'deleted', 'archived'));

-- Обновляем существующие записи с некорректными статусами (если есть)
UPDATE products 
SET products_status = 'active' 
WHERE products_status NOT IN ('active', 'out_of_stock', 'deleted', 'archived');
