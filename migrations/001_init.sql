-- Создание таблицы ролей
CREATE TABLE IF NOT EXISTS roles (
    roles_id_pk SERIAL PRIMARY KEY,
    roles_name VARCHAR(50) NOT NULL UNIQUE,
    roles_created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Вставка ролей
INSERT INTO roles (roles_name) VALUES 
('admin'),
('user') ON CONFLICT (roles_name) DO NOTHING;

-- Создание таблицы пользователей
CREATE TABLE IF NOT EXISTS users (
    users_id_pk SERIAL PRIMARY KEY,
    users_username VARCHAR(100) NOT NULL UNIQUE,
    users_email VARCHAR(255) NOT NULL UNIQUE,
    users_password_hash VARCHAR(255) NOT NULL,
    users_roles_id_fk INTEGER REFERENCES roles(roles_id_pk),
    users_access_token_secret VARCHAR(255),
    users_refresh_token_secret VARCHAR(255),
    users_access_token_jti VARCHAR(255),
    users_refresh_token_jti VARCHAR(255),
    users_auth_time TIMESTAMP,
    users_email_verified BOOLEAN DEFAULT false,
    users_email_verification_token VARCHAR(255),
    users_website VARCHAR(255),
    users_created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    users_updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Администратор создаётся после деплоя через CLI:
--   ./scripts/create_admin.sh <username> <email> <password>
-- или напрямую:
--   docker compose exec app ./create_admin admin admin@hozdacha.ru <password>

-- Создание таблицы категорий
CREATE TABLE IF NOT EXISTS categories (
    categories_id_pk SERIAL PRIMARY KEY,
    categories_name VARCHAR(255) NOT NULL,
    categories_description TEXT,
    categories_created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Создание таблицы товаров
CREATE TABLE IF NOT EXISTS products (
    products_id_pk SERIAL PRIMARY KEY,
    products_name VARCHAR(255) NOT NULL,
    products_description TEXT,
    products_price DECIMAL(10, 2) NOT NULL,
    products_image_url VARCHAR(500),
    products_moysklad_id VARCHAR(255) UNIQUE,
    products_category_id_fk INTEGER REFERENCES categories(categories_id_pk),
    products_stock INTEGER DEFAULT 0,
    products_active BOOLEAN DEFAULT true,
    products_created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    products_updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Индекс для поиска товаров
CREATE INDEX IF NOT EXISTS idx_products_name ON products(products_name);
CREATE INDEX IF NOT EXISTS idx_products_active ON products(products_active);
CREATE INDEX IF NOT EXISTS idx_products_moysklad_id ON products(products_moysklad_id);

-- Создание таблицы акций
CREATE TABLE IF NOT EXISTS promotions (
    promotions_id_pk SERIAL PRIMARY KEY,
    promotions_title VARCHAR(255) NOT NULL,
    promotions_description TEXT,
    promotions_discount DECIMAL(5, 2) DEFAULT 0,
    promotions_image_url VARCHAR(500),
    promotions_active BOOLEAN DEFAULT true,
    promotions_start_date TIMESTAMP,
    promotions_end_date TIMESTAMP,
    promotions_created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    promotions_updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Создание таблицы заказов
CREATE TABLE IF NOT EXISTS orders (
    orders_id_pk SERIAL PRIMARY KEY,
    orders_user_id_fk INTEGER REFERENCES users(users_id_pk),
    orders_status VARCHAR(50) DEFAULT 'pending',
    orders_total_price DECIMAL(10, 2) NOT NULL,
    orders_customer_name VARCHAR(255) NOT NULL,
    orders_phone VARCHAR(50) NOT NULL,
    orders_address TEXT,
    orders_comment TEXT,
    orders_moysklad_id VARCHAR(255),
    orders_created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    orders_updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Создание таблицы элементов заказа
CREATE TABLE IF NOT EXISTS order_items (
    order_items_id_pk SERIAL PRIMARY KEY,
    order_items_order_id_fk INTEGER NOT NULL REFERENCES orders(orders_id_pk) ON DELETE CASCADE,
    order_items_product_id_fk INTEGER NOT NULL REFERENCES products(products_id_pk),
    order_items_quantity INTEGER NOT NULL DEFAULT 1,
    order_items_price DECIMAL(10, 2) NOT NULL
);

-- Индексы для заказов
CREATE INDEX IF NOT EXISTS idx_orders_user_id ON orders(orders_user_id_fk);
CREATE INDEX IF NOT EXISTS idx_orders_status ON orders(orders_status);
CREATE INDEX IF NOT EXISTS idx_order_items_order_id ON order_items(order_items_order_id_fk);



