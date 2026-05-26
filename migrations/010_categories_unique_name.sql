-- 010_categories_unique_name.sql
-- Уникальность имени категории нужна для ON CONFLICT upsert при синхронизации с МойСклад.
ALTER TABLE categories
    ADD CONSTRAINT categories_name_unique UNIQUE (categories_name);
