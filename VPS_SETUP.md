# 🚀 VPS Установка и Деплой

## 📋 Требования к серверу

### Минимальные характеристики:
- **CPU**: 2 ядра (Intel Xeon E5/E3 или AMD EPYC)
- **RAM**: 2GB
- **SSD**: 60GB
- **OS**: Ubuntu 20.04/22.04 или Debian 11/12

### Рекомендуемые характеристики:
- **CPU**: 4 ядра для роста до DAU 500+
- **RAM**: 4GB для запаса производительности
- **SSD**: 80GB для роста данных

---

## 🔧 Установка зависимостей

### 1. Обновление системы
```bash
sudo apt update && sudo apt upgrade -y
```

### 2. Установка Docker и Docker Compose
```bash
# Установка Docker
curl -fsSL https://get.docker.com -o get-docker.sh
sudo sh get-docker.sh

# Добавление пользователя в группу docker
sudo usermod -aG docker $USER

# Установка Docker Compose
sudo curl -L "https://github.com/docker/compose/releases/download/v2.20.0/docker-compose-$(uname -s)-$(uname -m)" -o /usr/local/bin/docker-compose
sudo chmod +x /usr/local/bin/docker-compose

# Проверка установки
docker --version
docker-compose --version
```

### 3. Установка Nginx (для reverse proxy)
```bash
sudo apt install nginx -y
sudo systemctl start nginx
sudo systemctl enable nginx
```

### 4. Установка SSL (Let's Encrypt)
```bash
sudo apt install certbot python3-certbot-nginx -y
```

---

## 📦 Развертывание приложения

### 1. Клонирование репозитория
```bash
git clone https://github.com/TeleginSergey/hozdacha.git
cd hozdacha
```

### 2. Конфигурация переменных окружения
```bash
# Создание .env файла
cp .env.example .env
nano .env
```

**Обязательные переменные для VPS:**
```env
# База данных
DB_HOST=postgres
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=your_secure_password
DB_NAME=telegins_shop

# Redis
REDIS_HOST=redis
REDIS_PORT=6379

# Сервер
SERVER_HOST=0.0.0.0
SERVER_PORT=8080

# JWT
JWT_SECRET=your_jwt_secret_key_here

# МойСклад (если используется)
MOYSKLAD_TOKEN=your_moysklad_token
MOYSKLAD_WEBHOOK_SECRET=your_webhook_secret

# Домен
DOMAIN=your-domain.com
```

### 3. Запуск через Docker Compose
```bash
# Сборка и запуск
docker-compose up -d --build

# Проверка статуса
docker-compose ps

# Просмотр логов
docker-compose logs -f app
```

### 4. Создание администратора
```bash
docker-compose exec app sh -c "cd /root && ./create_admin admin admin@example.com your_secure_password"
```

---

## 🌐 Настройка Nginx Reverse Proxy

### 1. Создание конфигурации Nginx
```bash
sudo nano /etc/nginx/sites-available/hozdacha
```

**Содержимое файла:**
```nginx
server {
    listen 80;
    server_name your-domain.com www.your-domain.com;

    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # Для вебхуков МойСклад
    location /webhooks/ {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

### 2. Активация конфигурации
```bash
sudo ln -s /etc/nginx/sites-available/hozdacha /etc/nginx/sites-enabled/
sudo nginx -t
sudo systemctl reload nginx
```

### 3. Установка SSL сертификата
```bash
sudo certbot --nginx -d your-domain.com -d www.your-domain.com
```

---

## 🔒 Безопасность

### 1. Настройка Firewall
```bash
# Включение UFW
sudo ufw enable

# Разрешение необходимых портов
sudo ufw allow ssh
sudo ufw allow 'Nginx Full'

# Блокировка остальных
sudo ufw default deny incoming
sudo ufw default allow outgoing
```

### 2. Настройка SSH
```bash
sudo nano /etc/ssh/sshd_config
```

**Рекомендуемые настройки:**
```config
Port 22
PermitRootLogin no
PasswordAuthentication no
PubkeyAuthentication yes
```

```bash
sudo systemctl restart ssh
```

---

## 📊 Мониторинг

### 1. Установка утилит мониторинга
```bash
# htop для мониторинга процессов
sudo apt install htop -y

# iotop для мониторинга диска
sudo apt install iotop -y

# ncdu для анализа дискового пространства
sudo apt install ncdu -y
```

### 2. Скрипт мониторинга
```bash
nano monitor.sh
```

**Содержимое скрипта:**
```bash
#!/bin/bash

echo "=== System Monitor ==="
echo "CPU Usage:"
top -bn1 | grep "Cpu(s)" | awk '{print $2}' | awk -F'%' '{print $1}'

echo "Memory Usage:"
free -m | grep "Mem:" | awk '{printf "%.2f%%\n", $3/$2 * 100.0}'

echo "Disk Usage:"
df -h / | awk '{print $5}' | tail -1

echo "Docker Status:"
docker-compose ps

echo "=== End Monitor ==="
```

```bash
chmod +x monitor.sh
./monitor.sh
```

---

## 🔄 Обновление и обслуживание

### 1. Обновление приложения
```bash
git pull origin main
docker-compose down
docker-compose up -d --build
```

### 2. Резервное копирование БД
```bash
# Создание бэкапа
docker-compose exec postgres pg_dump -U postgres telegins_shop > backup_$(date +%Y%m%d_%H%M%S).sql

# Восстановление из бэкапа
docker-compose exec -T postgres psql -U postgres telegins_shop < backup_file.sql
```

### 3. Очистка Docker
```bash
# Очистка неиспользуемых контейнеров и образов
docker system prune -a
```

---

## 🚨 Траблшутинг

### Проблема: Приложение не запускается
```bash
# Проверка логов
docker-compose logs app

# Проверка статуса контейнеров
docker-compose ps

# Перезапуск
docker-compose restart app
```

### Проблема: Нет доступа к БД
```bash
# Проверка статуса PostgreSQL
docker-compose exec postgres pg_isready

# Проверка подключения
docker-compose exec postgres psql -U postgres -d telegins_shop -c "SELECT version();"
```

### Проблема: Nginx не работает
```bash
# Проверка конфигурации
sudo nginx -t

# Проверка логов
sudo tail -f /var/log/nginx/error.log

# Перезапуск
sudo systemctl restart nginx
```

---

## 📞 Поддержка

При возникновении проблем:

1. Проверьте логи контейнеров: `docker-compose logs -f`
2. Проверьте системные ресурсы: `htop`, `df -h`, `free -m`
3. Проверьте сетевые настройки: `netstat -tlnp`
4. Перезапустите сервисы: `docker-compose restart`

---

## 📈 Производительность

Для DAU 200-300 на сервере 2CPU/2GB/60GB:

- **CPU**: ~30% нагрузка
- **RAM**: ~1.45GB использование  
- **SSD**: ~40GB занято
- **Производительность**: 50-500ms время ответа

Мониторинг нагрузки рекомендуется проводить через `./monitor.sh` или Grafana/Prometheus для продвинутой аналитики.
