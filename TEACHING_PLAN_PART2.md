
#### 方案 A：单层架构（不推荐）

```go
// handler.go - 所有逻辑混在一起
func IngestHandler(c *gin.Context) {
    // 1. 解析请求
    var input struct {
        Text string `json:"text"`
    }
    c.BindJSON(&input)

    // 2. 调用 LLM（直接在 handler 里）
    httpClient := &http.Client{}
    req, _ := http.NewRequest("POST", "http://localhost:3456/v1/chat/completions", ...)
    resp, _ := httpClient.Do(req)
    // ... 解析 LLM 响应

    // 3. 数据库操作（直接在 handler 里）
    db, _ := sql.Open("sqlite3", "memory.db")
    db.Exec("INSERT INTO memories ...")

    // 4. 返回响应
    c.JSON(200, result)
}
```

**问题**：
1. **难以测试**：要测试业务逻辑，必须启动 HTTP 服务器
2. **难以复用**：如果文件监听也要摄入记忆，代码要复制一遍
3. **难以维护**：一个函数做太多事，改一处可能影响其他
4. **难以扩展**：想换数据库？要改遍所有 handler

#### 方案 B：分层架构（推荐）

```go
// handler/memory_handler.go - 只负责 HTTP
func (h *MemoryHandler) Ingest(c *gin.Context) {
    var input domain.IngestInput
    if err := c.ShouldBindJSON(&input); err != nil {
        c.JSON(400, gin.H{"error": "invalid input"})
        return
    }

    // 调用 Service 层
    memory, err := h.agentService.Ingest(c.Request.Context(), &input)
    if err != nil {
        c.JSON(500, gin.H{"error": err.Error()})
        return
    }

    c.JSON(200, gin.H{"status": "ingested", "memory": memory})
}

// service/agent_service.go - 业务逻辑
func (s *AgentService) Ingest(ctx context.Context, input *domain.IngestInput) (*domain.Memory, error) {
    // 调用 LLM 层
    structured, err := s.llm.StructureMemory(ctx, input.Text, input.Source)
    if err != nil {
        return nil, fmt.Errorf("structure memory: %w", err)
    }

    // 构建领域对象
    memory := &domain.Memory{
        Summary:    structured.Summary,
        Entities:   structured.Entities,
        // ...
    }

    // 调用 Repository 层
    return s.store.CreateMemory(ctx, memory)
}

// repository/sqlite_store.go - 数据访问
func (s *SQLiteStore) CreateMemory(ctx context.Context, m *domain.Memory) (*domain.Memory, error) {
    result, err := s.db.ExecContext(ctx,
        "INSERT INTO memories (summary, entities, ...) VALUES (?, ?, ...)",
        m.Summary, toJSON(m.Entities), ...)
    // ...
}

// llm/openai.go - LLM 调用
func (c *OpenAIClient) StructureMemory(ctx context.Context, text string) (*StructuredMemory, error) {
    // HTTP 调用 LLM API
    // ...
}
```

**优势**：
1. **易于测试**：可以单独测试 Service 层，mock LLM 和 Repository
2. **易于复用**：文件监听直接调用 `AgentService.Ingest()`
3. **易于维护**：每层职责清晰，改动影响范围小
4. **易于扩展**：换数据库只需改 Repository 层

**学生**：我明白了！这就像搭积木，每个积木有明确的功能，可以灵活组合。

**教授**：完全正确！这就是**模块化设计**的核心思想。

### 2.3 依赖注入（Dependency Injection）

**教授**：现在我要介绍一个重要概念：**依赖注入**。这是实现分层架构的关键技术。

**学生**：教授，什么是"依赖"？

**教授**：好问题！让我用代码说明：

```go
// 不好的设计：硬编码依赖
type AgentService struct {}

func (s *AgentService) Ingest(text string) {
    // 直接创建依赖对象
    llm := llm.NewOpenAIClient("http://localhost:3456", "key", "model")
    store := repository.NewSQLiteStore("memory.db")

    // 使用依赖
    structured := llm.StructureMemory(text)
    store.CreateMemory(structured)
}
```

**问题**：
1. **难以测试**：无法替换 LLM 和 Store 为 mock 对象
2. **难以配置**：URL、API key 硬编码
3. **紧耦合**：AgentService 直接依赖具体实现

```go
// 好的设计：依赖注入
type AgentService struct {
    llm   llm.Client        // 接口类型
    store repository.Store  // 接口类型
}

// 通过构造函数注入依赖
func NewAgentService(llmClient llm.Client, store repository.Store) *AgentService {
    return &AgentService{
        llm:   llmClient,
        store: store,
    }
}

func (s *AgentService) Ingest(text string) {
    structured := s.llm.StructureMemory(text)
    s.store.CreateMemory(structured)
}
```

**优势**：
1. **易于测试**：可以注入 mock 对象
2. **易于配置**：依赖在外部创建和配置
3. **松耦合**：依赖接口而非具体实现

**学生**：这样确实灵活多了！但是，谁来创建和注入这些依赖呢？

**教授**：非常好的问题！这就是 `main.go` 的职责。让我展示：

```go
// cmd/server/main.go
func main() {
    // 1. 加载配置
    cfg, _ := config.Load()

    // 2. 创建基础设施
    store, _ := repository.NewSQLiteStore(cfg.Database.DSN)
    llmClient := llm.NewOpenAIClient(cfg.LLM.BaseURL, cfg.LLM.APIKey, cfg.LLM.Model)

    // 3. 创建 Service（注入依赖）
    agentService := service.NewAgentService(store, llmClient, logger)

    // 4. 创建 Handler（注入 Service）
    memoryHandler := handler.NewMemoryHandler(agentService)

    // 5. 启动服务器
    router := server.SetupRouter(memoryHandler, ...)
    router.Run(":8080")
}
```

这就是**依赖注入容器**的概念。`main.go` 负责：
- 创建所有对象
- 配置依赖关系
- 启动应用

### 2.4 接口设计（Interface Design）

**教授**：Go 语言的接口设计非常优雅。让我展示核心接口：

```go
// internal/llm/client.go
type Client interface {
    StructureMemory(ctx context.Context, text string, source string) (*StructuredMemory, error)
    ConsolidateMemories(ctx context.Context, memoriesText string) (*ConsolidationResult, error)
    AnswerWithMemory(ctx context.Context, question string, memoriesText string, consolidationsText string) (string, error)
}
```

**学生**：教授，为什么要定义接口？直接用具体类型不行吗？

**教授**：让我用一个场景说明。假设你现在用的是 OpenAI 的 API，但将来想换成 Anthropic 的 Claude API。

**没有接口**：
```go
// 所有代码都依赖 OpenAIClient
type AgentService struct {
    llm *OpenAIClient  // 具体类型
}

// 要换 API，需要改所有地方
type AgentService struct {
    llm *ClaudeClient  // 改这里
}
```

**有接口**：
```go
// 代码依赖接口
type AgentService struct {
    llm Client  // 接口类型
}

// 只需在 main.go 改一行
// llmClient := llm.NewOpenAIClient(...)  // 旧的
llmClient := llm.NewClaudeClient(...)     // 新的
```

**学生**：哦！这就是"面向接口编程"！

**教授**：完全正确！这是**开闭原则**（Open-Closed Principle）的体现：
- **开放扩展**：可以添加新的实现（ClaudeClient）
- **关闭修改**：不需要修改使用方（AgentService）

### 2.5 数据流分析

**教授**：现在让我们追踪一个完整的请求流程，理解数据如何在各层之间流动。

#### 场景：用户摄入一条记忆

```
用户请求：
POST /api/v1/ingest
{"text": "AI agents are growing rapidly", "source": "article"}

↓ 数据流

┌─────────────────────────────────────────────────────────────┐
│ 1. Handler 层                                                │
│    - 接收 HTTP 请求                                          │
│    - 解析 JSON → domain.IngestInput                         │
│    - 验证参数                                                │
└────────────────────────┬────────────────────────────────────┘
                         │ domain.IngestInput
                         ↓
┌─────────────────────────────────────────────────────────────┐
│ 2. Service 层                                                │
│    - 调用 LLM 结构化                                         │
│    - 构建 domain.Memory 对象                                │
│    - 调用 Repository 存储                                    │
└────────────────────────┬────────────────────────────────────┘
                         │ (text, source)
                         ↓
┌─────────────────────────────────────────────────────────────┐
│ 3. LLM 层                                                    │
│    - 构造 prompt                                             │
│    - HTTP 调用 LLM API                                       │
│    - 解析 JSON 响应                                          │
│    - 返回 StructuredMemory                                   │
└────────────────────────┬────────────────────────────────────┘
                         │ StructuredMemory
                         ↓
┌─────────────────────────────────────────────────────────────┐
│ 2. Service 层（继续）                                        │
│    - 接收 StructuredMemory                                   │
│    - 构建完整的 domain.Memory                               │
└────────────────────────┬────────────────────────────────────┘
                         │ domain.Memory
                         ↓
┌─────────────────────────────────────────────────────────────┐
│ 4. Repository 层                                             │
│    - 生成 SQL 语句                                           │
│    - 执行数据库插入                                          │
│    - 返回带 ID 的 Memory                                     │
└────────────────────────┬────────────────────────────────────┘
                         │ domain.Memory (with ID)
                         ↓
┌─────────────────────────────────────────────────────────────┐
│ 1. Handler 层（返回）                                        │
│    - 构造 HTTP 响应                                          │
│    - 返回 JSON                                               │
└─────────────────────────────────────────────────────────────┘

用户响应：
{"status": "ingested", "memory": {...}}
```

**学生**：教授，我注意到数据在不同层之间转换了类型。为什么不用同一个类型？

**教授**：非常敏锐的观察！这涉及到**领域模型**（Domain Model）的概念。让我解释：

```go
// HTTP 层的数据结构（面向外部）
type IngestRequest struct {
    Text   string `json:"text"`
    Source string `json:"source"`
}

// 领域层的数据结构（面向业务）
type Memory struct {
    ID         int64
    Summary    string
    Entities   []string
    Topics     []string
    Importance float64
    // ... 更多业务字段
}

// 数据库层的数据结构（面向存储）
// 直接映射到 SQL 表结构
```

**为什么要分离**：
1. **关注点不同**：HTTP 关注传输，领域关注业务，数据库关注存储
2. **变化独立**：改 API 格式不影响业务逻辑
3. **验证分层**：HTTP 层验证格式，业务层验证逻辑

### 2.6 Python 版本的架构对比

**教授**：现在让我们对比 Python 版本的架构。Python 版本采用了**单文件架构**：

```python
# agent.py (~570 行)

# 1. 配置（全局变量）
MODEL = "claude-sonnet-4"
API_BASE_URL = "http://localhost:3456/v1"

# 2. 数据库操作（函数）
def store_memory(raw_text, summary, entities, ...):
    db = get_db()
    db.execute("INSERT INTO memories ...")

# 3. LLM 调用（函数）
async def _llm(system, user):
    r = await client.chat.completions.create(...)

# 4. Agent 逻辑（类）
class MemoryAgent:
    async def ingest(self, text, source):
        response = await _llm(system, text)
        parsed = _extract_json(response)
        store_memory(...)

# 5. HTTP API（函数）
def build_http(agent):
    app = web.Application()
    async def handle_ingest(req):
        data = await req.json()
        result = await agent.ingest(data["text"])
        return web.json_response(result)
    app.router.add_post("/ingest", handle_ingest)

# 6. 后台任务（函数）
async def watch_folder(agent, folder):
    while True:
        # 扫描文件
        await asyncio.sleep(5)

# 7. 主函数
async def main_async(args):
    agent = MemoryAgent()
    tasks = [
        watch_folder(agent, inbox),
        consolidation_loop(agent, 30),
    ]
    await asyncio.gather(*tasks)
```

**对比分析**：

| 维度 | Python 单文件 | Go 分层架构 |
|------|--------------|------------|
| **文件数量** | 1 个主文件 | ~15 个文件 |
| **代码行数** | ~570 行 | ~2000 行 |
| **职责分离** | 弱（函数级） | 强（模块级） |
| **可测试性** | 中等 | 高 |
| **可维护性** | 适合小项目 | 适合大项目 |
| **学习曲线** | 平缓 | 陡峭 |

**学生**：Python 版本看起来更简洁，为什么 Go 版本要这么复杂？

**教授**：非常好的问题！这涉及到**工程权衡**（Engineering Trade-offs）：

**Python 单文件架构的优势**：
1. **快速原型**：适合快速验证想法
2. **易于理解**：所有代码在一个文件，容易浏览
3. **低学习成本**：不需要理解复杂的架构

**Go 分层架构的优势**：
1. **团队协作**：多人可以并行开发不同模块
2. **长期维护**：清晰的边界，改动影响范围小
3. **质量保证**：每层可以独立测试
4. **性能优化**：可以针对性优化特定层

**选择建议**：
- **个人项目、快速原型** → Python 单文件
- **团队项目、生产系统** → Go 分层架构

---

### 2.7 第二讲小结

**教授**：让我总结第二讲的核心要点：

**核心概念**：
1. **分层架构**：Handler → Service → Repository → LLM
2. **关注点分离**：每层负责特定职责
3. **依赖注入**：通过构造函数注入依赖
4. **接口设计**：面向接口编程，易于扩展

**关键洞见**：
- 架构设计是**权衡**的艺术
- 简单不等于简陋，复杂不等于过度设计
- 选择架构要考虑**项目规模**和**团队情况**

**思考题**：
1. 如果要添加"记忆导出"功能，应该在哪一层实现？
2. 如何设计接口使得可以同时支持 SQLite 和 PostgreSQL？
3. Python 的 asyncio 和 Go 的 goroutine 在架构设计上有什么影响？

---

**教授**：好，我们再休息 10 分钟。下一讲我们将深入核心算法，看看记忆摄入、整合、查询的具体实现。

