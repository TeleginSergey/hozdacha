package usecase

import (
	"context"
	"testing"

	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/db"
)

func newTestProductUC(repo *mockProductRepo) *ProductUsecase {
	// Конструируем напрямую (тот же пакет), подставляя мок-кэш в inline-поле.
	return &ProductUsecase{
		repo:        repo,
		stockCache:  &mockStockCache{},
		stockBuffer: 0,
		logger:      zap.NewNop(),
	}
}

func TestProductUC_GetProductByID(t *testing.T) {
	repo := newMockProductRepo()
	repo.add(&db.Product{ID: 10, Name: "Лопата", Price: 100, Stock: 7, Active: true})
	uc := newTestProductUC(repo)

	p, err := uc.GetProductByID(context.Background(), 10)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if p == nil || p.ID != 10 {
		t.Fatalf("wrong product: %+v", p)
	}

	// Несуществующий товар.
	p, err = uc.GetProductByID(context.Background(), 999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p != nil {
		t.Error("expected nil for missing product")
	}
}

func TestProductUC_GetProductsByIDs(t *testing.T) {
	repo := newMockProductRepo()
	repo.add(&db.Product{ID: 1, Name: "A", Price: 10, Stock: 5, Active: true})
	repo.add(&db.Product{ID: 2, Name: "B", Price: 20, Stock: 5, Active: true})
	uc := newTestProductUC(repo)

	got, err := uc.GetProductsByIDs(context.Background(), []int64{2, 1})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d products, want 2", len(got))
	}
	if got[0].ID != 2 || got[1].ID != 1 {
		t.Errorf("order not preserved: %d, %d", got[0].ID, got[1].ID)
	}

	if empty, _ := uc.GetProductsByIDs(context.Background(), nil); len(empty) != 0 {
		t.Error("empty ids must return empty slice")
	}
}

func TestProductUC_CatalogAndSearch(t *testing.T) {
	repo := newMockProductRepo()
	uc := newTestProductUC(repo)

	if _, err := uc.GetCatalogProducts(context.Background(), 10, 0, nil); err != nil {
		t.Errorf("GetCatalogProducts error: %v", err)
	}
	cat := int64(3)
	if _, err := uc.GetCatalogProducts(context.Background(), 10, 0, &cat); err != nil {
		t.Errorf("GetCatalogProducts(category) error: %v", err)
	}
	if _, err := uc.SearchProducts(context.Background(), "лоп", nil, 10, 0); err != nil {
		t.Errorf("SearchProducts error: %v", err)
	}
	if _, err := uc.GetActiveProductsCount(context.Background(), nil); err != nil {
		t.Errorf("GetActiveProductsCount error: %v", err)
	}
	if _, err := uc.GetSearchProductsCount(context.Background(), "x", nil); err != nil {
		t.Errorf("GetSearchProductsCount error: %v", err)
	}
}

func TestProductUC_WithPricer(t *testing.T) {
	repo := newMockProductRepo()
	repo.add(&db.Product{ID: 10, Name: "Скидочный", Price: 200, Stock: 5, Active: true})
	uc := newTestProductUC(repo)

	links := newMockLinkQuery()
	links.productPromos[10] = &db.Promotion{ID: 1, Discount: 50, Kind: db.PromotionKindManual, Active: true}
	uc.SetPromotionPricer(NewPromotionPricer(links, nil, zap.NewNop()))

	p, err := uc.GetProductByID(context.Background(), 10)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if p.EffectivePrice == nil || *p.EffectivePrice != 100 {
		t.Errorf("promo not applied via product usecase: %+v", p.EffectivePrice)
	}
}
