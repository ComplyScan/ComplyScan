package control

import "errors"

type Decision struct {
	Approved bool
	Outcome  string
}

// ApplyReviewerOverride is production-shaped fixture code: an authorised
// reviewer must approve the replacement before the consequential result moves
// forward. The fixture intentionally contains no AI risk-control test suite.
func ApplyReviewerOverride(reviewerAuthorised bool, replacement string) (Decision, error) {
	if !reviewerAuthorised {
		return Decision{}, errors.New("reviewer is not authorised")
	}
	if replacement == "" {
		return Decision{}, errors.New("replacement decision is required")
	}
	return Decision{Approved: true, Outcome: replacement}, nil
}
