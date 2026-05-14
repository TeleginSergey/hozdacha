# Исправления: Синхронизация и Бронирование Товаров

## Дата: 14 мая 2026
## Commit: Fix Moysklad sync and stock reservation

---

## 1. Проблемы, которые были исправлены

### 1.1 Redis-резервирование остатков (Stock Cache)
**Проблема:**
- При cache miss (товар не в Redis) функция `GetAvailableStock` возвращала `-2`, что приводило к ошибкам в корзине
- `ReleaseStock` мог оставить отрицательные значения в Redis

**Решение:**
- `GetAvailableStock` теперь при cache miss автоматически загружает остаток из БД и кэширует его
- `ReleaseStock` использует Lua-скрипт для атомарного уменьшения и удаления ключа при нулевом остатке
- Добавлена функция `ConsumeReservedStock` для окончательного списания товара при создании заказа

### 1.2 Логика добавления товара в корзину
**Проблема:**
- Если товар уже в корзине, при добавлении ещё одного количества резерв не увеличивался
- Проверка остатка была неправильной

**Решение:**
- При добавлении существующего товара теперь явно резервируется дополнительное количество
- Откат резерва при ошибке обновления БД

### 1.3 Погашение резерва после создания заказа
**Проблема:**
- После создания заказа корзина просто очищалась, но резерв в Redis не погашался
- Товар снова становился доступным до вебхука/синка с МойСклад

**Решение:**
- Добавлена функция `CommitCartReservation()` которая:
  1. Уменьшает остаток товара в БД на количество заказанного
  2. Обновляет статус товара (active/out_of_stock)
  3. Освобождает резерв в Redis
  4. Обновляет кэш остатков
- Функция вызывается ДО очистки корзины

### 1.4 Синхронизация МойСклад через Worker Pool
**Проблема:**
- `SyncSingleProduct` брал остаток из `product.Stock`, но при полной синхронизации остатки лежали в отдельном `stockMap`
- Это приводило к массовому синку нулевых остатков
- Нет защиты от перекрывающихся full/delta sync

**Решение:**
- Добавлено поле `StockByID` в `SyncTask` для передачи остатков в worker pool
- Worker pool перед синком товара подставляет остаток из `StockByID`
- Добавлена функция `GetStockMapForSync()` для отдельного получения остатков
- Добавлена защита `syncRunning` (atomic flag) в `Scheduler` для исключения параллельных синков

---

## 2. Архитектурные изменения

### 2.1 Файлы, которые были изменены

```
internal/cache/redis.go              (+37, -5)    - Улучшена логика резервирования
internal/usecase/cart.go             (+74, -6)    - Добавлены CommitCartReservation и ClearCartAfterOrder
internal/usecase/order.go            (+10, -24)   - Упрощена проверка остатков (используется DB, не cache)
internal/handlers/orders.go          (+9, -1)     - Вызов CommitCartReservation перед очисткой
internal/services/sync_worker.go     (+27, -4)    - Добавлено StockByID в SyncTask
internal/services/moysklad_sync.go   (+13, 0)     - Добавлена GetStockMapForSync()
internal/services/scheduler.go       (+18, 0)     - Добавлена защита от параллельных синков
```

### 2.2 Поток работы бронирования (Reservation Flow)

```
1. Пользователь добавляет товар в корзину
   ├─ Проверка доступного остатка (Redis cache + DB fallback)
   ├─ Резервирование товара в Redis (reserved:productID += quantity)
   └─ Сохранение в БД (cart_items)

2. Пользователь создаёт заказ
   ├─ Проверка остатков товаров в БД
   ├─ Создание заказа в БД (orders + order_items)
   ├─ CommitCartReservation():
   │  ├─ Уменьшение stock в БД (products.stock -= quantity)
   │  ├─ Обновление статуса товара
   │  ├─ Освобождение резерва в Redis (reserved:productID -= quantity)
   │  └─ Обновление кэша остатков
   └─ ClearCartAfterOrder(): Удаление товаров из корзины

3. Синхронизация с МойСклад
   ├─ Получение остатков отдельным запросом (GetStockReport)
   ├─ Получение товаров (GetProducts)
   ├─ Разбиение на батчи и отправка в worker pool со StockByID
   └─ Worker pool синкирует товары с правильными остатками
```

---

## 3. Оптимизация для сервера 4 ядра / 4 ГБ RAM

### 3.1 Текущие настройки (в config)

```yaml
moysklad:
  sync_workers: 3          # Для 4 ядер: 3-4 воркера
  sync_interval: 5m        # Дельта-синк каждые 5 минут
  reseed_full_interval: 24h # Полный синк раз в сутки
  stock_buffer: 5          # 5% буфер для резервирования
  max_concurrent_requests: 8
```

### 3.2 Рекомендации для Dokploy

**Переменные окружения:**
```bash
# Уменьшить параллелизм для экономии памяти
MOYSKLAD_SYNC_WORKERS=2
MOYSKLAD_SYNC_INTERVAL=10m
MOYSKLAD_MAX_CONCURRENT_REQUESTS=4

# Redis
REDIS_HOST=redis
REDIS_PORT=6379
REDIS_DB=0

# Postgres
DB_MAX_OPEN_CONNS=10
DB_MAX_IDLE_CONNS=5
```

**Docker Compose (если используется):**
```yaml
services:
  app:
    mem_limit: 1.5g
    cpus: 2
    environment:
      MOYSKLAD_SYNC_WORKERS: 2
      
  redis:
    mem_limit: 512m
    
  postgres:
    mem_limit: 1.5g
```

---

## 4. Тестирование на Dokploy

### 4.1 Проверить логи синхронизации
```bash
docker logs <container_name> | grep "Worker pool stats"
```

Должны видеть:
- `processed_tasks` > 0 (товары синкируются)
- `failed_tasks` должны быть минимальны
- `success_rate` > 95%

### 4.2 Проверить бронирование
1. Добавить товар в корзину
2. Проверить Redis: `redis-cli GET reserved:productID` (должно быть > 0)
3. Создать заказ
4. Проверить Redis: `redis-cli GET reserved:productID` (должно быть 0)
5. Проверить БД: `SELECT stock FROM products WHERE id = productID` (должен уменьшиться)

### 4.3 Проверить синхронизацию
```bash
# Запустить полный синк вручную
curl -X POST http://localhost:8080/api/admin/products/sync/full \
  -H "Authorization: Bearer <token>"

# Смотреть логи
docker logs <container_name> | grep "full product sync"
```

---

## 5. Известные ограничения и TODO

### 5.1 Текущее состояние
- ✅ Redis используется для резервирования (не Postgres очередь)
- ✅ Синхронизация через worker pool с правильными остатками
- ✅ Защита от параллельных синков
- ✅ Корректное погашение резерва после заказа

### 5.2 Возможные улучшения (на будущее)
- [ ] Полная Redis job queue вместо in-memory channels (для масштабирования)
- [ ] Метрики Prometheus для мониторинга синка
- [ ] Retry-логика для неудачных синков товаров
- [ ] Graceful shutdown для worker pool
- [ ] Unit-тесты для cart/order usecase

---

## 6. Миграция на Dokploy

1. **Убедиться, что Redis доступен** (переменная `REDIS_HOST`)
2. **Убедиться, что Postgres доступен** (переменная `DB_*`)
3. **Пересобрать образ:**
   ```bash
   docker build -t hozdacha:latest .
   ```
4. **Перезагрузить контейнер в Dokploy** (обновить image tag)
5. **Проверить логи** первых 5 минут после старта

---

## 7. Контрольный список

- [x] Компиляция проходит без ошибок (`go test ./...`)
- [x] Коммит залит на GitHub
- [x] Изменения протестированы локально (без сервисов)
- [ ] Протестировано на Dokploy (требует запуска)
- [ ] Проверены логи синхронизации
- [ ] Проверено бронирование (добавление → заказ → проверка БД/Redis)

---

**Автор:** Cascade AI  
**Статус:** Готово к деплою на Dokploy
