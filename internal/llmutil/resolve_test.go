package llmutil

import (
	"os"
	"testing"
)

func TestResolveLLMConfig_MainTier_NoEnv(t *testing.T) {
	clearLLMEnv(t)
	main := MainLLMConfig{
		APIKey: "main-key", Model: "gpt-5.4",
		Endpoint: "https://api.example.com", Timeout: 30,
	}
	got := ResolveLLMConfig(main, nil, TierMain)
	if got.APIKey != "main-key" || got.Model != "gpt-5.4" || got.Timeout != 30 {
		t.Errorf("main no-env: got %+v", got)
	}
}

func TestResolveLLMConfig_MainTier_EnvOverrides(t *testing.T) {
	clearLLMEnv(t)
	os.Setenv("MEMORY_LLM_KEY", "env-key")
	os.Setenv("MEMORY_LLM_MODEL", "env-model")
	os.Setenv("MEMORY_LLM_TIMEOUT", "60")
	defer clearLLMEnv(t)

	main := MainLLMConfig{APIKey: "main-key", Model: "gpt-5.4", Timeout: 30}
	got := ResolveLLMConfig(main, nil, TierMain)
	if got.APIKey != "env-key" || got.Model != "env-model" || got.Timeout != 60 {
		t.Errorf("env override: got %+v", got)
	}
}

func TestResolveLLMConfig_SummaryTier_FallbackToMain(t *testing.T) {
	clearLLMEnv(t)
	main := MainLLMConfig{
		APIKey: "main-key", Model: "gpt-5.4",
		Endpoint: "https://main.com", Timeout: 30,
	}
	got := ResolveLLMConfig(main, nil, TierSummary)
	// summary not configured → all fields from main
	if got.APIKey != "main-key" || got.Model != "gpt-5.4" || got.Endpoint != "https://main.com" || got.Timeout != 30 {
		t.Errorf("summary fallback: got %+v", got)
	}
}

func TestResolveLLMConfig_SummaryTier_YamlOverride(t *testing.T) {
	clearLLMEnv(t)
	main := MainLLMConfig{APIKey: "main-key", Model: "gpt-5.4", Endpoint: "https://main.com", Timeout: 30}
	summary := &SummarySubConfig{Model: "gpt-4o-mini"} // only model overridden
	got := ResolveLLMConfig(main, summary, TierSummary)
	if got.APIKey != "main-key" {
		t.Errorf("APIKey should inherit: got %s", got.APIKey)
	}
	if got.Model != "gpt-4o-mini" {
		t.Errorf("Model should be summary's: got %s", got.Model)
	}
	if got.Endpoint != "https://main.com" {
		t.Errorf("Endpoint should inherit: got %s", got.Endpoint)
	}
	if got.Timeout != 30 {
		t.Errorf("Timeout should inherit (summary.Timeout=0): got %d", got.Timeout)
	}
}

func TestResolveLLMConfig_SummaryTier_EnvOverridesYaml(t *testing.T) {
	clearLLMEnv(t)
	os.Setenv("MEMORY_LLM_SUMMARY_MODEL", "env-summary-model")
	os.Setenv("MEMORY_LLM_SUMMARY_TIMEOUT", "15")
	defer clearLLMEnv(t)

	main := MainLLMConfig{APIKey: "main-key", Model: "gpt-5.4", Timeout: 30}
	summary := &SummarySubConfig{Model: "yaml-summary-model", Timeout: 20}
	got := ResolveLLMConfig(main, summary, TierSummary)
	if got.Model != "env-summary-model" {
		t.Errorf("env > yaml: got %s", got.Model)
	}
	if got.Timeout != 15 {
		t.Errorf("env timeout: got %d", got.Timeout)
	}
}

func TestResolveLLMConfig_SummaryTier_FullChain(t *testing.T) {
	// Verify full chain: SUMMARY env > summary yaml > main env > main yaml
	clearLLMEnv(t)
	os.Setenv("MEMORY_LLM_KEY", "main-env-key")          // main env override
	os.Setenv("MEMORY_LLM_SUMMARY_ENDPOINT", "env-sum-ep") // summary env override
	defer clearLLMEnv(t)

	main := MainLLMConfig{APIKey: "yaml-main", Model: "yaml-model", Endpoint: "yaml-ep", Timeout: 30}
	summary := &SummarySubConfig{Model: "yaml-summary"} // only model
	got := ResolveLLMConfig(main, summary, TierSummary)

	// Field-by-field expectations:
	// APIKey: no SUMMARY_KEY env, no summary.APIKey → main env "main-env-key"
	// Model: no SUMMARY_MODEL env, has summary.Model → "yaml-summary"
	// Endpoint: SUMMARY_ENDPOINT env wins → "env-sum-ep"
	// Timeout: no SUMMARY_TIMEOUT, no summary.Timeout, no MAIN env → main yaml 30
	if got.APIKey != "main-env-key" {
		t.Errorf("APIKey full-chain: got %s, want main-env-key", got.APIKey)
	}
	if got.Model != "yaml-summary" {
		t.Errorf("Model full-chain: got %s, want yaml-summary", got.Model)
	}
	if got.Endpoint != "env-sum-ep" {
		t.Errorf("Endpoint full-chain: got %s, want env-sum-ep", got.Endpoint)
	}
	if got.Timeout != 30 {
		t.Errorf("Timeout full-chain: got %d, want 30", got.Timeout)
	}
}

func TestResolveLLMConfig_ToCallConfig(t *testing.T) {
	r := ResolvedLLMConfig{APIKey: "k", Model: "m", Endpoint: "e", Timeout: 5}
	c := r.ToCallConfig()
	if c.APIKey != "k" || c.Model != "m" || c.Endpoint != "e" || c.Timeout != 5 {
		t.Errorf("ToCallConfig mismatch: %+v", c)
	}
}

func clearLLMEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"MEMORY_LLM_KEY", "MEMORY_LLM_MODEL", "MEMORY_LLM_ENDPOINT", "MEMORY_LLM_TIMEOUT",
		"MEMORY_LLM_SUMMARY_KEY", "MEMORY_LLM_SUMMARY_MODEL", "MEMORY_LLM_SUMMARY_ENDPOINT", "MEMORY_LLM_SUMMARY_TIMEOUT",
	} {
		os.Unsetenv(k)
	}
}
