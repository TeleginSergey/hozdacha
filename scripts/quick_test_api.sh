#!/bin/bash
# Быстрый тест API МойСклад

cd "$(dirname "$0")/.."

if [ ! -f .env ]; then
    echo "❌ .env не найден!"
    exit 1
fi

TOKEN=$(grep "^MOYSKLAD_TOKEN=" .env | cut -d= -f2)

if [ -z "$TOKEN" ]; then
    echo "❌ MOYSKLAD_TOKEN не найден!"
    exit 1
fi

echo "🔍 Тестирование API МойСклад"
echo "Токен: ${TOKEN:0:20}..."
echo ""

# 1. Проверка подключения
echo "1️⃣  Проверка подключения:"
curl -s -H "Authorization: Bearer $TOKEN" \
     -H "Accept: application/json" \
     "https://api.moysklad.ru/api/remap/1.2/context" | python3 -m json.tool 2>/dev/null | head -10 || echo "Ошибка подключения"
echo ""

# 2. Один товар
echo "2️⃣  Структура товара:"
curl -s -H "Authorization: Bearer $TOKEN" \
     -H "Accept: application/json" \
     "https://api.moysklad.ru/api/remap/1.2/entity/product?limit=1" | python3 -m json.tool 2>/dev/null | head -80 || curl -s -H "Authorization: Bearer $TOKEN" -H "Accept: application/json" "https://api.moysklad.ru/api/remap/1.2/entity/product?limit=1" | head -80
echo ""

# 3. Товар с остатками
echo "3️⃣  Товар с expand=stock:"
curl -s -H "Authorization: Bearer $TOKEN" \
     -H "Accept: application/json" \
     "https://api.moysklad.ru/api/remap/1.2/entity/product?limit=1&expand=stock" | python3 -m json.tool 2>/dev/null | grep -A 20 "stock" | head -30 || echo "Остатки не найдены"
echo ""

