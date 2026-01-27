package observability

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

// MetricsCollector provides Prometheus-style metrics in a simple format
type MetricsCollector struct {
	counters   map[string]*Counter
	gauges     map[string]*Gauge
	histograms map[string]*Histogram
	mu         sync.RWMutex
}

// Counter tracks cumulative values
type Counter struct {
	value float64
	mu    sync.Mutex
}

// Gauge tracks current values
type Gauge struct {
	value float64
	mu    sync.Mutex
}

// Histogram tracks distribution of values
type Histogram struct {
	sum   float64
	count uint64
	mu    sync.Mutex
}

var (
	defaultCollector *MetricsCollector
	once             sync.Once
)

// GetCollector returns the singleton metrics collector
func GetCollector() *MetricsCollector {
	once.Do(func() {
		defaultCollector = &MetricsCollector{
			counters:   make(map[string]*Counter),
			gauges:     make(map[string]*Gauge),
			histograms: make(map[string]*Histogram),
		}
	})
	return defaultCollector
}

// Counter methods
func (c *Counter) Inc() {
	c.Add(1)
}

func (c *Counter) Add(val float64) {
	c.mu.Lock()
	c.value += val
	c.mu.Unlock()
}

func (c *Counter) Value() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.value
}

// Gauge methods
func (g *Gauge) Set(val float64) {
	g.mu.Lock()
	g.value = val
	g.mu.Unlock()
}

func (g *Gauge) Inc() {
	g.Add(1)
}

func (g *Gauge) Dec() {
	g.Add(-1)
}

func (g *Gauge) Add(val float64) {
	g.mu.Lock()
	g.value += val
	g.mu.Unlock()
}

func (g *Gauge) Value() float64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.value
}

// Histogram methods
func (h *Histogram) Observe(val float64) {
	h.mu.Lock()
	h.sum += val
	h.count++
	h.mu.Unlock()
}

func (h *Histogram) Sum() float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sum
}

func (h *Histogram) Count() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.count
}

func (h *Histogram) Avg() float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.count == 0 {
		return 0
	}
	return h.sum / float64(h.count)
}

// MetricsCollector methods
func (m *MetricsCollector) Counter(name string) *Counter {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.counters[name]; ok {
		return c
	}
	c := &Counter{}
	m.counters[name] = c
	return c
}

func (m *MetricsCollector) Gauge(name string) *Gauge {
	m.mu.Lock()
	defer m.mu.Unlock()
	if g, ok := m.gauges[name]; ok {
		return g
	}
	g := &Gauge{}
	m.gauges[name] = g
	return g
}

func (m *MetricsCollector) Histogram(name string) *Histogram {
	m.mu.Lock()
	defer m.mu.Unlock()
	if h, ok := m.histograms[name]; ok {
		return h
	}
	h := &Histogram{}
	m.histograms[name] = h
	return h
}

// Timer measures duration and records to histogram
func (m *MetricsCollector) Timer(name string) func() {
	start := time.Now()
	return func() {
		duration := time.Since(start).Seconds()
		m.Histogram(name).Observe(duration)
	}
}

// Handler returns HTTP handler for /metrics endpoint
func (m *MetricsCollector) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")

		m.mu.RLock()
		defer m.mu.RUnlock()

		// Write counters
		for name, counter := range m.counters {
			fmt.Fprintf(w, "# TYPE %s counter\n", name)
			fmt.Fprintf(w, "%s %.2f\n", name, counter.Value())
		}

		// Write gauges
		for name, gauge := range m.gauges {
			fmt.Fprintf(w, "# TYPE %s gauge\n", name)
			fmt.Fprintf(w, "%s %.2f\n", name, gauge.Value())
		}

		// Write histograms
		for name, histogram := range m.histograms {
			fmt.Fprintf(w, "# TYPE %s histogram\n", name)
			fmt.Fprintf(w, "%s_sum %.6f\n", name, histogram.Sum())
			fmt.Fprintf(w, "%s_count %d\n", name, histogram.Count())
			fmt.Fprintf(w, "%s_avg %.6f\n", name, histogram.Avg())
		}
	}
}

// Predefined metric names
const (
	// Data Collector metrics
	MetricCandlesReceived     = "data_collector_candles_received_total"
	MetricCandlesPublished    = "data_collector_candles_published_total"
	MetricWSConnections       = "data_collector_websocket_connections"
	MetricWSReconnects        = "data_collector_websocket_reconnects_total"
	MetricWSErrors            = "data_collector_websocket_errors_total"

	// Metrics Calculator metrics
	MetricCandlesProcessed      = "metrics_calculator_candles_processed_total"
	MetricMetricsCalculated     = "metrics_calculator_metrics_calculated_total"
	MetricDBInsertDuration      = "metrics_calculator_db_insert_duration_seconds"
	MetricCalculationDuration   = "metrics_calculator_calculation_duration_seconds"
	MetricRingBufferSize        = "metrics_calculator_ring_buffer_size"

	// Alert Engine metrics
	MetricAlertsEvaluated       = "alert_engine_alerts_evaluated_total"
	MetricAlertsTriggered       = "alert_engine_alerts_triggered_total"
	MetricAlertsDuplicated      = "alert_engine_alerts_duplicated_total"
	MetricEvaluationDuration    = "alert_engine_evaluation_duration_seconds"
	MetricWebhooksSent          = "alert_engine_webhooks_sent_total"
	MetricWebhooksFailed        = "alert_engine_webhooks_failed_total"

	// API Gateway metrics
	MetricHTTPRequests          = "api_gateway_http_requests_total"
	MetricHTTPDuration          = "api_gateway_http_duration_seconds"
	MetricWSClientConnections   = "api_gateway_websocket_connections"
	MetricWSMessagesSent        = "api_gateway_websocket_messages_sent_total"
	MetricWSMessagesFailed      = "api_gateway_websocket_messages_failed_total"

	// NATS metrics
	MetricNATSMessagesPublished = "nats_messages_published_total"
	MetricNATSMessagesReceived  = "nats_messages_received_total"
	MetricNATSPublishErrors     = "nats_publish_errors_total"

	// Database metrics
	MetricDBQueries             = "database_queries_total"
	MetricDBErrors              = "database_errors_total"
	MetricDBConnectionPool      = "database_connection_pool_size"
)
