package inventory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/1eonardodawinki/ComplyScan/internal/discovery"
)

type evaluationLabel struct {
	Name         string       `json:"name"`
	EvidenceType EvidenceType `json:"evidence_type"`
	Scope        Scope        `json:"scope"`
}

type evaluationCase struct {
	Path   string            `json:"path"`
	Labels []evaluationLabel `json:"labels"`
}

type evaluationMetrics struct {
	TruePositive  int
	FalsePositive int
	FalseNegative int
	TrueNegative  int
}

func TestDetectionQualityCorpus(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "evaluation")
	data, err := os.ReadFile(filepath.Join(root, "labels.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []evaluationCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}

	var metrics evaluationMetrics
	for _, testCase := range cases {
		content, err := os.ReadFile(filepath.Join(root, "files", filepath.FromSlash(testCase.Path)))
		if err != nil {
			t.Errorf("%s: %v", testCase.Path, err)
			continue
		}
		repo := discovery.Repository{Files: []discovery.File{{
			Path: testCase.Path, Kind: discovery.Classify(testCase.Path), Size: int64(len(content)), Content: content,
		}}}
		got := Analyze(repo)
		caseMetrics := compareLabels(testCase.Labels, got)
		metrics.add(caseMetrics)
		if caseMetrics.FalsePositive > 0 || caseMetrics.FalseNegative > 0 {
			t.Errorf("%s: labels=%s signals=%s", testCase.Path, formatLabels(testCase.Labels), formatSignals(got))
		}
	}

	precision := ratio(metrics.TruePositive, metrics.TruePositive+metrics.FalsePositive)
	recall := ratio(metrics.TruePositive, metrics.TruePositive+metrics.FalseNegative)
	t.Logf("AI signal corpus: precision=%.1f%% recall=%.1f%% tp=%d fp=%d fn=%d negative_cases=%d",
		precision*100, recall*100, metrics.TruePositive, metrics.FalsePositive, metrics.FalseNegative, metrics.TrueNegative)
	if precision < 0.95 {
		t.Errorf("precision %.3f is below 0.95", precision)
	}
	if recall < 0.90 {
		t.Errorf("recall %.3f is below 0.90", recall)
	}
}

func compareLabels(want []evaluationLabel, got []Signal) evaluationMetrics {
	wanted := make(map[string]struct{}, len(want))
	actual := make(map[string]struct{}, len(got))
	for _, label := range want {
		wanted[labelKey(label.Name, label.EvidenceType, label.Scope)] = struct{}{}
	}
	for _, signal := range got {
		actual[labelKey(signal.Name, signal.EvidenceType, signal.Scope)] = struct{}{}
	}
	metrics := evaluationMetrics{}
	for key := range wanted {
		if _, ok := actual[key]; ok {
			metrics.TruePositive++
		} else {
			metrics.FalseNegative++
		}
	}
	for key := range actual {
		if _, ok := wanted[key]; !ok {
			metrics.FalsePositive++
		}
	}
	if len(wanted) == 0 && len(actual) == 0 {
		metrics.TrueNegative = 1
	}
	return metrics
}

func (metrics *evaluationMetrics) add(other evaluationMetrics) {
	metrics.TruePositive += other.TruePositive
	metrics.FalsePositive += other.FalsePositive
	metrics.FalseNegative += other.FalseNegative
	metrics.TrueNegative += other.TrueNegative
}

func labelKey(name string, evidenceType EvidenceType, scope Scope) string {
	return fmt.Sprintf("%s|%s|%s", name, evidenceType, scope)
}

func ratio(numerator, denominator int) float64 {
	if denominator == 0 {
		return 1
	}
	return float64(numerator) / float64(denominator)
}

func formatLabels(labels []evaluationLabel) string {
	values := make([]string, 0, len(labels))
	for _, label := range labels {
		values = append(values, labelKey(label.Name, label.EvidenceType, label.Scope))
	}
	sort.Strings(values)
	return fmt.Sprint(values)
}

func formatSignals(signals []Signal) string {
	values := make([]string, 0, len(signals))
	for _, signal := range signals {
		values = append(values, labelKey(signal.Name, signal.EvidenceType, signal.Scope))
	}
	sort.Strings(values)
	return fmt.Sprint(values)
}
