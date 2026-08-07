package main

import "net/http"

const externalModelVersion = "provider-model-v3"

func handleProviderHealth(response http.ResponseWriter, _ *http.Request) {
	monitorExternalModelProvider()
	response.WriteHeader(http.StatusNoContent)
}

func monitorExternalModelProvider() {
	checkPinnedModelVersion(externalModelVersion)
}

func checkPinnedModelVersion(_ string) {}
