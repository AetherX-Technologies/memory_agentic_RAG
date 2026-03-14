# HybridMem-RAG 部署指南

## 系统要求

- Go 1.21+
- 磁盘空间：100MB（基础）+ 数据库大小
- 内存：最低 256MB，推荐 512MB+

## 快速启动

### 1. 编译

```bash
# 克隆仓库
git clone <repository-url>
cd memory_agentic_RAG

# 编译服务端
go build -o hybridmem-server cmd/server/main.go

# 编译 CLI 工具
go build -o hybridmem-cli cmd/cli/main.go
```

### 2. 启动服务

```bash
# 使用默认配置（端口 8080，内存数据库）
./hybridmem-server

# 指定数据库路径
./hybridmem-server -db /path/to/data.db

# 指定端口
./hybridmem-server -port 9000
```

### 3. 验证

```bash
curl http://localhost:8080/api/memories/stats
# 预期输出: {"total":0,"by_category":{}}
```

## 配置选项

### 环境变量

```bash
# Jina API Key（启用 Rerank）
export JINA_API_KEY="your-api-key"

# 数据库路径
export DB_PATH="/var/lib/hybridmem/data.db"

# 服务端口
export PORT="8080"
```

### 配置文件

创建 `config.yaml`：

```yaml
database:
  path: "./data.db"

server:
  port: 8080
  max_body_size: 10485760  # 10MB

rerank:
  enabled: true
  provider: "jina"
  api_key: "${JINA_API_KEY}"
  model: "jina-reranker-v2-base-multilingual"
  blend_weight: 0.6
  unreturn_penalty: 0.8
```

启动时加载：

```bash
./hybridmem-server -config config.yaml
```

## 生产部署

### Docker 部署

创建 `Dockerfile`：

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o server cmd/server/main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/server .
EXPOSE 8080
CMD ["./server"]
```

构建并运行：

```bash
docker build -t hybridmem-rag .
docker run -d -p 8080:8080 \
  -v /data:/data \
  -e DB_PATH=/data/hybridmem.db \
  -e JINA_API_KEY=your-key \
  hybridmem-rag
```

### Systemd 服务

创建 `/etc/systemd/system/hybridmem.service`：

```ini
[Unit]
Description=HybridMem-RAG Service
After=network.target

[Service]
Type=simple
User=hybridmem
WorkingDirectory=/opt/hybridmem
ExecStart=/opt/hybridmem/server -db /var/lib/hybridmem/data.db
Restart=on-failure
Environment="JINA_API_KEY=your-key"

[Install]
WantedBy=multi-user.target
```

启动服务：

```bash
sudo systemctl daemon-reload
sudo systemctl enable hybridmem
sudo systemctl start hybridmem
```

## 性能调优

### 数据库优化

```sql
-- 定期执行 VACUUM（回收空间）
VACUUM;

-- 分析查询计划
ANALYZE;
```

### 连接池配置

修改 `internal/store/store.go`：

```go
db.SetMaxOpenConns(25)      // 最大连接数
db.SetMaxIdleConns(5)       // 空闲连接数
db.SetConnMaxLifetime(time.Hour)
```

### 内存限制

```bash
# 限制最大内存使用（Linux）
systemd-run --scope -p MemoryMax=512M ./hybridmem-server
```

## 备份与恢复

### 备份

```bash
# 在线备份（SQLite）
sqlite3 data.db ".backup backup.db"

# 或直接复制（需先停止服务）
cp data.db data.db.backup
```

### 恢复

```bash
# 停止服务
systemctl stop hybridmem

# 恢复数据库
cp backup.db data.db

# 启动服务
systemctl start hybridmem
```

## 监控

### 健康检查

```bash
# 检查服务状态
curl http://localhost:8080/api/memories/stats

# 检查响应时间
time curl -X POST http://localhost:8080/api/memories/search \
  -H "Content-Type: application/json" \
  -d '{"query":"test","limit":10}'
```

### 日志

```bash
# 查看服务日志
journalctl -u hybridmem -f

# 或 Docker 日志
docker logs -f <container-id>
```

## 故障排查

### 问题 1: "no such table: vectors"

**原因**: 数据库未初始化

**解决**:
```bash
# 删除旧数据库
rm data.db

# 重启服务（自动初始化）
./hybridmem-server
```

### 问题 2: Rerank 失败

**原因**: API Key 无效或网络问题

**解决**:
```bash
# 验证 API Key
curl -H "Authorization: Bearer $JINA_API_KEY" \
  https://api.jina.ai/v1/rerank

# 禁用 Rerank（降级）
./hybridmem-server -rerank=false
```

### 问题 3: 性能下降

**原因**: 数据库碎片化

**解决**:
```bash
sqlite3 data.db "VACUUM; ANALYZE;"
```

## 安全建议

1. **API Key 保护**: 使用环境变量，不要硬编码
2. **网络隔离**: 仅在内网暴露服务，或使用反向代理
3. **请求限制**: 配置 rate limiting（可用 nginx）
4. **数据加密**: 敏感数据使用 SQLite 加密扩展

## 升级指南

```bash
# 1. 备份数据
sqlite3 data.db ".backup backup.db"

# 2. 停止服务
systemctl stop hybridmem

# 3. 更新二进制
cp new-server /opt/hybridmem/server

# 4. 启动服务
systemctl start hybridmem

# 5. 验证
curl http://localhost:8080/api/memories/stats
```

## 移动端部署

### iOS

```bash
# 编译框架（需 gomobile）
gomobile bind -target=ios -o HybridMem.xcframework ./pkg/mobile

# 集成到 Xcode 项目
# 1. 拖拽 HybridMem.xcframework 到项目
# 2. 在 Swift 中导入: import HybridMem
```

### Android

```bash
# 编译 AAR
gomobile bind -target=android -o hybridmem.aar ./pkg/mobile

# 集成到 Android Studio
# 1. 复制 hybridmem.aar 到 app/libs/
# 2. 在 build.gradle 添加: implementation files('libs/hybridmem.aar')
```

## 支持

- GitHub Issues: <repository-url>/issues
- 文档: <repository-url>/docs
