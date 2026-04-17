package harness

import "strings"

// ModelPricing is USD per 1M tokens for a single model. Keep all four fields
// — the cache rates are what make the dashboard compelling, since prompt
// caching is where graymatter actually saves the user money.
//
// Prices are public list prices from Anthropic, last refreshed 2026-04-16.
// If Anthropic re-tiers or a new model ships, update ModelPrices below. The
// dashboard falls back to zero-cost (not a fabricated estimate) for any model
// not present in the table.
type ModelPricing struct {
	InputPer1M      float64 // per 1,000,000 input tokens
	OutputPer1M     float64 // per 1,000,000 output tokens
	CacheReadPer1M  float64 // prompt-cache read (the cheap hit)
	CacheWritePer1M float64 // prompt-cache creation (the expensive miss)
}

// ModelPrices is the canonical pricing map. Keys are matched with prefix
// contains-semantics (see LookupPricing) so both full model IDs
// ("claude-sonnet-4-6-20260301") and short aliases ("sonnet-4.6") resolve to
// the same row.
var ModelPrices = map[string]ModelPricing{
	// Claude 4.x family.
	"claude-opus-4":   {InputPer1M: 15.00, OutputPer1M: 75.00, CacheReadPer1M: 1.50, CacheWritePer1M: 18.75},
	"claude-sonnet-4": {InputPer1M: 3.00, OutputPer1M: 15.00, CacheReadPer1M: 0.30, CacheWritePer1M: 3.75},
	"claude-haiku-4":  {InputPer1M: 0.25, OutputPer1M: 1.25, CacheReadPer1M: 0.03, CacheWritePer1M: 0.31},

	// Claude 3.x legacy aliases — same schedule as the 4.x line until Anthropic
	// publishes distinct numbers. Conservative: prefer 4.x keys when possible.
	"claude-3-opus":   {InputPer1M: 15.00, OutputPer1M: 75.00, CacheReadPer1M: 1.50, CacheWritePer1M: 18.75},
	"claude-3-sonnet": {InputPer1M: 3.00, OutputPer1M: 15.00, CacheReadPer1M: 0.30, CacheWritePer1M: 3.75},
	"claude-3-haiku":  {InputPer1M: 0.25, OutputPer1M: 1.25, CacheReadPer1M: 0.03, CacheWritePer1M: 0.31},
}

// LookupPricing resolves a model identifier to its pricing row. Matching is
// case-insensitive and uses "contains" so that both the official model IDs
// returned by the Anthropic SDK and user-friendly shorthands work.
//
// Returns (ModelPricing{}, false) for unknown models — callers MUST render
// "—" instead of fabricating a cost.
func LookupPricing(model string) (ModelPricing, bool) {
	if model == "" {
		return ModelPricing{}, false
	}
	lower := strings.ToLower(model)
	for key, p := range ModelPrices {
		if strings.Contains(lower, key) {
			return p, true
		}
	}
	return ModelPricing{}, false
}

// CostUSD applies pricing to a token record and returns the total cost in
// dollars. Any leg whose model has no pricing contributes zero to the sum
// (signalled via the second return — false means "at least one leg was
// uncosted", so the UI can display a "partial" flag).
func (p ModelPricing) CostUSD(input, output, cacheRead, cacheWrite uint64) float64 {
	return float64(input)*p.InputPer1M/1_000_000 +
		float64(output)*p.OutputPer1M/1_000_000 +
		float64(cacheRead)*p.CacheReadPer1M/1_000_000 +
		float64(cacheWrite)*p.CacheWritePer1M/1_000_000
}
