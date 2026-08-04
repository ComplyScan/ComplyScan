package main

import "net/http"

func main() {
	registerRoutes()
}

func registerRoutes() {
	http.HandleFunc("/decisions/override", handleOverrideDecision)
	http.HandleFunc("/admin/stop", handleStopModel)
}
