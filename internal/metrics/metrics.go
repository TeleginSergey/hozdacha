// Package metrics — централизованная телеметрия приложения на Prometheus.
//
// Метрики собираются в собственный Registry (не в DefaultRegisterer), чтобы
// контролировать состав и отдавать его как через /metrics (формат Prometheus),
// так и через /api/admin/metrics (JSON-снимок для админ-панели).
//
// Пакет использует package-level синглтоны — это прагматичный и распространённый
// для app-метрик подход: счётчики дёргаются из разных слоёв (middleware,
// хендлеры, сервисы, кэш) без проброса объекта по всему графу зависимостей.
package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Registry — изолированный реестр коллекторов этого приложения.
var Registry = prometheus.NewRegistry()

// startedAt фиксирует момент старта процесса для расчёта uptime.
var startedAt = time.Now()

var (
	// --- HTTP (RED: Rate, Errors, Duration) ---

	httpRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Общее число HTTP-запросов по методу, маршруту и статусу.",
	}, []string{"method", "path", "status"})

	httpDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "Длительность обработки HTTP-запроса в секундах.",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	}, []string{"method", "path"})

	httpInFlight = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "http_requests_in_flight",
		Help: "Число запросов, обрабатываемых прямо сейчас.",
	})

	// --- Бизнес-метрики заказов ---

	ordersCreated = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "app_orders_created_total",
		Help: "Число успешно созданных заказов (броней).",
	})

	orderTransitions = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "app_order_transitions_total",
		Help: "Число переходов заказа в терминальный статус.",
	}, []string{"to"}) // completed | cancelled | expired

	orderRevenue = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "app_order_revenue_rub_total",
		Help: "Суммарная стоимость созданных заказов в рублях.",
	})

	// --- Синхронизация с МойСклад ---

	moyskladSyncRuns = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "app_moysklad_sync_runs_total",
		Help: "Число прогонов синхронизации с МойСклад по результату.",
	}, []string{"result"}) // success | error

	moyskladSyncDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "app_moysklad_sync_duration_seconds",
		Help:    "Длительность прогона синхронизации с МойСклад в секундах.",
		Buckets: []float64{0.5, 1, 2.5, 5, 10, 30, 60, 120, 300},
	})

	// --- Кэш остатков (Redis) ---

	stockCacheEvents = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "app_stock_cache_events_total",
		Help: "Обращения к кэшу остатков по результату (hit/miss).",
	}, []string{"result"}) // hit | miss
)

func init() {
	Registry.MustRegister(
		httpRequests,
		httpDuration,
		httpInFlight,
		ordersCreated,
		orderTransitions,
		orderRevenue,
		moyskladSyncRuns,
		moyskladSyncDuration,
		stockCacheEvents,
		// Стандартные коллекторы Go-runtime и процесса (go_* / process_*).
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
}

// =============================================================================
// Точки записи — вызываются из middleware/хендлеров/сервисов.
// =============================================================================

// ObserveHTTP фиксирует завершённый HTTP-запрос.
func ObserveHTTP(method, path, status string, seconds float64) {
	httpRequests.WithLabelValues(method, path, status).Inc()
	httpDuration.WithLabelValues(method, path).Observe(seconds)
}

// IncInFlight / DecInFlight — счётчик запросов «в полёте».
func IncInFlight() { httpInFlight.Inc() }
func DecInFlight() { httpInFlight.Dec() }

// OrderCreated отмечает создание заказа на сумму total рублей.
func OrderCreated(total float64) {
	ordersCreated.Inc()
	if total > 0 {
		orderRevenue.Add(total)
	}
}

// OrderTransition отмечает перевод заказа в терминальный статус
// (completed | cancelled | expired).
func OrderTransition(to string) {
	orderTransitions.WithLabelValues(to).Inc()
}

// MoyskladSync фиксирует прогон синхронизации: результат и длительность.
func MoyskladSync(ok bool, d time.Duration) {
	result := "success"
	if !ok {
		result = "error"
	}
	moyskladSyncRuns.WithLabelValues(result).Inc()
	moyskladSyncDuration.Observe(d.Seconds())
}

// StockCache фиксирует обращения к кэшу остатков: hits попаданий и misses промахов.
func StockCache(hits, misses int) {
	if hits > 0 {
		stockCacheEvents.WithLabelValues("hit").Add(float64(hits))
	}
	if misses > 0 {
		stockCacheEvents.WithLabelValues("miss").Add(float64(misses))
	}
}

// DBPoolStat — минимальный снимок пула соединений (без импорта pgx в этот пакет).
type DBPoolStat struct {
	Total, InUse, Idle int32
}

// RegisterDBPool регистрирует GaugeFunc-коллекторы пула БД. fn вызывается на
// каждый scrape, отдавая актуальный снимок. Вызывать один раз при старте.
func RegisterDBPool(fn func() DBPoolStat) {
	Registry.MustRegister(
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "app_db_pool_total_conns",
			Help: "Всего соединений в пуле БД.",
		}, func() float64 { return float64(fn().Total) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "app_db_pool_in_use_conns",
			Help: "Занятых соединений в пуле БД.",
		}, func() float64 { return float64(fn().InUse) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "app_db_pool_idle_conns",
			Help: "Свободных соединений в пуле БД.",
		}, func() float64 { return float64(fn().Idle) }),
	)
	dbPoolSource = fn
}

// dbPoolSource хранится отдельно, чтобы Snapshot() мог отдать значения в JSON.
var dbPoolSource func() DBPoolStat

// UptimeSeconds — сколько процесс работает.
func UptimeSeconds() float64 { return time.Since(startedAt).Seconds() }
