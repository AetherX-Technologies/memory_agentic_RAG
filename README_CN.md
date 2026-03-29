# HybridMem-RAG

> **AI Agent 记忆系统，从被动检索到主动知识合成**
> 纯 Go • MCP + HTTP Tool API • 跨平台 • 10k memories in 39ms

[English](./README.md) | [HTTP API](./docs/API.md) | [使用指南](./docs/USAGE_GUIDE.md) | [架构文档](./docs/architecture/INDEX.md)

---

## 项目定位

HybridMem-RAG 是一个长期记忆后端，给大模型或 AI Agent 提供：

- 记忆存储
- 记忆召回
- 去重与冲突控制
- 软删除与恢复
- 导入导出
- 合并洞察

它当前支持两种接入方式：

- `MCP`
  适合 Claude Code、Chatbox 等 MCP 客户端
- `HTTP`
  适合本地服务、浏览器插件和自定义 Agent 编排

---

## 核心能力

| 能力 | 说明 |
|---|---|
| 记忆存储 | 支持 fact、preference、skill、episode、instruction、relationship |
| 智能去重 | `content_hash` 去重，支持语义召回链路 |
| 混合检索 | BM25 + 向量 + **加权 RRF 融合** + rerank + MMR + CJK 停用词优化 |
| 自动触发 | **FastText ML 分类器** (CJK/EN 双模型, 98%+) + `ShouldRetrieve` 自适应跳过 |
| 噪音过滤 | 过滤 AI 否认、元问题、样板文本 |
| 记忆合并 | LLM 发现模式、洞察和连接关系 |
| MCP 工具 | 9 个工具，stdio JSON-RPC |
| HTTP Tool API | `/api/v1/tools`、`/api/v1/tools/call`、`/api/v1/tools/{name}` |
| Legacy REST | 兼容旧接入，保留 `/api/memories/*` |

---

## 快速开始

### 1. 编译

```bash
git clone https://github.com/AetherX-Technologies/memory_agentic_RAG.git
cd memory_agentic_RAG

# 首次需要编译 FastText C++ 库
git clone --depth 1 https://github.com/facebookresearch/fastText.git /tmp/fasttext
cd /tmp/fasttext && mkdir build && cd build && cmake .. -DCMAKE_POSITION_INDEPENDENT_CODE=ON -DCMAKE_POLICY_VERSION_MINIMUM=3.5 && make -j$(nproc)
cp /tmp/fasttext/build/libfasttext_pic.a /path/to/memory_agentic_RAG/internal/fasttext/lib/libfasttext.a

# 编译
CGO_ENABLED=1 go build -tags fts5 -o hybridmem-mcp ./cmd/mcp_server/
CGO_ENABLED=1 go build -tags fts5 -o hybridmem-server ./cmd/server/
```

### 2. 运行 MCP

```bash
MEMORY_DB_PATH=./memory.db ./hybridmem-mcp
```

### 3. 运行 HTTP

```bash
MEMORY_HTTP_ADDR=127.0.0.1:8080 \
MEMORY_DB_PATH=./memory.db \
./hybridmem-server
```

### 4. 健康检查

```bash
curl http://127.0.0.1:8080/api/health
```

### 5. Tool API 示例

```bash
curl -X POST http://127.0.0.1:8080/api/v1/tools/memory_store \
  -H "Content-Type: application/json" \
  -d '{
    "content": "用户喜欢咖啡",
    "type": "preference"
  }'
```

```bash
curl -X POST http://127.0.0.1:8080/api/v1/tools/memory_recall \
  -H "Content-Type: application/json" \
  -d '{
    "query": "用户喜欢什么饮品",
    "limit": 5
  }'
```

---

## HTTP 说明

HTTP 当前分两层：

- `Tool API`
  推荐新接入使用，语义与 MCP 对齐
- `Legacy REST`
  兼容旧客户端和浏览器插件

推荐新接入优先使用：

- `GET /api/v1/tools`
- `POST /api/v1/tools/call`
- `POST /api/v1/tools/{name}`

详细参考：

- [HTTP API 详细文档](./docs/API.md)
- [使用指南中的 HTTP 章节](./docs/USAGE_GUIDE.md#5-http-api-使用)
- [HTTP 真实测试报告](./docs/HTTP_TEST_REPORT.md)

---

## MCP 工具

| 工具 | 说明 |
|---|---|
| `memory_store` | 存储记忆 |
| `memory_recall` | 召回记忆 |
| `memory_forget` | 软删除 |
| `memory_update` | 更新记忆 |
| `memory_export` | 导出 |
| `memory_import` | 导入 |
| `memory_forget_by_tag` | 按标签批量删除 |
| `memory_consolidate` | 触发合并 |
| `memory_should_capture` | 判断是否值得存储 |

---

## 目录结构

```text
internal/
├── api/             # HTTP Tool API + legacy REST
├── bootstrap/       # MCP/HTTP 共用启动装配
├── memservice/      # MCP/HTTP 共用业务语义
├── mcp/             # MCP 协议适配层
├── store/           # SQLite + FTS5 + 检索评分
├── trigger/         # ShouldCapture / ShouldRetrieve / IsNoise
├── consolidate/     # 记忆合并
├── config/          # 统一配置
└── embedder/        # embedding 接入
```

---

## 测试

推荐命令：

```bash
go test -tags fts5 ./internal/...
go run -tags fts5 ./cmd/full_memory_test/
go run -tags fts5 ./cmd/trigger_test/
```

HTTP 相关验证可以结合：

```bash
go test -tags fts5 ./internal/api ./internal/mcp ./internal/store ./internal/bootstrap ./internal/memservice
```

---

## 说明

- 当前 HTTP Tool API 与 MCP 共用同一套 bootstrap 和业务层
- 当前 legacy REST 仍保留，但不等同于 MCP 工具语义
- 浏览器插件当前仍依赖 legacy `POST /api/memories`

---

## License

MIT
