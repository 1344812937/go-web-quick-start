package config

import (
	"path/filepath"
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

func TestGatewayConfigDefaultsPayloadLogDetail(t *testing.T) {
	var gatewayConfig GatewayConfig
	if err := defaults.Set(&gatewayConfig); err != nil {
		t.Fatalf("defaults.Set() error = %v", err)
	}
	if gatewayConfig.PayloadLogDetail != PayloadLogDetailDefault {
		t.Fatalf("payload log detail = %q, want %q", gatewayConfig.PayloadLogDetail, PayloadLogDetailDefault)
	}
}

func TestApplicationConfigManagerSaveUpdatesPayloadLogDetailAtRuntime(t *testing.T) {
	originalConfigPath := configPath
	configPath = filepath.Join(t.TempDir(), "config.toml")
	t.Cleanup(func() { configPath = originalConfigPath })

	manager := &ApplicationConfigManager{}
	cfg := &ApplicationConfig{GatewayConfig: GatewayConfig{PayloadLogDetail: PayloadLogDetailSummary}}
	if err := manager.Save(cfg); err != nil {
		t.Fatalf("Save(summary) error = %v", err)
	}
	if got := EffectivePayloadLogDetail(manager.GetConfig()); got != PayloadLogDetailSummary {
		t.Fatalf("runtime payload log detail = %q, want %q", got, PayloadLogDetailSummary)
	}

	cfg.GatewayConfig.PayloadLogDetail = PayloadLogDetailNone
	if err := manager.Save(cfg); err != nil {
		t.Fatalf("Save(none) error = %v", err)
	}
	if got := EffectivePayloadLogDetail(manager.GetConfig()); got != PayloadLogDetailNone {
		t.Fatalf("updated runtime payload log detail = %q, want %q", got, PayloadLogDetailNone)
	}

	cfg.GatewayConfig.PayloadLogDetail = "verbose"
	if err := manager.Save(cfg); err == nil {
		t.Fatal("Save(invalid) error = nil")
	}
	if got := EffectivePayloadLogDetail(manager.GetConfig()); got != PayloadLogDetailNone {
		t.Fatalf("invalid save changed runtime payload log detail to %q", got)
	}
}

func TestApplyRuntimeFallbacksUsesDefaultWebAddress(t *testing.T) {
	cfg := &ApplicationConfig{}
	plan := startupGuidePlan{
		DefaultSharedToken: "test_shared_token",
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
