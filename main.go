/*
Receive a post request (Hook), set the Values as Environment Variable and start a script.
While the script is running other requests are ignored
*/
package main

import (
	"errors"
	"flag"
	"log"
	"net/http"
	"strconv"
)

var configuration Configuration

func main() {
	var configLocation string
	flag.StringVar(&configLocation, "c", "/etc/webhook/config.json", "Provide config.json location")
	flag.Parse()

	var err error
	configuration, err = readConfiguration(configLocation)
	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/", handlePostRequest)
	log.Printf("Starting server (%s:%d) \n", configuration.Server.Host, configuration.Server.Port)

	err = http.ListenAndServe(configuration.Server.Host+":"+strconv.Itoa(configuration.Server.Port), nil)
	if !errors.Is(err, http.ErrServerClosed) && err != nil {
		log.Fatal(err)
	}
}
