# 记忆系统增强方案：自动触发 + 自适应跳过 + 噪音过滤

> 创建时间：2026-03-23
> 参考：memory-lancedb-pro (`/Volumes/SN770Coder/code/memory-lancedb-pro-main`)
> 状态：待审核
> 目标：补齐 HybridMem-RAG 与 memory-lancedb-pro 的关键差距

---

## 一、问题总结

当前记忆系统**所有模块都已实现，但没有自动触发入口**：

```
用户说："记住，我是Go开发者"
  ↓
Chatbox/MCP → 直接发给 LLM → 回复 → 结束
  ↓
记忆系统：😴 没人叫我
```

需要补齐 3 个核心机制：
1. **ShouldCapture** — 什么时候该存记忆
2. **ShouldRetrieve** — 什么时候该查记忆
3. **NoiseFilter** — 什么内容不该存

---

## 二、方案 1：自动触发存储（ShouldCapture）

### 2.1 触发词检测

参考 memory-lancedb-pro 的 `MEMORY_TRIGGERS`，设计多语言触发词表：

```go
// internal/trigger/capture.go

// 显式记忆指令（用户明确要求记住）
var explicitTriggers = []string{
    // 中文
    "记住", "请记住", "记下", "别忘了", "帮我记", "你记一下",
    // 英文
    "remember", "don't forget", "keep in mind", "note that",
}

// 隐式自我描述（用户在陈述自身信息）
var implicitTriggers = []string{
    // 身份/事实
    "我是", "我在", "我叫", "我的名字", "我的职业", "我的工作",
    "I am", "I'm a", "my name is", "I work at",
    // 偏好
    "我喜欢", "我偏好", "我习惯", "我更喜欢", "我觉得", "我认为",
    "I like", "I prefer", "I think", "I believe",
    // 否定偏好
    "我不喜欢", "我讨厌", "我不想", "不要",
    "I don't like", "I hate", "don't",
    // 技能
    "我会", "我擅长", "我学过", "我用过", "我做过",
    "I know", "I can", "I've been using",
    // 指令
    "以后", "每次", "总是", "一直", "请以后", "请用",
    "from now on", "always", "never",
    // 关系
    "是我的", "我的同事", "我的老板", "我的团队",
    "my colleague", "my boss", "my team",
}

// 正则模式（电话、邮箱等结构化信息）
var regexTriggers = []*regexp.Regexp{
    regexp.MustCompile(`\+\d{10,}`),            // 电话
    regexp.MustCompile(`[\w.-]+@[\w.-]+\.\w+`), // 邮箱
    regexp.MustCompile(`\d{4}[-/]\d{1,2}[-/]\d{1,2}`), // 日期
}
```

### 2.2 ShouldCapture 函数

```go
func ShouldCapture(text string) (bool, CaptureReason) {
    // 1. 长度过滤：太短或太长都不存
    runeLen := utf8.RuneCountInString(text)
    if isCJK(text) {
        if runeLen < 4 { return false, "" }  // 中文至少4字
    } else {
        if runeLen < 10 { return false, "" } // 英文至少10字
    }
    if runeLen > 500 { return false, "" }    // 太长的不自动存

    // 2. 显式触发词（最高优先级）
    for _, t := range explicitTriggers {
        if strings.Contains(text, t) {
            return true, ReasonExplicit
        }
    }

    // 3. 隐式触发词
    for _, t := range implicitTriggers {
        if strings.Contains(text, t) {
            return true, ReasonImplicit
        }
    }

    // 4. 正则匹配
    for _, r := range regexTriggers {
        if r.MatchString(text) {
            return true, ReasonPattern
        }
    }

    return false, ""
}
```

### 2.3 集成点

在 MCP Server 的消息处理中自动触发：

```go
// 方案1：在 memory_store 之外，新增 memory_auto_process 工具
// 方案2：在 Chatbox 集成层（stream-text.ts）中调用
// 方案3：在 MCP Server 的 message handler 中拦截

// 推荐方案2（Chatbox 集成层）：
// stream-text.ts → onComplete →
//   if ShouldCapture(userMessage) → Extract → Store
```

### 2.4 置信度分级

| 触发类型 | Confidence | 示例 |
|---------|-----------|------|
| 显式指令 | 0.95 | "记住，我是工程师" |
| 隐式自述 | 0.7 | "我在北京工作" |
| 正则模式 | 0.6 | "我的邮箱是 xxx@yyy.com" |
| LLM 提取 | 0.8-1.0 | LLM 判断后赋值 |
| Fallback 规则 | 0.3 | 当前 fallback 模式 |

---

## 三、方案 2：自适应检索跳过（ShouldRetrieve）

### 3.1 问题

当前每次 `memory_recall` 都执行检索。但 "你好"、"继续"、"ls" 这类消息不需要查记忆。

memory-lancedb-pro 报告：**~60-70% 的查询可以跳过**，大幅降低延迟和成本。

### 3.2 跳过模式

```go
// internal/trigger/retrieve.go

// 应该跳过检索的模式
var skipPatterns = []*regexp.Regexp{
    // 寒暄
    regexp.MustCompile(`(?i)^(hi|hello|hey|你好|嗨|早上好|晚上好|good\s*(morning|afternoon|evening))\b`),
    // 命令
    regexp.MustCompile(`(?i)^(run|build|test|ls|cd|git|npm|pip|docker|curl|grep|find|make|sudo)\b`),
    // 确认
    regexp.MustCompile(`(?i)^(yes|no|yep|nope|ok|okay|sure|fine|好的|是的|不是|继续|嗯)\s*[.!]?$`),
    // 操作指令
    regexp.MustCompile(`(?i)^(go ahead|continue|proceed|do it|start|begin|next|实施|开始|继续)\s*[.!]?$`),
    // 纯 emoji（Go regexp 不支持 \p{Emoji}，用 Unicode 范围近似）
    regexp.MustCompile(`^[\x{1F300}-\x{1F9FF}\x{2600}-\x{27BF}\x{FE00}-\x{FE0F}\x{200D}\s]+$`),
    // 斜杠命令
    regexp.MustCompile(`^/`),
    // 纯代码块
    regexp.MustCompile("(?s)^```.*```$"),
}

// 强制检索的模式（即使短也要查）
var forceRetrievePatterns = []*regexp.Regexp{
    // 明确回忆意图
    regexp.MustCompile(`(?i)\b(remember|recall|forgot|memory)\b`),
    regexp.MustCompile(`(你记得|还记得|之前|上次|以前|提到过|说过)`),
    // 时间引用
    regexp.MustCompile(`(?i)\b(last time|before|previously|yesterday|ago)\b`),
    // 个人信息查询
    regexp.MustCompile(`(?i)\b(my (name|email|phone|address|birthday|preference))\b`),
    regexp.MustCompile(`(我的(名字|邮箱|电话|地址|生日|偏好))`),
    // 反问
    regexp.MustCompile(`(?i)\b(what did (i|we)|did i (tell|say|mention))\b`),
    regexp.MustCompile(`(我(说过|提到过|告诉过你))`),
}
```

### 3.3 ShouldRetrieve 函数

```go
func ShouldRetrieve(text string) bool {
    cleaned := strings.TrimSpace(text)

    // 1. 强制检索（最高优先级，不受长度限制）
    //    "你记得吗"(4字)、"remember?"(9字) 等短查询必须命中
    for _, r := range forceRetrievePatterns {
        if r.MatchString(cleaned) {
            return true
        }
    }

    // 2. 长度过滤（在强制检索之后，避免拦截合法短查询）
    runeLen := utf8.RuneCountInString(cleaned)
    if isCJK(cleaned) {
        if runeLen < 6 { return false }   // 中文至少6字
    } else {
        if runeLen < 15 { return false }  // 英文至少15字
    }

    // 3. 跳过模式
    for _, r := range skipPatterns {
        if r.MatchString(cleaned) {
            return false
        }
    }

    // 4. 默认：执行检索
    return true
}
```

### 3.4 集成点

```go
// MCP memory_recall 内部
func (s *Server) handleMemoryRecall(ctx, params) {
    query := p.Query

    // 新增：自适应跳过
    if !trigger.ShouldRetrieve(query) {
        return map[string]interface{}{
            "count": 0,
            "context": "",
            "skipped": true,
            "reason": "query does not require memory retrieval",
        }, nil
    }

    // 原有检索逻辑...
}
```

---

## 四、方案 3：噪音过滤（NoiseFilter）

### 4.1 问题

当前系统会存储所有通过提取的内容，包括 AI 的拒绝回复、元问题、样板文本。

### 4.2 噪音模式

```go
// internal/trigger/noise.go

// AI 拒绝/否认模式（不应存为记忆）
var denialPatterns = []*regexp.Regexp{
    regexp.MustCompile(`(?i)(I don't have (any )?information|I wasn't able to find|no relevant memories)`),
    regexp.MustCompile(`(我没有(任何)?相关信息|我找不到|没有相关记忆|我不确定)`),
    regexp.MustCompile(`(?i)(I cannot recall|I don't recall|I'm not sure)`),
}

// 元问题模式（关于记忆系统本身的问题）
var metaPatterns = []*regexp.Regexp{
    regexp.MustCompile(`(?i)(do you remember|can you recall|did I tell you)`),
    regexp.MustCompile(`(你记得吗|你还记得|我告诉过你吗)`),
}

// 样板/重复模式
var boilerplatePatterns = []*regexp.Regexp{
    regexp.MustCompile(`(?i)^(hello|hi|hey|thanks|thank you|ok|okay)[\s.!]*$`),
    regexp.MustCompile(`^(你好|谢谢|好的|嗯)[\s。！]*$`),
    regexp.MustCompile(`(?i)HEARTBEAT`),
    regexp.MustCompile(`(?i)fresh session`),
}
```

### 4.3 IsNoise 函数

```go
func IsNoise(text string) (bool, string) {
    for _, r := range denialPatterns {
        if r.MatchString(text) { return true, "ai_denial" }
    }
    for _, r := range metaPatterns {
        if r.MatchString(text) { return true, "meta_question" }
    }
    for _, r := range boilerplatePatterns {
        if r.MatchString(text) { return true, "boilerplate" }
    }
    return false, ""
}
```

### 4.4 集成点

**两处过滤**：

```go
// 1. 存储前过滤（在 memory_store / StoreWithDedup 中）
func (s *Server) handleMemoryStore(ctx, params) {
    if isNoise, reason := trigger.IsNoise(p.Content); isNoise {
        return map[string]interface{}{
            "action": "filtered",
            "reason": "noise: " + reason,
        }, nil
    }
    // 原有存储逻辑...
}

// 2. 检索结果过滤（在 recall 返回前）
func filterNoiseResults(results []SearchResult) []SearchResult {
    var clean []SearchResult
    for _, r := range results {
        if isNoise, _ := trigger.IsNoise(r.Entry.Text); !isNoise {
            clean = append(clean, r)
        }
    }
    return clean
}
```

---

## 五、方案 4：MMR 多样性去重

### 5.1 问题

检索 top-10 可能有 5 条都在说 "用户是工程师" 的变体，浪费 token 预算。

### 5.2 实现

```go
// internal/store/mmr.go

// MMRRerank 应用最大边际相关性，降权相似结果。
// 注意：BM25Search 不填充 Memory.Vector，调用前需为无向量结果补充嵌入。
// 若向量为空，该条目不参与相似度惩罚（maxSim 保持 0），确保 BM25 结果不被错误跳过。
func MMRRerank(results []SearchResult, lambda float64, simThreshold float64) []SearchResult {
    if len(results) <= 1 { return results }

    // 复制切片，避免修改调用方的底层数组
    pool := make([]SearchResult, len(results))
    copy(pool, results)

    selected := []SearchResult{pool[0]}
    remaining := pool[1:]

    for len(remaining) > 0 && len(selected) < len(pool) {
        bestIdx := -1
        bestScore := -1.0

        for i, candidate := range remaining {
            // 计算与已选结果的最大相似度
            // 若任一方向量为空，cosine 返回 0（不惩罚）
            maxSim := 0.0
            if len(candidate.Entry.Vector) > 0 {
                for _, sel := range selected {
                    if len(sel.Entry.Vector) > 0 {
                        sim := cosineSimilarity(candidate.Entry.Vector, sel.Entry.Vector)
                        if sim > maxSim { maxSim = sim }
                    }
                }
            }

            // MMR: lambda * relevance - (1-lambda) * similarity
            mmrScore := lambda*candidate.Score - (1-lambda)*maxSim

            // 如果与已选太相似（>threshold），大幅降权
            if maxSim > simThreshold {
                mmrScore *= 0.3
            }

            if mmrScore > bestScore {
                bestScore = mmrScore
                bestIdx = i
            }
        }

        if bestIdx >= 0 {
            selected = append(selected, remaining[bestIdx])
            // 安全删除：不修改原始切片
            remaining = append(remaining[:bestIdx:bestIdx], remaining[bestIdx+1:]...)
        } else {
            break
        }
    }

    return selected
}
```

### 5.3 集成点

MMR 需要在 **MCP recall 路径** 中集成，因为 `HybridSearch` 直接返回 `topK` 结果，
不经过 `ApplyScoring`。正确的集成点是 `handleMemoryRecall`：

```go
// internal/mcp/tools.go — handleMemoryRecall 中，在 filter 之后、formatContext 之前
func (s *Server) handleMemoryRecall(ctx, params) {
    // ... HybridSearch + filter ...

    // 新增：MMR 多样性去重（需要向量，见下方注意事项）
    if config.MMREnabled && len(filtered) > 1 {
        filtered = store.MMRRerank(filtered, config.MMRLambda, config.MMRSimThreshold)
    }

    formatted := formatContext(filtered, memoryBudget)
    // ...
}
```

> **注意**：也应在 `ApplyScoring` 中添加 MMR 调用，确保层次检索路径同样受益。

---

## 六、方案 5：乘法时间衰减

### 6.1 问题

当前只有**加法新近度提升**（Stage 1），没有**乘法时间衰减**。

memory-lancedb-pro 有两个独立机制：
- **Recency Boost**（加法，14天半衰期）— 让新记忆排名靠前
- **Time Decay**（乘法，60天半衰期）— 让旧记忆全面降权

### 6.2 实现

```go
// scoring.go 新增

func applyTimeDecay(results []SearchResult, config ScoringConfig) {
    halfLife := config.TimeDecayHalfLifeDays
    if halfLife <= 0 { halfLife = 60 }
    floor := config.TimeDecayFloor
    if floor <= 0 { floor = 0.5 } // 可配置地板值，默认 0.5
    now := time.Now().Unix()

    for i := range results {
        ts := results[i].Entry.Timestamp
        if ts <= 0 { ts = now }
        ageDays := float64(now-ts) / SecondsPerDay

        // 乘法衰减：score *= floor + (1-floor) * exp(-ln(2) * age / halfLife)
        // 使用 ln(2) 确保恰好在 halfLife 天时衰减到 floor + (1-floor)*0.5
        // 即：60 天时得分 = floor + (1-floor)*0.5 = 0.75x（当 floor=0.5 时）
        decay := floor + (1-floor)*math.Exp(-math.Ln2*ageDays/float64(halfLife))
        results[i].Score *= decay
    }
}
```

### 6.3 ScoringConfig 扩展

```go
type ScoringConfig struct {
    // 现有字段...

    // 新增
    TimeDecayHalfLifeDays int     // 乘法衰减半衰期（默认 60）
    TimeDecayFloor        float64 // 衰减地板值（默认 0.5，范围 0-1）
    MMREnabled            bool    // 是否启用 MMR 多样性
    MMRLambda             float64 // MMR lambda（默认 0.7）
    MMRSimThreshold       float64 // MMR 相似度阈值（默认 0.85）
}
```

---

## 七、实现优先级

| 优先级 | 方案 | 影响 | 工作量 | 文件 |
|--------|------|------|--------|------|
| **P0** | 自动触发存储 | 解决"没人调"的根本问题 | 1天 | 新增 `internal/trigger/` |
| **P0** | 自适应检索跳过 | 减少 60-70% 无效检索 | 0.5天 | 同上 |
| **P1** | 噪音过滤 | 防止垃圾记忆 | 0.5天 | 同上 |
| **P1** | MMR 多样性 | 提升检索结果质量 | 0.5天 | `internal/store/` |
| **P2** | 乘法时间衰减 | 完善评分管道 | 0.5天 | `internal/store/scoring.go` |
| **合计** | | | **3天** | |

---

## 八、新增包结构

```
internal/trigger/
├── capture.go       # ShouldCapture — 判断是否存储
├── retrieve.go      # ShouldRetrieve — 判断是否检索
├── noise.go         # IsNoise — 噪音过滤
└── trigger_test.go  # 测试
```

---

## 九、与现有系统的关系

```
                    ┌─────────────┐
用户消息 ──────────→│ ShouldCapture│──→ true ──→ Extractor → Dedup → Store
                    └─────────────┘       │
                                          └──→ false ──→ 跳过

                    ┌──────────────┐
检索请求 ──────────→│ShouldRetrieve│──→ true ──→ VectorSearch → Scoring → MMR
                    └──────────────┘       │
                                           └──→ false ──→ 返回空

                    ┌─────────┐
存储/检索内容 ─────→│ IsNoise  │──→ true ──→ 丢弃
                    └─────────┘       │
                                      └──→ false ──→ 正常处理
```

**不影响任何现有模块**，纯增量添加。
