// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2023 Christian Charon

package deploy

// Package deploy executes a configured shell script in response to a trigger.
// Only one execution is allowed at a time; concurrent requests are dropped.

import (
	"context"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"
	"webhook/config"
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
	log.Printf("running script %s", d.configuration.Script())

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(d.configuration.Timeout())*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, d.configuration.Script())
	cmd.Env = append(scriptEnv(), "WEBHOOK_ID="+id, "WEBHOOK_PARAM="+param)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		log.Println("command execution failed: ", err)
	}

	d.deployRunning.Store(false)
}

// scriptEnv returns a minimal environment for the subprocess.
// Forwarding os.Environ() entirely risks leaking server-side secrets
// (API keys, passwords) that happen to be in the server's environment.
// Only variables a deployment script is likely to need are passed through.
func scriptEnv() []string {
	allowed := map[string]bool{
		"PATH": true, "HOME": true, "USER": true, "LOGNAME": true,
		"SHELL": true, "TERM": true, "TZ": true,
	}
	var env []string
	for _, kv := range os.Environ() {
		key := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			key = kv[:i]
		}
		if allowed[key] || strings.HasPrefix(key, "LANG") || strings.HasPrefix(key, "LC_") {
			env = append(env, kv)
		}
	}
	return env
}
