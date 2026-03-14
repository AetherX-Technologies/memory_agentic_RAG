# HybridMem-RAG 产品需求文档（PRD）

> 版本：1.0
> 创建日期：2024-03-13
> 项目类型：重构项目（TypeScript → Go）

---

## 一、项目背景

### 1.1 现状

**Memory LanceDB Pro**（TypeScript 实现）：
- 基于 `@lancedb/lancedb` 向量数据库
- 实现了混合检索（Vector + BM25）
- 12阶段评分管道
- 作用域隔离、噪声过滤等增强功能
- 支持桌面平台（Windows/macOS/Linux）

**问题**：
- LanceDB 依赖 CGO + Rust，iOS 编译极其困难
- 无法实现真正的跨平台（特别是 iOS 离线使用）

### 1.2 目标

**重构为纯 Go 实现**：
- 使用 `modernc.org/sqlite` 替代 LanceDB
- 自实现向量检索算法
- 保持所有核心功能
- 支持真正的跨平台（包括 iOS）

---

## 二、核心需求

### 2.1 功能需求（必须 100% 对标）

#### F1: 智能存储
- [ ] 存储记忆（文本 + 向量 + 元数据）
- [ ] 自动向量化（调用 OpenAI Embedding API）
- [ ] 去重检测（相似度 > 0.98 拒绝存储）
- [ ] 元数据支持：category, scope, importance, timestamp

#### F2: 混合检索
- [ ] 向量检索（余弦相似度）
- [ ] BM25 全文检索（SQLite FTS5）
- [ ] RRF 融合算法（k=60）
- [ ] 候选池大小：20

#### F3: 12阶段评分管道
- [ ] Stage 1: 自适应跳过（问候语、命令）
- [ ] Stage 2: 查询向量化
- [ ] Stage 3: 并行检索（Vector + BM25）
- [ ] Stage 4: RRF 融合
- [ ] Stage 5: 交叉编码器重排（可选）
- [ ] Stage 6: 新近度提升（半衰期 14 天）
- [ ] Stage 7: 重要性加权（0.7 + 0.3 × importance）
- [ ] Stage 8: 长度归一化（锚点 500 字符）
- [ ] Stage 9: 访问强化衰减
- [ ] Stage 10: 关联图谱加权（未来扩展）
- [ ] Stage 11: 硬性过滤（最低分 0.35）
- [ ] Stage 12: 噪声过滤 + MMR 多样性

#### F4: 作用域隔离
- [ ] 支持作用域类型：global, agent:<id>, custom:<name>
- [ ] 访问控制：agent 只能访问授权作用域
- [ ] 默认作用域：agent:<id>

#### F5: 噪声过滤
- [ ] 拒绝回复模式（"I don't have information"）
- [ ] 元问题模式（"do you remember"）
- [ ] 会话样板（"hi", "hello"）

#### F6: HTTP API
- [ ] POST /api/memories - 存储记忆
- [ ] GET /api/memories/search - 检索记忆
- [ ] DELETE /api/memories/:id - 删除记忆
- [ ] PUT /api/memories/:id - 更新记忆
- [ ] GET /api/memories/stats - 统计信息

#### F7: MCP Server（可选）
- [ ] memory_recall - 检索工具
- [ ] memory_store - 存储工具
- [ ] memory_forget - 删除工具
- [ ] memory_update - 更新工具

---

### 2.2 性能需求

| 指标 | 目标值 | 测试条件 |
|------|--------|----------|
| 向量检索延迟 | < 50ms | 10000 条记忆 |
| 混合检索延迟 | < 200ms | 10000 条记忆，含12阶段 |
| 存储延迟 | < 100ms | 含向量化 API 调用 |
| 内存占用 | < 500MB | 10000 条记忆 |
| 二进制大小 | < 20MB | 纯 Go 编译 |

**性能对比基准**：
- LanceDB 方案：10000 条检索 ~20ms
- 纯 Go 方案：10000 条检索 ~50ms（可接受）

---

### 2.3 跨平台需求

| 平台 | 支持方式 | 优先级 |
|------|----------|--------|
| Windows | 单一二进制 | P0 |
| macOS | 单一二进制 | P0 |
| Linux | 单一二进制 | P0 |
| Android | gomobile 框架 | P1 |
| iOS | gomobile 框架 | P1 |

**编译要求**：
- 纯 Go（不能用 CGO）
- 使用 `modernc.org/sqlite`（纯 Go SQLite）
- 使用 `gomobile bind` 打包移动端框架

---

## 三、技术约束

### 3.1 必须使用的技术

| 组件 | 技术选型 | 原因 |
|------|----------|------|
| 数据库 | modernc.org/sqlite | 纯 Go，跨平台 |
| 向量检索 | 自实现（余弦相似度） | 避免 CGO |
| 全文检索 | SQLite FTS5 | 内置，成熟 |
| HTTP 框架 | 标准库 net/http | 简单够用 |
| LLM API | go-openai | 官方推荐 |
| CLI 工具 | cobra | 标准选择 |

### 3.2 禁止使用的技术

- ❌ CGO（会导致 iOS 编译失败）
- ❌ LanceDB Go SDK（依赖 CGO）
- ❌ 任何需要 C 依赖的库

---

## 四、验收标准

### 4.1 功能验收

**测试场景 1：基础存储和检索**
```
输入：存储 100 条记忆
操作：检索 "用户喜欢什么"
预期：返回相关记忆，相关性分数 > 0.5
```

**测试场景 2：混合检索**
```
输入：存储包含关键词 "Python" 的记忆
操作：检索 "编程语言"（语义）和 "Python"（关键词）
预期：两种检索都能召回，RRF 融合后排序合理
```

**测试场景 3：作用域隔离**
```
输入：agent1 存储记忆到 scope "agent:agent1"
操作：agent2 尝试访问 agent1 的记忆
预期：访问被拒绝
```

**测试场景 4：噪声过滤**
```
输入：存储 "hi" 或 "I don't have information"
预期：被噪声过滤器拒绝
```

### 4.2 性能验收

**基准测试**：
```bash
# 向量检索性能
go test -bench=BenchmarkVectorSearch -benchtime=10s
# 预期：< 50ms per operation (10000 条记忆)

# 混合检索性能
go test -bench=BenchmarkHybridSearch -benchtime=10s
# 预期：< 200ms per operation

# 内存占用
go test -bench=BenchmarkMemoryUsage -benchmem
# 预期：< 500MB (10000 条记忆)
```

### 4.3 跨平台验收

**编译测试**：
```bash
# 桌面平台
GOOS=windows GOARCH=amd64 go build
GOOS=darwin GOARCH=arm64 go build
GOOS=linux GOARCH=amd64 go build

# 移动平台
gomobile bind -target=ios
gomobile bind -target=android
```

**运行测试**：
- Windows: 能启动 HTTP 服务，能存储和检索
- macOS: 同上
- Linux: 同上
- iOS: Swift 应用能调用 Go 框架
- Android: Kotlin 应用能调用 Go 框架

---

## 五、里程碑

### M1: 核心存储层（第 1 周）
- ✅ SQLite 数据库初始化
- ✅ 数据模型定义
- ✅ 基础 CRUD 操作
- ✅ 向量序列化/反序列化

### M2: 向量检索引擎（第 2 周）
- ✅ 余弦相似度实现
- ✅ 暴力向量搜索
- ✅ 性能测试（10000 条 < 50ms）

### M3: 混合检索（第 3 周）
- ✅ SQLite FTS5 集成
- ✅ BM25 全文检索
- ✅ RRF 融合算法

### M4: 评分管道（第 4 周）
- ✅ 12 阶段评分实现
- ✅ 新近度、重要性、长度归一化
- ✅ 噪声过滤

### M5: HTTP API（第 5 周）
- ✅ RESTful API 实现
- ✅ 作用域管理
- ✅ 错误处理

### M6: 移动端适配（第 6 周）
- ✅ gomobile 编译
- ✅ iOS 框架测试
- ✅ Android 框架测试

### M7: 优化与发布（第 7 周）
- ✅ 性能优化
- ✅ 文档完善
- ✅ 发布 v1.0

---

## 六、非功能需求

### 6.1 可维护性
- 代码注释覆盖率 > 80%
- 单元测试覆盖率 > 80%
- 关键算法有详细文档

### 6.2 可扩展性
- 支持插件式的评分因子
- 支持自定义作用域类型
- 支持多种 embedding 模型

### 6.3 可观测性
- 结构化日志（使用 zap）
- 性能指标（检索延迟、内存占用）
- 错误追踪

---

## 七、风险与缓解

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|----------|
| 纯 Go 向量检索性能不达标 | 中 | 高 | 提前验证，准备 IVF 优化 |
| gomobile 编译失败 | 低 | 中 | 使用官方示例验证 |
| SQLite FTS5 中文分词问题 | 中 | 中 | 使用 unicode61 tokenizer |
| 开发时间超预期 | 中 | 中 | MVP 优先，分阶段交付 |

---

## 八、参考资料

- 改造方案详解：`docs/references/transformation-guide.md`
- iOS 兼容分析：`docs/references/ios-compatibility.md`
- 开发参考资源：`docs/references/reference-resources.md`
- 原始实现：`../memory-lancedb-pro-main`

---

**文档状态**：已确认
**负责人**：AI + 用户
**预计完成时间**：7-8 周
