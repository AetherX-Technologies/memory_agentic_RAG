# HybridMem-RAG 项目指南

> 本项目采用 Vibe Coding 2.0 开发范式
> 项目类型：Go 语言重构（TypeScript Memory LanceDB Pro → Go 纯实现）
> 目标：跨平台个人知识库（桌面 + iOS/Android）

---

## 项目定位

这是一个**重构项目**，将 TypeScript 实现的 Memory LanceDB Pro 改造为纯 Go 实现，以支持真正的跨平台部署（包括 iOS 离线使用）。

**核心目标**：
- 功能对标：实现 Memory LanceDB Pro 的所有核心功能
- 技术升级：使用纯 Go + SQLite 替代 LanceDB（CGO）
- 跨平台：支持 Windows/macOS/Linux/Android/iOS

---

## Vibe Coding 规则

### 1. 渐进式读取（Progressive Disclosure）

**默认行为**：
- 启动任务时，先读取 `docs/PRD.md` 了解需求
- 需要架构信息时，先读取 `docs/architecture/INDEX.md`
- 根据当前任务，只加载相关的架构文档

**避免**：一次性加载所有文档

### 2. 计划持久化（Plan Persistence）

**默认行为**：
- 阅读 PRD 后，主动创建 `.context/plan.md`
- 将需求拆解为工程任务，使用状态标记：✅ 🚧 ⏳ ⚠️
- 每完成一个模块，主动更新 `plan.md`

### 3. 观测遮蔽（Observation Masking）

**默认行为**：
- 超过 50 行的输出重定向到 `.context/scratchpads/`
- 文件命名：`类型-日期-时间-描述.ext`
- 对话中只返回摘要 + 文件路径

### 4. 追加式记忆（Append-Only Memory）

**触发条件**（记录到 `.context/memory.jsonl`）：
- 重大技术决策
- 关键配置变更
- 棘手 Bug 的解决方案

**格式**：
```jsonl
{"date": "2024-03-13", "type": "decision", "tags": ["#database"], "summary": "使用 SQLite + 纯 Go", "files": ["internal/store/sqlite.go"]}
```

---

## 项目特定规则

### 5. 参考源码使用

**主要参考**：`../memory-lancedb-pro-main`（只读，不修改）

**禁止**：修改参考项目的任何文件

### 6. 代码质量标准

- 所有导出函数必须有注释
- 关键算法必须有单元测试
- 性能关键路径必须有 benchmark

**性能目标**：
- 10000 条记忆检索 < 50ms
- 混合检索 < 200ms

### 7. 开发流程

1. 读取 `.context/plan.md` 确认当前任务
2. 读取相关架构文档
3. 读取参考源码
4. 实现功能 + 单元测试
5. 运行测试，输出到 scratchpads
6. 更新 `plan.md` 状态
7. 记录关键决策到 `memory.jsonl`

---

## 文档结构

```
memory_agentic_RAG/
├── CLAUDE.md                    # 本文件
├── .context/                    # AI 工作区
│   ├── plan.md                  # 任务计划
│   ├── memory.jsonl             # 关键决策
│   └── scratchpads/             # 长输出
├── docs/                        # 设计文档
│   ├── PRD.md                   # 产品需求
│   ├── architecture/            # 架构设计
│   │   ├── INDEX.md
│   │   ├── data-model.md
│   │   ├── retrieval-engine.md
│   │   └── api-design.md
│   └── references/              # 参考资料
└── src/                         # 源代码
```

---

## 快速启动

**首次启动**：
```bash
cat docs/PRD.md
cat docs/architecture/INDEX.md
cat .context/plan.md
```

**恢复会话**：
```bash
tail -n 10 .context/memory.jsonl
grep "🚧" .context/plan.md
```

---

**最后更新**：2024-03-13
**项目状态**：初始化阶段
