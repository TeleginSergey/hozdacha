package usecase

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/db"
	"github.com/TeleginSergey/hozdacha/internal/services"
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

func TestPromotionPricer_DayPromotionDeadline(t *testing.T) {
	links := newMockLinkQuery()
	now := time.Now()
	// Дневная акция, заведённая «сегодня» → актуальна для текущего окна брони.
	links.productPromos[10] = &db.Promotion{
		ID: 1, Discount: 20, Kind: db.PromotionKindDay, Active: true, ValidFrom: &now,
	}
	p := NewPromotionPricer(links, nil, zap.NewNop())

	hasDay, deadline, err := p.DayPromotionDeadline(context.Background(), []int64{10}, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasDay {
		t.Fatal("expected a day promotion to be detected")
	}
	if want := services.ReservationDeadline(now); !deadline.Equal(want) {
		t.Errorf("deadline = %v, want %v", deadline, want)
	}

	// Обычная (manual) акция не считается дневной.
	manualLinks := newMockLinkQuery()
	manualLinks.productPromos[11] = &db.Promotion{ID: 2, Discount: 10, Kind: db.PromotionKindManual, Active: true}
	pm := NewPromotionPricer(manualLinks, nil, zap.NewNop())
	if h, _, _ := pm.DayPromotionDeadline(context.Background(), []int64{11}, now); h {
		t.Error("manual promo must not be treated as day promo")
	}

	// Пустой список товаров.
	if h, _, _ := p.DayPromotionDeadline(context.Background(), nil, now); h {
		t.Error("empty product list must yield no day promo")
	}
}

func TestPromotionPricer_InvalidateCategoryCache(t *testing.T) {
	p := NewPromotionPricer(newMockLinkQuery(), nil, zap.NewNop())
	p.InvalidateCategoryCache() // не должно паниковать
}
