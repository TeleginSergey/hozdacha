-- Удаляем поле адреса из таблицы users
ALTER TABLE users DROP COLUMN IF EXISTS users_address;

-- Добавляем поля полного имени и телефона если их нет
ALTER TABLE users ADD COLUMN IF NOT EXISTS users_full_name VARCHAR(255);
ALTER TABLE users ADD COLUMN IF NOT EXISTS users_phone VARCHAR(50) NOT NULL DEFAULT '';

-- Обновляем существующие NULL значения перед добавлением ограничений
UPDATE users SET users_full_name = '' WHERE users_full_name IS NULL;
UPDATE users SET users_phone = '' WHERE users_phone IS NULL;

-- Добавляем ограничения для обязательных полей
ALTER TABLE users ALTER COLUMN users_full_name SET NOT NULL;
ALTER TABLE users ALTER COLUMN users_phone SET NOT NULL;
