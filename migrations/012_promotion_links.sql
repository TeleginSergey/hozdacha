-- 012_promotion_links.sql
-- Привязка акций к товарам и категориям с приоритетом:
--   product-level (выше) > category-level (ниже) > базовая цена.
-- Одна акция может покрывать сразу несколько товаров и/или категорий.

CREATE TABLE IF NOT EXISTS promotion_products (
    promotion_id INTEGER NOT NULL REFERENCES promotions(promotions_id_pk) ON DELETE CASCADE,
    product_id   INTEGER NOT NULL REFERENCES products(products_id_pk)     ON DELETE CASCADE,
    created_at   TIMESTAMP NOT NULL DEFAULT NOW(),
    PRIMARY KEY (promotion_id, product_id)
);

CREATE INDEX IF NOT EXISTS promotion_products_product_idx
    ON promotion_products(product_id);

CREATE TABLE IF NOT EXISTS promotion_categories (
    promotion_id INTEGER NOT NULL REFERENCES promotions(promotions_id_pk) ON DELETE CASCADE,
    category_id  INTEGER NOT NULL REFERENCES categories(categories_id_pk) ON DELETE CASCADE,
    created_at   TIMESTAMP NOT NULL DEFAULT NOW(),
    PRIMARY KEY (promotion_id, category_id)
);

CREATE INDEX IF NOT EXISTS promotion_categories_category_idx
    ON promotion_categories(category_id);
