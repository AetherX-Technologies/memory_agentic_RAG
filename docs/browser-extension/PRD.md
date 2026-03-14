# 浏览器插件 PRD

## 产品定位

HybridMem-RAG 浏览器插件，自动捕捉 AI 对话并存储到本地知识库。

## 核心功能

### 1. 自动捕捉对话
- 支持平台：ChatGPT、Claude、Gemini
- 实时捕捉用户提问和 AI 回复
- 自动提取关键信息

### 2. 智能存储
- 连接本地 HybridMem-RAG 服务
- 自动向量化和存储
- 分类标记：
  - **fact**: 客观事实、技术知识、代码片段
  - **preference**: 用户偏好、习惯、选择
  - **insight**: AI 生成的洞见、总结、建议
- 重要性评分（0-1）：
  - 基础分: 0.5
  - 包含代码块（3个反引号或 `<pre><code>` 且 ≥50字符）: +0.2
  - 消息长度 > 500 字符: +0.1
  - AI 回复（非用户消息）: +0.1
  - 最终分数取 min(1.0, 累加值)
  - 示例：AI回复 + 代码块 + 长文本 = 0.5 + 0.1 + 0.2 + 0.1 = 0.9

### 3. 快速检索
- 弹窗界面快速搜索
- 跨平台统一检索
- 相关性排序

### 4. 上下文增强
- 自动提供相关历史记忆
- 帮助 AI 理解上下文

## 技术要求

- Chrome Extension Manifest V3
- 最低版本：Chrome 88+ / Edge 88+ (Manifest V3 支持)
- 连接本地 HTTP API (localhost:8080)
- 支持 Chrome/Edge 浏览器

**性能目标**：
- 消息捕捉延迟 < 100ms
- API 存储请求 < 500ms（请求超时：10秒）
- Popup 搜索响应 < 300ms

**速率限制**：
- 最大 10 请求/秒（滑动窗口）
- 最大 1000 请求/小时（滑动窗口）
- 突发允许：20 请求

**后台任务**：
- 健康检查间隔：60 秒（chrome.alarms 最小间隔）

## 向量化策略

**当前状态**: 服务端向量化功能待实现

插件设计假设服务端自动生成 embedding：

1. 插件捕捉对话文本
2. 发送纯文本到 `POST /api/memories`
3. **服务端需实现**：接收文本后调用 Jina API 生成向量
4. 服务端存储文本 + 向量

**实现要求**：
- 服务端需修改 `CreateMemory` 处理器
- 当请求中 `vector` 字段为空时，自动调用 embedding 服务
- 使用 Jina Embeddings API 生成 1024 维向量

**优势**：
- 插件轻量，无需集成 embedding 库
- API key 安全存储在服务端
- 统一向量化策略，保证一致性

## 错误处理

### 离线场景
- 插件检测到服务不可达时，暂存对话到 IndexedDB
- 服务恢复后自动重试上传
- 最多缓存 100 条对话

**IndexedDB 配置**：
- Database: `hybridmem-cache`
- Store: `conversations`
- Key: `id` (UUID)

**IndexedDB Schema**：
```typescript
interface CachedConversation {
  id: string              // UUID
  text: string            // 对话内容
  category: string        // fact/preference/insight
  scope: string           // browser:domain
  importance: number      // 0-1
  metadata: object        // 元数据
  timestamp: number       // 创建时间戳
  retryCount: number      // 重试次数（最多3次）
}
```

**重试策略**：
- 最多重试 3 次
- 超过 3 次后标记为失败，保留在缓存中供手动重试
- 成功后从 IndexedDB 删除

**缓存管理**：
- 达到 100 条时 FIFO 淘汰
- 失败项（3次重试后）保留 7 天
- 设置页面提供"清空失败项"按钮

### 连接失败
- 3 次重试（间隔 1s/2s/5s）
- 失败后显示通知（chrome.notifications API，10秒自动关闭）
- 提供手动重试按钮

### API 错误
- **400 错误**：记录日志，跳过该条对话
- **500 错误**：触发连接失败重试机制（1s/2s/5s）
- **网络错误**：视为连接失败，触发重试并缓存到 IndexedDB

## 安全考虑

- **本地通信**：仅连接 localhost，不发送数据到外部服务器
- **权限声明**：
  - `storage`: 离线缓存
  - `notifications`: 错误通知
  - `host_permissions`:
    - `https://chatgpt.com/*`
    - `https://claude.ai/*`
    - `https://gemini.google.com/*`
    - `http://localhost/*`
    - `http://127.0.0.1/*`
- **数据隔离**：不同 AI 平台的对话使用不同 scope 标记
- **用户控制**：提供开关按钮，用户可随时暂停捕捉

## 用户场景

1. 用户在 ChatGPT 对话
2. 插件自动捕捉对话内容
3. 后台调用本地 API 存储
4. 用户可随时搜索历史对话

## API 契约

### 健康检查
```
GET http://localhost:8080/api/health

Response 200:
{
  "status": "ok",
  "version": "1.0.0"
}
```

**版本协商**：
- 插件检查 `/api/health` 的 `version` 字段
- 最低服务端版本：1.0.0
- 服务端版本 < 1.0.0 时显示警告

### 存储对话
```
POST http://localhost:8080/api/memories
Content-Type: application/json

Request:
{
  "text": "对话内容",
  "category": "fact",
  "scope": "browser:chatgpt.com",
  "importance": 0.7,
  "metadata": "{\"source\":\"browser-plugin\",\"url\":\"https://chatgpt.com/...\",\"role\":\"assistant\",\"platform\":\"chatgpt\",\"messageId\":\"abc123\"}"
}

**Metadata 格式**：
- 类型：JSON 字符串（非对象）
- 必需字段（序列化为 JSON 字符串）：
  - `source`: "browser-plugin"
  - `url`: 有效的 HTTP(S) URL，最大 2048 字符
  - `role`: "user" | "assistant"
  - `platform`: "chatgpt" | "claude" | "gemini"
  - `messageId`: 字符串（1-255字符，字母数字+连字符），由去重策略生成

**Scope 格式**：`browser:<domain>`
- 提取规则：使用主域名（eTLD+1）
- 示例：
  - `https://chatgpt.com/c/xxx` → `browser:chatgpt.com`
  - `https://chat.openai.com/xxx` → `browser:openai.com`
  - `https://claude.ai/chat/xxx` → `browser:claude.ai`
- 边界情况：
  - Localhost/IP: `browser:localhost`
  - 自定义端口: 忽略端口号
  - 子域名: 始终使用 eTLD+1

Response 201:
- 提取规则：使用主域名（eTLD+1）
- 示例：
  - `https://chatgpt.com/c/xxx` → `browser:chatgpt.com`
  - `https://chat.openai.com/xxx` → `browser:openai.com`
  - `https://claude.ai/chat/xxx` → `browser:claude.ai`
- 边界情况：
  - Localhost/IP: `browser:localhost`
  - 自定义端口: 忽略端口号
  - 子域名: 始终使用 eTLD+1

**Metadata 字段**：
- `source` (必需): 固定值 "browser-plugin"
- `url` (必需): 有效的 HTTP(S) URL，最大 2048 字符
- `role` (必需): enum ["user", "assistant"]
- `platform` (必需): enum ["chatgpt", "claude", "gemini"]（与支持平台列表一致）
- `messageId` (必需): 字符串（1-255字符，字母数字+连字符），由去重策略生成

Response 201:
{ "id": "uuid" }

Response 400:
{ "error": "invalid request body" }

Response 500:
{ "error": "error message" }
```

**注意**：vector 字段由服务端自动生成，插件无需提供。

### 检索对话
```
GET http://localhost:8080/api/memories/search?q=查询&limit=10&offset=0

Query Parameters:
- q: string (必需, 1-500字符)
- limit: number (可选, 默认10, 最大50)
- offset: number (可选, 默认0)

Response 200:
[
  {
    "entry": { "id": "uuid", "text": "...", ... },
    "score": 0.95
  }
]
```
