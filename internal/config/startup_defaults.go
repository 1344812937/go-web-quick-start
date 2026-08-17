package config

import (
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type configFieldPresence struct {
	RoutingQualityWeight bool
	RoutingBalanceWeight bool
}

func detectConfigFieldPresence(content []byte) (configFieldPresence, error) {
	var raw map[string]any
	if err := toml.Unmarshal(content, &raw); err != nil {
		return configFieldPresence{}, err
	}
	return configFieldPresence{
		RoutingQualityWeight: hasTomlKey(raw, "gateway_config", "routing_quality_weight_percent"),
		RoutingBalanceWeight: hasTomlKey(raw, "gateway_config", "routing_balance_weight_percent"),
	}, nil
}

func applyRuntimeFallbacks(cfg *ApplicationConfig) (bool, error) {
	changed := false
	if strings.TrimSpace(cfg.WebConfig.Host) == "" {
		cfg.WebConfig.Host = DefaultWebHost
		changed = true
	}
	if strings.TrimSpace(cfg.WebConfig.Port) == "" {
		cfg.WebConfig.Port = DefaultWebPort
		changed = true
	}
	if strings.TrimSpace(cfg.NodeConfig.SharedToken) == "" {
		generatedToken, err := generateSharedToken()
		if err != nil {
			return false, err
		}
		cfg.NodeConfig.SharedToken = generatedToken
		changed = true
	}
	return changed, nil
}

func hasTomlKey(raw map[string]any, tableName string, key string) bool {
	sectionValue, ok := raw[tableName]
	if !ok {
		return false
	}
	sectionMap, ok := sectionValue.(map[string]any)
	if !ok {
		return false
	}
	_, ok = sectionMap[key]
	return ok
}
