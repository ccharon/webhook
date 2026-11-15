// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2023 Christian Charon

// Package deploy executes a configured shell script in response to a trigger.
// Only one execution is allowed at a time; concurrent requests are dropped.
package deploy

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ccharon/webhook/internal/config"
)

// KillDelay is how long a timed out or cancelled script gets after SIGTERM
// before it is killed.
const KillDelay = 10 * time.Second

type Deployment struct {
	configuration *config.Configuration
	killDelay     time.Duration

	// ctx is cancelled when Shutdown gives up waiting; it aborts a running script.
	ctx    context.Context
	cancel context.CancelFunc

	mu      sync.Mutex
	running bool
	closed  bool
	wg      sync.WaitGroup
}

// NewDeployment creates a Deployment that runs the script defined in configuration.
func NewDeployment(configuration *config.Configuration) *Deployment {
	ctx, cancel := context.WithCancel(context.Background())
	return &Deployment{configuration: configuration, killDelay: KillDelay, ctx: ctx, cancel: cancel}
}

// Execute starts the deployment in a background goroutine and returns true.
// It returns false without starting anything if a deployment is already running
// or Shutdown has been called.
func (d *Deployment) Execute(id string, param string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.running || d.closed {
		return false
	}
	d.running = true
	// Add happens under mu before closed is set, so it never races with Shutdown's Wait.
	d.wg.Go(func() { d.runScript(id, param) })
	return true
}

// Shutdown makes further Execute calls fail and waits for a running deployment to finish.
// If ctx ends first, the script's process group receives SIGTERM, then SIGKILL after
// KillDelay; Shutdown returns ctx.Err() once the script is gone.
func (d *Deployment) Shutdown(ctx context.Context) error {
	d.mu.Lock()
	d.closed = true
	d.mu.Unlock()
	defer d.cancel()

	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
	}
	select {
	case <-done: // finished at the deadline, nothing to abort
		return nil
	default:
	}
	d.cancel()
	<-done
	return ctx.Err()
}

func (d *Deployment) runScript(id string, param string) {
	defer func() {
		d.mu.Lock()
		d.running = false
		d.mu.Unlock()
	}()

	log := slog.With("script", d.configuration.Script(), "id", id, "param", param)
	log.Info("running script")

	ctx, cancel := context.WithTimeout(d.ctx, d.configuration.Timeout())
	defer cancel()

	cmd := exec.CommandContext(ctx, d.configuration.Script())
	cmd.Env = append(scriptEnv(), "WEBHOOK_ID="+id, "WEBHOOK_PARAM="+param)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// own process group, so docker/curl/rsync started by the script can be stopped together
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// on timeout or shutdown: SIGTERM to the whole group; if the script is still
	// alive after WaitDelay, os/exec kills it
	cmd.Cancel = func() error { return signalGroup(cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = d.killDelay

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start).Round(time.Millisecond)

	stopped := err != nil && ctx.Err() != nil && cmd.Process != nil
	if stopped {
		// Whatever the aborted script left behind is killed now, so no leftovers
		// overlap with the next deployment. A group id stays reserved while the
		// group has members, so this cannot reach an unrelated process.
		_ = signalGroup(cmd.Process.Pid, syscall.SIGKILL)
	}

	var exitErr *exec.ExitError
	switch {
	case stopped && d.ctx.Err() != nil:
		log.Warn("script terminated by shutdown", "duration", duration)
	case stopped:
		log.Error("script timed out and was terminated", "timeout", d.configuration.Timeout())
	case err == nil:
		log.Info("script finished", "duration", duration)
	case errors.As(err, &exitErr):
		log.Error("script failed", "exit_code", exitErr.ExitCode(), "err", err, "duration", duration)
	default:
		log.Error("script failed", "err", err, "duration", duration)
	}
}

// signalGroup sends sig to every process in the process group pgid.
func signalGroup(pgid int, sig syscall.Signal) error {
	err := syscall.Kill(-pgid, sig)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
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
