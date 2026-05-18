package services

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ValidateLoginRequest валидирует запрос на вход.
func ValidateLoginRequest(req LoginRequest) error {
	req.Username = strings.TrimSpace(req.Username)
	if len(req.Username) == 0 {
		return fmt.Errorf("username is required")
	}
	if utf8.RuneCountInString(req.Username) > 100 {
		return fmt.Errorf("username is too long")
	}
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

// ValidatePromotion валидирует данные акции.
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
