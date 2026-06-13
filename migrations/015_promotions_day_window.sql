-- 015_promotions_day_window.sql
-- Акции действуют 1 день: valid_from = момент синка, valid_until = конец суток по Москве.
-- kind = 'day' для акций с МойСклад, 'manual' для созданных руками.
-- locked — есть открытые заказы, акцию раньше valid_until не отзываем.

ALTER TABLE promotions
    ADD COLUMN IF NOT EXISTS promotions_valid_from  TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS promotions_valid_until TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS promotions_kind        VARCHAR(32) NOT NULL DEFAULT 'day',
    ADD COLUMN IF NOT EXISTS promotions_locked      BOOLEAN     NOT NULL DEFAULT false;

CREATE INDEX IF NOT EXISTS idx_promotions_valid_until
    ON promotions(promotions_valid_until)
    WHERE promotions_valid_until IS NOT NULL;

-- Бэкфилл для уже синкнутых акций: окно действия = сутки от created_at (Москва — это часовой пояс, в БД храним UTC).
UPDATE promotions
   SET promotions_valid_from  = COALESCE(promotions_valid_from,  promotions_created_at),
       promotions_valid_until = COALESCE(promotions_valid_until, promotions_created_at + INTERVAL '1 day'),
       promotions_kind        = COALESCE(NULLIF(promotions_kind, ''), 'day')
 WHERE promotions_valid_from IS NULL OR promotions_valid_until IS NULL;

-- Промо-акции без kind'а (NULL после миграции) считаем 'day' — это безопасно для фильтров.
UPDATE promotions
   SET promotions_kind = 'day'
 WHERE promotions_kind IS NULL OR promotions_kind = '';
