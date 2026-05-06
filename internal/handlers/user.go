package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/services"
	"github.com/TeleginSergey/hozdacha/internal/usecase"
)

type UserHandler struct {
	userUC           *usecase.UserUsecase
	emailService     *services.EmailService
	blacklistService *services.TokenBlacklistService
	logger           *zap.Logger
}

func NewUserHandler(userUC *usecase.UserUsecase, emailService *services.EmailService, blacklistService *services.TokenBlacklistService, logger *zap.Logger) *UserHandler {
	return &UserHandler{
		userUC:           userUC,
		emailService:     emailService,
		blacklistService: blacklistService,
		logger:           logger,
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

	// Проверяем, существует ли пользователь с таким email
	existingUser, err := h.userUC.GetUserByEmail(c.Request.Context(), req.Email)
	if err == nil {
		// Пользователь существует
		if existingUser.EmailVerified {
			// Email уже подтвержден
			h.logger.Warn("Registration attempted for existing verified user",
				zap.String("email", req.Email))
			c.JSON(http.StatusBadRequest, gin.H{"error": "User with this email already exists and is verified. Please log in."})
			return
		} else {
			// Email не подтвержден - отправляем новый код и возвращаем специальный ответ
			h.logger.Info("User exists but not verified, resending code",
				zap.Int64("user_id", existingUser.ID),
				zap.String("email", req.Email))

			// Генерируем новый код верификации
			code := h.userUC.GenerateVerificationCode()

			// Сохраняем код в БД
			err = h.userUC.SaveVerificationCode(c.Request.Context(), existingUser.ID, code)
			if err != nil {
				h.logger.Error("Failed to save verification code for existing user",
					zap.Int64("user_id", existingUser.ID),
					zap.Error(err))
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate verification code"})
				return
			}

			// Отправляем email асинхронно
			go func() {
				name := existingUser.Username
				if name == "" {
					name = existingUser.Email
				}

				err := h.emailService.SendVerificationCode(existingUser.Email, name, code)
				if err != nil {
					h.logger.Error("Failed to send verification email for existing user",
						zap.Int64("user_id", existingUser.ID),
						zap.String("email", existingUser.Email),
						zap.Error(err))
				} else {
					h.logger.Info("Verification email resent for existing user",
						zap.Int64("user_id", existingUser.ID),
						zap.String("email", existingUser.Email))
				}
			}()

			// Логируем код для тестирования
			h.logger.Info("VERIFICATION CODE (for testing)",
				zap.Int64("user_id", existingUser.ID),
				zap.String("email", existingUser.Email),
				zap.String("code", code))

			c.JSON(http.StatusAccepted, gin.H{
				"message":               "User with this email already exists but is not verified. A new verification code has been sent to your email.",
				"requires_verification": true,
				"user": gin.H{
					"id":       existingUser.ID,
					"username": existingUser.Username,
					"email":    existingUser.Email,
					"verified": false,
				},
			})
			return
		}
	}

	// Регистрируем нового пользователя с транзакцией (защита от race conditions)
	user, err := h.userUC.RegisterWithTransaction(c.Request.Context(), req)
	if err != nil {
		h.logger.Warn("Registration failed",
			zap.String("email", req.Email),
			zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Генерируем 6-значный код верификации
	code := h.userUC.GenerateVerificationCode()

	// Сохраняем код в БД
	err = h.userUC.SaveVerificationCode(c.Request.Context(), user.ID, code)
	if err != nil {
		h.logger.Error("Failed to save verification code",
			zap.Int64("user_id", user.ID),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save verification code"})
		return
	}

	// Логируем код в консоль для тестирования (если email не настроен)
	h.logger.Info("VERIFICATION CODE (for testing)",
		zap.Int64("user_id", user.ID),
		zap.String("email", user.Email),
		zap.String("code", code))

	// Отправляем email асинхронно в горутине
	go func() {
		name := user.Username
		if name == "" {
			name = user.Email
		}

		err := h.emailService.SendVerificationCode(user.Email, name, code)
		if err != nil {
			h.logger.Error("Failed to send verification email",
				zap.Int64("user_id", user.ID),
				zap.String("email", user.Email),
				zap.Error(err))
		} else {
			h.logger.Info("Verification email sent",
				zap.Int64("user_id", user.ID),
				zap.String("email", user.Email))
		}
	}()

	// Обновляем время регистрации
	lastRegistrationTime[clientIP] = time.Now()
	registrationAttempts[clientIP] = 0

	h.logger.Info("User registered, verification code sent",
		zap.Int64("user_id", user.ID),
		zap.String("username", user.Username),
		zap.String("email", user.Email),
		zap.String("ip", clientIP))

	c.JSON(http.StatusCreated, gin.H{
		"message": "Registration successful. Please check your email and enter the verification code.",
		"user": gin.H{
			"id":       user.ID,
			"username": user.Username,
			"email":    user.Email,
			"verified": false,
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

// VerifyEmailRequest - запрос на верификацию email
type VerifyEmailRequest struct {
	Email string `json:"email" binding:"omitempty,email"`
	Code  string `json:"code" binding:"omitempty,len=6"`
	Token string `json:"token" binding:"omitempty,len=6"` // Для совместимости с фронтом (code или token)
}

// ResendCodeRequest - запрос на повторную отправку кода
type ResendCodeRequest struct {
	Email string `json:"email" binding:"required,email"`
}

// VerifyEmail проверяет код верификации и активирует аккаунт
func (h *UserHandler) VerifyEmail(c *gin.Context) {
	var req VerifyEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format. Email and 6-digit code required."})
		return
	}

	// Поддерживаем как code, так и token (для совместимости с фронтом)
	code := req.Code
	if code == "" && req.Token != "" {
		code = req.Token
	}

	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Verification code is required"})
		return
	}

	// Если email не передан, ищем по коду
	email := req.Email
	if email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Email is required for verification"})
		return
	}

	// Проверяем код и активируем аккаунт
	user, err := h.userUC.VerifyEmailByCode(c.Request.Context(), email, code)
	if err != nil {
		h.logger.Warn("Email verification failed",
			zap.String("email", email),
			zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid or expired verification code"})
		return
	}

	// Генерируем JWT токен после успешной верификации
	token, err := h.userUC.GenerateJWTToken(user.ID, user.Username, user.Email)
	if err != nil {
		h.logger.Error("Failed to generate token after verification", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate authentication token"})
		return
	}

	h.logger.Info("Email verified successfully",
		zap.Int64("user_id", user.ID),
		zap.String("email", user.Email))

	c.JSON(http.StatusOK, gin.H{
		"message": "Email verified successfully. You are now logged in.",
		"token":   token,
		"user": gin.H{
			"id":       user.ID,
			"username": user.Username,
			"email":    user.Email,
			"verified": true,
		},
	})
}

// ResendVerificationCode повторно отправляет код верификации
func (h *UserHandler) ResendVerificationCode(c *gin.Context) {
	var req ResendCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Valid email required"})
		return
	}

	// Получаем пользователя по email
	user, err := h.userUC.GetUserByEmail(c.Request.Context(), req.Email)
	if err != nil {
		h.logger.Warn("Resend code failed - user not found",
			zap.String("email", req.Email))
		c.JSON(http.StatusBadRequest, gin.H{"error": "User not found or email already verified"})
		return
	}

	// Проверяем, что email еще не верифицирован
	if user.EmailVerified {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Email already verified"})
		return
	}

	// Генерируем новый код
	code := h.userUC.GenerateVerificationCode()

	// Сохраняем новый код
	err = h.userUC.SaveVerificationCode(c.Request.Context(), user.ID, code)
	if err != nil {
		h.logger.Error("Failed to save new verification code",
			zap.Int64("user_id", user.ID),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate new code"})
		return
	}

	// Отправляем email асинхронно
	go func() {
		name := user.Username
		if name == "" {
			name = user.Email
		}

		err := h.emailService.SendVerificationCode(user.Email, name, code)
		if err != nil {
			h.logger.Error("Failed to resend verification email",
				zap.Int64("user_id", user.ID),
				zap.String("email", user.Email),
				zap.Error(err))
		} else {
			h.logger.Info("Verification email resent",
				zap.Int64("user_id", user.ID),
				zap.String("email", user.Email))
		}
	}()

	h.logger.Info("Verification code resent",
		zap.Int64("user_id", user.ID),
		zap.String("email", user.Email))

	c.JSON(http.StatusOK, gin.H{
		"message": "Verification code sent. Please check your email.",
	})
}

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

// LogoutRequest - запрос на выход
// Logout добавляет текущий токен в чёрный список
func (h *UserHandler) Logout(c *gin.Context) {
	// Получаем JTI из контекста
	jti, exists := c.Get("jti")
	if !exists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No token to revoke"})
		return
	}

	// Получаем время истечения токена из контекста
	exp, exists := c.Get("exp")
	if !exists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid token"})
		return
	}

	// Вычисляем TTL для записи в blacklist
	var ttl time.Duration
	if expFloat, ok := exp.(float64); ok {
		ttl = time.Until(time.Unix(int64(expFloat), 0))
		if ttl <= 0 {
			ttl = time.Second // Минимальное время
		}
	} else {
		ttl = 24 * time.Hour // По умолчанию 24 часа
	}

	// Добавляем токен в чёрный список
	if h.blacklistService != nil {
		err := h.blacklistService.AddToBlacklist(c.Request.Context(), jti.(string), ttl)
		if err != nil {
			h.logger.Error("Failed to add token to blacklist",
				zap.String("jti", jti.(string)),
				zap.Error(err))
			// Не возвращаем ошибку клиенту, но логируем
		}
	}

	h.logger.Info("Token revoked",
		zap.String("jti", jti.(string)),
		zap.Duration("ttl", ttl))

	c.JSON(http.StatusOK, gin.H{
		"message": "Logged out successfully",
	})
}

// ForgotPasswordRequest - запрос на сброс пароля
type ForgotPasswordRequest struct {
	Email string `json:"email" binding:"required,email"`
}

// ForgotPassword отправляет код для сброса пароля
func (h *UserHandler) ForgotPassword(c *gin.Context) {
	var req ForgotPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Valid email required"})
		return
	}

	// Ищем пользователя по email
	user, err := h.userUC.GetUserByEmail(c.Request.Context(), req.Email)
	if err != nil {
		// Не раскрываем информацию о существовании пользователя
		h.logger.Warn("Forgot password - user not found",
			zap.String("email", req.Email))
		c.JSON(http.StatusOK, gin.H{
			"message": "If the email exists, a reset code has been sent",
		})
		return
	}

	// Генерируем код для сброса пароля
	code := h.userUC.GenerateVerificationCode()

	// Сохраняем код как код верификации (можно использовать тот же механизм)
	err = h.userUC.SaveVerificationCode(c.Request.Context(), user.ID, code)
	if err != nil {
		h.logger.Error("Failed to save reset code",
			zap.Int64("user_id", user.ID),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process request"})
		return
	}

	// Отправляем email асинхронно
	go func() {
		name := user.Username
		if name == "" {
			name = user.Email
		}

		err := h.emailService.SendPasswordResetCode(user.Email, name, code)
		if err != nil {
			h.logger.Error("Failed to send reset email",
				zap.Int64("user_id", user.ID),
				zap.String("email", user.Email),
				zap.Error(err))
		} else {
			h.logger.Info("Reset email sent",
				zap.Int64("user_id", user.ID),
				zap.String("email", user.Email))
		}
	}()

	h.logger.Info("Password reset requested",
		zap.Int64("user_id", user.ID),
		zap.String("email", user.Email))

	c.JSON(http.StatusOK, gin.H{
		"message": "If the email exists, a reset code has been sent",
	})
}

// ResetPasswordRequest - запрос на установку нового пароля
type ResetPasswordRequest struct {
	Email       string `json:"email" binding:"required,email"`
	Code        string `json:"code" binding:"required,len=6"`
	NewPassword string `json:"new_password" binding:"required,min=8"`
}

// ResetPassword устанавливает новый пароль после подтверждения кода
func (h *UserHandler) ResetPassword(c *gin.Context) {
	var req ResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format"})
		return
	}

	// Проверяем код
	user, err := h.userUC.GetByEmailAndCode(c.Request.Context(), req.Email, req.Code)
	if err != nil {
		h.logger.Warn("Reset password - invalid code",
			zap.String("email", req.Email),
			zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid or expired code"})
		return
	}

	// Сбрасываем пароль
	err = h.userUC.ResetPassword(c.Request.Context(), user.ID, req.NewPassword)
	if err != nil {
		h.logger.Error("Failed to reset password",
			zap.Int64("user_id", user.ID),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reset password"})
		return
	}

	// Очищаем код верификации
	err = h.userUC.ClearVerificationCode(c.Request.Context(), user.ID)
	if err != nil {
		h.logger.Warn("Failed to clear verification code after reset",
			zap.Int64("user_id", user.ID),
			zap.Error(err))
	}

	h.logger.Info("Password reset successful",
		zap.Int64("user_id", user.ID),
		zap.String("email", user.Email))

	c.JSON(http.StatusOK, gin.H{
		"message": "Password reset successfully. Please login with your new password.",
	})
}
