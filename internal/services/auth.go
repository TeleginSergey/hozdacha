package services

import (
	"context"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"

	"github.com/TeleginSergey/hozdacha/internal/db"
)

type AuthService struct {
	userQuery db.UserQuery
	jwtSecret string
	logger    *zap.Logger
}

func NewAuthService(userQuery db.UserQuery, jwtSecret string, logger *zap.Logger) *AuthService {
	return &AuthService{
		userQuery: userQuery,
		jwtSecret: jwtSecret,
		logger:    logger,
	}
}

type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type LoginResponse struct {
	Token    string `json:"token"`
	UserID   int64  `json:"user_id"`
	RoleID   int64  `json:"role_id"`
	Username string `json:"username"`
}

func (s *AuthService) Login(ctx context.Context, req LoginRequest) (*LoginResponse, error) {
	user, err := s.userQuery.GetByUsername(ctx, req.Username)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("invalid credentials")
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password))
	if err != nil {
		return nil, errors.New("invalid credentials")
	}

	token, err := s.generateToken(user.ID, user.RoleID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	user.AuthTime = &now
	_, err = s.userQuery.UpdateAuthTime(ctx, user.ID)
	if err != nil {
		s.logger.Warn("Failed to update auth time", zap.Error(err))
	}

	return &LoginResponse{
		Token:    token,
		UserID:   user.ID,
		RoleID:   user.RoleID,
		Username: user.Username,
	}, nil
}

func (s *AuthService) generateToken(userID, roleID int64) (string, error) {
	claims := jwt.MapClaims{
		"user_id": userID,
		"role_id": roleID,
		"exp":     time.Now().Add(24 * time.Hour).Unix(),
		"iat":     time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.jwtSecret))
}

func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}
