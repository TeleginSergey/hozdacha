package metrics

import (
	"testing"
	"time"
)

func TestSnapshot_ReflectsRecordedMetrics(t *testing.T) {
	// Записываем по одному событию каждого типа.
	ObserveHTTP("GET", "/api/products", "200", 0.05)
	ObserveHTTP("GET", "/api/products", "500", 0.2)
	OrderCreated(1500)
	OrderTransition("completed")
	OrderTransition("expired")
	MoyskladSync(true, 3*time.Second)
	StockCache(8, 2)

	s := GetSnapshot()

	if s.HTTP.Total < 2 {
		t.Errorf("HTTP.Total = %v, want >= 2", s.HTTP.Total)
	}
	if s.HTTP.Errors5xx < 1 {
		t.Errorf("HTTP.Errors5xx = %v, want >= 1", s.HTTP.Errors5xx)
	}
	if s.HTTP.AvgLatencyMs <= 0 {
		t.Errorf("HTTP.AvgLatencyMs = %v, want > 0", s.HTTP.AvgLatencyMs)
	}
	if s.Orders.Created < 1 {
		t.Errorf("Orders.Created = %v, want >= 1", s.Orders.Created)
	}
	if s.Orders.Revenue < 1500 {
		t.Errorf("Orders.Revenue = %v, want >= 1500", s.Orders.Revenue)
	}
	if s.Orders.Completed < 1 || s.Orders.Expired < 1 {
		t.Errorf("transitions not reflected: completed=%v expired=%v", s.Orders.Completed, s.Orders.Expired)
	}
	if s.Moysklad.SyncSuccess < 1 {
		t.Errorf("Moysklad.SyncSuccess = %v, want >= 1", s.Moysklad.SyncSuccess)
	}
	if s.Cache.Hits < 8 || s.Cache.Misses < 2 {
		t.Errorf("cache not reflected: hits=%v misses=%v", s.Cache.Hits, s.Cache.Misses)
	}
	if s.Cache.HitRate <= 0 || s.Cache.HitRate > 1 {
		t.Errorf("Cache.HitRate = %v, want (0,1]", s.Cache.HitRate)
	}
	// Runtime всегда заполняется.
	if s.Runtime.Goroutines <= 0 {
		t.Errorf("Runtime.Goroutines = %v, want > 0", s.Runtime.Goroutines)
	}
}

func TestRegisterDBPool_FeedsSnapshot(t *testing.T) {
	// Регистрация коллекторов пула выполняется один раз на процесс; если другой
	// тест уже её сделал, MustRegister паникует — поэтому защищаемся recover'ом
	// и проверяем только то, что источник снимка отдаёт значения.
	defer func() { _ = recover() }()
	RegisterDBPool(func() DBPoolStat {
		return DBPoolStat{Total: 10, InUse: 3, Idle: 7}
	})
	s := GetSnapshot()
	if s.DBPool.Total != 10 || s.DBPool.InUse != 3 || s.DBPool.Idle != 7 {
		t.Errorf("db pool snapshot = %+v, want 10/3/7", s.DBPool)
	}
}
