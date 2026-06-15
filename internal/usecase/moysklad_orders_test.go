package usecase

import (
	"context"
	"testing"

	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/db"
)

func newMoyskladOrderUC(cart *mockCartRepo, products *mockProductRepo, orders *mockOrderRepo) *MoyskladOrderUsecase {
	// moyskladClient=nil — тестируем ветки, которые завершаются до обращения к МойСклад.
	return NewMoyskladOrderUsecase(cart, products, orders, nil, zap.NewNop())
}

func TestMoyskladOrder_EmptyCart(t *testing.T) {
	uc := newMoyskladOrderUC(newMockCartRepo(), newMockProductRepo(), &mockOrderRepo{})
	if _, err := uc.CreateOrder(context.Background(), &CreateOrderRequest{UserID: 1}); err == nil {
		t.Fatal("expected empty-cart error")
	}
}

func TestMoyskladOrder_ProductNotFound(t *testing.T) {
	cart := newMockCartRepo()
	_ = cart.Create(context.Background(), &db.CartItem{UserID: 1, ProductID: 77, Quantity: 1})
	uc := newMoyskladOrderUC(cart, newMockProductRepo(), &mockOrderRepo{})
	if _, err := uc.CreateOrder(context.Background(), &CreateOrderRequest{UserID: 1}); err == nil {
		t.Fatal("expected product-not-found error")
	}
}

func TestMoyskladOrder_InsufficientStock(t *testing.T) {
	cart := newMockCartRepo()
	_ = cart.Create(context.Background(), &db.CartItem{UserID: 1, ProductID: 10, Quantity: 5})
	products := newMockProductRepo()
	products.add(&db.Product{ID: 10, Name: "Мало", Price: 100, Stock: 1, Active: true})
	uc := newMoyskladOrderUC(cart, products, &mockOrderRepo{})
	if _, err := uc.CreateOrder(context.Background(), &CreateOrderRequest{UserID: 1}); err == nil {
		t.Fatal("expected insufficient-stock error")
	}
}

func TestMoyskladOrder_GetOrderStatus(t *testing.T) {
	owner := int64(1)
	orders := &mockOrderRepo{orderByID: &db.Order{ID: 5, UserID: &owner, Status: "pending"}}
	uc := newMoyskladOrderUC(newMockCartRepo(), newMockProductRepo(), orders)

	o, err := uc.GetOrderStatus(context.Background(), 1, 5)
	if err != nil {
		t.Fatalf("GetOrderStatus error: %v", err)
	}
	if o.ID != 5 {
		t.Errorf("wrong order: %+v", o)
	}
	// Чужой заказ — доступ запрещён.
	if _, err := uc.GetOrderStatus(context.Background(), 999, 5); err == nil {
		t.Error("expected access-denied error")
	}
}
