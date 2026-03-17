# OpenViking 整合方案

> 将 OpenViking 的分层检索思想整合到 HybridMem-RAG 中

## 1. OpenViking 核心特性

**项目信息**：
- **仓库**：https://github.com/volcengine/OpenViking
- **Star 数**：12,487
- **语言**：Python
- **定位**：AI Agent 的上下文数据库

**核心理念**：
- 使用**文件系统范式**管理上下文（记忆、资源、技能）
- **分层上下文传递**（Hierarchical Context Delivery）
- 自我进化能力

## 2. 关键创新点

### 2.1 文件系统范式

OpenViking 将记忆组织成类似文件系统的层次结构：

```
/project
  /src
    /auth
      login.ts 相关记忆
      session.ts 相关记忆
    /api
      routes.ts 相关记忆
  /docs
    README.md 相关记忆
```

**优势**：
- 自然的层次结构
- 上下文局部性（相关记忆聚集）
- 搜索方向性（从当前层向上/向下搜索）

### 2.2 分层检索策略

不是全局搜索，而是：
1. 从当前文件所在层开始
2. 逐层向上搜索父目录
3. 每层独立评分
4. 跨层聚合结果

## 3. 整合到 HybridMem-RAG

### 3.1 数据模型扩展

**当前模型**：
```sql
CREATE TABLE memories (
  id TEXT PRIMARY KEY,
  content TEXT,
  vector BLOB,
  scope TEXT,
  -- ...
);
```

**扩展后**：
```sql
CREATE TABLE memories (
  id TEXT PRIMARY KEY,
  content TEXT,
  vector BLOB,
  scope TEXT,
  hierarchy_path TEXT,  -- 新增：层次路径
  hierarchy_level INT,  -- 新增：层级深度
  -- ...
);

CREATE INDEX idx_hierarchy ON memories(hierarchy_path);
```

### 3.2 存储时解析层次

```go
// 来源：OpenViking 的文件系统范式
func (s *Store) Insert(memory Memory) error {
    // 解析层次路径
    if memory.Source == "file" {
        memory.HierarchyPath = parseFilePath(memory.Metadata.FilePath)
        memory.HierarchyLevel = strings.Count(memory.HierarchyPath, "/")
    }

    return s.db.Insert(memory)
}

// 示例：/project/src/auth/login.ts
// 生成层次：
// - /project (level 1)
// - /project/src (level 2)
// - /project/src/auth (level 3)
```

### 3.3 分层混合检索

**核心算法**：结合 OpenViking 的分层思想 + Memory LanceDB Pro 的混合检索

```go
func (r *Retriever) HierarchicalHybridSearch(query string, currentPath string) []Memory {
    // 1. 解析当前路径的所有层级
    levels := parseHierarchyLevels(currentPath)
    // 例：["/project", "/project/src", "/project/src/auth"]

    var allResults []Memory

    // 2. 在每一层执行混合检索
    for i, level := range levels {
        // 向量检索 + BM25 检索（来源：Memory LanceDB Pro）
        vectorResults := r.vectorSearchInLevel(query, level, limit)
        bm25Results := r.bm25SearchInLevel(query, level, limit)

        // RRF 融合
        fusedResults := rrfFusion(vectorResults, bm25Results)

        // 层级加权（来源：OpenViking 的分层思想）
        weight := calculateLevelWeight(i, len(levels))
        for j := range fusedResults {
            fusedResults[j].Score *= weight
        }

        allResults = append(allResults, fusedResults...)
    }

    // 3. 跨层聚合
    return aggregateAndRerank(allResults, limit)
}

// 层级权重计算
func calculateLevelWeight(levelIndex, totalLevels int) float64 {
    // 当前层权重最高，越远权重越低
    distance := totalLevels - levelIndex - 1
    return math.Pow(0.8, float64(distance))
}
```

### 3.4 权重策略

| 层级 | 路径 | 权重 | 说明 |
|------|------|------|------|
| 3 | /project/src/auth | 1.0 | 当前层，最相关 |
| 2 | /project/src | 0.8 | 父层，次相关 |
| 1 | /project | 0.64 | 祖父层，较相关 |

**衰减公式**：`weight = 0.8^distance`

## 4. 精度提升分析

### 4.1 解决的问题

**问题 1：全局搜索噪声**
- **原方案**：在所有记忆中搜索，可能返回不相关的结果
- **OpenViking 方案**：优先搜索当前上下文，减少噪声

**问题 2：上下文丢失**
- **原方案**：不考虑记忆的空间位置
- **OpenViking 方案**：利用文件系统结构保留上下文

### 4.2 预期效果

| 指标 | 原方案 | 整合 OpenViking 后 |
|------|--------|-------------------|
| 召回率 | 85% | 90%+ |
| 精确率 | 75% | 85%+ |
| 上下文相关性 | 中 | 高 |
| 检索延迟 | 50ms | 80ms（可接受） |

## 5. 实现路径

### 5.1 阶段 1：数据模型扩展
- 增加 `hierarchy_path` 和 `hierarchy_level` 字段
- 创建层次索引
- 迁移现有数据

### 5.2 阶段 2：存储层改造
- 实现层次路径解析
- 自动提取文件路径的层次结构
- 支持非文件来源的层次标记

### 5.3 阶段 3：检索引擎升级
- 实现分层混合检索算法
- 层级权重计算
- 跨层结果聚合

### 5.4 阶段 4：性能优化
- 层级缓存
- 并行检索各层
- 索引优化

## 6. 与现有系统的兼容性

### 6.1 向后兼容
- 不影响现有的混合检索功能
- `hierarchy_path` 为空时回退到全局搜索
- 渐进式迁移

### 6.2 配置选项
```go
type SearchConfig struct {
    EnableHierarchical bool    // 是否启用分层检索
    MaxLevels          int     // 最大搜索层级
    LevelWeightDecay   float64 // 层级权重衰减系数
}
```

## 7. 总结

**OpenViking 的核心价值**：
- 文件系统范式提供自然的层次结构
- 分层检索提升上下文相关性
- 减少全局搜索的噪声

**整合后的优势**：
- Memory LanceDB Pro 的混合检索（Vector + BM25）
- OpenViking 的分层搜索方向
- 两者结合，精度和召回率双提升

---

**参考资料**：
- OpenViking 仓库：https://github.com/volcengine/OpenViking
- Memory LanceDB Pro：`../memory-lancedb-pro-main`
- HybridMem-RAG 架构：`docs/ARCHITECTURE.md`
