# 浏览器插件架构设计

## 整体架构

```
浏览器插件 (子)
├── Content Script (注入网页)
├── Background Service Worker (后台)
└── Popup UI (弹窗)
        ↓ HTTP API
本地服务 (母)
└── HybridMem-RAG Server (localhost:8080)
```

## 核心组件

### 1. Content Script
- 注入到 AI 网站页面
- 使用 MutationObserver 监听 DOM 变化
- 捕捉新消息并提取内容
- 发送到 Background

**MutationObserver 配置**：
```javascript
observer = new MutationObserver(callback)
observer.observe(targetNode, {
  childList: true,
  subtree: true
})
```

**去重策略**：
1. 优先使用平台特定 ID 属性（如 `data-message-id`）
2. 回退到内容哈希：`hash(role + text前100字符 + 页面URL)`
3. 维护已处理消息的 Map（ID → timestamp），TTL 1小时
4. 页面刷新时清空 Map

**文本提取规则**：
- 保留 Markdown 格式（代码块、列表等）
- 流式响应：等待 DOM 停止变化 2 秒后捕捉
- 检测生成指示器（如 "Generating..."）消失
- 多部分消息：合并为单条记忆

**平台选择器**：
- **ChatGPT**: `div[data-message-author-role]`
  - 用户消息: `[data-message-author-role="user"]`
  - AI 回复: `[data-message-author-role="assistant"]`
- **Claude**: `.font-claude-message`
  - 用户消息: 父元素包含 `data-is-user-message="true"`
  - AI 回复: 父元素包含 `data-is-user-message="false"`
- **Gemini**: `message-content`
  - 用户消息: `.user-message`
  - AI 回复: `.model-response`

### 2. Background Service Worker
- 接收 Content Script 消息
- 调用本地 API 存储记忆（服务端负责向量化）
- 管理插件状态
- 处理离线缓存和重试

**错误处理策略**：
- **429 (Rate Limit)**: 指数退避重试（1min → 2min → 5min），最多3次
- **503 (Service Unavailable)**: 立即缓存到 IndexedDB，30秒后重试
- **400/500**: 记录错误日志，跳过该条消息

### 3. Popup UI
- 搜索界面
- 显示检索结果
- 设置页面

**界面流程**：
1. **主界面**：搜索框 + 开关按钮（启用/暂停捕捉）
2. **搜索结果**：列表显示匹配的对话，点击复制到剪贴板
3. **设置页面**：
   - 服务地址配置（默认 localhost:8080）
   - 缓存管理（查看/清空离线缓存）
   - 平台开关（选择监听哪些 AI 平台）

**UI 状态**：
- 🟢 绿色：服务连接正常
- 🟡 黄色：离线模式（使用缓存）
- 🔴 红色：服务不可达且缓存已满

**状态检测**：
- Popup 打开时调用 `/api/health` 检测服务状态
- Background 每 60 秒轮询健康检查（chrome.alarms）
- 状态变化时通过 `chrome.runtime.sendMessage` 通知 Popup 更新

### 4. 适配器系统

**接口定义**：
```typescript
interface PlatformAdapter {
  // 检测当前页面是否匹配该平台
  matches(url: string): boolean

  // 提取消息元素
  detectMessages(): HTMLElement[]

  // 判断消息角色
  extractRole(element: HTMLElement): 'user' | 'assistant'

  // 提取消息文本
  extractText(element: HTMLElement): string
}
```

**实现**：
- **ChatGPTAdapter**: 匹配 `chatgpt.com`
- **ClaudeAdapter**: 匹配 `claude.ai`
- **GeminiAdapter**: 匹配 `gemini.google.com`

## 数据流

```
网页消息 → Content Script → Background
                              ↓
                         本地 API (POST /api/memories)
                              ↓
                         服务端向量化 (Jina API)
                              ↓
                         存储 (SQLite + FTS5)
```

## API 集成

- GET /api/health - 健康检查
- POST /api/memories - 存储对话
- GET /api/memories/search - 检索对话
