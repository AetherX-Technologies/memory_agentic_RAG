# HybridMem-RAG

混合检索增强生成（RAG）系统，纯 Go 实现，支持跨平台部署。

## 特性

- **混合检索**: 向量检索 + BM25 全文检索 + RRF 融合
- **智能重排**: 可选的 Jina Reranker 交叉编码器精排
- **多维评分**: 新近度、重要性、长度归一化
- **纯 Go 实现**: 无 CGO 依赖，真正跨平台
- **高性能**: 10000 条记忆检索 < 100ms

## 快速开始

### 安装

```bash
go install github.com/yourusername/memory_agentic_RAG/cmd/server@latest
```

### 启动服务

```bash
# 使用内存数据库
hybridmem-server

# 使用持久化数据库
hybridmem-server -db ./data.db
```

### 使用 API

```bash
# 创建记忆
curl -X POST http://localhost:8080/api/memories \
  -H "Content-Type: application/json" \
  -d '{
    "text": "Go 是一门编译型语言",
    "vector": [0.1, 0.2, ...],
    "category": "fact",
    "importance": 0.8
  }'

# 检索记忆
curl "http://localhost:8080/api/memories/search?q=Go语言&limit=5"
```

## 文档

- [API 文档](docs/API.md)
- [部署指南](docs/DEPLOYMENT.md)
- [架构说明](docs/ARCHITECTURE.md)
- [产品需求](docs/PRD.md)

## 性能

| 数据量 | 检索时间 |
|--------|----------|
| 1,000 | 8ms |
| 5,000 | 40ms |
| 10,000 | 83ms |

## 技术栈

- **数据库**: SQLite + FTS5
- **向量计算**: 纯 Go（余弦相似度）
- **HTTP**: Go 标准库
- **重排**: Jina Reranker API（可选）

## 开发

```bash
# 克隆仓库
git clone <repository-url>
cd memory_agentic_RAG

# 运行测试
go test ./...

# 编译
go build -o server cmd/server/main.go
```

## 许可证

MIT License
