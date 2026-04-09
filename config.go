package graymatter

import (
	"os"
	"time"
)

// EmbeddingMode controls how GrayMatter generates vector embeddings.
type EmbeddingMode int

const (
	// EmbeddingAuto detects the best available provider at runtime.
	EmbeddingAuto EmbeddingMode = iota
	// EmbeddingOllama forces Ollama (fails if not reachable).
	EmbeddingOllama
	// EmbeddingAnthropic forces Anthropic API (requires ANTHROPIC_API_KEY).
	EmbeddingAnthropic
	// EmbeddingKeyword disables vector search; uses keyword+recency only.
	EmbeddingKeyword
)

// Config holds all GrayMatter configuration. All fields have sane defaults.
type Config struct {
	// DataDir is the directory where gray.db and vector files are stored.
	// Default: ".graymatter"
	DataDir string

	// TopK is the maximum number of facts returned by Recall.
	// Default: 8
	TopK int

	// EmbeddingMode controls which embedding backend is used.
	// Default: EmbeddingAuto
	EmbeddingMode EmbeddingMode

	// OllamaURL is the base URL of the Ollama API.
	// Default: "http://localhost:11434"
	OllamaURL string

	// OllamaModel is the embedding model used with Ollama.
	// Default: "nomic-embed-text"
	OllamaModel string

	// AnthropicAPIKey for the Anthropic embeddings endpoint.
	// Default: value of ANTHROPIC_API_KEY env var.
	AnthropicAPIKey string

	// ConsolidateLLM specifies which LLM provider drives consolidation.
	// Values: "anthropic", "ollama", "" (disable consolidation).
	// Default: "anthropic" if key present, else "ollama" if reachable, else ""
	ConsolidateLLM string

	// ConsolidateModel is the model used for consolidation summarisation.
	// Default: "claude-haiku-4-5-20251001" (fast + cheap)
	ConsolidateModel string

	// ConsolidateThreshold is the fact count that triggers consolidation.
	// Default: 100
	ConsolidateThreshold int

	// DecayHalfLife is the half-life for the exponential weight decay curve.
	// Facts not accessed within this window lose half their weight.
	// Default: 720h (30 days)
	DecayHalfLife time.Duration

	// AsyncConsolidate runs consolidation in a background goroutine after Remember.
	// Default: true
	AsyncConsolidate bool
}

// DefaultConfig returns a Config with all defaults applied.
func DefaultConfig() Config {
	return Config{
		DataDir:              ".graymatter",
		TopK:                 8,
		EmbeddingMode:        EmbeddingAuto,
		OllamaURL:            envOrDefault("GRAYMATTER_OLLAMA_URL", "http://localhost:11434"),
		OllamaModel:          envOrDefault("GRAYMATTER_OLLAMA_MODEL", "nomic-embed-text"),
		AnthropicAPIKey:      os.Getenv("ANTHROPIC_API_KEY"),
		ConsolidateLLM:       "",
		ConsolidateModel:     "claude-haiku-4-5-20251001",
		ConsolidateThreshold: 100,
		DecayHalfLife:        720 * time.Hour,
		AsyncConsolidate:     true,
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Config implements memory.ConsolidateConfig so it can be passed directly
// to Store.Consolidate / Store.MaybeConsolidate without an adapter.

func (c Config) GetAnthropicAPIKey() string      { return c.AnthropicAPIKey }
func (c Config) GetConsolidateLLM() string       { return c.ConsolidateLLM }
func (c Config) GetConsolidateModel() string     { return c.ConsolidateModel }
func (c Config) GetConsolidateThreshold() int    { return c.ConsolidateThreshold }
func (c Config) GetDecayHalfLife() time.Duration { return c.DecayHalfLife }
