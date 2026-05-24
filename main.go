// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2023 Christian Charon

package main

// Entry point. Reads configuration, wires up the HTTP handler and starts the server.
// Intended to run as a systemd service, fronted by nginx which handles TLS termination.

import (
	"errors"
	"flag"
	"log"
	"net/http"
	"strconv"
	"time"
	"webhook/config"
	"webhook/deploy"
	"webhook/hook"
)

func init() {
	// when running as systemd service, timestamp and service name are automatically added as prefix
	log.SetFlags(log.Lshortfile)
}

func main() {
	var configLocation string
	flag.StringVar(&configLocation, "c", "/etc/webhook/config.json", "Provide config.json location")
	flag.Parse()

	configuration, err := config.NewConfiguration(configLocation)
	if err != nil {
		log.Fatal("configuration could not be read: ", err)
	}
	deployment := deploy.NewDeployment(configuration)
	handleFunc := hook.NewHook(configuration, deployment).HandleRequest

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleFunc)
	log.Printf("starting server (%s:%d) \n", configuration.Address(), configuration.Port())

	// explicit timeouts prevent slow clients from holding connections open indefinitely
	server := &http.Server{
		Addr:              configuration.Address() + ":" + strconv.Itoa(configuration.Port()),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
	err = server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal("server exited unexpected: ", err)
	}
}
