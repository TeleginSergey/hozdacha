package services

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// --- Хеширование пароля (bcrypt) ---

func TestHashPassword_VerifiesCorrectly(t *testing.T) {
	hash, err := HashPassword("S3cret!Pass")
	if err != nil {
		t.Fatalf("hash error: %v", err)
	}
	if hash == "" || hash == "S3cret!Pass" {
		t.Fatal("password was not hashed")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("S3cret!Pass")); err != nil {
		t.Errorf("correct password rejected: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("wrong")); err == nil {
		t.Error("wrong password accepted")
	}
}

func TestHashPassword_SaltedUnique(t *testing.T) {
	h1, _ := HashPassword("samePassword")
	h2, _ := HashPassword("samePassword")
	if h1 == h2 {
		t.Error("two hashes of the same password are identical (no salt?)")
	}
}

// --- Валидация формы входа ---

func TestValidateLoginRequest(t *testing.T) {
	valid := []LoginRequest{
		{Username: "ivan_petrov", Password: "password1"},
		{Username: "user.name-99", Password: "x"},
	}
	for _, r := range valid {
		if err := ValidateLoginRequest(r); err != nil {
			t.Errorf("valid request rejected (%+v): %v", r, err)
		}
	}

	invalid := map[string]LoginRequest{
		"empty username":   {Username: "  ", Password: "p"},
		"empty password":   {Username: "user", Password: ""},
		"bad chars":        {Username: "ив@н", Password: "p"},
		"too long pass":    {Username: "user", Password: strings.Repeat("a", 129)},
	}
	for name, r := range invalid {
		if err := ValidateLoginRequest(r); err == nil {
			t.Errorf("%s: expected validation error, got nil", name)
		}
	}
}

// --- Валидация акции ---

func TestValidatePromotion(t *testing.T) {
	if err := ValidatePromotion("Скидка недели", nil, 30); err != nil {
		t.Errorf("valid promotion rejected: %v", err)
	}
	if err := ValidatePromotion("  ", nil, 10); err == nil {
		t.Error("empty title accepted")
	}
	if err := ValidatePromotion("Акция", nil, -5); err == nil {
		t.Error("negative discount accepted")
	}
	if err := ValidatePromotion("Акция", nil, 150); err == nil {
		t.Error("discount > 100 accepted")
	}
	long := strings.Repeat("я", 256)
	if err := ValidatePromotion(long, nil, 10); err == nil {
		t.Error("too-long title accepted")
	}
}
