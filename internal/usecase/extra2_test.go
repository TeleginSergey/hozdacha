package usecase

import (
	"context"
	"testing"

	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/db"
)

func TestNewProductUsecase_Constructor(t *testing.T) {
	uc := NewProductUsecase(newMockProductRepo(), nil, 0.05, zap.NewNop())
	if uc == nil {
		t.Fatal("constructor returned nil")
	}
	uc.SetPromotionPricer(NewPromotionPricer(newMockLinkQuery(), nil, zap.NewNop()))
}

func TestOrder_GetUserOrders_WithData(t *testing.T) {
	uid := int64(1)
	orders := &mockOrderRepo{ordersList: []*db.Order{
		{ID: 1, UserID: &uid, Status: "pending"},
		{ID: 2, UserID: &uid, Status: "completed"},
	}}
	uc := NewOrderUsecase(orders, newMockProductRepo(), nil, nil, &mockEventRepo{}, zap.NewNop())

	got, err := uc.GetUserOrders(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetUserOrders error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d orders, want 2", len(got))
	}
}

func TestPromotionPricer_CategoryLevel(t *testing.T) {
	links := newMockLinkQuery()
	links.categoryPromos[3] = &db.Promotion{ID: 2, Title: "Категорийная", Discount: 40, Kind: db.PromotionKindManual, Active: true}
	p := NewPromotionPricer(links, nil, zap.NewNop())

	cat := int64(3)
	products := []*db.Product{{ID: 10, Name: "Товар", Price: 200, CategoryID: &cat}}
	p.Apply(context.Background(), products)

	if products[0].EffectivePrice == nil {
		t.Fatal("category promo not applied")
	}
	if products[0].PromotionType != "category" {
		t.Errorf("type = %q, want category", products[0].PromotionType)
	}
	if *products[0].EffectivePrice != 120 {
		t.Errorf("effective = %v, want 120 (200-40%%)", *products[0].EffectivePrice)
	}
}

func TestCart_AddExistingItem(t *testing.T) {
	products := newMockProductRepo()
	products.add(&db.Product{ID: 10, Name: "T", Price: 100, Stock: 5, Active: true})
	cart := newMockCartRepo()
	uc := newTestCartUC(cart, products)

	// Первое добавление — Create, второе — ветка UpdateQuantity (товар уже в корзине).
	_ = uc.AddToCart(context.Background(), &AddToCartRequest{UserID: 1, ProductID: 10, Quantity: 1})
	if err := uc.AddToCart(context.Background(), &AddToCartRequest{UserID: 1, ProductID: 10, Quantity: 2}); err != nil {
		t.Errorf("AddToCart(existing) error: %v", err)
	}
}

func TestProductUC_SearchWithCategory(t *testing.T) {
	repo := newMockProductRepo()
	repo.add(&db.Product{ID: 10, Name: "Лопата", Price: 100, Stock: 5, Active: true})
	uc := &ProductUsecase{
		repo:        repo,
		stockCache:  &mockStockCache{avail: map[int64]int{10: 4}},
		stockBuffer: 0,
		logger:      zap.NewNop(),
	}
	cat := int64(3)
	if _, err := uc.SearchProducts(context.Background(), "лоп", &cat, 10, 0); err != nil {
		t.Errorf("SearchProducts error: %v", err)
	}
	if _, err := uc.GetSearchProductsCount(context.Background(), "лоп", &cat); err != nil {
		t.Errorf("GetSearchProductsCount error: %v", err)
	}
}
