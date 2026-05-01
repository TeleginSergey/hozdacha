#!/bin/bash

# Скрипт для создания администратора через Docker

if [ "$#" -ne 3 ]; then
    echo "Usage: ./scripts/create_admin.sh <username> <email> <password>"
    exit 1
fi

USERNAME=$1
EMAIL=$2
PASSWORD=$3

docker compose exec app sh -c "cd /root && ./create_admin $USERNAME $EMAIL $PASSWORD"


