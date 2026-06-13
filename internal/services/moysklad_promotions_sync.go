package services

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/TeleginSergey/hozdacha/internal/db"
	"github.com/TeleginSergey/hozdacha/internal/moysklad"
)

// SetPromotionRepos подключает репозитории акций для опциональной синхронизации
// из МойСклад. Если хотя бы один из репозиториев nil, синхронизация выключена.
func (s *MoyskladSyncService) SetPromotionRepos(promo db.PromotionQuery, links db.PromotionLinkQuery) {
	s.promotionQuery = promo
	s.promotionLinkQuery = links
}

// PromotionSyncResult — статистика по проходу синхронизации акций.
type PromotionSyncResult struct {
	Fetched          int `json:"fetched"`
	Created          int `json:"created"`
	Updated          int `json:"updated"`
	Skipped          int `json:"skipped"`
	ProductLinksSet  int `json:"product_links_set"`
	CategoryLinksSet int `json:"category_links_set"`
	Errors           int `json:"errors"`
}

// SyncPromotions подтягивает specialpricediscount из МойСклад в локальные таблицы:
//   - upsert promotions (по promotions_moysklad_id);
//   - заменяет промежуточные связи promotion_products / promotion_categories.
//
// Для маппинга folder UUID → DB id категории используется кэш s.folderDBIDByUUID,
// который наполняется через RefreshCategories. Поэтому полную синхронизацию акций
// разумно запускать после синхронизации товаров/категорий.
func (s *MoyskladSyncService) SyncPromotions(ctx context.Context) (*PromotionSyncResult, error) {
	res := &PromotionSyncResult{}

	if s.moyskladClient == nil || s.promotionQuery == nil || s.promotionLinkQuery == nil {
		s.logger.Debug("promotion sync skipped: missing client or repos")
		return res, nil
	}

	discounts, err := s.moyskladClient.GetSpecialPriceDiscounts(ctx)
	if err != nil {
		return res, fmt.Errorf("failed to fetch specialpricediscount: %w", err)
	}
	res.Fetched = len(discounts)

	for i := range discounts {
		d := discounts[i]
		if d.ID == "" || d.Name == "" {
			res.Skipped++
			continue
		}

		promo, created, err := s.upsertPromotion(ctx, &d)
		if err != nil {
			s.logger.Warn("Failed to upsert promotion",
				zap.String("moysklad_id", d.ID),
				zap.String("name", d.Name),
				zap.Error(err))
			res.Errors++
			continue
		}
		if created {
			res.Created++
		} else {
			res.Updated++
		}

		productIDs := s.resolveProductIDs(ctx, d.Assortment)
		if err := s.promotionLinkQuery.ReplaceProductLinks(ctx, promo.ID, productIDs); err != nil {
			s.logger.Warn("Failed to replace product links",
				zap.Int64("promotion_id", promo.ID), zap.Error(err))
			res.Errors++
		} else {
			res.ProductLinksSet += len(productIDs)
		}

		categoryIDs := s.resolveCategoryIDs(d.ProductFolders)
		if err := s.promotionLinkQuery.ReplaceCategoryLinks(ctx, promo.ID, categoryIDs); err != nil {
			s.logger.Warn("Failed to replace category links",
				zap.Int64("promotion_id", promo.ID), zap.Error(err))
			res.Errors++
		} else {
			res.CategoryLinksSet += len(categoryIDs)
		}
	}

	s.logger.Info("Moysklad promotions synced",
		zap.Int("fetched", res.Fetched),
		zap.Int("created", res.Created),
		zap.Int("updated", res.Updated),
		zap.Int("skipped", res.Skipped),
		zap.Int("product_links", res.ProductLinksSet),
		zap.Int("category_links", res.CategoryLinksSet),
		zap.Int("errors", res.Errors))

	return res, nil
}

// upsertPromotion создаёт или обновляет запись в promotions на основе specialpricediscount.
// Возвращает promo и флаг created=true при первой вставке.
func (s *MoyskladSyncService) upsertPromotion(
	ctx context.Context,
	d *moysklad.MoyskladSpecialPriceDiscount,
) (*db.Promotion, bool, error) {
	existing, err := s.promotionQuery.GetByMoyskladID(ctx, d.ID)
	if err != nil {
		return nil, false, fmt.Errorf("get by moysklad id: %w", err)
	}

	// Явно проставляем временные метки, иначе при первой вставке в БД
	// полетят zero-значения, что раньше ломало SELECT'ы со scany
	// (cannot scan NULL into *time.Time для promotions_updated_at).
	now := time.Now()
	resDay := ReservationDate(now)
	validUntil := EndOfMoscowDay(resDay)
	moyID := d.ID
	promo := &db.Promotion{
		Title:      d.Name,
		Discount:   d.Discount,
		Active:     d.Active,
		MoyskladID: &moyID,
		Kind:       db.PromotionKindDay,
		ValidFrom:  &now,
		ValidUntil: &validUntil,
	}
	if existing == nil {
		// Новой записи — обе метки в now, чтобы поле UpdatedAt не было пустым.
		promo.CreatedAt = now
	}
	promo.UpdatedAt = now

	if existing == nil {
		inserted, err := s.promotionQuery.Insert(ctx, promo)
		if err != nil {
			return nil, false, fmt.Errorf("insert: %w", err)
		}
		return inserted, true, nil
	}

	updated, err := s.promotionQuery.Update(ctx, promo, existing.ID)
	if err != nil {
		return nil, false, fmt.Errorf("update: %w", err)
	}
	return updated, false, nil
}

// resolveProductIDs преобразует assortment-ссылки на товары в локальные product IDs.
// Товары, которых нет в нашей БД, пропускаются — это нормально, такие ещё не синхронизированы.
func (s *MoyskladSyncService) resolveProductIDs(
	ctx context.Context,
	refs []moysklad.MoyskladMetaRef,
) []int64 {
	if len(refs) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(refs))
	for _, ref := range refs {
		uuid := s.moyskladClient.ExtractUUIDFromHref(ref.Meta.Href)
		if uuid == "" {
			continue
		}
		prod, err := s.productQuery.GetByMoyskladID(ctx, uuid)
		if err != nil || prod == nil {
			continue
		}
		ids = append(ids, prod.ID)
	}
	return ids
}

// resolveCategoryIDs преобразует productFolders-ссылки в локальные category IDs
// через кэш folderDBIDByUUID, наполненный синхронизацией категорий.
func (s *MoyskladSyncService) resolveCategoryIDs(refs []moysklad.MoyskladMetaRef) []int64 {
	if len(refs) == 0 {
		return nil
	}
	s.folderMu.RLock()
	defer s.folderMu.RUnlock()
	if len(s.folderDBIDByUUID) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(refs))
	for _, ref := range refs {
		uuid := s.moyskladClient.ExtractUUIDFromHref(ref.Meta.Href)
		if uuid == "" {
			continue
		}
		if id, ok := s.folderDBIDByUUID[uuid]; ok {
			ids = append(ids, id)
		}
	}
	return ids
}
