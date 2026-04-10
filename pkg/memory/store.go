package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/angelnicolasc/graymatter/pkg/embedding"
)

var (
	bucketFacts    = []byte("facts")
	bucketSessions = []byte("sessions")
	bucketMeta     = []byte("meta")
	bucketAgents   = []byte("agents")
)

// SharedAgentID is the reserved agent ID for the shared memory namespace.
// Facts stored here are readable by all agents via RecallShared and RecallAll.
//
// Concurrency note: bbolt serialises concurrent write access via a file-level
// lock. Multiple processes writing shared memory will serialise, not race.
// Concurrent reads are always safe.
const SharedAgentID = "__shared__"

// StoreConfig is passed to Open to configure the Store.
type StoreConfig struct {
	DataDir       string
	Embedder      embedding.Provider
	DecayHalfLife time.Duration

	// VectorBackend overrides the default chromem-go vector store.
	// If nil, a persistent chromem-go instance is created under DataDir/vectors.
	// Use this to plug in Qdrant, Weaviate, pgvector, or any VectorStore impl.
	VectorBackend VectorStore

	// MaxAsyncConsolidations bounds concurrent background consolidations.
	// 0 is normalised to 2 by Open().
	MaxAsyncConsolidations int

	// OnConsolidateError is called when an async consolidation goroutine errors.
	// If nil, errors are discarded. Must be safe for concurrent use.
	OnConsolidateError func(agentID string, err error)

	// OnRecall, if non-nil, is called after each Recall with timing and count.
	OnRecall func(agentID, query string, resultCount int, duration time.Duration)

	// OnPut, if non-nil, is called after each successful Put.
	OnPut func(agentID, factID string, duration time.Duration)

	// Logger receives structured log events. Uses log.Default() if nil.
	Logger interface {
		Printf(format string, v ...any)
	}
}

// GraphAccessor is a narrow interface that pkg/memory uses to interact with
// the knowledge graph without importing pkg/kg (prevents import cycles).
type GraphAccessor interface {
	// Upsert inserts or updates a node in the graph.
	UpsertNode(id, label, entityType string) error
	// NeighborTexts returns text labels of nodes reachable from nodeID within depth hops.
	NeighborTexts(nodeID string, depth int) ([]string, error)
}

// EntityExtractorAccessor extracts entities from a text string.
// Implemented by pkg/kg.EntityExtractor.
type EntityExtractorAccessor interface {
	ExtractIDs(text string) ([]string, error) // returns canonical node IDs
}

// Store is the central storage layer. It combines bbolt for durable
// structured storage with a pluggable VectorStore for similarity search.
// All public methods are safe for concurrent use.
type Store struct {
	db      *bolt.DB
	vectors VectorStore
	embedder embedding.Provider
	cfg      StoreConfig

	mu sync.RWMutex

	// graph and extractor are set via SetKG after Open().
	// They are optional; Consolidate and Recall work without them.
	graph     GraphAccessor
	extractor EntityExtractorAccessor

	// Goroutine lifecycle. All goroutines launched by Store must acquire sema
	// and register with wg before doing work. Close() cancels shutdownCtx,
	// then waits for wg to reach zero before closing bbolt.
	shutdownCtx    context.Context
	shutdownCancel context.CancelFunc
	wg             sync.WaitGroup
	sema           chan struct{} // bounded semaphore; cap = MaxAsyncConsolidations
}

// Open creates or opens the GrayMatter store at cfg.DataDir.
func Open(cfg StoreConfig) (*Store, error) {
	if cfg.MaxAsyncConsolidations <= 0 {
		cfg.MaxAsyncConsolidations = 2
	}

	dbPath := filepath.Join(cfg.DataDir, "gray.db")
	db, err := bolt.Open(dbPath, 0o600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open bbolt: %w", err)
	}

	// Ensure top-level buckets exist.
	if err := db.Update(func(tx *bolt.Tx) error {
		for _, name := range [][]byte{bucketFacts, bucketSessions, bucketMeta, bucketAgents} {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init buckets: %w", err)
	}

	// Use the caller-supplied vector backend, or default to chromem-go.
	vectors := cfg.VectorBackend
	if vectors == nil {
		v, err := newChromemVectorStore(cfg.DataDir)
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("open vector store: %w", err)
		}
		vectors = v
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := &Store{
		db:             db,
		vectors:        vectors,
		embedder:       cfg.Embedder,
		cfg:            cfg,
		shutdownCtx:    ctx,
		shutdownCancel: cancel,
		sema:           make(chan struct{}, cfg.MaxAsyncConsolidations),
	}

	// Hydrate known agent IDs so collections are ready.
	_ = s.loadAgents()

	// Validate embedding dimensions against the stored value; warn on mismatch.
	if cfg.Embedder != nil {
		s.checkEmbedDimensions(cfg.Embedder)
	}

	// Re-index any facts that are in bbolt but missing from the vector store
	// (e.g. after a crash between a bbolt commit and the vector write).
	s.reconcileVectors()

	return s, nil
}

// Put stores a new observation for agentID.
func (s *Store) Put(ctx context.Context, agentID, text string) error {
	start := time.Now()

	var emb []float32
	if s.embedder != nil {
		var err error
		emb, err = s.embedder.Embed(ctx, text)
		if err != nil {
			// Non-fatal: fall back to keyword-only for this fact.
			emb = nil
		}
	}

	f := newFact(agentID, text, emb)

	// Persist to bbolt.
	if err := s.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.Bucket(bucketFacts).CreateBucketIfNotExists([]byte(agentID))
		if err != nil {
			return err
		}
		data, err := f.marshal()
		if err != nil {
			return err
		}
		if err := b.Put([]byte(f.ID), data); err != nil {
			return err
		}
		// Register agent.
		return tx.Bucket(bucketAgents).Put([]byte(agentID), []byte("1"))
	}); err != nil {
		return fmt.Errorf("put fact: %w", err)
	}

	// Add to vector index if we have an embedding.
	if len(emb) > 0 {
		s.recordEmbedDimensions(len(emb))
		if err := s.addToVector(ctx, agentID, f); err != nil {
			// Non-fatal: bbolt write already succeeded.
			_ = err
		}
	}

	if s.cfg.OnPut != nil {
		s.cfg.OnPut(agentID, f.ID, time.Since(start))
	}
	return nil
}

// Delete removes a fact by ID for agentID.
func (s *Store) Delete(agentID, factID string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketFacts).Bucket([]byte(agentID))
		if b == nil {
			return nil
		}
		return b.Delete([]byte(factID))
	})
}

// List returns all facts for agentID, sorted newest first.
func (s *Store) List(agentID string) ([]Fact, error) {
	var facts []Fact
	if err := s.db.View(func(tx *bolt.Tx) error {
		parent := tx.Bucket(bucketFacts)
		b := parent.Bucket([]byte(agentID))
		if b == nil {
			return nil
		}
		return b.ForEach(func(_, v []byte) error {
			f, err := unmarshalFact(v)
			if err != nil {
				return nil // skip corrupt entries
			}
			facts = append(facts, f)
			return nil
		})
	}); err != nil {
		return nil, err
	}
	// Sort newest first.
	sortFactsByTime(facts)
	return facts, nil
}

// ListAgents returns all known agent IDs.
func (s *Store) ListAgents() ([]string, error) {
	var agents []string
	if err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketAgents).ForEach(func(k, _ []byte) error {
			agents = append(agents, string(k))
			return nil
		})
	}); err != nil {
		return nil, err
	}
	return agents, nil
}

// Stats returns aggregate statistics for agentID.
func (s *Store) Stats(agentID string) (MemoryStats, error) {
	facts, err := s.List(agentID)
	if err != nil {
		return MemoryStats{}, err
	}
	st := MemoryStats{AgentID: agentID, FactCount: len(facts)}
	if len(facts) == 0 {
		return st, nil
	}
	var weightSum float64
	st.OldestAt = facts[0].CreatedAt
	st.NewestAt = facts[0].CreatedAt
	for _, f := range facts {
		weightSum += f.Weight
		if f.CreatedAt.Before(st.OldestAt) {
			st.OldestAt = f.CreatedAt
		}
		if f.CreatedAt.After(st.NewestAt) {
			st.NewestAt = f.CreatedAt
		}
	}
	st.AvgWeight = weightSum / float64(len(facts))
	return st, nil
}

// UpdateFact persists a modified fact (used by consolidation + decay).
func (s *Store) UpdateFact(agentID string, f Fact) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketFacts).Bucket([]byte(agentID))
		if b == nil {
			return nil
		}
		data, err := f.marshal()
		if err != nil {
			return err
		}
		return b.Put([]byte(f.ID), data)
	})
}

// Close signals all background goroutines to stop, waits for them to exit,
// then flushes and closes the underlying stores.
func (s *Store) Close() error {
	s.shutdownCancel()
	s.wg.Wait()
	_ = s.vectors.Close()
	return s.db.Close()
}

// DB exposes the raw bbolt handle (used by session package).
func (s *Store) DB() *bolt.DB {
	return s.db
}

// PutShared stores a new observation in the shared memory namespace.
// Shared facts are accessible to all agents via RecallShared and RecallAll.
func (s *Store) PutShared(ctx context.Context, text string) error {
	return s.Put(ctx, SharedAgentID, text)
}

// RecallShared returns the top-k most relevant shared facts for query.
func (s *Store) RecallShared(ctx context.Context, query string, topK int) ([]string, error) {
	return s.Recall(ctx, SharedAgentID, query, topK)
}

// RecallAll merges agent-scoped and shared-scoped results, deduplicates, and
// re-ranks by Reciprocal Rank Fusion. It returns at most topK combined facts.
func (s *Store) RecallAll(ctx context.Context, agentID, query string, topK int) ([]string, error) {
	agentResults, err := s.Recall(ctx, agentID, query, topK)
	if err != nil {
		return nil, fmt.Errorf("recall agent: %w", err)
	}
	sharedResults, err := s.Recall(ctx, SharedAgentID, query, topK)
	if err != nil {
		return nil, fmt.Errorf("recall shared: %w", err)
	}

	// Deduplicate and merge, preserving agent-scoped results first.
	seen := make(map[string]bool, len(agentResults)+len(sharedResults))
	merged := make([]string, 0, len(agentResults)+len(sharedResults))
	for _, f := range agentResults {
		if !seen[f] {
			seen[f] = true
			merged = append(merged, f)
		}
	}
	for _, f := range sharedResults {
		if !seen[f] {
			seen[f] = true
			merged = append(merged, f)
		}
	}
	if len(merged) > topK {
		merged = merged[:topK]
	}
	return merged, nil
}

// SetKG wires an optional knowledge graph and entity extractor into the store.
// Call this after Open() to enable graph enrichment in Recall and Consolidate.
// Both arguments are optional; pass nil to disable the corresponding feature.
func (s *Store) SetKG(graph GraphAccessor, extractor EntityExtractorAccessor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.graph = graph
	s.extractor = extractor
}

// --- internal helpers ---

func (s *Store) loadAgents() error {
	agents, err := s.ListAgents()
	if err != nil {
		return err
	}
	for _, id := range agents {
		_ = s.vectors.EnsureCollection(id)
	}
	return nil
}

// reconcileVectors ensures every bbolt fact with an embedding is present in
// the vector store. Called once at Open() to repair divergences caused by crashes
// between the bbolt write and the vector write.
// Best-effort: individual errors are silently ignored (bbolt is source of truth).
// AddDocument is idempotent (overwrites on duplicate ID).
func (s *Store) reconcileVectors() {
	agents, err := s.ListAgents()
	if err != nil {
		return
	}
	ctx := context.Background()
	for _, agentID := range agents {
		facts, err := s.List(agentID)
		if err != nil {
			continue
		}
		for _, f := range facts {
			if len(f.Embedding) == 0 {
				continue
			}
			_ = s.addToVector(ctx, agentID, f)
		}
	}
}

// checkEmbedDimensions reads the stored embedding dimension from the meta bucket
// and warns if the current provider's dimension differs. On first use it records
// the current dimension so future opens can detect provider switches.
func (s *Store) checkEmbedDimensions(emb embedding.Provider) {
	const metaKeyDims = "embed_dims"
	currentDims := emb.Dimensions()
	if currentDims <= 0 {
		return // provider doesn't know its dims yet (e.g. Ollama before first call)
	}

	_ = s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketMeta)
		stored := b.Get([]byte(metaKeyDims))
		if stored == nil {
			val, _ := json.Marshal(currentDims)
			return b.Put([]byte(metaKeyDims), val)
		}
		var storedDims int
		if err := json.Unmarshal(stored, &storedDims); err != nil {
			return nil
		}
		if storedDims != currentDims {
			log.Printf("graymatter: WARNING embedding dimension mismatch: stored=%d current=%d (provider=%s). "+
				"Vector search results may be inaccurate. Consider re-indexing your data.",
				storedDims, currentDims, emb.Name())
		}
		return nil
	})
}

// recordEmbedDimensions writes the embedding dimension to meta if not already set.
// Called the first time a fact with an embedding is persisted.
func (s *Store) recordEmbedDimensions(dims int) {
	const metaKeyDims = "embed_dims"
	_ = s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketMeta)
		if b.Get([]byte(metaKeyDims)) != nil {
			return nil // already recorded
		}
		val, _ := json.Marshal(dims)
		return b.Put([]byte(metaKeyDims), val)
	})
}

func (s *Store) addToVector(ctx context.Context, agentID string, f Fact) error {
	metadata := map[string]string{
		"agent_id":   agentID,
		"created_at": f.CreatedAt.Format(time.RFC3339),
	}
	return s.vectors.AddDocument(ctx, agentID, f.ID, f.Text, f.Embedding, metadata)
}

// vectorSearch returns at most n results from the vector index.
func (s *Store) vectorSearch(ctx context.Context, agentID, query string, n int) ([]VectorResult, error) {
	if s.embedder == nil {
		return nil, nil
	}
	qEmb, err := s.embedder.Embed(ctx, query)
	if err != nil || len(qEmb) == 0 {
		return nil, nil
	}
	return s.vectors.Query(ctx, agentID, qEmb, n)
}

// marshalJSON helper for meta bucket.
func marshalJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}

func sortFactsByTime(facts []Fact) {
	for i := 1; i < len(facts); i++ {
		for j := i; j > 0 && facts[j].CreatedAt.After(facts[j-1].CreatedAt); j-- {
			facts[j], facts[j-1] = facts[j-1], facts[j]
		}
	}
}
