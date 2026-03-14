# Release Checklist v1.0

## Pre-Release

- [x] 核心功能完成
  - [x] 存储层（SQLite + FTS5）
  - [x] 向量检索
  - [x] BM25 全文检索
  - [x] RRF 融合
  - [x] Rerank 重排
  - [x] HTTP API

- [x] 测试验证
  - [x] 单元测试通过
  - [x] 真实数据测试
  - [x] 性能基准测试
  - [x] Rerank 对比测试

- [x] 文档完善
  - [x] README.md
  - [x] API.md
  - [x] DEPLOYMENT.md
  - [x] ARCHITECTURE.md
  - [x] PRD.md

- [x] 代码质量
  - [x] 代码审查通过
  - [x] 关键 Bug 修复
  - [x] 性能优化完成

## Release Tasks

- [ ] 版本标记
  ```bash
  git tag -a v1.0.0 -m "Release v1.0.0"
  git push origin v1.0.0
  ```

- [ ] 编译二进制
  ```bash
  # Linux
  GOOS=linux GOARCH=amd64 go build -o dist/hybridmem-server-linux-amd64 cmd/server/main.go

  # macOS
  GOOS=darwin GOARCH=amd64 go build -o dist/hybridmem-server-darwin-amd64 cmd/server/main.go
  GOOS=darwin GOARCH=arm64 go build -o dist/hybridmem-server-darwin-arm64 cmd/server/main.go

  # Windows
  GOOS=windows GOARCH=amd64 go build -o dist/hybridmem-server-windows-amd64.exe cmd/server/main.go
  ```

- [ ] 创建 Release Notes
  - 核心特性列表
  - 性能指标
  - 已知限制
  - 升级指南

- [ ] 发布到 GitHub Releases
  - 上传二进制文件
  - 添加 Release Notes
  - 标记为 Latest Release

## Post-Release

- [ ] 更新文档链接
- [ ] 通知用户
- [ ] 收集反馈

## 已知限制

1. 移动端框架编译需要网络环境（gomobile）
2. 向量化需要外部服务（客户端提供向量）
3. SQLite 适合单机场景（< 100万条）

## 下一版本计划

- [ ] 集成向量化服务（Jina Embeddings）
- [ ] 支持批量导入
- [ ] 添加 Web UI
- [ ] PostgreSQL 支持（大规模场景）
