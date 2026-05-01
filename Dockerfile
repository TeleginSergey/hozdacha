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

# Healthcheck для Dokploy
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

EXPOSE 8080

CMD ["./main"]