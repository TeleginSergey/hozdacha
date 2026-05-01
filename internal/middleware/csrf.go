package middleware

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"

	"github.com/gin-gonic/gin"
)

const csrfTokenHeader = "X-CSRF-Token"
const csrfTokenCookie = "csrf_token"

// GenerateCSRFToken генерирует случайный CSRF токен
func GenerateCSRFToken() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// CSRFProtection middleware для защиты от CSRF атак
// Для API с JWT токенами CSRF не критичен, но можно использовать для форм
func CSRFProtection() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Для GET запросов просто устанавливаем токен
		if c.Request.Method == "GET" {
			token, err := GenerateCSRFToken()
			if err == nil {
				c.SetCookie(csrfTokenCookie, token, 3600, "/", "", false, true) // HttpOnly, Secure в продакшн
				c.Set("csrf_token", token)
			}
			c.Next()
			return
		}

		// Для POST/PUT/DELETE проверяем токен
		if c.Request.Method == "POST" || c.Request.Method == "PUT" || c.Request.Method == "DELETE" {
			// Пропускаем API запросы с JWT токенами (они защищены другим способом)
			if c.GetHeader("Authorization") != "" {
				c.Next()
				return
			}

			// Для форм проверяем CSRF токен
			cookieToken, _ := c.Cookie(csrfTokenCookie)
			headerToken := c.GetHeader(csrfTokenHeader)
			formToken := c.PostForm("csrf_token")

			if cookieToken == "" {
				c.JSON(http.StatusForbidden, gin.H{"error": "CSRF token missing"})
				c.Abort()
				return
			}

			if headerToken != cookieToken && formToken != cookieToken {
				c.JSON(http.StatusForbidden, gin.H{"error": "Invalid CSRF token"})
				c.Abort()
				return
			}
		}

		c.Next()
	}
}


