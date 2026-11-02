package report

import (
	"bytes"
	"encoding/csv"
	"github.com/wussh/kubecompact/internal/compatibility"
	"github.com/wussh/kubecompact/internal/model"
	"strings"
	"testing"
	"time"
)

func TestProducedComponentBlockerSurvivesAllReportFilters(t *testing.T) {
	now := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	c := model.Component{ID: "installation", Name: "sample", Namespace: "pilot", Project: "example", IdentityConfidence: "HIGH", Versions: []model.VersionObservation{{Role: "running", Scheme: "semver", Raw: "1.2.3", Confidence: "HIGH"}}}
	evidence := model.Evidence{ID: "knownissue-proof", Project: "example", URL: "https://official.example/issue", SourceHash: "hash", Valid: true, Priority: 0, Confidence: "HIGH", VerifiedAt: now, RetrievedAt: now, ExpiresAt: now.Add(time.Hour), Excerpt: "Authoritative issue affects target"}
	rule := model.Rule{ID: "knownissue-finding", Project: "example", ComponentRange: "1.2.x", KubernetesRange: ">=1.35 <1.36", Kind: "knownissue", Effect: "exclude", Evidence: evidence}
	a := compatibility.Evaluate(model.Inventory{Components: []model.Component{c}}, []model.Rule{rule}, model.CheckOptions{TargetKubernetes: "1.35", Now: now})
	if len(a.Components) != 1 || len(a.Components[0].Checks) != 1 || a.Decision != "BLOCKED" {
		t.Fatalf("producer fixture wrong: %+v", a)
	}
	// Optional report metadata must survive alongside the actual producer's local-only blocker.
	a.Components[0].Checks[0].Details = map[string]string{"reviewedContext": "bounded-issue-context"}
	for _, filter := range []model.Filters{{}, {Namespace: "absent"}} {
		for _, format := range []string{"json", "csv", "table", "html"} {
			var b bytes.Buffer
			if err := Write(&b, a, format, filter); err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"BLOCKED", "knownissue-finding", "knownissue", "installation", "Authoritative issue affects target", "BLOCKER", "knownissue-proof", "bounded-issue-context"} {
				if !strings.Contains(b.String(), want) {
					t.Errorf("%s filter=%+v lost %q", format, filter, want)
				}
			}
		}
	}
}

func TestCSVCheckCollectionDeduplicatesOnlyIdenticalRecords(t *testing.T) {
	c := model.CheckResult{ID: "shared", Scope: "installation", Domain: "chart", State: "FAIL", Severity: "BLOCKER", Reason: "same", Details: map[string]string{"key": "value"}}
	conflict := c
	conflict.Reason = "conflicting-reason"
	a := reportFixture()
	a.Checks = []model.CheckResult{c}
	a.Components[0].Checks = []model.CheckResult{c, conflict}
	var b bytes.Buffer
	if err := Write(&b, a, "csv", model.Filters{Namespace: "absent"}); err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(&b).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	reasons := map[string]bool{}
	for _, record := range records[1:] {
		if record[18] == "check" && record[0] == "shared" {
			n++
			reasons[record[11]] = true
		}
	}
	if n != 2 || !reasons["same"] || !reasons["conflicting-reason"] {
		t.Fatalf("dedup discarded conflicts or retained duplicate: %v", records)
	}
}
