package harness

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

// bucketTokenUsage holds per-agent/per-model/per-day token rollups. Every key
// is `{agentID}|{model}|{yyyymmdd}` and every value is a JSON TokenUsage.
// Storing pre-aggregated day rows keeps the hot path tiny (one bbolt Put per
// API call) and the read path trivially bounded (≤ agents × models × 30 rows
// for a 30-day panel).
var bucketTokenUsage = []byte("token_usage")

// TokenUsage is the aggregated token ledger row for one (agent, model, day)
// triple. All counters are cumulative Σ; RecordTokenUsage adds, never
// replaces.
type TokenUsage struct {
	AgentID    string `json:"agent_id"`
	Model      string `json:"model"`
	Day        string `json:"day"` // yyyymmdd
	Input      uint64 `json:"input"`
	Output     uint64 `json:"output"`
	CacheRead  uint64 `json:"cache_read"`
	CacheWrite uint64 `json:"cache_write"`
	Requests   uint64 `json:"requests"`
}

// TokenUsageSummary is the compact aggregate consumed by the dashboard. All
// rollups come from the same ledger rows — no estimation, no extrapolation.
type TokenUsageSummary struct {
	Loaded     bool
	Input      uint64
	Output     uint64
	CacheRead  uint64
	CacheWrite uint64
	Requests   uint64
	TotalUSD   float64
	Partial    bool // true if any row's model was not in ModelPrices

	// Aggregated by model for the "by model" breakdown.
	ByModel []ModelBreakdown

	// Cache hit rate: cache_read / (input + cache_read). 0 if no reads.
	CacheHitRate float64
}

// ModelBreakdown is one row of the per-model breakdown table.
type ModelBreakdown struct {
	Model    string
	Input    uint64
	Output   uint64
	Requests uint64
	CostUSD  float64
	Sharepct float64 // share of total USD cost, in [0, 100]
}

// dayKey returns the canonical yyyymmdd day tag for t (UTC).
func dayKey(t time.Time) string { return t.UTC().Format("20060102") }

// tokenKey builds the bbolt key for (agent, model, day).
func tokenKey(agent, model, day string) []byte {
	return []byte(agent + "|" + model + "|" + day)
}

// initTokenUsageBucket ensures the bucket exists. Safe to call many times.
func initTokenUsageBucket(db *bolt.DB) error {
	return db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(bucketTokenUsage)
		return err
	})
}

// RecordTokenUsage adds the given token counts to the rolling ledger row for
// (agent, model, today). Safe to call on every successful API response — the
// whole operation is a single read-modify-write transaction ≤ 1 ms on bbolt.
// Failures are swallowed by callers (see runner.go) so accounting never
// breaks a run.
func RecordTokenUsage(db *bolt.DB, agent, model string, input, output, cacheRead, cacheWrite uint64) error {
	if db == nil {
		return fmt.Errorf("nil db")
	}
	if agent == "" || model == "" {
		return fmt.Errorf("agent and model required")
	}

	day := dayKey(time.Now())
	key := tokenKey(agent, model, day)

	return db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(bucketTokenUsage)
		if err != nil {
			return err
		}
		var row TokenUsage
		if raw := b.Get(key); raw != nil {
			_ = json.Unmarshal(raw, &row)
		}
		row.AgentID = agent
		row.Model = model
		row.Day = day
		row.Input += input
		row.Output += output
		row.CacheRead += cacheRead
		row.CacheWrite += cacheWrite
		row.Requests++

		data, err := json.Marshal(row)
		if err != nil {
			return err
		}
		return b.Put(key, data)
	})
}

// LoadTokenUsageSummary aggregates the ledger over the last `days` days
// (inclusive of today, UTC day boundaries), applies ModelPrices to each row,
// and returns a summary ready for the dashboard.
//
// Returns an empty summary (Loaded=true, zero counters) when the bucket
// exists but has no rows — the caller should render an empty-state card, not
// fabricate numbers.
func LoadTokenUsageSummary(db *bolt.DB, days int) (TokenUsageSummary, error) {
	if db == nil {
		return TokenUsageSummary{}, fmt.Errorf("nil db")
	}
	if days <= 0 {
		days = 30
	}

	cutoff := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -(days - 1))
	cutoffKey := dayKey(cutoff)

	byModel := map[string]*ModelBreakdown{}
	sum := TokenUsageSummary{Loaded: true}

	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketTokenUsage)
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			var row TokenUsage
			if err := json.Unmarshal(v, &row); err != nil {
				return nil // skip corrupt entry
			}
			if row.Day < cutoffKey {
				return nil
			}

			sum.Input += row.Input
			sum.Output += row.Output
			sum.CacheRead += row.CacheRead
			sum.CacheWrite += row.CacheWrite
			sum.Requests += row.Requests

			pricing, priced := LookupPricing(row.Model)
			cost := 0.0
			if priced {
				cost = pricing.CostUSD(row.Input, row.Output, row.CacheRead, row.CacheWrite)
			} else {
				sum.Partial = true
			}
			sum.TotalUSD += cost

			canonical := canonicalModelName(row.Model)
			mb, ok := byModel[canonical]
			if !ok {
				mb = &ModelBreakdown{Model: canonical}
				byModel[canonical] = mb
			}
			mb.Input += row.Input
			mb.Output += row.Output
			mb.Requests += row.Requests
			mb.CostUSD += cost
			return nil
		})
	})
	if err != nil {
		return TokenUsageSummary{}, err
	}

	// Cache hit rate: reads vs all input-side tokens (reads + fresh).
	totalInputSide := sum.Input + sum.CacheRead
	if totalInputSide > 0 {
		sum.CacheHitRate = float64(sum.CacheRead) / float64(totalInputSide)
	}

	// Build ordered by-model list (desc by cost then by requests).
	for _, mb := range byModel {
		if sum.TotalUSD > 0 {
			mb.Sharepct = mb.CostUSD / sum.TotalUSD * 100
		}
		sum.ByModel = append(sum.ByModel, *mb)
	}
	sortByModel(sum.ByModel)

	return sum, nil
}

// canonicalModelName shortens official Anthropic IDs to the friendly labels
// you actually want on a dashboard: "claude-sonnet-4-6-20260301" →
// "sonnet-4.6". Falls back to the raw string for anything unexpected.
func canonicalModelName(model string) string {
	lower := strings.ToLower(model)
	switch {
	case strings.Contains(lower, "opus-4"):
		return "opus-4"
	case strings.Contains(lower, "sonnet-4-7"), strings.Contains(lower, "sonnet-4.7"):
		return "sonnet-4.7"
	case strings.Contains(lower, "sonnet-4-6"), strings.Contains(lower, "sonnet-4.6"):
		return "sonnet-4.6"
	case strings.Contains(lower, "sonnet-4"):
		return "sonnet-4"
	case strings.Contains(lower, "haiku-4"):
		return "haiku-4"
	case strings.Contains(lower, "3-opus"):
		return "3-opus"
	case strings.Contains(lower, "3-sonnet"):
		return "3-sonnet"
	case strings.Contains(lower, "3-haiku"):
		return "3-haiku"
	}
	return model
}

func sortByModel(xs []ModelBreakdown) {
	// Insertion sort — max models in practice is ~4, overkill to import sort.
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0; j-- {
			if xs[j].CostUSD > xs[j-1].CostUSD ||
				(xs[j].CostUSD == xs[j-1].CostUSD && xs[j].Requests > xs[j-1].Requests) {
				xs[j], xs[j-1] = xs[j-1], xs[j]
				continue
			}
			break
		}
	}
}
