package main

import "net/http"

func handleOverrideDecision(response http.ResponseWriter, request *http.Request) {
	if !authorizeOperator(request) {
		http.Error(response, "forbidden", http.StatusForbidden)
		return
	}
	persistOverrideDecision()
	auditDecisionEvent()
}
