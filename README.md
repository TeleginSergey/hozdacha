# Хозяйкин Дом - Интернет-магазин хозяйственных товаров

Веб-сайт для магазина хозяйственных товаров с интеграцией МойСклад и email-верификацией для регистрации.

## Функционал

- 📦 Каталог товаров с поиском
- 🎯 Акции на главной странице
- 🛒 Корзина и оформление заказов
- 👨‍💼 Админ-панель для управления акциями и товарами
- � Регистрация с email-верификацией
- 🔗 Интеграция с МойСклад для синхронизации товаров

## Установка

### Вариант 1: С Docker (рекомендуется) ⭐

**Быстрый старт:**
```bash
# 1. Создайте .env файл из примера
cp .env.example .env
nano .env  # Заполните необходимые значения

# 2. Запустите
docker compose up -d --build

# 3. Создайте администратора
docker compose exec app sh -c "cd /root && ./create_admin admin admin@example.com your_password"

# 4. Откройте http://localhost:8081
```

Приложение будет доступно по адресу `http://localhost:8081`

📖 **Полная инструкция с МойСклад:** [COMPLETE_SETUP.md](COMPLETE_SETUP.md)  
🚀 **Деплой в продакшн:** [DEPLOY.md](DEPLOY.md)  
🔒 **Безопасность:** [SECURITY.md](SECURITY.md)

### Вариант 2: Локальная установка

1. Установите зависимости:
```bash
go mod download
```

2. Создайте файл `.env` на основе `.env.example` и заполните необходимые переменные

3. Создайте базу данных PostgreSQL:
```sql
CREATE DATABASE telegins_shop;
```

4. Запустите миграции:
```bash
psql -U postgres -d telegins_shop -f migrations/001_init.sql
```

5. Создайте первого администратора:
```bash
go run cmd/create_admin/main.go admin admin@example.com your_password
```

6. Запустите приложение:
```bash
go run cmd/main.go
```

Приложение будет доступно по адресу `http://localhost:8080`

## Структура проекта

```
telegins_shop/
├── cmd/              # Точка входа приложения
├── internal/
│   ├── config/       # Конфигурация
│   ├── db/           # Работа с БД
│   ├── models/       # Модели данных
│   ├── handlers/     # HTTP handlers
│   ├── middleware/   # Middleware
│   ├── services/     # Бизнес-логика
│   ├── moysklad/     # Интеграция с МойСклад
│   └── telegram/     # Telegram-бот
├── web/              # Frontend (HTML, CSS, JS)
├── migrations/       # SQL миграции
└── go.mod
```

## API Endpoints

### Публичные
- `GET /` - Главная страница
- `GET /catalog` - Каталог товаров
- `GET /api/products` - Список товаров
- `GET /api/products/:id` - Детали товара
- `GET /api/promotions` - Активные акции
- `POST /api/cart/add` - Добавить в корзину
- `POST /api/orders` - Создать заявку

### Админ
- `POST /api/admin/login` - Вход в админ-панель
- `GET /admin` - Админ-панель
- `POST /api/admin/promotions` - Создать акцию
- `PUT /api/admin/promotions/:id` - Обновить акцию
- `DELETE /api/admin/promotions/:id` - Удалить акцию
- `POST /api/admin/products/sync` - Синхронизация с МойСклад

