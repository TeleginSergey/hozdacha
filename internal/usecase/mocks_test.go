package usecase

import (
	"context"
	"fmt"
	"sync"
	"time"

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

func (m *mockProductRepo) all() []*db.Product {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*db.Product, 0, len(m.products))
	for _, p := range m.products {
		cp := *p
		out = append(out, &cp)
	}
	return out
}
func (m *mockProductRepo) GetByMoyskladID(ctx context.Context, moyskladID string) (*db.Product, error) {
	return nil, nil
}
func (m *mockProductRepo) GetAll(ctx context.Context, limit, offset int) ([]*db.Product, error) {
	return m.all(), nil
}
func (m *mockProductRepo) GetActive(ctx context.Context, limit, offset int) ([]*db.Product, error) {
	return m.all(), nil
}
func (m *mockProductRepo) Search(ctx context.Context, query string, categoryID *int64, limit, offset int) ([]*db.Product, error) {
	return m.all(), nil
}
func (m *mockProductRepo) GetByCategory(ctx context.Context, categoryID int64, limit, offset int) ([]*db.Product, error) {
	return m.all(), nil
}
func (m *mockProductRepo) CountActive(ctx context.Context) (int, error) { return len(m.products), nil }
func (m *mockProductRepo) CountByCategory(ctx context.Context, c int64) (int, error) {
	return len(m.products), nil
}
func (m *mockProductRepo) CountSearch(ctx context.Context, q string, c *int64) (int, error) {
	return len(m.products), nil
}
func (m *mockProductRepo) ListIDsByCategoryTrees(ctx context.Context, ids []int64) ([]int64, error) {
	return ids, nil
}
func (m *mockProductRepo) FilterPromotionalIDs(ctx context.Context, ids []int64, search string, categoryID *int64) ([]int64, error) {
	return ids, nil
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

// Стоковые методы (для usecase.ProductRepository).
func (m *mockProductRepo) BatchGetAvailableStocks(ctx context.Context, ids []int64, q db.ProductQuery) (map[int64]int, error) {
	return map[int64]int{}, nil
}
func (m *mockProductRepo) SetStockToCache(ctx context.Context, productID int64, stock int) error {
	return nil
}
func (m *mockProductRepo) InvalidateStockCache(ctx context.Context, productID int64) error {
	return nil
}
func (m *mockProductRepo) BatchSetStocks(ctx context.Context, products []*db.Product) error {
	return nil
}

// ---- mock stockCache (inline-интерфейс в ProductUsecase) ------------------

type mockStockCache struct {
	avail map[int64]int
}

func (m *mockStockCache) GetAvailableStock(ctx context.Context, productID int64, q db.ProductQuery) (int, error) {
	if m.avail != nil {
		if v, ok := m.avail[productID]; ok {
			return v, nil
		}
	}
	return -2, nil // cache miss → используется сток из БД
}
func (m *mockStockCache) BatchGetAvailableStocks(ctx context.Context, ids []int64, q db.ProductQuery) (map[int64]int, error) {
	if m.avail != nil {
		return m.avail, nil
	}
	return map[int64]int{}, nil
}
func (m *mockStockCache) SetStockToCache(ctx context.Context, productID int64, stock int) error {
	return nil
}
func (m *mockStockCache) InvalidateStockCache(ctx context.Context, productID int64) error { return nil }
func (m *mockStockCache) BatchSetStocks(ctx context.Context, products []*db.Product) error {
	return nil
}

// ---- mock db.PromotionLinkQuery ------------------------------------------

type mockLinkQuery struct {
	productPromos  map[int64]*db.Promotion
	categoryPromos map[int64]*db.Promotion
	productIDs     []int64
	categoryIDs    []int64
}

func newMockLinkQuery() *mockLinkQuery {
	return &mockLinkQuery{productPromos: map[int64]*db.Promotion{}, categoryPromos: map[int64]*db.Promotion{}}
}

func (m *mockLinkQuery) ReplaceProductLinks(ctx context.Context, promotionID int64, productIDs []int64) error {
	return nil
}
func (m *mockLinkQuery) ReplaceCategoryLinks(ctx context.Context, promotionID int64, categoryIDs []int64) error {
	return nil
}
func (m *mockLinkQuery) ListProductIDs(ctx context.Context, promotionID int64) ([]int64, error) {
	return m.productIDs, nil
}
func (m *mockLinkQuery) ListCategoryIDs(ctx context.Context, promotionID int64) ([]int64, error) {
	return m.categoryIDs, nil
}
func (m *mockLinkQuery) ListProductIDsForPromotions(ctx context.Context, ids []int64) (map[int64][]int64, error) {
	out := map[int64][]int64{}
	for _, id := range ids {
		out[id] = m.productIDs
	}
	return out, nil
}
func (m *mockLinkQuery) ListCategoryIDsForPromotions(ctx context.Context, ids []int64) (map[int64][]int64, error) {
	out := map[int64][]int64{}
	for _, id := range ids {
		out[id] = m.categoryIDs
	}
	return out, nil
}
func (m *mockLinkQuery) BestActiveProductPromotions(ctx context.Context, productIDs []int64) (map[int64]*db.Promotion, error) {
	return m.productPromos, nil
}
func (m *mockLinkQuery) BestActiveCategoryPromotions(ctx context.Context, categoryIDs []int64) (map[int64]*db.Promotion, error) {
	return m.categoryPromos, nil
}

// ---- mock db.PromotionQuery ----------------------------------------------

type mockPromotionQuery struct {
	active []*db.Promotion
}

func (m *mockPromotionQuery) GetByID(ctx context.Context, id int64) (*db.Promotion, error) {
	return nil, nil
}
func (m *mockPromotionQuery) GetByMoyskladID(ctx context.Context, moyskladID string) (*db.Promotion, error) {
	return nil, nil
}
func (m *mockPromotionQuery) GetActive(ctx context.Context) ([]*db.Promotion, error) {
	return m.active, nil
}
func (m *mockPromotionQuery) GetAll(ctx context.Context) ([]*db.Promotion, error) {
	return m.active, nil
}
func (m *mockPromotionQuery) Insert(ctx context.Context, p *db.Promotion) (*db.Promotion, error) {
	return p, nil
}
func (m *mockPromotionQuery) Update(ctx context.Context, p *db.Promotion, id int64) (*db.Promotion, error) {
	return p, nil
}
func (m *mockPromotionQuery) Delete(ctx context.Context, id int64) error { return nil }

// ---- mock OrderRepository -------------------------------------------------

// mockOrderRepo эмулирует атомарное списание остатка под мьютексом —
// аналог UPDATE ... WHERE stock >= qty в транзакции PostgreSQL.
type mockOrderRepo struct {
	mu        sync.Mutex
	stock     int
	nextID    int64
	created   int
	failCAErr error     // принудительная ошибка CreateOrderAtomic
	orderByID *db.Order // что вернёт GetByID

	cancelMoyskladID *string       // CancelPendingOrderAtomic: id для удаления в МойСклад
	cancelRestored   []*db.Product // CancelPendingOrderAtomic: возвращённые товары
	cancelAlready    bool          // CancelPendingOrderAtomic: заказ уже не pending
	completeAlready  bool          // CompleteOrder: заказ уже не pending
	ordersList       []*db.Order   // что вернёт GetByUserID
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
	return m.cancelMoyskladID, m.cancelAlready, m.cancelRestored, nil
}
func (m *mockOrderRepo) CompleteOrder(ctx context.Context, orderID int64) (bool, error) {
	return m.completeAlready, nil
}
func (m *mockOrderRepo) GetByID(ctx context.Context, id int64) (*db.Order, error) {
	return m.orderByID, nil
}
func (m *mockOrderRepo) Update(ctx context.Context, o *db.Order, id int64) (*db.Order, error) {
	return o, nil
}
func (m *mockOrderRepo) GetByUserID(ctx context.Context, userID int64) ([]*db.Order, error) {
	return m.ordersList, nil
}
func (m *mockOrderRepo) GetItemsByOrderID(ctx context.Context, orderID int64) ([]*db.OrderItem, error) {
	return nil, nil
}

// Дополнительные методы для полного db.OrderQuery (нужны MoyskladOrderUsecase).
func (m *mockOrderRepo) Insert(ctx context.Context, o *db.Order) (*db.Order, error) {
	m.nextID++
	o.ID = m.nextID
	return o, nil
}
func (m *mockOrderRepo) GetAll(ctx context.Context, limit, offset int) ([]*db.Order, error) {
	return nil, nil
}
func (m *mockOrderRepo) ListExpiredPendingIDs(ctx context.Context, now time.Time, limit int) ([]int64, error) {
	return nil, nil
}
func (m *mockOrderRepo) ListOrders(ctx context.Context, f db.OrderListFilter) ([]*db.Order, int, error) {
	return nil, 0, nil
}
func (m *mockOrderRepo) StatsByStatus(ctx context.Context, from, to *time.Time) (map[string]int, error) {
	return map[string]int{}, nil
}
func (m *mockOrderRepo) RevenueByPeriod(ctx context.Context, from, to *time.Time) (*db.RevenueStats, error) {
	return &db.RevenueStats{}, nil
}
func (m *mockOrderRepo) GetUserStats(ctx context.Context, userID int64) (*db.UserOrderStats, error) {
	return nil, nil
}
func (m *mockOrderRepo) GetUserTopProducts(ctx context.Context, userID int64, limit int) ([]*db.UserTopProduct, error) {
	return nil, nil
}

// ---- mock db.OrderEventQuery ---------------------------------------------

type mockEventRepo struct {
	count int
	err   error
}

func (m *mockEventRepo) Insert(ctx context.Context, orderID int64, eventType string, actorUserID *int64, payload any) error {
	m.count++
	return m.err
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
	err     error // принудительная ошибка для всех операций
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
	if m.err != nil {
		return m.err
	}
	delete(m.items, cartKey(userID, productID))
	return nil
}
func (m *mockCartRepo) Clear(ctx context.Context, userID int64) error {
	if m.err != nil {
		return m.err
	}
	m.cleared = true
	m.items = make(map[string]*db.CartItem)
	return nil
}
func (m *mockCartRepo) GetTotal(ctx context.Context, userID int64) (float64, error) {
	if m.err != nil {
		return 0, m.err
	}
	return m.total, nil
}
