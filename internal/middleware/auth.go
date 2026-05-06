package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// BlacklistChecker интерфейс для проверки чёрного списка токенов
type BlacklistChecker interface {
	IsBlacklisted(ctx context.Context, jti string) (bool, error)
	IsUserBlacklisted(ctx context.Context, userID int64, tokenIssuedAt int64) (bool, error)
}

var blacklistChecker BlacklistChecker

// SetBlacklistChecker устанавливает сервис для проверки чёрного списка
func SetBlacklistChecker(checker BlacklistChecker) {
	blacklistChecker = checker
}

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

			// Проверяем чёрный список по JTI (если есть)
			if jti, ok := claims["jti"].(string); ok && blacklistChecker != nil {
				isBlacklisted, err := blacklistChecker.IsBlacklisted(c.Request.Context(), jti)
				if err != nil {
					// Ошибка Redis не должна блокировать доступ
					c.Next()
					return
				}
				if isBlacklisted {
					// Токен отозван - логируем и продолжаем
					c.Next()
					return
				}
			}

			// Проверяем, не были ли отозваны все токены пользователя
			if iat, ok := claims["iat"].(float64); ok && blacklistChecker != nil {
				isUserBlacklisted, err := blacklistChecker.IsUserBlacklisted(c.Request.Context(), int64(userID.(float64)), int64(iat))
				if err != nil {
					c.Next()
					return
				}
				if isUserBlacklisted {
					// Все токены пользователя отозваны
					c.Next()
					return
				}
			}

			// Устанавливаем user_id и другие данные в контекст
			c.Set("user_id", int64(userID.(float64)))
			c.Set("username", claims["username"])
			c.Set("email", claims["email"])
			c.Set("role", claims["role"])
			c.Set("jti", claims["jti"]) // JWT ID для возможного отзыва
			c.Set("iat", claims["iat"]) // Время выпуска токена
			c.Set("exp", claims["exp"]) // Время истечения токена
		}
		c.Next()
	}
}

// generateJTI создаёт уникальный идентификатор для JWT токена
func generateJTI() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func GenerateToken(userID int64, username, email, secret string) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"user_id":  userID,
		"username": username,
		"email":    email,
		"jti":      generateJTI(),                  // Уникальный ID токена для отзыва
		"iat":      now.Unix(),                     // Время выпуска
		"exp":      now.Add(time.Hour * 24).Unix(), // Токен на 24 часа
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// GenerateTokenWithRole генерирует JWT с указанием роли пользователя
func GenerateTokenWithRole(userID int64, username, email, role, secret string) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"user_id":  userID,
		"username": username,
		"email":    email,
		"role":     role,                           // Роль пользователя (admin/user)
		"jti":      generateJTI(),                  // Уникальный ID токена для отзыва
		"iat":      now.Unix(),                     // Время выпуска
		"exp":      now.Add(time.Hour * 24).Unix(), // Токен на 24 часа
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

// RequireAdmin проверяет, что пользователь имеет роль администратора
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, exists := c.Get("role")
		if !exists {
			c.JSON(http.StatusForbidden, gin.H{"error": "Access denied - role not found"})
			c.Abort()
			return
		}

		roleStr, ok := role.(string)
		if !ok || roleStr != "admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "Access denied - admin only"})
			c.Abort()
			return
		}

		c.Next()
	}
}
