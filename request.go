package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

const (
	megabyte = 1024 * 1024
)

type Hook struct {
	Id    string
	Image string
	Token string
}

func handlePostRequest(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeError(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	if r.Method != http.MethodPost {
		writeError(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		msg := "Content-Type header is not application/json"
		writeError(w, msg, http.StatusUnsupportedMediaType)
		return
	}

	h, err := deserializeHook(w, r)
	if err != nil {
		return
	}

	if !deployRunning.Load() {
		deployRunning.Store(true)

		msg := "Starting Deployment!"
		writeInfo(w, msg, http.StatusOK)

		go execDeployment(h, &deployRunning)

	} else {
		msg := "Deployment already in progess!"
		writeWarning(w, msg, http.StatusTooManyRequests)
	}

	return
}

func deserializeHook(w http.ResponseWriter, r *http.Request) (h Hook, err error) {
	r.Body = http.MaxBytesReader(w, r.Body, megabyte)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	err = dec.Decode(&h)

	if err != nil {
		var syntaxError *json.SyntaxError
		var unmarshalTypeError *json.UnmarshalTypeError

		switch {
		case errors.As(err, &syntaxError):
			msg := fmt.Sprintf("Request body contains badly-formed JSON (at position %d)", syntaxError.Offset)
			writeError(w, msg, http.StatusBadRequest)

		case errors.Is(err, io.ErrUnexpectedEOF):
			msg := fmt.Sprintf("Request body contains badly-formed JSON")
			writeError(w, msg, http.StatusBadRequest)

		case errors.As(err, &unmarshalTypeError):
			msg := fmt.Sprintf("Request body contains an invalid value for the %q field (at position %d)", unmarshalTypeError.Field, unmarshalTypeError.Offset)
			writeError(w, msg, http.StatusBadRequest)

		case strings.HasPrefix(err.Error(), "json: unknown field "):
			fieldName := strings.TrimPrefix(err.Error(), "json: unknown field ")
			msg := fmt.Sprintf("Request body contains unknown field %s", fieldName)
			writeError(w, msg, http.StatusBadRequest)

		case errors.Is(err, io.EOF):
			msg := "Request body must not be empty"
			writeError(w, msg, http.StatusBadRequest)

		case err.Error() == "http: request body too large":
			msg := "Request body must not be larger than 1MB"
			writeError(w, msg, http.StatusRequestEntityTooLarge)

		default:
			log.Print(err.Error())
			writeError(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}

		return h, err
	}

	// Call decode again, using a pointer to an empty anonymous struct as
	// the destination. If the request body only contained a single JSON
	// object this will return an io.EOF error. So if we get anything else,
	// we know that there is additional data in the request body.
	err = dec.Decode(&struct{}{})
	if !errors.Is(err, io.EOF) {
		msg := "Request body must only contain a single JSON object"
		writeError(w, msg, http.StatusBadRequest)
		return h, err
	}

	if len(strings.TrimSpace(h.Id)) == 0 {
		msg := "id can not be empty"
		writeError(w, msg, http.StatusBadRequest)
		err = errors.New(msg)
		return h, err
	}
	if len(strings.TrimSpace(h.Image)) == 0 {
		msg := "ts can not be empty"
		writeError(w, msg, http.StatusBadRequest)
		err = errors.New(msg)
		return h, err
	}

	if strings.TrimSpace(h.Token) != configuration.Token {
		msg := "this does not compute"
		writeError(w, msg, http.StatusForbidden)
		err = errors.New(msg)
		return h, err
	}

	return h, nil
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
	log.Println(msg)
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(httpStatus)

	_, err := w.Write([]byte("{\"status\": \"" + status + "\", \"message\": \"" + jsonEscape(msg) + "\"}"))
	if err != nil {
		log.Println("Error writing response")
	}
}

func jsonEscape(i string) string {
	b, err := json.Marshal(i)
	if err != nil {
		panic(err)
	}
	// Trim the beginning and trailing " character
	return string(b[1 : len(b)-1])
}
