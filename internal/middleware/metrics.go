package middleware

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/TeleginSergey/hozdacha/internal/metrics"
)

// Metrics — Gin-middleware, фиксирующий RED-метрики по каждому запросу.
//
// В качестве метки path используется шаблон маршрута (c.FullPath(), например
// "/api/products/:id"), а не реальный URL — иначе id/телефоны в пути взорвали бы
// кардинальность Prometheus. Сам /metrics не измеряем, чтобы scrape не накручивал
// собственную статистику.
func Metrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.FullPath()
		if path == "" {
			path = "unmatched" // 404 и прочие незаматченные маршруты
		}
		if path == "/metrics" {
			c.Next()
			return
		}

		metrics.IncInFlight()
		start := time.Now()

		c.Next()

		metrics.DecInFlight()
		metrics.ObserveHTTP(
			c.Request.Method,
			path,
			strconv.Itoa(c.Writer.Status()),
			time.Since(start).Seconds(),
		)
	}
}
