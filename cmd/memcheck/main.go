package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/yourusername/hybridmem-rag/internal/embedder"
)

func processRSS() int64 {
	pid := os.Getpid()
	// macOS: ps -o rss= -p PID (returns KB)
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0
	}
	kb, _ := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	return kb / 1024 // MB
}

func goHeapMB() uint64 {
	var m runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m)
	return m.Alloc / 1024 / 1024
}

func main() {
	fmt.Printf("1. 启动时         Go Heap=%3dMB  进程 RSS=%3dMB\n", goHeapMB(), processRSS())

	emb, err := embedder.NewLocalEmbedder(embedder.Config{
		ModelPath: "models/qwen3-embedding-0.6b-onnx-uint8/dynamic_uint8.onnx",
		BatchSize: 16,
	})
	if err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }

	fmt.Printf("2. 模型加载后     Go Heap=%3dMB  进程 RSS=%3dMB\n", goHeapMB(), processRSS())

	_, _ = emb.Embed("测试句子。")
	fmt.Printf("3. 推理 1 句后    Go Heap=%3dMB  进程 RSS=%3dMB\n", goHeapMB(), processRSS())

	texts := make([]string, 100)
	for i := range texts { texts[i] = fmt.Sprintf("批量测试第%d句。", i) }
	_, _ = emb.EmbedBatch(texts)
	fmt.Printf("4. 批量 100 句后  Go Heap=%3dMB  进程 RSS=%3dMB\n", goHeapMB(), processRSS())

	texts500 := make([]string, 500)
	for i := range texts500 { texts500[i] = fmt.Sprintf("大批量第%d句，这是更长的句子用来模拟真实文档中的内容。", i) }
	_, _ = emb.EmbedBatch(texts500)
	fmt.Printf("5. 批量 500 句后  Go Heap=%3dMB  进程 RSS=%3dMB\n", goHeapMB(), processRSS())

	emb.Close()
	time.Sleep(200 * time.Millisecond)
	runtime.GC()
	fmt.Printf("6. Close + GC 后  Go Heap=%3dMB  进程 RSS=%3dMB\n", goHeapMB(), processRSS())
}
