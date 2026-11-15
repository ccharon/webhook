// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2023 Christian Charon

// Package deploy executes a configured shell script in response to a trigger.
// Only one execution is allowed at a time; concurrent requests are dropped.
package deploy

import (
	"context"
	"github.com/ccharon/webhook/internal/config"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync/atomic"
)

type Deployment struct {
	configuration *config.Configuration
	deployRunning atomic.Bool
}

// NewDeployment creates a Deployment that runs the script defined in configuration.
func NewDeployment(configuration *config.Configuration) *Deployment {
	return &Deployment{configuration: configuration}
}

// Execute starts the deployment in a background goroutine and returns true.
// If a deployment is already running it returns false immediately without starting a new one.
// CompareAndSwap makes the check-and-set atomic, preventing a race between concurrent callers.
func (d *Deployment) Execute(id string, param string) (started bool) {
	if d.deployRunning.CompareAndSwap(false, true) {
		go d.runScript(id, param)
		return true
	}
	return false
}

func (d *Deployment) runScript(id string, param string) {
	defer d.deployRunning.Store(false)

	slog.Info("running script", "script", d.configuration.Script(), "id", id, "param", param)

	ctx, cancel := context.WithTimeout(context.Background(), d.configuration.Timeout())
	defer cancel()

	cmd := exec.CommandContext(ctx, d.configuration.Script())
	cmd.Env = append(scriptEnv(), "WEBHOOK_ID="+id, "WEBHOOK_PARAM="+param)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		slog.Error("command execution failed", "err", err)
	}
}

// scriptEnv returns a minimal environment for the subprocess.
// Forwarding os.Environ() entirely risks leaking server-side secrets
// (API keys, passwords) that happen to be in the server's environment.
// Only variables a deployment script is likely to need are passed through.
func scriptEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		switch {
		case slices.Contains(allowedEnv, key),
			strings.HasPrefix(key, "LANG"),
			strings.HasPrefix(key, "LC_"):
			env = append(env, kv)
		}
	}
	return env
}

var allowedEnv = []string{"PATH", "HOME", "USER", "LOGNAME", "SHELL", "TERM", "TZ"}
