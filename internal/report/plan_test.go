package report

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wussh/kubecompact/internal/model"
)

func planFixture() model.UpgradePlan {
	blocked := model.Assessment{
		SchemaVersion:    model.SchemaVersion,
		TargetKubernetes: "1.34",
		ResolvedTarget:   "1.34.0",
		Components: []model.ComponentAssessment{
			{
				Component: model.Component{ID: "visible", Name: "visible", Namespace: "prod", Project: "demo", ManagedBy: []string{"helm"}},
				Verdict:   model.Compatible,
				Checks: []model.CheckResult{{
					ID: "hidden-check", Domain: "schema", Scope: "visible",
					State: "FAIL", Severity: "BLOCKER", Reason: "HIDDEN_BLOCKER",
				}},
			},
			{
				Component: model.Component{ID: "other", Name: "other", Namespace: "dev", Project: "demo"},
				Verdict:   model.Unknown,
				Reason:    "SUPPORT_UNKNOWN",
			},
		},
		Coverage: []model.Coverage{{Collector: "inventory", Scope: "saved", State: "COMPLETE"}},
	}
	blocked.Checks = []model.CheckResult{{
		ID: "global-check", Domain: "plan-backup", Scope: "1.33-to-1.34",
		State: "FAIL", Severity: "BLOCKER", Reason: "OPERATOR_PREREQUISITE_FAILED",
	}}
	blocked.Gate()
	if blocked.Decision != "BLOCKED" || blocked.ExitCode != 2 {
		panic("fixture gate")
	}
	incomplete := model.Assessment{
		SchemaVersion:    model.SchemaVersion,
		TargetKubernetes: "1.35",
		ResolvedTarget:   "1.35.0",
	}
	incomplete.Checks = []model.CheckResult{{
		ID: "pending", Domain: "plan-inventory", Scope: "1.34-to-1.35",
		State: "UNKNOWN", Severity: "WARNING", Reason: "HOP_OBSERVATION_MISSING",
	}}
	incomplete.Gate()
	return model.UpgradePlan{
		SchemaVersion:    model.PlanSchema,
		AssessedAt:       blocked.AssessedAt,
		Distribution:     "k3s",
		ClusterID:        "synthetic-lab",
		FromKubernetes:   "1.33",
		TargetKubernetes: "1.35",
		SourceRelease:    "v1.33.0+k3s1",
		InputSHA256:      strings.Repeat("a", 64),
		PolicyRevision:   "911393ceaf029778b356593cc8b6fab977293e96",
		PolicySources:    map[string]string{"policy": "digest"},
		Decision:         "BLOCKED",
		ExitCode:         2,
		Omissions:        []string{"Read-only conditional plan; no execution authority."},
		Hops: []model.PlanHop{
			{
				ID: "1.33-to-1.34", FromRelease: "v1.33.0+k3s1", ToRelease: "v1.34.1+k3s1",
				CompatibilityMinor: "1.34", ResolvedTarget: "1.34.0",
				Assessment: blocked,
				Steps: []model.PlanStep{{
					ID: "step-1", Scope: "server-s1",
					Description:   "=HYPERLINK(\"https://evil.invalid\")",
					StopCondition: "Stop on failed health.",
					EvidenceIDs:   []string{"EV-1"},
				}},
				StopConditions: []string{"Stop before this hop unless every prerequisite is PASS."},
			},
			{
				ID: "1.34-to-1.35", FromRelease: "v1.34.1+k3s1", ToRelease: "",
				CompatibilityMinor: "1.35", ResolvedTarget: "1.35.0", DependsOn: "1.33-to-1.34",
				Assessment:     incomplete,
				Steps:          []model.PlanStep{{ID: "step-2", Scope: "1.34-to-1.35", Description: "Observe.", StopCondition: "Missing evidence."}},
				StopConditions: []string{"Stop before advancing without fresh observation."},
			},
		},
	}
}

func TestPlanFormatsPreserveGatesAndHiddenFindings(t *testing.T) {
	for _, format := range []string{"json", "csv", "table", "html"} {
		t.Run(format, func(t *testing.T) {
			var buffer bytes.Buffer
			if err := WritePlan(&buffer, planFixture(), format, model.Filters{Component: "missing"}); err != nil {
				t.Fatal(err)
			}
			output := buffer.String()
			for _, required := range []string{"HIDDEN_BLOCKER", "BLOCKED", "INCOMPLETE", "1.33-to-1.34", "1.34-to-1.35", "Stop on failed health."} {
				if !strings.Contains(output, required) {
					t.Fatalf("%s output omitted %q", format, required)
				}
			}
		})
	}
}

func TestPlanJSONProjection(t *testing.T) {
	plan := planFixture()
	var buffer bytes.Buffer
	if err := WritePlan(&buffer, plan, "json", model.Filters{Component: "missing"}); err != nil {
		t.Fatal(err)
	}
	var got model.UpgradePlan
	if err := json.Unmarshal(buffer.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Decision != "BLOCKED" || got.ExitCode != 2 {
		t.Fatalf("plan gate changed: %s/%d", got.Decision, got.ExitCode)
	}
	if len(got.Hops) != 2 {
		t.Fatalf("hops=%d", len(got.Hops))
	}
	if len(got.Hops[0].Assessment.Components) != 0 {
		t.Fatal("component filter did not hide rows")
	}
	found := false
	for _, check := range got.Hops[0].Assessment.Checks {
		if check.Reason == "HIDDEN_BLOCKER" {
			found = true
		}
	}
	if !found {
		t.Fatal("component-local blocker disappeared from JSON checks")
	}
	if got.Hops[0].Assessment.Decision != "BLOCKED" || got.Hops[0].Assessment.ExitCode != 2 {
		t.Fatal("hop gate changed")
	}
	if got.Hops[1].Assessment.Decision != "INCOMPLETE" || got.Hops[1].Assessment.ExitCode != 3 {
		t.Fatal("later hop gate changed")
	}
	if len(got.Hops[1].Assessment.Checks) != 1 || got.Hops[1].Assessment.Checks[0].Reason != "HOP_OBSERVATION_MISSING" {
		t.Fatal("later hop checks changed")
	}
}

func TestPlanCSVNamedColumnsAndRows(t *testing.T) {
	var buffer bytes.Buffer
	if err := WritePlan(&buffer, planFixture(), "csv", model.Filters{Component: "missing"}); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&buffer).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 5 {
		t.Fatalf("rows=%d", len(rows))
	}
	header := rows[0]
	index := headerIndexes(header)
	for _, name := range []string{
		"plan_decision", "plan_exit_code", "hop_id", "hop_decision", "hop_exit_code",
		"id", "component", "assessment", "explanation", "row_type", "details", "overall_decision", "overall_exit_code",
	} {
		if _, ok := index[name]; !ok {
			t.Fatalf("header missing %q: %v", name, header)
		}
	}
	kinds := map[string]int{}
	for _, row := range rows[1:] {
		if len(row) != len(header) {
			t.Fatalf("row width %d want %d", len(row), len(header))
		}
		kinds[row[index["row_type"]]]++
		if row[index["plan_decision"]] != "BLOCKED" || row[index["plan_exit_code"]] != "2" {
			t.Fatalf("plan gate not distinguished on %q row: %v", row[index["row_type"]], row)
		}
		switch row[index["row_type"]] {
		case "plan-summary":
			if row[index["hop_decision"]] != "" {
				t.Fatal("plan summary claims a hop gate")
			}
		case "hop", "check", "coverage", "component":
			if row[index["hop_decision"]] == "" || row[index["hop_exit_code"]] == "" {
				t.Fatalf("hop gate missing on %q row", row[index["row_type"]])
			}
		case "step":
			if row[index["hop_decision"]] == "" || row[index["hop_exit_code"]] == "" {
				t.Fatal("step row lacks hop gate")
			}
			if row[index["details"]] == "" || !strings.Contains(row[index["details"]], "stopCondition") {
				t.Fatal("step row lacks stop condition detail")
			}
			if row[index["id"]] == "step-1" && !strings.HasPrefix(row[index["explanation"]], "'=") {
				t.Fatalf("step explanation not neutralized: %q", row[index["explanation"]])
			}
		default:
			t.Fatalf("unexpected row type %q", row[index["row_type"]])
		}
	}
	for _, kind := range []string{"plan-summary", "hop", "step", "check", "coverage"} {
		if kinds[kind] == 0 {
			t.Fatalf("missing %q rows: %v", kind, kinds)
		}
	}
}

func TestPlanHTMLSelfContainedAndEscaped(t *testing.T) {
	plan := planFixture()
	plan.Hops[0].Steps[0].Description = `<script>alert("canary")</script>`
	var buffer bytes.Buffer
	if err := WritePlan(&buffer, plan, "html", model.Filters{}); err != nil {
		t.Fatal(err)
	}
	output := buffer.String()
	for _, forbidden := range []string{"<iframe", "<script src", "<link ", "<img ", "src=", "href="} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("HTML contains external/embedded resource marker %q", forbidden)
		}
	}
	if strings.Contains(output, `<script>alert("canary")</script>`) {
		t.Fatal("HTML did not escape hostile step text")
	}
	if !strings.Contains(output, "&lt;script&gt;") {
		t.Fatal("escaped canary absent")
	}
	for _, required := range []string{
		"HIDDEN_BLOCKER", "HOP_OBSERVATION_MISSING", "Stop before this hop unless every prerequisite is PASS.",
		"Plan: BLOCKED", "Hop decision: BLOCKED", "Hop decision: INCOMPLETE",
		"Filters below only hide component rows", "Reset filters",
	} {
		if !strings.Contains(output, required) {
			t.Fatalf("HTML omitted %q", required)
		}
	}
}

func TestPlanDoesNotMutateSuppliedPlan(t *testing.T) {
	plan := planFixture()
	before, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"json", "csv", "table", "html"} {
		var buffer bytes.Buffer
		if err := WritePlan(&buffer, plan, format, model.Filters{Component: "missing"}); err != nil {
			t.Fatalf("%s: %v", format, err)
		}
	}
	after, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("WritePlan mutated the supplied plan")
	}
	if len(plan.Hops[0].Assessment.Components) != 2 || plan.Hops[0].Assessment.Decision != "BLOCKED" {
		t.Fatal("supplied hop assessment was mutated")
	}
}

func TestPlanRejectsForeignSchema(t *testing.T) {
	plan := planFixture()
	plan.SchemaVersion = model.SchemaVersion
	var buffer bytes.Buffer
	if err := WritePlan(&buffer, plan, "json", model.Filters{}); err == nil {
		t.Fatal("assessment schema accepted as a plan")
	}
	if err := WritePlan(&buffer, planFixture(), "pdf", model.Filters{}); err == nil {
		t.Fatal("unsupported format accepted")
	}
}

func TestPlanEmptyHopCSVStillWritesSummary(t *testing.T) {
	plan := planFixture()
	plan.Hops = nil
	var buffer bytes.Buffer
	if err := WritePlan(&buffer, plan, "csv", model.Filters{}); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&buffer).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[1][headerIndexes(rows[0])["row_type"]] != "plan-summary" {
		t.Fatalf("unexpected CSV: %v", rows)
	}
}
