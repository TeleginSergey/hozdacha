package usecase

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/db"
	"github.com/TeleginSergey/hozdacha/internal/moysklad"
)

func TestCreateOrder_PickupValidation(t *testing.T) {
	products := newMockProductRepo()
	products.add(&db.Product{ID: 10, Name: "T", Price: 100, Stock: 5, Active: true})
	uc := newTestOrderUC(&mockOrderRepo{stock: 5}, products)

	past := time.Now().Add(-time.Hour)
	if _, err := uc.CreateOrder(context.Background(), CreateOrderRequest{
		UserID: 1, CustomerName: "И", Phone: "+7", PickupAt: &past,
		Items: []CreateOrderItem{{ProductID: 10, Quantity: 1}},
	}); err == nil {
		t.Error("expected error: pickup in the past")
	}

	tooLate := time.Now().Add(100 * 24 * time.Hour)
	if _, err := uc.CreateOrder(context.Background(), CreateOrderRequest{
		UserID: 1, CustomerName: "И", Phone: "+7", PickupAt: &tooLate,
		Items: []CreateOrderItem{{ProductID: 10, Quantity: 1}},
	}); err == nil {
		t.Error("expected error: pickup after reservation expiry")
	}
}

func TestCreateOrder_InactiveProduct(t *testing.T) {
	products := newMockProductRepo()
	products.add(&db.Product{ID: 10, Name: "Снят", Price: 100, Stock: 5, Active: false})
	uc := newTestOrderUC(&mockOrderRepo{stock: 5}, products)
	if _, err := uc.CreateOrder(context.Background(), CreateOrderRequest{
		UserID: 1, CustomerName: "И", Phone: "+7",
		Items: []CreateOrderItem{{ProductID: 10, Quantity: 1}},
	}); err == nil {
		t.Error("expected error for inactive product")
	}
}

// Откат заказа в МойСклад, если локальное сохранение упало.
func TestCreateOrder_MoyskladRollbackOnDBError(t *testing.T) {
	srv := moyskladTestServer(t)
	defer srv.Close()
	client := moysklad.NewClient(srv.URL, "tok", zap.NewNop(), 1000, 0, moysklad.WithOrderDefaults("o", "a"))

	products := newMockProductRepo()
	products.add(&db.Product{ID: 10, Name: "T", Price: 100, Stock: 5, Active: true, MoyskladID: msID("ms-10")})
	orders := &mockOrderRepo{stock: 5, failCAErr: db.ErrInsufficientStock}

	uc := NewOrderUsecase(orders, products, nil, client, &mockEventRepo{}, zap.NewNop())
	if _, err := uc.CreateOrder(context.Background(), CreateOrderRequest{
		UserID: 1, CustomerName: "И", Phone: "+7",
		Items: []CreateOrderItem{{ProductID: 10, Quantity: 1}},
	}); err == nil {
		t.Error("expected error when DB save fails after moysklad create")
	}
}

// createMoyskladOrder возвращает ошибку, если в МойСклад нет организаций.
func TestMoyskladOrderUsecase_NoOrganizations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rows":[],"meta":{}}`)) // пусто для всех эндпоинтов
	}))
	defer srv.Close()
	client := moysklad.NewClient(srv.URL, "tok", zap.NewNop(), 1000, 0, moysklad.WithOrderDefaults("o", "a"))

	cart := newMockCartRepo()
	_ = cart.Create(context.Background(), &db.CartItem{UserID: 1, ProductID: 10, Quantity: 1})
	products := newMockProductRepo()
	products.add(&db.Product{ID: 10, Name: "T", Price: 100, Stock: 5, Active: true, MoyskladID: msID("ms-10")})

	uc := NewMoyskladOrderUsecase(cart, products, &mockOrderRepo{}, client, zap.NewNop())
	if _, err := uc.CreateOrder(context.Background(), &CreateOrderRequest{UserID: 1}); err == nil {
		t.Error("expected error when no organizations in moysklad")
	}
}
