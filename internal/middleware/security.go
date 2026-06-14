package middleware

import (
	"html"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

// SecurityHeaders добавляет безопасные HTTP заголовки
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Защита от XSS
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY") // Защита от clickjacking
		c.Header("X-XSS-Protection", "1; mode=block")

		// Content Security Policy
		c.Header("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline' 'unsafe-eval'; "+
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data: https:; "+
				"font-src 'self' data:; "+
				"connect-src 'self'; "+
				// Разрешаем встраивать виджет Яндекс.Карт (страница контактов).
				"frame-src 'self' https://yandex.ru https://*.yandex.ru; "+
				"frame-ancestors 'none';")

		// Strict Transport Security (для HTTPS)
		if c.Request.TLS != nil {
			c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		// Referrer Policy
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")

		// Permissions Policy
		c.Header("Permissions-Policy",
			"geolocation=(), microphone=(), camera=()")

		c.Next()
	}
}

// SanitizeInput санитизирует строковые входные данные от XSS
func SanitizeInput(input string) string {
	// Удаляем опасные паттерны ДО html-экранирования (case-insensitive)
	dangerous := []string{
		"<script",
		"</script>",
		"javascript:",
		"onerror=",
		"onload=",
		"onclick=",
		"onmouseover=",
	}

	lower := strings.ToLower(input)
	for _, pattern := range dangerous {
		for strings.Contains(lower, pattern) {
			idx := strings.Index(lower, pattern)
			input = input[:idx] + input[idx+len(pattern):]
			lower = strings.ToLower(input)
		}
	}

	// Экранируем оставшиеся HTML-спецсимволы
	return html.EscapeString(input)
}

// SanitizeString санитизирует строку, сохраняя базовые символы
func SanitizeString(s string, maxLength int) string {
	if maxLength > 0 && len(s) > maxLength {
		s = s[:maxLength]
	}
	return strings.TrimSpace(SanitizeInput(s))
}

// ValidatePhone проверяет формат телефона
func ValidatePhone(phone string) bool {
	// Разрешаем цифры, пробелы, +, -, (, )
	matched, _ := regexp.MatchString(`^[\d\s\+\-\(\)]{10,20}$`, phone)
	return matched
}

// ValidateEmail проверяет формат email
func ValidateEmail(email string) bool {
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	return emailRegex.MatchString(email)
}

// CORS настройки для API. allowedOrigins читается из CORS_ALLOWED_ORIGINS (через config).
func CORS(allowedOrigins []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")

		allowed := origin == ""
		for _, o := range allowedOrigins {
			if origin == o {
				allowed = true
				break
			}
		}

		if allowed {
			c.Header("Access-Control-Allow-Origin", origin)
		}

		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		c.Header("Access-Control-Max-Age", "3600")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
