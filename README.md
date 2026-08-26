# Хоздача

Интернет-магазин с бронированием товаров и интеграцией с МойСклад.

## Стек

- **Go** 1.22+ (Gin, pgx, squirrel, scany)
- **PostgreSQL** 15+ (основное хранилище)
- **Redis** 7+ (кэш остатков)
- **МойСклад API** (учётная система, касса, CRM)

## Возможности

### Магазин
- Каталог товаров с поиском и фильтрацией по категориям
- Корзина (wishlist) с проверкой доступности
- Оформление заказа с атомарным списанием стока
- Бронирование товара на 48 часов
- Личный кабинет: профиль, история заказов, отмена брони

### Админ-панель (`/admin`)
- Дашборд: статистика по статусам заказов, процент no-show
- Список заказов с фильтрами (статус, дата, поиск по имени/телефону)
- Карточка заказа: состав, таймлайн событий, кнопки «Выкуплен» / «Истечь»
- Поиск клиентов по имени, email, телефону
- Карточка клиента: статистика, топ товаров, средний чек
- Управление акциями
- Ручная синхронизация с МойСклад (дельта / полная)

### Интеграция с МойСклад
- Webhook-first обновление остатков (реальное время)
- Создание CustomerOrder с резервом позиций при оформлении заказа
- Автоматическое удаление заказа в МС при отмене/истечении
- Дельта-синхронизация карточек товаров (резервная, раз в час)
- Полная пересинхронизация (раз в сутки)

### Аудит
- Лог всех событий по заказу: создание, синхронизация с МС, выкуп, отмена, истечение
- Фиксация actor (кто из админов выполнил действие)

## Быстрый старт

### Переменные окружения

```bash
DATABASE_URL=postgres://user:pass@localhost:5432/telegins_shop?sslmode=disable
REDIS_URL=redis://localhost:6379/0
JWT_SECRET=your-secret-key
MOYSKLAD_TOKEN=your-token
MOYSKLAD_WEBHOOK_SECRET=your-webhook-secret
MOYSKLAD_AUTO_SYNC=true
MOYSKLAD_SYNC_INTERVAL=1h
MOYSKLAD_RESEED_FULL_INTERVAL=24h
MOYSKLAD_STOCK_BUFFER=3.0
```

### Запуск

```bash
go mod download
go run cmd/server/main.go
# или
go build -o server cmd/server/main.go && ./server
```

Сервер на порту из `PORT` (по умолчанию 8080).

### Docker

```bash
docker build -t telegins-shop .
docker run -p 8080:8080 --env-file .env telegins-shop
```

## Структура

```
cmd/server/          # Точка входа
internal/
  app/               # Композиция зависимостей
  cache/             # Redis: кэш остатков, Lua-скрипты
  config/            # Конфигурация из env
  db/                # Доступ к данным (SQL)
  handlers/          # HTTP-обработчики (Gin)
  middleware/        # JWT, CORS, rate-limit
  moysklad/          # Клиент API МойСклад
  resilience/        # Circuit breaker, retry
  services/          # Синхронизация, email, планировщик
  usecase/           # Бизнес-логика
migrations/          # SQL-миграции
web/
  static/            # CSS, JS
  templates/         # HTML-шаблоны
```

## API

### Публичные

| Метод | Путь | Описание |
|-------|------|----------|
| GET | `/api/products` | Список товаров |
| GET | `/api/products/search?q=` | Поиск |
| GET | `/api/products/:id` | Карточка товара |
| GET | `/api/promotions` | Активные акции |
| POST | `/api/auth/register` | Регистрация |
| POST | `/api/auth/login` | Вход |

### Авторизованные (JWT)

| Метод | Путь | Описание |
|-------|------|----------|
| GET/PUT | `/api/auth/profile` | Профиль |
| GET/POST/PUT/DELETE | `/api/cart` | Корзина |
| POST | `/api/orders` | Создать заказ |
| GET | `/api/orders` | Мои заказы |
| POST | `/api/orders/:id/cancel` | Отменить бронь |

### Админские (JWT + admin)

| Метод | Путь | Описание |
|-------|------|----------|
| GET | `/api/admin/orders` | Список с фильтрами |
| GET | `/api/admin/orders/today` | Брони на сегодня |
| GET | `/api/admin/orders/stats` | Статистика |
| GET | `/api/admin/orders/lookup` | Быстрый поиск |
| GET | `/api/admin/orders/:id` | Карточка заказа |
| POST | `/api/admin/orders/:id/ship` | Выкуплен |
| POST | `/api/admin/orders/:id/expire` | Истечь |
| GET | `/api/admin/users` | Поиск клиентов |
| GET | `/api/admin/users/:id/stats` | Карточка клиента |
| POST | `/api/admin/products/sync` | Дельта-синк |
| POST | `/api/admin/products/sync/full` | Полный синк |

## Лицензия

MIT
