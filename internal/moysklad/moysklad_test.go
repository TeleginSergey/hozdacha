package moysklad

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/resilience"
)

// Клиент API: разбор ответа «МойСклад» через эмулированный сервер (httptest).
func TestClient_GetProducts_ParsesRows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/entity/product" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rows":[{"name":"Лопата"},{"name":"Грабли"}],"meta":{"size":2,"nextHref":""}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-token", zap.NewNop(), 1000, 1)
	products, err := c.GetProducts(context.Background())
	if err != nil {
		t.Fatalf("GetProducts error: %v", err)
	}
	if len(products) != 2 {
		t.Fatalf("got %d products, want 2", len(products))
	}
}

// Поведение при недоступности «МойСклад» (HTTP 500): запрос завершается ошибкой,
// клиент не паникует.
func TestClient_GetProducts_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-token", zap.NewNop(), 1000, 0)
	if _, err := c.GetProducts(context.Background()); err == nil {
		t.Fatal("expected error on HTTP 500, got nil")
	}
}

// Отказоустойчивость: серия ошибок 500 от API «МойСклад» открывает circuit breaker,
// и дальнейшие вызовы отсекаются без обращения к внешнему сервису.
func TestMoyskladServerError_OpensCircuitBreaker(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Error(w, "internal", http.StatusInternalServerError)
	}))
	defer srv.Close()

	cb := resilience.NewCircuitBreaker(3, time.Minute)
	call := func() error {
		resp, err := http.Get(srv.URL)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 500 {
			return errPlaceholder
		}
		return nil
	}

	for i := 0; i < 3; i++ {
		_ = cb.Call(context.Background(), call)
	}
	hitsBefore := hits
	err := cb.Call(context.Background(), call)
	if err != resilience.ErrCircuitOpen {
		t.Fatalf("err = %v, want ErrCircuitOpen", err)
	}
	if hits != hitsBefore {
		t.Error("breaker is open but the external server was still called")
	}
}

var errPlaceholder = &serverError{}

type serverError struct{}

func (e *serverError) Error() string { return "moysklad 500" }
