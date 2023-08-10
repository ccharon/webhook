package util

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// fancy unmarshal of json data structures to have a more meaningful error if something goes wrong
// also I tried to use generics on this the first time ...

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
			fieldName := strings.TrimPrefix(err.Error(), "json: unknown field ")
			err = errors.New(fmt.Sprintf("data contains unknown field %s", fieldName))
		case errors.Is(err, io.EOF):
			err = errors.New("data must not be empty")
		default:
			// keep the error we already got
		}

		return result, err
	}

	// Call decode again, using a pointer to an empty anonymous struct as the destination. If the configuration only
	// contained a single JSON object this will return an io.EOF error. So if we get anything else, we know that there
	// is additional data in the request body.
	err = dec.Decode(&struct{}{})
	if !errors.Is(err, io.EOF) {
		return result, errors.New("data must only contain a single JSON object")
	}

	return result, nil
}
