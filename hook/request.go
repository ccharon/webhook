// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2023 Christian Charon

package hook

// Package hook validates incoming HTTP requests and dispatches them to an Executor.
// Authentication uses HMAC-SHA256 over the raw request body, the same mechanism
// as GitHub webhooks: the caller signs the body with the shared secret and sends
// the result in the X-Hub-Signature-256 header as "sha256=<hex>". The secret
// never travels over the wire, and because the body contains the timestamp every
// signature is unique — combining HMAC verification with the timestamp window
// closes both forgery and replay attack vectors.

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
	"webhook/config"
	"webhook/util"
)

const (
	megabyte = 1024 * 1024
)

var (
	// idPattern allows UUID-compatible identifiers: alphanumeric, hyphens, underscores, max 36 characters.
	idPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,36}$`)
	// paramPattern allows short identifiers such as version tags and names: alphanumeric, dots, hyphens, underscores.
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
}

// NewHook creates a Hook that validates requests against configuration and dispatches
// accepted requests to executor.
func NewHook(configuration *config.Configuration, executor Executor) *Hook {
	return &Hook{configuration: configuration, executor: executor}
}

// UnixSeconds is a Unix timestamp in seconds since 1970-01-01 00:00:00 UTC.
type UnixSeconds int64

type request struct {
	Id          string      `json:"id"`
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
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	got, err := hex.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

// HandleRequest is the single HTTP handler. It enforces method, path, content-type,
// body size, HMAC signature, field presence, and replay protection before
// handing off to the executor.
func (h *Hook) HandleRequest(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeError(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		writeError(w, "content-type header is not application/json", http.StatusUnsupportedMediaType)
		return
	}

	// read the raw body first so the HMAC can be verified before any JSON parsing
	r.Body = http.MaxBytesReader(w, r.Body, megabyte)
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
	if !VerifySignature(body, r.Header.Get("X-Hub-Signature-256"), h.configuration.Token()) {
		writeError(w, "signature verification failed", http.StatusForbidden)
		return
	}

	request, err := util.Unmarshal[request](bytes.NewReader(body))
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}

	if len(strings.TrimSpace(request.Id)) == 0 {
		writeError(w, "id must not be empty", http.StatusBadRequest)
		return
	}
	if !idPattern.MatchString(request.Id) {
		writeError(w, "id contains invalid characters or exceeds 36 characters", http.StatusBadRequest)
		return
	}
	if len(strings.TrimSpace(request.Param)) == 0 {
		writeError(w, "param must not be empty", http.StatusBadRequest)
		return
	}
	if !paramPattern.MatchString(request.Param) {
		writeError(w, "param contains invalid characters", http.StatusBadRequest)
		return
	}
	if len(request.Param) > h.configuration.ParamMaxLength() {
		writeError(w, fmt.Sprintf("param must not exceed %d characters", h.configuration.ParamMaxLength()), http.StatusBadRequest)
		return
	}

	// reject requests whose timestamp deviates more than 30 seconds from server time;
	// this window must be narrow enough to block replays yet wide enough to tolerate
	// reasonable clock skew between caller and server
	delta := time.Now().Unix() - int64(request.UnixSeconds)
	if delta > 30 || delta < -30 {
		writeError(w, "unix_seconds must be within 30 seconds of server time", http.StatusForbidden)
		return
	}

	if !h.executor.Execute(request.Id, request.Param) {
		log.Printf("deployment already in progress, request dropped id=%q param=%q", request.Id, request.Param)
		writeWarning(w, "deployment already in progress", http.StatusTooManyRequests)
		return
	}
	log.Printf("deployment triggered id=%q param=%q", request.Id, request.Param)
	writeInfo(w, "starting deployment", http.StatusOK)
	return
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
	log.Println("sending response: ", msg)

	response := response{Status: status, Message: msg}

	b, err := json.Marshal(response)
	if err != nil {
		log.Println("failed to create response: ", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// header must be set before WriteHeader; once WriteHeader is called the header is frozen
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(httpStatus)

	_, err = w.Write(b)
	if err != nil {
		// WriteHeader was already called above — calling it again would be a no-op.
		// Log the failure; the connection is likely broken at this point.
		log.Println("failed to write response: ", err)
		return
	}
}
