package hook_test

// Tests for HandleRequest use httptest to exercise the full validation pipeline
// without starting a real server or running a real deployment script.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"webhook/config"
	"webhook/hook"
)

// mockExecutor simulates an Executor whose availability is controlled by the test.
type mockExecutor struct {
	busy bool
}

func (m *mockExecutor) Execute(id, param string) bool {
	return !m.busy
}

// newTestConfig writes a minimal config file to a temp directory and loads it.
func newTestConfig(t *testing.T) *config.Configuration {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	content := `{"server":{"host":"localhost","port":6080},"token":"test-secret-for-hook-tests-32-chars!","script":"/bin/true"}`
	if err := os.WriteFile(cfgPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.NewConfiguration(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// signBody computes the X-Hub-Signature-256 header value for a given body and secret.
func signBody(body, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// validBody returns a well-formed request body with a current timestamp.
// The token field is intentionally absent — authentication is via the HMAC header.
func validBody() string {
	return fmt.Sprintf(`{"id":"1","param":"echoip","unix_seconds":%d}`, time.Now().Unix())
}

// TestHandleRequest covers the complete validation pipeline in a single table-driven
// test: method, path, content-type, body size, HMAC signature, field presence,
// timestamp, and executor state.
func TestHandleRequest(t *testing.T) {
	cfg := newTestConfig(t)

	const secret = "test-secret-for-hook-tests-32-chars!"
	// sentinel: use in signature field to send no X-Hub-Signature-256 header
	const noHeader = "NONE"

	tests := []struct {
		name           string
		method         string
		path           string
		contentType    string
		body           string
		signature      string // "" = compute correct HMAC, "NONE" = omit header, other = send as-is
		executorBusy   bool
		expectedStatus int
	}{
		{
			name:           "wrong method GET",
			method:         http.MethodGet,
			path:           "/",
			contentType:    "application/json",
			body:           validBody(),
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:           "wrong path",
			method:         http.MethodPost,
			path:           "/deploy",
			contentType:    "application/json",
			body:           validBody(),
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "wrong content-type",
			method:         http.MethodPost,
			path:           "/",
			contentType:    "text/plain",
			body:           validBody(),
			expectedStatus: http.StatusUnsupportedMediaType,
		},
		{
			name:           "empty body",
			method:         http.MethodPost,
			path:           "/",
			contentType:    "application/json",
			body:           "",
			expectedStatus: http.StatusBadRequest,
		},
		{
			// missing header → verifySignature returns false before any JSON is parsed
			name:           "missing signature header",
			method:         http.MethodPost,
			path:           "/",
			contentType:    "application/json",
			body:           validBody(),
			signature:      noHeader,
			expectedStatus: http.StatusForbidden,
		},
		{
			// wrong HMAC — body is intact but the signature doesn't match
			name:           "invalid signature",
			method:         http.MethodPost,
			path:           "/",
			contentType:    "application/json",
			body:           validBody(),
			signature:      "sha256=0000000000000000000000000000000000000000000000000000000000000000",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "missing id",
			method:         http.MethodPost,
			path:           "/",
			contentType:    "application/json",
			body:           fmt.Sprintf(`{"id":"","param":"x","unix_seconds":%d}`, time.Now().Unix()),
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "missing param",
			method:         http.MethodPost,
			path:           "/",
			contentType:    "application/json",
			body:           fmt.Sprintf(`{"id":"1","param":"","unix_seconds":%d}`, time.Now().Unix()),
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "unknown JSON field",
			method:         http.MethodPost,
			path:           "/",
			contentType:    "application/json",
			body:           fmt.Sprintf(`{"id":"1","param":"x","unix_seconds":%d,"extra":"field"}`, time.Now().Unix()),
			expectedStatus: http.StatusBadRequest,
		},
		{
			// replay protection: a request older than 30 seconds must be rejected
			name:           "timestamp too old",
			method:         http.MethodPost,
			path:           "/",
			contentType:    "application/json",
			body:           fmt.Sprintf(`{"id":"1","param":"x","unix_seconds":%d}`, time.Now().Unix()-31),
			expectedStatus: http.StatusForbidden,
		},
		{
			// replay protection: a request from the future indicates clock skew or tampering
			name:           "timestamp too far in future",
			method:         http.MethodPost,
			path:           "/",
			contentType:    "application/json",
			body:           fmt.Sprintf(`{"id":"1","param":"x","unix_seconds":%d}`, time.Now().Unix()+31),
			expectedStatus: http.StatusForbidden,
		},
		{
			// a missing timestamp field defaults to zero (Unix epoch 1970), which is always rejected
			name:           "missing timestamp",
			method:         http.MethodPost,
			path:           "/",
			contentType:    "application/json",
			body:           `{"id":"1","param":"x","unix_seconds":0}`,
			expectedStatus: http.StatusForbidden,
		},
		{
			// id may only contain alphanumeric characters, hyphens, and underscores
			name:           "id with invalid characters",
			method:         http.MethodPost,
			path:           "/",
			contentType:    "application/json",
			body:           fmt.Sprintf(`{"id":"bad$id","param":"echoip","unix_seconds":%d}`, time.Now().Unix()),
			expectedStatus: http.StatusBadRequest,
		},
		{
			// id is capped at 36 characters (UUID length)
			name:           "id too long",
			method:         http.MethodPost,
			path:           "/",
			contentType:    "application/json",
			body:           fmt.Sprintf(`{"id":"%s","param":"echoip","unix_seconds":%d}`, strings.Repeat("a", 37), time.Now().Unix()),
			expectedStatus: http.StatusBadRequest,
		},
		{
			// param may only contain alphanumeric characters, dots, hyphens, and underscores — no slashes
			name:           "param with invalid characters",
			method:         http.MethodPost,
			path:           "/",
			contentType:    "application/json",
			body:           fmt.Sprintf(`{"id":"1","param":"bad/param","unix_seconds":%d}`, time.Now().Unix()),
			expectedStatus: http.StatusBadRequest,
		},
		{
			// param is capped at 64 characters by default
			name:           "param too long",
			method:         http.MethodPost,
			path:           "/",
			contentType:    "application/json",
			body:           fmt.Sprintf(`{"id":"1","param":"%s","unix_seconds":%d}`, strings.Repeat("a", 65), time.Now().Unix()),
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "valid request executor free",
			method:         http.MethodPost,
			path:           "/",
			contentType:    "application/json",
			body:           validBody(),
			executorBusy:   false,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "valid request executor busy",
			method:         http.MethodPost,
			path:           "/",
			contentType:    "application/json",
			body:           validBody(),
			executorBusy:   true,
			expectedStatus: http.StatusTooManyRequests,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := hook.NewHook(cfg, &mockExecutor{busy: tt.executorBusy})
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", tt.contentType)

			sig := tt.signature
			if sig == "" {
				sig = signBody(tt.body, secret)
			}
			if sig != noHeader {
				req.Header.Set("X-Hub-Signature-256", sig)
			}

			w := httptest.NewRecorder()
			h.HandleRequest(w, req)
			if w.Code != tt.expectedStatus {
				t.Errorf("status: got %d, want %d (body: %s)", w.Code, tt.expectedStatus, w.Body.String())
			}
		})
	}
}

// TestHandleRequestBodyTooLarge verifies that a body exceeding 1 MB is rejected
// before the HMAC is verified or the JSON decoder tries to parse it.
func TestHandleRequestBodyTooLarge(t *testing.T) {
	cfg := newTestConfig(t)
	h := hook.NewHook(cfg, &mockExecutor{})

	longParam := strings.Repeat("a", 1024*1024)
	body := fmt.Sprintf(`{"id":"1","param":%q,"unix_seconds":%d}`, longParam, time.Now().Unix())

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// signature is irrelevant — the size limit is hit before signature verification
	req.Header.Set("X-Hub-Signature-256", signBody(body, "test-secret"))
	w := httptest.NewRecorder()
	h.HandleRequest(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusRequestEntityTooLarge)
	}
}
