package config

import (
	"testing"

	"github.com/creasty/defaults"
)

func TestWebConfigDefaults(t *testing.T) {
	var webConfig WebConfig
	if err := defaults.Set(&webConfig); err != nil {
		t.Fatalf("defaults.Set() error = %v", err)
	}
	if webConfig.Host != DefaultWebHost {
		t.Fatalf("default host = %q, want %q", webConfig.Host, DefaultWebHost)
	}
	if webConfig.Port != DefaultWebPort {
		t.Fatalf("default port = %q, want %q", webConfig.Port, DefaultWebPort)
	}
}

func TestApplyRuntimeFallbacksUsesDefaultWebAddress(t *testing.T) {
	cfg := &ApplicationConfig{}
	plan := startupGuidePlan{
		DefaultSharedToken: "test_shared_token",
		DefaultAccessToken: "test_access_token",
	}

	changed, err := applyRuntimeFallbacks(cfg, plan)
	if err != nil {
		t.Fatalf("applyRuntimeFallbacks() error = %v", err)
	}
	if !changed {
		t.Fatal("applyRuntimeFallbacks() changed = false, want true")
	}
	if cfg.WebConfig.Host != DefaultWebHost || cfg.WebConfig.Port != DefaultWebPort {
		t.Fatalf("web address = %s:%s, want %s:%s", cfg.WebConfig.Host, cfg.WebConfig.Port, DefaultWebHost, DefaultWebPort)
	}
}
