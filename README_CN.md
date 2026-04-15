# HybridMem-RAG

> **AI Agent 记忆系统 — 从被动检索到主动知识合成**
> 纯 Go • MCP + HTTP Tool API • 跨平台 • 10k memories in 39ms

**[📘 集成指南](./docs/INTEGRATION_GUIDE.md)** | [使用指南](./docs/USAGE_GUIDE.md) | [HTTP API](./docs/API.md) | [架构文档](./docs/architecture/INDEX.md) | [开发路线图](./docs/DEV_ROADMAP.md) | [English](./README.md)

---

## 🚀 外部项目集成

想把 HybridMem-RAG 集成到你的项目？**直接看 [集成指南](./docs/INTEGRATION_GUIDE.md)**。支持三种方式：

- **MCP Server** — Claude Code / Claude Desktop / Cherry Studio / Cline
- **HTTP API** — Python / Node.js / Java / Rust 任意语言
- **Go 库** — 直接 `go get` 导入

---

## 项目定位

HybridMem-RAG 是一个长期记忆后端，给大模型或 AI Agent 提供：

- 记忆存储 / 去重 / 冲突检测 / 软删除
- 混合检索（BM25 + 向量 + RRF + Reranker + MMR）
- 自动触发（FastText ML 判断何时该存 / 何时该查）
- 记忆聚合（LLM 发现跨记忆模式和洞察）
- 跨平台（macOS / Linux / Windows）

```
用户消息 → 是否该捕获? → 提取(LLM) → 噪音过滤 → 去重/冲突检测 → 存储
                                                                    ↓
MCP 9 工具 ← 格式化上下文 ← MMR 多样性 ← 混合检索 ← 是否该检索?
                                                                    ↓
                            Leaf 语义分组合并 → 跨记忆洞察 → 限流 / 日志
```

---

## ✨ v2 最新能力

在 v1 基线（混合检索、FastText 触发、去重、聚合）之上新增：

| 能力 | 作用 |
|------|------|
| **智能记忆联想** | 存入时自动建立双向 `connections`（余弦 0.7–0.85），recall 时展开为 🔗 关联记忆区域 |
| **长文本自动摘要** | >500 tokens 的记忆自动用小模型生成 Abstract；嵌入用 Abstract，原文 Text 保留供 FTS。实测压缩 **最高 423×** |
| **Leaf 语义分组** | 聚合改为按 connections 图 BFS 分组（每组 ≤10），不再盲取最新 50。孤立组 fallback 处理冷启动 |
| **Embedder 专属阈值** | 每个嵌入模型（Qwen3-0.6B / Qwen3-4B / Jina v3 / OpenAI v3）独立阈值。未知模型自动用 23 对标定，缓存到 `~/.hybridmem/` |
| **双层 LLM 配置** | 主模型（如 `gpt-4o`）做深度推理；可选**小模型**（如 `gpt-4o-mini`）做摘要 — 独立 key/endpoint/timeout |
| **CJK 精确 token 预算** | `max_tokens` 参数支持 CJK 精确估算（CJK ×1.5, ASCII ×0.25, Emoji ×2.0），误差 <10% |
| **Tags 完整持久化** | store/update/export/import 全路径覆盖；`*[]string` 指针类型区分"保持"和"清除" |
| **SourceConv 过滤** | recall 可按对话 ID 过滤（`source_conv` 参数），输出显示 `[conv:id]` 后缀 |

详细技术设计见 [SMART_ASSOCIATION.md](./docs/SMART_ASSOCIATION.md)。

---

## 核心特性

| 特性 | 说明 |
|------|------|
| **记忆提取** | LLM 自动提取 6 种类型（fact/preference/skill/episode/instruction/relationship），fallback 规则提取 |
| **智能去重** | content_hash 精确去重 + 向量语义去重（>0.93 按模型校准）+ LLM 冲突检测 |
| **混合检索** | BM25 + 向量 + **加权 RRF 融合** + Reranking + 分层检索 + CJK 优化 |
| **评分管道** | 新近度衰减 + 重要性加权 + 置信度 + 访问频率 + 类型差异化半衰期 + **乘法时间衰减** |
| **自动触发** | **FastText ML 分类器**（CJK + EN 双模型，98%+ 精度）+ ShouldRetrieve 自适应跳过（~60–70% 无效查询被跳过） |
| **噪音过滤** | AI 否认 / 元问题 / 样板文本自动过滤 |
| **MMR 多样性** | 最大边际相关性去重 |
| **垃圾桶** | 软删除 → 30 天恢复期 → 永久清理 |
| **记忆合并** | **按 connections 图语义分组的 LLM 聚合** (v2)，发现跨主题洞察 |
| **MCP Server** | 9 个工具，stdio JSON-RPC，兼容 Claude Code / Chatbox |
| **HTTP Tool API** | `/api/v1/tools` + `/api/v1/tools/call` + 工具别名 |
| **限流** | 20 ops/min, 200 ops/hour 防止记忆膨胀 |

---

## 性能

| 规模 | Insert (p50) | VectorSearch (p50) | HybridSearch (p50) | Export |
|------|-------------|--------------------|--------------------|--------|
| 100 | 365µs | 368µs | 471µs | 254µs |
| 1,000 | 268µs | 3.6ms | 3.6ms | 2.1ms |
| **10,000** | **248µs** | **39.4ms** | **38.2ms** | **21.6ms** |

*测试环境：Apple Silicon，SQLite FTS5，128 维向量。*

真实文档测试（5 个文件，130KB，55k tokens）：
- 长文本摘要每条耗时 **~2–3s**（gpt-4o-mini）
- 5 个文档跨主题聚合耗时 **~52s**，产出高质量洞察
- Token 压缩率：**平均 214× 到 344×**

---

## 快速开始

### 1. 构建

```bash
git clone https://github.com/AetherX-Technologies/memory_agentic_RAG.git
cd memory_agentic_RAG

# 构建 FastText C++ 库（只需一次）
git clone --depth 1 https://github.com/facebookresearch/fastText.git /tmp/fasttext
cd /tmp/fasttext && mkdir build && cd build
cmake .. -DCMAKE_POSITION_INDEPENDENT_CODE=ON -DCMAKE_POLICY_VERSION_MINIMUM=3.5
make -j$(nproc)
cp /tmp/fasttext/build/libfasttext_pic.a \
   /path/to/memory_agentic_RAG/internal/fasttext/lib/libfasttext.a

# 构建二进制
cd /path/to/memory_agentic_RAG
CGO_ENABLED=1 go build -tags fts5 -o hybridmem-mcp    ./cmd/mcp_server/
CGO_ENABLED=1 go build -tags fts5 -o hybridmem-server ./cmd/server/
```

### 2. 配置

创建 `config.local.yaml`（已 gitignore，包含你的 API key）：

```yaml
embedding:
  provider: "local"                                    # 或 "openai" / "jina"
  local:
    model_path: "models/qwen3-embedding-0.6b-onnx-uint8/dynamic_uint8.onnx"

llm:
  api_key: "YOUR_MAIN_LLM_KEY"
  model: "gpt-4o"                                      # consolidation / 冲突检测
  endpoint: "https://api.openai.com/v1/chat/completions"
  timeout: 30

  summary:                                             # 可选：摘要等轻量任务用小模型
    api_key: "YOUR_LLM_KEY"
    model: "gpt-4o-mini"
    endpoint: "https://api.openai.com/v1/chat/completions"
    timeout: 30
```

### 3. 作为 MCP Server 运行

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

### 4. 作为 HTTP Server 运行

```bash
MEMORY_HTTP_ADDR=127.0.0.1:8080 \
MEMORY_CONFIG_PATH=/path/to/config.local.yaml \
./hybridmem-server
```

参考：
- [HTTP API](./docs/API.md)
- [集成指南](./docs/INTEGRATION_GUIDE.md)
- [使用指南](./docs/USAGE_GUIDE.md)

### 5. 跑测试

```bash
# 单元测试（14 个包）
go test -tags fts5 ./internal/...

# 完整集成测试（mock embedder）
go run -tags fts5 ./cmd/full_memory_test/

# 真实生产模型测试（ONNX + FastText + 远程 LLM）
go run -tags fts5 ./cmd/realtest/

# 真实文档测试（~/Downloads 里的文件）
go run -tags fts5 ./cmd/realdoc_test/

# 真实 embedder 余弦分布标定
go run -tags fts5 ./cmd/calibration_test/
```

---

## MCP 工具

| 工具 | 说明 |
|------|------|
| `memory_store` | 存储记忆（自动去重 + content_hash + 噪音过滤 + 可选 tags） |
| `memory_recall` | 语义检索 + 自适应跳过 + MMR + 格式化上下文 + 聚合洞察 + 关联记忆展开 |
| `memory_forget` | 软删除（30 天可恢复） |
| `memory_update` | 更新内容（自动重新向量化）或元数据 |
| `memory_export` | 全量 JSON 备份（含 Abstract 和 tags） |
| `memory_import` | 批量恢复 |
| `memory_forget_by_tag` | 按标签批量删除（PII 清理等） |
| `memory_consolidate` | 触发 Leaf 分组的 LLM 聚合 |
| `memory_should_capture` | 预检文本是否值得存储（FastText ML + 触发词 + 噪音） |

---

## 文档

| 文档 | 受众 |
|------|------|
| [集成指南](./docs/INTEGRATION_GUIDE.md) | 外部项目开发者 |
| [使用指南](./docs/USAGE_GUIDE.md) | MCP/HTTP API 用户 |
| [HTTP API 参考](./docs/API.md) | REST API 用户 |
| [架构索引](./docs/architecture/INDEX.md) | 架构概览 |
| [智能记忆联想](./docs/SMART_ASSOCIATION.md) | v2 技术设计 |
| [开发路线图](./docs/DEV_ROADMAP.md) | 贡献者、未来规划 |
| [下阶段提案](./docs/NEXT_PHASE_PROPOSAL.md) | 架构评审 |
| [部署文档](./docs/DEPLOYMENT.md) | 运维、部署工程师 |

---

## 许可

MIT
