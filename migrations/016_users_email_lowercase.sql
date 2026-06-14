-- 016_users_email_lowercase.sql
-- Нормализуем существующие email: убираем пробелы по краям и приводим к нижнему регистру.
-- Email регистронезависим, но раньше хранился «как ввели», из-за чего вход/регистрация
-- были чувствительны к регистру.
--
-- На users_email есть UNIQUE-ограничение (миграция 001), причём в Postgres оно
-- регистрозависимое — поэтому в базе могли появиться дубли в разном регистре
-- (User@x и user@x как «разные»). Чтобы UPDATE не упал на таких коллизиях,
-- нормализуем только те строки, для которых нет другой записи с тем же
-- email в нижнем регистре. Оставшиеся редкие дубли всё равно находятся при входе
-- (сравнение идёт через LOWER), их можно при необходимости слить вручную.
UPDATE users u
SET users_email = lower(trim(u.users_email))
WHERE u.users_email <> lower(trim(u.users_email))
  AND NOT EXISTS (
      SELECT 1 FROM users u2
      WHERE u2.users_id_pk <> u.users_id_pk
        AND lower(trim(u2.users_email)) = lower(trim(u.users_email))
  );
