-- Добавление колонок для верификации email через 6-значный код

-- Добавляем колонку для хранения 6-значного кода верификации
ALTER TABLE users 
ADD COLUMN IF NOT EXISTS users_email_verification_code VARCHAR(6);

-- Добавляем колонку для времени истечения кода верификации
ALTER TABLE users 
ADD COLUMN IF NOT EXISTS users_verification_expires_at TIMESTAMP;

-- Создаем индекс для быстрого поиска по email и коду
CREATE INDEX IF NOT EXISTS idx_users_email_code 
ON users(users_email, users_email_verification_code);

-- Обновляем существующих пользователей как верифицированных (для обратной совместимости)
UPDATE users 
SET users_email_verified = true 
WHERE users_email_verified IS NULL;

COMMENT ON COLUMN users.users_email_verification_code IS '6-значный код верификации email';
COMMENT ON COLUMN users.users_verification_expires_at IS 'Время истечения кода верификации (TTL 30 минут)';
