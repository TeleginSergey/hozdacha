package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type rateLimiter struct {
	visitors map[string]*visitor
	mu       sync.RWMutex
	rate     time.Duration
	limit    int
}

type visitor struct {
	lastSeen time.Time
	count    int
}

var limiter *rateLimiter

func init() {
	limiter = &rateLimiter{
		visitors: make(map[string]*visitor),
		rate:     time.Minute,
		limit:    300, // 300 запросов в минуту (увеличено для пагинации товаров)
	}

	// Очистка старых записей каждые 5 минут
	go func() {
		for {
			time.Sleep(5 * time.Minute)
			limiter.cleanup()
		}
	}()
}

func (rl *rateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for ip, v := range rl.visitors {
		if now.Sub(v.lastSeen) > 10*time.Minute {
			delete(rl.visitors, ip)
		}
	}
}

func (rl *rateLimiter) getVisitor(ip string) *visitor {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, exists := rl.visitors[ip]
	if !exists {
		v = &visitor{
			lastSeen: time.Now(),
			count:    0,
		}
		rl.visitors[ip] = v
	}
	return v
}

func (rl *rateLimiter) allow(ip string) bool {
	v := rl.getVisitor(ip)

	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	if now.Sub(v.lastSeen) > rl.rate {
		v.count = 0
		v.lastSeen = now
	}

	if v.count >= rl.limit {
		return false
	}

	v.count++
	v.lastSeen = now
	return true
}

// RateLimit middleware для ограничения количества запросов
func RateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()

		if !limiter.allow(ip) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "Too many requests. Please try again later.",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// ProductRateLimit middleware для API товаров (более высокий лимит для пагинации)
func ProductRateLimit() gin.HandlerFunc {
	productLimiter := &rateLimiter{
		visitors: make(map[string]*visitor),
		rate:     time.Minute,
		limit:    600, // 600 запросов в минуту для API товаров
	}

	go func() {
		for {
			time.Sleep(5 * time.Minute)
			productLimiter.cleanup()
		}
	}()

	return func(c *gin.Context) {
		ip := c.ClientIP()

		if !productLimiter.allow(ip) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "Too many requests. Please try again later.",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// AuthRateLimit — жёсткий лимит для аутентификационных эндпоинтов
// (login / verify / reset / register): гасит перебор кодов и спам.
func AuthRateLimit() gin.HandlerFunc {
	authLimiter := &rateLimiter{
		visitors: make(map[string]*visitor),
		rate:     time.Minute,
		limit:    30, // 30 запросов в минуту на IP — с запасом для легитимного входа (и общих IP/CGNAT)
	}

	go func() {
		for {
			time.Sleep(5 * time.Minute)
			authLimiter.cleanup()
		}
	}()

	return func(c *gin.Context) {
		if !authLimiter.allow(c.ClientIP()) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "Слишком много запросов. Попробуйте позже.",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

// StrictRateLimit для админ-панели
func StrictRateLimit() gin.HandlerFunc {
	adminLimiter := &rateLimiter{
		visitors: make(map[string]*visitor),
		rate:     time.Minute,
		limit:    300, // 300 запросов в минуту для админки (≈5 в секунду — достаточно для нормальной работы)
	}

	return func(c *gin.Context) {
		ip := c.ClientIP()

		if !adminLimiter.allow(ip) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "Too many requests. Please try again later.",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
