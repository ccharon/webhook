// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2023 Christian Charon

package util

// Package util provides shared helpers used across packages.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Unmarshal decodes a single JSON object from r into type T.
// Unknown fields are rejected. Decoder errors are translated into human-readable strings.
func Unmarshal[T any](r io.Reader) (result T, err error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()

	err = dec.Decode(&result)

	if err != nil {
		var syntaxError *json.SyntaxError
		var unmarshalTypeError *json.UnmarshalTypeError

		switch {
		case errors.As(err, &syntaxError):
			err = errors.New(fmt.Sprintf("data contains badly-formed JSON (at position %d)", syntaxError.Offset))
		case errors.Is(err, io.ErrUnexpectedEOF):
			err = errors.New(fmt.Sprintf("data contains badly-formed JSON"))
		case errors.As(err, &unmarshalTypeError):
			err = errors.New(fmt.Sprintf("data contains an invalid value for the %q field (at position %d)", unmarshalTypeError.Field, unmarshalTypeError.Offset))
		case strings.HasPrefix(err.Error(), "json: unknown field "):
			// the field name is user-controlled — never interpolate it into messages that are logged
			err = errors.New("data contains an unknown field")
		case errors.Is(err, io.EOF):
			err = errors.New("data must not be empty")
		default:
			// replace unknown decoder errors with a generic message so that
			// Go-internal details (type names, struct fields) never reach the caller
			err = errors.New("data is invalid")
		}

		return result, err
	}

	// A second Decode into an empty struct returns io.EOF if and only if the reader
	// contained exactly one JSON value. Anything else means trailing data was present.
	err = dec.Decode(&struct{}{})
	if !errors.Is(err, io.EOF) {
		return result, errors.New("data must only contain a single JSON object")
	}

	return result, nil
}
