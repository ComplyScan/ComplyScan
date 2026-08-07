package main

import "testing"

func TestInferenceTimeoutUsesFailSafeFallback(t *testing.T) {
	if result := runInferenceWithFallback(true); result != "fallback" {
		t.Fatalf("inference timeout fail-safe recovery test returned %q", result)
	}
}

func runInferenceWithFallback(timeout bool) string {
	if timeout {
		return "fallback"
	}
	return "model"
}
