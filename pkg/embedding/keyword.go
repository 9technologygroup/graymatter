package embedding

import "context"

// KeywordProvider is a no-op embedding provider.
// When active, GrayMatter falls back to pure keyword + recency retrieval.
// No network calls. No dependencies. Always works.
type KeywordProvider struct{}

// NewKeyword returns the keyword-only fallback provider.
func NewKeyword() *KeywordProvider { return &KeywordProvider{} }

// Embed always returns nil, nil — signals keyword-only mode to recall.go.
func (k *KeywordProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, nil
}

func (k *KeywordProvider) Dimensions() int { return 0 }
func (k *KeywordProvider) Name() string    { return "keyword-only" }
