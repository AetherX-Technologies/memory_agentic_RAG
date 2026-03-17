# HybridMem-RAG

> **生产级混合检索系统，支持可配置的搜索模式**
> 纯 Go 实现 • 跨平台 • 高性能

[English](./README.md) | [架构文档](./docs/architecture/INDEX.md) | [API 参考](./docs/API.md)

---

## 🚀 什么是 HybridMem-RAG？

HybridMem-RAG 是一个先进的检索增强生成（RAG）系统，结合多种搜索策略以提供高精度结果。完全使用 Go 语言构建，零 CGO 依赖，可在 Windows、macOS、Linux、iOS 和 Android 上无缝运行。

### 核心创新

**🎯 三种可配置检索模式**
- **模式 1**：纯 BM25 关键词搜索（最快，<500µs）
- **模式 2**：混合搜索（BM25 + 向量 + RRF 融合）
- **模式 3**：完整管道（混合 + 重排，92% 准确率）

**🧠 分层检索架构**
- 受 OpenViking 文件系统感知检索启发
- 逐层搜索，加权聚合
- 基于文档层次结构的上下文感知结果

**⚡ 高性能设计**
- SQLite FTS5 + 自定义中文分词器
- 并行向量搜索 + SIMD 优化
- RRF（倒数排名融合）分数混合
- 12 阶段评分管道 + 重排

**🔧 生产就绪特性**
- 支持 OpenAI 兼容的嵌入 API
- 支持 Jina 兼容的重排 API
- 通过 YAML 配置（无需修改代码）
- 真实场景综合测试套件

---

## 📊 性能基准

| 数据集大小 | 模式 1 (BM25) | 模式 2 (混合) | 模式 3 (完整) |
|-----------|---------------|--------------|--------------|
| 1,000 文档 | <1ms         | ~50ms        | ~200ms       |
| 10,000 文档| <1ms         | ~100ms       | ~300ms       |
| **准确率** | 60-70%       | 75-85%       | **90-95%**   |

*在包含 10 个复杂查询的真实中文文档语料库上测试*

---

## 🎯 快速开始

### 安装

```bash
git clone https://github.com/AetherX-Technologies/memory_agentic_RAG.git
cd memory_agentic_RAG
go mod download
```

### 配置

复制示例配置并添加你的 API 密钥：

```bash
cd cmd/real_world_test
cp config.yaml.example config.yaml
# 编辑 config.yaml 填入你的 API 凭证
```

**config.yaml 结构：**

```yaml
retrieval_mode: 3  # 1=BM25, 2=混合, 3=完整

embedding:
  enabled: true
  provider: "openai"
  api_key: "你的API密钥"
  model: "text-embedding-3-small"
  endpoint: "https://api.openai.com/v1/embeddings"
  dimension: 1536

rerank:
  enabled: true
  provider: "jina"
  api_key: "你的API密钥"
  model: "jina-reranker-v2-base-multilingual"
  endpoint: "https://api.jina.ai/v1/rerank"
```

### 运行测试

```bash
# 使用你的文档语料库测试
go run cmd/real_world_test/main.go

# 运行单元测试
go test ./internal/store/...
```

---

## 🏗️ 架构概览

```
┌─────────────────────────────────────────────────────────┐
│                    查询输入                              │
└────────────────────┬────────────────────────────────────┘
                     ↓
┌─────────────────────────────────────────────────────────┐
│              分层混合检索层                              │
│  • 层级 1: /project        → 向量 + BM25 → RRF          │
│  • 层级 2: /project/src    → 向量 + BM25 → RRF          │
│  • 层级 3: /project/src/auth → 向量 + BM25 → RRF        │
│                    ↓ 加权聚合                            │
└────────────────────┬────────────────────────────────────┘
                     ↓
┌─────────────────────────────────────────────────────────┐
│              12 阶段评分管道                             │
│  1. 自适应过滤      2. 向量化                           │
│  3. 并行检索        4. RRF 融合                         │
│  5. 交叉编码器重排  6. 新近度提升                       │
│  7. 重要性加权      8. 长度归一化                       │
│  9. 访问强化        10. 连接图谱加权                    │
│  11. 硬性过滤       12. MMR 多样性                      │
└────────────────────┬────────────────────────────────────┘
                     ↓
┌─────────────────────────────────────────────────────────┐
│                  Top-K 结果                             │
└─────────────────────────────────────────────────────────┘
```

### 核心组件

- **`internal/store/`**：存储层（SQLite + FTS5）
- **`internal/store/embedding.go`**：向量嵌入集成
- **`internal/store/rerank.go`**：重排 API 集成
- **`internal/store/hierarchical.go`**：逐层搜索
- **`internal/store/hybrid.go`**：RRF 融合逻辑
- **`internal/store/bm25.go`**：中文分词全文搜索

---

## 🌟 核心特性

### 1. 可配置检索模式

无需修改代码即可切换模式：

```yaml
retrieval_mode: 1  # 快速关键词搜索
retrieval_mode: 2  # 平衡混合搜索
retrieval_mode: 3  # 最高准确率（含重排）
```

### 2. 中文语言支持

- SQLite 自定义中文分词器
- 正确的词语切分（非字符级）
- 针对 CJK 语言优化

### 3. 分层上下文感知

文档按文件系统路径组织：
```
/project/backend/auth/login.go
/project/backend/auth/session.go
/project/frontend/components/LoginForm.tsx
```

搜索优先返回当前上下文层级的结果。

### 4. API 兼容性

**嵌入 API：**
- OpenAI (text-embedding-3-small/large)
- Azure OpenAI
- 任何 OpenAI 兼容端点

**重排 API：**
- Jina AI Reranker
- Cohere Rerank（即将支持）
- Voyage AI Rerank（即将支持）

### 5. 零 CGO 依赖

与 LanceDB 或 Faiss 不同，HybridMem-RAG 使用纯 Go：
- ✅ 交叉编译到任何平台
- ✅ 静态二进制部署
- ✅ 无 C++ 运行时依赖
- ✅ 支持 iOS/Android

---

## 📚 文档

- **[English Documentation](./README.md)** - 完整英文文档
- **[架构指南](./docs/architecture/INDEX.md)** - 系统设计细节
- **[产品需求文档](./docs/PRD.md)** - 产品需求
- **[技术评审](./docs/TECHNICAL_REVIEW.md)** - 代码质量分析
- **[OpenViking 集成](./docs/references/openviking-integration.md)** - 分层检索设计
- **[构建指南](./BUILD.md)** - 编译说明

---

## 🧪 测试

### 真实场景测试套件

项目包含使用真实文档语料库的综合测试套件：

```bash
cd cmd/real_world_test
go run main.go
```

**测试场景：**
- 精确关键词匹配
- 语义相似度搜索
- 多词复杂查询
- 跨领域知识检索
- 技术术语识别

**示例输出：**
```
╔════════════════════════════════════════════════════════════════╗
║              HybridMem-RAG 真实场景测试报告                     ║
╚════════════════════════════════════════════════════════════════╝

检索模式: 3 - 完整管道（混合 + 重排）

✓ 数据库初始化: 245ms
✓ Embedder 已启用 (openai)
✓ 录入 1,247 个真实文档

━━━ 测试 1: 精确关键词-人工智能 ━━━
    查询: "人工智能"
    ✓ 准确率: 100.0% | 相关结果: 5/5 | 总结果: 10
    前3结果: AI概述.md, 机器学习基础.md, 深度学习教程.md

[... 9 个更多测试 ...]

【综合报告】
────────────────────────────────────────────────────────────────
文档数量:     1,247
测试用例:     10
通过率:       100.0%
平均准确率:   92.0%
评级:         🚀 优秀 - 可用于生产环境
```

---

## 🛠️ 技术栈

| 组件 | 技术 | 用途 |
|------|------|------|
| **数据库** | SQLite + FTS5 | 自定义分词器全文搜索 |
| **向量存储** | SQLite BLOB | 序列化 float32 数组 |
| **向量搜索** | 纯 Go + SIMD | 余弦相似度计算 |
| **嵌入** | OpenAI API | 文本 → 向量转换 |
| **重排** | Jina API | 交叉编码器评分 |
| **HTTP 服务器** | Go 标准库 | RESTful API |
| **配置** | YAML | 运行时配置 |

---

## 🤝 贡献

欢迎贡献！请先阅读我们的[贡献指南](./CONTRIBUTING.md)。

### 开发环境设置

```bash
# 克隆仓库
git clone https://github.com/AetherX-Technologies/memory_agentic_RAG.git
cd memory_agentic_RAG

# 安装依赖
go mod download

# 运行测试
go test -v ./...

# 运行基准测试
go test -bench=. ./internal/store/
```

---

## 📄 许可证

MIT License - 详见 [LICENSE](./LICENSE)

---

## 🙏 致谢

本项目整合了以下项目的思想：
- **Memory LanceDB Pro** - 12 阶段评分管道
- **OpenViking** - 分层文件系统感知检索
- **Memory Agent** - 主动整合机制

---

## 📞 联系方式

- **问题反馈**：[GitHub Issues](https://github.com/AetherX-Technologies/memory_agentic_RAG/issues)
- **讨论交流**：[GitHub Discussions](https://github.com/AetherX-Technologies/memory_agentic_RAG/discussions)

---

**由 AetherX Technologies 用 ❤️ 构建**
