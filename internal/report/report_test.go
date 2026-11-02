package report

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"github.com/wussh/kubecompact/internal/model"
	"strings"
	"testing"
)

func reportFixture() model.Assessment {
	a := model.Assessment{SchemaVersion: model.SchemaVersion, TargetKubernetes: "1.35", Components: []model.ComponentAssessment{
		{Component: model.Component{ID: "one", Name: "=HYPERLINK(\"bad\")", Namespace: "alpha", ManagedBy: []string{"Helm"}}, Verdict: model.Unknown, Reason: "<script>alert('CANARY')</script>"},
		{Component: model.Component{ID: "two", Name: "app", Namespace: "beta", ManagedBy: []string{"Flux"}}, Verdict: model.Incompatible, Reason: "removed"},
	}}
	a.Gate()
	return a
}

func TestReportsDistinguishEvidenceSharingURL(t *testing.T) {
	a := reportFixture()
	a.Evidence = []model.Evidence{{ID: "claim-one", URL: "https://official.example/support"}, {ID: "claim-two", URL: "https://official.example/support"}}
	a.Components[0].EvidenceIDs = []string{"claim-one", "claim-two", "missing-metadata"}
	for _, format := range []string{"csv", "html", "table", "json"} {
		var b bytes.Buffer
		if err := Write(&b, a, format, model.Filters{}); err != nil {
			t.Fatal(err)
		}
		for _, id := range a.Components[0].EvidenceIDs {
			if !strings.Contains(b.String(), id) {
				t.Fatalf("%s lost exact reference %s", format, id)
			}
		}
	}
}
func TestFilteringNeverHidesOverallBlocker(t *testing.T) {
	var b bytes.Buffer
	if err := Write(&b, reportFixture(), "json", model.Filters{Namespace: "alpha"}); err != nil {
		t.Fatal(err)
	}
	var a model.Assessment
	if err := json.Unmarshal(b.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	if a.Decision != "BLOCKED" || len(a.Components) != 1 || a.Components[0].Component.ID != "one" {
		t.Fatalf("filter lost gating state: %+v", a)
	}
}
func TestCSVFormulaAndHTMLMarkupAreEscaped(t *testing.T) {
	a := reportFixture()
	var b bytes.Buffer
	if err := Write(&b, a, "csv", model.Filters{}); err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(&b).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range records[1:] {
		for _, cell := range row {
			if strings.Contains(cell, "HYPERLINK") {
				found = true
				if !strings.HasPrefix(cell, "'") {
					t.Fatalf("formula not neutralized: %q", cell)
				}
			}
		}
	}
	if !found {
		t.Fatal("component disappeared from CSV")
	}
	b.Reset()
	if err := Write(&b, a, "html", model.Filters{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "<script>alert('CANARY')</script>") {
		t.Fatal("untrusted script rendered")
	}
}
func TestUnknownFormatAndOutputFailures(t *testing.T) {
	if Write(&bytes.Buffer{}, reportFixture(), "pdf", model.Filters{}) == nil {
		t.Fatal("unsupported format accepted")
	}
	if Write(failedWriter{}, reportFixture(), "json", model.Filters{}) == nil {
		t.Fatal("write failure discarded")
	}
}

type failedWriter struct{}

func (failedWriter) Write(p []byte) (int, error) { return 0, bytes.ErrTooLarge }

func TestRequestedAndRenderedImagesAreNotReportedAsRunning(t *testing.T) {
	a := reportFixture()
	a.Components = a.Components[:1]
	a.Components[0].Component.Images = []model.ImageObservation{
		{Role: "requested", Image: "example.invalid/app:future"},
		{Role: "rendered", Image: "example.invalid/app:chart"},
		{Role: "running", Image: "example.invalid/app:current", ImageID: "sha256:observed"},
	}
	var b bytes.Buffer
	if err := Write(&b, a, "csv", model.Filters{}); err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(&b).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if got := records[1][7]; got != "example.invalid/app:current [sha256:observed]" {
		t.Fatalf("running image misrepresented: %q", got)
	}
}

func TestCSVPreservesGlobalFindingsWithoutComponents(t *testing.T) {
	for _, blocker := range []bool{false, true} {
		a := model.Assessment{TargetKubernetes: "1.35", Checks: []model.CheckResult{
			{ID: "missing-usage", Domain: "usage", Scope: "cluster", State: "UNKNOWN", Severity: "WARNING", Reason: "USAGE_UNAVAILABLE", EvidenceIDs: []string{"usage-evidence"}},
		}, Coverage: []model.Coverage{{Collector: "cache", Scope: "global", State: "PARTIAL", Code: "CACHE_UNAVAILABLE"}}}
		if blocker {
			a.Checks = append(a.Checks, model.CheckResult{ID: "removed-api", Domain: "api", Scope: "policy/v1beta1/PodSecurityPolicy", State: "FAIL", Severity: "BLOCKER", Reason: "API_REMOVED", EvidenceIDs: []string{"catalog-evidence"}, Details: map[string]string{"removed": "1.25"}})
		}
		a.Gate()
		var b bytes.Buffer
		if err := Write(&b, a, "csv", model.Filters{Namespace: "absent"}); err != nil {
			t.Fatal(err)
		}
		records, err := csv.NewReader(strings.NewReader(b.String())).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		if len(records) < 4 {
			t.Fatalf("CSV lost summary/checks/coverage: %s", b.String())
		}
		wantDecision, wantExit := "INCOMPLETE", "3"
		if blocker {
			wantDecision, wantExit = "BLOCKED", "2"
		}
		for _, record := range records[1:] {
			if record[15] != wantDecision || record[16] != wantExit {
				t.Fatalf("wrong zero-component decision: %v", record)
			}
		}
		for _, want := range []string{a.Decision, "missing-usage", "usage-evidence", "CACHE_UNAVAILABLE", "coverage", "summary", "check"} {
			if !strings.Contains(b.String(), want) {
				t.Errorf("CSV lost %q: %s", want, b.String())
			}
		}
		if blocker {
			for _, want := range []string{"BLOCKED", "removed-api", "API_REMOVED", "BLOCKER", "catalog-evidence", "policy/v1beta1/PodSecurityPolicy"} {
				if !strings.Contains(b.String(), want) {
					t.Errorf("CSV lost blocker field %q", want)
				}
			}
		}
	}
}

func TestCSVComponentFiltersPreserveGlobalRowsAndCellSafety(t *testing.T) {
	a := reportFixture()
	a.Checks = []model.CheckResult{
		{ID: "global-blocker", Domain: "api", Scope: "=UNTRUSTED()", State: "FAIL", Severity: "BLOCKER", Reason: "API_REMOVED", EvidenceIDs: []string{"proof"}, Details: map[string]string{"replacement": "apps/v1 Deployment"}},
		{ID: "required-unknown", Domain: "usage", Scope: "global", State: "UNKNOWN", Severity: "WARNING", Reason: "USAGE_UNAVAILABLE"},
	}
	a.Coverage = []model.Coverage{{Collector: "cache", State: "PARTIAL", Code: "CACHE_UNAVAILABLE", Omissions: []string{"@UNTRUSTED()"}}}
	a.Evidence = []model.Evidence{{ID: "proof", URL: "https://official.example/support"}}
	a.Gate()
	for _, f := range []model.Filters{{Namespace: "alpha"}, {Component: "absent"}, {Status: "COMPATIBLE"}} {
		var b bytes.Buffer
		if err := Write(&b, a, "csv", f); err != nil {
			t.Fatal(err)
		}
		records, err := csv.NewReader(strings.NewReader(b.String())).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		types := map[string]int{}
		for _, record := range records[1:] {
			types[record[18]]++
			if record[15] != "BLOCKED" || record[16] != "2" {
				t.Fatalf("filtered CSV changed global decision: %v", record)
			}
			for _, cell := range record {
				if strings.HasPrefix(cell, "=") || strings.HasPrefix(cell, "@") {
					t.Fatalf("unescaped formula: %q", cell)
				}
			}
		}
		if types["summary"] != 1 || types["check"] != 2 || types["coverage"] != 1 {
			t.Fatalf("filter dropped global rows: %v", types)
		}
		for _, want := range []string{"global-blocker", "required-unknown", "proof", "https://official.example/support"} {
			if !strings.Contains(b.String(), want) {
				t.Errorf("filtered CSV lost %s", want)
			}
		}
	}
}
