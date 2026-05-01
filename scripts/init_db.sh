#!/bin/bash

# Скрипт для инициализации БД после запуска контейнера

echo "Waiting for PostgreSQL to be ready..."
until PGPASSWORD=$DB_PASSWORD psql -h postgres -U $DB_USER -d $DB_NAME -c '\q' 2>/dev/null; do
  echo "PostgreSQL is unavailable - sleeping"
  sleep 1
done

echo "PostgreSQL is up - applying migrations..."

# Применяем миграции
PGPASSWORD=$DB_PASSWORD psql -h postgres -U $DB_USER -d $DB_NAME -f /migrations/001_init.sql

echo "Database initialized!"



