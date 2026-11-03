package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wussh/kubecompact/internal/model"
)

func cliRun(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func writeTestJSON(t *testing.T, path string, value any) string {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func cliTimes() (time.Time, time.Time) {
	now := time.Now().UTC()
	return now.Add(-time.Hour), now.Add(time.Hour)
}

func TestPlanRejectsUnsafeOptionsBeforeInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(input, []byte(`{"schemaVersion":"kubecompat.io/plan-input/v1alpha1"}`), 0600); err != nil {
		t.Fatal(err)
	}
	base := []string{"plan", "--from-k8s", "1.33", "--target-k8s", "1.35", "--distribution", "k3s", "--plan-input", input}
	for _, extra := range [][]string{
		{"--online"},
		{"--manifests", "input.yaml"},
		{"--git-url", "https://example.invalid/repo"},
		{"--inventory", "inventory.json"},
		{"--input", "assessment.json"},
		{"--helm-storage"},
		{"--credentials-file", "credentials.json"},
		{"--ca-file", "ca.pem"},
		{"--allow-hosts", "example.invalid"},
		{"--schemas", "schemas"},
		{"--deprecated-usage", "usage.json"},
	} {
		code, _, _ := cliRun(t, append(append([]string{}, base...), extra...)...)
		if code != 1 {
			t.Fatalf("%v accepted for plan: exit %d", extra, code)
		}
	}
	for _, args := range [][]string{
		{"plan"},
		{"plan", "--from-k8s", "1.33"},
		{"plan", "--target-k8s", "1.35", "--plan-input", input},
		{"plan", "--from-k8s", "1.33", "--target-k8s", "1.35"},
		{"plan", "--from-k8s", "1.33", "--target-k8s", "1.35", "--plan-input", input, "--distribution", "rke2"},
		{"plan", "--from-k8s", "1.33", "--target-k8s", "1.35", "--plan-input", input, "extra"},
		{"plan", "--from-k8s", "1.33", "--target-k8s", "1.35", "--plan-input", input, "--format", "pdf"},
		{"plan", "--from-k8s", "1.33", "--target-k8s", "1.33", "--plan-input", input},
		{"plan", "--from-k8s", "1.34", "--target-k8s", "1.33", "--plan-input", input},
		{"plan", "--from-k8s", "1.36", "--target-k8s", "1.38", "--plan-input", input},
		{"plan", "--from-k8s", "1.31", "--target-k8s", "1.35", "--plan-input", input},
		{"plan", "--from-k8s", "1.33", "--target-k8s", "1.35.1", "--plan-input", input},
	} {
		code, _, _ := cliRun(t, args...)
		if code != 1 {
			t.Fatalf("%v exit %d, want 1", args, code)
		}
	}
}

func TestPlanHelpAndPartialExit3(t *testing.T) {
	if code, out, errs := cliRun(t, "plan", "--help"); code != 0 || !strings.Contains(out+errs, "offline") {
		t.Fatalf("help exit %d output %q/%q", code, out, errs)
	}
	dir := t.TempDir()
	observed, _ := cliTimes()
	input := model.PlanInput{
		SchemaVersion: model.PlanInputSchema,
		ClusterID:     "synthetic-lab",
		SourceRelease: "v1.33.0+k3s1",
		ObservedAt:    observed,
		Releases:      map[string]string{"1.34": "v1.34.1+k3s1", "1.35": "v1.35.0+k3s1"},
	}
	inputPath := filepath.Join(dir, "plan.json")
	digest := writeTestJSON(t, inputPath, input)
	outPath := filepath.Join(dir, "plan-output.json")
	code, stdout, stderr := cliRun(t,
		"plan", "--from-k8s", "1.33", "--target-k8s", "1.35", "--distribution", "k3s",
		"--plan-input", inputPath, "--offline", "--cache", filepath.Join(dir, "missing-cache"),
		"--format", "json", "--output", outPath)
	if code != 3 {
		t.Fatalf("partial plan exit %d stderr %q", code, stderr)
	}
	if stdout != "" {
		t.Fatalf("output file expected, stdout=%q", stdout)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	var plan model.UpgradePlan
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Decision != "INCOMPLETE" || plan.ExitCode != 3 || len(plan.Hops) != 2 {
		t.Fatalf("plan=%s/%d hops=%d", plan.Decision, plan.ExitCode, len(plan.Hops))
	}
	if plan.InputSHA256 != digest || len(plan.InputSHA256) != 64 {
		t.Fatalf("input digest=%q want=%q", plan.InputSHA256, digest)
	}
	for _, hop := range plan.Hops {
		if hop.Assessment.ExitCode != 3 {
			t.Fatalf("hop %s exit=%d", hop.ID, hop.Assessment.ExitCode)
		}
		if len(hop.StopConditions) == 0 || len(hop.Steps) == 0 {
			t.Fatalf("hop %s lacks conditions/steps", hop.ID)
		}
		found := false
		for _, check := range hop.Assessment.Checks {
			if check.Reason == "HOP_OBSERVATION_MISSING" {
				found = true
			}
		}
		if !found {
			t.Fatalf("hop %s missing required UNKNOWN", hop.ID)
		}
	}
	if len(plan.Omissions) == 0 {
		t.Fatal("plan omits explicit limitations")
	}
	if !strings.Contains(dataString(plan), "1.34.1+k3s1") || strings.Contains(dataString(plan), "v1.34.0+k3s1") {
		t.Fatal("release identity was invented or omitted")
	}
}

func dataString(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func TestPlanKnownBlockerExit2(t *testing.T) {
	dir := t.TempDir()
	observed, valid := cliTimes()
	inventory := model.Inventory{
		SchemaVersion: model.SchemaVersion,
		Clusters: []model.ClusterObservation{{
			ID:                "synthetic-lab",
			KubernetesVersion: "v1.33.0+k3s1",
			Distribution:      "k3s",
			ObservedAt:        observed,
			Nodes: []model.NodeObservation{
				{Name: "s1", KubeletVersion: "v1.33.0+k3s1", Architecture: "amd64", Ready: true, ControlPlane: true},
			},
		}},
		Resources: []model.Resource{{
			Ref: model.ResourceRef{ClusterID: "synthetic-lab", APIVersion: "policy/v1beta1", Group: "policy", Kind: "PodDisruptionBudget", Namespace: "prod", Name: "pdb"},
		}},
	}
	inventoryPath := filepath.Join(dir, "inventory.json")
	inventoryDigest := writeTestJSON(t, inventoryPath, inventory)
	input := model.PlanInput{
		SchemaVersion: model.PlanInputSchema,
		ClusterID:     "synthetic-lab",
		SourceRelease: "v1.33.0+k3s1",
		ObservedAt:    observed,
		Releases:      map[string]string{"1.34": "v1.34.1+k3s1"},
		Hops: []model.HopInput{{
			FromRelease:     "v1.33.0+k3s1",
			ToRelease:       "v1.34.1+k3s1",
			ClusterID:       "synthetic-lab",
			ObservedAt:      observed,
			ValidUntil:      valid,
			EvidenceOwner:   "synthetic-operator",
			Datastore:       "sqlite",
			Servers:         []string{"s1"},
			Order:           []string{"s1"},
			InventoryFile:   "inventory.json",
			InventorySHA256: inventoryDigest,
		}},
	}
	inputPath := filepath.Join(dir, "plan.json")
	writeTestJSON(t, inputPath, input)
	outPath := filepath.Join(dir, "plan-blocked.json")
	code, _, stderr := cliRun(t,
		"plan", "--from-k8s", "1.33", "--target-k8s", "1.34", "--distribution", "k3s",
		"--plan-input", inputPath, "--offline", "--cache", filepath.Join(dir, "missing-cache"),
		"--format", "json", "--output", outPath)
	if code != 2 {
		t.Fatalf("known blocker exit=%d stderr=%q", code, stderr)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "API_REMOVED") || !strings.Contains(string(data), "BLOCKED") {
		t.Fatalf("known blocker missing: %s", data)
	}
}

func TestPlanReferencedInventoryErrors(t *testing.T) {
	dir := t.TempDir()
	observed, valid := cliTimes()
	plan := func(h model.HopInput) string {
		input := model.PlanInput{
			SchemaVersion: model.PlanInputSchema,
			ClusterID:     "synthetic-lab",
			SourceRelease: "v1.33.0+k3s1",
			ObservedAt:    observed,
			Releases:      map[string]string{"1.34": "v1.34.1+k3s1"},
			Hops:          []model.HopInput{h},
		}
		path := filepath.Join(dir, "plan-"+model.ID(h.InventoryFile, h.InventorySHA256, h.ClusterID)+".json")
		writeTestJSON(t, path, input)
		return path
	}
	baseHop := model.HopInput{
		FromRelease: "v1.33.0+k3s1", ToRelease: "v1.34.1+k3s1", ClusterID: "synthetic-lab",
		ObservedAt: observed, ValidUntil: valid, EvidenceOwner: "operator",
		InventoryFile: "missing.json", InventorySHA256: strings.Repeat("0", 64),
	}
	validInventory := model.Inventory{SchemaVersion: model.SchemaVersion, Clusters: []model.ClusterObservation{{
		ID: "synthetic-lab", KubernetesVersion: "v1.33.0+k3s1", Distribution: "k3s", ObservedAt: observed,
	}}}
	validPath := filepath.Join(dir, "inventory.json")
	validDigest := writeTestJSON(t, validPath, validInventory)
	cases := []struct {
		name string
		hop  model.HopInput
		exit int
	}{
		{"missing inventory", baseHop, 4},
		{"digest mismatch", func() model.HopInput {
			h := baseHop
			h.InventoryFile = "inventory.json"
			h.InventorySHA256 = strings.Repeat("f", 64)
			return h
		}(), 1},
		{"absolute path", func() model.HopInput { h := baseHop; h.InventoryFile = validPath; return h }(), 1},
		{"traversal", func() model.HopInput { h := baseHop; h.InventoryFile = "../inventory.json"; return h }(), 1},
		{"remote uri", func() model.HopInput {
			h := baseHop
			h.InventoryFile = "https://example.invalid/inventory.json"
			return h
		}(), 1},
		{"invalid digest", func() model.HopInput { h := baseHop; h.InventorySHA256 = "nothex"; return h }(), 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := plan(tc.hop)
			code, _, _ := cliRun(t,
				"plan", "--from-k8s", "1.33", "--target-k8s", "1.34", "--distribution", "k3s",
				"--plan-input", path, "--offline", "--cache", filepath.Join(dir, "missing-cache"),
				"--format", "json")
			if code != tc.exit {
				t.Fatalf("exit=%d want=%d", code, tc.exit)
			}
		})
	}

	malformed := filepath.Join(dir, "malformed.json")
	if err := os.WriteFile(malformed, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	malformedDigest := sha256.Sum256([]byte("{"))
	for name, value := range map[string]model.HopInput{
		"malformed inventory": func() model.HopInput {
			h := baseHop
			h.InventoryFile = "malformed.json"
			h.InventorySHA256 = hex.EncodeToString(malformedDigest[:])
			return h
		}(),
		"wrong inventory schema": func() model.HopInput {
			h := baseHop
			h.InventoryFile = "wrong-schema.json"
			h.InventorySHA256 = writeTestJSON(t, filepath.Join(dir, "wrong-schema.json"), map[string]any{"schemaVersion": "other"})
			return h
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			code, _, _ := cliRun(t,
				"plan", "--from-k8s", "1.33", "--target-k8s", "1.34", "--distribution", "k3s",
				"--plan-input", plan(value), "--offline", "--cache", filepath.Join(dir, "missing-cache"),
				"--format", "json")
			if code != 1 {
				t.Fatalf("exit=%d want=1", code)
			}
		})
	}

	outside := filepath.Join(t.TempDir(), "outside.json")
	writeTestJSON(t, outside, validInventory)
	linkDir := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(linkDir, "link.json")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	symlinkHop := baseHop
	symlinkHop.InventoryFile = "link.json"
	symlinkHop.InventorySHA256 = validDigest
	input := model.PlanInput{
		SchemaVersion: model.PlanInputSchema, ClusterID: "synthetic-lab", SourceRelease: "v1.33.0+k3s1",
		ObservedAt: observed, Releases: map[string]string{"1.34": "v1.34.1+k3s1"}, Hops: []model.HopInput{symlinkHop},
	}
	symlinkPlan := filepath.Join(linkDir, "plan.json")
	writeTestJSON(t, symlinkPlan, input)
	if code, _, _ := cliRun(t,
		"plan", "--from-k8s", "1.33", "--target-k8s", "1.34", "--distribution", "k3s",
		"--plan-input", symlinkPlan, "--offline", "--cache", filepath.Join(dir, "missing-cache"),
		"--format", "json"); code != 1 {
		t.Fatalf("symlink escape exit=%d want=1", code)
	}
}

func TestPlanInputShapeValidation(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"unknown field":  `{"schemaVersion":"kubecompat.io/plan-input/v1alpha1","clusterId":"x","observedAt":"2026-01-01T00:00:00Z","unknown":true}`,
		"assessment":     `{"schemaVersion":"kubecompat.io/v1alpha1","clusterId":"x"}`,
		"multiple docs":  `{"schemaVersion":"kubecompat.io/plan-input/v1alpha1"} {}`,
		"malformed":      `{`,
		"missing schema": `{"clusterId":"x"}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, strings.ReplaceAll(name, " ", "-")+".json")
			if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
				t.Fatal(err)
			}
			code, _, _ := cliRun(t,
				"plan", "--from-k8s", "1.33", "--target-k8s", "1.35", "--distribution", "k3s",
				"--plan-input", path, "--offline", "--cache", filepath.Join(dir, "missing-cache"))
			if code != 1 {
				t.Fatalf("exit=%d want=1", code)
			}
		})
	}
}

func TestPlanCanariesNeverEmitted(t *testing.T) {
	dir := t.TempDir()
	observed, valid := cliTimes()
	input := model.PlanInput{
		SchemaVersion: model.PlanInputSchema,
		ClusterID:     "synthetic-lab",
		SourceRelease: "v1.33.0+k3s1",
		ObservedAt:    observed,
		Releases:      map[string]string{"1.34": "v1.34.1+k3s1"},
		Hops: []model.HopInput{{
			FromRelease: "v1.33.0+k3s1", ToRelease: "v1.34.1+k3s1", ClusterID: "synthetic-lab",
			ObservedAt: observed, ValidUntil: valid, EvidenceOwner: "operator",
			Receipts: []model.OperatorReceipt{{
				Kind: "backup", Status: "PASS", Owner: "operator",
				ObservedAt: observed, ValidUntil: valid, EvidenceID: "EV-1",
				Notes: "TOKEN_CANARY_ANNOTATION_ENV_SECRET",
			}},
		}},
	}
	inputPath := filepath.Join(dir, "plan.json")
	writeTestJSON(t, inputPath, input)
	for _, format := range []string{"json", "table", "csv", "html"} {
		code, stdout, stderr := cliRun(t,
			"plan", "--from-k8s", "1.33", "--target-k8s", "1.34", "--distribution", "k3s",
			"--plan-input", inputPath, "--offline", "--cache", filepath.Join(dir, "missing-cache"),
			"--format", format)
		if code != 3 {
			t.Fatalf("%s exit=%d stderr=%q", format, code, stderr)
		}
		if strings.Contains(stdout, "TOKEN_CANARY") || strings.Contains(stderr, "TOKEN_CANARY") {
			t.Fatalf("%s leaked receipt notes", format)
		}
	}
}

func TestPlanOutputFailureExit4(t *testing.T) {
	dir := t.TempDir()
	observed, _ := cliTimes()
	input := model.PlanInput{
		SchemaVersion: model.PlanInputSchema, ClusterID: "synthetic-lab",
		SourceRelease: "v1.33.0+k3s1", ObservedAt: observed,
		Releases: map[string]string{"1.34": "v1.34.1+k3s1"},
	}
	inputPath := filepath.Join(dir, "plan.json")
	writeTestJSON(t, inputPath, input)
	code, _, _ := cliRun(t,
		"plan", "--from-k8s", "1.33", "--target-k8s", "1.34", "--distribution", "k3s",
		"--plan-input", inputPath, "--offline", "--cache", filepath.Join(dir, "missing-cache"),
		"--format", "json", "--output", filepath.Join(dir, "missing", "plan.json"))
	if code != 4 {
		t.Fatalf("output failure exit=%d want=4", code)
	}
}

func TestSavedPlanReportDispatch(t *testing.T) {
	dir := t.TempDir()
	observed, _ := cliTimes()
	input := model.PlanInput{
		SchemaVersion: model.PlanInputSchema, ClusterID: "synthetic-lab",
		SourceRelease: "v1.33.0+k3s1", ObservedAt: observed,
		Releases: map[string]string{"1.34": "v1.34.1+k3s1", "1.35": "v1.35.0+k3s1"},
	}
	inputPath := filepath.Join(dir, "plan.json")
	writeTestJSON(t, inputPath, input)
	planPath := filepath.Join(dir, "saved-plan.json")
	if code, _, stderr := cliRun(t,
		"plan", "--from-k8s", "1.33", "--target-k8s", "1.35", "--distribution", "k3s",
		"--plan-input", inputPath, "--offline", "--cache", filepath.Join(dir, "missing-cache"),
		"--format", "json", "--output", planPath); code != 3 {
		t.Fatalf("plan exit=%d stderr=%q", code, stderr)
	}
	for _, format := range []string{"json", "table", "csv", "html"} {
		code, stdout, stderr := cliRun(t, "report", "--input", planPath, "--format", format)
		if code != 0 {
			t.Fatalf("saved plan report %s exit=%d stderr=%q", format, code, stderr)
		}
		if !strings.Contains(stdout, "1.33-to-1.34") || !strings.Contains(stdout, "INCOMPLETE") {
			t.Fatalf("saved plan report %s omitted plan content", format)
		}
	}
	if code, _, _ := cliRun(t, "report", "--input", planPath, "--target-k8s", "1.34"); code != 1 {
		t.Fatalf("plan target mismatch exit=%d want=1", code)
	}
	crossType := map[string]string{
		"plan-schema-assessment-fields": `{"schemaVersion":"kubecompat.io/upgrade-plan/v1alpha1","components":[]}`,
		"assessment-schema-plan-fields": `{"schemaVersion":"kubecompat.io/v1alpha1","hops":[]}`,
	}
	for name, payload := range crossType {
		path := filepath.Join(dir, name+".json")
		if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
			t.Fatal(err)
		}
		if code, _, _ := cliRun(t, "report", "--input", path); code != 1 {
			t.Fatalf("cross-type %s exit=%d want=1", name, code)
		}
	}
}

func TestSavedAssessmentReportStillWorks(t *testing.T) {
	dir := t.TempDir()
	assessment := model.Assessment{
		SchemaVersion:    model.SchemaVersion,
		TargetKubernetes: "1.35",
		ResolvedTarget:   "1.35.0",
		Decision:         "NO_KNOWN_BLOCKERS",
		ExitCode:         0,
	}
	path := filepath.Join(dir, "assessment.json")
	writeTestJSON(t, path, assessment)
	code, stdout, stderr := cliRun(t, "report", "--input", path, "--format", "json")
	if code != 0 || !strings.Contains(stdout, model.SchemaVersion) || stderr != "" {
		t.Fatalf("assessment report exit=%d stderr=%q", code, stderr)
	}
}
