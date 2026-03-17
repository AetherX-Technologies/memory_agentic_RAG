# 构建说明

## 构建要求

本项目使用 SQLite FTS5 和 simple tokenizer 扩展，需要使用特定的构建标签。

## 构建命令

### 开发环境

```bash
go run -tags "sqlite_extensions fts5" ./cmd/server
```

### 生产构建

```bash
go build -tags "sqlite_extensions fts5" -o hybridmem-rag ./cmd/server
```

### 测试

```bash
go test -tags "sqlite_extensions fts5" ./...
```

## 构建标签说明

- `sqlite_extensions`: 启用 SQLite 扩展加载功能（mattn/go-sqlite3）
- `fts5`: 启用 SQLite FTS5 全文搜索支持

## 依赖项

### Simple Tokenizer 扩展

项目依赖 simple tokenizer 扩展用于中文分词。首次构建前需要编译扩展：

```bash
./scripts/build-simple-tokenizer.sh
```

编译后的扩展文件位于 `lib/` 目录：
- macOS: `lib/libsimple.dylib`
- Linux: `lib/libsimple.so`
- Windows: `lib/simple.dll`

## 跨平台注意事项

### macOS
- 需要 Xcode Command Line Tools
- 需要 CMake: `brew install cmake`

### Linux
- 需要 gcc/g++
- 需要 CMake: `sudo apt-get install cmake g++ libsqlite3-dev`

### Windows
- 需要 Visual Studio 2019+
- 需要 CMake

## CGO 要求

本项目使用 mattn/go-sqlite3，需要启用 CGO：

```bash
export CGO_ENABLED=1
```

对于交叉编译，需要配置相应的交叉编译工具链。
