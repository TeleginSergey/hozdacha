package usecase

import (
	"context"
	"time"
	"testing"

	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/db"
)

func TestPromotionPricer_Apply_ProductLevel(t *testing.T) {
	links := newMockLinkQuery()
	links.productPromos[10] = &db.Promotion{ID: 1, Title: "−20%", Discount: 20, Kind: db.PromotionKindManual, Active: true}
	p := NewPromotionPricer(links, nil, zap.NewNop())

	products := []*db.Product{{ID: 10, Name: "Товар", Price: 100}}
	p.Apply(context.Background(), products)

	if products[0].EffectivePrice == nil {
		t.Fatal("effective price not set")
	}
	if *products[0].EffectivePrice != 80 {
		t.Errorf("effective = %v, want 80", *products[0].EffectivePrice)
	}
	if products[0].PromotionType != "product" {
		t.Errorf("type = %q, want product", products[0].PromotionType)
	}
}

func TestPromotionPricer_Apply_NoPromo(t *testing.T) {
	p := NewPromotionPricer(newMockLinkQuery(), nil, zap.NewNop())
	products := []*db.Product{{ID: 1, Price: 100}}
	p.Apply(context.Background(), products)
	if products[0].EffectivePrice != nil {
		t.Error("no promo expected, but effective price set")
	}
}

func TestPromotionPricer_Apply_Empty(t *testing.T) {
	p := NewPromotionPricer(newMockLinkQuery(), nil, zap.NewNop())
	p.Apply(context.Background(), nil) // не должно паниковать
}

func TestPromotionPricer_CheckDayPromotionWindow(t *testing.T) {
	links := newMockLinkQuery()
	p := NewPromotionPricer(links, nil, zap.NewNop())

	// Нет акций → окно не нарушено.
	if err := p.CheckDayPromotionWindow(context.Background(), []int64{10}, time.Now()); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	// Не-дневная акция → не блокируем.
	links.productPromos[10] = &db.Promotion{ID: 1, Discount: 10, Kind: db.PromotionKindManual, Active: true}
	if err := p.CheckDayPromotionWindow(context.Background(), []int64{10}, time.Now()); err != nil {
		t.Errorf("manual promo must not block: %v", err)
	}
	// Пустой список.
	if err := p.CheckDayPromotionWindow(context.Background(), nil, time.Now()); err != nil {
		t.Errorf("empty list must be ok: %v", err)
	}
}

func TestPromotionPricer_InvalidateCategoryCache(t *testing.T) {
	p := NewPromotionPricer(newMockLinkQuery(), nil, zap.NewNop())
	p.InvalidateCategoryCache() // не должно паниковать
}
