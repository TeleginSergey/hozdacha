#!/bin/sh
set -e
cd /root

# Отдельный режим: docker compose run ... migrate-up
# или в Dokploy «Custom Command»: migrate-up (без основного процесса — контейнер завершится после миграций, это нормально для one-shot).
if [ "$1" = "migrate-up" ]; then
	exec ./main migrate-up
fi
if [ "$1" = "./main" ] && [ "$2" = "migrate-up" ]; then
	exec ./main migrate-up
fi

# Опционально: миграции при каждом старте приложения (Dokploy: задать AUTO_MIGRATE=true)
if [ "${AUTO_MIGRATE:-}" = "true" ] || [ "${AUTO_MIGRATE:-}" = "1" ]; then
	./main migrate-up
fi

exec "$@"
