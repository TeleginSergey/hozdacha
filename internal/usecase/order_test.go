package usecase

import (
	"context"
	"errors"
	"sync"
	"testing"

	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/db"
)

func newTestOrderUC(orders *mockOrderRepo, products *mockProductRepo) *OrderUsecase {
	// moysklad=nil и stockCache=nil — изолируем бизнес-логику usecase.
	return NewOrderUsecase(orders, products, nil, nil, &mockEventRepo{}, zap.NewNop())
}

func sampleProduct(id int64, stock int) *db.Product {
	return &db.Product{ID: id, Name: "Тестовый товар", Price: 100, Stock: stock, Active: true}
}

func sampleOrderReq(productID int64, qty int) CreateOrderRequest {
	return CreateOrderRequest{
		UserID:       1,
		CustomerName: "Иван",
		Phone:        "+79990000000",
		Items:        []CreateOrderItem{{ProductID: productID, Quantity: qty}},
	}
}

func TestCreateOrder_Success(t *testing.T) {
	products := newMockProductRepo()
	products.add(sampleProduct(10, 5))
	orders := &mockOrderRepo{stock: 5}
	uc := newTestOrderUC(orders, products)

	order, err := uc.CreateOrder(context.Background(), sampleOrderReq(10, 2))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if order == nil || order.ID == 0 {
		t.Fatalf("order not created: %+v", order)
	}
	if order.Status != "pending" {
		t.Errorf("status = %q, want pending", order.Status)
	}
	if order.TotalPrice != 200 {
		t.Errorf("total = %v, want 200 (100*2)", order.TotalPrice)
	}
	if orders.stock != 3 {
		t.Errorf("stock after order = %d, want 3", orders.stock)
	}
}

func TestCreateOrder_InsufficientStock(t *testing.T) {
	products := newMockProductRepo()
	products.add(sampleProduct(10, 1))
	orders := &mockOrderRepo{stock: 1}
	uc := newTestOrderUC(orders, products)

	_, err := uc.CreateOrder(context.Background(), sampleOrderReq(10, 5))
	if err == nil {
		t.Fatal("expected insufficient stock error, got nil")
	}
	if !errors.Is(err, db.ErrInsufficientStock) {
		t.Errorf("error = %v, want wrapping ErrInsufficientStock", err)
	}
}

func TestCreateOrder_ValidationErrors(t *testing.T) {
	products := newMockProductRepo()
	products.add(sampleProduct(10, 5))
	uc := newTestOrderUC(&mockOrderRepo{stock: 5}, products)

	cases := map[string]CreateOrderRequest{
		"no name":  {UserID: 1, Phone: "+7", Items: []CreateOrderItem{{ProductID: 10, Quantity: 1}}},
		"no phone": {UserID: 1, CustomerName: "Иван", Items: []CreateOrderItem{{ProductID: 10, Quantity: 1}}},
		"no items": {UserID: 1, CustomerName: "Иван", Phone: "+7"},
		"bad qty":  {UserID: 1, CustomerName: "Иван", Phone: "+7", Items: []CreateOrderItem{{ProductID: 10, Quantity: 0}}},
	}
	for name, req := range cases {
		if _, err := uc.CreateOrder(context.Background(), req); err == nil {
			t.Errorf("%s: expected validation error, got nil", name)
		}
	}
}

func TestCreateOrder_ProductNotFound(t *testing.T) {
	uc := newTestOrderUC(&mockOrderRepo{stock: 5}, newMockProductRepo())
	if _, err := uc.CreateOrder(context.Background(), sampleOrderReq(999, 1)); err == nil {
		t.Fatal("expected error for missing product")
	}
}

// Нагрузочный/конкурентный тест: 50 параллельных заказов на один товар с остатком 1.
// Подтверждает отсутствие гонки — создаётся ровно один заказ, остальные получают
// «недостаточно товара» (эмуляция атомарного UPDATE ... WHERE stock >= qty).
func TestCreateOrder_Concurrent50_OnlyOneSucceeds(t *testing.T) {
	const goroutines = 50

	products := newMockProductRepo()
	products.add(sampleProduct(10, 1))
	orders := &mockOrderRepo{stock: 1}
	uc := newTestOrderUC(orders, products)

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		success  int
		insuffic int
	)
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_, err := uc.CreateOrder(context.Background(), sampleOrderReq(10, 1))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				success++
			case errors.Is(err, db.ErrInsufficientStock):
				insuffic++
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	if success != 1 {
		t.Errorf("successful orders = %d, want exactly 1", success)
	}
	if insuffic != goroutines-1 {
		t.Errorf("insufficient-stock responses = %d, want %d", insuffic, goroutines-1)
	}
	if orders.created != 1 {
		t.Errorf("orders created in repo = %d, want 1", orders.created)
	}
	if orders.stock != 0 {
		t.Errorf("final stock = %d, want 0", orders.stock)
	}
}
