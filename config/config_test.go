package config_test

// Tests for NewConfiguration cover the three outcomes: successful parse with correct
// getter values, file-not-found, and malformed/unknown-field JSON.

import (
	"os"
	"path/filepath"
	"testing"

	"webhook/config"
)

// TestNewConfiguration verifies that a valid config file is parsed correctly
// and that errors are returned for missing files and invalid JSON.
func TestNewConfiguration(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		// uses test/config.json as a representative real-world fixture
		cfg, err := config.NewConfiguration("../test/config.json")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Address() != "localhost" {
			t.Errorf("Address: got %q, want %q", cfg.Address(), "localhost")
		}
		if cfg.Port() != 6080 {
			t.Errorf("Port: got %d, want %d", cfg.Port(), 6080)
		}
		if cfg.Token() != "this-is-a-local-dev-token-not-for-prod" {
			t.Errorf("Token: got %q, want %q", cfg.Token(), "this-is-a-local-dev-token-not-for-prod")
		}
		if cfg.Script() != "./test/deploy.sh" {
			t.Errorf("Script: got %q, want %q", cfg.Script(), "./test/deploy.sh")
		}
	})

	t.Run("token too short", func(t *testing.T) {
		// a token shorter than 32 characters must be rejected at load time
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.json")
		content := `{"server":{"host":"localhost","port":6080},"token":"tooshort","script":"/bin/sh","timeout":300}`
		if err := os.WriteFile(cfgPath, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := config.NewConfiguration(cfgPath)
		if err == nil {
			t.Fatal("expected error for short token, got nil")
		}
	})

	t.Run("non-existent file", func(t *testing.T) {
		_, err := config.NewConfiguration("/nonexistent/config.json")
		if err == nil {
			t.Fatal("expected error for non-existent file, got nil")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		f, err := os.CreateTemp("", "config-*.json")
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(f.Name())
		f.WriteString(`{invalid}`)
		f.Close()

		_, err = config.NewConfiguration(f.Name())
		if err == nil {
			t.Fatal("expected error for invalid JSON, got nil")
		}
	})

	t.Run("param_max_length default", func(t *testing.T) {
		// omitting param_max_length must return the safe default of 64
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.json")
		content := `{"server":{"host":"localhost","port":6080},"token":"test-token-min-32-chars-for-tests","script":"/bin/sh","timeout":300}`
		if err := os.WriteFile(cfgPath, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.NewConfiguration(cfgPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.ParamMaxLength() != 64 {
			t.Errorf("ParamMaxLength: got %d, want 64", cfg.ParamMaxLength())
		}
	})

	t.Run("param_max_length configured", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.json")
		content := `{"server":{"host":"localhost","port":6080},"token":"test-token-min-32-chars-for-tests","script":"/bin/sh","timeout":300,"param_max_length":128}`
		if err := os.WriteFile(cfgPath, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.NewConfiguration(cfgPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.ParamMaxLength() != 128 {
			t.Errorf("ParamMaxLength: got %d, want 128", cfg.ParamMaxLength())
		}
	})

	t.Run("param_max_length clamped to max", func(t *testing.T) {
		// values above 65536 are clamped so a misconfigured server can't open the input wider than intended
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.json")
		content := `{"server":{"host":"localhost","port":6080},"token":"test-token-min-32-chars-for-tests","script":"/bin/sh","timeout":300,"param_max_length":100000}`
		if err := os.WriteFile(cfgPath, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.NewConfiguration(cfgPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.ParamMaxLength() != 65536 {
			t.Errorf("ParamMaxLength: got %d, want 65536", cfg.ParamMaxLength())
		}
	})

	t.Run("unknown field", func(t *testing.T) {
		// DisallowUnknownFields must reject a config with extra keys so that
		// typos in the config file don't silently produce a zero-value field
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.json")
		content := `{"server":{"host":"localhost","port":6080},"token":"test-token-min-32-chars-for-tests","script":"/bin/sh","unknown":"field"}`
		if err := os.WriteFile(cfgPath, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := config.NewConfiguration(cfgPath)
		if err == nil {
			t.Fatal("expected error for unknown field, got nil")
		}
	})
}
