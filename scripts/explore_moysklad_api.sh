#!/bin/bash

# Исследование API МойСклад - какие данные доступны
# Использование: ./scripts/explore_moysklad_api.sh

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

echo "🔍 ИССЛЕДОВАНИЕ API МОЙСКЛАД"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# Получаем один товар и смотрим структуру
echo "📦 Структура товара (полный ответ):"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
curl -s \
    -H "Authorization: Bearer $TOKEN" \
    -H "Accept: application/json" \
    "$BASE_URL/entity/product?limit=1" | python3 -m json.tool 2>/dev/null | head -150

echo ""
echo ""
echo "📦 Товар с expand=stock (остатки):"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
curl -s \
    -H "Authorization: Bearer $TOKEN" \
    -H "Accept: application/json" \
    "$BASE_URL/entity/product?limit=1&expand=stock" | python3 -m json.tool 2>/dev/null | head -200

echo ""
echo ""
echo "📊 Отчет по остаткам (структура):"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
curl -s \
    -H "Authorization: Bearer $TOKEN" \
    -H "Accept: application/json" \
    "$BASE_URL/report/stock/bystore?limit=2" | python3 -m json.tool 2>/dev/null | head -100

echo ""
echo ""
echo "📋 Метаданные товара (доступные поля):"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
curl -s \
    -H "Authorization: Bearer $TOKEN" \
    -H "Accept: application/json" \
    "$BASE_URL/entity/product/metadata" | python3 -m json.tool 2>/dev/null | grep -E '"name"|"type"' | head -30

