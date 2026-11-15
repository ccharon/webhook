// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2023 Christian Charon

// Package config loads and exposes server configuration read from a JSON file.
// All fields are unexported; callers access values exclusively through getter methods
// so the configuration is effectively immutable after construction.
// All validation happens once in NewConfiguration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ccharon/webhook/internal/jsonx"
)

const (
	minTokenLength = 32
	// placeholderToken is the value shipped in the example config; it must never be used.
	placeholderToken = "REPLACE_WITH_SECRET_MIN_32_CHARS"

	defaultTimeout = 300 * time.Second
	maxTimeoutSecs = 86400

	defaultParamMaxLength = 64
	maxParamMaxLength     = 65536
)

type Configuration struct {
	address        string
	port           int
	token          string
	script         string
	timeout        time.Duration
	paramMaxLength int
}

type configdata struct {
	Server         server `json:"server"`
	Token          string `json:"token"`
	Script         string `json:"script"`
	Timeout        int    `json:"timeout"`
	ParamMaxLength int    `json:"param_max_length"`
}

type server struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

// NewConfiguration reads, parses and validates the JSON file at configLocation.
// A relative script path is converted to an absolute path.
// Timeout (seconds) and param_max_length default to 300 and 64 when omitted or 0.
func NewConfiguration(configLocation string) (*Configuration, error) {
	f, err := os.Open(configLocation)
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	cfg, err := jsonx.Unmarshal[configdata](f)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if cfg.Server.Host == "" {
		return nil, fmt.Errorf("invalid config: server.host must not be empty")
	}
	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		return nil, fmt.Errorf("invalid config: server.port must be between 1 and 65535, got %d", cfg.Server.Port)
	}
	if len(cfg.Token) < minTokenLength {
		return nil, fmt.Errorf("invalid config: token must be at least %d characters, shorter secrets weaken HMAC-SHA256 authentication", minTokenLength)
	}
	if cfg.Token == placeholderToken {
		return nil, fmt.Errorf("invalid config: token is still the placeholder value, set a real secret")
	}

	script, err := validateScript(cfg.Script)
	if err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	if cfg.Timeout < 0 || cfg.Timeout > maxTimeoutSecs {
		return nil, fmt.Errorf("invalid config: timeout must be between 0 and %d seconds, got %d", maxTimeoutSecs, cfg.Timeout)
	}
	timeout := defaultTimeout
	if cfg.Timeout > 0 {
		timeout = time.Duration(cfg.Timeout) * time.Second
	}

	if cfg.ParamMaxLength < 0 || cfg.ParamMaxLength > maxParamMaxLength {
		return nil, fmt.Errorf("invalid config: param_max_length must be between 0 and %d, got %d", maxParamMaxLength, cfg.ParamMaxLength)
	}
	paramMaxLength := defaultParamMaxLength
	if cfg.ParamMaxLength > 0 {
		paramMaxLength = cfg.ParamMaxLength
	}

	return &Configuration{
		address:        cfg.Server.Host,
		port:           cfg.Server.Port,
		token:          cfg.Token,
		script:         script,
		timeout:        timeout,
		paramMaxLength: paramMaxLength,
	}, nil
}

// validateScript checks that path names an existing, executable regular file
// and returns its absolute form.
func validateScript(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("script must not be empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("script path %q: %w", path, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("script: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("script %q is not a regular file", abs)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("script %q is not executable", abs)
	}
	return abs, nil
}

// Address returns the host/IP the server should bind to.
func (c *Configuration) Address() string { return c.address }

// Port returns the TCP port the server should listen on.
func (c *Configuration) Port() int { return c.port }

// Token returns the shared secret used to authenticate incoming requests.
func (c *Configuration) Token() string { return c.token }

// Script returns the absolute path of the shell script to execute on a valid request.
func (c *Configuration) Script() string { return c.script }

// Timeout returns the maximum duration the script is allowed to run.
func (c *Configuration) Timeout() time.Duration { return c.timeout }

// ParamMaxLength returns the maximum allowed byte length of the param field.
func (c *Configuration) ParamMaxLength() int { return c.paramMaxLength }
