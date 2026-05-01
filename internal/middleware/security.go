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
	// Экранируем HTML теги
	sanitized := html.EscapeString(input)
	
	// Удаляем опасные паттерны
	dangerous := []string{
		"<script",
		"</script>",
		"javascript:",
		"onerror=",
		"onload=",
		"onclick=",
		"onmouseover=",
	}
	
	lower := strings.ToLower(sanitized)
	for _, pattern := range dangerous {
		if strings.Contains(lower, pattern) {
			sanitized = strings.ReplaceAll(sanitized, pattern, "")
		}
	}
	
	return sanitized
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

// CORS настройки для API
func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		
		// Разрешаем только определенные источники (можно настроить)
		allowedOrigins := []string{
			"http://localhost:8081",
			"http://localhost:8080",
			"http://127.0.0.1:8081",
			"http://127.0.0.1:8080",
		}
		
		allowed := false
		for _, allowedOrigin := range allowedOrigins {
			if origin == allowedOrigin {
				allowed = true
				break
			}
		}
		
		if allowed || origin == "" {
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


