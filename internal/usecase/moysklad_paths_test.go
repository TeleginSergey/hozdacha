package usecase

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/db"
	"github.com/TeleginSergey/hozdacha/internal/moysklad"
)

// moyskladTestServer эмулирует минимальный набор эндпоинтов МойСклад,
// нужный для создания заказа (организация, контрагент, customerorder).
func moyskladTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/entity/organization"):
			_, _ = w.Write([]byte(`{"rows":[{"href":"http://x/entity/organization/org1"}],"meta":{}}`))
		case strings.HasPrefix(r.URL.Path, "/entity/counterparty"):
			_, _ = w.Write([]byte(`{"rows":[{"href":"http://x/entity/counterparty/ag1"}],"meta":{}}`))
		case strings.HasPrefix(r.URL.Path, "/entity/customerorder"):
			// POST (создание), PUT (переименование), DELETE — все отвечают объектом заказа.
			_, _ = w.Write([]byte(`{"id":"mo-123","name":"order"}`))
		default:
			_, _ = w.Write([]byte(`{"rows":[],"meta":{}}`))
		}
	}))
}

func msID(s string) *string { return &s }

// order.go: ветка создания заказа с реальным клиентом МойСклад.
func TestCreateOrder_WithMoysklad(t *testing.T) {
	srv := moyskladTestServer(t)
	defer srv.Close()
	client := moysklad.NewClient(srv.URL, "tok", zap.NewNop(), 1000, 0,
		moysklad.WithOrderDefaults("org1", "ag1"))

	products := newMockProductRepo()
	products.add(&db.Product{ID: 10, Name: "Товар", Price: 100, Stock: 5, Active: true, MoyskladID: msID("ms-10")})
	orders := &mockOrderRepo{stock: 5}

	uc := NewOrderUsecase(orders, products, nil, client, &mockEventRepo{}, zap.NewNop())

	order, err := uc.CreateOrder(context.Background(), CreateOrderRequest{
		UserID: 1, CustomerName: "Иван", Phone: "+79990000000",
		Items: []CreateOrderItem{{ProductID: 10, Quantity: 2}},
	})
	if err != nil {
		t.Fatalf("CreateOrder with moysklad error: %v", err)
	}
	if order == nil || order.MoyskladID == nil || *order.MoyskladID != "mo-123" {
		t.Errorf("moysklad id not attached: %+v", order)
	}
}

// order.go: МойСклад настроен не полностью (нет org/agent) → заказ не создаётся.
func TestCreateOrder_MoyskladNotConfigured(t *testing.T) {
	srv := moyskladTestServer(t)
	defer srv.Close()
	client := moysklad.NewClient(srv.URL, "tok", zap.NewNop(), 1000, 0) // без WithOrderDefaults

	products := newMockProductRepo()
	products.add(&db.Product{ID: 10, Name: "Товар", Price: 100, Stock: 5, Active: true, MoyskladID: msID("ms-10")})

	uc := NewOrderUsecase(&mockOrderRepo{stock: 5}, products, nil, client, &mockEventRepo{}, zap.NewNop())
	_, err := uc.CreateOrder(context.Background(), CreateOrderRequest{
		UserID: 1, CustomerName: "Иван", Phone: "+7",
		Items: []CreateOrderItem{{ProductID: 10, Quantity: 1}},
	})
	if err == nil {
		t.Fatal("expected 'not fully configured' error")
	}
}

// moysklad_orders.go: createMoyskladOrder через эмулированный сервер.
func TestMoyskladOrderUsecase_CreateOrder_HappyPath(t *testing.T) {
	srv := moyskladTestServer(t)
	defer srv.Close()
	client := moysklad.NewClient(srv.URL, "tok", zap.NewNop(), 1000, 0,
		moysklad.WithOrderDefaults("org1", "ag1"))

	cart := newMockCartRepo()
	_ = cart.Create(context.Background(), &db.CartItem{UserID: 1, ProductID: 10, Quantity: 2})
	products := newMockProductRepo()
	products.add(&db.Product{ID: 10, Name: "Товар", Price: 100, Stock: 5, Active: true, MoyskladID: msID("ms-10")})

	uc := NewMoyskladOrderUsecase(cart, products, &mockOrderRepo{}, client, zap.NewNop())
	resp, err := uc.CreateOrder(context.Background(), &CreateOrderRequest{UserID: 1})
	if err != nil {
		t.Fatalf("CreateOrder error: %v", err)
	}
	if resp == nil || resp.MoyskladID != "mo-123" {
		t.Errorf("unexpected response: %+v", resp)
	}
}
