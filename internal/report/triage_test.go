package report

import (
	"bytes"
	"fmt"
	"github.com/wussh/kubecompact/internal/model"
	"strings"
	"testing"
)

func TestTriagePreservesInstallationIdentityAndGlobalPriority(t *testing.T) {
	a := reportFixture()
	a.Components[0].Component.Resources = []model.ResourceRef{{ClusterID: "cluster", Kind: "Deployment", UID: "workload"}, {ClusterID: "cluster", Kind: "Pod", UID: "pod-one"}, {ClusterID: "cluster", Kind: "Pod", UID: "pod-two"}}
	a.Components[1].Component.Resources = []model.ResourceRef{{ClusterID: "cluster", Kind: "Deployment", UID: "workload"}}
	for i := range a.Components {
		a.Components[i].Component.Images = []model.ImageObservation{{Role: "running", Image: "example/app:1", ImageID: "sha256:same"}, {Role: "requested", Image: "example/app:2"}}
	}
	a.Checks = []model.CheckResult{{ID: "unknown-first", Domain: "schema", State: "UNKNOWN", Severity: "WARNING", Reason: "RAW_DOCUMENT_UNAVAILABLE", Scope: "document"}, {ID: "blocker-second", Domain: "api", State: "FAIL", Severity: "BLOCKER", Reason: "API_REMOVED", Scope: "removed-object", Details: map[string]string{"replacement": "apps/v1 Deployment"}, EvidenceIDs: []string{"catalog-proof"}}}
	a.Coverage = []model.Coverage{{Collector: "complete-source", State: "COMPLETE", Scope: "global"}}
	a.Gate()
	for _, format := range []string{"table", "html"} {
		var b bytes.Buffer
		if err := Write(&b, a, format, model.Filters{Namespace: "absent"}); err != nil {
			t.Fatal(err)
		}
		s := b.String()
		for _, want := range []string{"2 installations", "1 workloads", "1 observed running images", "catalog-proof", "apps/v1 Deployment", "removed-object", "original", "BLOCKED", "complete-source", "COMPLETE"} {
			if !strings.Contains(s, want) {
				t.Errorf("%s missing %q", format, want)
			}
		}
		if strings.Index(s, "API_REMOVED") > strings.Index(s, "RAW_DOCUMENT_UNAVAILABLE") {
			t.Errorf("%s does not prioritize blocker", format)
		}
	}
}

func TestNextEvidenceOnlyForActionableReviewedReasons(t *testing.T) {
	for _, format := range []string{"table", "html"} {
		a := reportFixture()
		a.Components = a.Components[:1]
		a.Components[0].Verdict = model.Compatible
		a.Components[0].Reason = "Fresh official support explicitly covers this application and target version"
		a.Checks = []model.CheckResult{{ID: "conversion", Domain: "runtime-validation", State: "NOT_APPLICABLE", Severity: "INFO", Reason: "STATIC_CHECKS_DO_NOT_EXECUTE_ADMISSION_CEL_CONVERSION_OR_APPLICATIONS"}, {ID: "passed", Domain: "api", State: "PASS", Severity: "INFO", Reason: "VERSION_UNKNOWN"}}
		var b bytes.Buffer
		if err := Write(&b, a, format, model.Filters{}); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(b.String(), "Supply observed deployed/running version evidence") || strings.Contains(b.String(), "Supply an explicit reviewed identity") {
			t.Fatalf("%s prescribed evidence for nonactionable findings", format)
		}
		a.Components[0].Verdict = model.Unknown
		a.Components[0].Reason = "Application identity or unambiguous deployed/running semantic version is missing"
		b.Reset()
		if err := Write(&b, a, format, model.Filters{}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(b.String(), "Supply observed deployed/running version evidence") {
			t.Errorf("%s lost genuine version-gap action", format)
		}
	}
}

func TestTableGroupsBoundRawDetailVolume(t *testing.T) {
	a := reportFixture()
	for i := range 1000 {
		a.Checks = append(a.Checks, model.CheckResult{ID: fmt.Sprint("finding-", i), Domain: "api", Scope: fmt.Sprint("scope-", i), State: "UNKNOWN", Severity: "WARNING", Reason: "unmapped", Details: map[string]string{"raw": strings.Repeat("detail", 100)}})
	}
	var b bytes.Buffer
	if err := Write(&b, a, "table", model.Filters{}); err != nil {
		t.Fatal(err)
	}
	if len(b.Bytes()) > 10000 || !strings.Contains(b.String(), "1000 findings") || !strings.Contains(b.String(), "JSON/CSV") {
		t.Fatalf("table is not bounded grouped triage (%d bytes)", b.Len())
	}
}

func TestCoverageActionsRequireIncompleteStateAcrossFormats(t *testing.T) {
	for _, state := range []string{"COMPLETE", "PARTIAL", "UNREADABLE"} {
		for _, code := range []string{"", "CACHE_UNAVAILABLE"} {
			a := model.Assessment{Coverage: []model.Coverage{{Collector: "source", State: state, Code: code}}}
			for _, format := range []string{"table", "html"} {
				var b bytes.Buffer
				if err := Write(&b, a, format, model.Filters{}); err != nil {
					t.Fatal(err)
				}
				hasAction := strings.Contains(b.String(), "Prepare the evidence cache") || strings.Contains(b.String(), "No mapped next action")
				if hasAction != (state != "COMPLETE") {
					t.Errorf("%s state=%s code=%q action mismatch: %s", format, state, code, b.String())
				}
				if state == "COMPLETE" && strings.Contains(strings.ToLower(b.String()), "next evidence:") {
					t.Errorf("%s healthy coverage retained empty action label", format)
				}
			}
		}
	}
}
