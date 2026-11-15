// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2023 Christian Charon

// Command webhook reads configuration, wires up the HTTP handler and starts the server.
// Intended to run as a systemd service, fronted by nginx which handles TLS termination.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/ccharon/webhook/internal/config"
	"github.com/ccharon/webhook/internal/deploy"
	"github.com/ccharon/webhook/internal/hook"
)

const (
	// httpShutdownTimeout bounds waiting for in-flight requests, which are tiny.
	httpShutdownTimeout = 5 * time.Second
	// deployGrace is how long a running deployment may continue after SIGTERM before
	// its process group is terminated. Together with httpShutdownTimeout and
	// deploy.KillDelay it must stay below the systemd unit's TimeoutStopSec (90s).
	deployGrace = 60 * time.Second
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

	if err := run(); err != nil {
		slog.Error("webhook exited", "err", err)
		os.Exit(1)
	}
}

// run returns instead of exiting so deferred cleanup runs before main decides the exit code.
func run() error {
	var configLocation string
	flag.StringVar(&configLocation, "c", "/etc/webhook/config.json", "Provide config.json location")
	flag.Parse()

	configuration, err := config.NewConfiguration(configLocation)
	if err != nil {
		return fmt.Errorf("configuration could not be read: %w", err)
	}
	deployment := deploy.NewDeployment(configuration)

	mux := http.NewServeMux()
	mux.HandleFunc("/", hook.NewHook(configuration, deployment).HandleRequest)

	// explicit timeouts prevent slow clients from holding connections open indefinitely
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    8 << 10,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// listening synchronously reports e.g. "address in use" before anything runs
	addr := net.JoinHostPort(configuration.Address(), strconv.Itoa(configuration.Port()))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	slog.Info("starting server", "addr", addr)

	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()

	select {
	case err = <-serveErr:
		err = fmt.Errorf("server exited unexpected: %w", err)
	case <-ctx.Done():
		slog.Info("shutdown requested", "grace", deployGrace)
	}
	// restore default signal handling: a second SIGTERM/SIGINT ends the process at once
	stop()

	httpCtx, cancel := context.WithTimeout(context.Background(), httpShutdownTimeout)
	defer cancel()
	if shutdownErr := server.Shutdown(httpCtx); shutdownErr != nil {
		slog.Warn("http shutdown incomplete", "err", shutdownErr)
	}

	deployCtx, cancel := context.WithTimeout(context.Background(), deployGrace)
	defer cancel()
	if shutdownErr := deployment.Shutdown(deployCtx); shutdownErr != nil {
		slog.Warn("running deployment did not finish within grace period and was terminated")
	}

	if err == nil {
		slog.Info("shutdown complete")
	}
	return err
}
