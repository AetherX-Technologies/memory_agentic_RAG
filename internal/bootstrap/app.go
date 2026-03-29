package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/yourusername/hybridmem-rag/internal/config"
	"github.com/yourusername/hybridmem-rag/internal/consolidate"
	"github.com/yourusername/hybridmem-rag/internal/embedder"
	"github.com/yourusername/hybridmem-rag/internal/store"
	"github.com/yourusername/hybridmem-rag/internal/trigger"
)

type App struct {
	Config       *config.AppConfig
	Store        store.Store
	Embedder     store.Embedder
	Consolidator *consolidate.Consolidator
	DBPath       string

	closeFuncs []func() error
}

func Load() (*App, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}

	dbPath := envOr("MEMORY_DB_PATH", cfg.Store.DBPath)
	if dbPath == "" {
		dbPath = "memory.db"
	}

	storeCfg := cfg.ToStoreConfig()
	storeCfg.DBPath = dbPath

	rerankProvider := envOr("MEMORY_RERANK_PROVIDER", cfg.Rerank.Provider)
	rerankKey := envOr("MEMORY_RERANK_KEY", cfg.Rerank.APIKey)
	rerankExplicitlyDisabled := !cfg.Rerank.Enabled && os.Getenv("MEMORY_RERANK_PROVIDER") == ""
	if rerankProvider != "" && rerankKey != "" && !rerankExplicitlyDisabled {
		storeCfg.RerankConfig.Enabled = true
		storeCfg.RerankConfig.Provider = rerankProvider
		storeCfg.RerankConfig.APIKey = rerankKey
		storeCfg.RerankConfig.Model = envOr("MEMORY_RERANK_MODEL", storeCfg.RerankConfig.Model)
		storeCfg.RerankConfig.Endpoint = envOr("MEMORY_RERANK_ENDPOINT", storeCfg.RerankConfig.Endpoint)
	} else {
		storeCfg.RerankConfig.Enabled = false
	}

	st, err := store.New(storeCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to open store: %w", err)
	}

	app := &App{
		Config: cfg,
		Store:  st,
		DBPath: dbPath,
		closeFuncs: []func() error{
			st.Close,
		},
	}

	emb, closeEmbedder, err := buildEmbedder(cfg)
	if err != nil {
		_ = app.Close()
		return nil, err
	}
	app.Embedder = emb
	if closeEmbedder != nil {
		app.closeFuncs = append(app.closeFuncs, closeEmbedder)
	}

	// Load FastText should_capture model (required)
	ftPath := envOr("MEMORY_FASTTEXT_MODEL", "models/should_capture_best.ftz")
	if !filepath.IsAbs(ftPath) {
		exeDir, _ := filepath.Abs(filepath.Dir(os.Args[0]))
		if candidate := filepath.Join(exeDir, ftPath); fileExists(candidate) {
			ftPath = candidate
		}
	}
	if err := trigger.InitFastText(ftPath); err != nil {
		_ = app.Close()
		return nil, fmt.Errorf("failed to load fasttext model: %w", err)
	}
	{
		app.closeFuncs = append(app.closeFuncs, func() error {
			trigger.CloseFastText()
			return nil
		})
	}

	llmKey := envOr("MEMORY_LLM_KEY", cfg.LLM.APIKey)
	if llmKey != "" {
		llmEndpoint := envOr("MEMORY_LLM_ENDPOINT", cfg.LLM.Endpoint)
		llmModel := envOr("MEMORY_LLM_MODEL", cfg.LLM.Model)
		app.Consolidator = consolidate.New(st, consolidate.Config{
			LLMAPIKey:   llmKey,
			LLMModel:    llmModel,
			LLMEndpoint: llmEndpoint,
			LLMTimeout:  120,
			MaxMemories: 50,
		})
	}

	return app, nil
}

func (a *App) Close() error {
	var firstErr error
	for i := len(a.closeFuncs) - 1; i >= 0; i-- {
		if err := a.closeFuncs[i](); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func buildEmbedder(cfg *config.AppConfig) (store.Embedder, func() error, error) {
	embedProvider := envOr("MEMORY_EMBED_PROVIDER", cfg.Embedding.Provider)
	embedKey := os.Getenv("MEMORY_EMBED_KEY")
	if embedKey == "" && (embedProvider == "jina" || embedProvider == "openai") {
		switch embedProvider {
		case "jina":
			embedKey = cfg.Embedding.Jina.APIKey
		case "openai":
			embedKey = cfg.Embedding.OpenAI.APIKey
		}
	}

	switch embedProvider {
	case "local":
		localCfg := cfg.ToLocalEmbedderConfig()
		if localCfg.ModelPath != "" && !filepath.IsAbs(localCfg.ModelPath) {
			exeDir, _ := filepath.Abs(filepath.Dir(os.Args[0]))
			candidate := filepath.Join(exeDir, localCfg.ModelPath)
			if _, err := os.Stat(candidate); err == nil {
				localCfg.ModelPath = candidate
			}
		}
		localEmb, err := embedder.NewLocalEmbedder(localCfg)
		if err != nil {
			return nil, nil, nil
		}
		return localEmb, localEmb.Close, nil
	case "jina", "openai":
		if embedKey == "" {
			return nil, nil, nil
		}
		apiCfg := store.EmbeddingConfig{
			Enabled:  true,
			Provider: embedProvider,
			APIKey:   embedKey,
			Timeout:  10,
		}
		switch embedProvider {
		case "jina":
			model := cfg.Embedding.Jina.Model
			if model == "" {
				model = "jina-embeddings-v3"
			}
			endpoint := cfg.Embedding.Jina.Endpoint
			if endpoint == "" {
				endpoint = "https://api.jina.ai/v1/embeddings"
			}
			apiCfg.Model = envOr("MEMORY_EMBED_MODEL", model)
			apiCfg.Endpoint = envOr("MEMORY_EMBED_ENDPOINT", endpoint)
		case "openai":
			model := cfg.Embedding.OpenAI.Model
			if model == "" {
				model = "text-embedding-3-small"
			}
			endpoint := cfg.Embedding.OpenAI.Endpoint
			if endpoint == "" {
				endpoint = "https://api.openai.com/v1/embeddings"
			}
			apiCfg.Model = envOr("MEMORY_EMBED_MODEL", model)
			apiCfg.Endpoint = envOr("MEMORY_EMBED_ENDPOINT", endpoint)
		}
		return store.NewEmbedder(apiCfg), nil, nil
	default:
		return nil, nil, nil
	}
}

func loadConfig() (*config.AppConfig, error) {
	configPath := envOr("MEMORY_CONFIG_PATH", "")
	if configPath != "" {
		cfg, err := config.Load(configPath)
		if err == nil {
			return cfg, nil
		}
	}

	exeDir, _ := filepath.Abs(filepath.Dir(os.Args[0]))
	for _, candidate := range []string{filepath.Join(exeDir, "config.yaml"), "config.yaml"} {
		if _, err := os.Stat(candidate); err == nil {
			cfg, loadErr := config.Load(candidate)
			if loadErr == nil {
				return cfg, nil
			}
		}
	}

	cfg := config.Default()
	cfg.Embedding.Provider = ""
	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
