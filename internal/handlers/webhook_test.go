package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"go.uber.org/zap"
)

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// Обработка вебхуков: проверка HMAC-подписи запроса от «МойСклад».
func TestWebhook_VerifySignature(t *testing.T) {
	const secret = "super-secret"
	h := NewWebhookHandler(secret, nil, 10, zap.NewNop())
	body := []byte(`{"events":[{"meta":{"type":"product"}}]}`)

	if !h.verifySignature(body, sign(secret, body)) {
		t.Error("valid signature rejected")
	}
	if h.verifySignature(body, "deadbeef") {
		t.Error("invalid signature accepted")
	}
	if h.verifySignature(body, sign("wrong-secret", body)) {
		t.Error("signature with wrong secret accepted")
	}
}

// Fail-closed: без настроенного секрета подпись считается невалидной.
func TestWebhook_VerifySignature_NoSecret(t *testing.T) {
	h := NewWebhookHandler("", nil, 10, zap.NewNop())
	if h.verifySignature([]byte("{}"), "anything") {
		t.Error("verifySignature must return false when secret is not configured (fail-closed)")
	}
}
