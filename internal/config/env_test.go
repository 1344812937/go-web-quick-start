package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvFileLoadsMissingEnvironmentValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("GATEWAY_ENV_TEST=file-value\n"), 0600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	t.Setenv("GATEWAY_ENV_TEST", "")
	if err := os.Unsetenv("GATEWAY_ENV_TEST"); err != nil {
		t.Fatalf("os.Unsetenv() error = %v", err)
	}

	if err := loadEnvFile(path); err != nil {
		t.Fatalf("loadEnvFile() error = %v", err)
	}
	if got := os.Getenv("GATEWAY_ENV_TEST"); got != "file-value" {
		t.Fatalf("GATEWAY_ENV_TEST = %q, want %q", got, "file-value")
	}
}

func TestLoadEnvFilePreservesExistingEnvironmentValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("GATEWAY_ENV_TEST=file-value\n"), 0600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	t.Setenv("GATEWAY_ENV_TEST", "process-value")

	if err := loadEnvFile(path); err != nil {
		t.Fatalf("loadEnvFile() error = %v", err)
	}
	if got := os.Getenv("GATEWAY_ENV_TEST"); got != "process-value" {
		t.Fatalf("GATEWAY_ENV_TEST = %q, want %q", got, "process-value")
	}
}

func TestLoadEnvFileAllowsMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := loadEnvFile(path); err != nil {
		t.Fatalf("loadEnvFile() error = %v", err)
	}
}

func TestLoadEnvFileRejectsMalformedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("INVALID LINE\n"), 0600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	if err := loadEnvFile(path); err == nil {
		t.Fatal("loadEnvFile() error = nil, want parse error")
	}
}

func TestOpenBrowserOnStart(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		unset     bool
		want      bool
		wantError bool
	}{
		{name: "unset defaults enabled", unset: true, want: true},
		{name: "empty defaults enabled", value: "", want: true},
		{name: "true", value: "true", want: true},
		{name: "one", value: "1", want: true},
		{name: "false", value: "false", want: false},
		{name: "zero", value: "0", want: false},
		{name: "invalid", value: "disabled", wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(OpenBrowserOnStartEnvKey, test.value)
			if test.unset {
				if err := os.Unsetenv(OpenBrowserOnStartEnvKey); err != nil {
					t.Fatalf("os.Unsetenv() error = %v", err)
				}
			}
			got, err := OpenBrowserOnStart()
			if test.wantError {
				if err == nil {
					t.Fatal("OpenBrowserOnStart() error = nil, want an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("OpenBrowserOnStart() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("OpenBrowserOnStart() = %t, want %t", got, test.want)
			}
		})
	}
}
