# MCP Server 实现方案

## 一、MCP 协议简介

Model Context Protocol (MCP) 是 Anthropic 推出的标准协议，用于 AI 应用与外部工具/数据源的集成。

### MCP 三大能力

1. **Tools**：函数调用，让 AI 执行操作
2. **Resources**：数据源，让 AI 读取信息
3. **Prompts**：提示词模板，预定义的交互模式

---

## 二、HybridMem-RAG MCP Server 设计

### 2.1 暴露的 Tools

```go
package mcp

type MemoryTools struct {
    ingestSvc   *service.IngestService
    retrievalSvc *service.RetrievalService
}

// Tool 1: memory_store - 存储记忆
func (m *MemoryTools) MemoryStore() Tool {
    return Tool{
        Name: "memory_store",
        Description: "存储新的记忆到知识库",
        InputSchema: Schema{
            Type: "object",
            Properties: map[string]Property{
                "text": {
                    Type: "string",
                    Description: "要存储的文本内容",
                },
                "category": {
                    Type: "string",
                    Enum: []string{"fact", "preference", "decision", "entity"},
                    Description: "记忆类型",
                },
            },
            Required: []string{"text"},
        },
    }
}

// Tool 2: memory_recall - 检索记忆
func (m *MemoryTools) MemoryRecall() Tool {
    return Tool{
        Name: "memory_recall",
        Description: "从知识库检索相关记忆",
        InputSchema: Schema{
            Type: "object",
            Properties: map[string]Property{
                "query": {
                    Type: "string",
                    Description: "检索查询",
                },
                "limit": {
                    Type: "integer",
                    Description: "返回结果数量",
                    Default: 5,
                },
            },
            Required: []string{"query"},
        },
    }
}

// Tool 3: memory_consolidate - 手动触发整合
func (m *MemoryTools) MemoryConsolidate() Tool {
    return Tool{
        Name: "memory_consolidate",
        Description: "手动触发记忆整合，发现关联和洞见",
        InputSchema: Schema{
            Type: "object",
            Properties: map[string]Property{},
        },
    }
}
```

### 2.2 MCP Server 启动

```go
package main

import (
    "context"
    "encoding/json"
    "os"
)

func serveMCP(ctx context.Context) error {
    server := mcp.NewServer("memory-rag", "1.0.0")

    // 注册 tools
    tools := &mcp.MemoryTools{
        ingestSvc:   ingestService,
        retrievalSvc: retrievalService,
    }

    server.RegisterTool(tools.MemoryStore())
    server.RegisterTool(tools.MemoryRecall())
    server.RegisterTool(tools.MemoryConsolidate())

    // 通过 stdio 通信
    return server.ServeStdio(ctx)
}
```

### 2.3 CLI 工具配置

用户在 Claude Desktop 或其他 MCP 客户端中配置：

```json
{
  "mcpServers": {
    "memory-rag": {
      "command": "/usr/local/bin/memory-rag",
      "args": ["serve", "--mcp"],
      "env": {
        "DB_HOST": "localhost",
        "DB_PORT": "5432"
      }
    }
  }
}
```

---

## 三、使用示例

### 在 Claude Desktop 中使用

用户：记住，我喜欢简洁的代码风格

Claude：<uses memory_store tool>
```json
{
  "text": "用户喜欢简洁的代码风格",
  "category": "preference"
}
```

---

用户：我之前说过什么代码风格偏好？

Claude：<uses memory_recall tool>
```json
{
  "query": "代码风格偏好",
  "limit": 3
}
```

返回：
```json
[
  {
    "memory": {
      "summary": "用户喜欢简洁的代码风格",
      "importance": 0.8
    },
    "score": 0.92
  }
]
```

