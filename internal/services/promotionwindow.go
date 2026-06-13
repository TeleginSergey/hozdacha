package services

import (
	"time"

	"github.com/TeleginSergey/hozdacha/internal/db"
)

// moscowLocation — часовой пояс, в котором магазин работает с покупателями.
// Все «окна бронирования» считаются по нему.
var moscowLocation = func() *time.Location {
	if loc, err := time.LoadLocation("Europe/Moscow"); err == nil {
		return loc
	}
	return time.FixedZone("MSK", 3*60*60)
}()

// reservationCutoffHour — час закрытия магазина (по Москве). После него
// «сегодняшние» акции перестают быть актуальными и фокус переключается на завтра.
func reservationCutoffHour(d time.Time) int {
	// Будни (пн–пт) — 18:00, выходные (сб–вс) — 16:00.
	w := d.Weekday()
	if w == time.Saturday || w == time.Sunday {
		return 16
	}
	return 18
}

func isBeforeReservationCutoff(msk time.Time) bool {
	cutoff := reservationCutoffHour(msk)
	cutoffTime := time.Date(msk.Year(), msk.Month(), msk.Day(), cutoff, 0, 0, 0, moscowLocation)
	return msk.Before(cutoffTime)
}

// IsOpenForReservationDay — «магазин открыт в выбранный день для самовывоза».
// Возвращает false только для 1 и 2 января (праздничные дни).
func IsOpenForReservationDay(d time.Time) bool {
	return reservationCutoffHour(d) >= 0
}

// ReservationTarget определяет, на какую дату (по Москве) сейчас нужно
// рекламировать акцию как «актуальную для брони»:
//   - до 18:00 (будни) / 16:00 (выходные) → "today"
//   - после закрытия                       → "tomorrow"
//
// Возвращает пару (target, note) — текст-подсказку, которую фронт показывает
// в карточке («Акция действует только на бронь сегодня/завтра»).
func ReservationTarget(now time.Time) (target string, note string) {
	if isBeforeReservationCutoff(now.In(moscowLocation)) {
		return "today", "Акция действует только на бронь сегодня"
	}
	return "tomorrow", "Акция действует только на бронь завтра"
}

// ReservationDate возвращает календарный день (полночь по Москве), на который
// сейчас действует акция для брони.
func ReservationDate(now time.Time) time.Time {
	msk := now.In(moscowLocation)
	y, m, d := msk.Date()
	day := time.Date(y, m, d, 0, 0, 0, 0, moscowLocation)
	if isBeforeReservationCutoff(msk) {
		return day
	}
	return day.AddDate(0, 0, 1)
}

// EndOfMoscowDay — конец календарного дня по Москве (23:59:59.999999999).
func EndOfMoscowDay(day time.Time) time.Time {
	msk := day.In(moscowLocation)
	y, m, d := msk.Date()
	return time.Date(y, m, d, 23, 59, 59, 999999999, moscowLocation)
}

// PromotionReservationDay вычисляет день брони, на который была заведена
// дневная акция (по моменту valid_from или created_at).
func PromotionReservationDay(promo *db.Promotion) time.Time {
	if promo == nil {
		return time.Time{}
	}
	ref := promo.CreatedAt
	if promo.ValidFrom != nil {
		ref = *promo.ValidFrom
	}
	return ReservationDate(ref)
}

// PromotionAppliesForReservation проверяет, подходит ли акция для текущего окна брони.
// Дневные акции (kind=day) привязаны к конкретному дню; manual/permanent — всегда.
func PromotionAppliesForReservation(promo *db.Promotion, now time.Time) bool {
	if promo == nil || !promo.Active {
		return false
	}
	if promo.Kind != db.PromotionKindDay {
		return true
	}
	want := ReservationDate(now)
	got := PromotionReservationDay(promo)
	return sameMoscowDay(got, want)
}

// FilterPromotionsForReservation оставляет только акции, актуальные для текущего окна брони.
func FilterPromotionsForReservation(promotions []*db.Promotion, now time.Time) []*db.Promotion {
	if len(promotions) == 0 {
		return promotions
	}
	out := make([]*db.Promotion, 0, len(promotions))
	for _, p := range promotions {
		if PromotionAppliesForReservation(p, now) {
			out = append(out, p)
		}
	}
	return out
}

// IsReservationWindowOpen — открыто ли окно бронирования в текущий момент.
// Используется в CreateOrder для блокировки заказа акционных товаров в нерабочее время
// (после закрытия магазина «сегодняшняя» акция перестаёт быть актуальной).
// Возвращает true, если сейчас (по Москве) ещё не наступил час закрытия.
func IsReservationWindowOpen(now time.Time) bool {
	return isBeforeReservationCutoff(now.In(moscowLocation))
}

func sameMoscowDay(a, b time.Time) bool {
	ay, am, ad := a.In(moscowLocation).Date()
	by, bm, bd := b.In(moscowLocation).Date()
	return ay == by && am == bm && ad == bd
}
