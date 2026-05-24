package hook_test

// Direct unit tests for VerifySignature — the core security primitive.
// These tests are separate from hook_test.go so the function can be validated
// independently of the HTTP handler that calls it.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"webhook/hook"
)

func sign(body, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifySignature(t *testing.T) {
	const secret = "supersecret"
	const body = `{"id":"1","param":"test","unix_seconds":1716547200}`

	tests := []struct {
		name   string
		body   string
		header string
		secret string
		want   bool
	}{
		{
			name:   "correct signature",
			body:   body,
			header: sign(body, secret),
			secret: secret,
			want:   true,
		},
		{
			name:   "wrong secret",
			body:   body,
			header: sign(body, "wrongsecret"),
			secret: secret,
			want:   false,
		},
		{
			name:   "body tampered after signing",
			body:   `{"id":"1","param":"TAMPERED","unix_seconds":1716547200}`,
			header: sign(body, secret),
			secret: secret,
			want:   false,
		},
		{
			name:   "missing prefix",
			body:   body,
			header: hex.EncodeToString([]byte("noprefixhere")),
			secret: secret,
			want:   false,
		},
		{
			name:   "empty header",
			body:   body,
			header: "",
			secret: secret,
			want:   false,
		},
		{
			name:   "prefix only no hex",
			body:   body,
			header: "sha256=",
			secret: secret,
			want:   false,
		},
		{
			name:   "invalid hex after prefix",
			body:   body,
			header: "sha256=xyz!notvalidhex",
			secret: secret,
			want:   false,
		},
		{
			name:   "all-zero signature wrong length",
			body:   body,
			header: "sha256=0000",
			secret: secret,
			want:   false,
		},
		{
			name:   "empty body with correct signature",
			body:   "",
			header: sign("", secret),
			secret: secret,
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hook.VerifySignature([]byte(tt.body), tt.header, tt.secret)
			if got != tt.want {
				t.Errorf("VerifySignature(%q, %q, secret) = %v, want %v",
					tt.body, tt.header, got, tt.want)
			}
		})
	}
}
