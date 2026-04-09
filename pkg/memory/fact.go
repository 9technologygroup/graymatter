package memory

import (
	"encoding/json"
	"time"

	"github.com/oklog/ulid/v2"
)

// Fact is a single unit of memory: a piece of text an agent observed,
// enriched with metadata used for retrieval scoring and decay.
type Fact struct {
	ID          string    `json:"id"`
	AgentID     string    `json:"agent_id"`
	Text        string    `json:"text"`
	CreatedAt   time.Time `json:"created_at"`
	AccessedAt  time.Time `json:"accessed_at"`
	AccessCount int       `json:"access_count"`
	// Weight is the decay-adjusted relevance score in [0, 1].
	// New facts start at 1.0 and decay over time via the forgetting curve.
	Weight    float64   `json:"weight"`
	Embedding []float32 `json:"embedding,omitempty"`
}

// newFact creates a Fact with a new ULID and weight=1.0.
func newFact(agentID, text string, embedding []float32) Fact {
	now := time.Now().UTC()
	return Fact{
		ID:          ulid.Make().String(),
		AgentID:     agentID,
		Text:        text,
		CreatedAt:   now,
		AccessedAt:  now,
		AccessCount: 0,
		Weight:      1.0,
		Embedding:   embedding,
	}
}

// marshal serialises a Fact to JSON bytes for bbolt storage.
func (f Fact) marshal() ([]byte, error) {
	return json.Marshal(f)
}

// unmarshalFact deserialises a Fact from JSON bytes.
func unmarshalFact(data []byte) (Fact, error) {
	var f Fact
	if err := json.Unmarshal(data, &f); err != nil {
		return Fact{}, err
	}
	return f, nil
}

// MemoryStats holds aggregate statistics for a single agent.
type MemoryStats struct {
	AgentID   string    `json:"agent_id"`
	FactCount int       `json:"fact_count"`
	OldestAt  time.Time `json:"oldest_at"`
	NewestAt  time.Time `json:"newest_at"`
	AvgWeight float64   `json:"avg_weight"`
}
