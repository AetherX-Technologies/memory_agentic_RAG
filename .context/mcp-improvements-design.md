# MCP 改进方案：Consolidate 实际执行 + 性能基准

> 创建时间：2026-03-22
> 来源：Codex MCP 协议评估建议
> 状态：待审查

---

## 一、memory_consolidate 改进

### 1.1 问题

当前 `memory_consolidate` MCP 工具只返回状态（`{status: "ready", unconsolidated: 3}`），不执行合并。用户调用期望看到合并结果，实际什么都没发生。

### 1.2 方案

将 `memory_consolidate` 改为实际执行合并。需要 MCP Server 持有 Consolidator 实例。

#### 架构变更

```go
// MCP Server 新增 consolidator 字段
type Server struct {
    store        store.Store
    embedder     store.Embedder
    consolidator *consolidate.Consolidator  // 新增，可为 nil
    config       Config
    handlers     map[string]ToolHandler
    mu           sync.Mutex
}

// Config 新增 LLM 配置（可选）
type Config struct {
    ServerName    string
    ServerVersion string
    LLMAPIKey     string  // 可选：用于 consolidation
    LLMModel      string
    LLMEndpoint   string
}
```

#### 工具行为

```
调用 memory_consolidate：
  1. 无 consolidator → 返回 {status: "unavailable", reason: "LLM not configured"}
  2. 未合并记忆 < 2 → 返回 {status: "skipped", unconsolidated: N}
  3. 执行合并成功 → 返回 {status: "completed", insight: "...", patterns: [...], connections: N, consolidated: N}
  4. 合并失败 → 返回 error
```

#### 工具描述更新

```json
{
  "name": "memory_consolidate",
  "description": "分析未合并的记忆，发现关联和模式，生成跨记忆洞察。需要至少2条未合并记忆。"
}
```

### 1.3 降级策略

- LLM API key 未配置 → 返回明确提示，不报错
- LLM 调用失败 → 返回错误信息，不影响其他工具
- 合并是可选增强功能，不是核心路径

---

## 二、性能基准测试

### 2.1 目标

验证系统在不同数据规模下的延迟和吞吐：

| 规模 | 记忆数 | 测试项 |
|------|--------|--------|
| 小 | 100 | 基线 |
| 中 | 1,000 | 日常使用 |
| 大 | 10,000 | 重度用户 |

### 2.2 测试维度

#### Store 性能
- Insert 延迟（p50/p95/p99）
- VectorSearch 延迟（不同 top-K）
- HybridSearch 延迟
- BM25Search 延迟

#### MCP 工具延迟
- memory_store 端到端延迟
- memory_recall 端到端延迟
- memory_export 延迟（全量导出）

#### 清理性能
- RunCleanup 在不同数据量下的执行时间

### 2.3 实现

```go
// cmd/benchmark_memory/main.go
func main() {
    for _, size := range []int{100, 1000, 10000} {
        // 1. 创建 DB，插入 N 条记忆
        // 2. 测量 Insert 延迟（最后 100 条的 p50/p95）
        // 3. 测量 VectorSearch 延迟（10 次查询的 p50/p95）
        // 4. 测量 HybridSearch 延迟
        // 5. 测量 Export 延迟
        // 6. 测量 Cleanup 延迟
        // 7. 打印表格
    }
}
```

### 2.4 性能目标（参考现有基准）

| 操作 | 100条 | 1000条 | 10000条 |
|------|-------|--------|---------|
| Insert | < 1ms | < 2ms | < 5ms |
| VectorSearch(top-10) | < 5ms | < 20ms | < 100ms |
| HybridSearch(top-10) | < 10ms | < 50ms | < 200ms |
| Export(all) | < 10ms | < 100ms | < 1s |
| Cleanup | < 5ms | < 20ms | < 100ms |

> 注：现有基准（M2 阶段）10000 条 VectorSearch = 81.7ms，HybridSearch 未测。

### 2.5 mock 向量

性能测试使用固定维度的随机向量（768维），不依赖真实嵌入模型。

---

## 三、实现计划

| 步骤 | 内容 | 工作量 |
|------|------|--------|
| 1 | MCP Server Config 扩展 + Consolidator 注入 | 小 |
| 2 | handleMemoryConsolidate 实际执行合并 | 小 |
| 3 | 性能基准测试程序 | 中 |
| 4 | 测试 + 审查 | 小 |

---

## 四、不做的事

- **不加 `formatVersion`** — 当前只有一个版本，premature
- **不改 `memory_recall` 返回格式** — 已经返回结构化 `memories` 数组
- **不做 100k 测试** — 个人知识库场景 10k 已足够
