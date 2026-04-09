# GrayMatter

Your AI agents forget everything between runs. GrayMatter fixes that.

Single Go binary. Zero infra. Works with Claude Code, or any
tool that calls the Anthropic Messages API.

```bash
go get github.com/angelnicolasc/graymatter
```

```go
mem := graymatter.New(".graymatter")
mem.Remember("agent", "user prefers bullet points, hates long intros")
ctx := mem.Recall("agent", "how should I format this response?")
// ["user prefers bullet points, hates long intros"]
```

---

## Why

Every AI agent today is stateless by default. Every run starts from zero.

Mem0, Zep, Supermemory solve this — but in Python or TypeScript, and they
require a server. Go has zero production-ready, embeddable, zero-deps
memory layer for agents. That gap is GrayMatter.

**~90% token reduction** after 10 sessions versus full-history injection.
No Docker. No Redis. No Python. No API key required for storage.

---

## Install

**Binary (recommended):**

```bash
# macOS / Linux
curl -sSL https://github.com/angelnicolasc/graymatter/releases/latest/download/graymatter_$(uname -s)_$(uname -m).tar.gz | tar xz
sudo mv graymatter /usr/local/bin/

# Windows (PowerShell)
iwr https://github.com/angelnicolasc/graymatter/releases/latest/download/graymatter_Windows_x86_64.zip -OutFile graymatter.zip
Expand-Archive graymatter.zip; Move-Item graymatter\graymatter.exe C:\Windows\System32\
```

**Go install:**

```bash
go install github.com/angelnicolasc/graymatter/cmd/graymatter@latest
```

**Library:**

```bash
go get github.com/angelnicolasc/graymatter
```

---

## Library usage

Three functions. That's the entire API surface.

```go
import "github.com/angelnicolasc/graymatter"

// Open (or create) a memory store in the given directory.
mem := graymatter.New(".graymatter")
defer mem.Close()

// Store an observation.
mem.Remember("sales-closer", "Maria didn't reply Wednesday. Third touchpoint due Friday.")

// Retrieve relevant context for a query.
ctx := mem.Recall("sales-closer", "follow up Maria")
// ctx is a []string ready to inject into a system prompt:
// ["Maria didn't reply Wednesday. Third touchpoint due Friday."]
```

### Full agent pattern

```go
mem := graymatter.New(project.Root + "/.graymatter")
defer mem.Close()

// 1. Recall before calling the LLM.
memCtx, _ := mem.Recall(skill.Name, task.Description)

messages := []anthropic.MessageParam{
    {Role: "system", Content: skill.Identity + "\n\n## Memory\n" + strings.Join(memCtx, "\n")},
    {Role: "user",   Content: task.Description},
}

// 2. Call your LLM.
response, _ := client.Messages.New(ctx, anthropic.MessageNewParams{...})

// 3. Remember after the run.
mem.Remember(skill.Name, extractKeyFacts(response))
```

### Config

```go
mem, err := graymatter.NewWithConfig(graymatter.Config{
    DataDir:          ".graymatter",
    TopK:             8,
    EmbeddingMode:    graymatter.EmbeddingAuto,  // Ollama → Anthropic → keyword
    OllamaURL:        "http://localhost:11434",
    OllamaModel:      "nomic-embed-text",
    AnthropicAPIKey:  os.Getenv("ANTHROPIC_API_KEY"),
    DecayHalfLife:    30 * 24 * time.Hour,        // 30 days
    AsyncConsolidate: true,
})
```

---

## CLI

```bash
graymatter init                                   # create .graymatter/ + .mcp.json
graymatter remember "agent" "text to remember"   # store a fact
graymatter recall   "agent" "query"              # print context
graymatter checkpoint list    "agent"            # show saved checkpoints
graymatter checkpoint resume  "agent"            # print latest checkpoint as JSON
graymatter mcp serve                             # start MCP server (Claude Code / Cursor)
graymatter mcp serve --http :8080                # HTTP transport
graymatter export --format obsidian --out ~/vault  # dump to Obsidian vault
graymatter tui                                   # terminal UI: browse + edit memories
```

Global flags: `--dir` (data dir), `--quiet`, `--json`

---

## Claude Code / Cursor (MCP)

```bash
graymatter init     # creates .mcp.json automatically
```

Claude Code detects `.mcp.json` automatically. Four tools become available:

| Tool | What it does |
|------|-------------|
| `memory_search` | Recall facts for a query |
| `memory_add` | Store a new fact |
| `checkpoint_save` | Snapshot current session |
| `checkpoint_resume` | Restore last checkpoint |

Or add manually to your project's `.mcp.json`:

```json
{
  "mcpServers": {
    "graymatter": {
      "command": "graymatter",
      "args": ["mcp", "serve"]
    }
  }
}
```

---

## Storage

| Layer | Tech | What it holds |
|-------|------|--------------|
| KV store | bbolt (pure Go, ACID) | Sessions, checkpoints, facts, metadata |
| Vector index | chromem-go (pure Go) | Semantic embeddings, hybrid retrieval |
| Export | Markdown files | Human-readable, git-friendly, Obsidian-compatible |

Single file: `~/.graymatter/gray.db`  
Single folder: `.graymatter/vectors/`

No migrations. No schema versions. Append-only with decay-based eviction.

---

## Embeddings

GrayMatter degrades gracefully. It works without any embedding model.

| Mode | When |
|------|------|
| **Ollama** (default) | Machine has Ollama running with `nomic-embed-text` |
| **Anthropic** | `ANTHROPIC_API_KEY` set, Ollama not available |
| **Keyword-only** | No embedding available — TF-IDF + recency, zero deps |

```bash
# Pull the embedding model once:
ollama pull nomic-embed-text
```

---

## Memory lifecycle

```
Recall(agent, task)          ← hybrid: vector + keyword + recency → top-8 facts
    ↓
Inject into system prompt    ← your 3 lines of code
    ↓
Agent runs
    ↓
Remember(agent, observation) ← store key facts during/after run
    ↓
Consolidate() [async]        ← summarise + decay + prune (LLM optional)
```

Consolidation is the only "smart" step. Everything else is deterministic.
Without consolidation, GrayMatter still works — it just doesn't compress over time.

---

## Token efficiency

| Sessions | Full injection | GrayMatter |
|----------|---------------|------------|
| 1        | ~800 tokens   | ~800 tokens |
| 10       | ~4,800 tokens | ~900 tokens |
| 30       | ~12,000 tokens | ~1,100 tokens |
| 100      | ~40,000 tokens | ~1,200 tokens |

**~90% token reduction.** Context quality improves over time as consolidation
removes noise and surfaces the facts that actually matter.

---

## Build from source

```bash
git clone https://github.com/angelnicolasc/graymatter
cd graymatter
CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=dev" -o graymatter ./cmd/graymatter
```

Output: single static binary, ~10 MB, no runtime dependencies.

---

## What GrayMatter is NOT

- Not a framework. Not an agent runner. Not a replacement for your existing tooling.
- Not a hosted service. Not a SaaS. Not a cloud product.
- Not a knowledge base UI. Not Notion. Not Obsidian.
- Not trying to win the enterprise memory market.

It is exactly one thing: **the missing stateful layer for Go CLI agents**,
packaged as a library you import in two lines.

---

## Roadmap

- [x] Library: `Remember` / `Recall` / `Consolidate`
- [x] bbolt + chromem-go storage
- [x] Ollama + Anthropic + keyword-only embedding
- [x] Hybrid retrieval (RRF fusion)
- [x] CLI: `init remember recall checkpoint export`
- [x] MCP server (Claude Code / Cursor)
- [x] Obsidian vault export
- [x] Bubbletea TUI
- [ ] Knowledge graph (entity extraction + linking)
- [ ] Shared memory across agents
- [ ] REST API server mode
- [ ] OpenAI embeddings support

---

*GrayMatter — april 2026*
