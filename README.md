# HybridMem-RAG

> **AI Agent 记忆系统 — 从被动检索到主动知识合成**
> Pure Go • MCP Server • Cross-platform • 10k memories in 39ms

[中文文档](./README_CN.md) | [Architecture](./docs/architecture/INDEX.md) | [Design Doc](./.context/ai-memory-system-design.md)

---

## What is HybridMem-RAG?

一个完整的 **AI Agent 长期记忆后端**，自动从对话中提取、去重、存储、评分、合并记忆，并通过 MCP 协议暴露给 Claude Code 等 AI Agent。

```
对话 → 记忆提取(LLM) → 去重/冲突检测 → 存储 → 评分/衰减
                                                    ↓
MCP 8工具 ← 格式化上下文 ← 混合检索 + 合并洞察 ← 限流/日志
                                                    ↓
                                   定时合并 → 发现关联/模式/洞察
```

### Core Features

| Feature | Description |
|---------|-------------|
| **记忆提取** | LLM 自动提取 6 种记忆类型（fact/preference/skill/episode/instruction/relationship），fallback 规则提取 |
| **智能去重** | content_hash 精确去重 + 向量语义去重（>0.93）+ LLM 冲突检测 |
| **混合检索** | BM25 + Vector + RRF 融合 + Reranking + 分层检索 |
| **评分管道** | 新近度衰减 + 重要性加权 + 置信度 + 访问频率 + 类型差异化半衰期 |
| **垃圾桶** | 软删除 → 30天恢复期 → 永久清理（无数据直接丢失） |
| **记忆合并** | 定时 LLM 分析，发现跨记忆关联/模式/洞察，构建知识图谱 |
| **MCP Server** | 8 个工具，stdio JSON-RPC，兼容 Claude Code |
| **限流** | 20 ops/min, 200 ops/hour 防止记忆膨胀 |

---

## Performance

| Scale | Insert (p50) | VectorSearch (p50) | HybridSearch (p50) | Export |
|-------|-------------|--------------------|--------------------|--------|
| 100 | 365µs | 368µs | 471µs | 254µs |
| 1,000 | 268µs | 3.6ms | 3.6ms | 2.1ms |
| **10,000** | **248µs** | **39.4ms** | **38.2ms** | **21.6ms** |

*Tested on Apple Silicon, SQLite FTS5, 128-dim vectors*

---

## Quick Start

### Build

```bash
git clone https://github.com/AetherX-Technologies/memory_agentic_RAG.git
cd memory_agentic_RAG
go build -tags fts5 -o hybridmem-mcp ./cmd/mcp_server/
```

### Run as MCP Server (for Claude Code)

```bash
export MEMORY_DB_PATH=memory.db
./hybridmem-mcp
```

### Configure Claude Code

Add to your Claude Code MCP settings:

```json
{
  "mcpServers": {
    "memory": {
      "command": "/path/to/hybridmem-mcp",
      "env": {
        "MEMORY_DB_PATH": "/path/to/memory.db"
      }
    }
  }
}
```

### Run Tests

```bash
# Unit tests
go test -tags fts5 ./internal/...

# Full integration test (A→G, 21 assertions)
go run -tags fts5 ./cmd/full_memory_test/

# Performance benchmark
go run -tags fts5 ./cmd/benchmark_memory/

# Real LLM test (requires API key)
MEMORY_LLM_KEY=your_key go run -tags fts5 ./cmd/real_llm_test/
```

---

## MCP Tools

| Tool | Description |
|------|-------------|
| `memory_store` | 存储记忆（自动去重 + content_hash） |
| `memory_recall` | 语义检索 + 格式化上下文 + 合并洞察 |
| `memory_forget` | 软删除（移入垃圾桶，30天可恢复） |
| `memory_update` | 更新内容（自动重新向量化）或元数据 |
| `memory_export` | 全量导出为 JSON（备份） |
| `memory_import` | 批量导入（恢复备份） |
| `memory_forget_by_tag` | 按标签批量删除（PII 清理等） |
| `memory_consolidate` | 触发记忆合并，发现关联和模式 |

---

## Architecture

```
internal/
├── store/           # SQLite storage + vector search + scoring + trash
├── extractor/       # LLM memory extraction + fallback rules + JSON parser
├── dedup/           # Semantic dedup + conflict resolution + content_hash
├── consolidate/     # Memory consolidation + scheduler + connection graph
├── mcp/             # MCP Server (stdio JSON-RPC, 8 tools)
├── ratelimit/       # Sliding-window rate limiter
├── memlog/          # Structured JSON logging (slog)
├── llmutil/         # Shared LLM client (SSE streaming + JSON fallback)
├── config/          # Unified YAML config
├── embedder/        # Local ONNX embedding (Qwen3-0.6B)
├── generator/       # L0/L1 summary generation
├── parser/          # Document splitting (structural + semantic)
├── retrieval/       # OpenViking hierarchical retrieval
└── api/             # HTTP REST API
```

### Memory Types

| Type | Half-life | Example |
|------|-----------|---------|
| `instruction` | 365 days | "请用中文回复" |
| `fact` | 90 days | "用户是西北院工程师" |
| `preference` | 90 days | "喜欢简洁代码风格" |
| `skill` | 180 days | "Go 后端 3 年经验" |
| `relationship` | 90 days | "Alice 是 Bob 的上级" |
| `episode` | 30 days | "昨天讨论了监测系统" |

---

## Development

### Project Stats

- **Phases**: A→G (7 phases completed)
- **Codex Reviews**: 34+ rounds
- **Bugs Fixed**: 82+
- **Test Coverage**: 21 integration assertions + 50+ unit tests
- **Real LLM Verified**: Extraction (10 memories) + Consolidation (insights)

### Key Design Decisions

1. **SQLite over LanceDB** — Pure Go, no CGO, cross-platform (including iOS)
2. **Soft-delete over hard-delete** — 30-day trash bin, all cleanup reversible
3. **SSE streaming LLM** — Auto-detects stream vs JSON, compatible with all APIs
4. **Fallback extraction** — Rule-based extraction when LLM unavailable
5. **No auto-supersede without LLM** — Prevents incorrect importance decay

---

## License

MIT License

---

**Built with Go + SQLite + Claude Opus 4.6 + Codex gpt-5.4**
