package usecase

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/TeleginSergey/hozdacha/internal/db"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

type UserUsecase struct {
	users     db.UserQuery
	logger    *zap.Logger
	jwtSecret string
}

func NewUserUsecase(userRepo db.UserQuery, logger *zap.Logger, jwtSecret string) *UserUsecase {
	return &UserUsecase{
		users:     userRepo,
		logger:    logger,
		jwtSecret: jwtSecret,
	}
}

type RegisterRequest struct {
	Username string `json:"username" binding:"required,min=3,max=50"`
	Email    string `json:"email" binding:"omitempty,email"`
	Password string `json:"password" binding:"required,min=8"`
	Website  string `json:"website"` // Honeypot field - должно быть пустым
}

type ResetPasswordRequest struct {
	Username     string `json:"username" binding:"required"`
	SecretAnswer string `json:"secret_answer" binding:"required"`
	NewPassword  string `json:"new_password" binding:"required,min=8"`
}

type SetSecretQuestionRequest struct {
	SecretQuestion string `json:"secret_question" binding:"required"`
	SecretAnswer   string `json:"secret_answer" binding:"required"`
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type AuthResponse struct {
	Token string   `json:"token"`
	User  *db.User `json:"user"`
}

func (u *UserUsecase) Register(ctx context.Context, req RegisterRequest) (*db.User, error) {
	// Валидация
	if req.Username == "" || req.Password == "" {
		return nil, fmt.Errorf("username and password are required")
	}

	// Honeypot проверка - если поле website заполнено, это бот
	if strings.TrimSpace(req.Website) != "" {
		return nil, fmt.Errorf("bot detected")
	}

	// Проверяем сложность пароля
	if !u.isPasswordStrong(req.Password) {
		return nil, fmt.Errorf("password is too weak. Use at least 8 characters with letters, numbers and symbols.")
	}

	// Проверяем существует ли пользователь
	exists, err := u.users.ExistsByUsernameOrEmail(ctx, req.Username, req.Email)
	if err != nil {
		return nil, fmt.Errorf("failed to check user existence: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("user with this username or email already exists")
	}

	// Хешируем пароль
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	// Создаем пользователя (сразу активного)
	now := time.Now()
	user := &db.User{
		Username:      req.Username,
		Email:         req.Email,
		Password:      string(hashedPassword),
		RoleID:        2,    // Роль "user" (обычно ID 2)
		EmailVerified: true, // Сразу верифицирован
		CreatedAt:     &now,
		UpdatedAt:     &now,
	}

	user, err = u.users.Insert(ctx, user)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	// Пользователь сразу активен - email верификация не нужна
	u.logger.Info("User registered successfully",
		zap.Int64("user_id", user.ID),
		zap.String("email", user.Email))

	return user, nil
}

func (u *UserUsecase) Login(ctx context.Context, req LoginRequest) (*AuthResponse, error) {
	if req.Username == "" || req.Password == "" {
		return nil, fmt.Errorf("username and password are required")
	}

	// Ищем пользователя по username или email
	user, err := u.users.GetByUsername(ctx, req.Username)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	if user == nil {
		// Пробуем найти по email если не нашли по username
		user, err = u.users.GetByEmail(ctx, req.Username)
		if err != nil {
			return nil, fmt.Errorf("failed to get user: %w", err)
		}
	}

	if user == nil {
		return nil, fmt.Errorf("invalid username or password")
	}

	// Проверяем пароль
	err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password))
	if err != nil {
		return nil, fmt.Errorf("invalid username or password")
	}

	// Генерируем JWT токен
	token, err := generateJWTToken(user.ID, user.Username, user.Email, u.jwtSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}

	// Обновляем время последнего входа
	now := time.Now()
	user.AuthTime = &now
	_, err = u.users.UpdateLoginOrLogout(ctx, user, user.ID)
	if err != nil {
		u.logger.Warn("Failed to update user auth time", zap.Error(err))
	}

	// Не возвращаем чувствительные данные
	user.Password = ""
	user.AccessTokenSecret = ""
	user.RefreshTokenSecret = ""

	return &AuthResponse{
		Token: token,
		User:  user,
	}, nil
}

func generateJWTToken(userID int64, username, email, secret string) (string, error) {
	claims := jwt.MapClaims{
		"user_id":  userID,
		"username": username,
		"email":    email,
		"exp":      time.Now().Add(time.Hour * 24).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func (u *UserUsecase) GetProfile(ctx context.Context, userID int64) (*db.User, error) {
	user, err := u.users.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user profile: %w", err)
	}
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}

	// Не возвращаем чувствительные данные
	user.Password = ""
	user.AccessTokenSecret = ""
	user.RefreshTokenSecret = ""

	return user, nil
}

// Email verification methods removed - users are activated immediately

func (u *UserUsecase) UpdateProfile(ctx context.Context, userID int64, req map[string]interface{}) (*db.User, error) {
	user, err := u.users.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}

	// В структуре User нет полей Name, Phone, Address - пропускаем обновление
	// Можно добавить дополнительные поля в БД если нужно

	return u.users.Update(ctx, user, userID)
}

// isPasswordStrong проверяет сложность пароля
func (u *UserUsecase) isPasswordStrong(password string) bool {
	if len(password) < 8 {
		return false
	}

	var (
		hasUpper   = false
		hasLower   = false
		hasNumber  = false
		hasSpecial = false
	)

	for _, char := range password {
		switch {
		case char >= 'A' && char <= 'Z':
			hasUpper = true
		case char >= 'a' && char <= 'z':
			hasLower = true
		case char >= '0' && char <= '9':
			hasNumber = true
		case char == '!' || char == '@' || char == '#' || char == '$' || char == '%' || char == '^' || char == '&' || char == '*':
			hasSpecial = true
		}
	}

	// Требуем минимум 3 из 4 условий
	conditions := 0
	if hasUpper {
		conditions++
	}
	if hasLower {
		conditions++
	}
	if hasNumber {
		conditions++
	}
	if hasSpecial {
		conditions++
	}

	return conditions >= 3
}

func generateSecret() string {
	key := make([]byte, 32)
	rand.Read(key)
	return base64.URLEncoding.EncodeToString(key)
}
