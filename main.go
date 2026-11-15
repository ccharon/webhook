// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2023 Christian Charon

// Command webhook reads configuration, wires up the HTTP handler and starts the server.
// Intended to run as a systemd service, fronted by nginx which handles TLS termination.
package main

import (
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/ccharon/webhook/internal/config"
	"github.com/ccharon/webhook/internal/deploy"
	"github.com/ccharon/webhook/internal/hook"
)

func main() {
	// when running as systemd service, journald adds timestamp and service name itself
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	})))

	var configLocation string
	flag.StringVar(&configLocation, "c", "/etc/webhook/config.json", "Provide config.json location")
	flag.Parse()

	configuration, err := config.NewConfiguration(configLocation)
	if err != nil {
		slog.Error("configuration could not be read", "err", err)
		os.Exit(1)
	}
	deployment := deploy.NewDeployment(configuration)
	handleFunc := hook.NewHook(configuration, deployment).HandleRequest

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleFunc)

	addr := net.JoinHostPort(configuration.Address(), strconv.Itoa(configuration.Port()))
	slog.Info("starting server", "addr", addr)

	// explicit timeouts prevent slow clients from holding connections open indefinitely
	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
	err = server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server exited unexpected", "err", err)
		os.Exit(1)
	}
}
