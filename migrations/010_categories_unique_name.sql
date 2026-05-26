-- 010_categories_unique_name.sql
-- Уникальность имени категории (опционально, для производительности поиска).
-- UpsertByName работает и без этого constraint через SELECT+INSERT.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'categories_name_unique'
    ) THEN
        ALTER TABLE categories ADD CONSTRAINT categories_name_unique UNIQUE (categories_name);
    END IF;
END$$;
