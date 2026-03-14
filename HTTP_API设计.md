## 三、HTTP API 设计

### 3.1 核心接口

```yaml
# OpenAPI 3.0 规范
openapi: 3.0.0
info:
  title: HybridMem-RAG API
  version: 1.0.0

paths:
  # 存储记忆
  /api/memories:
    post:
      summary: 存储新记忆
      requestBody:
        content:
          application/json:
            schema:
              type: object
              properties:
                text:
                  type: string
                  description: 原始文本
                category:
                  type: string
                  enum: [fact, preference, decision, entity, insight]
                scope:
                  type: string
                  default: global
                metadata:
                  type: object
                  description: 扩展元数据
      responses:
        '201':
          description: 创建成功
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Memory'

  # 检索记忆
  /api/search:
    get:
      summary: 检索记忆
      parameters:
        - name: q
          in: query
          required: true
          schema:
            type: string
        - name: limit
          in: query
          schema:
            type: integer
            default: 5
        - name: scope
          in: query
          schema:
            type: string
      responses:
        '200':
          description: 检索成功
          content:
            application/json:
              schema:
                type: array
                items:
                  $ref: '#/components/schemas/SearchResult'

  # 删除记忆
  /api/memories/{id}:
    delete:
      summary: 删除记忆
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        '204':
          description: 删除成功

  # 统计信息
  /api/stats:
    get:
      summary: 获取统计信息
      responses:
        '200':
          description: 统计信息
          content:
            application/json:
              schema:
                type: object
                properties:
                  total_memories:
                    type: integer
                  unconsolidated:
                    type: integer
                  consolidations:
                    type: integer

components:
  schemas:
    Memory:
      type: object
      properties:
        id:
          type: string
        raw_text:
          type: string
        summary:
          type: string
        entities:
          type: array
          items:
            type: string
        topics:
          type: array
          items:
            type: string
        category:
          type: string
        scope:
          type: string
        importance:
          type: number
        created_at:
          type: string
          format: date-time

    SearchResult:
      type: object
      properties:
        memory:
          $ref: '#/components/schemas/Memory'
        score:
          type: number
```

### 3.2 Go 实现示例

```go
package api

import (
    "encoding/json"
    "net/http"
    "github.com/gorilla/mux"
)

type Handler struct {
    ingestSvc   *service.IngestService
    retrievalSvc *service.RetrievalService
}

func (h *Handler) RegisterRoutes(r *mux.Router) {
    r.HandleFunc("/api/memories", h.CreateMemory).Methods("POST")
    r.HandleFunc("/api/search", h.SearchMemories).Methods("GET")
    r.HandleFunc("/api/memories/{id}", h.DeleteMemory).Methods("DELETE")
    r.HandleFunc("/api/stats", h.GetStats).Methods("GET")
}

func (h *Handler) CreateMemory(w http.ResponseWriter, r *http.Request) {
    var req struct {
        Text     string                 `json:"text"`
        Category string                 `json:"category"`
        Scope    string                 `json:"scope"`
        Metadata map[string]interface{} `json:"metadata"`
    }

    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }

    memory, err := h.ingestSvc.Ingest(r.Context(), req.Text, req.Scope)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    w.WriteHeader(http.StatusCreated)
    json.NewEncoder(w).Encode(memory)
}

func (h *Handler) SearchMemories(w http.ResponseWriter, r *http.Request) {
    query := r.URL.Query().Get("q")
    limit := 5 // 默认值

    results, err := h.retrievalSvc.Search(r.Context(), query, []string{"global"}, limit)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    json.NewEncoder(w).Encode(results)
}
```

