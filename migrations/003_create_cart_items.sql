-- Создание таблицы корзины
CREATE TABLE IF NOT EXISTS cart_items (
    cart_items_id_pk SERIAL PRIMARY KEY,
    cart_items_user_id_fk INTEGER NOT NULL REFERENCES users(users_id_pk) ON DELETE CASCADE,
    cart_items_product_id_fk INTEGER NOT NULL REFERENCES products(products_id_pk) ON DELETE CASCADE,
    cart_items_quantity INTEGER NOT NULL DEFAULT 1 CHECK (cart_items_quantity > 0),
    cart_items_created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    cart_items_updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Индексы для оптимизации
CREATE INDEX IF NOT EXISTS idx_cart_items_user_id ON cart_items(cart_items_user_id_fk);
CREATE INDEX IF NOT EXISTS idx_cart_items_product_id ON cart_items(cart_items_product_id_fk);
CREATE INDEX IF NOT EXISTS idx_cart_items_user_product ON cart_items(cart_items_user_id_fk, cart_items_product_id_fk);

-- Уникальный индекс для предотвращения дубликатов
CREATE UNIQUE INDEX IF NOT EXISTS idx_cart_items_user_product_unique ON cart_items(cart_items_user_id_fk, cart_items_product_id_fk);
