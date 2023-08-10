/*
Receive a post request (HookRequest), set the Values as Environment Variable and start a script.
While the script is running other requests are ignored
*/
package main

import (
	"errors"
	"flag"
	"log"
	"net/http"
	"strconv"
	"webhook/config"
	"webhook/deploy"
	"webhook/hook"
)

func main() {
	var configLocation string
	flag.StringVar(&configLocation, "c", "/etc/webhook/config.json", "Provide config.json location")
	flag.Parse()

	configuration, err := config.NewConfiguration(configLocation)
	if err != nil {
		log.Fatal(err)
	}
	deployment := deploy.NewDeployment(configuration)
	handleFunc := hook.NewHook(configuration, deployment).HandleRequest

	http.HandleFunc("/", handleFunc)
	http.NotFoundHandler()
	log.Printf("Starting server (%s:%d) \n", configuration.Address(), configuration.Port())

	err = http.ListenAndServe(configuration.Address()+":"+strconv.Itoa(configuration.Port()), nil)
	if !errors.Is(err, http.ErrServerClosed) && err != nil {
		log.Fatal(err)
	}
}
