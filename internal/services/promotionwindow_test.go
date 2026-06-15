package services

import (
	"testing"
	"time"

	"github.com/TeleginSergey/hozdacha/internal/db"
)

func mskTime(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, moscowLocation)
}

func TestReservationTarget_weekdayBeforeCutoff(t *testing.T) {
	// Среда 17:30 — ещё «сегодня».
	now := mskTime(2026, time.June, 10, 17, 30)
	target, note := ReservationTarget(now)
	if target != "today" {
		t.Fatalf("target = %q, want today", target)
	}
	if note != "Акция действует только на бронь сегодня" {
		t.Fatalf("note = %q", note)
	}
}

func TestReservationTarget_weekdayAfterCutoff(t *testing.T) {
	// Среда 18:00 — уже «завтра».
	now := mskTime(2026, time.June, 10, 18, 0)
	target, note := ReservationTarget(now)
	if target != "tomorrow" {
		t.Fatalf("target = %q, want tomorrow", target)
	}
	if note != "Акция действует только на бронь завтра" {
		t.Fatalf("note = %q", note)
	}
}

func TestReservationTarget_weekendCutoff(t *testing.T) {
	// Суббота 15:59 — сегодня.
	before := mskTime(2026, time.June, 13, 15, 59)
	target, _ := ReservationTarget(before)
	if target != "today" {
		t.Fatalf("before cutoff: target = %q, want today", target)
	}

	// Суббота 16:00 — завтра.
	after := mskTime(2026, time.June, 13, 16, 0)
	target, _ = ReservationTarget(after)
	if target != "tomorrow" {
		t.Fatalf("after cutoff: target = %q, want tomorrow", target)
	}
}

func TestReservationDeadline(t *testing.T) {
	// Будний день до закрытия — крайний срок сегодня 18:00.
	wd := mskTime(2026, time.June, 10, 14, 0) // среда
	if got := ReservationDeadline(wd); !got.Equal(mskTime(2026, time.June, 10, 18, 0)) {
		t.Errorf("weekday before cutoff: %v, want 2026-06-10 18:00 MSK", got)
	}
	// Будний день после закрытия — крайний срок завтра 18:00.
	wdLate := mskTime(2026, time.June, 10, 19, 0)
	if got := ReservationDeadline(wdLate); !got.Equal(mskTime(2026, time.June, 11, 18, 0)) {
		t.Errorf("weekday after cutoff: %v, want 2026-06-11 18:00 MSK", got)
	}
	// Выходной до закрытия — крайний срок сегодня 16:00.
	we := mskTime(2026, time.June, 13, 12, 0) // суббота
	if got := ReservationDeadline(we); !got.Equal(mskTime(2026, time.June, 13, 16, 0)) {
		t.Errorf("weekend before cutoff: %v, want 2026-06-13 16:00 MSK", got)
	}
}

func TestPromotionAppliesForReservation_dayKind(t *testing.T) {
	// Акция синкнута в понедельник 19:00 → бронь на вторник.
	syncedMonEvening := mskTime(2026, time.June, 8, 19, 0)
	promo := &db.Promotion{
		Active:    true,
		Kind:      db.PromotionKindDay,
		ValidFrom: &syncedMonEvening,
	}

	// Понедельник 19:30 — показываем завтрашнюю (вторник).
	viewMonEvening := mskTime(2026, time.June, 8, 19, 30)
	if !PromotionAppliesForReservation(promo, viewMonEvening) {
		t.Fatal("promo should apply Monday evening for Tuesday booking")
	}

	// Вторник 10:00 — та же акция теперь «сегодня».
	viewTueMorning := mskTime(2026, time.June, 9, 10, 0)
	if !PromotionAppliesForReservation(promo, viewTueMorning) {
		t.Fatal("promo should apply Tuesday morning for Tuesday booking")
	}

	// Понедельник 10:00 — ещё рано, акция для вторника не должна показываться.
	viewMonMorning := mskTime(2026, time.June, 8, 10, 0)
	if PromotionAppliesForReservation(promo, viewMonMorning) {
		t.Fatal("promo should not apply Monday morning")
	}
}

func TestFilterPromotionsForReservation(t *testing.T) {
	todayPromo := &db.Promotion{
		ID:        1,
		Active:    true,
		Kind:      db.PromotionKindDay,
		ValidFrom: ptrTime(mskTime(2026, time.June, 10, 10, 0)),
	}
	tomorrowPromo := &db.Promotion{
		ID:        2,
		Active:    true,
		Kind:      db.PromotionKindDay,
		ValidFrom: ptrTime(mskTime(2026, time.June, 10, 19, 0)),
	}
	manual := &db.Promotion{ID: 3, Active: true, Kind: db.PromotionKindManual}

	now := mskTime(2026, time.June, 10, 19, 30)
	filtered := FilterPromotionsForReservation([]*db.Promotion{todayPromo, tomorrowPromo, manual}, now)

	if len(filtered) != 2 {
		t.Fatalf("len = %d, want 2", len(filtered))
	}
	if filtered[0].ID != 2 || filtered[1].ID != 3 {
		t.Fatalf("unexpected ids: %d, %d", filtered[0].ID, filtered[1].ID)
	}
}

func ptrTime(t time.Time) *time.Time {
	return &t
}
