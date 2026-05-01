#!/bin/bash

# Скрипт для тестирования API МойСклад
# Использование: ./scripts/test_moysklad_api.sh

# Загружаем токен из .env
if [ -f .env ]; then
    export $(grep -v '^#' .env | grep MOYSKLAD_TOKEN | xargs)
    TOKEN="${MOYSKLAD_TOKEN}"
else
    echo "❌ Файл .env не найден!"
    exit 1
fi

if [ -z "$TOKEN" ]; then
    echo "❌ MOYSKLAD_TOKEN не найден в .env!"
    exit 1
fi

BASE_URL="https://api.moysklad.ru/api/remap/1.2"

echo "🔍 Тестирование API МойСклад"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# Функция для выполнения запроса
make_request() {
    local endpoint=$1
    local description=$2
    echo "📋 $description"
    echo "   GET $endpoint"
    
    response=$(curl --compressed -s -w "\nHTTP_CODE:%{http_code}" \
        -H "Authorization: Bearer $TOKEN" \
        -H "Accept: application/json;charset=utf-8" \
        "$BASE_URL$endpoint")
    
    http_code=$(echo "$response" | grep "HTTP_CODE" | cut -d: -f2)
    body=$(echo "$response" | sed '/HTTP_CODE/d')
    
    if [ "$http_code" = "200" ]; then
        echo "   ✅ Успешно (HTTP $http_code)"
        # Показываем первые несколько строк ответа
        echo "$body" | head -20 | sed 's/^/   /'
        echo ""
    else
        echo "   ❌ Ошибка (HTTP $http_code)"
        echo "$body" | head -10 | sed 's/^/   /'
        echo ""
    fi
}

# 1. Получить информацию об аккаунте
make_request "/context" "Информация об аккаунте"

# 2. Получить товары (первые 5)
make_request "/entity/product?limit=5" "Товары (первые 5)"

# 3. Получить товары с остатками
make_request "/entity/product?limit=5&expand=stock" "Товары с остатками (expand=stock)"

# 4. Получить отчет по остаткам
make_request "/report/stock/bystore?limit=5" "Отчет по остаткам"

# 5. Получить складские остатки
make_request "/report/stock/all?limit=5" "Все складские остатки"

# 6. Получить товар по ID (первый товар)
echo "📋 Получение ID первого товара..."
first_product=$(curl -s \
    --compressed \
    -H "Authorization: Bearer $TOKEN" \
    -H "Accept: application/json;charset=utf-8" \
    "$BASE_URL/entity/product?limit=1" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)

if [ ! -z "$first_product" ]; then
    make_request "/entity/product/$first_product" "Товар по ID ($first_product)"
    make_request "/entity/product/$first_product?expand=stock" "Товар с остатками"
else
    echo "   ⚠️  Не удалось получить ID товара"
fi

# 7. Получить структуру товара (поля)
echo "📋 Структура товара (поля)"
echo "   GET /entity/product/metadata"
response=$(curl -s -w "\nHTTP_CODE:%{http_code}" \
    --compressed \
    -H "Authorization: Bearer $TOKEN" \
    -H "Accept: application/json;charset=utf-8" \
    "$BASE_URL/entity/product/metadata")
http_code=$(echo "$response" | grep "HTTP_CODE" | cut -d: -f2)
if [ "$http_code" = "200" ]; then
    echo "   ✅ Успешно (HTTP $http_code)"
    echo "$response" | sed '/HTTP_CODE/d' | jq '.attributes[] | {name, type}' 2>/dev/null | head -30 | sed 's/^/   /' || echo "$response" | head -20 | sed 's/^/   /'
else
    echo "   ❌ Ошибка (HTTP $http_code)"
fi
echo ""

# 8. Статистика
echo "📊 Статистика:"
total_products=$(curl -s \
    --compressed \
    -H "Authorization: Bearer $TOKEN" \
    -H "Accept: application/json;charset=utf-8" \
    "$BASE_URL/entity/product?limit=1" | grep -o '"size":[0-9]*' | cut -d: -f2)

if [ ! -z "$total_products" ]; then
    echo "   Всего товаров: $total_products"
fi

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "✅ Тестирование завершено!"
echo ""
echo "💡 Полезные endpoints:"
echo "   - /entity/product - товары"
echo "   - /entity/product/{id} - товар по ID"
echo "   - /report/stock/bystore - остатки по складам"
echo "   - /report/stock/all - все остатки"
echo "   - /entity/product/metadata - метаданные (поля товара)"

