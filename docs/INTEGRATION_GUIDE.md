# HybridMem-RAG 集成指南

> 本文档说明如何把 HybridMem-RAG 作为 AI 记忆后端集成到你的外部项目中。
>
> 支持三种集成方式：**MCP Server**（最常用）、**HTTP API**、**Go 库直接导入**。

---

## 一、选哪种集成方式？

| 你的场景 | 推荐方式 |
|---------|---------|
| 使用 Claude Code / Claude Desktop / Chatbox 等 MCP 客户端 | **MCP Server** |
| 你的项目是 Python / Node.js / Java 等非 Go 语言 | **HTTP API** |
| 你的项目是 Go，想直接引用源码 | **Go 库导入** |
| 想用 AI Agent 通过工具调用访问记忆 | **MCP Server** 或 **HTTP Tool API** |

---

## 二、方式 A：MCP Server 集成（推荐）

适用于 **Claude Code**、**Claude Desktop**、**Cherry Studio**、**Cline** 等 MCP-aware 客户端。

### 2.1 一次性构建

```bash
# 1. 克隆项目
git clone https://github.com/AetherX-Technologies/memory_agentic_RAG.git
cd memory_agentic_RAG

# 2. 构建 FastText C++ 库（只需一次）
git clone --depth 1 https://github.com/facebookresearch/fastText.git /tmp/fasttext
cd /tmp/fasttext && mkdir build && cd build
cmake .. -DCMAKE_POSITION_INDEPENDENT_CODE=ON -DCMAKE_POLICY_VERSION_MINIMUM=3.5
make -j$(nproc)
cp /tmp/fasttext/build/libfasttext_pic.a /path/to/memory_agentic_RAG/internal/fasttext/lib/libfasttext.a

# 3. 构建 MCP server 二进制
cd /path/to/memory_agentic_RAG
CGO_ENABLED=1 go build -tags fts5 -o hybridmem-mcp ./cmd/mcp_server/
```

### 2.2 配置本地 config

创建 `config.local.yaml`（**不会被 git 追踪**）：

```yaml
store:
  db_path: "data/memories.db"        # 可改为绝对路径

embedding:
  provider: "local"                   # 或 "openai" / "jina"
  local:
    model_path: "models/qwen3-embedding-0.6b-onnx-uint8/dynamic_uint8.onnx"

llm:
  api_key: "YOUR_MAIN_LLM_KEY"
  model: "gpt-4o"                     # consolidation / 冲突检测用
  endpoint: "https://api.openai.com/v1/chat/completions"
  timeout: 30

  summary:                            # 可选：摘要等轻量任务用小模型
    api_key: "YOUR_LLM_KEY"           # 可与 main 相同
    model: "gpt-4o-mini"              # 小模型降成本
    endpoint: "https://api.openai.com/v1/chat/completions"
    timeout: 30
```

### 2.3 在客户端配置 MCP

**Claude Code / Claude Desktop** — 编辑 `~/Library/Application Support/Claude/claude_desktop_config.json`：

```json
{
  "mcpServers": {
    "memory": {
      "command": "/absolute/path/to/memory_agentic_RAG/hybridmem-mcp",
      "env": {
        "MEMORY_CONFIG_PATH": "/absolute/path/to/memory_agentic_RAG/config.local.yaml"
      }
    }
  }
}
```

**Cherry Studio / Cline** — 根据客户端文档填入相同的 command + env。

### 2.4 可用工具（9 个）

重启客户端后，你的 AI 可以自主调用：

| 工具 | 触发场景 |
|------|---------|
| `memory_store` | 用户说"记住..."或有值得保留的信息 |
| `memory_recall` | 需要历史上下文、用户偏好 |
| `memory_update` | 用户修正已有记忆 |
| `memory_forget` | 用户要求删除 |
| `memory_export` / `memory_import` | 备份 / 迁移 |
| `memory_forget_by_tag` | 按标签批量清理 |
| `memory_consolidate` | 触发跨记忆洞察发现 |
| `memory_should_capture` | 让 AI 先判断是否值得记 |

---

## 三、方式 B：HTTP API 集成

适用于 **Python / Node.js / Java / Rust** 等任何能发 HTTP 请求的项目。

### 3.1 启动 HTTP Server

```bash
# 构建
CGO_ENABLED=1 go build -tags fts5 -o hybridmem-server ./cmd/server/

# 运行
MEMORY_HTTP_ADDR=127.0.0.1:8080 \
MEMORY_CONFIG_PATH=/path/to/config.local.yaml \
./hybridmem-server
```

### 3.2 Python 客户端示例

```python
import requests

BASE = "http://127.0.0.1:8080/api/v1"

def remember(content, mem_type="fact", tags=None):
    r = requests.post(f"{BASE}/tools/memory_store", json={
        "content": content,
        "type": mem_type,
        "tags": tags or [],
    })
    return r.json()

def recall(query, limit=5, max_tokens=1000):
    r = requests.post(f"{BASE}/tools/memory_recall", json={
        "query": query,
        "limit": limit,
        "max_tokens": max_tokens,
    })
    return r.json()["context"]  # 已格式化的上下文字符串

# 使用
remember("用户偏好简洁的代码风格", mem_type="preference", tags=["代码风格"])
context = recall("用户的编程偏好")
print(context)
# [记忆系统召回 1 条]
# 💡 偏好
# - 用户偏好简洁的代码风格
```

### 3.3 Node.js 客户端示例

```javascript
const BASE = 'http://127.0.0.1:8080/api/v1';

async function remember(content, type = 'fact', tags = []) {
  const r = await fetch(`${BASE}/tools/memory_store`, {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({content, type, tags}),
  });
  return r.json();
}

async function recall(query, {limit = 5, maxTokens = 1000} = {}) {
  const r = await fetch(`${BASE}/tools/memory_recall`, {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({query, limit, max_tokens: maxTokens}),
  });
  const data = await r.json();
  return data.context;
}
```

### 3.4 完整 API 参考

见 [docs/API.md](./API.md)。

---

## 四、方式 C：作为 Go 库使用

### 4.1 添加依赖

```bash
go get github.com/AetherX-Technologies/memory_agentic_RAG@latest
```

### 4.2 最小示例

```go
package main

import (
    "context"
    "fmt"
    "crypto/sha256"
    "encoding/hex"

    "github.com/AetherX-Technologies/memory_agentic_RAG/internal/bootstrap"
    "github.com/AetherX-Technologies/memory_agentic_RAG/internal/dedup"
    "github.com/AetherX-Technologies/memory_agentic_RAG/internal/extractor"
    "github.com/AetherX-Technologies/memory_agentic_RAG/internal/memservice"
)

func main() {
    // 加载配置（读取 config.local.yaml 或 env vars）
    app, err := bootstrap.Load()
    if err != nil {
        panic(err)
    }
    defer app.Close()

    // 构造 dedup 管道（含长文本自动摘要）
    dd := dedup.New(app.Store, app.Embedder, dedup.DefaultConfigFromLLM(
        app.MainLLM.APIKey, app.MainLLM.Model, app.MainLLM.Endpoint, app.MainLLM.Timeout))
    if ab := app.Abstractor(); ab != nil {
        dd.SetAbstractor(ab)
    }

    // 存入一条记忆
    content := "用户是 Go 后端工程师，偏好简洁代码风格"
    h := sha256.Sum256([]byte(content))
    result, _ := dd.StoreWithDedup(context.Background(), extractor.ExtractedMemory{
        Content:     content,
        MemoryType:  "fact",
        Importance:  0.8,
        Confidence:  0.9,
        ContentHash: hex.EncodeToString(h[:8]),
        SourceConv:  "my-app",
    })
    fmt.Printf("Stored: %s (action=%s)\n", result.ID, result.Action)

    // 检索
    svc := memservice.New(app.Store, app.Embedder, app.Consolidator)
    svc.SetDedup(dd)
    resp, _ := svc.Recall(context.Background(), memservice.RecallRequest{
        Query:     "用户的技术背景",
        Limit:     5,
        MaxTokens: 1000,
    }, memservice.DefaultToolRecallOptions())
    fmt.Println(resp.Context)
}
```

---

## 五、配置详解

### 5.1 配置优先级

```
环境变量 > config.local.yaml > config.yaml > 代码默认值
```

| 文件 | 用途 | 是否提交到 Git |
|------|------|--------------|
| `config.yaml` | 默认配置模板（含 provider、model 等，**不含 API key**） | ✅ 提交 |
| `config.local.yaml` | 本地生产配置（含 API key） | ❌ **已 gitignore** |
| `MEMORY_CONFIG_PATH` 指定的文件 | 强制覆盖 | 按你决定 |

### 5.2 关键环境变量

| 变量 | 作用 |
|------|------|
| `MEMORY_CONFIG_PATH` | 显式指定配置文件路径 |
| `MEMORY_DB_PATH` | SQLite 数据库路径 |
| `MEMORY_EMBED_PROVIDER` | `local` / `jina` / `openai` |
| `MEMORY_EMBED_KEY` | 嵌入 API key |
| `MEMORY_LLM_KEY` | 主模型 API key |
| `MEMORY_LLM_MODEL` | 主模型名 |
| `MEMORY_LLM_ENDPOINT` | 主模型 endpoint |
| `MEMORY_LLM_SUMMARY_MODEL` | 小模型名（摘要用） |
| `MEMORY_LLM_SUMMARY_KEY` | 小模型 key（可与主模型相同） |
| `MEMORY_LLM_SUMMARY_ENDPOINT` | 小模型 endpoint |
| `MEMORY_CONSOLIDATION_TIMEOUT` | Consolidation 超时（秒，默认 ≥120） |
| `MEMORY_HTTP_ADDR` | HTTP server 监听地址（默认 `:8080`） |

### 5.3 嵌入模型选择

| Provider | 特点 | 适合 |
|----------|------|------|
| `local` (Qwen3-0.6B ONNX) | 离线、零延迟、免费 | 本地部署、低延迟场景 |
| `jina` (v3, 1024d) | 高精度、多语言 | 生产环境多语言文档 |
| `openai` (text-embedding-3) | OpenAI 原生 + 任何 OpenAI 兼容 API | 接入 GPT 体系 |

**首次使用新模型时自动校准阈值**（存到 `~/.hybridmem/calibration.json`）。

---

## 六、功能速览

### 6.1 已交付核心能力

| 能力 | 说明 |
|------|------|
| **CJK-aware Token 预算** | 按字符类分权（CJK ×1.5, ASCII ×0.25），误差 <10% |
| **智能记忆联想** | 存入时自动建立语义连接，recall 时展开关联记忆 |
| **长文本自动摘要** | >500 tokens 的记忆自动生成 Abstract（最高 300× 压缩） |
| **Leaf 语义分组 Consolidation** | 按 connections 图分组聚合，产生跨主题洞察 |
| **Embedder 阈值自动校准** | 首次使用新模型时 23 对标定，找到最优阈值 |
| **摘要模型分离** | 主 LLM (深度推理) 和 小 LLM (摘要)独立配置 |
| **Tags 完整持久化** | 所有 store/update/export/import 路径覆盖 |
| **SourceConv 过滤** | recall 可按对话 ID 过滤 + 显示来源 |

### 6.2 详细技术文档

- [架构总览](./architecture/INDEX.md)
- [智能记忆联想](./SMART_ASSOCIATION.md)
- [下一阶段规划](./NEXT_PHASE_PROPOSAL.md)
- [开发路线图](./DEV_ROADMAP.md)
- [使用指南](./USAGE_GUIDE.md)
- [HTTP API 参考](./API.md)
- [部署文档](./DEPLOYMENT.md)

---

## 七、常见问题

### Q1: 启动失败，说找不到 FastText 模型？

`trigger/capture.go` 需要 CJK + EN 双模型：
- `models/should_capture_best.ftz`（已随仓库提供）
- `models/should_capture_en_best.ftz`（已随仓库提供）

如果你从 release 下载的只是二进制，需要同时下载 models 目录并放在可执行文件同级。

### Q2: MCP 客户端连不上 server？

检查：
1. `command` 写的是**绝对路径**，不是相对路径
2. 二进制有执行权限（`chmod +x hybridmem-mcp`）
3. 查看客户端日志，通常会显示 bootstrap 的错误输出

### Q3: 长文本 Abstract 没生成？

Abstract 只在以下条件都满足时触发：
- 记忆内容 > 500 tokens
- 配置了 `llm.summary` 或 `llm` 段（有可用 API key）
- 通过 `StoreWithDedup` 路径存入（MCP/Tool API 走这里，Legacy REST POST 不走）

### Q4: 我不想用 OpenAI，想用自己的 LLM？

任何 OpenAI 兼容 API 都行（`/v1/chat/completions` 格式）。改 `endpoint` 字段即可。
支持流式 SSE 和标准 JSON 两种响应格式。

### Q5: 数据隐私？

- 全部数据在本地 SQLite（默认 `data/memories.db`）
- 嵌入可选 `local` provider 完全离线
- LLM 调用只发送当前被处理的单条/组记忆给你配的 API（不会收集到项目方服务器）

---

## 八、许可与贡献

- License: MIT
- Issues / PRs: https://github.com/AetherX-Technologies/memory_agentic_RAG
