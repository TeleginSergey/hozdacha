package usecase

import (
	"context"
	"testing"

	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/db"
)

func TestOrder_CancelByUserAndGetOrders(t *testing.T) {
	orders := &mockOrderRepo{}
	events := &mockEventRepo{}
	uc := NewOrderUsecase(orders, newMockProductRepo(), nil, nil, events, zap.NewNop())

	if err := uc.CancelOrderByUser(context.Background(), 1, 42); err != nil {
		t.Fatalf("CancelOrderByUser error: %v", err)
	}
	if _, err := uc.GetUserOrders(context.Background(), 1); err != nil {
		t.Fatalf("GetUserOrders error: %v", err)
	}
	uc.SetPromotionPricer(NewPromotionPricer(newMockLinkQuery(), nil, zap.NewNop()))
}

func TestCartUC_GetCart(t *testing.T) {
	products := newMockProductRepo()
	products.add(&db.Product{ID: 10, Name: "Товар", Price: 100, Stock: 5, Active: true})
	cart := newMockCartRepo()
	uc := newTestCartUC(cart, products)

	_ = uc.AddToCart(context.Background(), &AddToCartRequest{UserID: 1, ProductID: 10, Quantity: 2})

	items, err := uc.GetCart(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetCart error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d cart items, want 1", len(items))
	}
	if items[0].Product == nil || items[0].Product.ID != 10 {
		t.Errorf("product not attached to cart item: %+v", items[0])
	}
	if items[0].Quantity != 2 {
		t.Errorf("quantity = %d, want 2", items[0].Quantity)
	}
}
