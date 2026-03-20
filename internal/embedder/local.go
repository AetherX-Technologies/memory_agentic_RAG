package embedder

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"

	"github.com/sugarme/tokenizer/pretrained"
	ort "github.com/yalue/onnxruntime_go"

	"github.com/sugarme/tokenizer"
)

// Config holds configuration for the local ONNX-based embedder.
type Config struct {
	ModelPath     string   // Path to ONNX model file
	TokenizerPath string   // Path to tokenizer.json (default: same dir as model)
	LibraryPath   string   // Path to ONNX Runtime shared library (auto-detected if empty)
	BatchSize     int      // Max batch size for inference (default: 32)
	MaxSeqLen     int      // Max sequence length (default: 512)
	HiddenDim     int      // Hidden dimension of the model (default: 1024 for Qwen3-0.6B)
	InputNames    []string // Model input tensor names (default: ["input_ids", "attention_mask"])
	OutputNames   []string // Model output tensor names (default: ["last_hidden_state"])
}

// LocalEmbedder performs local embedding inference using ONNX Runtime with Qwen3.
// Used for semantic splitting only — retrieval uses the API embedder.
type LocalEmbedder struct {
	session *ort.DynamicAdvancedSession
	tok     *tokenizer.Tokenizer
	config  Config
	mu      sync.Mutex
}

// NewLocalEmbedder creates a new local embedder with the Qwen3 ONNX model.
func NewLocalEmbedder(config Config) (*LocalEmbedder, error) {
	// Apply defaults
	if config.BatchSize <= 0 {
		config.BatchSize = 32
	}
	if config.MaxSeqLen <= 0 {
		config.MaxSeqLen = 512
	}
	if config.HiddenDim <= 0 {
		config.HiddenDim = 1024 // Qwen3-Embedding-0.6B
	}
	if len(config.InputNames) == 0 {
		config.InputNames = []string{"input_ids", "attention_mask"}
	}
	if len(config.OutputNames) == 0 {
		config.OutputNames = []string{"sentence_embedding_quantized"}
	}

	// 1. Check model file exists
	if _, err := os.Stat(config.ModelPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("model file not found: %s\nPlease download from: https://huggingface.co/electroglyph/Qwen3-Embedding-0.6B-onnx-uint8", config.ModelPath)
	}

	// 2. Determine tokenizer path
	tokPath := config.TokenizerPath
	if tokPath == "" {
		tokPath = filepath.Join(filepath.Dir(config.ModelPath), "tokenizer.json")
	}
	if _, err := os.Stat(tokPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("tokenizer file not found: %s\nPlease ensure tokenizer.json is in the same directory as the model", tokPath)
	}

	// 3. Initialize ONNX Runtime (singleton — safe to call multiple times)
	libPath := config.LibraryPath
	if libPath == "" {
		libPath = findONNXRuntimeLibrary()
	}
	if !ort.IsInitialized() {
		ort.SetSharedLibraryPath(libPath)
		if err := ort.InitializeEnvironment(); err != nil {
			return nil, fmt.Errorf("failed to initialize ONNX Runtime: %w\nLibrary path: %s", err, libPath)
		}
	}

	// 4. Load tokenizer
	tk, err := pretrained.FromFile(tokPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load tokenizer: %w\nPlease ensure tokenizer.json is valid", err)
	}

	// 5. Create ONNX session with optimized settings
	opts, err := ort.NewSessionOptions()
	if err != nil {
		return nil, fmt.Errorf("failed to create session options: %w", err)
	}
	if err := opts.SetIntraOpNumThreads(4); err != nil {
		opts.Destroy()
		return nil, fmt.Errorf("failed to set intra-op threads: %w", err)
	}
	if err := opts.SetInterOpNumThreads(1); err != nil {
		opts.Destroy()
		return nil, fmt.Errorf("failed to set inter-op threads: %w", err)
	}
	if err := opts.SetCpuMemArena(true); err != nil {
		opts.Destroy()
		return nil, fmt.Errorf("failed to enable CPU mem arena: %w", err)
	}
	if err := opts.SetMemPattern(true); err != nil {
		opts.Destroy()
		return nil, fmt.Errorf("failed to enable mem pattern: %w", err)
	}

	session, err := ort.NewDynamicAdvancedSession(
		config.ModelPath,
		config.InputNames,
		config.OutputNames,
		opts,
	)
	opts.Destroy()
	if err != nil {
		return nil, fmt.Errorf("failed to create ONNX session: %w\nModel file may be corrupted, please re-download", err)
	}

	return &LocalEmbedder{
		session: session,
		tok:     tk,
		config:  config,
	}, nil
}

// Embed embeds a single text and returns the normalized embedding vector.
func (e *LocalEmbedder) Embed(text string) ([]float32, error) {
	results, err := e.EmbedBatch([]string{text})
	if err != nil {
		return nil, err
	}
	return results[0], nil
}

// EmbedBatch embeds multiple texts, processing in sub-batches of config.BatchSize.
func (e *LocalEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	results := make([][]float32, len(texts))
	for i := 0; i < len(texts); i += e.config.BatchSize {
		end := i + e.config.BatchSize
		if end > len(texts) {
			end = len(texts)
		}
		batchResults, err := e.inferBatch(texts[i:end])
		if err != nil {
			return nil, fmt.Errorf("batch embedding failed at offset %d: %w", i, err)
		}
		copy(results[i:end], batchResults)
	}

	return results, nil
}

// inferBatch runs ONNX inference on a single batch and returns mean-pooled, L2-normalized embeddings.
func (e *LocalEmbedder) inferBatch(texts []string) ([][]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	batchSize := len(texts)

	// 1. Tokenize all texts
	maxLen := 0
	allIds := make([][]int, batchSize)
	allMasks := make([][]int, batchSize)
	for i, text := range texts {
		enc, err := e.tok.EncodeSingle(text, true)
		if err != nil {
			return nil, fmt.Errorf("tokenization failed for text %d: %w", i, err)
		}
		allIds[i] = enc.Ids
		allMasks[i] = enc.AttentionMask
		if len(enc.Ids) > maxLen {
			maxLen = len(enc.Ids)
		}
	}
	if maxLen > e.config.MaxSeqLen {
		maxLen = e.config.MaxSeqLen
	}
	if maxLen == 0 {
		maxLen = 1 // avoid zero-size tensor
	}

	// 2. Build padded input tensors (row-major: [batch, seq])
	inputIdsData := make([]int64, batchSize*maxLen)
	attMaskData := make([]int64, batchSize*maxLen)
	for i := 0; i < batchSize; i++ {
		seqLen := len(allIds[i])
		if seqLen > maxLen {
			seqLen = maxLen
		}
		for j := 0; j < seqLen; j++ {
			inputIdsData[i*maxLen+j] = int64(allIds[i][j])
			attMaskData[i*maxLen+j] = int64(allMasks[i][j])
		}
		// remaining positions already 0 (padding)
	}

	inputShape := ort.NewShape(int64(batchSize), int64(maxLen))
	inputIdsTensor, err := ort.NewTensor(inputShape, inputIdsData)
	if err != nil {
		return nil, fmt.Errorf("failed to create input_ids tensor: %w", err)
	}
	defer inputIdsTensor.Destroy()

	attMaskTensor, err := ort.NewTensor(inputShape, attMaskData)
	if err != nil {
		return nil, fmt.Errorf("failed to create attention_mask tensor: %w", err)
	}
	defer attMaskTensor.Destroy()

	// 3. Create output tensor
	// Model output depends on quantization: uint8 models output [batch, hidden] directly (pooled).
	// Non-quantized models may output [batch, seq, hidden] requiring mean pooling.
	hiddenDim := e.config.HiddenDim
	outputShape := ort.NewShape(int64(batchSize), int64(hiddenDim))
	outputTensor, err := ort.NewEmptyTensor[uint8](outputShape)
	if err != nil {
		return nil, fmt.Errorf("failed to create output tensor: %w", err)
	}
	defer outputTensor.Destroy()

	// 4. Run inference
	if err := e.session.Run(
		[]ort.Value{inputIdsTensor, attMaskTensor},
		[]ort.Value{outputTensor},
	); err != nil {
		return nil, fmt.Errorf("ONNX inference failed: %w", err)
	}

	// 5. Convert uint8 output to float32 and L2 normalize
	outputData := outputTensor.GetData()
	results := make([][]float32, batchSize)

	for i := 0; i < batchSize; i++ {
		embedding := make([]float32, hiddenDim)
		base := i * hiddenDim
		for k := 0; k < hiddenDim; k++ {
			// uint8 [0, 255] → float32 [-1, 1] (center and scale)
			embedding[k] = (float32(outputData[base+k]) - 128.0) / 128.0
		}
		results[i] = l2Normalize(embedding)
	}

	return results, nil
}

// Close releases all ONNX resources.
func (e *LocalEmbedder) Close() error {
	if e.session != nil {
		return e.session.Destroy()
	}
	return nil
}

// l2Normalize normalizes a vector to unit length.
func l2Normalize(v []float32) []float32 {
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	norm = math.Sqrt(norm)
	if norm == 0 {
		return v
	}
	invNorm := float32(1.0 / norm)
	result := make([]float32, len(v))
	for i, x := range v {
		result[i] = x * invNorm
	}
	return result
}

// findONNXRuntimeLibrary tries common paths for the ONNX Runtime shared library.
func findONNXRuntimeLibrary() string {
	paths := []string{
		"/opt/homebrew/lib/libonnxruntime.dylib", // macOS ARM64 (Homebrew)
		"/usr/local/lib/libonnxruntime.dylib",    // macOS Intel
		"/usr/lib/libonnxruntime.so",             // Linux
		"/usr/local/lib/libonnxruntime.so",       // Linux (local install)
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "libonnxruntime.dylib" // fallback: let dlopen find it
}
