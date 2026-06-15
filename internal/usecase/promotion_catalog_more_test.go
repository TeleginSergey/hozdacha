package usecase

import (
	"context"
	"testing"

	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/db"
)

func fptr(v float64) *float64 { return &v }

func TestFilterByPromotionKind(t *testing.T) {
	products := []*db.Product{
		{ID: 1, PromotionType: "product"},
		{ID: 2, PromotionType: "category"},
		nil,
	}

	if got := filterByPromotionKind(products, "all"); len(got) != 3 {
		t.Errorf("all: got %d, want 3 (pass-through)", len(got))
	}
	if got := filterByPromotionKind(products, ""); len(got) != 3 {
		t.Errorf("empty: got %d, want 3 (pass-through)", len(got))
	}
	if got := filterByPromotionKind(products, "Product"); len(got) != 1 || got[0].ID != 1 {
		t.Errorf("product: got %+v, want [1]", got)
	}
	if got := filterByPromotionKind(products, "category"); len(got) != 1 || got[0].ID != 2 {
		t.Errorf("category: got %+v, want [2]", got)
	}
}

func TestFilterProductsWithPromotion(t *testing.T) {
	products := []*db.Product{
		{ID: 1, Price: 100, EffectivePrice: fptr(80)},  // скидка
		{ID: 2, Price: 100, EffectivePrice: fptr(100)}, // нет скидки
		{ID: 3, Price: 100},                            // нет EffectivePrice
		nil,
	}
	got := filterProductsWithPromotion(products)
	if len(got) != 1 || got[0].ID != 1 {
		t.Errorf("got %+v, want only product 1", got)
	}
}

func TestBuildPromoSummaries_Kinds(t *testing.T) {
	links := newMockLinkQuery()
	links.productIDs = []int64{10}
	links.categoryIDs = []int64{5}
	// Оба набора непустые → kind=mixed для каждой акции.
	promos := []*db.Promotion{
		{ID: 1, Title: "A", Discount: 30},
		nil,
		{ID: 2, Title: "B", Discount: 10},
	}
	out := buildPromoSummaries(context.Background(), links, promos)
	if len(out) != 2 {
		t.Fatalf("got %d summaries, want 2", len(out))
	}
	// Отсортировано по убыванию скидки.
	if out[0].Discount < out[1].Discount {
		t.Errorf("summaries not sorted by discount desc: %+v", out)
	}
	if out[0].Kind != "mixed" {
		t.Errorf("kind = %q, want mixed", out[0].Kind)
	}
}

func TestBuildPromoSummaries_CategoryOnly(t *testing.T) {
	links := newMockLinkQuery()
	links.categoryIDs = []int64{5} // только категории → kind=category
	promos := []*db.Promotion{{ID: 1, Title: "Cat", Discount: 15}}
	out := buildPromoSummaries(context.Background(), links, promos)
	if len(out) != 1 || out[0].Kind != "category" {
		t.Fatalf("got %+v, want kind=category", out)
	}
}

func TestBuildCategoriesFromPromos(t *testing.T) {
	repo := newMockProductRepo()
	catID := int64(5)
	repo.add(&db.Product{ID: 10, Name: "Товар", Price: 100, Active: true, CategoryID: &catID})
	uc := newTestCatalogUC(&mockPromotionQuery{}, newMockLinkQuery(), repo)

	promos := []PromotionCatalogPromo{
		{ID: 1, Title: "Категорийная", Discount: 30, Kind: "category", CategoryIDs: []int64{5}},
		{ID: 2, Title: "Товарная", Discount: 20, Kind: "product", ProductIDs: []int64{10}},
	}
	cats := uc.buildCategoriesFromPromos(context.Background(), promos)
	if len(cats) != 1 {
		t.Fatalf("got %d categories, want 1 (both promos map to cat 5)", len(cats))
	}
	if len(cats[0].Promo) != 2 {
		t.Errorf("category should reference both promos, got %d", len(cats[0].Promo))
	}
	// Отсортировано по убыванию скидки.
	if cats[0].Promo[0].Discount < cats[0].Promo[1].Discount {
		t.Errorf("promo refs not sorted by discount desc: %+v", cats[0].Promo)
	}

	// Пустой вход → nil.
	if got := uc.buildCategoriesFromPromos(context.Background(), nil); got != nil {
		t.Errorf("empty promos must return nil, got %+v", got)
	}
}

func TestPromotionCatalog_CategoryPromotion(t *testing.T) {
	promos := &mockPromotionQuery{active: []*db.Promotion{
		{ID: 1, Title: "Скидка на категорию", Discount: 40, Kind: db.PromotionKindManual, Active: true},
	}}
	links := newMockLinkQuery()
	links.categoryIDs = []int64{5} // акция привязана к категории
	links.productIDs = []int64{10} // и кандидатом выступает товар 10
	links.categoryPromos[5] = &db.Promotion{ID: 1, Title: "Скидка на категорию", Discount: 40, Kind: db.PromotionKindManual, Active: true}

	repo := newMockProductRepo()
	catID := int64(5)
	repo.add(&db.Product{ID: 10, Name: "Товар в категории", Price: 100, Stock: 5, Active: true, CategoryID: &catID})

	// Pricer проставит акцию категории, иначе товар отфильтруется.
	uc := newTestCatalogUC(promos, links, repo)
	uc.productUC.SetPromotionPricer(NewPromotionPricer(links, nil, zap.NewNop()))

	res, err := uc.ListProducts(context.Background(), "", nil, "category", 24, 0)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(res.Products) != 1 {
		t.Fatalf("got %d promo products, want 1", len(res.Products))
	}
	if res.Products[0].PromotionType != "category" {
		t.Errorf("type = %q, want category", res.Products[0].PromotionType)
	}
	if res.Total != 1 {
		t.Errorf("total = %d, want 1", res.Total)
	}
}

func TestPromotionCatalog_Paging(t *testing.T) {
	promos := &mockPromotionQuery{active: []*db.Promotion{
		{ID: 1, Title: "Распродажа", Discount: 25, Kind: db.PromotionKindManual, Active: true},
	}}
	links := newMockLinkQuery()
	links.productIDs = []int64{10, 11}
	links.productPromos[10] = &db.Promotion{ID: 1, Discount: 25, Kind: db.PromotionKindManual, Active: true}
	links.productPromos[11] = &db.Promotion{ID: 1, Discount: 25, Kind: db.PromotionKindManual, Active: true}

	repo := newMockProductRepo()
	repo.add(&db.Product{ID: 10, Name: "A", Price: 100, Stock: 5, Active: true})
	repo.add(&db.Product{ID: 11, Name: "B", Price: 100, Stock: 5, Active: true})

	uc := newTestCatalogUC(promos, links, repo)
	uc.productUC.SetPromotionPricer(NewPromotionPricer(links, nil, zap.NewNop()))

	// offset за пределами total — не должно паниковать, страница пустая.
	res, err := uc.ListProducts(context.Background(), "", nil, "all", 24, 100)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(res.Products) != 0 {
		t.Errorf("offset beyond total: got %d products, want 0", len(res.Products))
	}
	if res.Total != 2 {
		t.Errorf("total = %d, want 2", res.Total)
	}

	// limit=1 → одна позиция на странице.
	res, err = uc.ListProducts(context.Background(), "", nil, "all", 1, 0)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(res.Products) != 1 {
		t.Errorf("limit=1: got %d products, want 1", len(res.Products))
	}
}

func TestPromotionPricer_Apply_CategoryLevel(t *testing.T) {
	links := newMockLinkQuery()
	links.categoryPromos[5] = &db.Promotion{ID: 2, Title: "−30% категория", Discount: 30, Kind: db.PromotionKindManual, Active: true}
	p := NewPromotionPricer(links, nil, zap.NewNop())

	catID := int64(5)
	products := []*db.Product{{ID: 10, Name: "Товар", Price: 200, CategoryID: &catID}}
	p.Apply(context.Background(), products)

	if products[0].EffectivePrice == nil {
		t.Fatal("category promo not applied")
	}
	if *products[0].EffectivePrice != 140 {
		t.Errorf("effective = %v, want 140", *products[0].EffectivePrice)
	}
	if products[0].PromotionType != "category" {
		t.Errorf("type = %q, want category", products[0].PromotionType)
	}
}
