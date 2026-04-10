package server

import (
	"expvar"
	"net/http"
	"sync/atomic"
	"time"
)

// serverMetrics holds all exported metrics for the REST server.
// Values are published via expvar and exposed at GET /metrics.
type serverMetrics struct {
	requestsTotal  *expvar.Map // key: "METHOD /path status=NNN"
	requestLatency *expvar.Map // key: "METHOD /path" → cumulative µs (atomic int)
	factsTotal     *expvar.Map // key: agentID → count
	recallTotal    *expvar.Map // key: agentID → count
}

// getOrNewMap returns the existing expvar.Map for name, or creates a new one.
// expvar.NewMap panics on duplicate registration (global singleton), so we must
// guard against multiple servers sharing the same process (e.g. in tests).
func getOrNewMap(name string) *expvar.Map {
	if v := expvar.Get(name); v != nil {
		if m, ok := v.(*expvar.Map); ok {
			return m
		}
	}
	return expvar.NewMap(name)
}

func newServerMetrics(name string) *serverMetrics {
	return &serverMetrics{
		requestsTotal:  getOrNewMap(name + ".requests_total"),
		requestLatency: getOrNewMap(name + ".request_latency_us"),
		factsTotal:     getOrNewMap(name + ".facts_total"),
		recallTotal:    getOrNewMap(name + ".recall_total"),
	}
}

func (m *serverMetrics) recordRequest(method, path string, status int, d time.Duration) {
	key := method + " " + path
	m.requestsTotal.Add(key, 1)
	// Accumulate latency in microseconds using an atomic int stored in the map.
	m.requestLatency.Add(key, d.Microseconds())
}

func (m *serverMetrics) recordFact(agentID string) {
	m.factsTotal.Add(agentID, 1)
}

func (m *serverMetrics) recordRecall(agentID string) {
	m.recallTotal.Add(agentID, 1)
}

// metricsHandler wraps expvar.Handler so it can be registered on our mux.
func metricsHandler() http.Handler {
	return expvar.Handler()
}

// instrumentedResponseWriter captures the status code and measures duration.
type instrumentedResponseWriter struct {
	http.ResponseWriter
	status    int
	startedAt time.Time
	written   atomic.Bool
}

func newInstrumentedRW(w http.ResponseWriter) *instrumentedResponseWriter {
	return &instrumentedResponseWriter{
		ResponseWriter: w,
		status:         http.StatusOK,
		startedAt:      time.Now(),
	}
}

func (rw *instrumentedResponseWriter) WriteHeader(code int) {
	if rw.written.CompareAndSwap(false, true) {
		rw.status = code
		rw.ResponseWriter.WriteHeader(code)
	}
}

func (rw *instrumentedResponseWriter) elapsed() time.Duration {
	return time.Since(rw.startedAt)
}
