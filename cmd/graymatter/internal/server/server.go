// Package server exposes GrayMatter memory operations over a minimal HTTP/JSON
// REST API. It is intentionally small — its only purpose is to let non-Go
// processes (Python scripts, shell agents, etc.) interact with the same bbolt
// store that the CLI uses.
//
// Routes:
//
//	POST   /remember           body: {"agent":"<id>","text":"<text>"}
//	GET    /recall?agent=<id>&q=<query>[&k=<int>]
//	POST   /consolidate        body: {"agent":"<id>"}  (requires ANTHROPIC_API_KEY env var)
//	GET    /facts?agent=<id>[&limit=<int>]
//	DELETE /forget             body: {"agent":"<id>","query":"<query>"}
//	GET    /healthz
package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/angelnicolasc/graymatter/pkg/embedding"
	"github.com/angelnicolasc/graymatter/pkg/memory"
)

const (
	defaultTopK   = 5
	defaultLimit  = 50
	readTimeout   = 15 * time.Second
	writeTimeout  = 30 * time.Second
	idleTimeout   = 60 * time.Second
	shutdownGrace = 5 * time.Second
)

// Server wraps an HTTP server backed by a GrayMatter memory store.
// The store is opened once at construction time and shared across all
// requests; call Shutdown to close it.
type Server struct {
	httpSrv *http.Server
	store   *memory.Store // nil if Open failed; handlers return 503 in that case
	metrics *serverMetrics
	dataDir string
	addr    string
	logger  *slog.Logger
}

// New creates a Server that opens the memory store at dataDir and will listen
// on addr (e.g. ":8080"). The store is opened once and reused for all requests.
func New(addr, dataDir string, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}

	emb := embedding.AutoDetect(embedding.Config{Mode: embedding.ModeKeyword})
	store, err := memory.Open(memory.StoreConfig{
		DataDir:  dataDir,
		Embedder: emb,
	})
	if err != nil {
		logger.Error("graymatter server: failed to open store", "error", err)
		// store remains nil; storeReady() will reject all data requests with 503.
	}

	m := newServerMetrics("graymatter_server")
	s := &Server{
		store:   store,
		metrics: m,
		dataDir: dataDir,
		addr:    addr,
		logger:  logger,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("POST /remember", s.handleRemember)
	mux.HandleFunc("GET /recall", s.handleRecall)
	mux.HandleFunc("POST /consolidate", s.handleConsolidate)
	mux.HandleFunc("GET /facts", s.handleFacts)
	mux.HandleFunc("DELETE /forget", s.handleForget)
	mux.Handle("GET /metrics", metricsHandler())

	s.httpSrv = &http.Server{
		Addr:         addr,
		Handler:      s.loggingMiddleware(mux),
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
	}
	return s
}

// Addr returns the address the server is listening on.
func (s *Server) Addr() string { return s.addr }

// ListenAndServe starts the HTTP server. Blocks until shutdown.
func (s *Server) ListenAndServe() error {
	s.logger.Info("graymatter REST API listening", "addr", s.addr)
	return s.httpSrv.ListenAndServe()
}

// Serve accepts connections on l. Used in tests to bind to a free port.
func (s *Server) Serve(l net.Listener) error {
	s.addr = l.Addr().String()
	s.logger.Info("graymatter REST API listening", "addr", s.addr)
	return s.httpSrv.Serve(l)
}

// Shutdown gracefully stops the HTTP server and closes the underlying store.
func (s *Server) Shutdown(ctx context.Context) error {
	shutCtx, cancel := context.WithTimeout(ctx, shutdownGrace)
	defer cancel()
	httpErr := s.httpSrv.Shutdown(shutCtx)
	if s.store != nil {
		if storeErr := s.store.Close(); storeErr != nil && httpErr == nil {
			return storeErr
		}
	}
	return httpErr
}

// storeReady returns false and writes a 503 if the store failed to open.
func (s *Server) storeReady(w http.ResponseWriter) bool {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "store unavailable")
		return false
	}
	return true
}

// --- handlers ---

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type rememberRequest struct {
	Agent string `json:"agent"`
	Text  string `json:"text"`
}

func (s *Server) handleRemember(w http.ResponseWriter, r *http.Request) {
	var req rememberRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Agent == "" || req.Text == "" {
		writeError(w, http.StatusBadRequest, "agent and text are required")
		return
	}
	if !s.storeReady(w) {
		return
	}
	if err := s.store.Put(r.Context(), req.Agent, req.Text); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.metrics.recordFact(req.Agent)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleRecall(w http.ResponseWriter, r *http.Request) {
	agent := r.URL.Query().Get("agent")
	query := r.URL.Query().Get("q")
	if agent == "" || query == "" {
		writeError(w, http.StatusBadRequest, "agent and q query params are required")
		return
	}
	topK := defaultTopK
	if ks := r.URL.Query().Get("k"); ks != "" {
		if v, err := strconv.Atoi(ks); err == nil && v > 0 {
			topK = v
		}
	}
	if !s.storeReady(w) {
		return
	}
	results, err := s.store.Recall(r.Context(), agent, query, topK)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.metrics.recordRecall(agent)
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

type consolidateRequest struct {
	Agent string `json:"agent"`
}

func (s *Server) handleConsolidate(w http.ResponseWriter, r *http.Request) {
	var req consolidateRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Agent == "" {
		writeError(w, http.StatusBadRequest, "agent is required")
		return
	}
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		writeError(w, http.StatusServiceUnavailable, "ANTHROPIC_API_KEY not set; consolidate requires LLM access")
		return
	}
	if !s.storeReady(w) {
		return
	}
	cfg := &restConsolidateCfg{apiKey: apiKey}
	if err := s.store.Consolidate(r.Context(), req.Agent, cfg); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleFacts(w http.ResponseWriter, r *http.Request) {
	agent := r.URL.Query().Get("agent")
	if agent == "" {
		writeError(w, http.StatusBadRequest, "agent query param is required")
		return
	}
	limit := defaultLimit
	if ls := r.URL.Query().Get("limit"); ls != "" {
		if v, err := strconv.Atoi(ls); err == nil && v > 0 {
			limit = v
		}
	}
	if !s.storeReady(w) {
		return
	}
	facts, err := s.store.List(agent)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(facts) > limit {
		facts = facts[:limit]
	}

	// Return only the fields needed by external callers.
	type factView struct {
		ID        string    `json:"id"`
		Text      string    `json:"text"`
		Weight    float64   `json:"weight"`
		CreatedAt time.Time `json:"created_at"`
	}
	views := make([]factView, len(facts))
	for i, f := range facts {
		views[i] = factView{ID: f.ID, Text: f.Text, Weight: f.Weight, CreatedAt: f.CreatedAt}
	}
	writeJSON(w, http.StatusOK, map[string]any{"facts": views})
}

type forgetRequest struct {
	Agent string `json:"agent"`
	Query string `json:"query"`
}

// handleForget deletes the single most-similar fact to the query.
func (s *Server) handleForget(w http.ResponseWriter, r *http.Request) {
	var req forgetRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Agent == "" || req.Query == "" {
		writeError(w, http.StatusBadRequest, "agent and query are required")
		return
	}
	if !s.storeReady(w) {
		return
	}
	// Recall 1 result to find the best match, then delete its fact ID.
	results, err := s.store.Recall(r.Context(), req.Agent, req.Query, 1)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(results) == 0 {
		writeJSON(w, http.StatusOK, map[string]string{"status": "not_found"})
		return
	}

	// Find the fact ID by matching the recalled text.
	facts, err := s.store.List(req.Agent)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, f := range facts {
		if f.Text == results[0] {
			if err := s.store.Delete(req.Agent, f.ID); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "deleted_id": f.ID})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "not_found"})
}

// restConsolidateCfg is a minimal ConsolidateConfig used by the REST layer.
type restConsolidateCfg struct {
	apiKey string
}

func (c *restConsolidateCfg) GetAnthropicAPIKey() string     { return c.apiKey }
func (c *restConsolidateCfg) GetConsolidateLLM() string      { return "anthropic" }
func (c *restConsolidateCfg) GetConsolidateModel() string    { return "claude-haiku-4-5-20251001" }
func (c *restConsolidateCfg) GetConsolidateThreshold() int   { return 20 }
func (c *restConsolidateCfg) GetDecayHalfLife() time.Duration { return 168 * time.Hour } // 1 week

// --- HTTP utilities ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // headers already sent; encoding errors are unrecoverable
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := newInstrumentedRW(w)
		next.ServeHTTP(rw, r)
		elapsed := rw.elapsed()
		s.logger.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.status,
			"duration", elapsed.String(),
		)
		s.metrics.recordRequest(r.Method, r.URL.Path, rw.status, elapsed)
	})
}
