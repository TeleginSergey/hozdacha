package usecase

import (
	"context"
	"math"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/db"
)

// PromotionPricer применяет приоритетные акции к товарам:
//  1. акция на конкретный товар (выше);
//  2. акция на категорию или любую её родительскую категорию (ниже);
//  3. базовая цена (без акции).
//
// Если на одном уровне товару подходит несколько акций, выбирается с большей скидкой.
//
// Карта предков категорий кэшируется на короткий промежуток, чтобы не запрашивать БД
// на каждый рендер каталога.
type PromotionPricer struct {
	links         db.PromotionLinkQuery
	categories    *db.CategoryQuery
	logger        *zap.Logger

	ancestorTTL  time.Duration
	mu           sync.RWMutex
	ancestors    map[int64][]int64
	ancestorsExp time.Time
}

func NewPromotionPricer(
	links db.PromotionLinkQuery,
	categories *db.CategoryQuery,
	logger *zap.Logger,
) *PromotionPricer {
	return &PromotionPricer{
		links:       links,
		categories:  categories,
		logger:      logger,
		ancestorTTL: 5 * time.Minute,
	}
}

// Apply проставляет EffectivePrice / DiscountPercent / PromotionType / PromotionTitle
// для каждого товара. Безопасен при пустом входе и при отсутствии акций.
// Ошибки не фатальны: при сбое выборки акций товары останутся с базовой ценой.
func (p *PromotionPricer) Apply(ctx context.Context, products []*db.Product) {
	if p == nil || len(products) == 0 {
		return
	}

	productIDs := make([]int64, 0, len(products))
	categoryIDsSet := make(map[int64]struct{})
	for _, prod := range products {
		if prod == nil {
			continue
		}
		productIDs = append(productIDs, prod.ID)
		if prod.CategoryID != nil {
			categoryIDsSet[*prod.CategoryID] = struct{}{}
		}
	}

	// Получаем акции на товары пакетом.
	productPromos, err := p.links.BestActiveProductPromotions(ctx, productIDs)
	if err != nil {
		p.logger.Warn("failed to load product-level promotions", zap.Error(err))
		productPromos = map[int64]*db.Promotion{}
	}

	// Готовим расширенный набор категорий: к каждой добавляем всех предков,
	// чтобы акция на родителя действовала на потомков.
	expandedCategoryIDs := categoryIDsSet
	ancestorMap := p.getAncestorMap(ctx)
	if len(ancestorMap) > 0 {
		expandedCategoryIDs = make(map[int64]struct{}, len(categoryIDsSet))
		for cid := range categoryIDsSet {
			expandedCategoryIDs[cid] = struct{}{}
			for _, ancestor := range ancestorMap[cid] {
				expandedCategoryIDs[ancestor] = struct{}{}
			}
		}
	}

	categoryIDs := make([]int64, 0, len(expandedCategoryIDs))
	for cid := range expandedCategoryIDs {
		categoryIDs = append(categoryIDs, cid)
	}

	categoryPromos, err := p.links.BestActiveCategoryPromotions(ctx, categoryIDs)
	if err != nil {
		p.logger.Warn("failed to load category-level promotions", zap.Error(err))
		categoryPromos = map[int64]*db.Promotion{}
	}

	for _, prod := range products {
		if prod == nil {
			continue
		}
		applyToProduct(prod, productPromos, categoryPromos, ancestorMap)
	}
}

func applyToProduct(
	prod *db.Product,
	productPromos map[int64]*db.Promotion,
	categoryPromos map[int64]*db.Promotion,
	ancestorMap map[int64][]int64,
) {
	// 1. Product-level — высший приоритет.
	if promo, ok := productPromos[prod.ID]; ok && promo != nil && promo.Discount > 0 {
		setEffective(prod, promo, "product")
		return
	}

	// 2. Category-level: ищем по самой категории и всем её предкам, берём максимальную скидку.
	if prod.CategoryID == nil {
		return
	}

	chain := ancestorMap[*prod.CategoryID]
	if len(chain) == 0 {
		chain = []int64{*prod.CategoryID}
	}

	var best *db.Promotion
	for _, cid := range chain {
		promo, ok := categoryPromos[cid]
		if !ok || promo == nil || promo.Discount <= 0 {
			continue
		}
		if best == nil || promo.Discount > best.Discount {
			best = promo
		}
	}
	if best != nil {
		setEffective(prod, best, "category")
	}
}

func setEffective(prod *db.Product, promo *db.Promotion, ptype string) {
	discount := promo.Discount
	if discount <= 0 {
		return
	}
	if discount > 100 {
		discount = 100
	}
	effective := prod.Price * (1 - discount/100)
	// Округляем до 2 знаков, чтобы не показывать «199.999999».
	effective = math.Round(effective*100) / 100

	prod.EffectivePrice = &effective
	d := discount
	prod.DiscountPercent = &d
	prod.PromotionType = ptype
	prod.PromotionTitle = promo.Title
}

func (p *PromotionPricer) getAncestorMap(ctx context.Context) map[int64][]int64 {
	p.mu.RLock()
	if time.Now().Before(p.ancestorsExp) && p.ancestors != nil {
		m := p.ancestors
		p.mu.RUnlock()
		return m
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()
	// Повторная проверка после захвата write-блокировки.
	if time.Now().Before(p.ancestorsExp) && p.ancestors != nil {
		return p.ancestors
	}
	if p.categories == nil {
		return nil
	}
	m, err := p.categories.AncestorMap(ctx)
	if err != nil {
		p.logger.Warn("failed to load category ancestors", zap.Error(err))
		// Не кэшируем ошибочный пустой результат надолго — попробуем снова через 30 сек.
		p.ancestors = map[int64][]int64{}
		p.ancestorsExp = time.Now().Add(30 * time.Second)
		return p.ancestors
	}
	p.ancestors = m
	p.ancestorsExp = time.Now().Add(p.ancestorTTL)
	return p.ancestors
}

// InvalidateCategoryCache сбрасывает закэшированную карту предков (вызывать при изменении дерева категорий).
func (p *PromotionPricer) InvalidateCategoryCache() {
	p.mu.Lock()
	p.ancestors = nil
	p.ancestorsExp = time.Time{}
	p.mu.Unlock()
}
