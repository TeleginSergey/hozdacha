package usecase

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"regexp"
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
	Username string `json:"username" binding:"omitempty,min=3,max=50"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
	Phone    string `json:"phone" binding:"required"`
	Name     string `json:"name" binding:"required,max=32"`
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
	if req.Email == "" || req.Password == "" || req.Name == "" || req.Phone == "" {
		return nil, fmt.Errorf("email, password, name and phone are required")
	}

	// Honeypot проверка - если поле website заполнено, это бот
	if strings.TrimSpace(req.Website) != "" {
		return nil, fmt.Errorf("bot detected")
	}

	// Проверяем сложность пароля
	if !u.isPasswordStrong(req.Password) {
		return nil, fmt.Errorf("password is too weak. Use at least 8 characters with letters, numbers and symbols.")
	}

	// Генерируем username из email, если не указан
	username := req.Username
	if username == "" {
		// Берём часть до @ из email
		parts := strings.Split(req.Email, "@")
		username = parts[0]
		// Добавляем случайные цифры для уникальности
		n, _ := rand.Int(rand.Reader, big.NewInt(10000))
		username = fmt.Sprintf("%s%d", username, n.Int64())
	}

	// Проверяем существует ли пользователь
	exists, err := u.users.ExistsByUsernameOrEmail(ctx, username, req.Email)
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

	// Создаем пользователя (требуется верификация email)
	now := time.Now()
	user := &db.User{
		Username:      username,
		Email:         req.Email,
		Password:      string(hashedPassword),
		RoleID:        2,     // Роль "user" (обычно ID 2)
		EmailVerified: false, // Требуется верификация email
		FullName:      &req.Name,
		Phone:         &req.Phone,
		CreatedAt:     &now,
		UpdatedAt:     &now,
	}

	user, err = u.users.Insert(ctx, user)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	u.logger.Info("User registered, awaiting email verification",
		zap.Int64("user_id", user.ID),
		zap.String("email", user.Email))

	return user, nil
}

// GenerateVerificationCode генерирует криптографически стойкий 6-значный код верификации
func (u *UserUsecase) GenerateVerificationCode() string {
	n, err := rand.Int(rand.Reader, big.NewInt(900000))
	if err != nil {
		// Криптогенератор недоступен — падаем, чтобы не выдавать предсказуемые коды
		panic(fmt.Sprintf("crypto/rand unavailable: %v", err))
	}
	return fmt.Sprintf("%06d", 100000+n.Int64())
}

// SaveVerificationCode сохраняет код верификации в БД
func (u *UserUsecase) SaveVerificationCode(ctx context.Context, userID int64, code string) error {
	expiresAt := time.Now().Add(30 * time.Minute) // Код действителен 30 минут
	return u.users.UpdateVerificationCode(ctx, userID, code, expiresAt)
}

// VerifyEmailByCode проверяет код верификации и активирует аккаунт
func (u *UserUsecase) VerifyEmailByCode(ctx context.Context, email, code string) (*db.User, error) {
	// Ищем пользователя с указанным email и кодом
	user, err := u.users.GetByEmailAndCode(ctx, email, code)
	if err != nil {
		return nil, fmt.Errorf("invalid or expired verification code")
	}

	// Помечаем email как верифицированный
	err = u.users.VerifyEmailByCode(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to verify email: %w", err)
	}

	// Обновляем данные пользователя
	user.EmailVerified = true
	user.EmailVerificationCode = nil
	user.VerificationExpiresAt = nil

	u.logger.Info("Email verified successfully",
		zap.Int64("user_id", user.ID),
		zap.String("email", user.Email))

	return user, nil
}

// RegisterWithTransaction регистрирует пользователя в одной транзакции для защиты от race conditions
func (u *UserUsecase) RegisterWithTransaction(ctx context.Context, req RegisterRequest) (*db.User, error) {
	// Валидация
	if req.Email == "" || req.Password == "" || req.Name == "" || req.Phone == "" {
		return nil, fmt.Errorf("email, password, name and phone are required")
	}

	// Honeypot проверка - если поле website заполнено, это бот
	if strings.TrimSpace(req.Website) != "" {
		return nil, fmt.Errorf("bot detected")
	}

	// Проверяем сложность пароля
	if !u.isPasswordStrong(req.Password) {
		return nil, fmt.Errorf("password is too weak. Use at least 8 characters with letters, numbers and symbols.")
	}

	// Генерируем username из email, если не указан
	username := req.Username
	if username == "" {
		// Берём часть до @ из email
		parts := strings.Split(req.Email, "@")
		username = parts[0]
		// Добавляем случайные цифры для уникальности
		n, _ := rand.Int(rand.Reader, big.NewInt(10000))
		username = fmt.Sprintf("%s%d", username, n.Int64())
	}

	// Проверяем существует ли пользователь (в транзакции)
	exists, err := u.users.ExistsByUsernameOrEmail(ctx, username, req.Email)
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

	// Начинаем транзакцию
	tx, err := u.users.BeginTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Создаем пользователя (требуется верификация email)
	now := time.Now()
	user := &db.User{
		Username:      username,
		Email:         req.Email,
		Password:      string(hashedPassword),
		RoleID:        2,     // Роль "user" (обычно ID 2)
		EmailVerified: false, // Требуется верификация email
		FullName:      &req.Name,
		Phone:         &req.Phone,
		CreatedAt:     &now,
		UpdatedAt:     &now,
	}

	// Вставляем пользователя в транзакции
	user, err = u.users.InsertWithTx(ctx, tx, user)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	// Коммитим транзакцию
	err = tx.Commit(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	u.logger.Info("User registered with transaction, awaiting email verification",
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

	// Проверяем, что email верифицирован
	if !user.EmailVerified {
		return nil, fmt.Errorf("email not verified. Please verify your email before logging in")
	}

	// Определяем роль пользователя
	role := "user"
	if user.RoleID == 1 {
		role = "admin"
	}

	// Генерируем JWT токен с ролью
	token, err := generateJWTTokenWithRole(user.ID, user.Username, user.Email, role, u.jwtSecret)
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
	user.AccessTokenSecret = nil
	user.RefreshTokenSecret = nil

	return &AuthResponse{
		Token: token,
		User:  user,
	}, nil
}

// GenerateJWTToken генерирует JWT токен для пользователя
func (u *UserUsecase) GenerateJWTToken(userID int64, username, email string) (string, error) {
	// По умолчанию роль "user"
	return generateJWTTokenWithRole(userID, username, email, "user", u.jwtSecret)
}

// GetUserByEmail получает пользователя по email
func (u *UserUsecase) GetUserByEmail(ctx context.Context, email string) (*db.User, error) {
	user, err := u.users.GetByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}
	return user, nil
}

func generateJWTToken(userID int64, username, email, secret string) (string, error) {
	// Для обратной совместимости - генерируем токен без роли
	return generateJWTTokenWithRole(userID, username, email, "user", secret)
}

// generateJWTTokenWithRole генерирует JWT токен с указанием роли
func generateJWTTokenWithRole(userID int64, username, email, role, secret string) (string, error) {
	now := time.Now()

	// Генерируем уникальный JTI (JWT ID) для возможности отзыва токена
	jti := generateJTI()

	claims := jwt.MapClaims{
		"user_id":  userID,
		"username": username,
		"email":    email,
		"role":     role,                           // Роль пользователя
		"jti":      jti,                            // Уникальный ID токена
		"exp":      now.Add(time.Hour * 24).Unix(), // 24 часа
		"iat":      now.Unix(),                     // Время выпуска
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// generateJTI создаёт уникальный идентификатор для JWT токена
func generateJTI() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
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
	user.AccessTokenSecret = nil
	user.RefreshTokenSecret = nil

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

	// Обновляем поля если они переданы
	if fullName, ok := req["full_name"].(string); ok {
		if len(fullName) > 32 {
			return nil, fmt.Errorf("имя не должно быть длиннее 32 символов")
		}
		user.FullName = &fullName
	}
	if phone, ok := req["phone"].(string); ok {
		digits := regexp.MustCompile(`\D`).ReplaceAllString(phone, "")
		if len(digits) != 11 || (digits[0] != '7' && digits[0] != '8') {
			return nil, fmt.Errorf("неверный формат телефона")
		}
		user.Phone = &phone
	}

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

// GetByEmailAndCode возвращает пользователя по email и коду верификации
func (u *UserUsecase) GetByEmailAndCode(ctx context.Context, email, code string) (*db.User, error) {
	return u.users.GetByEmailAndCode(ctx, email, code)
}

// ResetPassword сбрасывает пароль пользователя
func (u *UserUsecase) ResetPassword(ctx context.Context, userID int64, newPassword string) error {
	// Проверяем сложность пароля
	if !u.isPasswordStrong(newPassword) {
		return fmt.Errorf("password is too weak")
	}

	// Хешируем новый пароль
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	// Получаем пользователя
	user, err := u.users.GetByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	// Обновляем пароль
	user.Password = string(hashedPassword)
	now := time.Now()
	user.UpdatedAt = &now

	_, err = u.users.Update(ctx, user, userID)
	if err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}

	u.logger.Info("Password reset successfully",
		zap.Int64("user_id", userID))

	return nil
}

// ClearVerificationCode очищает код верификации после использования
func (u *UserUsecase) ClearVerificationCode(ctx context.Context, userID int64) error {
	return u.users.VerifyEmailByCode(ctx, userID)
}
