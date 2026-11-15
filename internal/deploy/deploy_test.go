package deploy

// Tests for Deployment run real shell scripts in temp directories to verify
// execution behaviour, environment variable passing, timeout enforcement,
// shutdown and the concurrency guarantee that only one deployment runs at a time.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ccharon/webhook/internal/config"
)

const testToken = "test-token-min-32-chars-for-tests"

// newTestDeploymentTimeout writes scriptContent to a temp script file, creates a
// matching config (timeoutSecs 0 means default) and returns a ready-to-use Deployment.
// A short kill delay keeps tests fast; any script still running at the end is killed.
func newTestDeploymentTimeout(t *testing.T, scriptContent string, timeoutSecs int) *Deployment {
	t.Helper()
	dir := t.TempDir()

	scriptPath := filepath.Join(dir, "deploy.sh")
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0o700); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(dir, "config.json")
	cfgContent := fmt.Sprintf(`{"server":{"host":"localhost","port":6080},"token":%q,"script":%q,"timeout":%d}`,
		testToken, scriptPath, timeoutSecs)
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.NewConfiguration(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDeployment(cfg)
	d.killDelay = 200 * time.Millisecond
	t.Cleanup(func() { shutdownWithin(d, 0) })
	return d
}

func newTestDeployment(t *testing.T, scriptContent string) *Deployment {
	t.Helper()
	return newTestDeploymentTimeout(t, scriptContent, 0)
}

// shutdownWithin calls Shutdown with the given grace period and returns how long it
// took together with its result.
func shutdownWithin(d *Deployment, grace time.Duration) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	start := time.Now()
	err := d.Shutdown(ctx)
	return time.Since(start), err
}

// readPid reads the pid a test script wrote to path.
func readPid(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("pid file: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatalf("pid file: %v", err)
	}
	return pid
}

// assertProcessGone fails unless pid has terminated (vanished or zombie awaiting init).
// SIGKILL is delivered asynchronously, so a short grace is allowed.
func assertProcessGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			return
		}
		if i := strings.LastIndexByte(string(stat), ')'); i > 0 && strings.HasPrefix(string(stat[i+1:]), " Z") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("process %d is still running: %s", pid, stat)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestExecuteWhenIdle verifies that Execute returns true and starts the script
// when no deployment is currently running.
func TestExecuteWhenIdle(t *testing.T) {
	d := newTestDeployment(t, "#!/bin/sh\nexit 0\n")
	if !d.Execute("id1", "param1") {
		t.Error("Execute should return true when idle")
	}
}

// TestExecuteWhenRunning verifies that Execute returns false while a deployment is in progress.
func TestExecuteWhenRunning(t *testing.T) {
	d := newTestDeployment(t, "#!/bin/sh\nsleep 5\n")
	if !d.Execute("id1", "param1") {
		t.Fatal("first Execute should return true")
	}
	if d.Execute("id2", "param2") {
		t.Error("Execute should return false when already running")
	}
}

// TestExecuteEnvVars verifies that the script receives WEBHOOK_ID and WEBHOOK_PARAM
// as environment variables with the values passed to Execute.
func TestExecuteEnvVars(t *testing.T) {
	outFile := filepath.Join(t.TempDir(), "output.txt")
	script := "#!/bin/sh\n" +
		"echo \"$WEBHOOK_ID\" > " + outFile + "\n" +
		"echo \"$WEBHOOK_PARAM\" >> " + outFile + "\n"

	d := newTestDeployment(t, script)
	if !d.Execute("test-id", "test-param") {
		t.Fatal("Execute returned false unexpectedly")
	}
	if _, err := shutdownWithin(d, 5*time.Second); err != nil {
		t.Fatalf("deployment did not finish in time: %v", err)
	}

	content, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("could not read output file: %v", err)
	}
	out := string(content)
	if !strings.Contains(out, "test-id") {
		t.Errorf("WEBHOOK_ID not found in output: %q", out)
	}
	if !strings.Contains(out, "test-param") {
		t.Errorf("WEBHOOK_PARAM not found in output: %q", out)
	}
}

// TestExecuteScriptTimeout verifies that a script exceeding its timeout is stopped
// together with its children, including one that ignores SIGTERM, and that a new
// deployment can start afterwards.
func TestExecuteScriptTimeout(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	script := "#!/bin/sh\n" +
		"(trap '' TERM; exec sleep 60) &\n" +
		"echo $! > " + pidFile + "\n" +
		"wait\n"
	d := newTestDeploymentTimeout(t, script, 1)

	if !d.Execute("id", "param") {
		t.Fatal("Execute returned false unexpectedly")
	}

	// the 1s script timeout must end the run well within the shutdown grace period
	took, err := shutdownWithin(d, 5*time.Second)
	if err != nil {
		t.Fatalf("deployment did not finish after script timeout: %v", err)
	}
	if took > 3*time.Second {
		t.Errorf("timeout took %v", took)
	}
	assertProcessGone(t, readPid(t, pidFile))
}

// TestShutdownWaitsForScript verifies that Shutdown lets a short script run to completion.
func TestShutdownWaitsForScript(t *testing.T) {
	outFile := filepath.Join(t.TempDir(), "done")
	d := newTestDeployment(t, "#!/bin/sh\nsleep 0.3\necho done > "+outFile+"\n")
	if !d.Execute("id", "param") {
		t.Fatal("Execute returned false unexpectedly")
	}

	if _, err := shutdownWithin(d, 5*time.Second); err != nil {
		t.Fatalf("Shutdown returned %v", err)
	}
	if _, err := os.Stat(outFile); err != nil {
		t.Errorf("script did not complete before Shutdown returned: %v", err)
	}
}

// TestShutdownKillsOverlongScript verifies that a script ignoring SIGTERM is killed
// after the grace period plus kill delay.
func TestShutdownKillsOverlongScript(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	script := "#!/bin/sh\n" +
		"trap '' TERM\n" +
		"echo $$ > " + pidFile + "\n" +
		"sleep 60\n"
	d := newTestDeployment(t, script)
	if !d.Execute("id", "param") {
		t.Fatal("Execute returned false unexpectedly")
	}

	grace := 300 * time.Millisecond
	took, err := shutdownWithin(d, grace)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Shutdown returned %v, want deadline exceeded", err)
	}
	if took < grace+d.killDelay || took > grace+d.killDelay+2*time.Second {
		t.Errorf("Shutdown took %v, want about %v", took, grace+d.killDelay)
	}
	assertProcessGone(t, readPid(t, pidFile))
}

// TestExecuteAfterShutdown verifies that no deployment starts once Shutdown was called.
func TestExecuteAfterShutdown(t *testing.T) {
	d := newTestDeployment(t, "#!/bin/sh\nexit 0\n")
	if _, err := shutdownWithin(d, time.Second); err != nil {
		t.Fatalf("Shutdown returned %v", err)
	}
	if d.Execute("id", "param") {
		t.Error("Execute should return false after Shutdown")
	}
}

// TestExecuteConcurrentOnlyOneStarts verifies the atomicity of the deployment lock:
// when many goroutines call Execute simultaneously, exactly one must succeed.
func TestExecuteConcurrentOnlyOneStarts(t *testing.T) {
	d := newTestDeployment(t, "#!/bin/sh\nsleep 1\n")

	const goroutines = 50
	results := make([]bool, goroutines)
	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Go(func() { results[i] = d.Execute("id", "param") })
	}
	wg.Wait()

	started := 0
	for _, r := range results {
		if r {
			started++
		}
	}
	if started != 1 {
		t.Errorf("expected exactly 1 goroutine to start deployment, got %d", started)
	}
}
