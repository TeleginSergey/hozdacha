# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Копируем go mod файлы
COPY go.mod go.sum* ./
RUN go mod download

# Копируем исходный код
COPY . .

# Собираем приложение
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main ./cmd/main.go

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata wget
WORKDIR /root/

# Копируем бинарники из builder
COPY --from=builder /app/main .
COPY --from=builder /app/web ./web
COPY --from=builder /app/migrations ./migrations

# Создаем директорию для логов
RUN mkdir -p /var/log/telegins_shop

# Optimized healthcheck для Dokploy (работает под высокой нагрузкой)
HEALTHCHECK --interval=60s --timeout=10s --start-period=30s --retries=3 \
  CMD wget --no-verbose --tries=1 --timeout=5 --spider http://localhost:8080/health || exit 1

EXPOSE 8080

CMD ["./main"]