package usecase

import (
	"context"
	"testing"

	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/db"
)

func newTestCartUC(cart *mockCartRepo, products *mockProductRepo) *CartUsecase {
	return NewCartUsecase(cart, products, zap.NewNop())
}

func TestAddToCart_NewItem(t *testing.T) {
	products := newMockProductRepo()
	products.add(sampleProduct(10, 5))
	cart := newMockCartRepo()
	uc := newTestCartUC(cart, products)

	err := uc.AddToCart(context.Background(), &AddToCartRequest{UserID: 1, ProductID: 10, Quantity: 2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cart.created != 1 {
		t.Errorf("created items = %d, want 1", cart.created)
	}
	if it := cart.items[cartKey(1, 10)]; it == nil || it.Quantity != 2 {
		t.Errorf("cart item wrong: %+v", it)
	}
}

func TestAddToCart_OutOfStock(t *testing.T) {
	products := newMockProductRepo()
	products.add(&db.Product{ID: 11, Name: "Нет в наличии", Price: 50, Stock: 0, Active: true})
	uc := newTestCartUC(newMockCartRepo(), products)

	err := uc.AddToCart(context.Background(), &AddToCartRequest{UserID: 1, ProductID: 11, Quantity: 1})
	if err == nil {
		t.Fatal("expected out-of-stock error")
	}
}

func TestAddToCart_InactiveProduct(t *testing.T) {
	products := newMockProductRepo()
	products.add(&db.Product{ID: 12, Name: "Снят", Price: 50, Stock: 3, Active: false})
	uc := newTestCartUC(newMockCartRepo(), products)

	if err := uc.AddToCart(context.Background(), &AddToCartRequest{UserID: 1, ProductID: 12, Quantity: 1}); err == nil {
		t.Fatal("expected error for inactive product")
	}
}

func TestAddToCart_ProductNotFound(t *testing.T) {
	uc := newTestCartUC(newMockCartRepo(), newMockProductRepo())
	if err := uc.AddToCart(context.Background(), &AddToCartRequest{UserID: 1, ProductID: 999, Quantity: 1}); err == nil {
		t.Fatal("expected error for missing product")
	}
}

func TestUpdateCart_ItemNotInCart(t *testing.T) {
	products := newMockProductRepo()
	products.add(sampleProduct(10, 5))
	uc := newTestCartUC(newMockCartRepo(), products)

	err := uc.UpdateCart(context.Background(), &UpdateCartRequest{UserID: 1, ProductID: 10, Quantity: 3})
	if err == nil {
		t.Fatal("expected error: item not found in cart")
	}
}

func TestRemoveAndClearCart(t *testing.T) {
	products := newMockProductRepo()
	products.add(sampleProduct(10, 5))
	cart := newMockCartRepo()
	uc := newTestCartUC(cart, products)

	_ = uc.AddToCart(context.Background(), &AddToCartRequest{UserID: 1, ProductID: 10, Quantity: 1})
	if err := uc.RemoveFromCart(context.Background(), 1, 10); err != nil {
		t.Fatalf("remove error: %v", err)
	}
	if _, ok := cart.items[cartKey(1, 10)]; ok {
		t.Error("item still present after remove")
	}

	_ = uc.AddToCart(context.Background(), &AddToCartRequest{UserID: 1, ProductID: 10, Quantity: 1})
	if err := uc.ClearCart(context.Background(), 1); err != nil {
		t.Fatalf("clear error: %v", err)
	}
	if !cart.cleared {
		t.Error("cart not cleared")
	}
}

func TestGetCartTotal(t *testing.T) {
	cart := newMockCartRepo()
	cart.total = 350.5
	uc := newTestCartUC(cart, newMockProductRepo())

	total, err := uc.GetCartTotal(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 350.5 {
		t.Errorf("total = %v, want 350.5", total)
	}
}
