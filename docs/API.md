# HybridMem-RAG API 文档

## 基础信息

- **Base URL**: `http://localhost:8080`
- **Content-Type**: `application/json`

## 端点列表

### 1. 创建记忆

```http
POST /api/memories
```

**请求体**:
```json
{
  "text": "记忆内容",
  "vector": [0.1, 0.2, ...],
  "category": "fact",
  "scope": "global",
  "importance": 0.8,
  "metadata": "可选元数据"
}
```

**响应**:
```json
{
  "id": "uuid"
}
```

### 2. 检索记忆

```http
GET /api/memories/search?q=查询文本&limit=10
```

**响应**:
```json
[
  {
    "entry": {
      "id": "uuid",
      "text": "记忆内容",
      "category": "fact",
      "scope": "global",
      "importance": 0.8,
      "timestamp": 1234567890
    },
    "score": 0.95
  }
]
```

### 3. 删除记忆

```http
DELETE /api/memories/:id
```

**响应**: 204 No Content

### 4. 更新记忆

```http
PUT /api/memories/:id
```

**请求体**: 同创建记忆

### 5. 统计信息

```http
GET /api/memories/stats
```

**响应**:
```json
{
  "total": 100,
  "by_category": {"fact": 50, "event": 30}
}
```

## 配置

### Rerank 配置

在 `Config` 中启用 rerank：

```go
RerankConfig: store.RerankConfig{
    Enabled: true,
    Provider: "jina",
    APIKey: "your-api-key",
    Model: "jina-reranker-v2-base-multilingual",
}
```

## 错误响应

```json
{
  "error": "错误信息"
}
```

状态码：400（请求错误）、404（未找到）、500（服务器错误）
