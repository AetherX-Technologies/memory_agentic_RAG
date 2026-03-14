# 架构地图

> 本文档是架构设计的入口，采用渐进式加载原则
> 根据当前任务，按需阅读相关子文档

---

## 架构概览

```
HybridMem-RAG (纯 Go 实现)
├── 存储层 (SQLite)
│   ├── memories 表（元数据）
│   ├── vectors 表（向量）
│   └── fts_memories（全文索引）
├── 检索引擎
│   ├── 向量检索（余弦相似度）
│   ├── BM25 检索（FTS5）
│   └── RRF 融合
├── 评分管道（12阶段）
├── 接口层
│   ├── HTTP API
│   └── MCP Server（可选）
└── 移动端框架（gomobile）
```

---

## 文档导航

### 核心模块（按开发顺序）

**1. 数据模型设计** → `data-model.md`
- 何时阅读：实现存储层时
- 内容：数据库表结构、字段定义、索引设计

**2. 检索引擎设计** → `retrieval-engine.md`
- 何时阅读：实现检索功能时
- 内容：向量检索算法、RRF 融合、12阶段评分

**3. API 接口设计** → `api-design.md`
- 何时阅读：实现 HTTP API 时
- 内容：RESTful 接口定义、请求/响应格式

### 参考资料

**改造方案** → `../references/transformation-guide.md`
- TypeScript → Go 的映射关系
- 关键算法位置

**iOS 兼容分析** → `../references/ios-compatibility.md`
- 为什么选择纯 Go
- gomobile 使用指南

**开发参考资源** → `../references/reference-resources.md`
- 参考源码位置
- Go 依赖库清单

---

## 技术栈

| 层次 | 技术选型 | 说明 |
|------|----------|------|
| 数据库 | modernc.org/sqlite | 纯 Go SQLite |
| 向量检索 | 自实现 | 余弦相似度 |
| 全文检索 | SQLite FTS5 | 内置 BM25 |
| HTTP 框架 | net/http | 标准库 |
| LLM API | go-openai | OpenAI 客户端 |
| 移动端 | gomobile | iOS/Android 框架 |

---

## 关键设计决策

### 决策1：为什么不用 LanceDB？
- **原因**：LanceDB 依赖 CGO + Rust，iOS 编译困难
- **替代**：modernc.org/sqlite + 纯 Go 向量检索
- **代价**：性能稍慢（50ms vs 20ms），但可接受

### 决策2：向量检索算法
- **MVP**：暴力搜索（10000 条 < 50ms）
- **优化**：IVF 索引（可选，100000 条 < 100ms）
- **参考**：`../memory-lancedb-pro-main/src/retriever.ts`

### 决策3：移动端部署
- **iOS**：gomobile bind 编译框架
- **Android**：gomobile bind 编译框架
- **限制**：不能使用 CGO

---

## 开发顺序建议

1. **存储层**（第1周）→ 阅读 `data-model.md`
2. **向量检索**（第2周）→ 阅读 `retrieval-engine.md` 第1-2节
3. **混合检索**（第3周）→ 阅读 `retrieval-engine.md` 第3-4节
4. **评分管道**（第4周）→ 阅读 `retrieval-engine.md` 第5节
5. **HTTP API**（第5周）→ 阅读 `api-design.md`
6. **移动端**（第6周）→ 阅读 `../references/ios-compatibility.md`

---

**最后更新**：2024-03-13
