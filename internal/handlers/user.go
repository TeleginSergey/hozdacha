package handlers

import (
	"net/http"
	"strings"
	"sync"
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
	// codeAttempts — счётчик неверных попыток ввода кода по email (анти-брутфорс).
	codeAttempts *attemptTracker
}

// maxCodeAttempts — после стольких неверных попыток код инвалидируется и нужен новый.
const maxCodeAttempts = 5

func NewUserHandler(userUC *usecase.UserUsecase, emailService *services.EmailService, blacklistService *services.TokenBlacklistService, logger *zap.Logger) *UserHandler {
	return &UserHandler{
		userUC:           userUC,
		emailService:     emailService,
		blacklistService: blacklistService,
		logger:           logger,
		codeAttempts:     newAttemptTracker(15 * time.Minute),
	}
}

// Тайминг регистрации / входа по IP (защищен mutex от data race)
var (
	registrationMu       sync.RWMutex
	registrationAttempts = make(map[string]int)
	lastRegistrationTime = make(map[string]time.Time)
)

// Периодически чистим карты тайминга регистрации/входа, чтобы они не росли
// бесконечно по уникальным IP (защита от утечки памяти / OOM-DoS).
func init() {
	go func() {
		for {
			time.Sleep(10 * time.Minute)
			registrationMu.Lock()
			now := time.Now()
			for ip, t := range lastRegistrationTime {
				if now.Sub(t) > time.Hour {
					delete(lastRegistrationTime, ip)
					delete(registrationAttempts, ip)
				}
			}
			registrationMu.Unlock()
		}
	}()
}

func (h *UserHandler) Register(c *gin.Context) {
	var req usecase.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный формат запроса"})
		return
	}

	// Защита от спама - проверяем IP
	clientIP := c.ClientIP()

	// Rate limiting: максимум 3 попытки в час с одного IP
	const maxAttemptsPerHour = 3
	const blockDuration = 1 * time.Hour
	const minRegistrationInterval = 1 * time.Minute

	// Ограничение по времени (не чаще 1 раза в минуту)
	registrationMu.RLock()
	lastTime, exists := lastRegistrationTime[clientIP]
	registrationMu.RUnlock()

	if exists {
		if time.Since(lastTime) < minRegistrationInterval {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "Подождите перед повторной попыткой"})
			return
		}

		if time.Since(lastTime) < blockDuration {
			registrationMu.Lock()
			registrationAttempts[clientIP]++
			attempts := registrationAttempts[clientIP]
			registrationMu.Unlock()

			if attempts > maxAttemptsPerHour {
				h.logger.Warn("Too many registration attempts",
					zap.String("ip", clientIP),
					zap.Int("attempts", attempts))
				c.JSON(http.StatusTooManyRequests, gin.H{
					"error":       "Слишком много попыток. Попробуйте позже.",
					"retry_after": int(time.Until(lastTime.Add(time.Hour)).Seconds()),
				})
				return
			}

			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":       "Подождите перед повторной регистрацией",
				"retry_after": int(time.Until(lastTime.Add(time.Hour)).Seconds()),
			})
			return
		}
	}

	// Валидация
	if len(req.Email) < 3 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Почта должна быть не короче 3 символов"})
		return
	}
	if !strings.Contains(req.Email, "@") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный формат почты"})
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
			c.JSON(http.StatusBadRequest, gin.H{"error": "Пользователь с такой почтой уже зарегистрирован. Войдите."})
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
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось создать код подтверждения"})
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

			c.JSON(http.StatusAccepted, gin.H{
				"message":               "Пользователь с такой почтой уже есть, но не подтверждён. Новый код отправлен на почту.",
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось сохранить код подтверждения"})
		return
	}

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
	registrationMu.Lock()
	lastRegistrationTime[clientIP] = time.Now()
	registrationAttempts[clientIP] = 0
	registrationMu.Unlock()

	h.logger.Info("User registered, verification code sent",
		zap.Int64("user_id", user.ID),
		zap.String("username", user.Username),
		zap.String("email", user.Email),
		zap.String("ip", clientIP))

	c.JSON(http.StatusCreated, gin.H{
		"message": "Регистрация прошла успешно. Проверьте почту и введите код подтверждения.",
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный формат запроса"})
		return
	}

	// Защита от брутфорса
	clientIP := c.ClientIP()
	registrationMu.RLock()
	lastLogin, loginExists := lastRegistrationTime[clientIP]
	registrationMu.RUnlock()
	if loginExists && time.Since(lastLogin) < 5*time.Second {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Слишком много попыток входа. Подождите."})
		return
	}

	user, err := h.userUC.Login(c.Request.Context(), req)
	registrationMu.Lock()
	lastRegistrationTime[clientIP] = time.Now()
	registrationMu.Unlock()
	if err != nil {
		h.logger.Warn("Login failed",
			zap.String("email", req.Email),
			zap.String("ip", clientIP),
			zap.Error(err))
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Пользователь с такой почтой не найден. Зарегистрируйтесь."})
		return
	}

	// Генерируем код и отправляем на почту
	code := h.userUC.GenerateVerificationCode()
	err = h.userUC.SaveVerificationCode(c.Request.Context(), user.ID, code)
	if err != nil {
		h.logger.Error("Failed to save login code", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось создать код"})
		return
	}

	go func() {
		name := user.Username
		if user.FullName != nil && *user.FullName != "" {
			name = *user.FullName
		}
		if err := h.emailService.SendVerificationCode(user.Email, name, code); err != nil {
			h.logger.Error("Failed to send login code", zap.Error(err))
		}
	}()

	h.logger.Info("Login code sent",
		zap.Int64("user_id", user.ID),
		zap.String("email", user.Email))

	c.JSON(http.StatusOK, gin.H{
		"message": "Код отправлен на почту. Введите его для входа.",
		"email":   user.Email,
	})
}

func (h *UserHandler) GetProfile(c *gin.Context) {
	// Проверяем авторизацию
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Не авторизован"})
		return
	}

	user, err := h.userUC.GetProfile(c.Request.Context(), userID.(int64))
	if err != nil {
		h.logger.Error("Failed to get user profile", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось загрузить профиль"})
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

// invalidateCode стирает текущий код подтверждения пользователя (по email),
// чтобы после превышения лимита попыток перебор был бесполезен.
func (h *UserHandler) invalidateCode(c *gin.Context, email string) {
	user, err := h.userUC.GetUserByEmail(c.Request.Context(), email)
	if err != nil || user == nil {
		return
	}
	if err := h.userUC.ClearVerificationCode(c.Request.Context(), user.ID); err != nil {
		h.logger.Warn("Failed to invalidate verification code", zap.Int64("user_id", user.ID), zap.Error(err))
	}
}

// VerifyEmail проверяет код верификации и активирует аккаунт
func (h *UserHandler) VerifyEmail(c *gin.Context) {
	var req VerifyEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный формат запроса. Нужна почта и код из 6 цифр."})
		return
	}

	// Поддерживаем как code, так и token (для совместимости с фронтом)
	code := req.Code
	if code == "" && req.Token != "" {
		code = req.Token
	}

	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Нужен код подтверждения"})
		return
	}

	// Если email не передан, ищем по коду
	email := req.Email
	if email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Нужна почта для подтверждения"})
		return
	}

	// Анти-брутфорс: лимит неверных попыток по email. После порога код
	// инвалидируется, чтобы дальнейший перебор был бесполезен.
	attemptKey := strings.ToLower(strings.TrimSpace(email))
	if h.codeAttempts.get(attemptKey) >= maxCodeAttempts {
		h.invalidateCode(c, attemptKey)
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Слишком много попыток. Запросите новый код."})
		return
	}

	// Проверяем код и активируем аккаунт
	user, err := h.userUC.VerifyEmailByCode(c.Request.Context(), email, code)
	if err != nil {
		n := h.codeAttempts.inc(attemptKey)
		if n >= maxCodeAttempts {
			h.invalidateCode(c, attemptKey)
		}
		h.logger.Warn("Email verification failed",
			zap.String("email", email),
			zap.Int("attempts", n),
			zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный или просроченный код подтверждения"})
		return
	}
	h.codeAttempts.reset(attemptKey)

	// Генерируем JWT токен после успешной верификации
	token, err := h.userUC.GenerateJWTToken(user.ID, user.Username, user.Email)
	if err != nil {
		h.logger.Error("Failed to generate token after verification", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось создать токен"})
		return
	}

	h.logger.Info("Email verified successfully",
		zap.Int64("user_id", user.ID),
		zap.String("email", user.Email))

	c.JSON(http.StatusOK, gin.H{
		"message": "Почта подтверждена. Вы вошли.",
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "Нужна правильная почта"})
		return
	}

	// Получаем пользователя по email
	user, err := h.userUC.GetUserByEmail(c.Request.Context(), req.Email)
	if err != nil {
		h.logger.Warn("Resend code failed - user not found",
			zap.String("email", req.Email))
		c.JSON(http.StatusBadRequest, gin.H{"error": "Пользователь не найден или почта уже подтверждена"})
		return
	}

	// Проверяем, что email еще не верифицирован
	if user.EmailVerified {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Почта уже подтверждена"})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось создать новый код"})
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
		"message": "Код отправлен. Проверьте почту.",
	})
}

func (h *UserHandler) VerifyToken(c *gin.Context) {
	// Проверяем авторизацию
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Неверный токен"})
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
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Не авторизован"})
		return
	}

	var req map[string]interface{}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный формат запроса"})
		return
	}

	user, err := h.userUC.UpdateProfile(c.Request.Context(), userID.(int64), req)
	if err != nil {
		h.logger.Error("Failed to update profile", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Профиль обновлён",
		"user":    user,
	})
}

func (h *UserHandler) GenerateTOTP(c *gin.Context) {
	// Проверяем авторизацию
	_, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Не авторизован"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Двухфакторная аутентификация пока не настроена",
	})
}

func (h *UserHandler) VerifyTOTP(c *gin.Context) {
	var req struct {
		Token string `json:"token"`
		Code  string `json:"code"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный формат запроса"})
		return
	}

	// Проверяем авторизацию
	_, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Не авторизован"})
		return
	}

	// Для простоты примера - всегда успех
	c.JSON(http.StatusOK, gin.H{
		"message": "Двухфакторная проверка пройдена",
	})
}

// LogoutRequest - запрос на выход
// Logout добавляет текущий токен в чёрный список
func (h *UserHandler) Logout(c *gin.Context) {
	// Получаем JTI из контекста
	jti, exists := c.Get("jti")
	if !exists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Нет токена для отзыва"})
		return
	}

	// Получаем время истечения токена из контекста
	exp, exists := c.Get("exp")
	if !exists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный токен"})
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
		"message": "Вы вышли",
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "Нужна правильная почта"})
		return
	}

	// Ищем пользователя по email
	user, err := h.userUC.GetUserByEmail(c.Request.Context(), req.Email)
	if err != nil {
		// Не раскрываем информацию о существовании пользователя
		h.logger.Warn("Forgot password - user not found",
			zap.String("email", req.Email))
		c.JSON(http.StatusOK, gin.H{
			"message": "Если почта есть в системе, код отправлен",
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось обработать запрос"})
		return
	}

	// Отправляем email синхронно — чтобы видеть ошибки
	name := user.Username
	if name == "" {
		name = user.Email
	}

	h.logger.Info("Password reset requested — sending email",
		zap.Int64("user_id", user.ID),
		zap.String("email", user.Email),
		zap.String("code", code))

	err = h.emailService.SendPasswordResetCode(user.Email, name, code)
	if err != nil {
		h.logger.Error("Failed to send reset email",
			zap.Int64("user_id", user.ID),
			zap.String("email", user.Email),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось отправить письмо: " + err.Error()})
		return
	}

	h.logger.Info("Reset email sent successfully",
		zap.Int64("user_id", user.ID),
		zap.String("email", user.Email))

	c.JSON(http.StatusOK, gin.H{
		"message": "Если почта есть в системе, код отправлен",
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный формат запроса"})
		return
	}

	// Анти-брутфорс кода (как в VerifyEmail).
	attemptKey := strings.ToLower(strings.TrimSpace(req.Email))
	if h.codeAttempts.get(attemptKey) >= maxCodeAttempts {
		h.invalidateCode(c, attemptKey)
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Слишком много попыток. Запросите новый код."})
		return
	}

	// Проверяем код
	user, err := h.userUC.GetByEmailAndCode(c.Request.Context(), req.Email, req.Code)
	if err != nil {
		n := h.codeAttempts.inc(attemptKey)
		if n >= maxCodeAttempts {
			h.invalidateCode(c, attemptKey)
		}
		h.logger.Warn("Reset password - invalid code",
			zap.String("email", req.Email),
			zap.Int("attempts", n),
			zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": "Неверный или просроченный код"})
		return
	}
	h.codeAttempts.reset(attemptKey)

	if !strings.ContainsAny(req.NewPassword, "ABCDEFGHIJKLMNOPQRSTUVWXYZАБВГДЕЁЖЗИЙКЛМНОПРСТУФХЦЧШЩЪЫЬЭЮЯ") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Пароль должен содержать хотя бы одну заглавную букву"})
		return
	}

	// Сбрасываем пароль
	err = h.userUC.ResetPassword(c.Request.Context(), user.ID, req.NewPassword)
	if err != nil {
		h.logger.Error("Failed to reset password",
			zap.Int64("user_id", user.ID),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось сбросить пароль"})
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
		"message": "Пароль изменён. Войдите с новым паролем.",
	})
}
