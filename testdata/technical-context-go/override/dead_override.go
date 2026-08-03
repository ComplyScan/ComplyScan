package main

// deadOverrideDecision resembles the live control but has no production path
// and intentionally lacks authorization and audit relationships.
func deadOverrideDecision() {
	updateDecision()
}
