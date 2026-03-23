# HybridMem-RAG 使用指南

> 详细的安装、配置、使用和运维文档

---

## 目录

1. [环境要求](#1-环境要求)
2. [安装与构建](#2-安装与构建)
3. [MCP Server 使用](#3-mcp-server-使用)
4. [MCP 工具详解](#4-mcp-工具详解)
5. [HTTP API 使用](#5-http-api-使用)
6. [配置文件](#6-配置文件)
7. [记忆提取](#7-记忆提取)
8. [记忆合并](#8-记忆合并)
9. [垃圾桶与数据恢复](#9-垃圾桶与数据恢复)
10. [性能调优](#10-性能调优)
11. [测试与验证](#11-测试与验证)
12. [常见问题](#12-常见问题)

---

## 1. 环境要求

| 组件 | 最低版本 | 说明 |
|------|---------|------|
| Go | 1.21+ | 编译需要 |
| SQLite | 内置 | 通过 go-sqlite3 自动编译 |
| GCC/Clang | 系统自带 | go-sqlite3 需要 CGO |
| LLM API | 可选 | 记忆提取和合并需要 |

**操作系统**: macOS / Linux / Windows（均已测试）

---

## 2. 安装与构建

### 2.1 获取源码

```bash
git clone https://github.com/AetherX-Technologies/memory_agentic_RAG.git
cd memory_agentic_RAG
go mod download
```

### 2.2 构建 MCP Server

```bash
# 构建（必须加 -tags fts5 以启用全文搜索）
go build -tags fts5 -o hybridmem-mcp ./cmd/mcp_server/

# 验证
./hybridmem-mcp --help  # 通过 stdin/stdout 运行
```

### 2.3 构建 HTTP Server

```bash
go build -tags fts5 -o hybridmem-server ./cmd/server/
```

### 2.4 交叉编译

```bash
# Linux
GOOS=linux GOARCH=amd64 go build -tags fts5 -o hybridmem-mcp-linux ./cmd/mcp_server/

# Windows
GOOS=windows GOARCH=amd64 go build -tags fts5 -o hybridmem-mcp.exe ./cmd/mcp_server/
```

---

## 3. MCP Server 使用

### 3.1 启动

```bash
# 基本启动（数据库默认为当前目录的 memory.db）
./hybridmem-mcp

# 指定数据库路径
MEMORY_DB_PATH=/path/to/my-memory.db ./hybridmem-mcp
```

### 3.2 配置 Claude Code

在 Claude Code 的 MCP 配置中添加：

```json
{
  "mcpServers": {
    "memory": {
      "command": "/absolute/path/to/hybridmem-mcp",
      "env": {
        "MEMORY_DB_PATH": "/absolute/path/to/memory.db"
      }
    }
  }
}
```

**配置文件位置**:
- macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`
- Linux: `~/.config/Claude/claude_desktop_config.json`
- Claude Code CLI: 项目根目录 `.claude/settings.json` 中的 `mcpServers`

### 3.3 验证连接

配置完成后，在 Claude Code 中输入：

```
记住：我是一名 Go 开发者
```

Claude 应该调用 `memory_store` 工具存储这条记忆。

---

## 4. MCP 工具详解

### 4.1 memory_store — 存储记忆

```json
{
  "content": "用户是西北院的水利工程师",
  "type": "fact",
  "importance": 0.8,
  "tags": ["西北院", "水利", "工程师"]
}
```

**参数**:
| 参数 | 类型 | 必填 | 默认 | 说明 |
|------|------|------|------|------|
| content | string | ✅ | - | 记忆内容 |
| type | string | - | fact | fact/preference/skill/episode/instruction/relationship |
| importance | number | - | 0.7 | 重要性 [0, 1] |
| tags | string[] | - | [] | 标签（用于按标签删除） |

**返回**: `{action: "stored", id: "uuid"}`

### 4.2 memory_recall — 检索记忆

```json
{
  "query": "用户的技术背景",
  "limit": 10,
  "types": ["fact", "skill"],
  "min_importance": 0.3,
  "max_tokens": 1000
}
```

**参数**:
| 参数 | 类型 | 必填 | 默认 | 说明 |
|------|------|------|------|------|
| query | string | ✅ | - | 检索查询 |
| limit | int | - | 10 | 最大返回数 |
| types | string[] | - | 全部 | 过滤记忆类型 |
| min_importance | number | - | 0.1 | 最低重要性阈值 |
| max_tokens | int | - | 1000 | 上下文 token 预算 |

**返回**: 格式化的上下文文本 + 结构化记忆数组 + 合并洞察

```
[记忆系统召回 3 条]

📌 指令
- 回复用中文

👤 用户信息
- 西北院水利工程师

🔧 技能
- Go 后端 3 年经验

🧠 洞察
- 用户是西北院的 Go 开发者，主要做水利相关技术项目
```

### 4.3 memory_forget — 删除记忆

```json
{"id": "memory-uuid-here"}
```

记忆不会立即删除，而是移入**垃圾桶**，30 天内可恢复。

### 4.4 memory_update — 更新记忆

```json
{
  "id": "memory-uuid-here",
  "content": "新的记忆内容",
  "importance": 0.9
}
```

- **只改 importance**：直接更新，不影响内容
- **改 content**：旧记忆软删除 → 新记忆创建 → 自动记录 supersession 关系

### 4.5 memory_export — 导出备份

```json
{
  "types": ["fact", "instruction"],
  "include_expired": false
}
```

返回完整的 JSON 数组，可用于备份和迁移。

### 4.6 memory_import — 导入恢复

```json
{
  "memories": [
    {"content": "用户是工程师", "type": "fact", "importance": 0.8}
  ]
}
```

自动跳过 content_hash 冲突的重复记忆。

### 4.7 memory_forget_by_tag — 按标签批量删除

```json
{"tag": "pii:employer", "dry_run": true}
```

- `dry_run: true`（默认）：只返回匹配数量，不删除
- `dry_run: false`：实际执行软删除

### 4.8 memory_consolidate — 触发记忆合并

```json
{}
```

分析未合并的记忆，发现关联和模式。需要配置 LLM API key。

---

## 5. HTTP API 使用

### 5.1 启动 HTTP 服务

```bash
go run -tags fts5 ./cmd/server/
# 默认监听 :8080
```

### 5.2 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/memories` | 创建记忆 |
| GET | `/api/memories/search?q=xxx&limit=10` | 检索记忆 |
| DELETE | `/api/memories/:id` | 删除记忆 |
| GET | `/api/memories/stats` | 统计信息 |
| GET | `/api/memories/:id/content` | 获取完整内容 |
| GET | `/api/health` | 健康检查 |

### 5.3 示例

```bash
# 创建记忆
curl -X POST http://localhost:8080/api/memories \
  -H "Content-Type: application/json" \
  -d '{"text": "用户是Go开发者", "category": "memory", "scope": "global", "importance": 0.8}'

# 检索
curl "http://localhost:8080/api/memories/search?q=开发者&limit=5"

# 健康检查
curl http://localhost:8080/api/health
```

---

## 6. 配置文件

### 6.1 统一配置 (config.yaml)

```yaml
store:
  db_path: "data/memories.db"
  vector_dim: 0  # 0 = 自动

embedding:
  provider: "local"  # local | jina | openai
  local:
    model_path: "models/qwen3-embedding-0.6b-onnx-uint8/dynamic_uint8.onnx"
    batch_size: 16
  openai:
    api_key: "sk-xxx"
    model: "text-embedding-3-small"
    endpoint: "https://api.openai.com/v1/embeddings"

rerank:
  enabled: true
  provider: "jina"
  api_key: "jina-xxx"
  model: "jina-reranker-v2-base-multilingual"

llm:
  api_key: "your-api-key"
  model: "gpt-4o-mini"
  endpoint: "https://api.openai.com/v1/chat/completions"
  timeout: 30

splitter:
  max_chunk_size: 512
  min_chunk_size: 256

retrieval:
  mode: "hybrid"  # bm25 | vector | hybrid | openviking
  alpha: 0.7
```

### 6.2 环境变量

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `MEMORY_DB_PATH` | MCP Server 数据库路径 | `memory.db` |
| `MEMORY_LLM_KEY` | LLM API 密钥（用于提取/合并） | 无 |

---

## 7. 记忆提取

### 7.1 LLM 模式

当配置了 LLM API key 时，系统使用 LLM 从对话中提取记忆：

```
用户："我叫张伟，是西北院的水利工程师，Go 写了3年"

→ LLM 提取：
  [fact] 用户叫张伟 (importance=0.6)
  [fact] 用户是西北院的水利工程师 (importance=0.8)
  [skill] 用户有3年Go后端开发经验 (importance=0.8)
```

### 7.2 Fallback 模式

无 LLM 时自动切换为规则提取：

| 前缀 | 类型 | 示例 |
|------|------|------|
| 我是/我在/我叫 | fact | "我是工程师" |
| 我喜欢/我偏好 | preference | "我喜欢简洁风格" |
| 我会/我擅长 | skill | "我会Python" |
| 不要/以后/记住/请 | instruction | "以后用中文回复" |

### 7.3 提取质量

| 指标 | LLM 模式 | Fallback 模式 |
|------|---------|---------------|
| 类型准确率 | ~95% | ~70% |
| 信息完整度 | 高 | 中 |
| Confidence | 0.8-1.0 | 0.3 |
| 延迟 | 3-15s | <1ms |

---

## 8. 记忆合并

### 8.1 自动合并

通过 Scheduler 定时执行（默认每 30 分钟）：

```go
scheduler := consolidate.NewScheduler(consolidator, 30*time.Minute, 2, logger)
scheduler.Start()
defer scheduler.Stop()
```

### 8.2 手动合并

通过 MCP 工具 `memory_consolidate` 触发。

### 8.3 合并结果示例

```json
{
  "insight": "用户是西北院的Go开发者，主要做水利相关技术项目",
  "patterns": [
    "职业领域（水利工程）与技术实现（Go后端/SQLite）强绑定",
    "技术栈以Go为主、Python为辅"
  ],
  "connections": [
    {"from_id": "m1", "to_id": "m2", "relationship": "同一用户的职业与技能"}
  ]
}
```

---

## 9. 垃圾桶与数据恢复

### 9.1 软删除流程

```
正常记忆 ──删除──→ 垃圾桶（deleted_at > 0）──30天后──→ 永久删除
                        ↑
                  可恢复 / 可永久删除
```

### 9.2 Store API

```go
// 移入垃圾桶
st.SoftDelete(id, time.Now().Unix())

// 查看垃圾桶
trash, _ := st.ListTrash(50)

// 恢复
st.Restore(id)

// 永久删除
st.PermanentDelete(id)

// 自动清理（每日运行）
st.RunCleanup(time.Now().Unix())
```

### 9.3 RunCleanup 执行的操作

1. 标记过期记忆（`expires_at` 已过）
2. 软删除低价值记忆（importance < 0.1, confidence < 0.1, 从未召回, > 180天）
3. 软删除过期 > 30 天的记忆
4. 永久删除垃圾桶中 > 30 天的记忆

---

## 10. 性能调优

### 10.1 数据库

```bash
# WAL 模式已默认启用（提升并发写入性能）
# 连接池最大 25 连接

# 查看数据库大小
ls -lh memory.db
```

### 10.2 基准参考

| 规模 | Insert | VectorSearch | HybridSearch |
|------|--------|-------------|--------------|
| 100 | 365µs | 368µs | 471µs |
| 1,000 | 268µs | 3.6ms | 3.6ms |
| 10,000 | 248µs | 39.4ms | 38.2ms |

### 10.3 运行基准测试

```bash
go run -tags fts5 ./cmd/benchmark_memory/
```

---

## 11. 测试与验证

### 11.1 单元测试

```bash
go test -tags fts5 ./internal/extractor/ ./internal/dedup/ ./internal/consolidate/ \
  ./internal/mcp/ ./internal/ratelimit/ -v
```

### 11.2 集成测试（A→G 全链路）

```bash
go run -tags fts5 ./cmd/full_memory_test/
```

预期输出：21 通过 / 0 失败

### 11.3 真实 LLM 测试

```bash
MEMORY_LLM_KEY=your_api_key go run -tags fts5 ./cmd/real_llm_test/
```

测试项：
- LLM 记忆提取（4 段对话 → 10 条记忆，5 种类型）
- 语义去重（10 → 7，3 条重复拦截）
- LLM 记忆合并（生成洞察 + 模式 + 连接）

### 11.4 MCP 协议测试

```bash
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}' | \
  MEMORY_DB_PATH=/tmp/test.db ./hybridmem-mcp
```

---

## 12. 常见问题

### Q: 编译报错 "fts5 not found"

加 `-tags fts5` 编译标志：

```bash
go build -tags fts5 -o hybridmem-mcp ./cmd/mcp_server/
```

### Q: 数据库打不开，报 WAL 错误

确保使用文件路径而非 `:memory:`。WAL 模式需要文件系统支持。

### Q: LLM 提取全部走了 fallback（confidence=0.3）

检查：
1. API key 是否正确设置
2. Endpoint 是否可达
3. 模型名称是否正确
4. 如果 API 要求流式，确认已使用 `llmutil.CallLLM`（内置 SSE 支持）

### Q: 合并超时

增大 `LLMTimeout`（默认 30s，建议 120s）：

```go
consolidate.Config{LLMTimeout: 120}
```

### Q: 如何备份数据？

```bash
# 通过 MCP
# 调用 memory_export 工具，保存返回的 JSON

# 直接复制数据库
cp memory.db memory.db.backup
```

### Q: 如何清理所有数据？

```bash
rm memory.db memory.db-shm memory.db-wal
```

### Q: 向量维度不匹配

设置 `VectorDim: 0` 禁用维度校验（自动适配）：

```go
store.Config{DBPath: "memory.db", VectorDim: 0}
```

---

## 附录：项目文件说明

| 路径 | 说明 |
|------|------|
| `cmd/mcp_server/` | MCP Server 入口 |
| `cmd/server/` | HTTP Server 入口 |
| `cmd/full_memory_test/` | A→G 集成测试 |
| `cmd/benchmark_memory/` | 性能基准测试 |
| `cmd/real_llm_test/` | 真实 LLM 测试 |
| `internal/store/` | 存储层（SQLite + FTS5 + Vector） |
| `internal/extractor/` | 记忆提取器 |
| `internal/dedup/` | 去重与冲突解决 |
| `internal/consolidate/` | 记忆合并 |
| `internal/mcp/` | MCP Server |
| `internal/ratelimit/` | 限流器 |
| `internal/llmutil/` | LLM 调用工具（SSE + JSON） |
| `config.yaml` | 统一配置文件 |
| `.context/ai-memory-system-design.md` | 系统设计文档 |
| `.context/consolidation-design.md` | 合并功能设计 |
