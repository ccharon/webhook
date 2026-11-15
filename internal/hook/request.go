// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2023 Christian Charon

// Package hook validates incoming HTTP requests and dispatches them to an Executor.
//
// Authentication uses HMAC-SHA256 over the raw request body, the same mechanism
// as GitHub webhooks: the caller signs the body with the shared secret and sends
// the result in the X-Hub-Signature-256 header as "sha256=<hex>". The secret
// never travels over the wire.
//
// Replay protection has two parts. The signed body carries a unix_seconds
// timestamp that must lie within ±maxClockSkew of server time, so a captured
// request becomes useless once that window has passed. Inside the window, the
// signature of every authenticated request is remembered until its timestamp
// can no longer pass the window check; a second request with the same signature
// is rejected with 403 "request was already processed". A legitimate retry
// carries a new timestamp and therefore a new signature.
package hook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ccharon/webhook/internal/config"
	"github.com/ccharon/webhook/internal/jsonx"
)

const (
	maxBodyBytes = 1024 * 1024

	// maxClockSkew is how far (in seconds) unix_seconds may deviate from server
	// time in either direction: narrow enough to keep the replay cache small,
	// wide enough to tolerate reasonable clock skew between caller and server.
	maxClockSkew = 30
)

var (
	// idPattern allows UUID-compatible identifiers: alphanumeric, hyphens, underscores, max 36 characters.
	idPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,36}$`)
	// paramPattern allows short identifiers such as version tags and names: alphanumeric, dots, hyphens, underscores.
	// Slashes are deliberately not allowed.
	paramPattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)
)

// Executor is the interface for the action triggered by a valid request.
// Execute returns true if the action was started, false if one is already running.
type Executor interface {
	Execute(id, param string) bool
}

type Hook struct {
	configuration *config.Configuration
	executor      Executor
	now           func() time.Time // injectable clock for tests

	mu sync.Mutex
	// seen maps the HMAC of every accepted request to the last unix second at
	// which its timestamp still passes the window check. Only authenticated
	// requests are inserted, so its size is bounded by the legitimate caller's
	// request rate times the window; no further eviction is needed.
	seen map[[sha256.Size]byte]int64
}

// NewHook creates a Hook that validates requests against configuration and dispatches
// accepted requests to executor.
func NewHook(configuration *config.Configuration, executor Executor) *Hook {
	return &Hook{
		configuration: configuration,
		executor:      executor,
		now:           time.Now,
		seen:          make(map[[sha256.Size]byte]int64),
	}
}

// UnixSeconds is a Unix timestamp in seconds since 1970-01-01 00:00:00 UTC.
type UnixSeconds int64

type request struct {
	ID          string      `json:"id"`
	Param       string      `json:"param"`
	UnixSeconds UnixSeconds `json:"unix_seconds"`
}

type response struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// VerifySignature checks that header equals "sha256=HMAC-SHA256(secret, body)".
// hmac.Equal provides constant-time comparison to prevent timing attacks.
func VerifySignature(body []byte, header string, secret string) bool {
	_, ok := verifySignature(body, header, secret)
	return ok
}

// verifySignature is VerifySignature that also returns the verified MAC, which
// serves as the replay-cache key. Keying on the decoded bytes rather than the
// header string means upper- and lower-case hex spellings count as the same request.
func verifySignature(body []byte, header string, secret string) (sum [sha256.Size]byte, ok bool) {
	sig, ok := strings.CutPrefix(header, "sha256=")
	if !ok {
		return sum, false
	}
	got, err := hex.DecodeString(sig)
	if err != nil {
		return sum, false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := mac.Sum(nil)
	if !hmac.Equal(got, want) {
		return sum, false
	}
	copy(sum[:], want)
	return sum, true
}

// withinWindow reports whether ts is at most maxClockSkew seconds away from now.
// Comparing against bounds derived from now (a realistic time) avoids the int64
// overflow that now-ts would hit for extreme caller-supplied values.
func withinWindow(ts, now int64) bool {
	return ts >= now-maxClockSkew && ts <= now+maxClockSkew
}

// markSeen atomically records sig and reports whether it was new. ts must already
// have passed withinWindow(ts, now). The entry is kept while now <= ts+maxClockSkew,
// exactly as long as withinWindow can still accept ts, so there is no gap in which
// a replay could pass both checks. Expired entries are pruned here, lazily.
func (h *Hook) markSeen(sig [sha256.Size]byte, ts, now int64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for k, lastValid := range h.seen {
		if now > lastValid {
			delete(h.seen, k)
		}
	}
	if _, dup := h.seen[sig]; dup {
		return false
	}
	h.seen[sig] = ts + maxClockSkew
	return true
}

// HandleRequest is the single HTTP handler. It enforces method, path, content-type,
// body size, HMAC signature, field presence, timestamp window and replay protection
// before handing off to the executor.
func (h *Hook) HandleRequest(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeError(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mediaType != "application/json" {
		writeError(w, "content-type header is not application/json", http.StatusUnsupportedMediaType)
		return
	}

	// read the raw body first so the HMAC can be verified before any JSON parsing
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(w, "data must not be larger than 1MB", http.StatusRequestEntityTooLarge)
			return
		}
		writeError(w, "could not read request body", http.StatusBadRequest)
		return
	}

	// verify HMAC before parsing JSON — unauthenticated data is never decoded
	sig, ok := verifySignature(body, r.Header.Get("X-Hub-Signature-256"), h.configuration.Token())
	if !ok {
		writeError(w, "signature verification failed", http.StatusForbidden)
		return
	}

	payload, err := jsonx.Unmarshal[request](bytes.NewReader(body))
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}

	if payload.ID == "" {
		writeError(w, "id must not be empty", http.StatusBadRequest)
		return
	}
	if !idPattern.MatchString(payload.ID) {
		writeError(w, "id contains invalid characters or exceeds 36 characters", http.StatusBadRequest)
		return
	}
	if payload.Param == "" {
		writeError(w, "param must not be empty", http.StatusBadRequest)
		return
	}
	if !paramPattern.MatchString(payload.Param) {
		writeError(w, "param contains invalid characters", http.StatusBadRequest)
		return
	}
	if len(payload.Param) > h.configuration.ParamMaxLength() {
		writeError(w, fmt.Sprintf("param must not exceed %d characters", h.configuration.ParamMaxLength()), http.StatusBadRequest)
		return
	}

	// one clock reading for both checks so the cache expiry lines up exactly with the window
	now := h.now().Unix()
	ts := int64(payload.UnixSeconds)
	if !withinWindow(ts, now) {
		writeError(w, fmt.Sprintf("unix_seconds must be within %d seconds of server time", maxClockSkew), http.StatusForbidden)
		return
	}

	// record before executing, whatever the executor answers: a request rejected
	// with 429 must not become replayable once the running deployment finishes
	if !h.markSeen(sig, ts, now) {
		slog.Warn("replayed request rejected", "id", payload.ID, "param", payload.Param)
		writeError(w, "request was already processed", http.StatusForbidden)
		return
	}

	if !h.executor.Execute(payload.ID, payload.Param) {
		slog.Warn("deployment already in progress, request dropped", "id", payload.ID, "param", payload.Param)
		writeWarning(w, "deployment already in progress", http.StatusTooManyRequests)
		return
	}
	slog.Info("deployment triggered", "id", payload.ID, "param", payload.Param)
	writeInfo(w, "starting deployment", http.StatusOK)
}

func writeError(w http.ResponseWriter, msg string, httpStatus int) {
	writeResponse(w, "ERROR", msg, httpStatus)
}

func writeWarning(w http.ResponseWriter, msg string, httpStatus int) {
	writeResponse(w, "WARN", msg, httpStatus)
}

func writeInfo(w http.ResponseWriter, msg string, httpStatus int) {
	writeResponse(w, "OK", msg, httpStatus)
}

func writeResponse(w http.ResponseWriter, status string, msg string, httpStatus int) {
	slog.Info("sending response", "status", httpStatus, "message", msg)

	b, err := json.Marshal(response{Status: status, Message: msg})
	if err != nil {
		slog.Error("failed to create response", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// header must be set before WriteHeader; once WriteHeader is called the header is frozen
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)

	if _, err := w.Write(b); err != nil {
		// WriteHeader was already called above — calling it again would be a no-op.
		// Log the failure; the connection is likely broken at this point.
		slog.Error("failed to write response", "err", err)
	}
}
