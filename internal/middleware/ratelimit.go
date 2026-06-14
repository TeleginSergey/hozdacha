package middleware

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/TeleginSergey/hozdacha/internal/cache"
)

// rateStore — общее хранилище счётчиков (Redis при наличии, иначе in-memory).
// По умолчанию in-memory, чтобы middleware работал и без вызова SetRateStore.
var rateStore = cache.NewRateStore(nil, nil)

// SetRateStore подключает общее хранилище лимитов (вызывается из app при старте,
// чтобы лимиты считались в Redis и были корректны при нескольких репликах).
func SetRateStore(s *cache.RateStore) {
	if s != nil {
		rateStore = s
	}
}

func limit(c *gin.Context, name string, n int, window time.Duration) bool {
	return rateStore.Allow(c.Request.Context(), "rl:"+name+":"+c.ClientIP(), n, window)
}

func tooMany(c *gin.Context) {
	c.JSON(http.StatusTooManyRequests, gin.H{
		"error": "Too many requests. Please try again later.",
	})
	c.Abort()
}

// RateLimit — общий лимит запросов (увеличен для пагинации товаров).
func RateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !limit(c, "global", 300, time.Minute) {
			tooMany(c)
			return
		}
		c.Next()
	}
}

// ProductRateLimit — более высокий лимит для API товаров (пагинация).
func ProductRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !limit(c, "product", 600, time.Minute) {
			tooMany(c)
			return
		}
		c.Next()
	}
}

// AuthRateLimit — жёсткий лимит для аутентификационных эндпоинтов
// (login / verify / reset / register): гасит перебор кодов и спам.
func AuthRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !limit(c, "auth", 30, time.Minute) {
			tooMany(c)
			return
		}
		c.Next()
	}
}

// StrictRateLimit для админ-панели.
func StrictRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !limit(c, "admin", 300, time.Minute) {
			tooMany(c)
			return
		}
		c.Next()
	}
}
