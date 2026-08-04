package main

import "net/http"

func handleStopModel(response http.ResponseWriter, request *http.Request) {
	if !authorizeOperator(request) {
		http.Error(response, "forbidden", http.StatusForbidden)
		return
	}
	disableModelInference()
}

func disableModelInference() {}
