package model

import "testing"

func TestEmptyAssessmentCannotPass(t *testing.T) {
	a := Assessment{}
	a.Gate()
	if a.ExitCode != 3 || a.Decision != "INCOMPLETE" {
		t.Fatalf("empty evidence passed: %+v", a)
	}
}
func TestKnownBlockerPreservesIncompleteCoverage(t *testing.T) {
	a := Assessment{Components: []ComponentAssessment{{Verdict: Compatible}}, Coverage: []Coverage{{State: "UNREADABLE", Collector: "flux"}}, Checks: []CheckResult{{State: "FAIL", Severity: "BLOCKER", ID: "removed-api"}}}
	a.Gate()
	if a.ExitCode != 2 || a.Decision != "BLOCKED" || a.Coverage[0].State != "UNREADABLE" {
		t.Fatalf("gating lost blocker or gap: %+v", a)
	}
}
