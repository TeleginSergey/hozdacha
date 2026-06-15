package metrics

import (
	"runtime"
	"strings"

	dto "github.com/prometheus/client_model/go"
)

// Snapshot — точечный срез метрик для отображения в админ-панели.
// Счётчики кумулятивны с момента старта процесса.
type Snapshot struct {
	UptimeSeconds float64 `json:"uptime_seconds"`

	HTTP struct {
		Total        float64            `json:"total"`
		Errors5xx    float64            `json:"errors_5xx"`
		InFlight     float64            `json:"in_flight"`
		AvgLatencyMs float64            `json:"avg_latency_ms"`
		ByClass      map[string]float64 `json:"by_class"` // 2xx/3xx/4xx/5xx
	} `json:"http"`

	Orders struct {
		Created   float64 `json:"created"`
		Completed float64 `json:"completed"`
		Cancelled float64 `json:"cancelled"`
		Expired   float64 `json:"expired"`
		Revenue   float64 `json:"revenue"`
	} `json:"orders"`

	Moysklad struct {
		SyncSuccess        float64 `json:"sync_success"`
		SyncError          float64 `json:"sync_error"`
		AvgDurationSeconds float64 `json:"avg_duration_seconds"`
	} `json:"moysklad"`

	Cache struct {
		Hits    float64 `json:"hits"`
		Misses  float64 `json:"misses"`
		HitRate float64 `json:"hit_rate"` // 0..1
	} `json:"cache"`

	DBPool struct {
		Total float64 `json:"total"`
		InUse float64 `json:"in_use"`
		Idle  float64 `json:"idle"`
	} `json:"db_pool"`

	Runtime struct {
		Goroutines float64 `json:"goroutines"`
		AllocMB    float64 `json:"alloc_mb"`
		NumGC      float64 `json:"num_gc"`
	} `json:"runtime"`
}

// GetSnapshot собирает срез из реестра Prometheus + runtime.
func GetSnapshot() Snapshot {
	var s Snapshot
	s.UptimeSeconds = UptimeSeconds()
	s.HTTP.ByClass = map[string]float64{}

	families, err := Registry.Gather()
	if err == nil {
		for _, mf := range families {
			switch mf.GetName() {
			case "http_requests_total":
				for _, m := range mf.GetMetric() {
					v := m.GetCounter().GetValue()
					s.HTTP.Total += v
					status := labelValue(m, "status")
					if class := statusClass(status); class != "" {
						s.HTTP.ByClass[class] += v
					}
					if strings.HasPrefix(status, "5") {
						s.HTTP.Errors5xx += v
					}
				}
			case "http_request_duration_seconds":
				var sum, count float64
				for _, m := range mf.GetMetric() {
					h := m.GetHistogram()
					sum += h.GetSampleSum()
					count += float64(h.GetSampleCount())
				}
				if count > 0 {
					s.HTTP.AvgLatencyMs = (sum / count) * 1000
				}
			case "http_requests_in_flight":
				s.HTTP.InFlight = firstGauge(mf)
			case "app_orders_created_total":
				s.Orders.Created = firstCounter(mf)
			case "app_order_revenue_rub_total":
				s.Orders.Revenue = firstCounter(mf)
			case "app_order_transitions_total":
				for _, m := range mf.GetMetric() {
					switch labelValue(m, "to") {
					case "completed":
						s.Orders.Completed = m.GetCounter().GetValue()
					case "cancelled":
						s.Orders.Cancelled = m.GetCounter().GetValue()
					case "expired":
						s.Orders.Expired = m.GetCounter().GetValue()
					}
				}
			case "app_moysklad_sync_runs_total":
				for _, m := range mf.GetMetric() {
					switch labelValue(m, "result") {
					case "success":
						s.Moysklad.SyncSuccess = m.GetCounter().GetValue()
					case "error":
						s.Moysklad.SyncError = m.GetCounter().GetValue()
					}
				}
			case "app_moysklad_sync_duration_seconds":
				for _, m := range mf.GetMetric() {
					h := m.GetHistogram()
					if c := float64(h.GetSampleCount()); c > 0 {
						s.Moysklad.AvgDurationSeconds = h.GetSampleSum() / c
					}
				}
			case "app_stock_cache_events_total":
				for _, m := range mf.GetMetric() {
					switch labelValue(m, "result") {
					case "hit":
						s.Cache.Hits = m.GetCounter().GetValue()
					case "miss":
						s.Cache.Misses = m.GetCounter().GetValue()
					}
				}
			}
		}
	}

	if total := s.Cache.Hits + s.Cache.Misses; total > 0 {
		s.Cache.HitRate = s.Cache.Hits / total
	}

	if dbPoolSource != nil {
		st := dbPoolSource()
		s.DBPool.Total = float64(st.Total)
		s.DBPool.InUse = float64(st.InUse)
		s.DBPool.Idle = float64(st.Idle)
	}

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	s.Runtime.Goroutines = float64(runtime.NumGoroutine())
	s.Runtime.AllocMB = float64(ms.Alloc) / (1024 * 1024)
	s.Runtime.NumGC = float64(ms.NumGC)

	return s
}

func labelValue(m *dto.Metric, name string) string {
	for _, l := range m.GetLabel() {
		if l.GetName() == name {
			return l.GetValue()
		}
	}
	return ""
}

func firstCounter(mf *dto.MetricFamily) float64 {
	if ms := mf.GetMetric(); len(ms) > 0 {
		return ms[0].GetCounter().GetValue()
	}
	return 0
}

func firstGauge(mf *dto.MetricFamily) float64 {
	if ms := mf.GetMetric(); len(ms) > 0 {
		return ms[0].GetGauge().GetValue()
	}
	return 0
}

// statusClass превращает "200"/"404"/"503" в "2xx"/"4xx"/"5xx".
func statusClass(status string) string {
	if len(status) == 0 {
		return ""
	}
	switch status[0] {
	case '1', '2', '3', '4', '5':
		return string(status[0]) + "xx"
	default:
		return ""
	}
}
