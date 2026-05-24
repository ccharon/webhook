package util

import (
	"strings"
	"testing"
)

type testStruct struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

// TestUnmarshal verifies that Unmarshal returns the correct value for valid input
// and a descriptive error for every documented failure mode of the decoder.
func TestUnmarshal(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantErr     bool
		errContains string
		want        testStruct
	}{
		{
			name:  "valid JSON",
			input: `{"name":"Alice","age":30}`,
			want:  testStruct{Name: "Alice", Age: 30},
		},
		{
			name:        "syntax error",
			input:       `{"name":"Alice",}`,
			wantErr:     true,
			errContains: "badly-formed JSON",
		},
		{
			name:        "unknown field",
			input:       `{"name":"Alice","age":30,"extra":"field"}`,
			wantErr:     true,
			errContains: "unknown field",
		},
		{
			name:        "empty body",
			input:       "",
			wantErr:     true,
			errContains: "must not be empty",
		},
		{
			name:        "whitespace only",
			input:       "   ",
			wantErr:     true,
			errContains: "must not be empty",
		},
		{
			name:        "multiple JSON objects",
			input:       `{"name":"Alice","age":30}{"name":"Bob","age":25}`,
			wantErr:     true,
			errContains: "single JSON object",
		},
		{
			name:        "type mismatch",
			input:       `{"name":"Alice","age":"not-a-number"}`,
			wantErr:     true,
			errContains: "invalid value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Unmarshal[testStruct](strings.NewReader(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errContains)
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.want {
				t.Errorf("got %+v, want %+v", result, tt.want)
			}
		})
	}
}
