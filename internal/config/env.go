package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

const (
	defaultEnvFileName      = ".env"
	GatewayMasterKeyEnvKey  = "GATEWAY_MASTER_KEY"
	GatewayAdminUsernameKey = "GATEWAY_ADMIN_USERNAME"
	GatewayAdminPasswordKey = "GATEWAY_ADMIN_PASSWORD"
	StartOpenWebKey         = "START_OPEN_WEB"
	AllowIframeEnvKey       = "GATEWAY_ALLOW_IFRAME"
)

var requiredRuntimeEnvKeys = []string{
	GatewayMasterKeyEnvKey,
	GatewayAdminUsernameKey,
	GatewayAdminPasswordKey,
}

// LoadRuntimeEnv loads optional process configuration from the working directory.
func LoadRuntimeEnv(args []string) error {
	envPath := filepath.Join(".", defaultEnvFileName)
	if err := loadOrCreateRuntimeEnv(envPath); err != nil {
		return err
	}
	startOpenWeb, err := resolveStartOpenWeb(args, envPath)
	if err != nil {
		return err
	}
	if err := os.Setenv(StartOpenWebKey, strconv.FormatBool(startOpenWeb)); err != nil {
		return fmt.Errorf("set %s: %w", StartOpenWebKey, err)
	}
	_, err = AllowIframeEmbedding()
	return err
}

func loadOrCreateRuntimeEnv(path string) error {
	if err := loadEnvFile(path); err != nil {
		return err
	}
	generated := make(map[string]string)
	for _, key := range requiredRuntimeEnvKeys {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			continue
		}
		value, err := generateRuntimeEnvValue(key)
		if err != nil {
			return fmt.Errorf("generate %s: %w", key, err)
		}
		generated[key] = value
	}
	if len(generated) == 0 {
		return nil
	}
	if err := appendEnvValues(path, generated); err != nil {
		return err
	}
	for _, key := range requiredRuntimeEnvKeys {
		if value, ok := generated[key]; ok {
			if err := os.Setenv(key, value); err != nil {
				return fmt.Errorf("set %s: %w", key, err)
			}
		}
	}
	return nil
}

func generateRuntimeEnvValue(key string) (string, error) {
	size := 32
	if key == GatewayAdminUsernameKey {
		size = 6
	}
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	switch key {
	case GatewayMasterKeyEnvKey:
		return base64.StdEncoding.EncodeToString(raw), nil
	case GatewayAdminUsernameKey:
		return "admin_" + base64.RawURLEncoding.EncodeToString(raw), nil
	case GatewayAdminPasswordKey:
		return "gw_" + base64.RawURLEncoding.EncodeToString(raw), nil
	default:
		return base64.RawURLEncoding.EncodeToString(raw), nil
	}
}

func appendEnvValues(path string, values map[string]string) error {
	content, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	var addition strings.Builder
	if len(content) > 0 && content[len(content)-1] != '\n' {
		addition.WriteByte('\n')
	}
	addition.WriteString("# Automatically generated missing startup values.\n")
	for _, key := range requiredRuntimeEnvKeys {
		if value, ok := values[key]; ok {
			addition.WriteString(key)
			addition.WriteByte('=')
			addition.WriteString(value)
			addition.WriteByte('\n')
		}
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create environment directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return fmt.Errorf("restrict %s: %w", path, err)
	}
	if _, err := file.WriteString(addition.String()); err != nil {
		_ = file.Close()
		return fmt.Errorf("append %s: %w", path, err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
}

// OpenBrowserOnStart reports whether startup should open the local interface.
// The setting defaults to true and accepts the boolean forms supported by strconv.ParseBool.
func OpenBrowserOnStart() (bool, error) {
	raw := strings.TrimSpace(os.Getenv(StartOpenWebKey))
	if raw == "" {
		return true, nil
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be true, false, 1, or 0: %w", StartOpenWebKey, err)
	}
	return enabled, nil
}

// resolveStartOpenWeb applies command line > .env > process environment precedence.
func resolveStartOpenWeb(args []string, envPath string) (bool, error) {
	if value, configured, err := startOpenWebFromArgs(args); err != nil {
		return false, err
	} else if configured {
		return value, nil
	}

	envValues, err := readEnvFileValues(envPath)
	if err != nil {
		return false, err
	}
	if raw, configured := envValues[StartOpenWebKey]; configured {
		return parseStartOpenWeb(raw, ".env")
	}

	if raw, configured := os.LookupEnv(StartOpenWebKey); configured {
		return parseStartOpenWeb(raw, "environment")
	}
	return true, nil
}

func startOpenWebFromArgs(args []string) (bool, bool, error) {
	flags := flag.NewFlagSet("startup", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	value := true
	flags.BoolVar(&value, StartOpenWebKey, true, "open the web interface after startup")
	if err := flags.Parse(args); err != nil {
		return false, false, fmt.Errorf("parse command line %s: %w", StartOpenWebKey, err)
	}
	configured := false
	flags.Visit(func(current *flag.Flag) {
		if current.Name == StartOpenWebKey {
			configured = true
		}
	})
	return value, configured, nil
}

func parseStartOpenWeb(raw string, source string) (bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return true, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s from %s must be true, false, 1, or 0: %w", StartOpenWebKey, source, err)
	}
	return value, nil
}

func readEnvFileValues(path string) (map[string]string, error) {
	values, err := godotenv.Read(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return values, nil
}

// AllowIframeEmbedding reports whether other pages may embed the application.
// The setting defaults to true and accepts the boolean forms supported by strconv.ParseBool.
func AllowIframeEmbedding() (bool, error) {
	raw := strings.TrimSpace(os.Getenv(AllowIframeEnvKey))
	if raw == "" {
		return true, nil
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be true, false, 1, or 0: %w", AllowIframeEnvKey, err)
	}
	return enabled, nil
}

func loadEnvFile(path string) error {
	if err := godotenv.Load(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("load %s: %w", path, err)
	}
	return nil
}
