package llmutil

import (
	"os"
	"strconv"
)

// Tier identifies which LLM configuration to resolve.
type Tier string

const (
	TierMain    Tier = "main"    // primary LLM for consolidation, conflict detection, deep reasoning
	TierSummary Tier = "summary" // smaller LLM for summarization, structured extraction
)

// ResolvedLLMConfig is the final concrete config for an LLM tier after fallback resolution.
type ResolvedLLMConfig struct {
	APIKey   string
	Model    string
	Endpoint string
	Timeout  int
}

// ToCallConfig converts to the Config used by CallLLM.
func (r ResolvedLLMConfig) ToCallConfig() Config {
	return Config{
		APIKey:   r.APIKey,
		Model:    r.Model,
		Endpoint: r.Endpoint,
		Timeout:  r.Timeout,
	}
}

// MainLLMConfig is the minimal interface needed to resolve LLM configs.
// Decoupled from internal/config to avoid import cycles — callers pass values directly.
type MainLLMConfig struct {
	APIKey   string
	Model    string
	Endpoint string
	Timeout  int
}

// SummarySubConfig is the optional summary tier override.
type SummarySubConfig struct {
	APIKey   string
	Model    string
	Endpoint string
	Timeout  int
}

// ResolveLLMConfig returns the final config for the given tier with fallback chain:
//
//   For tier=main:
//     env MEMORY_LLM_X > main.X
//
//   For tier=summary:
//     env MEMORY_LLM_SUMMARY_X > summary.X (yaml) > env MEMORY_LLM_X > main.X
//
// Each field resolves independently.
func ResolveLLMConfig(main MainLLMConfig, summary *SummarySubConfig, tier Tier) ResolvedLLMConfig {
	// Start with main tier resolved (env overrides)
	mainKey := envOr("MEMORY_LLM_KEY", main.APIKey)
	mainModel := envOr("MEMORY_LLM_MODEL", main.Model)
	mainEndpoint := envOr("MEMORY_LLM_ENDPOINT", main.Endpoint)
	mainTimeout := envIntOr("MEMORY_LLM_TIMEOUT", main.Timeout)

	if tier == TierMain {
		return ResolvedLLMConfig{
			APIKey:   mainKey,
			Model:    mainModel,
			Endpoint: mainEndpoint,
			Timeout:  mainTimeout,
		}
	}

	// tier == summary: layer summary overrides on top of resolved main
	summaryKey := mainKey
	summaryModel := mainModel
	summaryEndpoint := mainEndpoint
	summaryTimeout := mainTimeout

	if summary != nil {
		if summary.APIKey != "" {
			summaryKey = summary.APIKey
		}
		if summary.Model != "" {
			summaryModel = summary.Model
		}
		if summary.Endpoint != "" {
			summaryEndpoint = summary.Endpoint
		}
		if summary.Timeout > 0 {
			summaryTimeout = summary.Timeout
		}
	}

	// Env vars have highest precedence
	if v := os.Getenv("MEMORY_LLM_SUMMARY_KEY"); v != "" {
		summaryKey = v
	}
	if v := os.Getenv("MEMORY_LLM_SUMMARY_MODEL"); v != "" {
		summaryModel = v
	}
	if v := os.Getenv("MEMORY_LLM_SUMMARY_ENDPOINT"); v != "" {
		summaryEndpoint = v
	}
	if v := os.Getenv("MEMORY_LLM_SUMMARY_TIMEOUT"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			summaryTimeout = i
		}
	}

	return ResolvedLLMConfig{
		APIKey:   summaryKey,
		Model:    summaryModel,
		Endpoint: summaryEndpoint,
		Timeout:  summaryTimeout,
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envIntOr(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			return i
		}
	}
	return fallback
}
