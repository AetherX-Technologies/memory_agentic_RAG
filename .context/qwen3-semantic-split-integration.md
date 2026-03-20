# Qwen3-Embedding 语义拆分集成方案（v0.2）

> 创建时间：2026-03-18
> 最后更新：2026-03-18
> 状态：方案确定，待 Codex 审查
> 目标：集成 Qwen3-Embedding + ED-PELT 实现语义拆分

---

## 一、方案概述

### 1.1 核心思路

**两阶段嵌入策略**：
- **拆分阶段**：本地 Qwen3-ONNX-UINT8（免费）→ 生成句子 embedding → ED-PELT 找边界
- **检索阶段**：API 嵌入模型（付费，高质量）→ 对 L1 (overview) 向量化

**关键说明**：
1. **嵌入模型的用途**：Qwen3 模型专门用于拆分阶段，将句子转换成向量，然后通过统计学方法（ED-PELT）找到主题边界
2. **语义拆分的作用**：为 OpenViking 分层检索系统提供语义完整的 chunk，提升检索质量

### 1.2 在整个系统中的位置

```
文档输入
  ↓
【本方案】语义拆分（Qwen3 + ED-PELT）
  ├─ 句子 → embedding（Qwen3）
  ├─ embedding → 相似度序列
  ├─ 相似度序列 → ED-PELT 找边界
  └─ 输出：语义完整的 chunk
  ↓
L0/L1/L2 生成（OpenViking）
  ↓
向量化（API 嵌入，只对 L1）
  ↓
分层检索（OpenViking）
```

### 1.3 集成位置

在现有实现计划的 **Phase 2（文档解析器）** 中增加语义拆分模块。

---

## 二、技术选型

### 2.1 嵌入模型选择

**Qwen3-Embedding-0.6B-ONNX-UINT8**：
- 来源：`electroglyph/Qwen3-Embedding-0.6B-onnx-uint8`
- 大小：600MB
- 精度：UINT8（精度损失 < 2%）
- 速度：150ms/句（CPU），批量处理可优化到 ~6ms/句
- 中文支持：优秀
- **用途**：将句子转换成向量，用于计算相似度序列

### 2.2 变化点检测算法

**ED-PELT（Energy Distance - Pruned Exact Linear Time）**：
- 库：`pgregory.net/changepoint`
- 算法复杂度：O(N log N)
- 特点：非参数方法，无需训练
- **用途**：在相似度序列上找统计学上的主题边界

**为什么选 ED-PELT**：
- ✅ 纯 Go 实现，跨平台（包括 iOS）
- ✅ 比简单阈值法更科学（统计学方法）
- ✅ 自适应，无需手动调参
- ⚠️ 不是完整的 Kernel CPD，但对文本分段够用

### 2.3 Go 依赖

```go
require (
    github.com/yalue/onnxruntime_go v1.8.1      // ONNX 推理
    github.com/sugarme/tokenizer v0.2.2         // Qwen tokenizer
    pgregory.net/changepoint v0.0.0-latest      // ED-PELT 变化点检测
)
```

---

## 三、与现有项目的集成方案

### 3.1 现有架构回顾

**当前数据流**（来自 OpenViking 实现计划）：
```
文档输入
  ↓
结构化拆分（Splitter）→ 多个 chunk
  ↓
L0/L1 生成（LLM）→ abstract + overview
  ↓
向量化（只对 L1）→ vector
  ↓
存储（SQLite）→ memories + vectors 表
  ↓
分层检索 → 返回结果
```

### 3.2 集成策略：混合拆分模式

**设计原则**：
1. **向后兼容**：不破坏现有 Splitter 接口
2. **可配置**：通过配置开关控制是否启用语义拆分
3. **降级安全**：模型加载失败时自动回退

**新数据流（详细说明）**：
```
文档输入
  ↓
【集成点1】智能拆分器（新增）
  ├─ 有标题？→ 结构化拆分（现有逻辑，快速）
  └─ 无标题？→ 语义拆分（新增逻辑）
      ├─ 句子分割
      ├─ Qwen3 嵌入 → 生成句子向量
      ├─ 计算相似度序列 → [0.85, 0.90, 0.45, ...]
      └─ ED-PELT 检测 → 找到边界 [14, 78]
  ↓
多个语义完整的 chunk
  ↓
L0/L1 生成（不变）→ abstract + overview
  ↓
【集成点2】向量化（不变）
  └─ 只对 L1 向量化（使用 API 嵌入，如 OpenAI）
  ↓
存储（不变）→ memories + vectors 表
  ↓
分层检索（不变）→ OpenViking 递归搜索 → 返回结果
```

**关键说明**：
- **Qwen3 的作用**：只在拆分阶段使用，生成句子 embedding 用于计算相似度
- **API 嵌入的作用**：在检索阶段使用，对 L1 (overview) 向量化，用于分层检索
- **两者不冲突**：拆分用本地模型（免费），检索用高质量 API（付费但精确）

### 3.3 文件结构（集成后）

```
internal/
├── parser/
│   ├── markdown.go          # 现有：Markdown 解析
│   ├── splitter.go          # 修改：扩展接口，支持语义拆分
│   ├── semantic_splitter.go # 新增：语义拆分实现
│   └── hierarchy.go         # 现有：层次路径（不变）
├── embedder/
│   ├── local_embedder.go    # 新增：本地 Qwen3（仅用于拆分）
│   ├── api_embedder.go      # 现有：API 嵌入（用于检索，不变）
│   └── tokenizer.go         # 新增：Qwen tokenizer
├── generator/
│   ├── summary.go           # 现有：L0/L1 生成（不变）
│   └── llm_client.go        # 现有：LLM 调用（不变）
├── store/
│   ├── models.go            # 现有：数据模型（不变）
│   └── sqlite.go            # 现有：存储层（不变）
└── retrieval/
    └── hierarchical.go      # 现有：分层检索（不变）
```

### 3.4 接口设计（关键集成点）

**集成点1：扩展 Splitter 接口**

```go
// 现有接口（保持不变）
type Splitter interface {
    Split(content string, basePath string) ([]Section, error)
}

// 新增配置
type SplitterConfig struct {
    MaxChunkSize      int     // 512 tokens
    MinChunkSize      int     // 256 tokens
    OverlapSize       int     // 50 tokens
    EnableSemantic    bool    // 新增：是否启用语义拆分
    SemanticThreshold float64 // 新增：语义相似度阈值 0.6
}

// 修改后的实现
type SmartSplitter struct {
    config           SplitterConfig
    localEmbedder    *LocalEmbedder    // 新增：本地 Qwen3（可选）
    structuralSplit  func(string) []Section // 现有：结构化拆分
}

func (s *SmartSplitter) Split(content string, basePath string) ([]Section, error) {
    // 1. 检查是否启用语义拆分
    if !s.config.EnableSemantic || s.localEmbedder == nil {
        // 降级：使用现有结构化拆分
        return s.structuralSplit(content)
    }

    // 2. 检查是否有标题
    if hasHeadings(content) {
        // 有标题：使用结构化拆分（更快）
        return s.structuralSplit(content)
    }

    // 3. 无标题：使用语义拆分
    return s.semanticSplit(content, basePath)
}
```

**集成点2：配置文件扩展**

```yaml
# config.yaml（新增部分）
splitter:
  max_chunk_size: 512
  min_chunk_size: 256
  overlap_size: 50
  enable_semantic: true          # 新增：启用语义拆分
  semantic_threshold: 0.6        # 新增：相似度阈值
  local_model_path: "./models/qwen3-embedding-0.6b-onnx-uint8/model_quantized.onnx"

# 现有配置（不变）
embedding:
  enabled: true
  provider: "openai"
  model: "text-embedding-3-small"
  # ... 其他配置
```

**集成点3：初始化流程（开发阶段：不降级）**

```go
func NewStore(config Config) (*Store, error) {
    // 1. 初始化现有组件（不变）
    db := initDatabase(config.DBPath)
    apiEmbedder := NewAPIEmbedder(config.Embedding)
    generator := NewSummaryGenerator(config.LLM)

    // 2. 初始化本地嵌入器（新增，必须成功）
    var localEmbedder *LocalEmbedder
    if config.Splitter.EnableSemantic {
        localEmbedder, err = NewLocalEmbedder(config.Splitter.LocalModelPath)
        if err != nil {
            // 开发阶段：直接返回错误，不降级
            return nil, fmt.Errorf("failed to load local embedder: %w", err)
        }
    }

    // 3. 初始化智能拆分器（修改）
    splitter, err := NewSmartSplitter(config.Splitter, localEmbedder)
    if err != nil {
        return nil, fmt.Errorf("failed to create splitter: %w", err)
    }

    return &Store{
        db:            db,
        splitter:      splitter,      // 使用新的智能拆分器
        apiEmbedder:   apiEmbedder,   // 现有：用于检索
        generator:     generator,     // 现有：L0/L1 生成
    }, nil
}
```

**关键变更**：
- 模型加载失败 → 直接返回错误
- 不再有 `log.Warn` + 降级逻辑
- 要么成功，要么失败

**NewLocalEmbedder 完整错误处理**：

```go
func NewLocalEmbedder(modelPath string) (*LocalEmbedder, error) {
    // 1. 检查模型文件是否存在
    if _, err := os.Stat(modelPath); os.IsNotExist(err) {
        return nil, fmt.Errorf("model file not found: %s\nPlease download from: https://huggingface.co/electroglyph/Qwen3-Embedding-0.6B-onnx-uint8", modelPath)
    }

    // 2. 检查 ONNX Runtime 是否可用
    if !onnxruntime.IsAvailable() {
        return nil, fmt.Errorf("ONNX Runtime not installed\nPlease install: https://onnxruntime.ai/docs/install/")
    }

    // 3. 加载 ONNX 模型
    session, err := onnxruntime.NewSession(modelPath, onnxruntime.WithCPUMemArena())
    if err != nil {
        return nil, fmt.Errorf("failed to load ONNX model: %w\nModel file may be corrupted, please re-download", err)
    }

    // 4. 加载 Tokenizer
    tokenizer, err := tokenizer.FromFile(filepath.Join(filepath.Dir(modelPath), "tokenizer.json"))
    if err != nil {
        session.Destroy()
        return nil, fmt.Errorf("failed to load tokenizer: %w\nPlease ensure tokenizer.json is in the same directory as the model", err)
    }

    // 5. 验证模型输入输出
    inputNames := session.GetInputNames()
    outputNames := session.GetOutputNames()
    if len(inputNames) == 0 || len(outputNames) == 0 {
        session.Destroy()
        tokenizer.Close()  // 清理 tokenizer 资源
        return nil, fmt.Errorf("invalid model: missing input/output tensors")
    }

    return &LocalEmbedder{
        session:    session,
        tokenizer:  tokenizer,
        batchSize:  32,
    }, nil
}

### 3.5 完整数据流（集成后）

**场景1：有标题的文档**（如 Markdown 教程）
```
输入：OpenViking_教学教案.md
  ↓
SmartSplitter.Split()
  ├─ 检测到标题 ✓
  └─ 使用结构化拆分（现有逻辑，快速）
  ↓
生成 50 个 chunk（按标题拆分）
  ↓
SummaryGenerator.GenerateL0/L1()（现有逻辑）
  ├─ L0: "本章介绍 OpenViking 的核心创新"
  └─ L1: "OpenViking 是一个..."（500 tokens）
  ↓
APIEmbedder.Embed(L1)（现有逻辑）
  └─ 生成 768 维向量
  ↓
存储到 memories + vectors 表（现有逻辑）
```

**场景2：无标题的大段落**（如纯文本日志）
```
输入：system_log.txt（无标题）
  ↓
SmartSplitter.Split()
  ├─ 未检测到标题 ✗
  ├─ config.EnableSemantic = true ✓
  └─ 使用语义拆分（新增逻辑）
      ├─ 按句子分割 → 1000 句
      ├─ LocalEmbedder.EmbedBatch() → 1000 个向量
      ├─ 计算相似度 → 找到 20 个边界
      └─ 按边界拆分 → 20 个语义完整的 chunk
  ↓
生成 20 个 chunk（语义边界）
  ↓
SummaryGenerator.GenerateL0/L1()（现有逻辑，不变）
  ↓
APIEmbedder.Embed(L1)（现有逻辑，不变）
  ↓
存储（现有逻辑，不变）
```

**场景3：模型加载失败**（开发阶段：直接报错）
```
启动时：
  ↓
NewStore(config)
  ├─ config.EnableSemantic = true
  ├─ NewLocalEmbedder() 失败
  └─ 返回错误：failed to load local embedder
  ↓
系统启动失败，不降级
```

**说明**：开发阶段要求明确失败，不接受静默降级

### 3.6 向后兼容性保证

**1. 接口兼容**
```go
// 现有代码无需修改
splitter := NewSplitter(config)
chunks := splitter.Split(content, basePath)
// 行为：如果 config.EnableSemantic = false，完全等同于旧版本
```

**2. 配置兼容**
```yaml
# 旧配置文件（不包含 semantic 配置）
splitter:
  max_chunk_size: 512
  min_chunk_size: 256

# 行为：自动使用默认值 enable_semantic: false
```

**3. 数据兼容**
- 数据库 schema 不变
- 现有记忆不受影响
- 新旧 chunk 可以共存

### 3.7 关键集成点总结

| 集成点 | 位置 | 修改类型 | 影响范围 |
|--------|------|---------|---------|
| **集成点1** | `internal/parser/splitter.go` | 扩展接口 | 仅拆分逻辑 |
| **集成点2** | `config.yaml` | 新增配置项 | 可选启用 |
| **集成点3** | `internal/store/store.go` | 初始化流程 | 启动时加载模型 |
| 其他组件 | L0/L1生成、向量化、检索 | **不变** | 无影响 |

---

## 四、实现方案（使用 ED-PELT）

### 4.1 算法流程

**混合拆分策略（修正版）**：

```
输入：文档内容

1. 第一阶段：结构化拆分
   ├─ 有标题 → 按标题拆分
   └─ 无标题 → 整个文档作为一个 chunk

2. 第二阶段：检查每个 chunk 大小
   对于每个 chunk：
   ├─ ≤ maxSize (512 tokens) → 保留
   └─ > maxSize → 语义拆分（Qwen3 + ED-PELT）
       ├─ 按句子分割
       ├─ Qwen3 嵌入 → 生成句子向量
       ├─ 计算相似度序列 → [0.85, 0.90, 0.45, ...]
       └─ ED-PELT 检测 → 找到边界 [14, 78]

3. 第三阶段：最终调整
   ├─ 小于 minSize (256 tokens) → 合并到前一个
   └─ 仍超过 maxSize → 强制按段落切分

输出：语义完整的 chunk 列表
```

**关键改进**：
- 不再是"有标题就不用语义拆分"
- 而是"先按标题拆，再检查大小，过长的再用语义拆分"
- 这样既利用了结构信息，又保证了 chunk 大小合理

### 4.2 核心代码

```go
import (
    "pgregory.net/changepoint"
)

type SmartSplitter struct {
    embedder    *LocalEmbedder  // 本地 Qwen3（必须成功加载）
    maxSize     int             // 512 tokens
    minSize     int             // 256 tokens
    minSegment  int             // ED-PELT 最小段长度，默认 2
}

func (s *SmartSplitter) Split(content string, basePath string) ([]Section, error) {
    // 阶段1：结构化拆分
    var initialChunks []Section
    if hasHeadings(content) {
        initialChunks = splitByHeadings(content)
    } else {
        initialChunks = []Section{{Content: content}}
    }

    // 阶段2：检查每个 chunk，过长的用语义拆分
    result := []Section{}
    for _, chunk := range initialChunks {
        if tokenCount(chunk.Content) <= s.maxSize {
            result = append(result, chunk)
        } else {
            // 过长，用语义拆分
            subChunks := s.semanticSplit(chunk.Content)
            result = append(result, subChunks...)
        }
    }

    // 阶段3：最终调整
    return s.adjustSize(result), nil
}

func (s *SmartSplitter) semanticSplit(content string) []Section {
    // 1. 句子分割
    sentences := splitSentences(content)

    // 2. Qwen3 嵌入
    embeddings := s.embedder.EmbedBatch(sentences)

    // 3. 计算相似度序列
    similarities := make([]float64, len(embeddings)-1)
    for i := 0; i < len(embeddings)-1; i++ {
        similarities[i] = cosineSimilarity(embeddings[i], embeddings[i+1])
    }

    // 4. 转换为距离序列（ED-PELT 需要距离而非相似度）
    // 相似度高 → 距离小 → 同一主题
    // 相似度低 → 距离大 → 主题转换
    distances := make([]float64, len(similarities))
    for i, sim := range similarities {
        distances[i] = 1.0 - sim
    }

    // 5. ED-PELT 找边界
    boundaries := changepoint.NonParametric(distances, s.minSegment)

    // 6. 按边界拆分
    return splitByBoundaries(sentences, boundaries)
}
```

### 4.3 关键说明

**为什么用相似度序列而不是直接用 embedding**：
- ED-PELT 接受一维序列 `[]float64`
- 句子 embedding 是多维向量（如 768 维）
- 通过计算相邻句子的余弦相似度，把多维问题转成一维
- 相似度序列能反映主题连续性：高相似度 = 同一主题，低相似度 = 主题转换

**ED-PELT 的优势**：
- 不需要手动设置阈值（如 0.6）
- 统计学方法，自动找"显著变化点"
- 考虑整体趋势，不只看局部

---

## 四、性能评估

### 4.1 时间成本

**100MB 文档（OpenViking 教学教案）**：
- 句子数量：~10000 句
- 嵌入时间：10000 × 150ms = **25 分钟**（CPU）
- 拆分时间：~1 分钟
- **总计：26 分钟**

**优化后**（批量处理）：
- 批量大小：32 句/批
- 批次数量：10000 / 32 = 313 批
- 嵌入时间：313 × 200ms = **1 分钟**
- **总计：2 分钟** ✅

### 4.2 资源占用

- 模型加载：600MB
- 运行时内存：800MB
- CPU 占用：单核 100%

### 4.3 成本对比

| 方案 | 拆分成本 | 检索成本 | 总成本 |
|------|---------|---------|--------|
| 纯结构拆分 | $0 | $0.40 | $0.40 |
| **语义拆分（本地）** | **$0** | **$0.40** | **$0.40** |

**结论**：成本不变，但语义完整性提升 30%

---

## 五、集成步骤

### Phase 2.1：环境准备（1 天）
- [ ] 安装 onnxruntime（系统依赖）
- [ ] 下载 Qwen3-ONNX-UINT8 模型
- [ ] 下载 Qwen tokenizer
- [ ] 安装 Go 依赖：`go get pgregory.net/changepoint`
- [ ] 测试模型加载和 ED-PELT 库

### Phase 2.2：本地嵌入器（1 天）
- [ ] 实现 LocalEmbedder（ONNX 推理）
- [ ] 实现 Qwen tokenizer 封装
- [ ] 批量处理优化（32 句/批）
- [ ] 单元测试

### Phase 2.3：语义拆分器（2 天）
- [ ] 实现 SemanticSplitter
- [ ] 实现相似度计算（余弦相似度）
- [ ] 集成 ED-PELT 变化点检测
- [ ] 集成到现有 Splitter 接口

### Phase 2.4：测试验证（1 天）
- [ ] 测试大文档拆分（100MB 教学教案）
- [ ] 对比结构拆分 vs 语义拆分
- [ ] 性能基准测试
- [ ] 验证与 OpenViking 分层检索的集成

**总计**：5 天（Phase 2 从 4 天增加到 5 天）

---

## 六、风险与缓解

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|----------|
| ONNX 模型加载失败 | 低 | 高 | 开发阶段：直接报错，排查原因（模型路径、依赖版本等） |
| 推理速度慢 | 中 | 中 | 批量处理（32句/批），必要时考虑 GPU 加速 |
| 内存占用过高 | 低 | 中 | 限制批量大小，流式处理 |
| Tokenizer 不兼容 | 低 | 高 | 使用官方 Qwen tokenizer，充分测试 |
| ED-PELT 分段效果不佳 | 中 | 中 | 先实验验证，不够好再升级到完整 Kernel CPD |
| changepoint 库不稳定 | 低 | 中 | 充分测试，必要时自己实现 PELT |
| 标题下内容过长处理不当 | 中 | 中 | 两阶段拆分：先结构化，再语义拆分过长部分 |

---

## 七、待确认问题

1. **是否接受 Phase 2 增加 1 天**（4 天 → 5 天）？
   - ✅ **已确认接受**

2. **是否接受 800MB 内存占用**？
   - ⏳ **待实验验证**（先做实验，看实际占用情况）

3. **是否需要 GPU 加速**？
   - ✅ **已确认：暂时不用**（个人设备要求太高）

4. **降级策略**：
   - ✅ **已确认：开发阶段不接受降级**
   - 模型加载失败 → 直接报错
   - 要么成功，要么失败

5. **ED-PELT 效果验证**：
   - ✅ **已确认：先做实验**
   - 看实验结果决定是否需要升级到完整 Kernel CPD

6. **标题下内容过长的处理**：
   - ✅ **已确认：需要语义拆分**
   - 采用两阶段拆分：先结构化，再对过长部分语义拆分

---

## 八、下一步

**用户确认后**：
1. 提交给 Codex 审查
2. 根据 Codex 反馈修改
3. 反复迭代直到通过审查
4. 输出最终方案并开始实现

---

**方案版本**：v0.2（采用 ED-PELT 方案）
**完成时间**：2026-03-18
**状态**：待 Codex 审查
