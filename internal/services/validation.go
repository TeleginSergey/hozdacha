package services

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/TeleginSergey/hozdacha/internal/middleware"
)

// ValidateOrderRequest валидирует запрос на создание заказа
func ValidateOrderRequest(req CreateOrderRequest) error {
	// Валидация имени
	req.CustomerName = strings.TrimSpace(req.CustomerName)
	if len(req.CustomerName) == 0 {
		return fmt.Errorf("customer name is required")
	}
	if utf8.RuneCountInString(req.CustomerName) > 255 {
		return fmt.Errorf("customer name is too long (max 255 characters)")
	}
	req.CustomerName = middleware.SanitizeString(req.CustomerName, 255)

	// Валидация телефона
	req.Phone = strings.TrimSpace(req.Phone)
	if len(req.Phone) == 0 {
		return fmt.Errorf("phone is required")
	}
	if !middleware.ValidatePhone(req.Phone) {
		return fmt.Errorf("invalid phone format")
	}

	// Валидация адреса (опционально)
	if req.Address != nil {
		*req.Address = strings.TrimSpace(*req.Address)
		if len(*req.Address) > 0 {
			*req.Address = middleware.SanitizeString(*req.Address, 500)
		}
	}

	// Валидация комментария (опционально)
	if req.Comment != nil {
		*req.Comment = strings.TrimSpace(*req.Comment)
		if len(*req.Comment) > 0 {
			*req.Comment = middleware.SanitizeString(*req.Comment, 1000)
		}
	}

	// Валидация товаров
	if len(req.Items) == 0 {
		return fmt.Errorf("at least one item is required")
	}
	if len(req.Items) > 100 {
		return fmt.Errorf("too many items (max 100)")
	}

	for i, item := range req.Items {
		if item.ProductID <= 0 {
			return fmt.Errorf("invalid product_id in item %d", i+1)
		}
		if item.Quantity <= 0 {
			return fmt.Errorf("invalid quantity in item %d", i+1)
		}
		if item.Quantity > 1000 {
			return fmt.Errorf("quantity too large in item %d (max 1000)", i+1)
		}
	}

	return nil
}

// ValidateLoginRequest валидирует запрос на вход
func ValidateLoginRequest(req LoginRequest) error {
	req.Username = strings.TrimSpace(req.Username)
	if len(req.Username) == 0 {
		return fmt.Errorf("username is required")
	}
	if utf8.RuneCountInString(req.Username) > 100 {
		return fmt.Errorf("username is too long")
	}

	// Проверка на опасные символы в username
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9._-]+$`, req.Username)
	if !matched {
		return fmt.Errorf("username contains invalid characters")
	}

	if len(req.Password) == 0 {
		return fmt.Errorf("password is required")
	}
	if len(req.Password) > 128 {
		return fmt.Errorf("password is too long")
	}

	return nil
}

// ValidatePromotion валидирует данные акции
func ValidatePromotion(title string, description *string, discount float64) error {
	title = strings.TrimSpace(title)
	if len(title) == 0 {
		return fmt.Errorf("title is required")
	}
	if utf8.RuneCountInString(title) > 255 {
		return fmt.Errorf("title is too long (max 255 characters)")
	}

	if description != nil {
		*description = strings.TrimSpace(*description)
		if utf8.RuneCountInString(*description) > 2000 {
			return fmt.Errorf("description is too long (max 2000 characters)")
		}
	}

	if discount < 0 || discount > 100 {
		return fmt.Errorf("discount must be between 0 and 100")
	}

	return nil
}
