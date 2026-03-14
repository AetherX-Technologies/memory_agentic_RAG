# Memory LanceDB Pro 改造方案

## 一、关键澄清

### 1.1 LanceDB vs Memory LanceDB Pro

**LanceDB**（底层库）：
- 向量数据库核心
- 提供基础的向量存储和检索
- 有多语言绑定：Python、TypeScript、Go

**Memory LanceDB Pro**（你的本地项目）：
- 基于 `@lancedb/lancedb` (TypeScript SDK)
- **增强功能**：
  - 混合检索（Vector + BM25）
  - 12阶段评分管道
  - 交叉编码器重排
  - 作用域隔离
  - 自适应检索
  - 噪声过滤
  - Session Memory
  - CLI 工具

### 1.2 改造方案

**不是**：把 TypeScript 代码转成 Go
**而是**：
1. 底层：用 `lancedb-go` 替换 `@lancedb/lancedb`
2. 增强功能：用 Go 重新实现 Pro 的功能

```
Memory LanceDB Pro (TypeScript)
├── @lancedb/lancedb ────────┐
├── 混合检索                  │
├── 12阶段评分                │  参考设计
├── 作用域隔离                │  ↓
└── ...                      │
                             │
新项目 (Go)                   │
├── lancedb-go ←─────────────┘ 替换
├── 混合检索 (重新实现)
├── 12阶段评分 (重新实现)
├── 作用域隔离 (重新实现)
└── ...
```

---

## 二、核心模块对应关系

### 从 TypeScript 到 Go 的映射

| TypeScript 模块 | 功能 | Go 实现方式 |
|----------------|------|------------|
| `src/store.ts` | LanceDB 存储层 | 使用 `lancedb-go` |
| `src/embedder.ts` | 向量化 | 使用 `go-openai` |
| `src/retriever.ts` | 混合检索 + 评分 | 纯 Go 实现 |
| `src/scopes.ts` | 作用域管理 | 纯 Go 实现 |
| `src/noise-filter.ts` | 噪声过滤 | 纯 Go 实现 |
| `src/adaptive-retrieval.ts` | 自适应检索 | 纯 Go 实现 |
| `src/tools.ts` | MCP Tools | 纯 Go 实现 |
| `cli.ts` | CLI 工具 | 使用 `cobra` |
| `src/chunker.ts` | 长文本分块 | 纯 Go 实现 |

---

## 三、LanceDB Pro 的增强功能详解

### 3.1 基础 LanceDB 提供的能力

**LanceDB 核心功能**（`@lancedb/lancedb` 或 `lancedb-go` 都有）：
- ✅ 向量存储（BLOB 格式）
- ✅ 向量检索（L2、余弦、点积）
- ✅ 元数据过滤（WHERE 子句）
- ✅ 全文检索（FTS，基于 Tantivy）
- ✅ 表管理（创建、删除、列表）
- ✅ 批量操作（插入、更新、删除）

### 3.2 Memory LanceDB Pro 的增强层

**Pro 在 LanceDB 之上添加的功能**：

#### 增强1：混合检索引擎（`retriever.ts`）
- **Vector Search**：调用 LanceDB 的向量检索
- **BM25 Search**：调用 LanceDB 的 FTS 全文检索
- **RRF 融合**：应用层实现倒数排名融合算法
- **交叉编码器重排**：调用 Jina Reranker API 进行精排

#### 增强2：12阶段评分管道（`retriever.ts`）
- Stage 1: 自适应跳过（问候语、命令等）
- Stage 2: 查询向量化
- Stage 3: 并行检索（Vector + BM25）
- Stage 4: RRF 融合
- Stage 5: 交叉编码器重排
- Stage 6: 新近度提升（时间衰减）
- Stage 7: 重要性加权
- Stage 8: 长度归一化
- Stage 9: 访问强化衰减
- Stage 10: 关联图谱加权（需要连接表）
- Stage 11: 硬性过滤（最低分数阈值）
- Stage 12: 噪声过滤 + MMR 多样性

#### 增强3：作用域隔离（`scopes.ts`）
- 定义作用域类型：global, agent:<id>, custom:<name>, project:<id>, user:<id>
- 访问控制：每个 agent 只能访问授权的作用域
- 默认作用域：agent 自动使用 `agent:<id>` 作用域

#### 增强4：噪声过滤（`noise-filter.ts`）
- 拒绝回复模式（"I don't have information"）
- 元问题模式（"do you remember"）
- 会话样板（"hi", "hello", "HEARTBEAT"）
- 在存储前过滤，避免浪费 embedding API 调用

#### 增强5：自适应检索（`adaptive-retrieval.ts`）
- 跳过模式：问候语、命令、确认、emoji
- 强制检索模式：记忆相关查询
- CJK 感知阈值：中文查询的特殊处理

#### 增强6：长文本分块（`chunker.ts`）
- 超过 embedding 上下文限制时自动分块
- 语义分割：优先在句子边界分割
- 重叠窗口：保持上下文连贯性

#### 增强7：MCP Tools（`tools.ts`）
- memory_recall：混合检索接口
- memory_store：存储记忆（含去重检测）
- memory_forget：删除记忆
- memory_update：更新记忆
- memory_stats：统计信息
- memory_list：列表查询

#### 增强8：Session Memory（`index.ts`）
- 自动捕获触发词（MEMORY_TRIGGERS）
- 类别检测：preference, fact, decision, entity, other
- 会话级别的记忆管理

---

## 四、Go 改造策略

### 4.1 存储层改造（`store.ts` → Go）

**TypeScript 实现**：
```typescript
// 使用 @lancedb/lancedb
const db = await lancedb.connect(dbPath);
const table = await db.openTable("memories");
await table.add([{ id, text, vector, ... }]);
const results = await table.vectorSearch(vector).limit(k).toArray();
```

**Go 改造**：
```go
// 使用 lancedb-go
import "github.com/lancedb/lancedb-go"

db, _ := lancedb.Connect(dbPath)
table, _ := db.OpenTable("memories")
table.Add([]Memory{{ID: id, Text: text, Vector: vector, ...}})
results, _ := table.VectorSearch(vector).Limit(k).Execute()
```

**关键点**：
- LanceDB Go SDK 的 API 与 TypeScript 版本高度相似
- 主要差异在类型系统（Go 的强类型 vs TypeScript）
- FTS 全文检索：需要确认 Go SDK 是否支持，如不支持需自己实现

### 4.2 混合检索改造（`retriever.ts` → Go）

**核心算法**：RRF 融合

**TypeScript 实现**：
```typescript
function rrfFusion(vectorResults, bm25Results, k = 60) {
  const scores = new Map();
  vectorResults.forEach((r, rank) => {
    scores.set(r.id, (scores.get(r.id) || 0) + 1 / (rank + k));
  });
  bm25Results.forEach((r, rank) => {
    scores.set(r.id, (scores.get(r.id) || 0) + 1 / (rank + k));
  });
  return Array.from(scores.entries())
    .sort((a, b) => b[1] - a[1])
    .map(([id, score]) => ({ id, score }));
}
```

**Go 改造**：
```go
func RRFFusion(vectorResults, bm25Results []SearchResult, k int) []ScoredResult {
    scores := make(map[string]float64)
    for rank, r := range vectorResults {
        scores[r.ID] += 1.0 / float64(rank+k)
    }
    for rank, r := range bm25Results {
        scores[r.ID] += 1.0 / float64(rank+k)
    }
    // 排序并返回
    results := make([]ScoredResult, 0, len(scores))
    for id, score := range scores {
        results = append(results, ScoredResult{ID: id, Score: score})
    }
    sort.Slice(results, func(i, j int) bool {
        return results[i].Score > results[j].Score
    })
    return results
}
```

**关键点**：
- RRF 算法是纯逻辑，直接翻译即可
- 12阶段评分管道同样是纯逻辑，逐个实现
- 交叉编码器重排：调用 Jina API（HTTP 请求）

### 4.3 作用域管理改造（`scopes.ts` → Go）

**TypeScript 实现**：
```typescript
class MemoryScopeManager {
  getAccessibleScopes(agentId: string): string[] {
    return ["global", `agent:${agentId}`];
  }
  isAccessible(scope: string, agentId: string): boolean {
    return this.getAccessibleScopes(agentId).includes(scope);
  }
}
```

**Go 改造**：
```go
type ScopeManager struct {
    accessRules map[string][]string
}

func (sm *ScopeManager) GetAccessibleScopes(agentID string) []string {
    return []string{"global", fmt.Sprintf("agent:%s", agentID)}
}

func (sm *ScopeManager) IsAccessible(scope, agentID string) bool {
    accessible := sm.GetAccessibleScopes(agentID)
    for _, s := range accessible {
        if s == scope { return true }
    }
    return false
}
```

**关键点**：
- 纯逻辑代码，直接翻译
- 可以扩展为更复杂的 RBAC 系统

### 4.4 噪声过滤改造（`noise-filter.ts` → Go）

**TypeScript 实现**：
```typescript
const DENIAL_PATTERNS = [
  /i don'?t have (any )?(information|data)/i,
  // ...
];

function isNoise(text: string): boolean {
  return DENIAL_PATTERNS.some(p => p.test(text));
}
```

**Go 改造**：
```go
var denialPatterns = []*regexp.Regexp{
    regexp.MustCompile(`(?i)i don'?t have (any )?(information|data)`),
    // ...
}

func IsNoise(text string) bool {
    for _, pattern := range denialPatterns {
        if pattern.MatchString(text) {
            return true
        }
    }
    return false
}
```

**关键点**：
- 正则表达式语法略有差异，需要调整
- Go 的 `regexp` 包性能较好

### 4.5 MCP Tools 改造（`tools.ts` → Go）

**TypeScript 实现**：
```typescript
api.registerTool({
  name: "memory_recall",
  parameters: Type.Object({ query: Type.String(), ... }),
  async execute(params) {
    const results = await retriever.retrieve(params);
    return { content: [...], details: {...} };
  }
});
```

**Go 改造**：
```go
// 使用 MCP Go SDK（如果有）或自己实现 MCP 协议
func RegisterMemoryRecallTool(server *MCPServer) {
    server.RegisterTool(Tool{
        Name: "memory_recall",
        Parameters: map[string]interface{}{
            "query": map[string]string{"type": "string"},
        },
        Execute: func(params map[string]interface{}) (ToolResult, error) {
            query := params["query"].(string)
            results, _ := retriever.Retrieve(query)
            return ToolResult{
                Content: []Content{{Type: "text", Text: formatResults(results)}},
                Details: map[string]interface{}{"count": len(results)},
            }, nil
        },
    })
}
```

**关键点**：
- MCP 协议需要自己实现（Go 没有官方 SDK）
- 或者先实现 HTTP API，MCP 作为可选功能

---

## 五、iOS 适配性评估

### 5.1 技术挑战

**LanceDB Go SDK 在 iOS 上的问题**：

#### 挑战1：CGO + Rust 交叉编译
- LanceDB 核心是 Rust 编写
- Go SDK 通过 CGO 调用 Rust 库
- iOS 需要编译到 ARM64 架构
- 需要 Rust 工具链支持 iOS target

**编译流程**：
```bash
# 1. 安装 Rust iOS target
rustup target add aarch64-apple-ios

# 2. 编译 LanceDB Rust 库到 iOS
cargo build --target aarch64-apple-ios --release

# 3. Go 交叉编译到 iOS（需要 CGO）
CGO_ENABLED=1 \
GOOS=ios \
GOARCH=arm64 \
CC=clang \
go build -buildmode=c-archive -o liblancedb.a
```

**问题**：
- ❌ LanceDB Rust 库可能不支持 iOS target
- ❌ 即使支持，编译配置极其复杂
- ❌ 需要 Xcode、iOS SDK、Rust 工具链同时配置正确

#### 挑战2：App Store 限制
- iOS 应用不允许动态加载外部库（除非是系统框架）
- 所有代码必须静态链接到应用包内
- LanceDB 的 Rust 库需要静态编译

#### 挑战3：Go Mobile 限制
- `gomobile bind` 不支持 CGO
- 如果使用 CGO，无法通过 `gomobile` 打包成 iOS 框架
- 需要手动处理 Go 和 iOS 的互操作

### 5.2 可行性评估

**方案A：直接编译 LanceDB Go SDK 到 iOS**
- 可行性：⭐（极低）
- 难度：⭐⭐⭐⭐⭐
- 原因：
  - LanceDB Rust 库可能不支持 iOS
  - 即使支持，编译链极其复杂
  - 需要深入了解 Rust、CGO、iOS 工具链
- 时间成本：2-4 周（仅验证可行性）
- **不推荐**：投入产出比太低

**方案B：iOS 使用纯 Go 的 SQLite + 向量检索**
- 可行性：⭐⭐⭐⭐⭐（很高）
- 难度：⭐⭐⭐
- 方案：
  - 使用 `modernc.org/sqlite`（纯 Go，无 CGO）
  - 自己实现向量检索（暴力搜索或简单 IVF）
  - 通过 `gomobile bind` 打包成 iOS 框架
- 优势：
  - ✅ 纯 Go，跨平台编译简单
  - ✅ 无需 CGO，`gomobile` 完美支持
  - ✅ 性能对个人使用足够
- 劣势：
  - ⚠️ 需要自己实现向量检索
  - ⚠️ 性能不如 LanceDB
- 时间成本：2-3 周
- **推荐**：最现实的方案

**方案C：iOS 通过网络访问桌面版**
- 可行性：⭐⭐⭐⭐⭐（很高）
- 难度：⭐
- 方案：
  - 桌面运行 LanceDB 版本（HTTP API）
  - iOS 应用通过 HTTP 访问
  - 局域网或 VPN 连接
- 优势：
  - ✅ 实现简单，iOS 只需 HTTP 客户端
  - ✅ 桌面版功能完整（LanceDB）
  - ✅ 数据集中管理
- 劣势：
  - ⚠️ 需要桌面设备在线
  - ⚠️ 离线无法使用
- 时间成本：3-5 天
- **推荐**：作为过渡方案

**方案D：iOS 原生应用（Swift + 本地存储）**
- 可行性：⭐⭐⭐⭐
- 难度：⭐⭐⭐⭐
- 方案：
  - 用 Swift 重写 iOS 版本
  - 使用 Core Data 或 SQLite
  - 调用 OpenAI API 进行向量化
- 优势：
  - ✅ 原生体验最好
  - ✅ 无跨平台编译问题
- 劣势：
  - ⚠️ 需要维护两套代码（Go + Swift）
  - ⚠️ 开发成本高
- 时间成本：4-6 周
- **不推荐**：除非有专门的 iOS 开发资源

### 5.3 推荐策略

**阶段1：桌面优先（0-2个月）**
- 使用 LanceDB Go SDK
- 支持 Windows/macOS/Linux
- 提供 HTTP API

**阶段2：Android 适配（2-3个月）**
- 尝试 LanceDB Go SDK 在 Android 上编译
- 如果失败，使用方案B（纯 Go SQLite）
- 通过 Termux 或原生应用部署

**阶段3：iOS 适配（3-4个月）**
- **优先方案C**：iOS 通过 HTTP 访问桌面版
- **备选方案B**：如果需要离线，使用纯 Go SQLite
- **不考虑方案A**：LanceDB 直接编译到 iOS（风险太高）

### 5.4 iOS 技术路线图

```
┌─────────────────────────────────────────────────────────┐
│                    iOS 部署方案                          │
└─────────────────────────────────────────────────────────┘
                         │
         ┌───────────────┴───────────────┐
         │                               │
    方案C（推荐）                    方案B（备选）
    网络访问桌面版                  纯 Go 本地存储
         │                               │
    ┌────┴────┐                    ┌────┴────┐
    │ 优势    │                    │ 优势    │
    │ - 简单  │                    │ - 离线  │
    │ - 快速  │                    │ - 独立  │
    │ - 功能全│                    │ - 纯Go  │
    └────┬────┘                    └────┬────┘
         │                               │
    ┌────┴────┐                    ┌────┴────┐
    │ 劣势    │                    │ 劣势    │
    │ - 需在线│                    │ - 需实现│
    │ - 依赖  │                    │ - 性能  │
    └─────────┘                    └─────────┘
```

### 5.5 结论

**iOS 适配性总结**：

1. **LanceDB 直接编译到 iOS**：❌ 不可行（技术风险极高）
2. **纯 Go SQLite 方案**：✅ 可行（推荐作为本地方案）
3. **网络访问桌面版**：✅ 可行（推荐作为首选方案）

**建议**：
- 短期（3个月内）：iOS 通过 HTTP 访问桌面版
- 中期（3-6个月）：如果需要离线，实现纯 Go SQLite 版本
- 长期（6个月+）：评估用户需求，决定是否投入 Swift 原生开发

**关键决策点**：
- iOS 用户是否需要离线使用？
  - 不需要 → 方案C（网络访问）
  - 需要 → 方案B（纯 Go SQLite）
- 是否愿意维护两套代码？
  - 不愿意 → 方案B 或 C
  - 愿意 → 方案D（Swift 原生）

---

## 六、总结与行动计划

### 6.1 改造方案总结

**核心理解**：
- LanceDB 是底层向量数据库（有 Go SDK）
- Memory LanceDB Pro 是在 LanceDB 之上的增强应用
- 改造 = 用 `lancedb-go` 替换 `@lancedb/lancedb` + 用 Go 重新实现增强功能

**技术栈**：
```
桌面版（Windows/macOS/Linux）:
├── lancedb-go (CGO)
├── 混合检索引擎 (纯 Go)
├── 12阶段评分管道 (纯 Go)
├── 作用域管理 (纯 Go)
├── 噪声过滤 (纯 Go)
└── HTTP API + MCP Server (纯 Go)

Android 版:
├── 优先尝试 lancedb-go
└── 备选 modernc.org/sqlite + 纯 Go 向量

iOS 版:
├── 优先方案: HTTP 访问桌面版
└── 备选方案: modernc.org/sqlite + 纯 Go 向量
```

### 6.2 实现难度评估

| 模块 | 难度 | 工作量 | 风险 |
|------|------|--------|------|
| LanceDB 存储层 | ⭐⭐⭐ | 1周 | 中（SDK 不成熟） |
| 混合检索引擎 | ⭐⭐ | 2周 | 低（纯逻辑） |
| 12阶段评分管道 | ⭐⭐ | 2周 | 低（纯逻辑） |
| 作用域管理 | ⭐ | 3天 | 低（纯逻辑） |
| 噪声过滤 | ⭐ | 2天 | 低（正则表达式） |
| HTTP API | ⭐ | 1周 | 低（标准库） |
| MCP Server | ⭐⭐⭐ | 1周 | 中（需自己实现协议） |
| 浏览器插件 | ⭐⭐ | 3天 | 低（复用 TS 版本） |
| iOS 适配 | ⭐⭐⭐⭐ | 2-4周 | 高（技术不确定） |

**总计**：6-9周（桌面版），额外 2-4周（iOS）

### 6.3 下一步行动

**阶段1：技术验证（1周）**
```bash
# 任务1: 验证 lancedb-go 可用性
go get github.com/lancedb/lancedb-go
# 编写测试代码：插入、向量检索、FTS 检索

# 任务2: 验证跨平台编译
CGO_ENABLED=1 GOOS=windows go build
CGO_ENABLED=1 GOOS=darwin go build
CGO_ENABLED=1 GOOS=linux go build

# 任务3: 验证 FTS 支持
# 测试 LanceDB 的全文检索功能是否可用
```

**阶段2：核心功能实现（4周）**
- 周1: 存储层 + 向量检索
- 周2: 混合检索 + RRF 融合
- 周3: 12阶段评分管道
- 周4: 作用域 + 噪声过滤

**阶段3：接口层实现（2周）**
- 周5: HTTP API
- 周6: MCP Server（可选）

**阶段4：多端适配（2-4周）**
- 周7-8: Android 适配
- 周9-10: iOS 适配（如需要）

### 6.4 关键决策

**决策1：是否接受 CGO？**
- ✅ 接受 → 使用 LanceDB（功能强大，性能好）
- ❌ 拒绝 → 使用 SQLite（纯 Go，编译简单）
- **建议**：接受 CGO，桌面版用 LanceDB

**决策2：iOS 如何部署？**
- 方案A：网络访问桌面版（推荐，快速）
- 方案B：纯 Go SQLite（备选，离线）
- 方案C：放弃 iOS（如果用户少）
- **建议**：先方案A，根据反馈决定是否做方案B

**决策3：MCP Server 是否必需？**
- ✅ 必需 → 投入 1周实现协议
- ❌ 可选 → 先做 HTTP API，MCP 后续补充
- **建议**：HTTP API 优先，MCP 作为增强功能

### 6.5 成功标准

**MVP（最小可用版本）**：
- ✅ 桌面版能运行（Windows/macOS/Linux）
- ✅ 能存储和检索记忆
- ✅ 混合检索工作正常
- ✅ HTTP API 可用
- ✅ 浏览器插件能连接

**完整版**：
- ✅ 12阶段评分管道完整
- ✅ 作用域隔离生效
- ✅ 噪声过滤工作
- ✅ MCP Server 可用
- ✅ Android 能部署
- ✅ iOS 能访问（网络或本地）

### 6.6 风险与缓解

**风险1：LanceDB Go SDK 不稳定**
- 缓解：阶段1 提前验证，准备 SQLite 备选方案

**风险2：iOS 编译失败**
- 缓解：优先使用网络访问方案，降低技术风险

**风险3：性能不达标**
- 缓解：性能测试，必要时优化或切换方案

**风险4：开发时间超预期**
- 缓解：MVP 优先，功能分阶段交付

---

## 附录：参考资料

**LanceDB 相关**：
- LanceDB Go SDK: https://github.com/lancedb/lancedb-go
- LanceDB 文档: https://lancedb.github.io/lancedb/

**Go 相关**：
- modernc.org/sqlite: https://pkg.go.dev/modernc.org/sqlite
- go-openai: https://github.com/sashabaranov/go-openai
- cobra CLI: https://github.com/spf13/cobra

**MCP 协议**：
- MCP 规范: https://spec.modelcontextprotocol.io/

**参考项目**：
- Memory Agent (Go): `../memory-agent`
- Memory LanceDB Pro (TypeScript): `../memory-lancedb-pro-main`
- OpenViking (Python): `../OpenViking-main`

---

## 六、总结与行动计划

### 6.1 改造方案总结

**核心理解**：
- LanceDB 是底层向量数据库（有 Go SDK）
- Memory LanceDB Pro 是在 LanceDB 之上的增强应用
- 改造 = 用 `lancedb-go` 替换 `@lancedb/lancedb` + 用 Go 重新实现增强功能

**技术栈**：
```
桌面版（Windows/macOS/Linux）:
├── lancedb-go (CGO)
├── 混合检索引擎 (纯 Go)
├── 12阶段评分管道 (纯 Go)
├── 作用域管理 (纯 Go)
├── 噪声过滤 (纯 Go)
└── HTTP API + MCP Server (纯 Go)

Android 版:
├── 优先尝试 lancedb-go
└── 备选 modernc.org/sqlite + 纯 Go 向量

iOS 版:
├── 优先方案: HTTP 访问桌面版
└── 备选方案: modernc.org/sqlite + 纯 Go 向量
```

### 6.2 实现难度评估

| 模块 | 难度 | 工作量 | 风险 |
|------|------|--------|------|
| LanceDB 存储层 | ⭐⭐⭐ | 1周 | 中（SDK 不成熟） |
| 混合检索引擎 | ⭐⭐ | 2周 | 低（纯逻辑） |
| 12阶段评分管道 | ⭐⭐ | 2周 | 低（纯逻辑） |
| 作用域管理 | ⭐ | 3天 | 低（纯逻辑） |
| 噪声过滤 | ⭐ | 2天 | 低（正则表达式） |
| HTTP API | ⭐ | 1周 | 低（标准库） |
| MCP Server | ⭐⭐⭐ | 1周 | 中（需自己实现协议） |
| 浏览器插件 | ⭐⭐ | 3天 | 低（复用 TS 版本） |
| iOS 适配 | ⭐⭐⭐⭐ | 2-4周 | 高（技术不确定） |

**总计**：6-9周（桌面版），额外 2-4周（iOS）

### 6.3 下一步行动

**阶段1：技术验证（1周）**
```bash
# 任务1: 验证 lancedb-go 可用性
go get github.com/lancedb/lancedb-go
# 编写测试代码：插入、向量检索、FTS 检索

# 任务2: 验证跨平台编译
CGO_ENABLED=1 GOOS=windows go build
CGO_ENABLED=1 GOOS=darwin go build
CGO_ENABLED=1 GOOS=linux go build

# 任务3: 验证 FTS 支持
# 测试 LanceDB 的全文检索功能是否可用
```

**阶段2：核心功能实现（4周）**
- 周1: 存储层 + 向量检索
- 周2: 混合检索 + RRF 融合
- 周3: 12阶段评分管道
- 周4: 作用域 + 噪声过滤

**阶段3：接口层实现（2周）**
- 周5: HTTP API
- 周6: MCP Server（可选）

**阶段4：多端适配（2-4周）**
- 周7-8: Android 适配
- 周9-10: iOS 适配（如需要）

### 6.4 关键决策

**决策1：是否接受 CGO？**
- ✅ 接受 → 使用 LanceDB（功能强大，性能好）
- ❌ 拒绝 → 使用 SQLite（纯 Go，编译简单）
- **建议**：接受 CGO，桌面版用 LanceDB

**决策2：iOS 如何部署？**
- 方案A：网络访问桌面版（推荐，快速）
- 方案B：纯 Go SQLite（备选，离线）
- 方案C：放弃 iOS（如果用户少）
- **建议**：先方案A，根据反馈决定是否做方案B

**决策3：MCP Server 是否必需？**
- ✅ 必需 → 投入 1周实现协议
- ❌ 可选 → 先做 HTTP API，MCP 后续补充
- **建议**：HTTP API 优先，MCP 作为增强功能

### 6.5 成功标准

**MVP（最小可用版本）**：
- ✅ 桌面版能运行（Windows/macOS/Linux）
- ✅ 能存储和检索记忆
- ✅ 混合检索工作正常
- ✅ HTTP API 可用
- ✅ 浏览器插件能连接

**完整版**：
- ✅ 12阶段评分管道完整
- ✅ 作用域隔离生效
- ✅ 噪声过滤工作
- ✅ MCP Server 可用
- ✅ Android 能部署
- ✅ iOS 能访问（网络或本地）

### 6.6 风险与缓解

**风险1：LanceDB Go SDK 不稳定**
- 缓解：阶段1 提前验证，准备 SQLite 备选方案

**风险2：iOS 编译失败**
- 缓解：优先使用网络访问方案，降低技术风险

**风险3：性能不达标**
- 缓解：性能测试，必要时优化或切换方案

**风险4：开发时间超预期**
- 缓解：MVP 优先，功能分阶段交付

---

## 附录：参考资料

**LanceDB 相关**：
- LanceDB Go SDK: https://github.com/lancedb/lancedb-go
- LanceDB 文档: https://lancedb.github.io/lancedb/

**Go 相关**：
- modernc.org/sqlite: https://pkg.go.dev/modernc.org/sqlite
- go-openai: https://github.com/sashabaranov/go-openai
- cobra CLI: https://github.com/spf13/cobra

**MCP 协议**：
- MCP 规范: https://spec.modelcontextprotocol.io/

**参考项目**：
- Memory Agent (Go): `../memory-agent`
- Memory LanceDB Pro (TypeScript): `../memory-lancedb-pro-main`
- OpenViking (Python): `../OpenViking-main`

