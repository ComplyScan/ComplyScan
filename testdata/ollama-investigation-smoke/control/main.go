package control

func RunDecisionWorkflow() (Decision, error) {
	return ApplyReviewerOverride(true, "manual decision")
}
