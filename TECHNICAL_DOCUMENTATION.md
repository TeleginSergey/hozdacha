# Техническая документация - Хозяйкин Дом

## 📋 Содержание

1. [Общее описание](#общее-описание)
2. [Архитектура системы](#архитектура-системы)
3. [Технологический стек](#технологический-стек)
4. [Структура проекта](#структура-проекта)
5. [База данных](#база-данных)
6. [API Reference](#api-reference)
7. [Аутентификация и авторизация](#аутентификация-и-авторизация)
8. [Интеграция с МойСклад](#интеграция-с-мойсклад)
9. [Административная панель](#административная-панель)
10. [Система заказов и возвратов](#система-заказов-и-возвратов)
11. [Почтовая система](#почтовая-система)
12. [Деплой и инфраструктура](#деплой-и-инфраструктура)
13. [Безопасность](#безопасность)
14. [Мониторинг и логирование](#мониторинг-и-логирование)

---

## Общее описание

**** - это полнофункциональный интернет-магазин хозяйственных товаров с интеграцией системы учета МойСклад. Приложение обеспечивает автоматическую синхронизацию товаров и остатков, систему управления заказами, а также комплексную систему аутентификации пользователей с верификацией email.

### Ключевые возможности:
- **Каталог товаров** с поиском и фильтрацией
- **Корзина и оформление заказов** с бронированием товаров
- **Личный кабинет** с историей заказов и управлением профилем
- **Email верификация** с 6-значным кодом подтверждения
- **Сброс пароля** через почту
- **Административная панель** с управлением пользователями, товарами и заказами
- **Интеграция МойСклад** с вебхуками для real-time синхронизации остатков
- **Система возвратов** с учетом бронирования товаров
- **HTTPS и SSL** через Let's Encrypt

---

## Архитектура системы

### Диаграмма компонентов

```
┌─────────────────────────────────────────────────────────────┐
│                        Клиенты                              │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │   Браузер   │  │  Мобильные  │  │   МойСклад API     │  │
│  │             │  │   клиенты   │  │   (Вебхуки)        │  │
│  └──────┬──────┘  └──────┬──────┘  └──────────┬──────────┘  │
└─────────┼────────────────┼───────────────────┼──────────────┘
          │                │                   │
          └────────────────┴───────────────────┘
                           │
                    ┌──────▼──────┐
                    │   Nginx     │  ← Reverse Proxy, SSL
                    │ (HTTPS)     │
                    └──────┬──────┘
                           │
              ┌────────────┼────────────┐
              │            │            │
       ┌──────▼─────┐ ┌────▼─────┐ ┌───▼────┐
       │    API     │ │  Static  │ │ Webhook│
       │   Server   │ │  Files   │ │ Handler│
       │  (Go/Gin)  │ │          │ │        │
       └─────┬──────┘ └──────────┘ └────────┘
             │
    ┌────────┼────────┐
    │        │        │
┌───▼───┐ ┌──▼───┐ ┌─▼────┐
│PostgreSQL│ │ Redis │ │Postfix│
│   DB    │ │ Cache │ │ Mail  │
└─────────┘ └───────┘ └───────┘
```

### Слоистая архитектура Backend

```
┌─────────────────────────────────────┐
│           Presentation Layer        │
│  ┌─────────┐ ┌─────────┐ ┌───────┐ │
│  │ Handlers│ │Middleware│ │Router│ │
│  └────┬────┘ └────┬────┘ └───┬───┘ │
└───────┼───────────┼─────────┼───────┘
        │           │         │
┌───────▼───────────▼─────────▼───────┐
│           Business Layer            │
│  ┌─────────┐ ┌─────────┐ ┌───────┐ │
│  │Usecase  │ │ Services│ │Domain │ │
│  └────┬────┘ └────┬────┘ └───┬───┘ │
└───────┼───────────┼─────────┼───────┘
        │           │         │
┌───────▼───────────▼─────────▼───────┐
│           Data Layer                │
│  ┌─────────┐ ┌─────────┐ ┌───────┐ │
│  │Repository│ │  Cache  │ │External│ │
│  └─────────┘ └─────────┘ └───────┘ │
└─────────────────────────────────────┘
```

---

## Технологический стек

### Backend
| Компонент | Технология | Версия | Назначение |
|-----------|-----------|--------|------------|
| Язык | Go | 1.25 | Основной язык разработки |
| Web Framework | Gin | 1.12 | HTTP сервер и роутинг |
| База данных | PostgreSQL | 15 | Основное хранилище данных |
| Кеширование | Redis | 7 | Сессии, кеш остатков, rate limiting |
| Аутентификация | JWT | v5 | Токены доступа |
| ORM/Query Builder | SQL + Scany | v2 | Работа с БД |
| Логирование | Zap | 1.27 | Структурированные логи |
| Валидация | Validator | v10 | Валидация входных данных |

### Инфраструктура
| Компонент | Технология | Назначение |
|-----------|-----------|------------|
| Контейнеризация | Docker + Compose | Изоляция сервисов |
| Reverse Proxy | Nginx | SSL termination, routing |
| SSL Certificates | Let's Encrypt | HTTPS соединения |
| Платформа деплоя | Dokploy | Автоматический деплой |
| Почтовый сервер | Postfix | Отправка email |

### Внешние интеграции
| Сервис | API | Назначение |
|--------|-----|------------|
| МойСклад | JSON API 1.2 | Синхронизация товаров и остатков |

---

## Структура проекта

```
telegins_shop/
├── cmd/
│   └── main.go                 # Точка входа приложения
├── internal/
│   ├── app/
│   │   └── app.go              # Инициализация приложения
│   ├── config/
│   │   └── config.go           # Конфигурация из env
│   ├── db/
│   │   ├── db.go               # Подключение к PostgreSQL
│   │   ├── users.go            # Запросы пользователей
│   │   ├── products.go         # Запросы товаров
│   │   ├── orders.go           # Запросы заказов
│   │   └── cart.go             # Запросы корзины
│   ├── handlers/
│   │   ├── router.go           # Маршрутизация
│   │   ├── auth.go             # Аутентификация
│   │   ├── user.go             # Управление пользователями
│   │   ├── product.go          # Товары
│   │   ├── order.go            # Заказы
│   │   ├── cart.go             # Корзина
│   │   ├── promotion.go        # Акции
│   │   ├── admin.go            # Админ панель
│   │   ├── webhook.go          # Вебхуки МойСклад
│   │   └── moysklad_sync.go    # Синхронизация
│   ├── middleware/
│   │   ├── auth.go             # JWT middleware
│   │   ├── cors.go             # CORS
│   │   ├── rate_limit.go       # Rate limiting
│   │   └── security.go         # Security headers
│   ├── models/
│   │   ├── user.go             # Модели пользователей
│   │   ├── product.go          # Модели товаров
│   │   ├── order.go            # Модели заказов
│   │   └── cart.go             # Модели корзины
│   ├── services/
│   │   ├── order.go            # Бизнес-логика заказов
│   │   ├── scheduler.go        # Планировщик задач
│   │   └── moysklad_sync.go    # Сервис синхронизации
│   ├── usecase/
│   │   ├── user.go             # Usecase пользователей
│   │   ├── order.go            # Usecase заказов
│   │   └── cart.go             # Usecase корзины
│   ├── moysklad/
│   │   └── client.go           # Клиент API МойСклад
│   ├── cache/
│       └── stock.go            # Кеш остатков
├── web/
│   ├── templates/              # HTML шаблоны
│   │   ├── index.html
│   │   ├── catalog.html
│   │   ├── cart.html
│   │   ├── login.html
│   │   ├── register.html
│   │   ├── profile.html
│   │   └── admin.html
│   └── static/                 # CSS, JS, изображения
│       └── css/
│       └── js/
├── migrations/                 # SQL миграции
│   ├── 001_init.sql
│   ├── 002_products_status.sql
│   └── 003_create_cart_items.sql
├── docker-compose.yml          # Docker Compose конфиг
├── Dockerfile                  # Docker образ
└── .env                        # Переменные окружения
```

---

## База данных

### ER-диаграмма

```
┌─────────────┐       ┌─────────────┐       ┌─────────────┐
│   roles     │       │    users    │       │   orders    │
├─────────────┤       ├─────────────┤       ├─────────────┤
│ roles_id_pk │◄──────│ users_roles │       │ orders_id_pk│
│ roles_name  │       ├─────────────┤       │ orders_users│
└─────────────┘       │ users_id_pk │◄──────┤ orders_total│
                      │ users_name  │       │ orders_stat │
┌─────────────┐       │ users_email │       │ orders_creat│
│  products   │       │ users_pass  │       └─────────────┘
├─────────────┤       │ users_phone │              │
│ products_id │       │ users_verify│       ┌──────┴──────┐
│ products_sku│       │ users_code  │       │ order_items │
│ products_nam│       └─────────────┘       ├─────────────┤
│ products_pri│              │              │ item_id_pk  │
│ products_sto│              │              │ item_order  │
│ products_ext│              │              │ item_product│
└─────────────┘              │              │ item_qty    │
       ▲                     │              │ item_price  │
       │                     │              └─────────────┘
       │              ┌──────┴──────┐              │
       │              │ cart_items  │              │
       │              ├─────────────┤              │
       │              │ cart_id_pk  │              │
       │              │ cart_user   │              │
       └──────────────┤ cart_product│◄─────────────┘
                      │ cart_qty    │
                      └─────────────┘
```

### Таблицы

#### 1. roles - Роли пользователей
```sql
CREATE TABLE roles (
    roles_id_pk SERIAL PRIMARY KEY,
    roles_name VARCHAR(50) NOT NULL UNIQUE,
    roles_created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Данные:
-- 1: admin
-- 2: user
```

#### 2. users - Пользователи
```sql
CREATE TABLE users (
    users_id_pk SERIAL PRIMARY KEY,
    users_username VARCHAR(100) NOT NULL UNIQUE,
    users_email VARCHAR(255) NOT NULL UNIQUE,
    users_password_hash VARCHAR(255) NOT NULL,
    users_roles_id_fk INTEGER REFERENCES roles(roles_id_pk),
    users_phone VARCHAR(20),
    users_name VARCHAR(255),
    users_address TEXT,
    users_email_verified BOOLEAN DEFAULT false,
    users_verification_code VARCHAR(6),
    users_verification_expires TIMESTAMP,
    users_reset_token VARCHAR(255),
    users_reset_expires TIMESTAMP,
    users_created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    users_updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

#### 3. products - Товары
```sql
CREATE TABLE products (
    products_id_pk SERIAL PRIMARY KEY,
    products_external_id VARCHAR(100) UNIQUE,  -- ID из МойСклад
    products_sku VARCHAR(100) UNIQUE,
    products_name VARCHAR(255) NOT NULL,
    products_description TEXT,
    products_price DECIMAL(10, 2) NOT NULL,
    products_stock DECIMAL(10, 3) DEFAULT 0,
    products_category VARCHAR(100),
    products_image_url TEXT,
    products_status VARCHAR(20) DEFAULT 'active',
    products_last_sync TIMESTAMP,
    products_created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    products_updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

#### 4. orders - Заказы
```sql
CREATE TABLE orders (
    orders_id_pk SERIAL PRIMARY KEY,
    orders_users_id_fk INTEGER REFERENCES users(users_id_pk),
    orders_total_price DECIMAL(10, 2) NOT NULL,
    orders_status VARCHAR(20) DEFAULT 'pending', -- pending, confirmed, shipped, delivered, cancelled, returned
    orders_customer_name VARCHAR(255),
    orders_phone VARCHAR(20),
    orders_address TEXT,
    orders_comment TEXT,
    orders_reserved_until TIMESTAMP,  -- Время окончания бронирования
    orders_created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    orders_updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

#### 5. order_items - Элементы заказа
```sql
CREATE TABLE order_items (
    order_items_id_pk SERIAL PRIMARY KEY,
    order_items_orders_id_fk INTEGER REFERENCES orders(orders_id_pk) ON DELETE CASCADE,
    order_items_products_id_fk INTEGER REFERENCES products(products_id_pk),
    order_items_quantity INTEGER NOT NULL,
    order_items_price DECIMAL(10, 2) NOT NULL,
    order_items_created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

#### 6. cart_items - Корзина
```sql
CREATE TABLE cart_items (
    cart_items_id_pk SERIAL PRIMARY KEY,
    cart_items_users_id_fk INTEGER REFERENCES users(users_id_pk) ON DELETE CASCADE,
    cart_items_products_id_fk INTEGER REFERENCES products(products_id_pk) ON DELETE CASCADE,
    cart_items_quantity INTEGER NOT NULL DEFAULT 1,
    cart_items_created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(cart_items_users_id_fk, cart_items_products_id_fk)
);
```

#### 7. promotions - Акции
```sql
CREATE TABLE promotions (
    promotions_id_pk SERIAL PRIMARY KEY,
    promotions_title VARCHAR(255) NOT NULL,
    promotions_description TEXT,
    promotions_discount_percent DECIMAL(5, 2),
    promotions_image_url TEXT,
    promotions_is_active BOOLEAN DEFAULT true,
    promotions_start_date TIMESTAMP,
    promotions_end_date TIMESTAMP,
    promotions_created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

#### 8. stock_reservations - Бронирование остатков
```sql
CREATE TABLE stock_reservations (
    reservation_id_pk SERIAL PRIMARY KEY,
    reservation_orders_id_fk INTEGER REFERENCES orders(orders_id_pk) ON DELETE CASCADE,
    reservation_products_id_fk INTEGER REFERENCES products(products_id_pk),
    reservation_quantity DECIMAL(10, 3) NOT NULL,
    reservation_expires_at TIMESTAMP NOT NULL,
    reservation_status VARCHAR(20) DEFAULT 'active', -- active, released, converted
    reservation_created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

---

## API Reference

### Аутентификация

#### POST /api/auth/register
Регистрация нового пользователя с отправкой кода верификации.

**Request:**
```json
{
  "username": "ivan123",
  "email": "ivan@example.com",
  "password": "SecurePass123!",
  "phone": "+79123456789",
  "name": "Иван Петров"
}
```

**Response (201 Created):**
```json
{
  "message": "Registration successful. Verification code sent to email.",
  "user_id": 123,
  "email": "ivan@example.com"
}
```

**Security:**
- Rate limiting: max 3 попытки в час с одного IP
- Honeypot поле для защиты от ботов
- Пароль должен содержать min 8 символов, цифры, заглавные буквы

#### POST /api/auth/verify-email
Подтверждение email по 6-значному коду.

**Request:**
```json
{
  "email": "ivan@example.com",
  "code": "123456"
}
```

**Response (200 OK):**
```json
{
  "message": "Email verified successfully",
  "token": "eyJhbGciOiJIUzI1NiIs...",
  "user": {
    "id": 123,
    "username": "ivan123",
    "email": "ivan@example.com",
    "email_verified": true
  }
}
```

**Ошибки:**
- 400: Invalid or expired verification code
- 429: Too many attempts

#### POST /api/auth/login
Авторизация пользователя.

**Request:**
```json
{
  "username": "ivan123",
  "password": "SecurePass123!"
}
```

**Response (200 OK):**
```json
{
  "message": "Login successful",
  "token": "eyJhbGciOiJIUzI1NiIs...",
  "user": {
    "id": 123,
    "username": "ivan123",
    "email": "ivan@example.com",
    "email_verified": true,
    "role": "user"
  }
}
```

#### POST /api/auth/forgot-password
Запрос на сброс пароля (отправка кода на email).

**Request:**
```json
{
  "email": "ivan@example.com"
}
```

**Response (200 OK):**
```json
{
  "message": "Password reset code sent to email"
}
```

#### POST /api/auth/reset-password
Сброс пароля с использованием кода.

**Request:**
```json
{
  "email": "ivan@example.com",
  "code": "123456",
  "new_password": "NewSecurePass456!"
}
```

**Response (200 OK):**
```json
{
  "message": "Password reset successful"
}
```

### Пользователи

#### GET /api/auth/profile
Получение профиля текущего пользователя.

**Headers:** `Authorization: Bearer <token>`

**Response (200 OK):**
```json
{
  "user": {
    "id": 123,
    "username": "ivan123",
    "email": "ivan@example.com",
    "name": "Иван Петров",
    "phone": "+79123456789",
    "address": "г. Москва, ул. Ленина 1",
    "email_verified": true,
    "created_at": "2024-01-15T10:30:00Z"
  }
}
```

#### PUT /api/auth/profile
Обновление профиля.

**Headers:** `Authorization: Bearer <token>`

**Request:**
```json
{
  "name": "Иван Иванов",
  "phone": "+79123456789",
  "address": "г. Москва, ул. Ленина 1, кв 10"
}
```

### Товары

#### GET /api/products
Получение списка товаров с пагинацией.

**Query Parameters:**
- `page` (int): Номер страницы (default: 1)
- `limit` (int): Количество на странице (default: 20, max: 100)
- `category` (string): Фильтр по категории
- `search` (string): Поиск по названию
- `sort` (string): Сортировка (price_asc, price_desc, name, newest)

**Response (200 OK):**
```json
{
  "products": [
    {
      "id": 1,
      "sku": "HOZ-001",
      "name": "Молоток строительный",
      "description": "Профессиональный молоток",
      "price": 599.99,
      "stock": 15.000,
      "category": "Инструменты",
      "image_url": "https://...",
      "status": "active"
    }
  ],
  "pagination": {
    "total": 150,
    "page": 1,
    "limit": 20,
    "total_pages": 8
  }
}
```

#### GET /api/products/:id
Получение детальной информации о товаре.

**Response (200 OK):**
```json
{
  "id": 1,
  "sku": "HOZ-001",
  "name": "Молоток строительный",
  "description": "Профессиональный молоток 500г",
  "price": 599.99,
  "stock": 15.000,
  "category": "Инструменты",
  "image_url": "https://...",
  "status": "active",
  "last_sync": "2024-01-15T12:00:00Z"
}
```

### Корзина

#### GET /api/cart
Получение содержимого корзины.

**Headers:** `Authorization: Bearer <token>`

**Response (200 OK):**
```json
{
  "cart": [
    {
      "id": 1,
      "product_id": 5,
      "quantity": 2,
      "product": {
        "id": 5,
        "name": "Отвертка",
        "price": 299.99,
        "stock": 20
      },
      "subtotal": 599.98
    }
  ],
  "total": 599.98,
  "item_count": 2
}
```

#### POST /api/cart
Добавление товара в корзину.

**Headers:** `Authorization: Bearer <token>`

**Request:**
```json
{
  "product_id": 5,
  "quantity": 2
}
```

**Response (200 OK):**
```json
{
  "message": "Product added to cart",
  "cart_item": {
    "id": 1,
    "product_id": 5,
    "quantity": 2
  }
}
```

**Ошибки:**
- 400: Insufficient stock (если товара меньше чем в корзине)
- 404: Product not found

#### PUT /api/cart
Обновление количества товара в корзине.

**Request:**
```json
{
  "product_id": 5,
  "quantity": 3
}
```

#### DELETE /api/cart/:product_id
Удаление товара из корзины.

#### DELETE /api/cart
Очистка всей корзины.

### Заказы

#### POST /api/orders
Создание заказа из корзины.

**Headers:** `Authorization: Bearer <token>`

**Request:**
```json
{
  "comment": "Доставка после 18:00"
}
```

**Response (201 Created):**
```json
{
  "order_id": 456,
  "message": "Order created successfully",
  "order": {
    "id": 456,
    "total_price": 1599.97,
    "status": "pending",
    "customer_name": "Иван Петров",
    "phone": "+79123456789",
    "address": "г. Москва, ул. Ленина 1",
    "reserved_until": "2024-01-15T18:30:00Z",
    "items": [
      {
        "product_id": 5,
        "name": "Отвертка",
        "quantity": 2,
        "price": 299.99
      },
      {
        "product_id": 8,
        "name": "Гвозди 100шт",
        "quantity": 1,
        "price": 999.99
      }
    ]
  }
}
```

**Бизнес-логика:**
1. Проверка наличия товаров в корзине
2. Валидация обязательных полей профиля (имя, телефон)
3. Резервирование остатков (бронирование на 24 часа)
4. Создание заказа в статусе "pending"
5. Отправка уведомления в Telegram
6. Очистка корзины

#### GET /api/orders
Получение истории заказов пользователя.

**Headers:** `Authorization: Bearer <token>`

**Response (200 OK):**
```json
{
  "orders": [
    {
      "id": 456,
      "total_price": 1599.97,
      "status": "confirmed",
      "created_at": "2024-01-15T14:30:00Z",
      "item_count": 2
    },
    {
      "id": 455,
      "total_price": 899.99,
      "status": "delivered",
      "created_at": "2024-01-10T11:20:00Z",
      "item_count": 1
    }
  ]
}
```

#### GET /api/orders/:id
Получение деталей заказа.

#### POST /api/orders/:id/cancel
Отмена заказа пользователем.

**Условия:**
- Заказ можно отменить только в статусе "pending" или "confirmed"
- При отмене происходит освобождение зарезервированных остатков

### Административные API

#### POST /api/admin/login
Вход в админ-панель.

**Request:**
```json
{
  "username": "admin",
  "password": "AdminSecurePass123!"
}
```

#### GET /api/admin/users
Получение списка пользователей (только для admin).

**Headers:** `Authorization: Bearer <admin_token>`

**Query Parameters:**
- `page`, `limit`: Пагинация
- `verified`: Фильтр по верификации (true/false)
- `search`: Поиск по имени/email

**Response (200 OK):**
```json
{
  "users": [
    {
      "id": 123,
      "username": "ivan123",
      "email": "ivan@example.com",
      "name": "Иван Петров",
      "phone": "+79123456789",
      "email_verified": true,
      "role": "user",
      "created_at": "2024-01-15T10:30:00Z",
      "orders_count": 5
    }
  ],
  "pagination": {
    "total": 150,
    "page": 1,
    "limit": 20
  }
}
```

#### GET /api/admin/users/:id
Получение деталей пользователя.

#### PUT /api/admin/users/:id
Обновление данных пользователя администратором.

**Request:**
```json
{
  "role": "admin",
  "email_verified": true,
  "name": "Иван Иванов"
}
```

#### DELETE /api/admin/users/:id
Удаление пользователя.

#### GET /api/admin/orders
Получение всех заказов (для администратора).

**Query Parameters:**
- `status`: Фильтр по статусу
- `date_from`, `date_to`: Фильтр по дате
- `search`: Поиск по имени клиента

#### PUT /api/admin/orders/:id/status
Изменение статуса заказа.

**Request:**
```json
{
  "status": "confirmed",
  "comment": "Заказ подтвержден, ожидает отправки"
}
```

**Доступные статусы:**
- `pending` - Ожидает подтверждения
- `confirmed` - Подтвержден
- `shipped` - Отправлен
- `delivered` - Доставлен
- `cancelled` - Отменен
- `returned` - Возвращен

#### POST /api/admin/orders/:id/return
Оформление возврата заказа.

**Request:**
```json
{
  "reason": "Товар не подошел",
  "items": [
    {
      "product_id": 5,
      "quantity": 1,
      "reason": "Брак"
    }
  ]
}
```

**Бизнес-логика:**
1. Проверка возможности возврата (доставленный заказ)
2. Создание записи о возврате
3. Возврат остатков на склад
4. Обновление статуса заказа

#### POST /api/admin/products/sync
Ручная синхронизация с МойСклад.

**Response (200 OK):**
```json
{
  "message": "Synchronization completed",
  "result": {
    "created": 15,
    "updated": 42,
    "errors": 0,
    "duration": "3.5s"
  }
}
```

### Вебхуки МойСклад

#### POST /api/webhooks/moysklad
Обработчик событий от МойСклад.

**Заголовки:**
- `X-MoySklad-Signature`: HMAC-SHA256 подпись

**События:**
- `CREATE` - Создание товара
- `UPDATE` - Изменение товара
- `DELETE` - Удаление товара

#### POST /api/webhooks/stock
Обработчик изменения остатков (webhookstock).

**Payload:**
```json
{
  "events": [
    {
      "meta": {
        "type": "webhookstock",
        "href": "https://api.moysklad.ru/api/remap/1.2/report/stock/all"
      },
      "accountId": "..."
    }
  ]
}
```

**Обработка:**
1. Проверка подписи вебхука
2. Запрос отчета по остаткам по URL
3. Обновление остатков в БД
4. Инвалидация кеша Redis

---

## Аутентификация и авторизация

### JWT Токены

Приложение использует JWT (JSON Web Tokens) для аутентификации.

**Структура токена:**
```json
{
  "header": {
    "alg": "HS256",
    "typ": "JWT"
  },
  "payload": {
    "user_id": 123,
    "username": "ivan123",
    "email": "ivan@example.com",
    "role": "user",
    "exp": 1705324800,
    "iat": 1705238400
  }
}
```

**Настройки:**
- Алгоритм: HS256
- Время жизни: 24 часа
- Обновление: через повторный login

### Middleware аутентификации

#### AuthMiddleware
Извлекает и валидирует JWT токен из заголовка `Authorization: Bearer <token>`.

#### RequireAuth
Проверяет наличие аутентифицированного пользователя в контексте.

#### RequireAdmin
Проверяет, что пользователь имеет роль `admin`.

### Email верификация

**Процесс:**
1. При регистрации генерируется 6-значный код (например: `483921`)
2. Код отправляется на email через Postfix
3. Код валиден в течение 30 минут
4. Пользователь вводит код для активации аккаунта
5. После верификации выдается JWT токен

**Шаблон письма:**
```
Здравствуйте, {name}!

Код подтверждения: {code}

Введите этот код в поле подтверждения на сайте.
Код действителен в течение 30 минут.

Если вы не регистрировались на сайте, проигнорируйте это письмо.
```

### Сброс пароля

**Процесс:**
1. Пользователь запрашивает сброс пароля по email
2. Генерируется 6-значный код
3. Код отправляется на email
4. Пользователь вводит код и новый пароль
5. Пароль обновляется, старый токен становится невалидным

---

## Интеграция с МойСклад

### Архитектура синхронизации

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│   МойСклад      │────►│   Вебхуки       │────►│   Наше API      │
│   (Источник)    │     │   (Real-time)   │     │   (/webhooks/*) │
└─────────────────┘     └─────────────────┘     └─────────────────┘
         │                                               │
         │              ┌─────────────────┐              │
         └─────────────►│  Резервная      │◄─────────────┘
           (HTTP API)    │  синхронизация  │   (Обработка)
                        │  (раз в 1 час)  │
                        └─────────────────┘
                                 │
                    ┌────────────┼────────────┐
                    ▼            ▼            ▼
              ┌─────────┐ ┌──────────┐ ┌─────────┐
              │Обновить │ │Добавить  │ │Удалить  │
              │товар    │ │товар     │ │товар    │
              └─────────┘ └──────────┘ └─────────┘
```

### Методы синхронизации

#### 1. Полная синхронизация
- **Когда:** Первоначальная загрузка, восстановление после сбоев
- **API:** `GET /api/remap/1.2/entity/product`
- **Время:** ~5-10 минут для 1000 товаров
- **Ограничение:** Не чаще 1 раза в день

#### 2. Дельта-синхронизация
- **Когда:** Регулярная проверка изменений
- **API:** `GET /api/remap/1.2/entity/product?updatedFrom={timestamp}`
- **Периодичность:** Каждый 1 час (резерв)
- **Преимущество:** Минимальная нагрузка

#### 3. Вебхуки (Real-time)
- **Когда:** Немедленное обновление
- **События:** CREATE, UPDATE, DELETE товара
- **Задержка:** < 5 секунд
- **Типы webhookstock:** Изменение остатков

### Конфигурация вебхуков в МойСклад

**URL для вебхуков:**
- Товары: `https://your-domain.com/api/webhooks/moysklad`
- Остатки: `https://your-domain.com/api/webhooks/stock`

**Настройки:**
- Метод: POST
- Формат: JSON
- Подпись: HMAC-SHA256
- Секретный ключ: `${MOYSKLAD_WEBHOOK_SECRET}`

### Оптимизация запросов

**Rate Limiting:**
- 3 запроса в секунду
- 180 запросов в минуту
- Защита от превышения лимита МойСклад

**Кеширование:**
- Остатки кешируются в Redis
- Время жизни кеша: 5 минут
- Инвалидация при webhook событиях

---

## Административная панель

### Структура админ-панели

```
/admin
├── /                    - Dashboard (статистика)
├── /users               - Управление пользователями
│   ├── /list            - Список пользователей
│   ├── /:id/edit        - Редактирование пользователя
│   └── /:id/orders      - Заказы пользователя
├── /orders              - Управление заказами
│   ├── /list            - Все заказы
│   ├── /:id/view        - Детали заказа
│   └── /:id/edit        - Редактирование заказа
├── /products            - Управление товарами
│   ├── /list            - Список товаров
│   ├── /sync            - Синхронизация с МойСклад
│   └── /:id/edit        - Редактирование товара
├── /promotions          - Управление акциями
│   ├── /list            - Список акций
│   └── /create          - Создание акции
└── /returns             - Возвраты
    └── /list            - Список возвратов
```

### Функции управления пользователями

**Список пользователей:**
- Пагинация с поиском
- Фильтры: верифицирован/не верифицирован, дата регистрации
- Сортировка по дате, количеству заказов
- Быстрые действия: блокировка, смена роли

**Детали пользователя:**
- Профильные данные
- История заказов
- Статистика покупок
- Возможность редактирования данных
- Ручная верификация email
- Сброс пароля от имени администратора

### Функции управления заказами

**Список заказов:**
- Фильтры по статусу, дате, сумме
- Поиск по имени клиента, номеру заказа
- Массовые действия (изменение статуса)
- Экспорт в CSV

**Обработка заказа:**
- Просмотр содержимого
- Изменение статуса с комментарием
- Добавление трек-номера доставки
- Связь с клиентом (email)
- Оформление возврата

---

## Система заказов и возвратов

### Жизненный цикл заказа

```
┌─────────────┐
│   CART      │ ──► Пользователь добавляет товары в корзину
└──────┬──────┘
       │
       ▼
┌─────────────┐
│   PENDING   │ ──► Заказ создан, остатки зарезервированы
└──────┬──────┘     (24 часа на оплату/подтверждение)
       │
       ▼
┌─────────────┐
│  CONFIRMED  │ ──► Заказ подтвержден менеджером
└──────┬──────┘
       │
       ▼
┌─────────────┐
│   SHIPPED   │ ──► Заказ отправлен клиенту
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  DELIVERED  │ ──► Заказ доставлен (можно вернуть в течение 14 дней)
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  COMPLETED  │ ──► Заказ завершен
└─────────────┘

Альтернативные пути:
PENDING ──► CANCELLED (отмена пользователем или таймаут)
CONFIRMED ──► CANCELLED (отмена менеджером)
DELIVERED ──► RETURNED (возврат товара)
```

### Бронирование (резервирование) остатков

**Механизм:**
1. При создании заказа проверяется доступность товаров
2. Товары резервируются на 24 часа
3. Зарезервированные товары недоступны для других покупателей
4. Если заказ не подтвержден за 24 часа - резерв снимается
5. При отмене заказа резерв снимается немедленно

**Таблица резервов:**
```sql
stock_reservations:
- reservation_id: ID резерва
- order_id: ID заказа
- product_id: ID товара
- quantity: Количество
- expires_at: Время истечения резерва
- status: active | released | converted
```

**Доступный остаток для покупки:**
```
available_stock = total_stock - reserved_stock
```

### Система возвратов

**Условия возврата:**
- Заказ в статусе DELIVERED
- Прошло не более 14 дней с доставки
- Товар в сохраненном виде (не использовался)

**Процесс возврата:**
1. Клиент запрашивает возврат через личный кабинет
2. Менеджер рассматривает заявку
3. При одобрении:
   - Создается запись о возврате
   - Товары возвращаются на склад
   - Остатки обновляются
   - Статус заказа меняется на RETURNED
4. Возврат средств осуществляется отдельно (вне системы)

**Частичный возврат:**
- Возможен возврат отдельных товаров из заказа
- Остальные товары остаются у клиента
- Пересчет суммы заказа

---

## Почтовая система

### Конфигурация Postfix

**Установка на сервер:**
```bash
# Установка Postfix
sudo apt update
sudo apt install postfix mailutils

# Настройка
sudo nano /etc/postfix/main.cf
```

**Основные настройки:**
```
myhostname = mail.your-domain.com
mydomain = your-domain.com
myorigin = $mydomain
inet_interfaces = loopback-only
mydestination = $myhostname, localhost.$mydomain, localhost
relayhost =
mynetworks = 127.0.0.0/8
mailbox_size_limit = 0
recipient_delimiter = +
```

**Отправка email из Go:**
```go
import (
    "net/smtp"
    "fmt"
)

func SendVerificationEmail(to, name, code string) error {
    from := "noreply@your-domain.com"
    password := os.Getenv("SMTP_PASSWORD")
    
    msg := fmt.Sprintf(
        "To: %s\r\n" +
        "Subject: Код подтверждения регистрации\r\n" +
        "\r\n" +
        "Здравствуйте, %s!\r\n\r\n" +
        "Код подтверждения: %s\r\n\r\n" +
        "Код действителен в течение 30 минут.\r\n",
        to, name, code,
    )
    
    err := smtp.SendMail(
        "localhost:25",
        nil,
        from,
        []string{to},
        []byte(msg),
    )
    
    return err
}
```

### Переменные окружения для почты

```env
# SMTP настройки (локальный Postfix)
SMTP_HOST=localhost
SMTP_PORT=25
SMTP_FROM=noreply@your-domain.com
SMTP_PASSWORD=                    # Если требуется auth

# Или внешний SMTP (Gmail, Яндекс и т.д.)
# SMTP_HOST=smtp.gmail.com
# SMTP_PORT=587
# SMTP_USER=your-email@gmail.com
# SMTP_PASSWORD=your-app-password
```

### Типы писем

1. **Верификация email** - 6-значный код
2. **Сброс пароля** - 6-значный код
3. **Подтверждение заказа** - Детали заказа
4. **Изменение статуса заказа** - Уведомление клиенту
5. **Восстановление пароля администратором** - Временный пароль

---

## Деплой и инфраструктура

### Архитектура деплоя

```
┌─────────────────────────────────────────────────────────┐
│                       Пользователь                      │
└─────────────────────────┬───────────────────────────────┘
                          │ HTTPS
┌─────────────────────────▼───────────────────────────────┐
│                       Nginx                             │
│  - SSL termination (Let's Encrypt)                      │
│  - Reverse proxy                                        │
│  - Static files serving                               │
└─────────────────────────┬───────────────────────────────┘
                          │
┌─────────────────────────▼───────────────────────────────┐
│                    Dokploy Server                       │
│  ┌─────────────────────────────────────────────────────┐│
│  │              Docker Compose Stack                   ││
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐ ││
│  │  │     App     │  │  PostgreSQL │  │    Redis    │ ││
│  │  │   (Go)      │  │     DB      │  │   Cache     │ ││
│  │  │  Port 8080  │  │  Port 5432  │  │  Port 6379  │ ││
│  │  └─────────────┘  └─────────────┘  └─────────────┘ ││
│  └─────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────┘
```

### Настройка Dokploy

**1. Создание проекта:**
- Войдите в панель Dokploy
- Нажмите "New Project"
- Укажите название: `hozdacha`

**2. Настройка репозитория:**
- Git Provider: GitHub
- Repository: `TeleginSergey/hozdacha`
- Branch: `main`

**3. Конфигурация:**
- Build Type: `docker-compose`
- Compose File: `docker-compose.yml`

**4. Переменные окружения:**
Загрузите `.env` файл или укажите переменные вручную:
```
DB_HOST=postgres
DB_PORT=5432
DB_NAME=hozdacha
DB_USER=postgres
DB_PASSWORD=your_secure_password
JWT_SECRET=your_jwt_secret
MOYSKLAD_TOKEN=your_token
MOYSKLAD_WEBHOOK_SECRET=your_webhook_secret
GIN_MODE=release
```

### Настройка домена и HTTPS

**1. Добавление домена в Dokploy:**
- Перейдите в настройки проекта
- Раздел "Domains"
- Добавьте ваш домен: `your-domain.com`

**2. Настройка DNS:**
```
Type: A
Name: @
Value: <Dokploy Server IP>
TTL: 3600
```

**3. SSL сертификат (Let's Encrypt):**
- Dokploy автоматически запрашивает сертификат
- Проверка через HTTP challenge
- Автообновление каждые 90 дней

### Docker Compose для продакшена

```yaml
version: '3.8'

services:
  app:
    build:
      context: .
      dockerfile: Dockerfile
    container_name: hozdacha_app
    env_file:
      - .env
    environment:
      - GIN_MODE=release
      - LOG_LEVEL=info
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy
    volumes:
      - ./web:/root/web
    restart: unless-stopped
    networks:
      - hozdacha_network

  postgres:
    image: postgres:15-alpine
    container_name: hozdacha_db
    environment:
      POSTGRES_DB: ${DB_NAME}
      POSTGRES_USER: ${DB_USER}
      POSTGRES_PASSWORD: ${DB_PASSWORD}
    volumes:
      - postgres_data:/var/lib/postgresql/data
      - ./migrations:/docker-entrypoint-initdb.d
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${DB_USER:-postgres}"]
      interval: 10s
      timeout: 5s
      retries: 5
    restart: unless-stopped
    networks:
      - hozdacha_network

  redis:
    image: redis:7-alpine
    container_name: hozdacha_redis
    volumes:
      - redis_data:/data
    command: redis-server --appendonly yes
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 10s
      timeout: 5s
      retries: 5
    restart: unless-stopped
    networks:
      - hozdacha_network

volumes:
  postgres_data:
  redis_data:

networks:
  hozdacha_network:
    driver: bridge
```

### Миграции базы данных

**Автоматические миграции:**
При первом запуске PostgreSQL выполняет скрипты из `/docker-entrypoint-initdb.d/`:

```
migrations/
├── 001_init.sql           # Создание таблиц
├── 002_products_status.sql # Добавление статусов товаров
└── 003_create_cart_items.sql # Создание корзины
```

**Ручные миграции (при обновлении):**
```bash
# Выполнить SQL скрипт
docker compose exec postgres psql -U postgres -d hozdacha -f /docker-entrypoint-initdb.d/004_new_feature.sql
```

---

## Безопасность

### Аутентификация и авторизация

- **JWT токены** с ограниченным временем жизни (24 часа)
- **Хеширование паролей** с использованием bcrypt
- **Защита от CSRF** через SameSite cookies
- **CORS** настроен только для разрешенных доменов

### Защита от атак

**Rate Limiting:**
```go
// Общий rate limit: 100 запросов/минуту с IP
// Регистрация: max 3 попытки/час
// Логин: max 5 попыток/минуту
```

**SQL Injection:**
- Использование параметризованных запросов
- SQL Builder (squirrel) с экранированием

**XSS Protection:**
- Экранирование вывода в HTML шаблонах
- Content Security Policy заголовки

**Security Headers:**
```go
w.Header().Set("X-Content-Type-Options", "nosniff")
w.Header().Set("X-Frame-Options", "DENY")
w.Header().Set("X-XSS-Protection", "1; mode=block")
w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
```

### Валидация данных

**Входные данные:**
- Валидация email формата
- Проверка длины пароля (min 8 символов)
- Санитизация строк от HTML тегов
- Валидация числовых значений

**Загрузка файлов:**
- Проверка MIME типа
- Ограничение размера
- Сканирование на вирусы (внешний сервис)

### Секреты и ключи

**Хранение:**
- Все секреты в переменных окружения
- Никаких секретов в коде
- `.env` файл в `.gitignore`

**Обязательные переменные:**
- `JWT_SECRET` - минимум 32 символа
- `DB_PASSWORD` - надежный пароль
- `MOYSKLAD_WEBHOOK_SECRET` - для вебхуков
- `REDIS_PASSWORD` - если Redis открыт наружу

---

## Мониторинг и логирование

### Структурированные логи

**Библиотека:** Uber Zap

**Уровни логирования:**
- **DEBUG** - Отладочная информация
- **INFO** - Основные события (запросы, заказы)
- **WARN** - Предупреждения (повторные попытки)
- **ERROR** - Ошибки (недоступность сервисов)
- **FATAL** - Критические ошибки (остановка приложения)

**Пример лога:**
```json
{
  "level": "info",
  "ts": 1705324800.123,
  "caller": "handlers/order.go:45",
  "msg": "Order created",
  "order_id": 456,
  "user_id": 123,
  "total": 1599.97,
  "duration_ms": 150
}
```

### Health Checks

**Endpoint:** `GET /health`

**Response:**
```json
{
  "status": "healthy",
  "services": {
    "database": "connected",
    "redis": "connected",
    "moysklad_api": "available"
  },
  "timestamp": "2024-01-15T14:30:00Z"
}
```

**Docker Healthcheck:**
```dockerfile
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1
```

### Метрики производительности

**Отслеживаемые показатели:**
- Время ответа API (p50, p95, p99)
- Количество запросов в секунду
- Ошибки (5xx rate)
- Использование ресурсов (CPU, RAM)
- Размер базы данных

**Инструменты:**
- Prometheus + Grafana (опционально)
- Встроенные метрики в логах

### Алертинг

**Условия для уведомлений:**
- 5xx ошибки > 1% в течение 5 минут
- Время ответа > 500ms (p95)
- База данных недоступна
- Redis недоступен
- МойСклад API недоступен > 10 минут

**Каналы уведомлений:**
- Email администратору
- Telegram бот
- Slack webhook

---

## Приложения

### Приложение А. Полный список API endpoints

| Method | Endpoint | Auth | Описание |
|--------|----------|------|----------|
| POST | /api/auth/register | No | Регистрация |
| POST | /api/auth/verify-email | No | Верификация email |
| POST | /api/auth/login | No | Авторизация |
| POST | /api/auth/forgot-password | No | Запрос сброса пароля |
| POST | /api/auth/reset-password | No | Сброс пароля |
| GET | /api/auth/profile | Yes | Профиль пользователя |
| PUT | /api/auth/profile | Yes | Обновление профиля |
| GET | /api/products | No | Список товаров |
| GET | /api/products/:id | No | Детали товара |
| GET | /api/cart | Yes | Корзина |
| POST | /api/cart | Yes | Добавить в корзину |
| PUT | /api/cart | Yes | Обновить количество |
| DELETE | /api/cart/:id | Yes | Удалить из корзины |
| POST | /api/orders | Yes | Создать заказ |
| GET | /api/orders | Yes | История заказов |
| GET | /api/orders/:id | Yes | Детали заказа |
| POST | /api/orders/:id/cancel | Yes | Отменить заказ |
| POST | /api/admin/login | No | Вход администратора |
| GET | /api/admin/users | Admin | Список пользователей |
| PUT | /api/admin/users/:id | Admin | Обновить пользователя |
| GET | /api/admin/orders | Admin | Все заказы |
| PUT | /api/admin/orders/:id/status | Admin | Изменить статус |
| POST | /api/webhooks/moysklad | No | Вебхук МойСклад |
| POST | /api/webhooks/stock | No | Вебхук остатков |
| GET | /health | No | Health check |

### Приложение Б. Переменные окружения

| Переменная | Обязательная | Значение по умолчанию | Описание |
|------------|--------------|----------------------|----------|
| DB_HOST | Да | localhost | Хост PostgreSQL |
| DB_PORT | Да | 5432 | Порт PostgreSQL |
| DB_NAME | Да | hozdacha | Имя базы данных |
| DB_USER | Да | postgres | Пользователь БД |
| DB_PASSWORD | Да | - | Пароль БД |
| JWT_SECRET | Да | - | Секрет JWT |
| JWT_EXPIRATION | Нет | 24h | Время жизни токена |
| REDIS_HOST | Да | redis | Хост Redis |
| REDIS_PORT | Да | 6379 | Порт Redis |
| MOYSKLAD_TOKEN | Да* | - | Токен МойСклад |
| MOYSKLAD_WEBHOOK_SECRET | Да* | - | Секрет вебхуков |
| GIN_MODE | Нет | debug | Режим Gin |
| LOG_LEVEL | Нет | info | Уровень логов |
| SMTP_HOST | Нет | localhost | SMTP сервер |
| SMTP_PORT | Нет | 25 | SMTP порт |

*Обязательно если используется интеграция с МойСклад

### Приложение В. Коды ошибок

| Код | HTTP Status | Описание |
|-----|-------------|----------|
| 400 | Bad Request | Неверный формат запроса |
| 401 | Unauthorized | Не авторизован |
| 403 | Forbidden | Нет доступа |
| 404 | Not Found | Ресурс не найден |
| 409 | Conflict | Конфликт данных |
| 422 | Unprocessable Entity | Ошибка валидации |
| 429 | Too Many Requests | Превышен лимит запросов |
| 500 | Internal Server Error | Внутренняя ошибка |
| 502 | Bad Gateway | Ошибка прокси |
| 503 | Service Unavailable | Сервис недоступен |

---

## Контакты и поддержка

**Разработчик:** Сергей Телегин
**Репозиторий:** https://github.com/TeleginSergey/hozdacha
**Документация:** https://docs.your-domain.com
**Email поддержки:** support@your-domain.com

---

*Документация актуальна для версии 1.0.0*
*Последнее обновление: Май 2024*
