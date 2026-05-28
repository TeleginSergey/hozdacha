-- 013_promotions_moysklad_id.sql
-- Идентификатор акции в МойСклад для синхронизации specialpricediscount.
-- NULL — акция создана вручную в админке и в МойСклад не отражена.

ALTER TABLE promotions
    ADD COLUMN IF NOT EXISTS promotions_moysklad_id VARCHAR(64);

CREATE UNIQUE INDEX IF NOT EXISTS promotions_moysklad_id_uniq
    ON promotions(promotions_moysklad_id)
    WHERE promotions_moysklad_id IS NOT NULL;
