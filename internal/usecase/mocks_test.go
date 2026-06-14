package usecase

import (
	"context"
	"fmt"
	"sync"

	"github.com/TeleginSergey/hozdacha/internal/db"
)

// =============================================================================
// Ручные моки репозиториев на основе интерфейсов (db.ProductQuery, OrderRepository,
// db.OrderEventQuery, db.CartItemQuery). Используются модульными тестами usecase-слоя.
// =============================================================================

// ---- mock db.ProductQuery -------------------------------------------------

type mockProductRepo struct {
	mu       sync.Mutex
	products map[int64]*db.Product
	getErr   error
}

func newMockProductRepo() *mockProductRepo {
	return &mockProductRepo{products: make(map[int64]*db.Product)}
}

func (m *mockProductRepo) add(p *db.Product) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.products[p.ID] = p
}

func (m *mockProductRepo) GetByID(ctx context.Context, id int64) (*db.Product, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.products[id]
	if !ok {
		return nil, nil
	}
	cp := *p
	return &cp, nil
}

func (m *mockProductRepo) GetByIDs(ctx context.Context, ids []int64) ([]*db.Product, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*db.Product, 0, len(ids))
	for _, id := range ids {
		if p, ok := m.products[id]; ok {
			cp := *p
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *mockProductRepo) GetByMoyskladID(ctx context.Context, moyskladID string) (*db.Product, error) {
	return nil, nil
}
func (m *mockProductRepo) GetAll(ctx context.Context, limit, offset int) ([]*db.Product, error) {
	return nil, nil
}
func (m *mockProductRepo) GetActive(ctx context.Context, limit, offset int) ([]*db.Product, error) {
	return nil, nil
}
func (m *mockProductRepo) Search(ctx context.Context, query string, categoryID *int64, limit, offset int) ([]*db.Product, error) {
	return nil, nil
}
func (m *mockProductRepo) GetByCategory(ctx context.Context, categoryID int64, limit, offset int) ([]*db.Product, error) {
	return nil, nil
}
func (m *mockProductRepo) CountActive(ctx context.Context) (int, error)               { return 0, nil }
func (m *mockProductRepo) CountByCategory(ctx context.Context, c int64) (int, error)  { return 0, nil }
func (m *mockProductRepo) CountSearch(ctx context.Context, q string, c *int64) (int, error) {
	return 0, nil
}
func (m *mockProductRepo) ListIDsByCategoryTrees(ctx context.Context, ids []int64) ([]int64, error) {
	return nil, nil
}
func (m *mockProductRepo) FilterPromotionalIDs(ctx context.Context, ids []int64, search string, categoryID *int64) ([]int64, error) {
	return nil, nil
}
func (m *mockProductRepo) ListCategoriesForProductIDs(ctx context.Context, ids []int64) ([]db.ProductCategoryRef, error) {
	return nil, nil
}
func (m *mockProductRepo) Insert(ctx context.Context, p *db.Product) (*db.Product, error) {
	return p, nil
}
func (m *mockProductRepo) Update(ctx context.Context, p *db.Product, id int64) (*db.Product, error) {
	return p, nil
}
func (m *mockProductRepo) Delete(ctx context.Context, id int64) error { return nil }

// ---- mock OrderRepository -------------------------------------------------

// mockOrderRepo эмулирует атомарное списание остатка под мьютексом —
// аналог UPDATE ... WHERE stock >= qty в транзакции PostgreSQL.
type mockOrderRepo struct {
	mu        sync.Mutex
	stock     int
	nextID    int64
	created   int
	failCAErr error // принудительная ошибка CreateOrderAtomic
}

func (m *mockOrderRepo) CreateOrderAtomic(ctx context.Context, order *db.Order, items []db.OrderItem, clearCartForUserID int64) (*db.Order, []*db.Product, error) {
	if m.failCAErr != nil {
		return nil, nil, m.failCAErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	need := 0
	for _, it := range items {
		need += it.Quantity
	}
	if m.stock < need {
		return nil, nil, fmt.Errorf("%w", db.ErrInsufficientStock)
	}
	m.stock -= need
	m.nextID++
	m.created++
	order.ID = m.nextID

	updated := make([]*db.Product, 0, len(items))
	for _, it := range items {
		updated = append(updated, &db.Product{ID: it.ProductID, Stock: m.stock})
	}
	return order, updated, nil
}

func (m *mockOrderRepo) InsertWithItems(ctx context.Context, order *db.Order, items []db.OrderItem) (*db.Order, error) {
	return order, nil
}
func (m *mockOrderRepo) CancelPendingOrderAtomic(ctx context.Context, orderID int64, targetStatus string, ownerUserID *int64) (*string, bool, []*db.Product, error) {
	return nil, false, nil, nil
}
func (m *mockOrderRepo) CompleteOrder(ctx context.Context, orderID int64) (bool, error) {
	// false = заказ был pending и только что отгружен (не идемпотентный повтор).
	return false, nil
}
func (m *mockOrderRepo) GetByID(ctx context.Context, id int64) (*db.Order, error)   { return nil, nil }
func (m *mockOrderRepo) Update(ctx context.Context, o *db.Order, id int64) (*db.Order, error) {
	return o, nil
}
func (m *mockOrderRepo) GetByUserID(ctx context.Context, userID int64) ([]*db.Order, error) {
	return nil, nil
}
func (m *mockOrderRepo) GetItemsByOrderID(ctx context.Context, orderID int64) ([]*db.OrderItem, error) {
	return nil, nil
}

// ---- mock db.OrderEventQuery ---------------------------------------------

type mockEventRepo struct{ count int }

func (m *mockEventRepo) Insert(ctx context.Context, orderID int64, eventType string, actorUserID *int64, payload any) error {
	m.count++
	return nil
}
func (m *mockEventRepo) ListByOrderID(ctx context.Context, orderID int64) ([]*db.OrderEvent, error) {
	return nil, nil
}

// ---- mock db.CartItemQuery -----------------------------------------------

type mockCartRepo struct {
	items   map[string]*db.CartItem // ключ "user:product"
	total   float64
	created int
	cleared bool
}

func newMockCartRepo() *mockCartRepo {
	return &mockCartRepo{items: make(map[string]*db.CartItem)}
}

func cartKey(u, p int64) string { return fmt.Sprintf("%d:%d", u, p) }

func (m *mockCartRepo) Create(ctx context.Context, item *db.CartItem) error {
	m.items[cartKey(item.UserID, item.ProductID)] = item
	m.created++
	return nil
}
func (m *mockCartRepo) GetByUserID(ctx context.Context, userID int64) ([]*db.CartItem, error) {
	out := []*db.CartItem{}
	for _, it := range m.items {
		if it.UserID == userID {
			out = append(out, it)
		}
	}
	return out, nil
}
func (m *mockCartRepo) GetByUserIDAndProductID(ctx context.Context, userID, productID int64) (*db.CartItem, error) {
	return m.items[cartKey(userID, productID)], nil
}
func (m *mockCartRepo) UpdateQuantity(ctx context.Context, userID, productID int64, quantity int) error {
	return nil
}
func (m *mockCartRepo) Delete(ctx context.Context, userID, productID int64) error {
	delete(m.items, cartKey(userID, productID))
	return nil
}
func (m *mockCartRepo) Clear(ctx context.Context, userID int64) error {
	m.cleared = true
	m.items = make(map[string]*db.CartItem)
	return nil
}
func (m *mockCartRepo) GetTotal(ctx context.Context, userID int64) (float64, error) {
	return m.total, nil
}
