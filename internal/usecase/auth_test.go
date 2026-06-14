package usecase

import (
	"strconv"
	"testing"
)

func TestNormalizeEmail(t *testing.T) {
	cases := map[string]string{
		"  User@Example.COM ": "user@example.com",
		"ABC@MAIL.RU":         "abc@mail.ru",
		"already@low.ru":      "already@low.ru",
		"\tСпейс@почта.рф\n":  "спейс@почта.рф",
	}
	for in, want := range cases {
		if got := normalizeEmail(in); got != want {
			t.Errorf("normalizeEmail(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGenerateVerificationCode_Format(t *testing.T) {
	uc := &UserUsecase{}
	for i := 0; i < 200; i++ {
		code := uc.GenerateVerificationCode()
		if len(code) != 6 {
			t.Fatalf("code %q length = %d, want 6", code, len(code))
		}
		n, err := strconv.Atoi(code)
		if err != nil {
			t.Fatalf("code %q is not numeric: %v", code, err)
		}
		if n < 100000 || n > 999999 {
			t.Fatalf("code %d out of [100000,999999]", n)
		}
	}
}

func TestGenerateVerificationCode_Randomness(t *testing.T) {
	uc := &UserUsecase{}
	seen := make(map[string]int)
	const n = 300
	for i := 0; i < n; i++ {
		seen[uc.GenerateVerificationCode()]++
	}
	// Криптослучайный генератор не должен выдавать одно и то же значение.
	if len(seen) < n/2 {
		t.Errorf("too many collisions: %d distinct codes out of %d", len(seen), n)
	}
}
