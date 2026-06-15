package usecase

import (
	"context"
	"testing"

	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/db"
)

func newTestCatalogUC(promos *mockPromotionQuery, links *mockLinkQuery, repo *mockProductRepo) *PromotionCatalogUsecase {
	productUC := newTestProductUC(repo)
	return NewPromotionCatalogUsecase(promos, links, productUC, repo, nil, zap.NewNop())
}

func TestPromotionCatalog_NoPromotions(t *testing.T) {
	uc := newTestCatalogUC(&mockPromotionQuery{}, newMockLinkQuery(), newMockProductRepo())
	res, err := uc.ListProducts(context.Background(), "", nil, "all", 24, 0)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	if len(res.Promotions) != 0 {
		t.Errorf("expected no promotions, got %d", len(res.Promotions))
	}
}

func TestPromotionCatalog_WithActivePromotion(t *testing.T) {
	promos := &mockPromotionQuery{active: []*db.Promotion{
		{ID: 1, Title: "Распродажа", Discount: 25, Kind: db.PromotionKindManual, Active: true},
	}}
	links := newMockLinkQuery()
	links.productIDs = []int64{10}
	links.productPromos[10] = &db.Promotion{ID: 1, Title: "Распродажа", Discount: 25, Kind: db.PromotionKindManual, Active: true}

	repo := newMockProductRepo()
	repo.add(&db.Product{ID: 10, Name: "Товар по акции", Price: 100, Stock: 5, Active: true})

	uc := newTestCatalogUC(promos, links, repo)
	res, err := uc.ListProducts(context.Background(), "", nil, "all", 24, 0)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	// Сводка акций должна быть построена.
	if len(res.Promotions) == 0 {
		t.Error("expected promotion summaries to be built")
	}
}

func TestPromotionCatalog_NilDeps(t *testing.T) {
	// Без promotionQuery/linkQuery/productUC возвращается пустой результат без ошибки.
	uc := &PromotionCatalogUsecase{logger: zap.NewNop()}
	res, err := uc.ListProducts(context.Background(), "", nil, "all", 24, 0)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
}
