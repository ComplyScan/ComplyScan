package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/reviewconsent"
)

// TestMain keeps setup and scan consent records out of the developer's real
// user cache. Individual tests still receive repository-bound record names.
func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "complyscan-cli-consent-tests-")
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defaultReviewConsentStore = func() (reviewconsent.Store, error) {
		return reviewconsent.NewStore(filepath.Join(root, "review-consent")), nil
	}
	code := m.Run()
	if err := os.RemoveAll(root); err != nil && code == 0 {
		_, _ = fmt.Fprintln(os.Stderr, err)
		code = 2
	}
	os.Exit(code)
}
