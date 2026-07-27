package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

const (
	defaultEnvFileName       = ".env"
	OpenBrowserOnStartEnvKey = "GATEWAY_OPEN_BROWSER"
)

// LoadRuntimeEnv loads optional process configuration from the working directory.
// Existing environment variables take precedence over values declared in .env.
func LoadRuntimeEnv() error {
	if err := loadEnvFile(filepath.Join(".", defaultEnvFileName)); err != nil {
		return err
	}
	_, err := OpenBrowserOnStart()
	return err
}

// OpenBrowserOnStart reports whether startup should open the local interface.
// The setting defaults to true and accepts the boolean forms supported by strconv.ParseBool.
func OpenBrowserOnStart() (bool, error) {
	raw := strings.TrimSpace(os.Getenv(OpenBrowserOnStartEnvKey))
	if raw == "" {
		return true, nil
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be true, false, 1, or 0: %w", OpenBrowserOnStartEnvKey, err)
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
