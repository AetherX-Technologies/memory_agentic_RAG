# Chatbox × HybridMem-RAG 记忆系统集成方案

> 创建时间：2026-03-22
> 目标：将 HybridMem-RAG 记忆系统集成到 Chatbox 桌面客户端
> 状态：待审核

---

## 一、Chatbox 技术栈分析

| 组件 | 技术 |
|------|------|
| 框架 | Electron + React 18 + TypeScript |
| 构建 | electron-vite (Vite-based) |
| 状态管理 | Jotai (atoms) + React Query |
| 存储 | IndexedDB (localforage) + ElectronStore (JSON) |
| LLM 集成 | Vercel AI SDK (20+ providers) |
| RAG | Mastra RAG + LibSQL 向量存储 |
| MCP | 已支持 `@modelcontextprotocol/sdk` |
| 移动端 | Capacitor (iOS/Android) |

### 关键发现

1. **已有 MCP 支持** — Chatbox 已集成 MCP stdio 传输（`@modelcontextprotocol/sdk`），与 hybridmem-mcp 的 newline-delimited JSON-RPC 兼容（均遵循 MCP stdio 规范）
2. **已有 RAG 基础设施** — Knowledge Base 系统支持文件嵌入 + 向量检索
3. **系统提示注入点明确** — `injectModelSystemPrompt()` 是注入记忆的最佳位置
4. **上下文压缩 = 记忆候选** — 压缩摘要已经是 LLM 生成的高质量内容

### MCP 传输兼容性说明

hybridmem-mcp 使用 **newline-delimited JSON-RPC 2.0 over stdio**，这是 MCP 标准 stdio 传输格式。服务端实现以下 MCP 生命周期方法：

| 方法 | 说明 |
|------|------|
| `initialize` | 返回 protocolVersion `2024-11-05`、capabilities、serverInfo |
| `notifications/initialized` | 静默接收，不返回响应 |
| `tools/list` | 返回 8 个工具定义 |
| `tools/call` | 执行工具并返回 `{ content: [{ type: "text", text: "..." }] }` |

Chatbox 的 `@modelcontextprotocol/sdk` 使用同一 stdio 传输协议，无需额外适配层。

---

## 二、集成方案（6 种，按侵入性排列）

### 方案 A：MCP 直连（零侵入，推荐先行）

```
Chatbox ──MCP stdio──→ hybridmem-mcp ──→ SQLite 记忆库
```

**原理**：利用 Chatbox 已有的 MCP 支持，直接将 hybridmem-mcp 注册为 MCP Server。

**改动**：
- Chatbox 端：**零代码改动**
- 用户在 Chatbox 设置 → MCP 服务器 → 添加 hybridmem-mcp

**工作流**：
1. 用户对话时，Claude/GPT 自动调用 `memory_store` 存储记忆
2. 需要回忆时，Claude/GPT 调用 `memory_recall` 检索
3. 记忆合并通过 `memory_consolidate` 触发（**前提**：MCP Server 启动时需配置 `MEMORY_LLM_KEY` 环境变量，否则该工具返回 "consolidation unavailable — no LLM configured"）

**优点**：
- 零侵入，不改 Chatbox 任何代码
- 立即可用（只需配置）
- 支持所有支持 tool_use 的模型

**缺点**：
- 依赖模型主动调用工具（不是每次都会调）
- 记忆注入不是自动的
- 用户体验不够无缝

**适合**：快速验证、MVP、不想改 Chatbox 代码的场景

---

### 方案 B：系统提示自动注入（轻侵入）

```
用户输入 → [记忆检索] → 系统提示 += 记忆上下文 → LLM 调用
         ← [记忆提取] ← 对话完成 ← LLM 响应
```

**改动文件**：
- `src/renderer/packages/model-calls/stream-text.ts` — 在 LLM 调用前插入记忆检索
- `src/renderer/packages/model-calls/message-utils.ts` — 扩展 `injectModelSystemPrompt()`

**实现**：

```typescript
// message-utils.ts — 扩展系统提示
async function injectModelSystemPrompt(messages, settings, sessionSettings) {
  // 原有逻辑...

  // 新增：注入记忆上下文（取最新一条 user 消息作为检索 query）
  if (settings.memoryEnabled) {
    const userMessage = messages.findLast(m => m.role === 'user')?.content
    const memories = await memoryService.recall(userMessage, { limit: 5, maxTokens: 500 })
    if (memories.context) {
      systemPrompt += '\n\n' + memories.context
    }
  }
}
```

```typescript
// stream-text.ts — 对话完成后提取记忆
async function streamText(...) {
  // 原有 LLM 调用...

  // 新增：响应完成后异步提取记忆
  onComplete(async (messages) => {
    if (settings.memoryEnabled) {
      await memoryService.extractAndStore(messages)
    }
  })
}
```

**优点**：
- 全自动，用户无感知
- 每次对话都注入相关记忆
- 不依赖模型的 tool_use 能力

**缺点**：
- 需要修改 Chatbox 源码
- 记忆检索增加延迟（~50ms）
- 需要维护 fork

**适合**：深度集成、最佳用户体验

---

### 方案 C：Knowledge Base 融合（利用已有 RAG）

```
Chatbox KB 系统
  ├── 文件知识库（已有）
  └── 记忆知识库（新增）← HybridMem-RAG
```

**原理**：将记忆系统包装为 Chatbox Knowledge Base 的一种特殊类型。

**改动文件**：
- `src/renderer/platform/knowledge-base/interface.ts` — 添加 MemoryKB 类型
- `src/renderer/packages/model-calls/toolsets/knowledge-base.ts` — 扩展搜索

**实现**：

```typescript
// 新增 MemoryKnowledgeBase 实现 KnowledgeBaseController
class MemoryKnowledgeBase implements KnowledgeBaseController {
  async search(query: string, limit: number): Promise<KBResult[]> {
    // callTool 返回 MCP 格式: { content: [{ type: "text", text: "..." }] }
    // text 内容为 JSON 字符串，需要解析并映射到 KBResult[]
    const mcpResult = await mcpClient.callTool('memory_recall', { query, limit })
    const textContent = mcpResult.content?.[0]?.text
    if (!textContent) return []

    // memory_recall 返回的 memories 是 []SearchResult，每项形如:
    // { entry: { id, text, memory_type, importance, ... }, score }
    const parsed = JSON.parse(textContent) as {
      memories: Array<{ entry: { id: string; text: string; memory_type: string; importance: number }; score: number }>
    }

    return parsed.memories.map((m) => ({
      id: m.entry.id,
      content: m.entry.text,
      score: m.score,
      metadata: { type: m.entry.memory_type, importance: m.entry.importance },
    }))
  }

  async ingest(text: string, source: string): Promise<void> {
    await mcpClient.callTool('memory_store', { content: text, type: 'episode' })
  }
}
```

**优点**：
- 复用 Chatbox 已有的 KB 搜索 UI
- 用户可在 KB 面板中查看/管理记忆
- 与文件知识库共享嵌入/搜索基础设施

**缺点**：
- KB 是按需搜索，不是自动注入
- 需要用户主动启用"记忆知识库"
- KB 的 UI 不完全适合记忆展示

**适合**：想复用 Chatbox RAG UI 的场景

---

### 方案 D：上下文压缩 → 记忆存储（利用已有压缩）

```
对话过长 → Chatbox 自动压缩 → 摘要生成 → [存入记忆系统]
```

**原理**：Chatbox 已有上下文压缩（compaction），在压缩时自动将摘要存为长期记忆。

**改动文件**：
- `src/renderer/packages/context-management/compaction.ts`

**实现**：

```typescript
// compaction.ts — generateSummaryWithStream 后
async function handleCompaction(session, summary) {
  // 原有压缩逻辑...

  // 新增：将摘要作为长期记忆存储
  if (settings.memoryEnabled) {
    await memoryService.store({
      content: summary,
      type: 'episode',
      importance: 0.7,
      sourceConv: session.id,
      tags: ['compaction', 'summary']
    })
  }
}
```

**优点**：
- 利用已有的高质量摘要
- 不增加额外 LLM 调用
- 自然的记忆采集时机

**缺点**：
- 只在压缩触发时采集
- 摘要粒度较粗（不是逐条提取）
- 不能捕获未触发压缩的短对话

**适合**：作为方案 B 的补充

---

### 方案 E：独立记忆面板（UI 集成）

```
Chatbox UI
  ├── 对话列表（已有）
  ├── 设置面板（已有）
  └── 记忆面板（新增）
      ├── 记忆列表
      ├── 记忆搜索
      ├── 垃圾桶
      ├── 合并洞察
      └── 导入/导出
```

**改动文件**：
- 新增 `src/renderer/pages/MemoryPanel.tsx`
- 修改 `src/renderer/routes.tsx` — 添加路由
- 新增 `src/renderer/stores/memoryStore.ts` — 记忆状态管理

**功能**：
- 查看所有记忆（按类型/时间/重要性排序）
- 搜索记忆（语义检索）
- 手动添加/编辑/删除记忆
- 查看合并洞察
- 垃圾桶管理
- 导入/导出（备份迁移）
- 记忆统计仪表盘

**优点**：
- 完整的记忆管理 UI
- 用户可直接操控记忆
- 透明度高

**缺点**：
- UI 开发工作量大
- 需要大量 Chatbox 前端修改
- 维护成本高

**适合**：产品级集成

---

### 方案 F：Electron IPC 原生集成（最深度）

```
Chatbox Main Process
  ├── ElectronStore（已有）
  ├── Knowledge Base Controller（已有）
  └── Memory Controller（新增）
      ├── 内嵌 Go 子进程（hybridmem-mcp）
      ├── IPC 通信
      └── 自动生命周期管理
```

**原理**：在 Chatbox 的 Electron 主进程中启动 hybridmem-mcp 作为子进程，通过 IPC 通信。

**改动文件**：
- `src/main/main.ts` — 添加子进程管理
- `src/main/memory/memory-controller.ts` — 新增
- `src/preload/index.ts` — 暴露 IPC API
- `src/renderer/adapters/memory.ts` — 前端调用封装

**实现**：

```typescript
// main.ts — 启动子进程
import { spawn } from 'child_process'

const memoryProcess = spawn('./hybridmem-mcp', [], {
  env: { MEMORY_DB_PATH: path.join(app.getPath('userData'), 'memory.db') },
  stdio: ['pipe', 'pipe', 'pipe']
})

// IPC 通信
ipcMain.handle('memory:recall', async (_, query) => {
  const request = { jsonrpc: '2.0', id: nextId++, method: 'tools/call',
    params: { name: 'memory_recall', arguments: { query } } }
  memoryProcess.stdin.write(JSON.stringify(request) + '\n')
  // ... 读取 stdout 响应
})
```

**优点**：
- 最深度集成，性能最优（IPC vs HTTP）
- 记忆库与 Chatbox 数据共存
- 自动生命周期管理

**缺点**：
- 改动最大
- 需要打包 Go 二进制
- 跨平台二进制分发复杂

**适合**：长期产品方案

---

## 三、方案对比

| 维度 | A (MCP直连) | B (系统提示) | C (KB融合) | D (压缩存储) | E (UI面板) | F (IPC原生) |
|------|:---:|:---:|:---:|:---:|:---:|:---:|
| **代码改动** | 零 | 小 | 中 | 小 | 大 | 大 |
| **自动化** | 低 | 高 | 中 | 中 | 低 | 高 |
| **用户体验** | 中 | 高 | 中 | 低 | 高 | 最高 |
| **记忆质量** | 高 | 高 | 中 | 中 | — | 高 |
| **实现难度** | 低 | 低 | 中 | 低 | 高 | 高 |
| **维护成本** | 低 | 低 | 中 | 低 | 高 | 中 |

---

## 四、推荐路径

### 阶段 1（立即可用）：方案 A — MCP 直连

```
耗时：0.5 天
改动：零代码，仅配置
效果：基本可用，依赖模型主动调工具
```

### 阶段 2（短期优化）：方案 A + B 组合

```
耗时：2-3 天
改动：Chatbox 2 个文件（~100 行）
效果：全自动记忆注入 + 提取
```

### 阶段 3（产品级）：方案 B + D + E

```
耗时：1-2 周
改动：Chatbox 多个文件 + 新增 UI 组件
效果：完整记忆管理面板 + 自动提取 + 压缩存储
```

### 阶段 4（终极方案）：方案 F + E

```
耗时：2-3 周
改动：Electron 主进程 + UI + 打包
效果：原生集成，最佳性能和体验
```

---

## 五、方案 A 立即实施步骤

### 5.1 构建 MCP Server

```bash
cd /Volumes/SN770Coder/code/memory_agentic_RAG
go build -tags fts5 -o hybridmem-mcp ./cmd/mcp_server/
```

### 5.2 配置 Chatbox

Chatbox 设置 → MCP 服务器 → 添加：

```json
{
  "name": "Memory System",
  "command": "/path/to/hybridmem-mcp",
  "env": {
    "MEMORY_DB_PATH": "/Users/<username>/Library/Application Support/chatbox/memory.db",
    "MEMORY_LLM_KEY": "<可选，填入后启用 memory_consolidate>"
  }
}
```

> **注意**：`MEMORY_DB_PATH` 必须使用绝对路径，不可使用 `~`（shell 波浪线扩展在 JSON 配置中不生效）。
> `MEMORY_LLM_KEY` 为可选项，配置后 `memory_consolidate` 工具才可用（否则返回 "consolidation unavailable"）。

### 5.3 测试

在 Chatbox 对话中输入：
```
请记住：我是一名 Go 开发者，在西北院工作。
```

模型应调用 `memory_store` 工具。之后在新对话中问：
```
你还记得我的职业吗？
```

模型应调用 `memory_recall` 检索并回答。

---

## 六、方案 B 实施细节

### 6.1 新增文件

```
src/renderer/services/
  └── memoryService.ts    # 记忆服务封装（调用 MCP）
```

### 6.2 修改文件

```
src/renderer/packages/model-calls/message-utils.ts  # 注入记忆
src/renderer/packages/model-calls/stream-text.ts     # 提取记忆
src/shared/types/settings.ts                         # 添加记忆设置
```

### 6.3 设置项

```typescript
// settings.ts 新增
memoryEnabled: boolean          // 是否启用记忆
memoryMaxInject: number         // 每次注入最大记忆数（默认 5）
memoryMaxTokens: number         // 记忆上下文 token 预算（默认 500）
memoryAutoExtract: boolean      // 自动提取记忆
memoryConsolidateInterval: number // 合并间隔（分钟，默认 30）
```

---

## 七、数据隔离

| 数据 | 存储位置 | 说明 |
|------|---------|------|
| Chatbox 对话 | IndexedDB / SQLite | 不变 |
| Chatbox 设置 | ElectronStore (JSON) | 新增记忆相关设置项 |
| 记忆数据 | memory.db (SQLite) | 独立数据库，由 hybridmem-mcp 管理 |
| 合并洞察 | memory.db (consolidations 表) | 同上 |
| 向量索引 | memory.db (vectors 表) | 同上 |

记忆数据完全独立于 Chatbox 数据，互不影响。

---

## 八、风险与缓解

| 风险 | 缓解方案 |
|------|---------|
| Chatbox 更新后 fork 不兼容 | 最小化改动，集中在 2-3 个文件 |
| MCP 工具调用延迟 | 异步提取，检索加缓存 |
| 模型不主动调工具 | 方案 B 自动注入，不依赖工具 |
| 记忆膨胀 | 限流 + 自动清理 + 垃圾桶 |
| 隐私泄露 | 记忆全部本地存储，不上传 |
| Go 二进制跨平台 | 预编译 macOS/Linux/Windows 三端 |
