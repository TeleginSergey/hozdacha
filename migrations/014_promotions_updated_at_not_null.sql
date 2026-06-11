-- 014_promotions_updated_at_not_null.sql
-- Починка: scany не умеет сканировать NULL в time.Time (только в *time.Time).
-- В логах: "cannot scan NULL into *time.Time" на /api/promotions и в PromotionPricer.
-- Бэкфилл существующих NULL → текущее время, затем NOT NULL + дефолт, чтобы новые
-- записи всегда имели значение, и upsert не оставлял updated_at в zero/NULL.

UPDATE promotions
   SET promotions_updated_at = COALESCE(promotions_updated_at, COALESCE(promotions_created_at, CURRENT_TIMESTAMP))
 WHERE promotions_updated_at IS NULL;

ALTER TABLE promotions
    ALTER COLUMN promotions_updated_at SET DEFAULT CURRENT_TIMESTAMP;

-- NOT NULL ставим, только если в таблице не осталось NULL'ов (защита от падения миграции).
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM promotions WHERE promotions_updated_at IS NULL
    ) THEN
        ALTER TABLE promotions ALTER COLUMN promotions_updated_at SET NOT NULL;
    END IF;
END $$;
