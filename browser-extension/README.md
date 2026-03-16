# HybridMem-RAG 浏览器插件

自动捕捉 AI 对话并存储到本地知识库。

## 功能

- ✅ 自动捕捉 ChatGPT/Claude/Gemini 对话
- ✅ 连接本地 HybridMem-RAG 服务
- ✅ 离线缓存和自动重试
- ✅ 健康检查和状态监控

## 安装

1. 确保本地服务运行在 `http://localhost:8080`
2. 打开 Chrome/Edge 扩展管理页面
3. 启用"开发者模式"
4. 点击"加载已解压的扩展程序"
5. 选择 `browser-extension` 目录

## 使用

插件会自动捕捉对话，无需手动操作。点击插件图标查看状态。

## 文件结构

```
browser-extension/
├── manifest.json
├── src/
│   ├── content/          # Content Scripts
│   │   ├── base-adapter.js
│   │   ├── chatgpt.js
│   │   ├── claude.js
│   │   └── gemini.js
│   ├── background/       # Service Worker
│   │   └── service-worker.js
│   └── popup/            # Popup UI
│       ├── popup.html
│       └── popup.js
└── public/               # 图标资源
```

## 开发

修改代码后，在扩展管理页面点击"重新加载"即可。
