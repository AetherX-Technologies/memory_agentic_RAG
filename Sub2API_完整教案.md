# Sub2API 项目完整教案
## 大学教授与好奇学生的深度对话

---

## 第一讲：项目概览与核心问题

**教授**：同学们好！今天我们要深入学习一个非常有意思的实战项目——Sub2API。在开始之前，我想先问大家一个问题：你们有没有订阅过 Claude Pro 或者 ChatGPT Plus？

**学生**：有的！我订阅了 Claude Pro，每个月 20 美元，可以无限使用 Claude。

**教授**：很好！那么现在假设你是一个小团队的技术负责人，团队有 10 个开发者，每个人都需要使用 Claude API 来辅助开发。你会怎么做？

**学生**：嗯...给每个人都订阅一个账号？但这样成本太高了，10 个人就是 200 美元/月。

**教授**：没错！这就是 Sub2API 要解决的核心问题。让我用一个更具体的场景来说明：

假设你订阅了 Claude Code，每月 200 美元，获得了大量的 API 配额。但问题是：
1. **配额无法分享**：你一个人用不完，但团队其他人却没有配额
2. **无法计费**：你不知道每个人用了多少，无法按使用量收费
3. **无法限流**：某个人可能一次性用光所有配额
4. **无法负载均衡**：如果你有多个订阅账号，无法智能分配请求

**学生**：我明白了！所以 Sub2API 就是要把这些订阅账号的配额"池化"，然后分发给多个用户使用？

**教授**：完全正确！Sub2API 的英文全称可以理解为 "Subscription to API"，它是一个 **AI API 网关平台**，核心功能是：

```
订阅账号（上游）→ Sub2API（中间层）→ 多个用户（下游）
```

让我画一个架构图来说明：

```
┌─────────────────────────────────────────────────────────────┐
│                        用户层                                │
│  用户A (API Key: sk-xxx)  用户B (API Key: sk-yyy)          │
└────────────────┬────────────────────┬───────────────────────┘
                 │                    │
                 ▼                    ▼
┌─────────────────────────────────────────────────────────────┐
│                     Sub2API 网关                             │
│  ┌──────────────────────────────────────────────────────┐  │
│  │  认证层：验证 API Key，检查配额和权限                 │  │
│  └──────────────────────────────────────────────────────┘  │
│  ┌──────────────────────────────────────────────────────┐  │
│  │  调度层：选择上游账号（负载均衡+粘性会话）            │  │
│  └──────────────────────────────────────────────────────┘  │
│  ┌──────────────────────────────────────────────────────┐  │
│  │  转发层：转发请求到上游，处理流式响应                 │  │
│  └──────────────────────────────────────────────────────┘  │
│  ┌──────────────────────────────────────────────────────┐  │
│  │  计费层：统计 token 使用量，扣除余额/配额             │  │
│  └──────────────────────────────────────────────────────┘  │
└────────────────┬────────────────────┬───────────────────────┘
                 │                    │
                 ▼                    ▼
┌─────────────────────────────────────────────────────────────┐
│                      上游账号池                              │
│  Claude账号1  Claude账号2  OpenAI账号1  Gemini账号1         │
└─────────────────────────────────────────────────────────────┘
```

**学生**：这个架构看起来很清晰！但我有个疑问：为什么需要"粘性会话"？直接轮询分配不行吗？

**教授**：非常好的问题！这涉及到 AI 对话的特殊性。让我举个例子：

假设你在用 Claude 写代码，第一轮对话用的是账号 A，Claude 返回了一些代码。第二轮你说"继续完善这段代码"，如果这时候切换到账号 B，会发生什么？

**学生**：哦！账号 B 没有之前的对话上下文，它不知道"这段代码"指的是什么！

**教授**：完全正确！这就是为什么需要**粘性会话（Sticky Session）**。同一个对话会话的所有请求，必须路由到同一个上游账号，保持上下文连续性。

Sub2API 通过以下方式实现粘性会话：
1. 从请求中提取 `session_xxx` 标识符（如果客户端提供）
2. 或者对请求内容（system + messages）进行哈希
3. 将 session hash 映射到特定的上游账号
4. 在 Redis 中缓存这个映射关系（TTL 1小时）

**学生**：明白了！那如果这个账号突然不可用了怎么办？

**教授**：这就涉及到**故障转移（Failover）**机制了。我们稍后会详细讲解。现在让我们先总结一下 Sub2API 的核心价值：

### Sub2API 的核心价值

1. **配额共享**：将个人订阅转化为团队可用的 API 服务
2. **精确计费**：Token 级别的使用统计和成本计算
3. **智能调度**：负载均衡 + 粘性会话 + 故障转移
4. **并发控制**：用户级和账号级的并发限制
5. **多账号管理**：统一管理多个上游账号（OAuth、API Key）
6. **协议适配**：支持 Anthropic、OpenAI、Gemini 等多种 API 格式

---

## 第二讲：技术栈与架构设计

**教授**：现在我们来看看 Sub2API 的技术选型。这是一个典型的前后端分离架构：

### 技术栈

**后端（Go）**：
- **语言**：Go 1.25.7（为什么选 Go？高并发、低延迟、部署简单）
- **Web 框架**：Gin（轻量级、高性能的 HTTP 框架）
- **ORM**：Ent（Facebook 开源，类型安全的 ORM）
- **依赖注入**：Wire（Google 开源，编译时 DI，零运行时开销）

**前端（Vue）**：
- **框架**：Vue 3.4+（Composition API）
- **构建工具**：Vite 5+（快速的开发服务器和构建工具）
- **样式**：TailwindCSS（实用优先的 CSS 框架）
- **状态管理**：Pinia（Vue 官方推荐的状态管理库）
- **包管理**：pnpm（快速、节省磁盘空间）

**基础设施**：
- **数据库**：PostgreSQL 15+（关系型数据，ACID 保证）
- **缓存/队列**：Redis 7+（高性能缓存、分布式锁、消息队列）

**学生**：为什么选择 Go 而不是 Node.js 或 Python？

**教授**：非常好的问题！让我从几个维度分析：

**1. 并发模型**
```go
// Go 的 goroutine 非常轻量，可以轻松处理数万并发
go func() {
    // 处理请求
}()
```

Go 的 goroutine 只占用 2KB 初始栈空间，而操作系统线程通常需要 2MB。这意味着：
- Node.js：单线程事件循环，CPU 密集型任务会阻塞
- Python：GIL 限制了多线程并发
- Go：可以轻松创建数万个 goroutine 处理并发请求

**2. 类型安全**
```go
// Go 是静态类型，编译时就能发现错误
func CalculateCost(tokens int64, pricePerToken float64) float64 {
    return float64(tokens) * pricePerToken
}
```

**3. 部署简单**
- Go 编译成单个二进制文件，无需运行时环境
- Node.js/Python 需要安装依赖、配置环境

**4. 性能**
- Go 的性能接近 C/C++，远超 Node.js/Python
- 对于 API 网关这种高吞吐场景，性能至关重要

**学生**：明白了！那为什么数据库选择 PostgreSQL 而不是 MySQL？

**教授**：PostgreSQL 在以下方面更有优势：

1. **JSONB 类型**：Sub2API 需要存储灵活的配置（如账号凭证、模型映射），JSONB 比 MySQL 的 JSON 性能更好
2. **复杂查询**：PostgreSQL 的查询优化器更强大
3. **事务隔离**：更严格的 ACID 保证
4. **扩展性**：丰富的扩展生态（如 PostGIS、全文搜索）

现在让我们看看整体的**分层架构**：

### 后端分层架构

```
┌─────────────────────────────────────────────────────────────┐
│                      Handler 层（HTTP）                      │
│  职责：接收 HTTP 请求，参数验证，调用 Service，返回响应      │
│  文件：backend/internal/handler/*.go                         │
└────────────────────────────┬────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────┐
│                     Service 层（业务逻辑）                    │
│  职责：核心业务逻辑，事务管理，调用 Repository               │
│  文件：backend/internal/service/*.go                         │
└────────────────────────────┬────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────┐
│                   Repository 层（数据访问）                   │
│  职责：数据库操作，缓存操作，对外提供接口                     │
│  文件：backend/internal/repository/*.go                      │
└────────────────────────────┬────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────┐
│                      Ent ORM（生成代码）                      │
│  职责：类型安全的数据库查询，自动生成 CRUD 代码               │
│  文件：backend/ent/*.go（自动生成，不要手动修改）             │
└─────────────────────────────────────────────────────────────┘
```

**学生**：我注意到有个"不要手动修改"的提示，这是为什么？

**教授**：这是 Ent ORM 的核心特性。让我详细解释：

### Ent ORM 的工作原理

**1. Schema 定义（你需要编辑的）**
```go
// backend/ent/schema/user.go
package schema

type User struct {
    ent.Schema
}

func (User) Fields() []ent.Field {
    return []ent.Field{
        field.String("email").Unique(),
        field.String("password"),
        field.Float("balance").Default(0),
    }
}

func (User) Edges() []ent.Edge {
    return []ent.Edge{
        edge.To("api_keys", APIKey.Type),  // 一对多关系
    }
}
```

**2. 代码生成（自动完成）**
```bash
cd backend
go generate ./ent
```

这个命令会生成数千行类型安全的代码：
- `backend/ent/user.go`：User 实体
- `backend/ent/user_create.go`：创建用户的构建器
- `backend/ent/user_query.go`：查询用户的构建器
- `backend/ent/user_update.go`：更新用户的构建器
- 等等...

**3. 类型安全的使用**
```go
// 创建用户
user, err := client.User.Create().
    SetEmail("user@example.com").
    SetPassword(hashedPassword).
    SetBalance(100.0).
    Save(ctx)

// 查询用户及其 API Keys
user, err := client.User.Query().
    Where(user.EmailEQ("user@example.com")).
    WithAPIKeys().  // 预加载关联数据
    Only(ctx)
```

**学生**：哇！这样编译器就能检查类型错误了，不会出现字段名拼写错误！

**教授**：完全正确！这就是 Ent 的优势。如果你修改了 Schema，必须重新生成代码，否则代码会编译失败。这也是为什么 CI/CD 中有检查：确保生成的代码已提交。

---

## 第三讲：依赖注入与服务初始化

**教授**：现在我们来看一个非常重要的概念：**依赖注入（Dependency Injection, DI）**。

**学生**：我听说过这个概念，但不太理解为什么需要它？

**教授**：让我用一个反例来说明。假设没有依赖注入，代码会是这样：

```go
// ❌ 不好的做法：硬编码依赖
type UserService struct {}

func (s *UserService) GetUser(id int64) (*User, error) {
    // 直接创建数据库连接
    db, _ := sql.Open("postgres", "...")
    // 直接创建 Redis 连接
    redis := redis.NewClient(&redis.Options{...})

    // 业务逻辑...
}
```

这样做有什么问题？

**学生**：嗯...每次调用都要创建连接？而且测试的时候怎么办？

**教授**：非常好！主要问题是：
1. **无法复用连接**：每次都创建新连接，性能差
2. **无法测试**：无法注入 mock 对象
3. **耦合度高**：Service 直接依赖具体实现
4. **配置分散**：连接参数散落在各处

现在看看使用依赖注入的正确做法：

```go
// ✅ 好的做法：依赖注入
type UserService struct {
    repo UserRepository  // 依赖接口，不是具体实现
}

func NewUserService(repo UserRepository) *UserService {
    return &UserService{repo: repo}
}

func (s *UserService) GetUser(id int64) (*User, error) {
    return s.repo.GetByID(id)  // 使用注入的依赖
}
```

**学生**：我明白了！但是如果有很多服务，每个服务都依赖其他服务，手动创建会很复杂吧？

**教授**：没错！这就是为什么需要 **Wire**。让我展示 Sub2API 的实际代码：

### Wire 依赖注入

**1. 定义 Provider（backend/cmd/server/wire.go）**
```go
//go:build wireinject

func InitializeApp() (*App, func(), error) {
    wire.Build(
        // 配置层
        config.ProviderSet,

        // 基础设施层
        repository.ProviderSet,  // Ent, Redis, 缓存

        // 业务逻辑层
        service.ProviderSet,     // 所有 Service

        // 中间件层
        middleware.ProviderSet,  // 认证、CORS 等

        // HTTP 层
        handler.ProviderSet,     // 所有 Handler
        server.ProviderSet,      // Router, HTTP Server

        // 应用层
        wire.Struct(new(App), "*"),
    )
    return nil, nil, nil
}
```

**2. Wire 自动生成代码（backend/cmd/server/wire_gen.go）**
```go
// 这个文件是自动生成的，包含了所有依赖的创建顺序
func InitializeApp() (*App, func(), error) {
    // 1. 加载配置
    configConfig := config.Load()

    // 2. 创建数据库连接
    client := repository.ProvideEnt(configConfig)

    // 3. 创建 Redis 连接
    redisClient := repository.ProvideRedis(configConfig)

    // 4. 创建 Repository
    userRepo := repository.NewUserRepository(client)

    // 5. 创建 Service
    userService := service.NewUserService(userRepo)

    // 6. 创建 Handler
    userHandler := handler.NewUserHandler(userService)

    // ... 数百行自动生成的代码

    // 7. 创建 HTTP Server
    httpServer := server.ProvideHTTPServer(router, configConfig)

    // 8. 组装 App
    app := &App{
        Config: configConfig,
        Server: httpServer,
        // ...
    }

    // 9. 返回清理函数
    cleanup := func() {
        redisClient.Close()
        client.Close()
    }

    return app, cleanup, nil
}
```

**学生**：太神奇了！Wire 自动计算出了依赖的创建顺序！

**教授**：是的！而且这一切都在**编译时**完成，没有运行时反射开销。如果依赖关系有问题（比如循环依赖），编译时就会报错。

现在让我们看看服务启动流程：

### 服务启动流程

```go
// backend/cmd/server/main.go
func main() {
    // 1. 检查是否需要初始化设置
    if setup.NeedsSetup() {
        // 启动设置向导
        setup.RunSetupWizard()
        return
    }

    // 2. 初始化应用（Wire 生成的函数）
    app, cleanup, err := InitializeApp()
    if err != nil {
        log.Fatal(err)
    }
    defer cleanup()  // 确保资源释放

    // 3. 启动 HTTP 服务器
    if err := app.Server.ListenAndServe(); err != nil {
        log.Fatal(err)
    }
}
```

**学生**：我注意到有个"设置向导"，这是做什么的？

**教授**：非常好的观察！Sub2API 的设计理念是"开箱即用"。第一次启动时，会引导用户完成：
1. 数据库配置（自动创建数据库）
2. Redis 配置
3. 管理员账号创建
4. JWT 密钥生成

这样用户不需要手动编辑配置文件，降低了部署门槛。

---

## 第四讲：认证与授权系统

**教授**：现在我们进入一个非常关键的模块：认证与授权。Sub2API 有两套完全独立的认证系统，你们能猜到为什么吗？

**学生**：是不是一套给管理员用，一套给 API 用户用？

**教授**：完全正确！让我详细解释：

### 双重认证架构

```
┌─────────────────────────────────────────────────────────────┐
│                    管理后台（Web UI）                         │
│              使用 JWT Token 认证                             │
│  登录 → 获取 access_token + refresh_token → 访问管理接口     │
└─────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│                    API 网关（程序调用）                       │
│              使用 API Key 认证                               │
│  生成 API Key (sk-xxx) → 请求时携带 → 验证并计费            │
└─────────────────────────────────────────────────────────────┘
```

**学生**：为什么不统一用一种认证方式？

**教授**：因为使用场景完全不同：

**JWT Token（管理后台）**：
- 用户是人，通过浏览器访问
- 需要登录/登出功能
- Token 有过期时间（通常 24 小时）
- 支持刷新 Token
- 需要防止 CSRF 攻击

**API Key（程序调用）**：
- 用户是程序，通过 HTTP 客户端访问
- 长期有效（除非手动撤销）
- 需要高性能验证（每个请求都要验证）
- 需要精确计费和限流
- 可以设置 IP 白名单/黑名单

现在让我们深入看看 API Key 的认证流程：

### API Key 认证流程（详细版）

```go
// backend/internal/server/middleware/api_key_auth.go

func APIKeyAuthMiddleware(service *service.APIKeyService) gin.HandlerFunc {
    return func(c *gin.Context) {
        // 第 1 步：提取 API Key
        // 优先级：Authorization Bearer > x-api-key > x-goog-api-key
        apiKey := extractAPIKey(c)
        if apiKey == "" {
            c.JSON(401, gin.H{"error": "Missing API key"})
            c.Abort()
            return
        }

        // 第 2 步：从缓存/数据库查询 API Key
        keyInfo, err := service.GetByKeyForAuth(c.Request.Context(), apiKey)
        if err != nil {
            c.JSON(401, gin.H{"error": "Invalid API key"})
            c.Abort()
            return
        }

        // 第 3 步：检查 API Key 状态
        if keyInfo.Status != "active" {
            c.JSON(403, gin.H{"error": "API key is disabled"})
            c.Abort()
            return
        }

        // 第 4 步：检查过期时间
        if keyInfo.IsExpired() {
            c.JSON(403, gin.H{"error": "API key expired"})
            c.Abort()
            return
        }

        // 第 5 步：检查 IP 限制
        clientIP := c.ClientIP()
        if !keyInfo.IsIPAllowed(clientIP) {
            c.JSON(403, gin.H{"error": "IP not allowed"})
            c.Abort()
            return
        }

        // 第 6 步：检查配额
        if keyInfo.IsQuotaExhausted() {
            c.JSON(429, gin.H{"error": "Quota exhausted"})
            c.Abort()
            return
        }

        // 第 7 步：检查用户状态
        user, err := service.GetUser(keyInfo.UserID)
        if !user.IsActive() {
            c.JSON(403, gin.H{"error": "User is disabled"})
            c.Abort()
            return
        }

        // 第 8 步：检查订阅限制（如果有）
        if keyInfo.GroupID != nil {
            subscription := service.GetSubscription(user.ID, *keyInfo.GroupID)
            if subscription != nil {
                if subscription.IsDailyLimitExceeded() {
                    c.JSON(429, gin.H{"error": "Daily limit exceeded"})
                    c.Abort()
                    return
                }
            }
        }

        // 第 9 步：检查余额（非订阅模式）
        if user.Balance <= 0 {
            c.JSON(402, gin.H{"error": "Insufficient balance"})
            c.Abort()
            return
        }

        // 第 10 步：将认证信息存入上下文
        c.Set("api_key", keyInfo)
        c.Set("user", AuthSubject{
            UserID:      user.ID,
            Concurrency: user.Concurrency,
        })

        // 第 11 步：异步更新最后使用时间
        go service.TouchLastUsed(keyInfo.ID)

        c.Next()
    }
}
```

**学生**：哇！这么多检查步骤！但我注意到第 2 步说"从缓存/数据库查询"，这是怎么实现的？

**教授**：非常好的问题！这涉及到一个关键的性能优化：**多级缓存**。

### API Key 多级缓存架构

```
请求到达
   ↓
┌─────────────────────────────────────────┐
│  L1 缓存（进程内存 - Ristretto）         │
│  - 容量：可配置（如 10000 个 key）       │
│  - TTL：60 秒                           │
│  - 命中率：~95%                         │
│  - 延迟：< 1μs                          │
└────────────┬────────────────────────────┘
             │ Miss
             ↓
┌─────────────────────────────────────────┐
│  L2 缓存（Redis）                        │
│  - TTL：300 秒                          │
│  - 命中率：~99%                         │
│  - 延迟：< 1ms                          │
└────────────┬────────────────────────────┘
             │ Miss
             ↓
┌─────────────────────────────────────────┐
│  数据库（PostgreSQL）                    │
│  - 永久存储                             │
│  - 延迟：5-10ms                         │
└─────────────────────────────────────────┘
```

**代码实现**：
```go
// backend/internal/service/api_key_auth_cache_impl.go

func (s *APIKeyAuthCacheService) GetByKeyForAuth(ctx context.Context, key string) (*APIKey, error) {
    // 尝试 L1 缓存
    if val, found := s.l1Cache.Get(key); found {
        return val.(*APIKey), nil
    }

    // 尝试 L2 缓存（Redis）
    cached, err := s.redis.Get(ctx, "apikey:"+key).Result()
    if err == nil {
        var apiKey APIKey
        json.Unmarshal([]byte(cached), &apiKey)

        // 回填 L1 缓存
        s.l1Cache.SetWithTTL(key, &apiKey, 1, 60*time.Second)
        return &apiKey, nil
    }

    // 使用 Singleflight 防止缓存击穿
    result, err, _ := s.sf.Do(key, func() (interface{}, error) {
        // 从数据库查询
        apiKey, err := s.repo.GetByKeyForAuth(ctx, key)
        if err != nil {
            return nil, err
        }

        // 写入 L2 缓存
        data, _ := json.Marshal(apiKey)
        s.redis.SetEx(ctx, "apikey:"+key, data, 300*time.Second)

        // 写入 L1 缓存
        s.l1Cache.SetWithTTL(key, apiKey, 1, 60*time.Second)

        return apiKey, nil
    })

    if err != nil {
        return nil, err
    }
    return result.(*APIKey), nil
}
```

**学生**：我看到了 `Singleflight`，这是什么？

**教授**：这是防止**缓存击穿**的关键技术！让我解释一个场景：

假设某个热门 API Key 的缓存刚好过期，此时有 1000 个并发请求同时到达。如果没有 Singleflight：
```
请求1 → 缓存 Miss → 查数据库
请求2 → 缓存 Miss → 查数据库
请求3 → 缓存 Miss → 查数据库
...
请求1000 → 缓存 Miss → 查数据库
```
数据库瞬间被 1000 个查询打爆！

使用 Singleflight 后：
```
请求1 → 缓存 Miss → 查数据库（获得锁）
请求2 → 缓存 Miss → 等待请求1的结果
请求3 → 缓存 Miss → 等待请求1的结果
...
请求1 完成 → 所有等待的请求共享结果
```
只有一个请求真正查询数据库，其他请求等待并共享结果！

**学生**：太聪明了！那 JWT 认证是怎么实现的？

**教授**：JWT 认证相对简单，但有一些巧妙的设计。让我展示：

### JWT 认证流程

**1. 登录获取 Token**
```go
// backend/internal/service/auth_service.go

func (s *AuthService) Login(email, password string) (*LoginResponse, error) {
    // 1. 查询用户
    user, err := s.userRepo.GetByEmail(email)
    if err != nil {
        return nil, ErrInvalidCredentials
    }

    // 2. 验证密码（bcrypt）
    if !user.VerifyPassword(password) {
        return nil, ErrInvalidCredentials
    }

    // 3. 检查 2FA
    if user.TOTPEnabled {
        return &LoginResponse{
            RequiresTOTP: true,
            TempToken:    generateTempToken(user.ID),
        }, nil
    }

    // 4. 生成 JWT Token
    accessToken, err := s.generateAccessToken(user)
    refreshToken, err := s.generateRefreshToken(user)

    return &LoginResponse{
        AccessToken:  accessToken,
        RefreshToken: refreshToken,
        ExpiresIn:    3600, // 1 小时
    }, nil
}

func (s *AuthService) generateAccessToken(user *User) (string, error) {
    claims := JWTClaims{
        UserID:       user.ID,
        Email:        user.Email,
        Role:         user.Role,
        TokenVersion: user.TokenVersion,  // 关键！用于撤销 Token
        RegisteredClaims: jwt.RegisteredClaims{
            ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
            IssuedAt:  jwt.NewNumericDate(time.Now()),
        },
    }

    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    return token.SignedString([]byte(s.config.JWT.Secret))
}
```

**学生**：我注意到有个 `TokenVersion` 字段，这是做什么的？

**教授**：这是一个非常巧妙的设计！让我解释一个问题：

JWT Token 是**无状态**的，一旦签发就无法撤销（除非等它过期）。但如果用户修改了密码，我们希望立即让所有旧 Token 失效，怎么办？

答案就是 `TokenVersion`：
```go
// 用户修改密码时
func (s *AuthService) ChangePassword(userID int64, newPassword string) error {
    // 1. 更新密码
    hashedPassword := bcrypt.Hash(newPassword)

    // 2. 递增 TokenVersion（关键！）
    err := s.userRepo.Update(userID, map[string]interface{}{
        "password":      hashedPassword,
        "token_version": gorm.Expr("token_version + 1"),
    })

    return err
}

// 验证 Token 时
func (s *AuthService) ValidateToken(tokenString string) (*JWTClaims, error) {
    // 1. 解析 Token
    claims, err := parseToken(tokenString)

    // 2. 查询用户当前的 TokenVersion
    user, err := s.userRepo.GetByID(claims.UserID)

    // 3. 比较版本号
    if claims.TokenVersion != user.TokenVersion {
        return nil, ErrTokenInvalid  // Token 已被撤销！
    }

    return claims, nil
}
```

**学生**：原来如此！修改密码后 TokenVersion 递增，所有旧 Token 的版本号就对不上了！

**教授**：完全正确！这样就实现了"伪撤销"——虽然 Token 本身还有效，但我们拒绝接受旧版本的 Token。

现在让我们看看 **Token 刷新机制**：

### Token 刷新机制

```go
// 前端代码：frontend/src/api/client.ts

// 响应拦截器
apiClient.interceptors.response.use(
  (response) => response,
  async (error) => {
    const originalRequest = error.config

    // 如果是 401 且不是刷新接口本身
    if (error.response?.status === 401 && !originalRequest._retry) {
      originalRequest._retry = true

      // 尝试刷新 Token
      const refreshToken = localStorage.getItem('refresh_token')
      if (refreshToken) {
        try {
          const response = await axios.post('/api/v1/auth/refresh', {
            refresh_token: refreshToken
          })

          const { access_token } = response.data.data
          localStorage.setItem('access_token', access_token)

          // 重试原始请求
          originalRequest.headers.Authorization = `Bearer ${access_token}`
          return apiClient(originalRequest)
        } catch (refreshError) {
          // 刷新失败，跳转登录页
          localStorage.clear()
          window.location.href = '/login'
        }
      }
    }

    return Promise.reject(error)
  }
)
```

**学生**：这个设计很巧妙！用户感觉不到 Token 过期，因为自动刷新了。但我有个疑问：如果多个请求同时遇到 401，会不会重复刷新？

**教授**：非常好的问题！这就需要**刷新队列**：

```typescript
// frontend/src/api/client.ts

let isRefreshing = false
let refreshSubscribers: Array<(token: string) => void> = []

function onRefreshed(token: string) {
  refreshSubscribers.forEach(callback => callback(token))
  refreshSubscribers = []
}

apiClient.interceptors.response.use(
  (response) => response,
  async (error) => {
    const originalRequest = error.config

    if (error.response?.status === 401 && !originalRequest._retry) {
      if (isRefreshing) {
        // 如果正在刷新，加入队列等待
        return new Promise((resolve) => {
          refreshSubscribers.push((token: string) => {
            originalRequest.headers.Authorization = `Bearer ${token}`
            resolve(apiClient(originalRequest))
          })
        })
      }

      originalRequest._retry = true
      isRefreshing = true

      try {
        const response = await axios.post('/api/v1/auth/refresh', {
          refresh_token: localStorage.getItem('refresh_token')
        })

        const { access_token } = response.data.data
        localStorage.setItem('access_token', access_token)

        // 通知所有等待的请求
        onRefreshed(access_token)
        isRefreshing = false

        originalRequest.headers.Authorization = `Bearer ${access_token}`
        return apiClient(originalRequest)
      } catch (refreshError) {
        isRefreshing = false
        localStorage.clear()
        window.location.href = '/login'
      }
    }

    return Promise.reject(error)
  }
)
```

**学生**：明白了！第一个请求负责刷新，其他请求排队等待结果。这样只会发起一次刷新请求！

---

## 第五讲：网关核心 - 请求转发与调度

**教授**：现在我们进入 Sub2API 最核心的部分：**网关调度系统**。这是整个项目最复杂、最精妙的模块。

**学生**：我很好奇，当一个 API 请求到达时，系统是如何选择上游账号的？

**教授**：非常好的问题！让我用一个完整的请求流程来说明：

### 完整请求流程（从接收到响应）

```
1. 客户端发起请求
   POST /v1/messages
   Authorization: Bearer sk-user-xxx
   {
     "model": "claude-3-5-sonnet-20241022",
     "messages": [{"role": "user", "content": "Hello"}]
   }
   ↓
2. API Key 认证中间件
   - 验证 API Key
   - 检查配额、余额、订阅
   - 加载用户信息和分组
   ↓
3. 请求解析
   - 提取 model, messages, stream 等字段
   - 生成 session hash（用于粘性会话）
   ↓
4. 账号选择（核心！）
   - 查询粘性会话缓存
   - 如果有绑定 → 使用绑定的账号
   - 如果无绑定 → 负载均衡选择
   ↓
5. 并发控制
   - 获取用户级并发槽位
   - 获取账号级并发槽位
   - 如果满了 → 等待（带超时）
   ↓
6. 请求转发
   - 构建上游请求
   - 应用模型映射
   - 设置认证头（OAuth token 或 API key）
   - 发送到上游 API
   ↓
7. 响应处理
   - 流式响应 → 逐块转发
   - 非流式响应 → 聚合后返回
   - 提取 token 使用量
   ↓
8. 计费与记录
   - 计算成本
   - 扣除余额/更新配额
   - 写入使用日志
   ↓
9. 释放资源
   - 释放并发槽位
   - 更新粘性会话 TTL
   ↓
10. 返回响应给客户端
```

**学生**：第 4 步的"账号选择"听起来很复杂，能详细讲讲吗？

**教授**：当然！这是整个系统的核心算法。让我分步骤讲解：

### 粘性会话（Sticky Session）机制

**为什么需要粘性会话？**

想象这个场景：
```
用户：请帮我写一个排序算法
Claude（账号A）：好的，这是冒泡排序...

用户：能优化一下吗？
Claude（账号B）：什么需要优化？（账号B没有上下文！）
```

**粘性会话的实现**：

```go
// backend/internal/service/gateway_service.go

func (s *GatewayService) GenerateSessionHash(
    req *ParsedRequest,
    clientIP, userAgent string,
    apiKeyID int64,
) string {
    // 优先级 1：客户端显式指定 session ID
    // 格式：metadata.user_id = "session_abc123"
    if strings.HasPrefix(req.MetadataUserID, "session_") {
        return req.MetadataUserID
    }

    // 优先级 2：如果请求标记为 ephemeral（临时），基于内容哈希
    // 这样相同的问题会路由到同一个账号（利用缓存）
    if req.CacheControl == "ephemeral" {
        content := req.System + "\n" + messagesString(req.Messages)
        return "content_" + sha256Hash(content)
    }

    // 优先级 3：基于会话上下文哈希
    // 包含：客户端 IP + User Agent + API Key ID + 消息内容
    sessionContext := fmt.Sprintf("%s|%s|%d|%s",
        clientIP, userAgent, apiKeyID, messagesString(req.Messages))
    return "session_" + sha256Hash(sessionContext)
}
```

**粘性会话缓存**：
```go
// Redis 中的存储
// Key: sticky_session:{groupID}:{sessionHash}
// Value: accountID
// TTL: 1 小时

func (s *GatewayService) GetStickyAccount(
    groupID int64,
    sessionHash string,
) (int64, bool) {
    key := fmt.Sprintf("sticky_session:%d:%s", groupID, sessionHash)
    accountID, err := s.redis.Get(ctx, key).Int64()
    if err != nil {
        return 0, false
    }
    return accountID, true
}

func (s *GatewayService) SetStickyAccount(
    groupID int64,
    sessionHash string,
    accountID int64,
) {
    key := fmt.Sprintf("sticky_session:%d:%s", groupID, sessionHash)
    s.redis.Set(ctx, key, accountID, 1*time.Hour)
}
```

**学生**：我明白了！但如果粘性绑定的账号不可用了怎么办？

**教授**：这就涉及到**故障转移（Failover）**机制：

### 故障转移与重试策略

```go
// backend/internal/service/gateway_service.go

func (s *GatewayService) Forward(
    ctx context.Context,
    req *ParsedRequest,
    groupID int64,
    sessionHash string,
) (*Response, error) {
    maxRetries := 3  // 最多重试 3 次
    var lastError error

    for attempt := 0; attempt < maxRetries; attempt++ {
        // 1. 选择账号
        account, releaseFunc, err := s.selectAccount(groupID, sessionHash)
        if err != nil {
            return nil, err
        }
        defer releaseFunc()

        // 2. 转发请求
        response, err := s.forwardToUpstream(ctx, req, account)

        // 3. 判断是否需要重试
        if err == nil {
            // 成功！更新粘性会话
            s.SetStickyAccount(groupID, sessionHash, account.ID)
            return response, nil
        }

        // 4. 分析错误类型
        if shouldRetry(err) {
            lastError = err

            // 清除粘性会话（如果账号有问题）
            if shouldClearSticky(err, account) {
                s.ClearStickyAccount(groupID, sessionHash)
            }

            // 标记账号暂时不可用
            if isAccountError(err) {
                s.markAccountUnavailable(account.ID, 5*time.Minute)
            }

            // 继续下一次重试
            continue
        }

        // 不可重试的错误，直接返回
        return nil, err
    }

    return nil, fmt.Errorf("max retries exceeded: %w", lastError)
}
```

**什么情况下需要重试？**

```go
func shouldRetry(err error) bool {
    // 1. 5xx 服务器错误
    if isHTTPError(err, 500, 502, 503, 504) {
        return true
    }

    // 2. 超时错误
    if errors.Is(err, context.DeadlineExceeded) {
        return true
    }

    // 3. 连接错误
    if isNetworkError(err) {
        return true
    }

    // 4. 特定的 API 错误
    if isOverloadedError(err) {  // 账号过载
        return true
    }

    return false
}

func shouldClearSticky(err error, account *Account) bool {
    // 1. 账号被禁用
    if account.Status == "disabled" {
        return true
    }

    // 2. 账号不可调度
    if !account.Schedulable {
        return true
    }

    // 3. 模型限流
    if isRateLimitError(err) {
        return true
    }

    return false
}
```

**学生**：我注意到有个"标记账号暂时不可用"，这是怎么实现的？

**教授**：这是一个非常重要的保护机制，叫做**熔断（Circuit Breaker）**：

### 账号熔断机制

```go
// backend/internal/service/gateway_service.go

func (s *GatewayService) markAccountUnavailable(
    accountID int64,
    duration time.Duration,
) {
    // 在 Redis 中设置标记
    key := fmt.Sprintf("account:unavailable:%d", accountID)
    s.redis.Set(ctx, key, "1", duration)

    // 同时更新数据库（可选）
    s.accountRepo.Update(accountID, map[string]interface{}{
        "temp_unschedulable_until": time.Now().Add(duration),
    })
}

func (s *GatewayService) isAccountAvailable(accountID int64) bool {
    key := fmt.Sprintf("account:unavailable:%d", accountID)
    exists, _ := s.redis.Exists(ctx, key).Result()
    return exists == 0
}
```

这样，如果某个账号连续出错，会被暂时"冷却"，避免持续失败。

**学生**：那如果没有粘性绑定，系统是如何选择账号的？

**教授**：这就是**负载均衡算法**了！Sub2API 使用了一个非常智能的调度器：

### 负载感知调度器

```go
// backend/internal/service/openai_account_scheduler.go

func (s *AccountScheduler) SelectAccount(
    groupID int64,
    model string,
) (*Account, error) {
    // 1. 获取分组内所有可用账号
    accounts := s.getAvailableAccounts(groupID)

    // 2. 过滤：只保留支持该模型的账号
    accounts = filterByModel(accounts, model)

    // 3. 过滤：排除暂时不可用的账号
    accounts = filterAvailable(accounts)

    // 4. 获取每个账号的负载信息
    loadInfos := s.getLoadInfos(accounts)

    // 5. 计算每个账号的得分
    scores := make([]float64, len(accounts))
    for i, account := range accounts {
        load := loadInfos[account.ID]

        // 得分计算公式（越低越好）
        scores[i] = calculateScore(
            account.Priority,           // 优先级（管理员配置）
            load.CurrentConcurrency,    // 当前并发数
            load.WaitingCount,          // 等待队列长度
            account.Concurrency,        // 最大并发数
            load.ErrorRate,             // 错误率
            load.AvgTTFT,              // 平均首 token 时间
        )
    }

    // 6. 选择得分最低的账号
    bestIndex := argmin(scores)
    return accounts[bestIndex], nil
}
```

**得分计算公式**：

```go
func calculateScore(
    priority int,
    currentConcurrency int,
    waitingCount int,
    maxConcurrency int,
    errorRate float64,
    avgTTFT float64,
) float64 {
    // 基础得分：优先级（越小越优先）
    score := float64(priority) * 100

    // 负载因子：当前使用率
    loadRate := float64(currentConcurrency) / float64(maxConcurrency)
    score += loadRate * 50

    // 等待惩罚
    score += float64(waitingCount) * 10

    // 错误率惩罚
    score += errorRate * 100

    // 性能因子：TTFT 越低越好
    score += avgTTFT / 1000  // 转换为秒

    return score
}
```

**学生**：这个算法考虑了好多因素！但我还是不太理解"并发控制"是怎么工作的？

**教授**：并发控制是防止系统过载的关键！让我详细解释：

### 并发控制系统

Sub2API 有**两级并发控制**：

```
┌─────────────────────────────────────────────────────────────┐
│                    用户级并发控制                             │
│  限制：每个用户最多 N 个并发请求                              │
│  目的：防止单个用户占用所有资源                               │
│  配置：user.concurrency（如 5）                              │
└─────────────────────────────────────────────────────────────┘
                             ↓
┌─────────────────────────────────────────────────────────────┐
│                    账号级并发控制                             │
│  限制：每个上游账号最多 M 个并发请求                          │
│  目的：遵守上游 API 的并发限制                               │
│  配置：account.concurrency（如 10）                          │
└─────────────────────────────────────────────────────────────┘
```

**实现原理（基于 Redis）**：

```go
// backend/internal/service/concurrency_service.go

type ConcurrencyService struct {
    redis *redis.Client
}

// 获取账号并发槽位
func (s *ConcurrencyService) AcquireAccountSlot(
    ctx context.Context,
    accountID int64,
    maxConcurrency int,
    requestID string,
) error {
    key := fmt.Sprintf("concurrency:account:%d", accountID)

    // Lua 脚本保证原子性
    script := `
        local key = KEYS[1]
        local max = tonumber(ARGV[1])
        local requestID = ARGV[2]
        local ttl = tonumber(ARGV[3])

        -- 获取当前并发数
        local current = redis.call('SCARD', key)

        -- 检查是否超限
        if current >= max then
            return 0  -- 失败
        end

        -- 添加请求 ID 到集合
        redis.call('SADD', key, requestID)
        redis.call('EXPIRE', key, ttl)
        return 1  -- 成功
    `

    result, err := s.redis.Eval(ctx, script, []string{key},
        maxConcurrency, requestID, 300).Int()

    if result == 0 {
        return ErrConcurrencyLimitExceeded
    }
    return nil
}

// 释放账号并发槽位
func (s *ConcurrencyService) ReleaseAccountSlot(
    ctx context.Context,
    accountID int64,
    requestID string,
) {
    key := fmt.Sprintf("concurrency:account:%d", accountID)
    s.redis.SRem(ctx, key, requestID)
}
```

**等待机制（带指数退避）**：

```go
// backend/internal/handler/gateway_helper.go

func (h *ConcurrencyHelper) AcquireAccountSlotWithWait(
    ctx context.Context,
    accountID int64,
    maxConcurrency int,
    requestID string,
) error {
    maxWaitTime := 30 * time.Second
    startTime := time.Now()

    backoff := 100 * time.Millisecond  // 初始等待 100ms

    for {
        // 尝试获取槽位
        err := h.service.AcquireAccountSlot(ctx, accountID, maxConcurrency, requestID)
        if err == nil {
            return nil  // 成功！
        }

        // 检查是否超时
        if time.Since(startTime) > maxWaitTime {
            return ErrWaitTimeout
        }

        // 如果是流式请求，发送 ping 保持连接
        if h.isStreaming {
            h.sendSSEPing()
        }

        // 指数退避等待
        time.Sleep(backoff)
        backoff = time.Duration(float64(backoff) * 1.5)  // 1.5 倍增长
        if backoff > 2*time.Second {
            backoff = 2 * time.Second  // 最大 2 秒
        }

        // 添加随机抖动（±20%）
        jitter := time.Duration(rand.Float64() * 0.4 * float64(backoff))
        backoff += jitter - time.Duration(0.2*float64(backoff))
    }
}
```

**学生**：我明白了！如果并发满了，请求会等待，而不是直接拒绝。而且等待时间会逐渐增加，避免"惊群效应"！

**教授**：完全正确！这就是**指数退避（Exponential Backoff）+ 随机抖动（Jitter）**的经典应用。

---

## 第六讲：流式响应处理与协议适配

**教授**：现在我们来看一个非常有趣的技术挑战：**流式响应（Server-Sent Events, SSE）**的处理。

**学生**：我知道 ChatGPT 的回复是一个字一个字蹦出来的，这就是流式响应吧？

**教授**：没错！但在 Sub2API 中，流式响应的处理要复杂得多。让我解释为什么：

### 流式响应的挑战

```
客户端 ←→ Sub2API ←→ 上游 API (Claude/OpenAI/Gemini)
```

Sub2API 需要：
1. **实时转发**：上游返回一块数据，立即转发给客户端（不能等全部接收完）
2. **协议转换**：不同上游的 SSE 格式不同，需要统一
3. **Token 统计**：从流式数据中提取 token 使用量
4. **错误处理**：流式传输中断时的处理
5. **保持连接**：长时间等待时发送 ping 防止超时

**SSE 格式示例（Anthropic）**：
```
event: message_start
data: {"type":"message_start","message":{"id":"msg_xxx","role":"assistant"}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}

event: message_stop
data: {"type":"message_stop"}
```

**学生**：看起来很复杂！Sub2API 是怎么处理的？

**教授**：让我展示核心代码：

### 流式响应转发实现

```go
// backend/internal/service/gateway_service.go

func (s *GatewayService) forwardStreamingResponse(
    ctx context.Context,
    upstreamResp *http.Response,
    clientWriter http.ResponseWriter,
) (*UsageInfo, error) {
    // 1. 设置响应头
    clientWriter.Header().Set("Content-Type", "text/event-stream")
    clientWriter.Header().Set("Cache-Control", "no-cache")
    clientWriter.Header().Set("Connection", "keep-alive")
    clientWriter.Header().Set("X-Accel-Buffering", "no")  // 禁用 Nginx 缓冲

    flusher, ok := clientWriter.(http.Flusher)
    if !ok {
        return nil, errors.New("streaming not supported")
    }

    // 2. 创建 SSE 扫描器
    scanner := bufio.NewScanner(upstreamResp.Body)
    scanner.Buffer(make([]byte, 64*1024), 1024*1024)  // 64KB 初始，1MB 最大

    var usage UsageInfo
    var eventType string
    var dataLines []string

    // 3. 逐行读取并转发
    for scanner.Scan() {
        line := scanner.Text()

        // 空行表示事件结束
        if line == "" {
            if len(dataLines) > 0 {
                // 处理完整事件
                event := parseSSEEvent(eventType, dataLines)

                // 提取 usage 信息
                if event.Type == "message_delta" || event.Type == "message_stop" {
                    extractUsage(event, &usage)
                }

                // 转发给客户端
                writeSSEEvent(clientWriter, eventType, dataLines)
                flusher.Flush()

                // 重置
                eventType = ""
                dataLines = nil
            }
            continue
        }

        // 解析事件类型
        if strings.HasPrefix(line, "event: ") {
            eventType = strings.TrimPrefix(line, "event: ")
            continue
        }

        // 解析数据行
        if strings.HasPrefix(line, "data: ") {
            dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
            continue
        }
    }

    if err := scanner.Err(); err != nil {
        return nil, fmt.Errorf("stream read error: %w", err)
    }

    return &usage, nil
}
```

**学生**：我注意到有个 `X-Accel-Buffering: no` 头，这是做什么的？

**教授**：非常好的观察！这是针对 Nginx 反向代理的优化。默认情况下，Nginx 会缓冲响应，这会导致流式响应延迟。设置这个头可以禁用缓冲，实现真正的实时转发。

现在让我们看看**协议适配**的问题：

### 多协议适配（Anthropic ↔ OpenAI ↔ Gemini）

Sub2API 支持三种主流 API 格式，但它们的协议差异很大：

**Anthropic Messages API**：
```json
{
  "model": "claude-3-5-sonnet-20241022",
  "messages": [{"role": "user", "content": "Hello"}],
  "max_tokens": 1024
}
```

**OpenAI Chat Completions API**：
```json
{
  "model": "gpt-4",
  "messages": [{"role": "user", "content": "Hello"}],
  "max_completion_tokens": 1024
}
```

**Gemini API**：
```json
{
  "contents": [{"role": "user", "parts": [{"text": "Hello"}]}],
  "generationConfig": {"maxOutputTokens": 1024}
}
```

**学生**：这三种格式完全不同！Sub2API 怎么处理？

**教授**：Sub2API 使用了**适配器模式**。让我展示 OpenAI → Anthropic 的转换：

```go
// backend/internal/service/openai_gateway_messages.go

func (s *OpenAIGatewayService) convertToAnthropicFormat(
    openaiReq *OpenAIRequest,
) (*AnthropicRequest, error) {
    anthropicReq := &AnthropicRequest{
        Model:      openaiReq.Model,
        MaxTokens:  openaiReq.MaxCompletionTokens,
        Stream:     openaiReq.Stream,
    }

    // 1. 转换 messages
    for _, msg := range openaiReq.Messages {
        if msg.Role == "system" {
            // OpenAI 的 system 消息 → Anthropic 的 system 参数
            anthropicReq.System = msg.Content
        } else {
            // user/assistant 消息直接转换
            anthropicReq.Messages = append(anthropicReq.Messages, Message{
                Role:    msg.Role,
                Content: msg.Content,
            })
        }
    }

    // 2. 转换 tools（函数调用）
    if len(openaiReq.Tools) > 0 {
        for _, tool := range openaiReq.Tools {
            anthropicReq.Tools = append(anthropicReq.Tools, Tool{
                Name:        tool.Function.Name,
                Description: tool.Function.Description,
                InputSchema: tool.Function.Parameters,
            })
        }
    }

    // 3. 转换 response_format
    if openaiReq.ResponseFormat != nil && openaiReq.ResponseFormat.Type == "json_object" {
        // OpenAI 的 JSON mode → Anthropic 的 JSON schema
        anthropicReq.Tools = append(anthropicReq.Tools, Tool{
            Name:        "json_response",
            Description: "Return response in JSON format",
            InputSchema: map[string]interface{}{
                "type": "object",
                "properties": map[string]interface{}{
                    "response": {"type": "string"},
                },
            },
        })
    }

    return anthropicReq, nil
}
```

**响应转换（Anthropic → OpenAI）**：

```go
func (s *OpenAIGatewayService) convertToOpenAIStreamEvent(
    anthropicEvent *SSEEvent,
) *OpenAIStreamEvent {
    switch anthropicEvent.Type {
    case "message_start":
        return &OpenAIStreamEvent{
            ID:      anthropicEvent.Message.ID,
            Object:  "chat.completion.chunk",
            Created: time.Now().Unix(),
            Model:   anthropicEvent.Message.Model,
            Choices: []Choice{{
                Index: 0,
                Delta: Delta{Role: "assistant"},
            }},
        }

    case "content_block_delta":
        return &OpenAIStreamEvent{
            ID:      anthropicEvent.Message.ID,
            Object:  "chat.completion.chunk",
            Created: time.Now().Unix(),
            Choices: []Choice{{
                Index: 0,
                Delta: Delta{Content: anthropicEvent.Delta.Text},
            }},
        }

    case "message_stop":
        return &OpenAIStreamEvent{
            ID:      anthropicEvent.Message.ID,
            Object:  "chat.completion.chunk",
            Created: time.Now().Unix(),
            Choices: []Choice{{
                Index:        0,
                FinishReason: "stop",
            }},
        }
    }

    return nil
}
```

**学生**：原来如此！Sub2API 充当了"翻译官"的角色，让客户端可以用 OpenAI 的格式访问 Claude API！

**教授**：完全正确！这就是 API 网关的核心价值之一：**协议统一**。

现在让我们看看一个特殊的技术细节：**Thinking Blocks（思考块）**的处理。

### Extended Thinking 支持

Claude 支持"扩展思考"功能，会在响应中包含思考过程：

```json
{
  "type": "content_block_start",
  "content_block": {
    "type": "thinking",
    "thinking": "让我分析一下这个问题..."
  }
}
```

但这会带来一些问题：
1. **Token 消耗**：思考过程会消耗大量 token
2. **签名错误**：某些情况下 Claude 会拒绝包含 thinking 的上下文
3. **重试失败**：重试时需要过滤掉 thinking blocks

**Sub2API 的处理策略**：

```go
// backend/internal/service/gateway_request.go

func FilterThinkingBlocksForRetry(messages []Message) []Message {
    filtered := make([]Message, 0, len(messages))

    for _, msg := range messages {
        if msg.Role != "assistant" {
            filtered = append(filtered, msg)
            continue
        }

        // 过滤 assistant 消息中的 thinking blocks
        newContent := make([]ContentBlock, 0)
        for _, block := range msg.Content {
            switch block.Type {
            case "thinking":
                // 将 thinking 转换为 text（保留内容但改变类型）
                newContent = append(newContent, ContentBlock{
                    Type: "text",
                    Text: "[Previous thinking: " + block.Thinking + "]",
                })

            case "text", "tool_use":
                // 保留 text 和 tool_use
                newContent = append(newContent, block)
            }
        }

        filtered = append(filtered, Message{
            Role:    msg.Role,
            Content: newContent,
        })
    }

    return filtered
}
```

**学生**：为什么要把 thinking 转换成 text，而不是直接删除？

**教授**：非常好的问题！因为 thinking 中可能包含重要的推理过程，直接删除会丢失上下文。转换成 text 可以保留信息，同时避免签名错误。

---

## 第七讲：计费系统与使用统计

**教授**：现在我们来看 Sub2API 的另一个核心模块：**精确计费系统**。

**学生**：我很好奇，Sub2API 是如何做到 Token 级别的精确计费的？

**教授**：这涉及到三个关键步骤：**Token 统计**、**成本计算**、**余额扣除**。让我逐一讲解：

### Token 统计

**从响应中提取 Token 使用量**：

```go
// backend/internal/service/gateway_service.go

func extractUsageFromResponse(response *http.Response) (*UsageInfo, error) {
    // 1. 如果是流式响应，从 SSE 事件中提取
    if isStreamingResponse(response) {
        return extractUsageFromSSE(response.Body)
    }

    // 2. 如果是非流式响应，从 JSON body 中提取
    var body map[string]interface{}
    json.NewDecoder(response.Body).Decode(&body)

    usage := &UsageInfo{}

    // Anthropic 格式
    if u, ok := body["usage"].(map[string]interface{}); ok {
        usage.InputTokens = int64(u["input_tokens"].(float64))
        usage.OutputTokens = int64(u["output_tokens"].(float64))

        // 缓存 token
        if cacheCreation, ok := u["cache_creation_input_tokens"].(float64); ok {
            usage.CacheCreationTokens = int64(cacheCreation)
        }
        if cacheRead, ok := u["cache_read_input_tokens"].(float64); ok {
            usage.CacheReadTokens = int64(cacheRead)
        }
    }

    return usage, nil
}
```

**学生**：我注意到有"缓存 token"，这是什么？

**教授**：这是 Claude 的 Prompt Caching 功能。让我解释：

### Prompt Caching 机制

当你发送一个很长的 system prompt 时，Claude 可以缓存它：

```
第一次请求：
- Input tokens: 10000（包含长 system prompt）
- Cache creation tokens: 10000（创建缓存）
- 计费：10000 * $3/MTok + 10000 * $3.75/MTok = $0.0675

第二次请求（5 分钟内）：
- Input tokens: 100（只有新的 user 消息）
- Cache read tokens: 10000（从缓存读取 system prompt）
- 计费：100 * $3/MTok + 10000 * $0.30/MTok = $0.0033

节省：95% 的成本！
```

**定价结构**：

```go
// backend/internal/service/billing_service.go

type ModelPricing struct {
    // 标准定价
    InputPricePerMTok  float64  // 输入 token 价格（每百万）
    OutputPricePerMTok float64  // 输出 token 价格

    // 缓存定价
    CacheCreationPricePerMTok float64  // 创建缓存价格（通常是输入价格的 1.25 倍）
    CacheReadPricePerMTok     float64  // 读取缓存价格（通常是输入价格的 0.1 倍）

    // 5 分钟 vs 1 小时缓存
    CacheCreation5mPricePerMTok float64
    CacheCreation1hPricePerMTok float64

    // 优先级定价（Priority tier）
    InputPricePerMTokPriority  float64  // 通常是标准价格的 2 倍
    OutputPricePerMTokPriority float64
}
```

**成本计算**：

```go
func (s *BillingService) CalculateCost(
    usage *UsageInfo,
    model string,
    serviceTier string,  // "standard" | "priority" | "flex"
) (*CostBreakdown, error) {
    // 1. 获取模型定价
    pricing, err := s.GetModelPricing(model)
    if err != nil {
        return nil, err
    }

    // 2. 根据服务等级调整价格
    var inputPrice, outputPrice float64
    switch serviceTier {
    case "priority":
        inputPrice = pricing.InputPricePerMTokPriority
        outputPrice = pricing.OutputPricePerMTokPriority
    case "flex":
        inputPrice = pricing.InputPricePerMTok * 0.5  // Flex 是标准价格的 50%
        outputPrice = pricing.OutputPricePerMTok * 0.5
    default:  // standard
        inputPrice = pricing.InputPricePerMTok
        outputPrice = pricing.OutputPricePerMTok
    }

    // 3. 计算各项成本
    breakdown := &CostBreakdown{}

    // 输入 token 成本
    breakdown.InputCost = float64(usage.InputTokens) / 1_000_000 * inputPrice

    // 输出 token 成本
    breakdown.OutputCost = float64(usage.OutputTokens) / 1_000_000 * outputPrice

    // 缓存创建成本
    if usage.CacheCreationTokens > 0 {
        breakdown.CacheCreationCost = float64(usage.CacheCreationTokens) / 1_000_000 *
            pricing.CacheCreationPricePerMTok
    }

    // 缓存读取成本
    if usage.CacheReadTokens > 0 {
        breakdown.CacheReadCost = float64(usage.CacheReadTokens) / 1_000_000 *
            pricing.CacheReadPricePerMTok
    }

    // 总成本
    breakdown.TotalCost = breakdown.InputCost + breakdown.OutputCost +
        breakdown.CacheCreationCost + breakdown.CacheReadCost

    // 4. 应用费率倍数（账号级别的加价）
    breakdown.ActualCost = breakdown.TotalCost * rateMultiplier

    return breakdown, nil
}
```

**学生**：我明白了！但是扣除余额的时候，如何保证不会重复扣费或者漏扣费？

**教授**：这是一个非常关键的问题！Sub2API 使用了**数据库事务**来保证原子性：

### 原子性计费（事务保证）

```go
// backend/internal/service/usage_service.go

func (s *UsageService) Create(ctx context.Context, req *CreateUsageLogRequest) error {
    // 开启数据库事务
    return s.client.WithTx(ctx, func(tx *ent.Tx) error {
        // 1. 创建使用日志
        usageLog, err := tx.UsageLog.Create().
            SetUserID(req.UserID).
            SetAPIKeyID(req.APIKeyID).
            SetAccountID(req.AccountID).
            SetModel(req.Model).
            SetInputTokens(req.InputTokens).
            SetOutputTokens(req.OutputTokens).
            SetCacheCreationTokens(req.CacheCreationTokens).
            SetCacheReadTokens(req.CacheReadTokens).
            SetInputCost(req.InputCost).
            SetOutputCost(req.OutputCost).
            SetCacheCreationCost(req.CacheCreationCost).
            SetCacheReadCost(req.CacheReadCost).
            SetTotalCost(req.TotalCost).
            SetActualCost(req.ActualCost).
            Save(ctx)
        if err != nil {
            return err  // 回滚事务
        }

        // 2. 扣除用户余额（在同一个事务中）
        _, err = tx.User.UpdateOneID(req.UserID).
            AddBalance(-req.ActualCost).  // 原子递减
            Save(ctx)
        if err != nil {
            return err  // 回滚事务
        }

        // 3. 更新 API Key 配额使用量
        _, err = tx.APIKey.UpdateOneID(req.APIKeyID).
            AddQuotaUsed(req.ActualCost).
            Save(ctx)
        if err != nil {
            return err  // 回滚事务
        }

        // 事务成功提交
        return nil
    })
}
```

**学生**：我明白了！如果任何一步失败，整个事务都会回滚，保证数据一致性！

**教授**：完全正确！这就是 ACID 中的 **Atomicity（原子性）**。但还有一个性能问题：每次请求都要写数据库，会不会很慢？

**学生**：确实！如果每秒有 1000 个请求，就要写 1000 次数据库...

**教授**：所以 Sub2API 使用了**异步计费 + 缓存**的优化策略：

### 异步计费与缓存优化

```go
// backend/internal/service/billing_cache_service.go

type BillingCacheService struct {
    redis       *redis.Client
    db          *ent.Client
    workerPool  chan struct{}      // 工作池（限制并发）
    taskQueue   chan *BillingTask  // 任务队列
}

func NewBillingCacheService(redis *redis.Client, db *ent.Client) *BillingCacheService {
    s := &BillingCacheService{
        redis:      redis,
        db:         db,
        workerPool: make(chan struct{}, 10),  // 10 个 worker
        taskQueue:  make(chan *BillingTask, 1000),  // 1000 个缓冲
    }

    // 启动 worker 池
    for i := 0; i < 10; i++ {
        go s.worker()
    }

    return s
}

// 异步扣除余额
func (s *BillingCacheService) QueueDeductBalance(
    userID int64,
    amount float64,
) {
    task := &BillingTask{
        Type:   "deduct_balance",
        UserID: userID,
        Amount: amount,
    }

    // 尝试加入队列（非阻塞）
    select {
    case s.taskQueue <- task:
        // 成功加入队列
    default:
        // 队列满了，同步执行（降级策略）
        s.deductBalanceSync(userID, amount)
    }
}

// Worker 处理任务
func (s *BillingCacheService) worker() {
    for task := range s.taskQueue {
        ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)

        switch task.Type {
        case "deduct_balance":
            s.deductBalanceCache(ctx, task.UserID, task.Amount)
        case "update_subscription":
            s.updateSubscriptionUsage(ctx, task.UserID, task.GroupID, task.Amount)
        }

        cancel()
    }
}
```

**Redis 原子扣费脚本**：

```go
// Lua 脚本保证原子性
const deductBalanceScript = `
local key = KEYS[1]
local amount = tonumber(ARGV[1])

-- 获取当前余额
local balance = tonumber(redis.call('GET', key) or 0)

-- 检查余额是否足够
if balance < amount then
    return -1  -- 余额不足
end

-- 扣除余额
local newBalance = balance - amount
redis.call('SET', key, newBalance)
redis.call('EXPIRE', key, 300)  -- 5 分钟 TTL

return newBalance
`

func (s *BillingCacheService) DeductBalanceCache(
    ctx context.Context,
    userID int64,
    amount float64,
) error {
    key := fmt.Sprintf("billing:balance:%d", userID)

    result, err := s.redis.Eval(ctx, deductBalanceScript,
        []string{key}, amount).Float64()

    if result == -1 {
        return ErrInsufficientBalance
    }

    return nil
}
```

**学生**：我明白了！先从 Redis 缓存中扣费（快速），然后异步写入数据库（持久化）！

**教授**：完全正确！这就是**最终一致性**的应用。但这里有个风险：如果 Redis 宕机了怎么办？

**学生**：那缓存中的余额就丢失了...

**教授**：所以 Sub2API 有**熔断机制**：

```go
// backend/internal/service/billing_cache_service.go

type CircuitBreaker struct {
    failureCount    int32
    lastFailureTime time.Time
    state           string  // "closed" | "open" | "half-open"
    threshold       int     // 失败阈值
    timeout         time.Duration
}

func (s *BillingCacheService) CheckBillingEligibility(
    ctx context.Context,
    userID int64,
    estimatedCost float64,
) error {
    // 如果熔断器打开，直接查数据库
    if s.circuitBreaker.IsOpen() {
        return s.checkBalanceFromDB(ctx, userID, estimatedCost)
    }

    // 尝试从缓存检查
    err := s.checkBalanceFromCache(ctx, userID, estimatedCost)
    if err != nil {
        // 记录失败
        s.circuitBreaker.RecordFailure()

        // 降级到数据库
        return s.checkBalanceFromDB(ctx, userID, estimatedCost)
    }

    return nil
}
```

---

## 第八讲：数据库设计与 Ent ORM

**教授**：现在让我们深入看看 Sub2API 的数据库设计。这是一个非常精心设计的关系型数据模型。

**学生**：我很好奇，Sub2API 有哪些核心实体？

**教授**：让我画一个实体关系图：

### 核心实体关系图

```
┌─────────────┐
│    User     │ 用户
│─────────────│
│ id          │
│ email       │
│ password    │
│ balance     │ 余额
│ concurrency │ 并发限制
│ role        │ 角色（admin/user）
└──────┬──────┘
       │ 1:N
       ↓
┌─────────────┐
│   APIKey    │ API 密钥
│─────────────│
│ id          │
│ key         │ sk-xxx
│ user_id     │
│ group_id    │ 可选：绑定到分组
│ quota       │ 配额限制
│ quota_used  │ 已使用配额
│ expires_at  │ 过期时间
└──────┬──────┘
       │ N:1
       ↓
┌─────────────┐
│    Group    │ 服务分组
│─────────────│
│ id          │
│ name        │
│ platform    │ claude/openai/gemini
│ rate_multiplier │ 费率倍数
└──────┬──────┘
       │ N:M
       ↓
┌─────────────┐
│   Account   │ 上游账号
│─────────────│
│ id          │
│ name        │
│ platform    │
│ type        │ oauth/apikey
│ credentials │ JSONB（存储 token/key）
│ concurrency │ 并发限制
│ priority    │ 优先级
│ schedulable │ 是否可调度
└─────────────┘

┌─────────────┐
│  UsageLog   │ 使用日志（不可变）
│─────────────│
│ id          │
│ user_id     │
│ api_key_id  │
│ account_id  │
│ model       │
│ input_tokens│
│ output_tokens│
│ total_cost  │
│ actual_cost │
│ created_at  │
└─────────────┘
```

**学生**：我注意到 Account 的 credentials 是 JSONB 类型，为什么不用单独的字段？

**教授**：非常好的问题！因为不同类型的账号需要存储的信息完全不同：

**OAuth 账号**：
```json
{
  "access_token": "eyJhbGc...",
  "refresh_token": "def502...",
  "expires_at": "2024-12-31T23:59:59Z",
  "session_key": "sk-ant-sid01-..."
}
```

**API Key 账号**：
```json
{
  "api_key": "sk-ant-api03-..."
}
```

**Setup Token 账号**：
```json
{
  "setup_token": "setup_xxx",
  "organization_id": "org_xxx"
}
```

使用 JSONB 可以灵活存储不同结构的数据，而且 PostgreSQL 的 JSONB 支持索引和查询。

**学生**：那 Ent ORM 是如何定义这些实体的？

**教授**：让我展示一个完整的 Schema 定义：

### Ent Schema 示例

```go
// backend/ent/schema/api_key.go

package schema

import (
    "entgo.io/ent"
    "entgo.io/ent/schema/edge"
    "entgo.io/ent/schema/field"
    "entgo.io/ent/schema/index"
)

type APIKey struct {
    ent.Schema
}

// Fields 定义字段
func (APIKey) Fields() []ent.Field {
    return []ent.Field{
        field.Int64("id"),
        field.String("key").Unique().NotEmpty(),  // API Key（唯一）
        field.String("name").Optional(),          // 名称
        field.Int64("user_id"),                   // 所属用户
        field.Int64("group_id").Optional(),       // 所属分组（可选）

        // 配额管理
        field.Float("quota").Default(0),          // 配额限制（USD）
        field.Float("quota_used").Default(0),     // 已使用配额

        // 速率限制（三个时间窗口）
        field.Float("rate_limit_5h").Default(0),  // 5 小时限制
        field.Float("rate_limit_1d").Default(0),  // 1 天限制
        field.Float("rate_limit_7d").Default(0),  // 7 天限制
        field.Float("rate_limit_5h_used").Default(0),
        field.Float("rate_limit_1d_used").Default(0),
        field.Float("rate_limit_7d_used").Default(0),
        field.Time("window_5h_start").Optional(),
        field.Time("window_1d_start").Optional(),
        field.Time("window_7d_start").Optional(),

        // IP 限制
        field.JSON("ip_whitelist", []string{}).Optional(),
        field.JSON("ip_blacklist", []string{}).Optional(),

        // 状态管理
        field.String("status").Default("active"),  // active/disabled/expired
        field.Time("expires_at").Optional(),       // 过期时间
        field.Time("last_used_at").Optional(),     // 最后使用时间

        // 软删除
        field.Time("deleted_at").Optional(),

        // 时间戳
        field.Time("created_at").Immutable().Default(time.Now),
        field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
    }
}

// Edges 定义关系
func (APIKey) Edges() []ent.Edge {
    return []ent.Edge{
        // 多对一：APIKey → User
        edge.From("user", User.Type).
            Ref("api_keys").
            Field("user_id").
            Required().
            Unique(),

        // 多对一：APIKey → Group（可选）
        edge.From("group", Group.Type).
            Ref("api_keys").
            Field("group_id").
            Unique(),

        // 一对多：APIKey → UsageLog
        edge.To("usage_logs", UsageLog.Type),
    }
}

// Indexes 定义索引
func (APIKey) Indexes() []ent.Index {
    return []ent.Index{
        // 唯一索引：key（排除软删除）
        index.Fields("key").Unique().
            Annotations(entsql.IndexWhere("deleted_at IS NULL")),

        // 复合索引：user_id + status（查询用户的活跃 key）
        index.Fields("user_id", "status"),

        // 单字段索引
        index.Fields("group_id"),
        index.Fields("expires_at"),
        index.Fields("last_used_at"),
    }
}
```

**学生**：我看到有个 `Immutable()` 修饰符，这是什么意思？

**教授**：`Immutable()` 表示字段创建后不可修改。比如 `created_at` 应该永远保持创建时的时间戳，不应该被更新。

现在让我展示 Ent 生成的代码是如何使用的：

### Ent 查询示例

```go
// 1. 创建 API Key
apiKey, err := client.APIKey.Create().
    SetKey("sk-user-abc123").
    SetName("My API Key").
    SetUserID(userID).
    SetGroupID(groupID).
    SetQuota(100.0).
    SetStatus("active").
    Save(ctx)

// 2. 查询用户的所有活跃 API Keys（带预加载）
apiKeys, err := client.APIKey.Query().
    Where(
        apikey.UserIDEQ(userID),
        apikey.StatusEQ("active"),
        apikey.DeletedAtIsNil(),  // 排除软删除
    ).
    WithUser().   // 预加载 User 关系
    WithGroup().  // 预加载 Group 关系
    Order(ent.Desc(apikey.FieldCreatedAt)).
    All(ctx)

// 3. 更新配额使用量（原子操作）
err = client.APIKey.UpdateOneID(apiKeyID).
    AddQuotaUsed(cost).  // 原子递增
    SetLastUsedAt(time.Now()).
    Save(ctx)

// 4. 软删除
err = client.APIKey.UpdateOneID(apiKeyID).
    SetDeletedAt(time.Now()).
    Save(ctx)

// 5. 复杂查询：查询即将过期的 API Keys
expiringKeys, err := client.APIKey.Query().
    Where(
        apikey.StatusEQ("active"),
        apikey.ExpiresAtNotNil(),
        apikey.ExpiresAtLT(time.Now().Add(7*24*time.Hour)),  // 7 天内过期
    ).
    WithUser().
    All(ctx)

// 6. 聚合查询：统计用户的总配额使用量
var result []struct {
    UserID    int64   `json:"user_id"`
    TotalUsed float64 `json:"total_used"`
}

err = client.APIKey.Query().
    Where(apikey.UserIDEQ(userID)).
    GroupBy(apikey.FieldUserID).
    Aggregate(ent.Sum(apikey.FieldQuotaUsed)).
    Scan(ctx, &result)
```

**学生**：太强大了！Ent 生成的代码完全类型安全，而且支持复杂查询！

**教授**：是的！而且 Ent 还有一个杀手级特性：**自动迁移**。

### 数据库迁移

```go
// backend/internal/repository/ent.go

func InitEnt(config *config.Config) (*ent.Client, error) {
    // 1. 连接数据库
    dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
        config.Database.Host,
        config.Database.Port,
        config.Database.User,
        config.Database.Password,
        config.Database.DBName,
        config.Database.SSLMode,
    )

    client, err := ent.Open("postgres", dsn)
    if err != nil {
        return nil, err
    }

    // 2. 自动迁移（开发环境）
    if config.Server.Mode == "debug" {
        if err := client.Schema.Create(ctx); err != nil {
            return nil, err
        }
    }

    // 3. 手动迁移（生产环境）
    if config.Server.Mode == "release" {
        // 读取并执行 migrations/ 目录下的 SQL 文件
        if err := runMigrations(client); err != nil {
            return nil, err
        }
    }

    return client, nil
}
```

**学生**：为什么生产环境不用自动迁移？

**教授**：因为自动迁移可能会：
1. **丢失数据**：删除字段时数据会丢失
2. **锁表**：大表的 ALTER TABLE 会锁表很久
3. **不可控**：无法审查迁移 SQL

所以生产环境使用手动迁移脚本，可以精确控制每一步。

---

## 第九讲：前端架构与状态管理

**教授**：现在让我们看看前端部分。Sub2API 使用 Vue 3 + Pinia 构建了一个现代化的单页应用。

**学生**：我对 Pinia 不太熟悉，它和 Vuex 有什么区别？

**教授**：Pinia 是 Vue 官方推荐的新一代状态管理库，比 Vuex 更简洁。让我展示一个实际的 Store：

### Pinia Store 示例

```typescript
// frontend/src/stores/auth.ts

import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import * as authAPI from '@/api/auth'

export const useAuthStore = defineStore('auth', () => {
  // State（使用 ref）
  const user = ref<User | null>(null)
  const token = ref<string | null>(localStorage.getItem('access_token'))
  const refreshToken = ref<string | null>(localStorage.getItem('refresh_token'))

  // Getters（使用 computed）
  const isAuthenticated = computed(() => !!token.value)
  const isAdmin = computed(() => user.value?.role === 'admin')

  // Actions（普通函数）
  async function login(email: string, password: string) {
    const response = await authAPI.login({ email, password })

    if (response.requires_totp) {
      // 需要 2FA
      return { requiresTOTP: true, tempToken: response.temp_token }
    }

    // 保存 token
    token.value = response.access_token
    refreshToken.value = response.refresh_token
    localStorage.setItem('access_token', response.access_token)
    localStorage.setItem('refresh_token', response.refresh_token)

    // 加载用户信息
    await fetchUser()

    return { requiresTOTP: false }
  }

  async function fetchUser() {
    const userData = await authAPI.getCurrentUser()
    user.value = userData
  }

  function logout() {
    user.value = null
    token.value = null
    refreshToken.value = null
    localStorage.clear()
  }

  // 自动刷新用户信息（每 60 秒）
  let refreshInterval: number | null = null
  function startAutoRefresh() {
    refreshInterval = setInterval(() => {
      if (isAuthenticated.value) {
        fetchUser()
      }
    }, 60000)
  }

  function stopAutoRefresh() {
    if (refreshInterval) {
      clearInterval(refreshInterval)
    }
  }

  return {
    // State
    user,
    token,
    refreshToken,

    // Getters
    isAuthenticated,
    isAdmin,

    // Actions
    login,
    logout,
    fetchUser,
    startAutoRefresh,
    stopAutoRefresh,
  }
})
```

**学生**：这比 Vuex 简洁多了！不需要 mutations 和 actions 的区分。

**教授**：是的！Pinia 使用 Composition API 风格，更符合 Vue 3 的设计理念。

现在让我们看看前端的路由保护：

### 路由守卫

```typescript
// frontend/src/router/index.ts

import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      component: () => import('@/views/LoginView.vue'),
      meta: { requiresAuth: false }
    },
    {
      path: '/dashboard',
      component: () => import('@/views/DashboardView.vue'),
      meta: { requiresAuth: true }
    },
    {
      path: '/admin',
      component: () => import('@/views/admin/DashboardView.vue'),
      meta: { requiresAuth: true, requiresAdmin: true }
    },
  ]
})

// 全局前置守卫
router.beforeEach((to, from, next) => {
  const authStore = useAuthStore()

  // 1. 检查是否需要认证
  if (to.meta.requiresAuth && !authStore.isAuthenticated) {
    // 保存目标路由，登录后跳转
    next({
      path: '/login',
      query: { redirect: to.fullPath }
    })
    return
  }

  // 2. 检查是否需要管理员权限
  if (to.meta.requiresAdmin && !authStore.isAdmin) {
    next('/dashboard')  // 重定向到普通用户页面
    return
  }

  next()
})

export default router
```

---

## 第十讲：性能优化与最佳实践

**教授**：最后，让我们总结一下 Sub2API 中使用的性能优化技术。

### 性能优化清单

**1. 多级缓存**
```
L1（进程内存）→ L2（Redis）→ L3（PostgreSQL）
```

**2. 连接池**
```go
// PostgreSQL 连接池
db.SetMaxOpenConns(100)      // 最大连接数
db.SetMaxIdleConns(10)       // 最大空闲连接
db.SetConnMaxLifetime(1*time.Hour)  // 连接最大生命周期
```

**3. Singleflight（防缓存击穿）**
```go
result, err, _ := sf.Do(key, func() (interface{}, error) {
    return fetchFromDB(key)
})
```

**4. 异步处理**
```go
// 异步更新最后使用时间
go service.TouchLastUsed(apiKeyID)
```

**5. 批量操作**
```go
// 批量查询负载信息
loadInfos := concurrencyService.GetBatchLoadInfo(accountIDs)
```

**6. 索引优化**
```sql
-- 复合索引（查询用户的使用日志）
CREATE INDEX idx_usage_log_user_created ON usage_logs(user_id, created_at DESC);

-- 部分索引（只索引活跃的 API Key）
CREATE UNIQUE INDEX idx_api_key_key ON api_keys(key) WHERE deleted_at IS NULL;
```

**7. 流式处理**
```go
// 不等待完整响应，逐块转发
scanner := bufio.NewScanner(response.Body)
for scanner.Scan() {
    clientWriter.Write(scanner.Bytes())
    flusher.Flush()
}
```

---

## 总结与思考

**教授**：同学们，我们用了十讲的时间，深入学习了 Sub2API 这个项目。让我们回顾一下核心要点：

### 核心技术要点

1. **分层架构**：Handler → Service → Repository → ORM
2. **依赖注入**：Wire 编译时 DI，零运行时开销
3. **双重认证**：JWT（管理后台）+ API Key（程序调用）
4. **多级缓存**：L1（内存）+ L2（Redis）+ L3（数据库）
5. **粘性会话**：保持对话上下文的连续性
6. **负载均衡**：基于优先级、负载、错误率的智能调度
7. **并发控制**：用户级 + 账号级的两级限制
8. **流式转发**：实时转发 SSE 响应
9. **协议适配**：统一 Anthropic、OpenAI、Gemini 三种格式
10. **精确计费**：Token 级别的成本计算和余额扣除
11. **事务保证**：使用数据库事务保证计费原子性
12. **类型安全**：Ent ORM 自动生成类型安全的代码

**学生**：教授，我有个最后的问题：如果让我从零开始设计一个类似的系统，应该从哪里入手？

**教授**：非常好的问题！让我给你一个实施路线图：

### 实施路线图

**阶段 1：MVP（最小可行产品）**
- [ ] 基础认证（JWT + API Key）
- [ ] 单一上游账号转发
- [ ] 简单的 Token 统计
- [ ] 基础的余额扣除

**阶段 2：核心功能**
- [ ] 多账号管理
- [ ] 粘性会话
- [ ] 负载均衡
- [ ] 并发控制

**阶段 3：性能优化**
- [ ] 多级缓存
- [ ] 异步计费
- [ ] 连接池优化
- [ ] 索引优化

**阶段 4：高级特性**
- [ ] 协议适配（多种 API 格式）
- [ ] 订阅系统
- [ ] 管理后台
- [ ] 监控告警

**学生**：谢谢教授！这个项目让我学到了很多实战技术！

**教授**：很高兴你有收获！记住，优秀的系统设计不是一蹴而就的，而是在不断迭代中逐步完善的。Sub2API 也是经过多次重构才达到现在的架构。

最后，我想强调几个工程实践的原则：

### 工程实践原则

1. **先保证正确性，再优化性能**：不要过早优化
2. **使用事务保证数据一致性**：宁可慢一点，也不能错
3. **多级缓存要有降级策略**：缓存失败时能回退到数据库
4. **异步处理要有失败重试**：网络不可靠，要做好容错
5. **日志和监控很重要**：出问题时能快速定位
6. **代码要有测试覆盖**：单元测试 + 集成测试
7. **文档要及时更新**：代码会说谎，文档不能

**学生们**：谢谢教授！

**教授**：下课！

---

## 附录：关键代码文件索引

### 后端核心文件

**认证与授权**
- `backend/internal/server/middleware/api_key_auth.go` - API Key 认证
- `backend/internal/server/middleware/jwt_auth.go` - JWT 认证
- `backend/internal/service/api_key_auth_cache_impl.go` - API Key 多级缓存

**网关核心**
- `backend/internal/service/gateway_service.go` - 网关主服务（287KB）
- `backend/internal/service/openai_account_scheduler.go` - 账号调度器
- `backend/internal/service/concurrency_service.go` - 并发控制
- `backend/internal/service/gateway_request.go` - 请求解析

**计费系统**
- `backend/internal/service/billing_service.go` - 计费服务
- `backend/internal/service/usage_service.go` - 使用日志
- `backend/internal/service/billing_cache_service.go` - 计费缓存

**数据访问**
- `backend/ent/schema/*.go` - 数据库 Schema 定义
- `backend/internal/repository/*.go` - Repository 层

**服务初始化**
- `backend/cmd/server/main.go` - 程序入口
- `backend/cmd/server/wire.go` - 依赖注入定义
- `backend/internal/server/router.go` - 路由配置

### 前端核心文件

**状态管理**
- `frontend/src/stores/auth.ts` - 认证状态
- `frontend/src/stores/app.ts` - 全局状态

**API 客户端**
- `frontend/src/api/client.ts` - Axios 配置
- `frontend/src/api/auth.ts` - 认证 API
- `frontend/src/api/keys.ts` - API Key 管理

**路由与视图**
- `frontend/src/router/index.ts` - 路由配置
- `frontend/src/views/` - 页面组件

---

**教案完**

本教案涵盖了 Sub2API 项目的所有核心原理和实现细节，从架构设计到代码实现，从性能优化到工程实践。希望能帮助学生深入理解现代 Web 应用的设计与开发。

