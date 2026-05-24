// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2023 Christian Charon

package config

// Package config loads and exposes server configuration read from a JSON file.
// All fields are unexported; callers access values exclusively through getter methods
// so the configuration is effectively immutable after construction.

import (
	"errors"
	"log"
	"os"
	"webhook/util"
)

type Configuration struct {
	address        string
	port           int
	token          string
	script         string
	timeout        int
	paramMaxLength int
}

type configdata struct {
	Server         server `json:"server"`
	Token          string `json:"token"`
	Script         string `json:"script"`
	Timeout        int    `json:"timeout"`
	ParamMaxLength int    `json:"param_max_length"`
}

type server struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

// NewConfiguration reads and parses the JSON file at configLocation.
// Returns an error if the file cannot be opened or contains invalid/unknown fields.
func NewConfiguration(configLocation string) (configuration *Configuration, err error) {
	configFile, err := os.Open(configLocation)
	if err != nil {
		return nil, err
	}

	defer func(configFile *os.File) {
		err := configFile.Close()
		if err != nil {
			log.Println("config file could not be closed: ", err)
		}
	}(configFile)

	cfg, err := util.Unmarshal[configdata](configFile)
	if err != nil {
		return nil, err
	}

	if len(cfg.Token) < 32 {
		return nil, errors.New("token must be at least 32 characters — shorter secrets weaken HMAC-SHA256 authentication")
	}

	c := Configuration{
		address:        cfg.Server.Host,
		port:           cfg.Server.Port,
		token:          cfg.Token,
		script:         cfg.Script,
		timeout:        cfg.Timeout,
		paramMaxLength: cfg.ParamMaxLength,
	}

	return &c, nil
}

// Address returns the host/IP the server should bind to.
func (c *Configuration) Address() (address string) {
	return c.address
}

// Port returns the TCP port the server should listen on.
func (c *Configuration) Port() (port int) {
	return c.port
}

// Token returns the shared secret used to authenticate incoming requests.
func (c *Configuration) Token() (token string) {
	return c.token
}

// Script returns the absolute path of the shell script to execute on a valid request.
func (c *Configuration) Script() (script string) {
	return c.script
}

// Timeout returns the maximum number of seconds the script is allowed to run.
// Returns 300 if not configured. Clamped to [1, 86400].
func (c *Configuration) Timeout() int {
	if c.timeout < 1 {
		return 300
	}
	if c.timeout > 86400 {
		return 86400
	}
	return c.timeout
}

// ParamMaxLength returns the maximum allowed byte length of the param field.
// Returns 64 if not configured. Clamped to [1, 65536].
func (c *Configuration) ParamMaxLength() int {
	if c.paramMaxLength < 1 {
		return 64
	}
	if c.paramMaxLength > 65536 {
		return 65536
	}
	return c.paramMaxLength
}
