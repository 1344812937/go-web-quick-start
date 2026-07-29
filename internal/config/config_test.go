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
	if gatewayConfig.RoutingPriceWeightPercent != DefaultRoutingPriceWeightPercent || gatewayConfig.RoutingEfficiencyWeightPercent != DefaultRoutingEfficiencyWeightPercent {
		t.Fatalf("routing weights = %d/%d", gatewayConfig.RoutingPriceWeightPercent, gatewayConfig.RoutingEfficiencyWeightPercent)
	}
}

func TestNormalizeCommonModelNamesUsesDefaultsAndDeduplicates(t *testing.T) {
	defaults := normalizeCommonModelNames(nil)
	if len(defaults) != len(DefaultCommonModelNames) || defaults[0] != DefaultCommonModelNames[0] {
		t.Fatalf("default common models = %#v", defaults)
	}
	normalized := normalizeCommonModelNames([]string{" gpt-5.6-sol ", "GPT-5.6-SOL", "", "gpt-5.4-mini"})
	if len(normalized) != 2 || normalized[0] != "gpt-5.6-sol" || normalized[1] != "gpt-5.4-mini" {
		t.Fatalf("normalized common models = %#v", normalized)
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

func TestApplicationConfigManagerSaveUpdatesRoutingWeightsAtRuntime(t *testing.T) {
	originalConfigPath := configPath
	configPath = filepath.Join(t.TempDir(), "config.toml")
	t.Cleanup(func() { configPath = originalConfigPath })

	manager := &ApplicationConfigManager{}
	cfg := &ApplicationConfig{GatewayConfig: GatewayConfig{
		RoutingPriceWeightPercent:      60,
		RoutingEfficiencyWeightPercent: 30,
	}}
	if err := manager.Save(cfg); err != nil {
		t.Fatalf("Save(weights) error = %v", err)
	}
	price, efficiency, quality := EffectiveRoutingDecisionWeights(manager.GetConfig())
	if price != 0.6 || efficiency != 0.3 || quality != 0.1 {
		t.Fatalf("runtime routing weights = %v/%v/%v", price, efficiency, quality)
	}

	cfg.GatewayConfig.RoutingPriceWeightPercent = 80
	cfg.GatewayConfig.RoutingEfficiencyWeightPercent = 30
	if err := manager.Save(cfg); err == nil {
		t.Fatal("Save(invalid weights) error = nil")
	}
	price, efficiency, quality = EffectiveRoutingDecisionWeights(manager.GetConfig())
	if price != 0.6 || efficiency != 0.3 || quality != 0.1 {
		t.Fatalf("invalid save changed runtime routing weights to %v/%v/%v", price, efficiency, quality)
	}
}

func TestRoutingDecisionWeightsReserveMinimumQualityShare(t *testing.T) {
	valid := GatewayConfig{RoutingPriceWeightPercent: 60, RoutingEfficiencyWeightPercent: 35}
	if err := normalizeRoutingDecisionWeights(&valid); err != nil {
		t.Fatalf("minimum quality share rejected: %v", err)
	}
	invalid := GatewayConfig{RoutingPriceWeightPercent: 60, RoutingEfficiencyWeightPercent: 36}
	if err := normalizeRoutingDecisionWeights(&invalid); err == nil {
		t.Fatal("routing weights below minimum quality share were accepted")
	}

	legacy := GatewayConfig{RoutingPriceWeightPercent: 60, RoutingEfficiencyWeightPercent: 40}
	if !migrateLegacyRoutingDecisionWeights(&legacy) {
		t.Fatal("legacy routing weights were not migrated")
	}
	if legacy.RoutingPriceWeightPercent != 60 || legacy.RoutingEfficiencyWeightPercent != 35 {
		t.Fatalf("migrated routing weights = %d/%d, want 60/35", legacy.RoutingPriceWeightPercent, legacy.RoutingEfficiencyWeightPercent)
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
