package usecase

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/db"
	"github.com/TeleginSergey/hozdacha/internal/moysklad"
)

// --- cart: ветки ошибок и успешного обновления ---

func TestCart_ErrorBranches(t *testing.T) {
	products := newMockProductRepo()
	products.add(&db.Product{ID: 10, Name: "T", Price: 100, Stock: 5, Active: true})
	cart := newMockCartRepo()
	cart.err = errors.New("db down")
	uc := newTestCartUC(cart, products)

	if err := uc.ClearCart(context.Background(), 1); err == nil {
		t.Error("ClearCart should propagate error")
	}
	if err := uc.RemoveFromCart(context.Background(), 1, 10); err == nil {
		t.Error("RemoveFromCart should propagate error")
	}
	if _, err := uc.GetCartTotal(context.Background(), 1); err == nil {
		t.Error("GetCartTotal should propagate error")
	}
}

func TestCart_UpdateSuccess(t *testing.T) {
	products := newMockProductRepo()
	products.add(&db.Product{ID: 10, Name: "T", Price: 100, Stock: 5, Active: true})
	cart := newMockCartRepo()
	_ = cart.Create(context.Background(), &db.CartItem{UserID: 1, ProductID: 10, Quantity: 1})
	uc := newTestCartUC(cart, products)

	if err := uc.UpdateCart(context.Background(), &UpdateCartRequest{UserID: 1, ProductID: 10, Quantity: 4}); err != nil {
		t.Errorf("UpdateCart should succeed: %v", err)
	}
}

// --- product: буфер стока и доступные остатки из кэша ---

func TestProductUC_StockBufferAndAvail(t *testing.T) {
	repo := newMockProductRepo()
	repo.add(&db.Product{ID: 10, Name: "A", Price: 100, Stock: 10, Active: true})
	uc := &ProductUsecase{
		repo:        repo,
		stockCache:  &mockStockCache{avail: map[int64]int{10: 8}},
		stockBuffer: 0.1,
		logger:      zap.NewNop(),
	}

	if _, err := uc.GetCatalogProducts(context.Background(), 10, 0, nil); err != nil {
		t.Errorf("GetCatalogProducts: %v", err)
	}
	got, err := uc.GetProductsByIDs(context.Background(), []int64{10})
	if err != nil {
		t.Fatalf("GetProductsByIDs: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 product, got %d", len(got))
	}
	p, _ := uc.GetProductByID(context.Background(), 10)
	if p == nil || p.Stock != 8 {
		t.Errorf("expected stock from cache (8), got %+v", p)
	}
}

// --- promotion_catalog: вспомогательные функции и фильтр по виду ---

func TestPromotionCatalog_Helpers(t *testing.T) {
	disc := 30.0
	if d := discountOf(&db.Product{DiscountPercent: &disc}); d != 30 {
		t.Errorf("discountOf = %v, want 30", d)
	}
	if d := discountOf(&db.Product{}); d != 0 {
		t.Errorf("discountOf(no promo) = %v, want 0", d)
	}
	ids := idsOf([]*db.Product{{ID: 5}, {ID: 7}})
	if len(ids) != 2 || ids[0] != 5 || ids[1] != 7 {
		t.Errorf("idsOf wrong: %v", ids)
	}
}

func TestPromotionCatalog_KindFilter(t *testing.T) {
	promos := &mockPromotionQuery{active: []*db.Promotion{
		{ID: 1, Title: "На товары", Discount: 20, Kind: db.PromotionKindManual, Active: true},
	}}
	links := newMockLinkQuery()
	links.productIDs = []int64{10}
	links.productPromos[10] = &db.Promotion{ID: 1, Discount: 20, Kind: db.PromotionKindManual, Active: true}
	repo := newMockProductRepo()
	repo.add(&db.Product{ID: 10, Name: "Товар", Price: 100, Stock: 5, Active: true})

	uc := newTestCatalogUC(promos, links, repo)
	if _, err := uc.ListProducts(context.Background(), "", nil, "product", 24, 0); err != nil {
		t.Errorf("ListProducts(kind=product): %v", err)
	}
	cat := int64(3)
	if _, err := uc.ListProducts(context.Background(), "лоп", &cat, "all", 24, 0); err != nil {
		t.Errorf("ListProducts(search+category): %v", err)
	}
}

// --- order: отмена с удалением в МойСклад + идемпотентные повторы ---

func TestOrder_CancelWithMoysklad(t *testing.T) {
	srv := moyskladTestServer(t)
	defer srv.Close()
	client := moysklad.NewClient(srv.URL, "tok", zap.NewNop(), 1000, 0, moysklad.WithOrderDefaults("o", "a"))

	orders := &mockOrderRepo{cancelMoyskladID: msID("mo-9")}
	uc := NewOrderUsecase(orders, newMockProductRepo(), nil, client, &mockEventRepo{}, zap.NewNop())

	if err := uc.CancelOrderByAdmin(context.Background(), 9, nil); err != nil {
		t.Errorf("cancel with moysklad delete: %v", err)
	}
}

func TestOrder_IdempotentCompleteAndCancel(t *testing.T) {
	// Заказ уже не pending → операции идемпотентны (ошибки нет, событие не пишется повторно).
	orders := &mockOrderRepo{completeAlready: true, cancelAlready: true}
	uc := NewOrderUsecase(orders, newMockProductRepo(), nil, nil, &mockEventRepo{}, zap.NewNop())

	if err := uc.CompleteOrder(context.Background(), 1, nil); err != nil {
		t.Errorf("idempotent complete: %v", err)
	}
	if err := uc.ExpireOrderByAdmin(context.Background(), 1, nil); err != nil {
		t.Errorf("idempotent expire: %v", err)
	}
}
