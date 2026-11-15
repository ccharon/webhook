package config_test

// Tests for NewConfiguration cover successful parsing with defaults, rejection
// of every invalid setting, and file/JSON errors.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ccharon/webhook/internal/config"
)

const goodToken = "test-token-min-32-chars-for-tests"

// writeConfig writes content to a temp config file and returns its path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// makeScript creates a script file with the given mode and returns its path.
func makeScript(t *testing.T, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "deploy.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNewConfiguration(t *testing.T) {
	t.Run("valid config fixture with relative script", func(t *testing.T) {
		// test/config.json uses "./test/deploy.sh", relative to the repo root
		t.Chdir("../..")
		cfg, err := config.NewConfiguration("test/config.json")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Address() != "localhost" {
			t.Errorf("Address: got %q", cfg.Address())
		}
		if cfg.Port() != 6080 {
			t.Errorf("Port: got %d", cfg.Port())
		}
		if cfg.Token() != "this-is-a-local-dev-token-not-for-prod" {
			t.Errorf("Token: got %q", cfg.Token())
		}
		if !filepath.IsAbs(cfg.Script()) || !strings.HasSuffix(cfg.Script(), "/test/deploy.sh") {
			t.Errorf("Script: got %q, want absolute path ending in /test/deploy.sh", cfg.Script())
		}
		if cfg.Timeout() != 300*time.Second {
			t.Errorf("Timeout: got %v", cfg.Timeout())
		}
	})

	t.Run("defaults and configured values", func(t *testing.T) {
		script := makeScript(t, 0o755)
		tests := []struct {
			name        string
			extra       string
			wantTimeout time.Duration
			wantParam   int
		}{
			{"omitted", ``, 300 * time.Second, 64},
			{"zero", `,"timeout":0,"param_max_length":0`, 300 * time.Second, 64},
			{"configured", `,"timeout":10,"param_max_length":128`, 10 * time.Second, 128},
			{"upper bounds", `,"timeout":86400,"param_max_length":65536`, 86400 * time.Second, 65536},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				p := writeConfig(t, `{"server":{"host":"localhost","port":6080},"token":"`+goodToken+`","script":"`+script+`"`+tt.extra+`}`)
				cfg, err := config.NewConfiguration(p)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if cfg.Timeout() != tt.wantTimeout {
					t.Errorf("Timeout: got %v, want %v", cfg.Timeout(), tt.wantTimeout)
				}
				if cfg.ParamMaxLength() != tt.wantParam {
					t.Errorf("ParamMaxLength: got %d, want %d", cfg.ParamMaxLength(), tt.wantParam)
				}
				if cfg.Script() != script {
					t.Errorf("Script: got %q, want %q", cfg.Script(), script)
				}
			})
		}
	})

	t.Run("rejected", func(t *testing.T) {
		script := makeScript(t, 0o755)
		noExec := makeScript(t, 0o644)
		dir := t.TempDir()

		// field builds a config with the given overrides; defaults are valid.
		build := func(host, port, token, scr, extra string) string {
			return `{"server":{"host":"` + host + `","port":` + port + `},"token":"` + token + `","script":"` + scr + `"` + extra + `}`
		}
		tests := []struct {
			name    string
			content string
			wantErr string
		}{
			{"empty host", build("", "6080", goodToken, script, ""), "host"},
			{"port zero", build("localhost", "0", goodToken, script, ""), "port"},
			{"port negative", build("localhost", "-1", goodToken, script, ""), "port"},
			{"port too high", build("localhost", "65536", goodToken, script, ""), "port"},
			{"token too short", build("localhost", "6080", "tooshort", script, ""), "token"},
			{"placeholder token", build("localhost", "6080", "REPLACE_WITH_SECRET_MIN_32_CHARS", script, ""), "placeholder"},
			{"empty script", build("localhost", "6080", goodToken, "", ""), "script"},
			{"missing script", build("localhost", "6080", goodToken, filepath.Join(dir, "nope.sh"), ""), "script"},
			{"script is directory", build("localhost", "6080", goodToken, dir, ""), "regular file"},
			{"script not executable", build("localhost", "6080", goodToken, noExec, ""), "not executable"},
			{"negative timeout", build("localhost", "6080", goodToken, script, `,"timeout":-1`), "timeout"},
			{"timeout too large", build("localhost", "6080", goodToken, script, `,"timeout":86401`), "timeout"},
			{"negative param_max_length", build("localhost", "6080", goodToken, script, `,"param_max_length":-1`), "param_max_length"},
			{"param_max_length too large", build("localhost", "6080", goodToken, script, `,"param_max_length":65537`), "param_max_length"},
			{"unknown field", build("localhost", "6080", goodToken, script, `,"unknown":"field"`), "unknown field"},
			{"invalid JSON", `{invalid}`, "parse config"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := config.NewConfiguration(writeConfig(t, tt.content))
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q does not contain %q", err, tt.wantErr)
				}
			})
		}
	})

	t.Run("non-existent file", func(t *testing.T) {
		if _, err := config.NewConfiguration("/nonexistent/config.json"); err == nil {
			t.Fatal("expected error for non-existent file, got nil")
		}
	})
}
