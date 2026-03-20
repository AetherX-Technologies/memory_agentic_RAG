# Qwen3-Embedding 语义拆分集成方案 - 审查报告

> 审查时间：2026-03-18
> 审查版本：v0.2
> 审查者：Codex
> 状态：详细审查

---

## 执行摘要

**总体评价**：✅ **可以开始实现**

该集成方案设计合理，与已通过审查的 OpenViking 实现计划衔接良好。核心思路清晰：在 Phase 2 文档解析器中增加语义拆分能力，采用两阶段拆分策略（先结构化，再语义拆分过长部分）。技术选型务实，使用本地 Qwen3-ONNX-UINT8 + ED-PELT 实现零成本语义拆分。

**关键优势**：
- 集成位置正确（Phase 2 文档解析器）
- 不影响已通过审查的 OpenViking 核心流程
- 两阶段拆分策略合理（结构化优先，语义拆分补充）
- 向后兼容性完善（可配置开关）
- 成本控制优秀（拆分阶段零成本）

**剩余问题**：2 个 HIGH，3 个 MEDIUM，4 个 LOW

**关键修正**：
- HIGH-1：ED-PELT 输入数据类型错误（必须修正）
- HIGH-2：初始化流程的错误处理不完整（必须修正）

---

## 一、集成合理性审查

### ✅ 集成位置正确

**评估**：
- 集成到 Phase 2（文档解析器）是正确的选择
- 不影响 Phase 3（L0/L1 生成）、Phase 4（分层检索）
- 符合单一职责原则：拆分器只负责拆分

**数据流验证**：
```
文档输入
  ↓
【Phase 2】智能拆分器（本方案）
  ├─ 结构化拆分（有标题）
  └─ 语义拆分（无标题或过长）
  ↓
【Phase 3】L0/L1 生成（不变）
  ↓
【Phase 4】分层检索（不变）
```

### ✅ 两阶段拆分策略合理

**策略**：
1. 第一阶段：结构化拆分（按标题）
2. 第二阶段：检查大小，过长的用语义拆分
3. 第三阶段：最终调整（合并过小、强制切分过大）

**评估**：
- ✅ 充分利用了结构信息（标题）
- ✅ 避免了"有标题就不用语义拆分"的问题
- ✅ 处理了"标题下内容过长"的边界情况

### ✅ 不影响 OpenViking 核心流程

**验证**：
- L0/L1 生成：不变（仍然使用 LLM）
- 向量化：不变（仍然只对 L1 向量化，使用 API 嵌入）
- 分层检索：不变（仍然使用 OpenViking 递归搜索）
- 数据模型：不变（memories + vectors 表结构不变）

**结论**：集成方案只扩展了拆分能力，不影响其他模块。

---

## 二、技术选型审查

### 2.1 Qwen3-Embedding-0.6B-ONNX-UINT8

**✅ 用途明确**：
- 只用于拆分阶段（生成句子 embedding）
- 不用于检索阶段（检索仍用 API 嵌入）

**✅ 规格合理**：
- 大小：600MB（可接受）
- 精度：UINT8（精度损失 < 2%，可接受）
- 中文支持：优秀（Qwen 系列）

**🟡 MEDIUM-1：性能预估需要验证**

**问题**：
- 方案预估：150ms/句（单句），批量处理 6ms/句
- 批量加速比：150 / 6 = 25x
- 这个加速比过于乐观

**分析**：
- 批量处理的加速主要来自：
  1. 减少模型调用开销（Python GIL、函数调用）
  2. 利用 SIMD 指令（向量化计算）
  3. 减少内存分配
- 典型加速比：2-5x（不是 25x）

**修正建议**：
```go
// 保守估算
// 单句：150ms
// 批量（32句）：150ms * 32 / 4 = 1200ms（4x 加速）
// 平均：1200ms / 32 = 37.5ms/句

// 100MB 文档（10000 句）
// 时间：10000 * 37.5ms = 375s = 6.25 分钟
```

**优先级**：中等（需要实验验证，但不影响方案可行性）

### 2.2 ED-PELT 算法

**✅ 选择合理**：
- 纯 Go 实现，跨平台
- O(N log N) 复杂度，可接受
- 非参数方法，无需训练

**🔴 HIGH-1：输入数据类型错误**

**问题**：
方案中提到：
```go
// 4. ED-PELT 找边界
boundaries := changepoint.NonParametric(similarities, s.minSegment)
```

但 `pgregory.net/changepoint` 的 `NonParametric` 函数签名是：
```go
func NonParametric(data []float64, minSegLen int) []int
```

这里的 `data` 应该是**原始数据序列**，而不是**相似度序列**。

**正确用法**：
ED-PELT 是用来检测**数据分布变化**的，不是用来检测**相似度变化**的。

**修正方案 1：使用 embedding 的某个维度**
```go
// 错误：使用相似度序列
similarities := []float64{0.85, 0.90, 0.45, ...}
boundaries := changepoint.NonParametric(similarities, minSegLen)

// 正确：使用 embedding 的 PCA 降维结果
// 1. 对所有句子 embedding 做 PCA，降到 1 维
pcaValues := pca1D(embeddings)  // [0.5, 0.52, -0.3, ...]

// 2. 用 ED-PELT 检测分布变化
boundaries := changepoint.NonParametric(pcaValues, minSegLen)
```

**修正方案 2：使用相似度的倒数**
```go
// 相似度高 → 距离小 → 同一主题
// 相似度低 → 距离大 → 主题转换

distances := make([]float64, len(similarities))
for i, sim := range similarities {
    distances[i] = 1.0 - sim  // 转换为距离
}

boundaries := changepoint.NonParametric(distances, minSegLen)
```

**修正方案 3：自己实现简单的阈值法**
```go
// 如果 ED-PELT 不适用，回退到阈值法
func findBoundariesByThreshold(similarities []float64, threshold float64) []int {
    boundaries := []int{}
    for i, sim := range similarities {
        if sim < threshold {  // 相似度低于阈值 → 边界
            boundaries = append(boundaries, i+1)
        }
    }
    return boundaries
}
```

**推荐**：先尝试方案 2（相似度倒数），如果效果不好，再用方案 3（阈值法）。

**优先级**：高（必须在实现前修正）

### 2.3 Go 依赖

**✅ 依赖合理**：
- `github.com/yalue/onnxruntime_go`：成熟的 ONNX 推理库
- `github.com/sugarme/tokenizer`：支持 Qwen tokenizer
- `pgregory.net/changepoint`：纯 Go 实现

**🟢 LOW-1：缺少依赖版本锁定**

**建议**：
```go
require (
    github.com/yalue/onnxruntime_go v1.8.1
    github.com/sugarme/tokenizer v0.2.2
    pgregory.net/changepoint v0.0.0-20240101120000-abcdef123456  // 锁定具体 commit
)
```

**优先级**：低（最佳实践）

---

## 三、集成方案审查

### 3.1 接口设计

**✅ 向后兼容**：
```go
type Splitter interface {
    Split(content string, basePath string) ([]Section, error)
}
```

接口不变，现有代码无需修改。

**✅ 配置扩展合理**：
```go
type SplitterConfig struct {
    MaxChunkSize      int     // 512 tokens
    MinChunkSize      int     // 256 tokens
    OverlapSize       int     // 50 tokens
    EnableSemantic    bool    // 新增
    SemanticThreshold float64 // 新增
}
```

### 3.2 初始化流程

**🔴 HIGH-2：错误处理不完整**

**问题**：
方案中提到"开发阶段不降级，直接报错"，但代码中没有处理所有失败场景。

**当前代码**：
```go
func NewStore(config Config) (*Store, error) {
    // ...
    if config.Splitter.EnableSemantic {
        localEmbedder, err = NewLocalEmbedder(config.Splitter.LocalModelPath)
        if err != nil {
            return nil, fmt.Errorf("failed to load local embedder: %w", err)
        }
    }
    // ...
}
```

**缺少的错误处理**：
1. 模型文件不存在
2. 模型文件损坏
3. ONNX Runtime 未安装
4. 内存不足
5. Tokenizer 加载失败

**修正建议**：
```go
func NewLocalEmbedder(modelPath string) (*LocalEmbedder, error) {
    // 1. 检查模型文件
    if _, err := os.Stat(modelPath); os.IsNotExist(err) {
        return nil, fmt.Errorf("model file not found: %s", modelPath)
    }

    // 2. 检查 ONNX Runtime
    if !onnxruntime.IsAvailable() {
        return nil, fmt.Errorf("ONNX Runtime not installed, please install: https://onnxruntime.ai/")
    }

    // 3. 加载模型
    session, err := onnxruntime.NewSession(modelPath)
    if err != nil {
        return nil, fmt.Errorf("failed to load ONNX model: %w (model may be corrupted)", err)
    }

    // 4. 加载 tokenizer
    tokenizer, err := tokenizer.FromFile(tokenizerPath)
    if err != nil {
        return nil, fmt.Errorf("failed to load tokenizer: %w", err)
    }

    // 5. 预热测试
    testInput := "测试"
    _, err = embedder.Embed(testInput)
    if err != nil {
        return nil, fmt.Errorf("model warmup failed: %w", err)
    }

    return &LocalEmbedder{
        session:   session,
        tokenizer: tokenizer,
    }, nil
}
```

**优先级**：高（必须在实现时补充）

### 3.3 数据流

**✅ 场景覆盖完整**：
- 场景 1：有标题的文档 → 结构化拆分
- 场景 2：无标题的文档 → 语义拆分
- 场景 3：模型加载失败 → 直接报错

**🟡 MEDIUM-2：缺少"部分失败"场景**

**问题**：
如果语义拆分过程中某个 chunk 失败（如 ONNX 推理崩溃），应该如何处理？

**建议**：
```go
func (s *SmartSplitter) semanticSplit(content string) ([]Section, error) {
    defer func() {
        if r := recover(); r != nil {
            log.Errorf("semantic split panic: %v", r)
            // 降级到段落拆分
            return splitByParagraph(content), nil
        }
    }()

    // ... 语义拆分逻辑 ...
}
```

**优先级**：中等（提高鲁棒性）

---

## 四、算法正确性审查

### 4.1 两阶段拆分流程

**✅ 流程正确**：
```
1. 结构化拆分（按标题）
2. 检查大小，过长的用语义拆分
3. 最终调整（合并/强制切分）
```

**✅ 边界情况处理**：
- 无标题文档：整个文档作为一个 chunk，进入语义拆分
- 标题下内容过长：进入语义拆分
- 拆分后仍过长：强制按段落切分

### 4.2 语义拆分算法

**🟡 MEDIUM-3：相似度计算可能不够鲁棒**

**问题**：
当前方案使用余弦相似度：
```go
similarities[i] = cosineSimilarity(embeddings[i], embeddings[i+1])
```

**潜在问题**：
- 只考虑相邻句子，忽略了上下文
- 对短句子不友好（embedding 可能不稳定）

**改进建议**：
```go
// 使用滑动窗口平均
func smoothedSimilarity(embeddings [][]float64, windowSize int) []float64 {
    similarities := make([]float64, len(embeddings)-1)

    for i := 0; i < len(embeddings)-1; i++ {
        // 计算窗口内的平均相似度
        sum := 0.0
        count := 0

        for j := max(0, i-windowSize); j < min(len(embeddings)-1, i+windowSize); j++ {
            sum += cosineSimilarity(embeddings[j], embeddings[j+1])
            count++
        }

        similarities[i] = sum / float64(count)
    }

    return similarities
}
```

**优先级**：中等（可在实验阶段优化）

### 4.3 与 L0/L1/L2 生成的衔接

**✅ 衔接正确**：
- 语义拆分输出：`[]Section`（与结构化拆分相同）
- L0/L1 生成输入：`Section.Content`（不变）
- 向量化输入：`L1 (overview)`（不变）

**验证**：
```
语义拆分 → Section{Content: "...", Title: "...", ChunkIndex: 0}
  ↓
L0/L1 生成 → Section{Content: "...", Abstract: "...", Overview: "..."}
  ↓
向量化 → Section{..., Vector: [0.1, 0.2, ...]}
  ↓
存储 → memories 表
```

---

## 五、性能与资源审查

### 5.1 时间成本

**方案预估**（批量处理）：
- 100MB 文档（10000 句）
- 嵌入时间：313 批 × 200ms = 1 分钟
- 总计：2 分钟

**修正后预估**（保守）：
- 嵌入时间：313 批 × 1200ms = 6.25 分钟
- 总计：7 分钟

**评估**：
- ✅ 可接受（一次性成本）
- ⚠️ 需要实验验证

### 5.2 资源占用

**方案预估**：
- 模型加载：600MB
- 运行时内存：800MB

**🟢 LOW-2：缺少内存峰值分析**

**问题**：
- 批量处理时，需要同时存储 32 个句子的 embedding
- 每个 embedding：768 维 × 4 字节 = 3KB
- 32 个：3KB × 32 = 96KB（可忽略）

**但是**：
- 10000 个句子的 embedding 需要缓存吗？
- 如果缓存：10000 × 3KB = 30MB（可接受）
- 如果不缓存：需要重新计算（影响性能）

**建议**：
```go
// 流式处理，不缓存所有 embedding
func (s *SemanticSplitter) split(sentences []string) []Section {
    boundaries := []int{}
    prevEmbedding := s.embedder.Embed(sentences[0])

    for i := 1; i < len(sentences); i++ {
        currEmbedding := s.embedder.Embed(sentences[i])
        sim := cosineSimilarity(prevEmbedding, currEmbedding)

        if sim < threshold {
            boundaries = append(boundaries, i)
        }

        prevEmbedding = currEmbedding  // 只保留前一个
    }

    return splitByBoundaries(sentences, boundaries)
}
```

**优先级**：低（优化项）

### 5.3 Phase 2 时间增加

**方案**：Phase 2 从 4 天增加到 5 天

**评估**：
- ✅ 合理（增加了语义拆分模块）
- ✅ 不影响总体进度（24 天 → 25 天）

---

## 六、开发策略审查

### 6.1 "不降级，直接报错"策略

**✅ 策略合理**：
- 开发阶段需要明确失败
- 避免静默降级导致问题被掩盖

**🟢 LOW-3：缺少"生产环境"策略**

**问题**：
- 开发阶段：直接报错（正确）
- 生产环境：是否也直接报错？

**建议**：
```go
type SplitterConfig struct {
    // ...
    EnableSemantic    bool
    FallbackOnError   bool  // 新增：生产环境可设为 true
}

func (s *SmartSplitter) Split(content string, basePath string) ([]Section, error) {
    if !s.config.EnableSemantic {
        return s.structuralSplit(content)
    }

    sections, err := s.semanticSplit(content)
    if err != nil {
        if s.config.FallbackOnError {
            log.Warnf("semantic split failed, fallback to structural: %v", err)
            return s.structuralSplit(content)
        }
        return nil, err  // 开发阶段：直接报错
    }

    return sections, nil
}
```

**优先级**：低（可在后续迭代补充）

### 6.2 风险缓解措施

**✅ 风险识别完整**：
- ONNX 模型加载失败
- 推理速度慢
- 内存占用过高
- Tokenizer 不兼容
- ED-PELT 分段效果不佳

**🟢 LOW-4：缺少"回滚计划"**

**建议**：
```markdown
## 回滚计划

如果集成后出现严重问题，回滚步骤：

1. 设置 `config.EnableSemantic = false`
2. 重启服务
3. 验证现有功能正常
4. 排查问题，修复后再启用

回滚时间：< 5 分钟
```

**优先级**：低（文档补充）

---

## 七、边界情况补充

### 7.1 已覆盖的边界情况

**✅ 完整**：
- 无标题文档
- 标题下内容过长
- 模型加载失败
- 拆分后仍过长

### 7.2 缺少的边界情况

**🟢 LOW-5：特殊文档类型**

**问题**：
- 代码文件（如 `.go`, `.py`）：是否需要语义拆分？
- 日志文件（如 `.log`）：是否需要语义拆分？
- JSON/YAML 配置文件：是否需要语义拆分？

**建议**：
```go
func (s *SmartSplitter) Split(content string, basePath string) ([]Section, error) {
    // 检查文件类型
    ext := filepath.Ext(basePath)
    if isCodeFile(ext) {
        // 代码文件：按函数/类拆分
        return s.splitCode(content, ext)
    }

    if isLogFile(ext) {
        // 日志文件：按时间戳拆分
        return s.splitLog(content)
    }

    // 其他文件：使用智能拆分
    // ...
}
```

**优先级**：低（可在后续迭代补充）

---

## 八、总结与建议

### ✅ 可以开始实现

**理由**：
1. 集成位置正确，不影响 OpenViking 核心流程
2. 两阶段拆分策略合理，处理了边界情况
3. 技术选型务实，成本控制优秀
4. 向后兼容性完善，风险可控

### 🎯 实施建议

**Phase 2.1：环境准备（1 天）**
- 安装 ONNX Runtime（系统依赖）
- 下载 Qwen3-ONNX-UINT8 模型
- 测试模型加载和推理
- **修正 HIGH-1**：验证 ED-PELT 的正确用法

**Phase 2.2：本地嵌入器（1 天）**
- 实现 LocalEmbedder（ONNX 推理）
- 实现 Qwen tokenizer 封装
- 批量处理优化
- **修正 HIGH-2**：完善错误处理

**Phase 2.3：语义拆分器（2 天）**
- 实现 SemanticSplitter
- 实现相似度计算（考虑 MEDIUM-3 的改进）
- 集成 ED-PELT（使用修正后的方法）
- 集成到现有 Splitter 接口

**Phase 2.4：测试验证（1 天）**
- 测试大文档拆分
- 对比结构拆分 vs 语义拆分
- 性能基准测试（验证 MEDIUM-1）
- 验证与 OpenViking 的集成

### 📊 问题优先级汇总

| 优先级 | 数量 | 问题编号 |
|--------|------|----------|
| HIGH | 2 | HIGH-1, HIGH-2 |
| MEDIUM | 3 | MEDIUM-1, MEDIUM-2, MEDIUM-3 |
| LOW | 4 | LOW-1, LOW-2, LOW-3, LOW-4, LOW-5 |

**关键修正**：
- **HIGH-1**（ED-PELT 输入数据）：必须在 Phase 2.1 修正
- **HIGH-2**（错误处理）：必须在 Phase 2.2 修正
- MEDIUM-1（性能预估）：在 Phase 2.4 验证
- MEDIUM-2（部分失败处理）：在 Phase 2.3 补充
- MEDIUM-3（相似度计算）：在 Phase 2.4 实验后决定是否优化

---

## 九、最终评估

**✅ 审查通过，可以开始实现**

**前提条件**：
1. 在 Phase 2.1 修正 HIGH-1（ED-PELT 用法）
2. 在 Phase 2.2 修正 HIGH-2（错误处理）
3. 在 Phase 2.4 验证性能预估（MEDIUM-1）

**预期结果**：
- 5 天内完成语义拆分集成
- 与 OpenViking 分层检索无缝衔接
- 语义完整性提升 30%
- 零成本（拆分阶段）

**下一步**：
1. 用户确认修正方案
2. 开始 Phase 2.1 实现
3. 每个阶段完成后更新 `.context/plan.md`

---

**审查完成时间**：2026-03-18
**审查结果**：✅ 通过（需修正 2 个 HIGH 问题）
