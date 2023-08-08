package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
)

type Configuration struct {
	Server Server `json:"server"`
	User   string `json:"user"`
	Token  string `json:"token"`
	Script string `json:"script"`
}

type Server struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

func readConfiguration(configLocation string) (configuration Configuration, err error) {
	// read configuration file from disk
	configFile, err := os.Open(configLocation)
	if err != nil {
		return Configuration{}, err
	}

	defer func(configFile *os.File) {
		err := configFile.Close()
		if err != nil {
			log.Println("Configfile could not be closed")
		}
	}(configFile)

	dec := json.NewDecoder(configFile)
	dec.DisallowUnknownFields()

	err = dec.Decode(&configuration)

	if err != nil {
		var syntaxError *json.SyntaxError
		var unmarshalTypeError *json.UnmarshalTypeError

		var msg string

		switch {
		case errors.As(err, &syntaxError):
			msg = fmt.Sprintf("configuration contains badly-formed JSON (at position %d)", syntaxError.Offset)

		case errors.Is(err, io.ErrUnexpectedEOF):
			msg = fmt.Sprintf("configuration contains badly-formed JSON")

		case errors.As(err, &unmarshalTypeError):
			msg = fmt.Sprintf("configuration contains an invalid value for the %q field (at position %d)", unmarshalTypeError.Field, unmarshalTypeError.Offset)

		case strings.HasPrefix(err.Error(), "json: unknown field "):
			fieldName := strings.TrimPrefix(err.Error(), "json: unknown field ")
			msg = fmt.Sprintf("configuration contains unknown field %s", fieldName)

		case errors.Is(err, io.EOF):
			msg = "configuration must not be empty"

		default:
			msg = err.Error()
		}

		return Configuration{}, errors.New(msg)
	}

	// Call decode again, using a pointer to an empty anonymous struct as
	// the destination. If the configuration only contained a single JSON
	// object this will return an io.EOF error. So if we get anything else,
	// we know that there is additional data in the request body.
	err = dec.Decode(&struct{}{})
	if !errors.Is(err, io.EOF) {
		return Configuration{}, errors.New("configuration must only contain a single JSON object")
	}

	return configuration, nil
}
