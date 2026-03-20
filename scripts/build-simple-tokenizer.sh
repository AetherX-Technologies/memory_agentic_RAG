#!/bin/bash
# 编译 simple tokenizer 扩展

set -e

echo "=== Building Simple Tokenizer ==="

# 检查是否已存在
if [ -d "simple" ]; then
    echo "✓ simple 目录已存在，跳过克隆"
else
    echo "→ 克隆 simple tokenizer..."
    git clone https://github.com/wangfenjin/simple.git
fi

cd simple

# 创建构建目录
mkdir -p build
cd build

# 编译
echo "→ 编译中..."
cmake ..
make

# 复制到项目 lib 目录
echo "→ 复制扩展文件..."
cd ../..
mkdir -p lib

case "$(uname -s)" in
    Darwin*)
        cp simple/build/libsimple.dylib lib/
        echo "✓ 已复制 libsimple.dylib 到 lib/"
        ;;
    Linux*)
        cp simple/build/libsimple.so lib/
        echo "✓ 已复制 libsimple.so 到 lib/"
        ;;
    MINGW*|MSYS*|CYGWIN*)
        cp simple/build/Release/simple.dll lib/
        echo "✓ 已复制 simple.dll 到 lib/"
        ;;
    *)
        echo "❌ 不支持的平台: $(uname -s)"
        exit 1
        ;;
esac

echo ""
echo "=== 编译完成 ==="
echo "扩展文件位置: $(pwd)/lib/"
echo ""
echo "下一步："
echo "1. 运行测试: go test ./internal/store/..."
echo "2. 运行集成测试: go run cmd/integration_test/main.go"
