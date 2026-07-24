package config

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/1344812937/go-web-quick-start/internal/projectmeta"
	"github.com/1344812937/go-web-quick-start/pkg/until"
	"os"
	"path/filepath"

	"github.com/creasty/defaults"
	"github.com/pelletier/go-toml/v2"
)

var log = until.Log
var configPath = filepath.Join("./", "config", "config.toml")

type ApplicationConfigManager struct {
	config *ApplicationConfig
}

func (acm *ApplicationConfigManager) GetConfig() *ApplicationConfig {
	if acm.config == nil {
		acm.Load()
	}
	return acm.config
}

func (acm *ApplicationConfigManager) Load() {
	firstRun := false
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		createDefaultConfig(configPath)
		firstRun = true
	}
	content, err := os.ReadFile(configPath)
	if err != nil {
		panic("读取配置文件失败")
	}
	presence, err := detectConfigFieldPresence(content)
	if err != nil {
		panic(fmt.Sprintf("解析配置项存在性失败: %v", err))
	}

	// 解析配置文件
	var cfg ApplicationConfig
	if err := defaults.Set(&cfg); err != nil {
		panic(err)
	}
	if err := toml.Unmarshal(content, &cfg); err != nil {
		panic("解析配置文件失败")
	}

	guidePlan, err := buildStartupGuidePlan(firstRun, presence, &cfg)
	if err != nil {
		panic(fmt.Sprintf("构建启动配置引导失败: %v", err))
	}
	if guidePlan.HasQuestions() {
		if canPromptForConfig() {
			if guideErr := runStartupConfigGuide(configPath, &cfg, guidePlan); guideErr != nil {
				panic(fmt.Sprintf("启动配置引导失败: %v", guideErr))
			}
		} else {
			log.Warn("检测到缺失启动配置，但当前环境不是交互式终端，已按默认值或自动生成值继续启动")
		}
	}
	needRewrite, err := applyRuntimeFallbacks(&cfg, guidePlan)
	if err != nil {
		panic(fmt.Sprintf("补全启动配置失败: %v", err))
	}
	if firstRun || guidePlan.HasQuestions() || needRewrite {
		if writeErr := writeConfigFile(configPath, &cfg); writeErr != nil {
			panic(fmt.Sprintf("写入配置文件失败: %v", writeErr))
		}
	}

	acm.config = &cfg
}

func NewApplicationConfigManager() *ApplicationConfigManager {
	configManager := &ApplicationConfigManager{}
	configManager.Load()
	return configManager
}

type ApplicationConfig struct {
	WebConfig  WebConfig  `toml:"web_config" json:"webConfig"`
	NodeConfig NodeConfig `toml:"node_config" json:"nodeConfig"`
	AuthConfig AuthConfig `toml:"auth_config" json:"authConfig"`
}

type WebConfig struct {
	Host string `toml:"host" json:"host" default:"localhost"`
	Port string `toml:"port" json:"port" default:"8888"`
}

type NodeConfig struct {
	SharedToken string `toml:"shared_token" json:"sharedToken"`
}

type AuthConfig struct {
	AccessToken string `toml:"access_token" json:"accessToken"`
}

func (acm *ApplicationConfigManager) Save(cfg *ApplicationConfig) error {
	if cfg == nil {
		return errors.New("配置不能为空")
	}
	if err := writeConfigFile(configPath, cfg); err != nil {
		return err
	}
	acm.config = cfg
	return nil
}

func generateSharedToken() (string, error) {
	return generatePrefixedToken(projectmeta.TokenPrefix)
}

func generateAccessToken() (string, error) {
	return generatePrefixedToken("access")
}

func generatePrefixedToken(prefix string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func encodeConfig(cfg *ApplicationConfig) ([]byte, error) {
	var buf bytes.Buffer
	encoder := toml.NewEncoder(&buf)
	encoder.SetIndentTables(true)
	encoder.SetTablesInline(false)
	if err := encoder.Encode(cfg); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeConfigFile(filePath string, cfg *ApplicationConfig) error {
	content, err := encodeConfig(cfg)
	if err != nil {
		return err
	}
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filePath, content, 0644)
}

func createDefaultConfig(filePath string) *ApplicationConfig {
	defaultConfig := &ApplicationConfig{}
	err := defaults.Set(defaultConfig)
	if err != nil {
		panic(err)
	}
	if err := writeConfigFile(filePath, defaultConfig); err != nil {
		panic(err)
	}
	return defaultConfig
}
