package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func AuthMiddleware(jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			// Не прерываем выполнение, просто не устанавливаем user_id
			c.Next()
			return
		}

		// Проверяем формат Bearer token
		if !strings.HasPrefix(authHeader, "Bearer ") {
			// Не прерываем выполнение, просто не устанавливаем user_id
			c.Next()
			return
		}
		tokenString := authHeader[7:] // Пропускаем "Bearer "

		// Валидируем токен
		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return []byte(jwtSecret), nil
		})

		if err != nil || !token.Valid {
			// Не прерываем выполнение, просто не устанавливаем user_id
			c.Next()
			return
		}

		// Извлекаем claims
		if claims, ok := token.Claims.(jwt.MapClaims); ok {
			userID, ok := claims["user_id"]
			if !ok {
				// Не прерываем выполнение, просто не устанавливаем user_id
				c.Next()
				return
			}

			// Проверяем время истечения токена
			if exp, ok := claims["exp"]; ok {
				if expFloat, ok := exp.(float64); ok {
					if time.Now().Unix() > int64(expFloat) {
						// Не прерываем выполнение, просто не устанавливаем user_id
						c.Next()
						return
					}
				}
			}

			// Устанавливаем user_id в контекст
			c.Set("user_id", int64(userID.(float64)))
			c.Set("username", claims["username"])
			c.Set("email", claims["email"])
		}
		c.Next()
	}
}

func GenerateToken(userID int64, username, email, secret string) (string, error) {
	claims := jwt.MapClaims{
		"user_id":  userID,
		"username": username,
		"email":    email,
		"exp":      time.Now().Add(time.Hour * 24).Unix(), // Токен на 24 часа
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		_, exists := c.Get("user_id")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization required"})
			c.Abort()
			return
		}
		c.Next()
	}
}
