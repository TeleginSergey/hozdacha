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
	Email string `json:"email" binding:"required,email"`
}

type AuthResponse struct {
	Token string   `json:"token"`
	User  *db.User `json:"user"`
}

// normalizeEmail приводит email к каноничному виду: без пробелов по краям и в нижнем
// регистре. Email регистронезависим, поэтому храним и сравниваем его нормализованным.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func (u *UserUsecase) Register(ctx context.Context, req RegisterRequest) (*db.User, error) {
	// Нормализуем email до всех проверок и сохранения.
	req.Email = normalizeEmail(req.Email)

	// Валидация
	if req.Email == "" || req.Name == "" || req.Phone == "" {
		return nil, fmt.Errorf("нужны почта, имя и телефон")
	}

	// Honeypot проверка - если поле website заполнено, это бот
	if strings.TrimSpace(req.Website) != "" {
		return nil, fmt.Errorf("обнаружен бот")
	}

	// Генерируем username из email, если не указан
	username := req.Username
	if username == "" {
		parts := strings.Split(req.Email, "@")
		username = strings.ToLower(parts[0])
		n, _ := rand.Int(rand.Reader, big.NewInt(10000))
		username = fmt.Sprintf("%s%d", username, n.Int64())
	}

	// Проверяем существует ли пользователь
	exists, err := u.users.ExistsByUsernameOrEmail(ctx, username, req.Email)
	if err != nil {
		return nil, fmt.Errorf("не удалось проверить пользователя: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("пользователь с таким именем или почтой уже есть")
	}

	// Создаем пользователя (без пароля — вход по коду)
	now := time.Now()
	user := &db.User{
		Username:      username,
		Email:         req.Email,
		Password:      "", // Без пароля
		RoleID:        2,
		EmailVerified: false,
		FullName:      &req.Name,
		Phone:         &req.Phone,
		CreatedAt:     &now,
		UpdatedAt:     &now,
	}

	user, err = u.users.Insert(ctx, user)
	if err != nil {
		return nil, fmt.Errorf("не удалось создать пользователя: %w", err)
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
	email = normalizeEmail(email)
	// Ищем пользователя с указанным email и кодом
	user, err := u.users.GetByEmailAndCode(ctx, email, code)
	if err != nil {
		return nil, fmt.Errorf("неверный или просроченный код")
	}

	// Помечаем email как верифицированный
	err = u.users.VerifyEmailByCode(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("не удалось подтвердить почту: %w", err)
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
	// Нормализуем email до всех проверок и сохранения.
	req.Email = normalizeEmail(req.Email)

	// Валидация
	if req.Email == "" || req.Name == "" || req.Phone == "" {
		return nil, fmt.Errorf("нужны почта, имя и телефон")
	}

	// Honeypot проверка
	if strings.TrimSpace(req.Website) != "" {
		return nil, fmt.Errorf("обнаружен бот")
	}

	// Генерируем username из email
	username := req.Username
	if username == "" {
		parts := strings.Split(req.Email, "@")
		username = strings.ToLower(parts[0])
		n, _ := rand.Int(rand.Reader, big.NewInt(10000))
		username = fmt.Sprintf("%s%d", username, n.Int64())
	}

	// Проверяем существует ли пользователь
	exists, err := u.users.ExistsByUsernameOrEmail(ctx, username, req.Email)
	if err != nil {
		return nil, fmt.Errorf("не удалось проверить пользователя: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("пользователь с таким именем или почтой уже есть")
	}

	// Начинаем транзакцию
	tx, err := u.users.BeginTx(ctx)
	if err != nil {
		return nil, fmt.Errorf("не удалось начать транзакцию: %w", err)
	}
	defer tx.Rollback(ctx)

	now := time.Now()
	user := &db.User{
		Username:      username,
		Email:         req.Email,
		Password:      "", // Без пароля
		RoleID:        2,
		EmailVerified: false,
		FullName:      &req.Name,
		Phone:         &req.Phone,
		CreatedAt:     &now,
		UpdatedAt:     &now,
	}

	user, err = u.users.InsertWithTx(ctx, tx, user)
	if err != nil {
		return nil, fmt.Errorf("не удалось создать пользователя: %w", err)
	}

	err = tx.Commit(ctx)
	if err != nil {
		return nil, fmt.Errorf("не удалось завершить транзакцию: %w", err)
	}

	u.logger.Info("User registered with transaction, awaiting email verification",
		zap.Int64("user_id", user.ID),
		zap.String("email", user.Email))

	return user, nil
}

// Login находит пользователя по email (для отправки кода).
// Сама проверка кода и выдача токена — в VerifyEmailByCode.
func (u *UserUsecase) Login(ctx context.Context, req LoginRequest) (*db.User, error) {
	req.Email = normalizeEmail(req.Email)
	if req.Email == "" {
		return nil, fmt.Errorf("нужна почта")
	}

	user, err := u.users.GetByEmail(ctx, req.Email)
	if err != nil {
		return nil, fmt.Errorf("не удалось найти пользователя: %w", err)
	}
	if user == nil {
		return nil, fmt.Errorf("пользователь с такой почтой не найден")
	}

	return user, nil
}

// GenerateJWTToken генерирует JWT токен для пользователя
func (u *UserUsecase) GenerateJWTToken(userID int64, username, email string) (string, error) {
	// По умолчанию роль "user"
	return generateJWTTokenWithRole(userID, username, email, "user", u.jwtSecret)
}

// GetUserByEmail получает пользователя по email
func (u *UserUsecase) GetUserByEmail(ctx context.Context, email string) (*db.User, error) {
	email = normalizeEmail(email)
	user, err := u.users.GetByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("пользователь не найден")
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
		return nil, fmt.Errorf("не удалось загрузить профиль: %w", err)
	}
	if user == nil {
		return nil, fmt.Errorf("пользователь не найден")
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
		return nil, fmt.Errorf("не удалось найти пользователя: %w", err)
	}
	if user == nil {
		return nil, fmt.Errorf("пользователь не найден")
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
		return fmt.Errorf("пароль слишком простой")
	}

	// Хешируем новый пароль
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("не удалось захешировать пароль: %w", err)
	}

	// Получаем пользователя
	user, err := u.users.GetByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("не удалось найти пользователя: %w", err)
	}

	// Обновляем пароль
	user.Password = string(hashedPassword)
	now := time.Now()
	user.UpdatedAt = &now

	_, err = u.users.Update(ctx, user, userID)
	if err != nil {
		return fmt.Errorf("не удалось обновить пароль: %w", err)
	}

	u.logger.Info("Password reset successfully",
		zap.Int64("user_id", userID))

	return nil
}

// ClearVerificationCode очищает код верификации после использования
func (u *UserUsecase) ClearVerificationCode(ctx context.Context, userID int64) error {
	return u.users.VerifyEmailByCode(ctx, userID)
}
