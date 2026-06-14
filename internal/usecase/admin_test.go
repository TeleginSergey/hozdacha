package usecase

import (
	"context"
	"testing"

	"go.uber.org/zap"
)

// Управление заказами админом: смена статуса (отгрузка / отмена / истечение).

func adminUC(orders *mockOrderRepo, events *mockEventRepo) *OrderUsecase {
	return NewOrderUsecase(orders, newMockProductRepo(), nil, nil, events, zap.NewNop())
}

func TestAdmin_CompleteOrder(t *testing.T) {
	events := &mockEventRepo{}
	uc := adminUC(&mockOrderRepo{}, events)

	admin := int64(1)
	if err := uc.CompleteOrder(context.Background(), 42, &admin); err != nil {
		t.Fatalf("CompleteOrder error: %v", err)
	}
	if events.count == 0 {
		t.Error("expected an audit-log event (shipped) to be recorded")
	}
}

func TestAdmin_CancelOrderByAdmin(t *testing.T) {
	events := &mockEventRepo{}
	uc := adminUC(&mockOrderRepo{}, events)

	admin := int64(1)
	if err := uc.CancelOrderByAdmin(context.Background(), 42, &admin); err != nil {
		t.Fatalf("CancelOrderByAdmin error: %v", err)
	}
	if events.count == 0 {
		t.Error("expected a cancel event to be recorded")
	}
}

func TestAdmin_ExpireOrder(t *testing.T) {
	uc := adminUC(&mockOrderRepo{}, &mockEventRepo{})
	if err := uc.ExpireOrderByAdmin(context.Background(), 7, nil); err != nil {
		t.Fatalf("ExpireOrderByAdmin error: %v", err)
	}
}
