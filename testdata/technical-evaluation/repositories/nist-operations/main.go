package main

import "net/http"

func main() {
	registerOperationalRoutes()
}

func registerOperationalRoutes() {
	http.HandleFunc("/operations/model-metrics", handleModelMetrics)
	http.HandleFunc("/operations/model-incident", handleModelIncident)
	http.HandleFunc("/operations/provider-health", handleProviderHealth)
}
