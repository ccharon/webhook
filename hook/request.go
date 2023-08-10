package hook

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"webhook/config"
	"webhook/deploy"
	"webhook/util"
)

const (
	megabyte = 1024 * 1024
)

type Hook struct {
	configuration *config.Configuration
	deployment    *deploy.Deployment
}

func NewHook(configuration *config.Configuration, deployment *deploy.Deployment) *Hook {
	return &Hook{configuration: configuration, deployment: deployment}
}

type request struct {
	Id    string `json:"id"`
	Image string `json:"image"`
	Token string `json:"token"`
}

type response struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

func (h *Hook) HandleRequest(w http.ResponseWriter, r *http.Request) {
	// only react on / path
	if r.URL.Path != "/" {
		writeError(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}
	// only post is supported
	if r.Method != http.MethodPost {
		writeError(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	// only accept application/json content
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		writeError(w, "Content-Type header is not application/json", http.StatusUnsupportedMediaType)
		return
	}

	// deserialize request body
	r.Body = http.MaxBytesReader(w, r.Body, megabyte)
	request, err := util.Unmarshal[request](r.Body)
	if err != nil {
		if err.Error() == "http: request body too large" {
			writeError(w, "data must not be larger than 1MB", http.StatusRequestEntityTooLarge)
		} else {
			writeError(w, err.Error(), http.StatusBadRequest)
		}
		return
	}

	// validate request
	if len(strings.TrimSpace(request.Id)) == 0 {
		writeError(w, "id must not be empty", http.StatusBadRequest)
		return
	}
	if len(strings.TrimSpace(request.Image)) == 0 {
		writeError(w, "image must not be empty", http.StatusBadRequest)
		return
	}

	// token has to match the token stored in config.yml
	if strings.TrimSpace(request.Token) != h.configuration.Token() {
		writeError(w, "token does not match", http.StatusForbidden)
		return
	}

	// execute deployment
	if h.deployment.Execute(request.Id, request.Image) {
		writeInfo(w, "Starting Deployment!", http.StatusOK)
	} else {
		writeWarning(w, "Deployment already in progress!", http.StatusTooManyRequests)
	}

	return
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

	response := response{
		Status:  status,
		Message: msg,
	}

	b, err := json.Marshal(response)
	if err != nil {
		log.Println("Error writing response")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(httpStatus)

	_, err = w.Write(b)
	if err != nil {
		log.Println("Error writing response")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}
