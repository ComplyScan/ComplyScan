package main

import "net/http"

func handleModelIncident(response http.ResponseWriter, _ *http.Request) {
	recoverModelIncident()
	response.WriteHeader(http.StatusNoContent)
}

func recoverModelIncident() {
	rollbackModelVersion()
}

func rollbackModelVersion() {}
