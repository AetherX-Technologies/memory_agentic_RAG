# Simple Tokenizer 集成方案

> 微信团队开源的 SQLite FTS5 中文分词扩展
> 项目地址：https://github.com/wangfenjin/simple

## 1. 概述

### 为什么选择 simple tokenizer

- ✅ **按字分词** - 支持任意长度的中文词（解决 trigram 的3字符限制）
- ✅ **拼音搜索** - 支持 "baogao" 搜索 "报告"
- ✅ **高性能** - 微信团队优化，100万条记录查询仅 2.9ms
- ✅ **轻量级** - 适合移动端部署
- ✅ **零维护** - 无需词典维护

### 与 trigram 对比

| 特性 | trigram | simple |
|------|---------|--------|
| 最小查询长度 | 3个字符 | 1个字符 |
| 拼音搜索 | ❌ | ✅ |
| 索引大小 | 较大 | 中等 |
| 部署复杂度 | 简单（内置） | 中等（需编译） |

## 2. 编译步骤

### 2.1 前置要求

- CMake 3.10+
- C++ 编译器（GCC/Clang/MSVC）
- SQLite 3.20+

### 2.2 macOS 编译

```bash
# 1. 克隆源码
git clone https://github.com/wangfenjin/simple.git
cd simple

# 2. 编译
mkdir build && cd build
cmake ..
make

# 3. 验证生成的文件
ls -lh libsimple.dylib
# 应该看到 libsimple.dylib 文件
```

### 2.3 Linux 编译

```bash
# 1. 安装依赖（Ubuntu/Debian）
sudo apt-get install cmake g++ libsqlite3-dev

# 2. 克隆并编译
git clone https://github.com/wangfenjin/simple.git
cd simple
mkdir build && cd build
cmake ..
make

# 3. 验证
ls -lh libsimple.so
```

### 2.4 Windows 编译

```bash
# 使用 Visual Studio 2019+
git clone https://github.com/wangfenjin/simple.git
cd simple
mkdir build && cd build
cmake .. -G "Visual Studio 16 2019"
cmake --build . --config Release

# 生成 simple.dll
```

## 3. 项目集成

### 3.1 目录结构

```
memory_agentic_RAG/
├── lib/
│   ├── libsimple.dylib    # macOS
│   ├── libsimple.so       # Linux
│   └── simple.dll         # Windows
├── internal/store/
│   ├── schema.go          # 修改 FTS 表定义
│   └── store.go           # 加载扩展
└── docs/references/
    └── simple-tokenizer-integration.md  # 本文档
```

### 3.2 修改 FTS 表定义

在 `internal/store/schema.go` 中：

```go
schemaFTS = `
CREATE VIRTUAL TABLE IF NOT EXISTS fts_memories USING fts5(
    memory_id UNINDEXED,
    content,
    tokenize='simple'  // 改为 simple
);
`
```

### 3.3 加载扩展

在 `internal/store/store.go` 中添加：

```go
import (
    "path/filepath"
    "runtime"
)

func (s *sqliteStore) loadSimpleTokenizer() error {
    // 根据平台选择扩展文件
    var extPath string
    switch runtime.GOOS {
    case "darwin":
        extPath = "./lib/libsimple.dylib"
    case "linux":
        extPath = "./lib/libsimple.so"
    case "windows":
        extPath = "./lib/simple.dll"
    default:
        return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
    }

    // 加载扩展
    _, err := s.db.Exec("SELECT load_extension(?)", extPath)
    if err != nil {
        return fmt.Errorf("failed to load simple tokenizer: %w", err)
    }

    return nil
}
```

在 `New()` 函数中调用：

```go
func New(config Config) (Store, error) {
    // ... 现有代码 ...

    // 加载 simple tokenizer
    if err := store.loadSimpleTokenizer(); err != nil {
        return nil, err
    }

    // ... 现有代码 ...
}
```

## 4. 测试验证

### 4.1 单元测试

```go
func TestSimpleTokenizer(t *testing.T) {
    st, _ := New(Config{DBPath: ":memory:", VectorDim: 0})
    defer st.Close()

    // 插入测试数据
    st.Insert(&Memory{
        Text: "人工智能技术报告",
        Scope: "global",
    })

    // 测试短词搜索
    results, err := st.Search(nil, "报告", "", 10, []string{"global"})
    if err != nil {
        t.Fatal(err)
    }
    if len(results) == 0 {
        t.Error("应该能搜索到'报告'")
    }

    // 测试拼音搜索
    results, err = st.Search(nil, "baogao", "", 10, []string{"global"})
    if err != nil {
        t.Fatal(err)
    }
    if len(results) == 0 {
        t.Error("应该支持拼音搜索")
    }
}
```

### 4.2 集成测试

```bash
# 重新运行集成测试
rm -f integration_test.db
go run cmd/integration_test/main.go

# 预期结果：
# ✅ "报告" 能搜索到
# ✅ "公司" 能搜索到
# ✅ 分层搜索正常工作
```

## 5. 部署指南

### 5.1 开发环境

```bash
# 1. 编译扩展（一次性）
cd simple && mkdir build && cd build
cmake .. && make

# 2. 复制到项目
cp libsimple.* ../../memory_agentic_RAG/lib/

# 3. 运行测试
cd ../../memory_agentic_RAG
go test ./internal/store/...
```

### 5.2 生产部署

**桌面应用**：
```bash
# 打包时包含扩展文件
├── hybridmem-rag          # 可执行文件
└── lib/
    └── libsimple.dylib    # 扩展文件
```

**移动端（iOS）**：
```bash
# 静态链接到 app
# 在 gomobile bind 时指定 -ldflags
gomobile bind -ldflags="-L./lib -lsimple" ...
```

**移动端（Android）**：
```bash
# 打包到 assets 目录
# 运行时复制到 app 私有目录
```

### 5.3 Docker 部署

```dockerfile
FROM golang:1.21-alpine

# 安装编译依赖
RUN apk add --no-cache cmake g++ make sqlite-dev

# 编译 simple tokenizer
RUN git clone https://github.com/wangfenjin/simple.git && \
    cd simple && mkdir build && cd build && \
    cmake .. && make && \
    cp libsimple.so /usr/local/lib/

# 复制项目代码
COPY . /app
WORKDIR /app

# 构建应用
RUN go build -o hybridmem-rag ./cmd/server

CMD ["./hybridmem-rag"]
```

## 6. 常见问题

### Q1: 加载扩展失败

**错误**：`Error: not authorized`

**解决**：SQLite 默认禁用扩展加载，需要编译时启用：

```go
// 使用支持扩展的 SQLite 驱动
import _ "github.com/mattn/go-sqlite3"

// 或者使用 modernc.org/sqlite（已支持）
import _ "modernc.org/sqlite"
```

### Q2: 找不到扩展文件

**错误**：`Error: cannot open shared object file`

**解决**：使用绝对路径或设置 LD_LIBRARY_PATH：

```go
extPath, _ := filepath.Abs("./lib/libsimple.so")
db.Exec("SELECT load_extension(?)", extPath)
```

### Q3: 跨平台编译

**问题**：如何为多个平台编译扩展？

**解决**：使用 GitHub Actions 自动化编译：

```yaml
# .github/workflows/build-simple.yml
name: Build Simple Tokenizer
on: [push]
jobs:
  build:
    strategy:
      matrix:
        os: [ubuntu-latest, macos-latest, windows-latest]
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v2
      - name: Build
        run: |
          git clone https://github.com/wangfenjin/simple.git
          cd simple && mkdir build && cd build
          cmake .. && make
      - name: Upload
        uses: actions/upload-artifact@v2
        with:
          name: simple-${{ matrix.os }}
          path: simple/build/libsimple.*
```

## 7. 性能优化

### 7.1 索引优化

```sql
-- 创建复合索引
CREATE INDEX idx_fts_hierarchy ON memories(hierarchy_path, scope);

-- 定期优化 FTS 索引
INSERT INTO fts_memories(fts_memories) VALUES('optimize');
```

### 7.2 查询优化

```go
// 使用 simple_query() 函数自动处理拼音
results, _ := db.Query(`
    SELECT * FROM fts_memories
    WHERE fts_memories MATCH simple_query(?)
`, query)
```

## 8. 下一步

- [ ] 编译 simple tokenizer
- [ ] 修改 schema.go
- [ ] 实现扩展加载逻辑
- [ ] 运行集成测试
- [ ] 验证拼音搜索
- [ ] 配置 CI/CD 自动编译

---

**最后更新**：2026-03-16
**维护者**：HybridMem-RAG Team
