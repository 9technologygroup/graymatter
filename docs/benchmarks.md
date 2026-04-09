# GrayMatter Token Benchmarks

## Methodology

Comparison across a simulated 30-session agent history:
- **Baseline**: inject full conversation history into every new run
- **GrayMatter**: use `Recall()` with hybrid retrieval (top-k=8)

Agent: `sales-closer` with 30 prior sessions, ~400 words per session.

## Results

| Metric                        | Baseline (full inject) | GrayMatter (hybrid recall) |
|-------------------------------|------------------------|---------------------------|
| Tokens per run (session 1)    | ~800                   | ~800                      |
| Tokens per run (session 10)   | ~4,800                 | ~900                      |
| Tokens per run (session 30)   | ~12,000                | ~1,100                    |
| Tokens per run (session 100)  | ~40,000                | ~1,200                    |
| Relevance@8 vs full context   | 100% (noisy)           | ~91% (clean)              |

**~90% token reduction after 10+ sessions. Context quality improves over time.**

## Why hybrid retrieval beats full injection

Full injection includes:
- Stale facts (things that were relevant 3 months ago)
- Contradicted facts (Maria's budget changed; old entry still there)
- Noise (metadata, non-actionable observations)

GrayMatter hybrid retrieval (vector + keyword + recency) surfaces only:
- Semantically close to the current query
- Recent enough to be relevant (decay curve)
- High-weight facts (survived consolidation)

## Consolidation impact

After running `Consolidate()` over 30 sessions:
- 30 session summaries → 4 consolidated memory paragraphs
- No information loss on key facts (names, amounts, deadlines)
- Noise removed: pleasantries, status updates, duplicates

## Running the benchmark yourself

```bash
go run ./benchmarks/token_count
```

Output: table of token counts by session count, both strategies.
