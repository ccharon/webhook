package hook_test

// Tests for the replay cache. The hook's clock is replaced via SetNow
// (export_test.go) so expiry can be tested without sleeping.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ccharon/webhook/internal/hook"
)

// fakeClock is a manually advanced clock, safe for concurrent use.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newReplayHook(t *testing.T, exec *mockExecutor) (*hook.Hook, *fakeClock) {
	t.Helper()
	clock := &fakeClock{t: time.Unix(1_800_000_000, 0)}
	h := hook.NewHook(newTestConfig(t), exec)
	h.SetNow(clock.Now)
	return h, clock
}

func bodyAt(ts int64) string {
	return fmt.Sprintf(`{"id":"1","param":"echoip","unix_seconds":%d}`, ts)
}

// send posts body signed with header sig ("" = correct signature) and returns status and message.
func send(t *testing.T, h *hook.Hook, body, sig string) (int, string) {
	t.Helper()
	if sig == "" {
		sig = signBody(body, testSecret)
	}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", sig)
	w := httptest.NewRecorder()
	h.HandleRequest(w, req)
	var resp struct{ Status, Message string }
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not JSON: %q", w.Body.String())
	}
	return w.Code, resp.Message
}

func TestReplayRejected(t *testing.T) {
	exec := &mockExecutor{}
	h, clock := newReplayHook(t, exec)
	body := bodyAt(clock.Now().Unix())

	if code, msg := send(t, h, body, ""); code != http.StatusOK {
		t.Fatalf("first request: got %d %q, want 200", code, msg)
	}
	clock.Advance(5 * time.Second)
	code, msg := send(t, h, body, "")
	if code != http.StatusForbidden || msg != "request was already processed" {
		t.Errorf("replay: got %d %q, want 403 %q", code, msg, "request was already processed")
	}
	// the same MAC spelled in upper-case hex is still the same request
	code, msg = send(t, h, body, "sha256="+strings.ToUpper(signBody(body, testSecret)[len("sha256="):]))
	if code != http.StatusForbidden || msg != "request was already processed" {
		t.Errorf("replay with upper-case hex: got %d %q, want 403 replay", code, msg)
	}
	if n := exec.calls.Load(); n != 1 {
		t.Errorf("executor calls: got %d, want 1", n)
	}
}

func TestReplayAfterBusyRejected(t *testing.T) {
	exec := &mockExecutor{busy: true}
	h, clock := newReplayHook(t, exec)
	body := bodyAt(clock.Now().Unix())

	if code, _ := send(t, h, body, ""); code != http.StatusTooManyRequests {
		t.Fatalf("first request: got %d, want 429", code)
	}
	exec.busy = false // the running deployment finished
	if code, msg := send(t, h, body, ""); code != http.StatusForbidden || msg != "request was already processed" {
		t.Errorf("replay after 429: got %d %q, want 403 replay", code, msg)
	}
	if n := exec.calls.Load(); n != 1 {
		t.Errorf("executor calls: got %d, want 1", n)
	}
}

func TestNewTimestampAccepted(t *testing.T) {
	exec := &mockExecutor{}
	h, clock := newReplayHook(t, exec)
	now := clock.Now().Unix()

	for _, ts := range []int64{now, now + 1} {
		if code, msg := send(t, h, bodyAt(ts), ""); code != http.StatusOK {
			t.Errorf("ts %d: got %d %q, want 200", ts, code, msg)
		}
	}
	if n := exec.calls.Load(); n != 2 {
		t.Errorf("executor calls: got %d, want 2", n)
	}
}

// TestReplayCacheExpiry checks that an entry lives exactly as long as its timestamp
// passes the window check: up to and including ts+30, then the timestamp check
// itself takes over and the entry is pruned on the next insert.
func TestReplayCacheExpiry(t *testing.T) {
	exec := &mockExecutor{}
	h, clock := newReplayHook(t, exec)
	start := clock.Now().Unix()
	old := bodyAt(start)

	if code, _ := send(t, h, old, ""); code != http.StatusOK {
		t.Fatalf("first request: got %d, want 200", code)
	}

	// last second at which the timestamp is valid: still a replay
	clock.Advance(30*time.Second + 999*time.Millisecond)
	if code, msg := send(t, h, old, ""); code != http.StatusForbidden || msg != "request was already processed" {
		t.Errorf("at ts+30: got %d %q, want 403 replay", code, msg)
	}

	// one second later the timestamp window rejects it
	clock.Advance(1 * time.Millisecond)
	if code, msg := send(t, h, old, ""); code != http.StatusForbidden || !strings.HasPrefix(msg, "unix_seconds must be within") {
		t.Errorf("at ts+31: got %d %q, want 403 timestamp", code, msg)
	}
	if n := h.SeenLen(); n != 1 {
		t.Errorf("cache before next insert: got %d entries, want 1 (pruning is lazy)", n)
	}

	// a fresh request triggers pruning of the expired entry
	if code, _ := send(t, h, bodyAt(clock.Now().Unix()), ""); code != http.StatusOK {
		t.Fatalf("fresh request: got %d, want 200", code)
	}
	if n := h.SeenLen(); n != 1 {
		t.Errorf("cache after prune: got %d entries, want 1", n)
	}
	if n := exec.calls.Load(); n != 2 {
		t.Errorf("executor calls: got %d, want 2", n)
	}
}

// TestConcurrentIdenticalRequests fires the same signed request from many
// goroutines at once; exactly one may reach the executor.
func TestConcurrentIdenticalRequests(t *testing.T) {
	exec := &mockExecutor{}
	h, clock := newReplayHook(t, exec)
	body := bodyAt(clock.Now().Unix())
	sig := signBody(body, testSecret)

	const n = 50
	codes := make([]int, n)
	var start, wg sync.WaitGroup
	start.Add(1)
	for i := range n {
		wg.Go(func() {
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Hub-Signature-256", sig)
			w := httptest.NewRecorder()
			start.Wait()
			h.HandleRequest(w, req)
			codes[i] = w.Code
		})
	}
	start.Done()
	wg.Wait()

	var ok, replay int
	for _, c := range codes {
		switch c {
		case http.StatusOK:
			ok++
		case http.StatusForbidden:
			replay++
		}
	}
	if ok != 1 || replay != n-1 {
		t.Errorf("got %d OK and %d 403, want 1 and %d", ok, replay, n-1)
	}
	if c := exec.calls.Load(); c != 1 {
		t.Errorf("executor calls: got %d, want 1", c)
	}
}

// FuzzHandleRequest feeds arbitrary bodies and signature headers to the handler.
// It must never panic, and a request whose signature does not verify must never
// reach the executor.
func FuzzHandleRequest(f *testing.F) {
	f.Add(bodyAt(1_800_000_000), "")
	f.Add(bodyAt(1_800_000_000), "sha256=00")
	f.Add(`{"id":"1","param":"x","unix_seconds":-9223372036854775808}`, "")
	f.Add(`not json`, "")
	f.Add(``, "garbage")

	f.Fuzz(func(t *testing.T, body, sig string) {
		exec := &mockExecutor{}
		h, _ := newReplayHook(t, exec)
		// an empty fuzzed signature means "sign correctly" so the post-auth paths get exercised too
		header := sig
		if header == "" {
			header = signBody(body, testSecret)
		}
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hub-Signature-256", header)
		h.HandleRequest(httptest.NewRecorder(), req)

		if exec.calls.Load() > 0 && !hook.VerifySignature([]byte(body), header, testSecret) {
			t.Fatalf("unauthenticated request reached the executor")
		}
	})
}
