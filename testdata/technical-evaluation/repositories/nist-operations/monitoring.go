package main

import "net/http"

func handleModelMetrics(response http.ResponseWriter, _ *http.Request) {
	recordModelInferenceTelemetry()
	response.WriteHeader(http.StatusNoContent)
}

func recordModelInferenceTelemetry() {
	emitMetric("model inference monitoring")
}

func emitMetric(_ string) {}
