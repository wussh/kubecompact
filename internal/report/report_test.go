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
	a.Components[0].EvidenceIDs = []string{"claim-one", "claim-two"}
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
