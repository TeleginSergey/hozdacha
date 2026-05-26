-- 011_categories_parent_id.sql
-- Иерархия категорий: ссылка на родительскую категорию.
-- NULL parent_id означает корневую (главную) категорию.
ALTER TABLE categories
    ADD COLUMN IF NOT EXISTS categories_parent_id_fk INTEGER
    REFERENCES categories(categories_id_pk) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS categories_parent_id_idx
    ON categories(categories_parent_id_fk);
