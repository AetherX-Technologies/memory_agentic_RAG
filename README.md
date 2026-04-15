# HybridMem-RAG

> **AI Agent Memory Backend — From Passive Retrieval to Active Knowledge Synthesis**
> Pure Go • MCP + HTTP Tool API • Cross-platform • 10k memories in 39ms

**[📘 Integration Guide](./docs/INTEGRATION_GUIDE.md)** | [Usage Guide](./docs/USAGE_GUIDE.md) | [HTTP API](./docs/API.md) | [Architecture](./docs/architecture/INDEX.md) | [Dev Roadmap](./docs/DEV_ROADMAP.md) | [中文文档](./README_CN.md)

---

## 🚀 For External Projects

Want to integrate HybridMem-RAG into your project? See the **[Integration Guide](./docs/INTEGRATION_GUIDE.md)**. Three supported paths:

- **MCP Server** — Claude Code / Claude Desktop / Cherry Studio / Cline
- **HTTP API** — Python / Node.js / Java / Rust / any language
- **Go Library** — `go get` import

---

## What is HybridMem-RAG?

A complete **AI Agent long-term memory backend** that automatically extracts, deduplicates, stores, scores, and consolidates memories from conversations. Exposed via MCP and HTTP Tool APIs to AI agents like Claude Code and Chatbox.

```
User message → ShouldCapture? → Extract (LLM) → Noise filter → Dedup/Conflict → Store
                                                                                    ↓
MCP 9 tools ← Format context ← MMR diversity ← Hybrid search ← ShouldRetrieve?
                                                                                    ↓
                                    Leaf consolidation → Cross-memory insights → Rate limit/log
```

---

## ✨ Latest Capabilities (v2)

Built on top of the v1 baseline (hybrid search, FastText triggers, dedup, consolidation). The v2 additions:

| Capability | What it does |
|-----------|-------------|
| **Smart Memory Association** | Auto-builds bidirectional `connections` during store-time (cosine 0.7–0.85); recall expands into linked memories as 🔗 Related Memories section |
| **Long-Text Auto-Abstract** | Content >500 tokens auto-summarized via small LLM; embedding uses Abstract while Text preserved for FTS. Real compression: **up to 423×** on 15k-token documents |
| **Leaf Semantic Grouping** | Consolidation now groups memories by `connections` graph (BFS, max 10 per group) instead of blindly taking newest 50. Orphan fallback for cold-start |
| **Embedder-Specific Profiles** | Thresholds tuned per embedder (Qwen3-0.6B local, Qwen3-4B API, Jina v3, OpenAI v3). Unknown models auto-calibrate via 23-pair benchmark, cached to `~/.hybridmem/` |
| **Dual-Tier LLM Config** | Main model (e.g. `gpt-4o`) for deep reasoning; optional **summary model** (e.g. `gpt-4o-mini`) for abstract generation — separate key/endpoint/timeout |
| **CJK-Aware Token Budget** | `max_tokens` parameter with accurate CJK estimation (CJK ×1.5, ASCII ×0.25, Emoji ×2.0). Error <10% vs cl100k_base |
| **Tags Persistence** | Fully wired across store/update/export/import; `*[]string` pointer type to distinguish keep vs clear |
| **SourceConv Filtering** | Recall by conversation ID (`source_conv` param), display `[conv:id]` suffix |

See [SMART_ASSOCIATION.md](./docs/SMART_ASSOCIATION.md) for technical details.

---

## Core Features

| Feature | Description |
|---------|-------------|
| **Memory Extraction** | Auto-extract 6 types (fact/preference/skill/episode/instruction/relationship) via LLM, with rule-based fallback |
| **Smart Dedup** | content_hash exact + vector semantic (>0.93 calibrated) + LLM conflict detection |
| **Hybrid Search** | BM25 + Vector + **Weighted RRF** + Reranking + Hierarchical + CJK-aware |
| **Scoring Pipeline** | Recency decay + importance + confidence + access frequency + type-aware half-life + **multiplicative decay** |
| **Auto Triggers** | **FastText ML classifier** (CJK + EN, 98%+ accuracy) + ShouldRetrieve adaptive skip (~60–70% invalid queries dropped) |
| **Noise Filter** | AI negation / meta questions / boilerplate filtered to prevent junk memories |
| **MMR Diversity** | Maximal Marginal Relevance to avoid duplicate top-K |
| **Trash / Restore** | Soft delete → 30-day grace → permanent cleanup |
| **Consolidation** | LLM-powered pattern discovery across memories, now with **semantic leaf grouping** (v2) |
| **MCP Server** | 9 tools, stdio JSON-RPC, compatible with Claude Code / Chatbox |
| **HTTP Tool API** | `/api/v1/tools` + `/api/v1/tools/call` + tool aliases, semantically aligned with MCP |
| **Rate Limiting** | 20 ops/min, 200 ops/hour to prevent memory bloat |

---

## Performance

| Scale | Insert (p50) | VectorSearch (p50) | HybridSearch (p50) | Export |
|-------|-------------|--------------------|--------------------|--------|
| 100 | 365µs | 368µs | 471µs | 254µs |
| 1,000 | 268µs | 3.6ms | 3.6ms | 2.1ms |
| **10,000** | **248µs** | **39.4ms** | **38.2ms** | **21.6ms** |

*Tested on Apple Silicon, SQLite FTS5, 128-dim vectors.*

Real-document test (5 files, 130KB, 55k tokens total):
- Long-text abstracts generated in **~2–3s each** via gpt-4o-mini
- Consolidation across 5 docs: **~52s** producing high-quality insights
- Token compression: **214× to 344×** average

---

## Quick Start

### 1. Build

```bash
git clone https://github.com/AetherX-Technologies/memory_agentic_RAG.git
cd memory_agentic_RAG

# Build FastText C++ library (one-time)
git clone --depth 1 https://github.com/facebookresearch/fastText.git /tmp/fasttext
cd /tmp/fasttext && mkdir build && cd build
cmake .. -DCMAKE_POSITION_INDEPENDENT_CODE=ON -DCMAKE_POLICY_VERSION_MINIMUM=3.5
make -j$(nproc)
cp /tmp/fasttext/build/libfasttext_pic.a \
   /path/to/memory_agentic_RAG/internal/fasttext/lib/libfasttext.a

# Build binaries
cd /path/to/memory_agentic_RAG
CGO_ENABLED=1 go build -tags fts5 -o hybridmem-mcp    ./cmd/mcp_server/
CGO_ENABLED=1 go build -tags fts5 -o hybridmem-server ./cmd/server/
```

### 2. Configure

Create `config.local.yaml` (gitignored — contains your API keys):

```yaml
embedding:
  provider: "local"                                    # or "openai" / "jina"
  local:
    model_path: "models/qwen3-embedding-0.6b-onnx-uint8/dynamic_uint8.onnx"

llm:
  api_key: "YOUR_MAIN_LLM_KEY"
  model: "gpt-4o"                                      # for consolidation / conflict detection
  endpoint: "https://api.openai.com/v1/chat/completions"
  timeout: 30

  summary:                                             # optional: small model for lightweight tasks
    api_key: "YOUR_LLM_KEY"
    model: "gpt-4o-mini"
    endpoint: "https://api.openai.com/v1/chat/completions"
    timeout: 30
```

### 3. Run as MCP Server

```json
// ~/Library/Application Support/Claude/claude_desktop_config.json
{
  "mcpServers": {
    "memory": {
      "command": "/absolute/path/to/hybridmem-mcp",
      "env": {
        "MEMORY_CONFIG_PATH": "/absolute/path/to/config.local.yaml"
      }
    }
  }
}
```

### 3. Run as HTTP Server

```bash
MEMORY_HTTP_ADDR=127.0.0.1:8080 \
MEMORY_CONFIG_PATH=/path/to/config.local.yaml \
./hybridmem-server
```

Reference:
- [HTTP API](./docs/API.md)
- [Integration Guide](./docs/INTEGRATION_GUIDE.md)
- [Usage Guide](./docs/USAGE_GUIDE.md)

### 4. Run Tests

```bash
# Unit tests (14 packages)
go test -tags fts5 ./internal/...

# Full integration test (mock embedder)
go run -tags fts5 ./cmd/full_memory_test/

# Real production-model test (ONNX + FastText + remote LLM)
go run -tags fts5 ./cmd/realtest/

# Real document test (~/Downloads files)
go run -tags fts5 ./cmd/realdoc_test/

# Cosine calibration against real embedder
go run -tags fts5 ./cmd/calibration_test/
```

---

## MCP Tools

| Tool | Description |
|------|-------------|
| `memory_store` | Store memory (auto dedup + content_hash + noise filter + optional tags) |
| `memory_recall` | Semantic search + adaptive skip + MMR + formatted context + consolidation insights + connection expansion |
| `memory_forget` | Soft delete (recoverable within 30 days) |
| `memory_update` | Update content (auto re-vectorize) or metadata |
| `memory_export` | Full JSON backup (includes Abstract + tags) |
| `memory_import` | Bulk restore |
| `memory_forget_by_tag` | Batch delete by tag (PII cleanup etc.) |
| `memory_consolidate` | Trigger leaf-grouped LLM consolidation |
| `memory_should_capture` | Pre-check if text is worth storing (FastText ML + triggers + noise) |

---

## Trigger System

Auto-decides **when to store** and **when to search** — no model-side tool-call required.

### ShouldCapture — Auto-store triggers

```
User message → Explicit trigger ("记住" / "remember")            → Store (conf=0.95)
             → FastText ML classifier (CJK/EN dual models)
               ├── skip (conf ≥ 0.61)                           → Skip
               ├── capture (conf ≥ 0.61)                        → Store (conf=0.85)
               └── uncertain                                     → Rule fallback
             → Implicit self-reference ("我是" / "I am" / "I like") → Store (conf=0.7)
             → Regex patterns (email / phone / date)            → Store (conf=0.6)
             → No trigger / too short / too long                 → Skip
```

**FastText models** (1.7MB × 2, 0.002ms/prediction):
- CJK model: character-level tokenization, 98.67% accuracy
- EN model: word-level tokenization, 99.1% accuracy
- Auto language routing via `textIsCJK()`

### ShouldRetrieve — Adaptive search skip

```
Query → Force patterns ("记得吗" / "remember" / "my name") → Search
      → Skip patterns ("hi" / "ok" / "git status" / emoji)  → Empty result
      → Default                                              → Search
```

**~60–70% of trivial queries are skipped**, drastically reducing latency and cost.

### IsNoise — Noise filtering

Blocks AI negations, meta-questions, and boilerplate from polluting storage.

---

## Architecture Highlights

### Smart Memory Association Pipeline

```
StoreWithDedup:
  1. Embed (uses Abstract if long content, else Text)
  2. VectorSearch candidates
  3. content_hash exact check → skip or update confidence
  4. Semantic dedup (cosine > DupThreshold, calibrated per-model)
  5. Conflict detection (LLM judgment) → supersede if contradictory
  6. Insert new memory
  7. buildConnectionsFiltered — link to related memories (cosine 0.7–0.85)

Recall:
  1. Hybrid search (vector + BM25 + weighted RRF)
  2. Rerank (optional Jina)
  3. MMR diversity
  4. Filter by type / importance / source_conv
  5. expandConnections — pull linked memories (top 3 seeds, deduped)
  6. Format: 📌 Instruction | 💡 Preference | 👤 Fact | 🔧 Skill
           | 🔗 Related Memories | 🧠 Insights (from consolidation)
```

### Auto-Calibration Flow

```
bootstrap.Load()
  → detectModelName ("openai:Qwen/Qwen3-Embedding-4B")
  → LoadCachedCalibration → hit? use it
  → miss? Calibrate:
      23-pair benchmark (7 dup + 9 related + 7 unrelated)
      Compute stats: min/max per category
      Derive DupThreshold / ConflictThreshold / ConnectionBand
      Save to ~/.hybridmem/calibration.json
```

---

## Documentation

| Document | Audience |
|----------|---------|
| [Integration Guide](./docs/INTEGRATION_GUIDE.md) | External project developers |
| [Usage Guide](./docs/USAGE_GUIDE.md) | End-users of the MCP/HTTP APIs |
| [HTTP API Reference](./docs/API.md) | REST API users |
| [Architecture Index](./docs/architecture/INDEX.md) | System architecture overview |
| [Smart Association](./docs/SMART_ASSOCIATION.md) | v2 feature technical doc |
| [Dev Roadmap](./docs/DEV_ROADMAP.md) | Contributors, future planning |
| [Next Phase Proposal](./docs/NEXT_PHASE_PROPOSAL.md) | Architects, design review |
| [Deployment](./docs/DEPLOYMENT.md) | Ops / deployment engineers |

---

## License

MIT
