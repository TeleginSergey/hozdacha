package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/usecase"
)

type UserHandler struct {
	userUC *usecase.UserUsecase
	logger *zap.Logger
}

func NewUserHandler(userUC *usecase.UserUsecase, logger *zap.Logger) *UserHandler {
	return &UserHandler{
		userUC: userUC,
		logger: logger,
	}
}

// Промежуточное хранилище для временных данных регистрации
var registrationAttempts = make(map[string]int)
var lastRegistrationTime = make(map[string]time.Time)

func (h *UserHandler) Register(c *gin.Context) {
	var req usecase.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}

	// Защита от спама - проверяем IP
	clientIP := c.ClientIP()

	// Rate limiting: максимум 3 попытки в час с одного IP
	const maxAttemptsPerHour = 3
	const blockDuration = 1 * time.Hour
	const minRegistrationInterval = 1 * time.Minute

	// Ограничение по времени (не чаще 1 раза в минуту)
	if lastTime, exists := lastRegistrationTime[clientIP]; exists {
		if time.Since(lastTime) < minRegistrationInterval {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "Please wait before trying again"})
			return
		}

		if time.Since(lastTime) < blockDuration {
			attempts := registrationAttempts[clientIP] + 1
			registrationAttempts[clientIP] = attempts

			if attempts > maxAttemptsPerHour {
				h.logger.Warn("Too many registration attempts",
					zap.String("ip", clientIP),
					zap.Int("attempts", attempts))
				c.JSON(http.StatusTooManyRequests, gin.H{
					"error":       "Too many registration attempts. Please try again later.",
					"retry_after": int(time.Until(lastTime.Add(time.Hour)).Seconds()),
				})
				return
			}

			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":       "Please wait before registering again",
				"retry_after": int(time.Until(lastTime.Add(time.Hour)).Seconds()),
			})
			return
		}
	}

	// Валидация
	if len(req.Email) < 3 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Email must be at least 3 characters"})
		return
	}
	if !strings.Contains(req.Email, "@") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid email format"})
		return
	}
	if len(req.Password) < 6 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Password must be at least 6 characters"})
		return
	}

	// Проверяем на простые пароли
	if isWeakPassword(req.Password) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Password is too weak. Use a combination of letters, numbers and symbols."})
		return
	}

	// Регистрируем пользователя
	user, err := h.userUC.Register(c.Request.Context(), req)
	if err != nil {
		h.logger.Warn("Registration failed",
			zap.String("email", req.Email),
			zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Обновляем время регистрации
	lastRegistrationTime[clientIP] = time.Now()
	registrationAttempts[clientIP] = 0

	h.logger.Info("User registered successfully",
		zap.Int64("user_id", user.ID),
		zap.String("username", user.Username),
		zap.String("email", user.Email),
		zap.String("ip", clientIP))

	c.JSON(http.StatusCreated, gin.H{
		"message": "Registration successful - you can now login",
		"user": gin.H{
			"id":       user.ID,
			"username": user.Username,
			"email":    user.Email,
			"active":   true, // Пользователь сразу активен
		},
	})
}

func (h *UserHandler) Login(c *gin.Context) {
	var req usecase.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}

	// Защита от брутфорса - проверяем IP
	clientIP := c.ClientIP()
	if lastTime, exists := lastRegistrationTime[clientIP]; exists {
		if time.Since(lastTime) < 5*time.Second {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "Too many login attempts. Please wait."})
			return
		}
	}

	authResp, err := h.userUC.Login(c.Request.Context(), req)
	if err != nil {
		h.logger.Warn("Login failed",
			zap.String("username", req.Username),
			zap.String("ip", clientIP),
			zap.Error(err))

		lastRegistrationTime[clientIP] = time.Now()
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid username or password"})
		return
	}

	// Обновляем время последней попытки
	lastRegistrationTime[clientIP] = time.Now()

	h.logger.Info("User logged in successfully",
		zap.Int64("user_id", authResp.User.ID),
		zap.String("username", authResp.User.Username),
		zap.String("ip", clientIP))

	c.JSON(http.StatusOK, gin.H{
		"message": "Login successful",
		"token":   authResp.Token,
		"user":    authResp.User,
	})
}

func (h *UserHandler) GetProfile(c *gin.Context) {
	// Проверяем авторизацию
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	user, err := h.userUC.GetProfile(c.Request.Context(), userID.(int64))
	if err != nil {
		h.logger.Error("Failed to get user profile", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get profile"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user": user,
	})
}

// Email verification removed - users are now activated immediately

func (h *UserHandler) VerifyToken(c *gin.Context) {
	// Проверяем авторизацию
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"valid":   true,
		"user_id": userID,
	})
}

func (h *UserHandler) UpdateProfile(c *gin.Context) {
	// Проверяем авторизацию
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req map[string]interface{}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}

	user, err := h.userUC.UpdateProfile(c.Request.Context(), userID.(int64), req)
	if err != nil {
		h.logger.Error("Failed to update profile", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update profile"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Profile updated successfully",
		"user":    user,
	})
}

func (h *UserHandler) GenerateTOTP(c *gin.Context) {
	// Проверяем авторизацию
	_, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "2FA setup not implemented yet",
	})
}

func (h *UserHandler) VerifyTOTP(c *gin.Context) {
	var req struct {
		Token string `json:"token"`
		Code  string `json:"code"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}

	// Проверяем авторизацию
	_, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	// Для простоты примера - всегда успех
	c.JSON(http.StatusOK, gin.H{
		"message": "2FA verification successful",
	})
}

// Проверка на слабые пароли
func isWeakPassword(password string) bool {
	weakPasswords := []string{
		"password", "123456", "123456789", "qwerty", "abc123",
		"password123", "admin", "letmein", "welcome", "monkey",
	}

	passwordLower := strings.ToLower(password)
	for _, weak := range weakPasswords {
		if passwordLower == weak {
			return true
		}
	}

	// Проверяем на простые паттерны
	if strings.Contains(passwordLower, "123") ||
		strings.Contains(passwordLower, "qwerty") ||
		strings.Contains(passwordLower, "abc") {
		return true
	}

	return false
}
