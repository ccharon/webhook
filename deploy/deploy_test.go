package deploy

// Tests for Deployment run real shell scripts in temp directories to verify
// execution behaviour, environment variable passing, timeout enforcement,
// and the concurrency guarantee that only one deployment runs at a time.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"webhook/config"
)

// newTestDeployment writes scriptContent to a temp script file, creates a matching
// config, and returns a ready-to-use Deployment. The script path is also returned
// for tests that need to reference it directly.
func newTestDeployment(t *testing.T, scriptContent string) (*Deployment, string) {
	t.Helper()
	dir := t.TempDir()

	scriptPath := filepath.Join(dir, "deploy.sh")
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0700); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(dir, "config.json")
	cfgContent := fmt.Sprintf(`{"server":{"host":"localhost","port":6080},"token":"x","script":%q}`, scriptPath)
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.NewConfiguration(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	return NewDeployment(cfg), scriptPath
}

// waitForIdle polls deployRunning until it becomes false or the timeout expires.
// Polling is necessary because runScript runs in a goroutine with no notification channel.
func waitForIdle(d *Deployment, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for d.deployRunning.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	return !d.deployRunning.Load()
}

// TestExecuteWhenIdle verifies that Execute returns true and starts the script
// when no deployment is currently running.
func TestExecuteWhenIdle(t *testing.T) {
	d, _ := newTestDeployment(t, "#!/bin/sh\nexit 0\n")
	if !d.Execute("id1", "param1") {
		t.Error("Execute should return true when idle")
	}
}

// TestExecuteWhenRunning verifies that Execute returns false when a deployment
// is already in progress. deployRunning is set directly to simulate a running deployment
// without needing a long-lived script.
func TestExecuteWhenRunning(t *testing.T) {
	d, _ := newTestDeployment(t, "#!/bin/sh\nsleep 5\n")
	d.deployRunning.Store(true)
	if d.Execute("id1", "param1") {
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

	d, _ := newTestDeployment(t, script)
	if !d.Execute("test-id", "test-param") {
		t.Fatal("Execute returned false unexpectedly")
	}

	if !waitForIdle(d, 5*time.Second) {
		t.Fatal("deployment did not finish in time")
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

// TestExecuteScriptTimeout verifies that a script running beyond the configured
// timeout is killed and that deployRunning is reset to false afterwards.
func TestExecuteScriptTimeout(t *testing.T) {
	dir := t.TempDir()

	scriptPath := filepath.Join(dir, "slow.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nsleep 60\n"), 0700); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(dir, "config.json")
	cfgContent := fmt.Sprintf(`{"server":{"host":"localhost","port":6080},"token":"x","script":%q,"timeout":1}`, scriptPath)
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.NewConfiguration(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDeployment(cfg)

	if !d.Execute("id", "param") {
		t.Fatal("Execute returned false unexpectedly")
	}

	// script should be killed after 1s; allow 3s for test timing
	if !waitForIdle(d, 3*time.Second) {
		t.Error("deployment did not finish after script timeout")
	}
}

// TestExecuteConcurrentOnlyOneStarts verifies the atomicity of the deployment lock:
// when many goroutines call Execute simultaneously, exactly one must succeed.
func TestExecuteConcurrentOnlyOneStarts(t *testing.T) {
	d, _ := newTestDeployment(t, "#!/bin/sh\nsleep 1\n")

	const goroutines = 50
	results := make([]bool, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			results[i] = d.Execute("id", "param")
		}(i)
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
