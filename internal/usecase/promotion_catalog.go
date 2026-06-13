package usecase

import (
	"context"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/db"
	"github.com/TeleginSergey/hozdacha/internal/services"
)

// PromotionCatalogUsecase собирает единый каталог акционных товаров:
// прямые привязки + товары из категорий с акцией.
type PromotionCatalogUsecase struct {
	promotionQuery db.PromotionQuery
	linkQuery      db.PromotionLinkQuery
	productUC      *ProductUsecase
	productRepo    ProductRepository
	categories     *db.CategoryQuery
	logger         *zap.Logger
}

func NewPromotionCatalogUsecase(
	promotionQuery db.PromotionQuery,
	linkQuery db.PromotionLinkQuery,
	productUC *ProductUsecase,
	productRepo ProductRepository,
	categories *db.CategoryQuery,
	logger *zap.Logger,
) *PromotionCatalogUsecase {
	return &PromotionCatalogUsecase{
		promotionQuery: promotionQuery,
		linkQuery:      linkQuery,
		productUC:      productUC,
		productRepo:    productRepo,
		categories:     categories,
		logger:         logger,
	}
}

type PromotionCatalogCategory struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type PromotionCatalogPromo struct {
	ID       int64   `json:"id"`
	Title    string  `json:"title"`
	Discount float64 `json:"discount"`
	Kind     string  `json:"kind"` // product | category
}

type PromotionCatalogResult struct {
	Products         []*db.Product
	Total            int
	Categories       []PromotionCatalogCategory
	Promotions       []PromotionCatalogPromo
	ReservationNote  string
	ReservationTarget string
}

// ListProducts возвращает страницу акционных товаров с фильтрами по названию, категории и типу акции.
func (u *PromotionCatalogUsecase) ListProducts(
	ctx context.Context,
	search string,
	categoryID *int64,
	kind string,
	limit, offset int,
) (*PromotionCatalogResult, error) {
	res := &PromotionCatalogResult{}

	if u.promotionQuery == nil || u.linkQuery == nil || u.productUC == nil {
		return res, nil
	}

	promotions, err := u.promotionQuery.GetActive(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	promotions = services.FilterPromotionsForReservation(promotions, now)
	target, note := services.ReservationTarget(now)
	res.ReservationTarget = target
	res.ReservationNote = note

	if len(promotions) == 0 {
		return res, nil
	}

	res.Promotions = buildPromoSummaries(ctx, u.linkQuery, promotions)

	candidateIDs, err := u.resolveCandidateIDs(ctx, promotions)
	if err != nil {
		return nil, err
	}
	if len(candidateIDs) == 0 {
		return res, nil
	}

	search = strings.TrimSpace(search)
	filteredIDs, err := u.productRepo.FilterPromotionalIDs(ctx, candidateIDs, search, categoryID)
	if err != nil {
		return nil, err
	}

	// Загружаем все кандидаты для сортировки по скидке, затем режем страницу.
	// Для типичного магазина акционных позиций немного — это приемлемо.
	allProducts, err := u.productUC.GetProductsByIDs(ctx, filteredIDs)
	if err != nil {
		return nil, err
	}

	promoProducts := filterProductsWithPromotion(allProducts)
	promoProducts = filterByPromotionKind(promoProducts, kind)

	// Категории для фильтра — из всего акционного каталога, не только текущей выборки.
	if catSourceIDs, err := u.productRepo.FilterPromotionalIDs(ctx, candidateIDs, "", nil); err == nil && len(catSourceIDs) > 0 {
		if catProducts, err := u.productUC.GetProductsByIDs(ctx, catSourceIDs); err == nil {
			cats, err := u.productRepo.ListCategoriesForProductIDs(ctx, idsOf(filterProductsWithPromotion(catProducts)))
			if err != nil {
				u.logger.Warn("failed to load promo categories", zap.Error(err))
			} else {
				res.Categories = make([]PromotionCatalogCategory, 0, len(cats))
				for _, c := range cats {
					res.Categories = append(res.Categories, PromotionCatalogCategory{ID: c.ID, Name: c.Name})
				}
			}
		}
	}

	sort.SliceStable(promoProducts, func(i, j int) bool {
		di := discountOf(promoProducts[i])
		dj := discountOf(promoProducts[j])
		if di != dj {
			return di > dj
		}
		return promoProducts[i].Name < promoProducts[j].Name
	})

	res.Total = len(promoProducts)
	if offset > res.Total {
		offset = res.Total
	}
	end := offset + limit
	if end > res.Total {
		end = res.Total
	}
	res.Products = promoProducts[offset:end]

	return res, nil
}

func buildPromoSummaries(ctx context.Context, links db.PromotionLinkQuery, promotions []*db.Promotion) []PromotionCatalogPromo {
	ids := make([]int64, 0, len(promotions))
	for _, p := range promotions {
		if p != nil {
			ids = append(ids, p.ID)
		}
	}
	productMap, _ := links.ListProductIDsForPromotions(ctx, ids)
	categoryMap, _ := links.ListCategoryIDsForPromotions(ctx, ids)

	out := make([]PromotionCatalogPromo, 0, len(promotions))
	for _, p := range promotions {
		if p == nil {
			continue
		}
		kind := "product"
		if len(productMap[p.ID]) == 0 && len(categoryMap[p.ID]) > 0 {
			kind = "category"
		} else if len(categoryMap[p.ID]) > 0 {
			kind = "mixed"
		}
		out = append(out, PromotionCatalogPromo{
			ID:       p.ID,
			Title:    p.Title,
			Discount: p.Discount,
			Kind:     kind,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Discount > out[j].Discount
	})
	return out
}

func (u *PromotionCatalogUsecase) resolveCandidateIDs(ctx context.Context, promotions []*db.Promotion) ([]int64, error) {
	ids := make([]int64, 0, len(promotions))
	for _, p := range promotions {
		if p != nil {
			ids = append(ids, p.ID)
		}
	}

	productMap, err := u.linkQuery.ListProductIDsForPromotions(ctx, ids)
	if err != nil {
		return nil, err
	}
	categoryMap, err := u.linkQuery.ListCategoryIDsForPromotions(ctx, ids)
	if err != nil {
		return nil, err
	}

	seen := make(map[int64]struct{})
	result := make([]int64, 0)

	add := func(id int64) {
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}

	for _, list := range productMap {
		for _, id := range list {
			add(id)
		}
	}

	categoryIDs := make([]int64, 0)
	catSeen := make(map[int64]struct{})
	for _, list := range categoryMap {
		for _, cid := range list {
			if _, ok := catSeen[cid]; ok {
				continue
			}
			catSeen[cid] = struct{}{}
			categoryIDs = append(categoryIDs, cid)
		}
	}

	if len(categoryIDs) > 0 {
		catProductIDs, err := u.productRepo.ListIDsByCategoryTrees(ctx, categoryIDs)
		if err != nil {
			return nil, err
		}
		for _, id := range catProductIDs {
			add(id)
		}
	}

	return result, nil
}

func filterProductsWithPromotion(products []*db.Product) []*db.Product {
	out := make([]*db.Product, 0, len(products))
	for _, p := range products {
		if p == nil || p.EffectivePrice == nil {
			continue
		}
		if *p.EffectivePrice < p.Price {
			out = append(out, p)
		}
	}
	return out
}

func filterByPromotionKind(products []*db.Product, kind string) []*db.Product {
	kind = strings.TrimSpace(strings.ToLower(kind))
	if kind == "" || kind == "all" {
		return products
	}
	out := make([]*db.Product, 0, len(products))
	for _, p := range products {
		if p == nil {
			continue
		}
		if kind == "product" && p.PromotionType == "product" {
			out = append(out, p)
		}
		if kind == "category" && p.PromotionType == "category" {
			out = append(out, p)
		}
	}
	return out
}

func discountOf(p *db.Product) float64 {
	if p == nil || p.DiscountPercent == nil {
		return 0
	}
	return *p.DiscountPercent
}

func idsOf(products []*db.Product) []int64 {
	ids := make([]int64, 0, len(products))
	for _, p := range products {
		if p != nil {
			ids = append(ids, p.ID)
		}
	}
	return ids
}
