package main

import "net/http"

func authorizeOperator(_ *http.Request) bool { return true }

func persistOverrideDecision() {}
