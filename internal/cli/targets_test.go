package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wussh/kubecompact/internal/model"
)

func TestCheckNewTargetsOffline(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "removed.yaml")
	if err := os.WriteFile(manifest, []byte("apiVersion: policy/v1beta1\nkind: PodDisruptionBudget\nmetadata:\n  name: removed\nspec:\n  minAvailable: 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"1.36", "1.37"} {
		output := filepath.Join(dir, "assessment-"+target+".json")
		code, _, stderr := cliRun(t,
			"check", "--manifests", manifest, "--target-k8s", target, "--offline",
			"--cache", filepath.Join(dir, "missing-cache"), "--format", "json", "--output", output)
		if code != 2 {
			t.Fatalf("check %s exit %d: %s", target, code, stderr)
		}
		raw, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		var assessment model.Assessment
		if err := json.Unmarshal(raw, &assessment); err != nil {
			t.Fatal(err)
		}
		if assessment.TargetKubernetes != target || assessment.ResolvedTarget != target+".0" {
			t.Fatalf("check %s target=%s resolved=%s", target, assessment.TargetKubernetes, assessment.ResolvedTarget)
		}
		report := filepath.Join(dir, "report-"+target+".json")
		if code, _, stderr := cliRun(t, "report", "--input", output, "--format", "json", "--output", report); code != 0 {
			t.Fatalf("report %s exit %d: %s", target, code, stderr)
		}
		if raw, err := os.ReadFile(report); err != nil || !strings.Contains(string(raw), `"targetKubernetes": "`+target+`"`) {
			t.Fatalf("report %s content: %v %s", target, err, raw)
		}
	}
}

func TestPlanNewTargetsOffline(t *testing.T) {
	dir := t.TempDir()
	observed := time.Now().UTC().Add(-time.Hour)
	input := model.PlanInput{
		SchemaVersion: model.PlanInputSchema,
		ClusterID:     "synthetic-lab",
		SourceRelease: "v1.35.0+k3s1",
		ObservedAt:    observed,
		Releases:      map[string]string{"1.36": "v1.36.5+k3s1", "1.37": "v1.37.1+k3s1"},
	}
	inputPath := filepath.Join(dir, "plan.json")
	writeTestJSON(t, inputPath, input)
	output := filepath.Join(dir, "plan-output.json")
	code, _, stderr := cliRun(t,
		"plan", "--from-k8s", "1.35", "--target-k8s", "1.37", "--distribution", "k3s",
		"--plan-input", inputPath, "--offline", "--cache", filepath.Join(dir, "missing-cache"),
		"--format", "json", "--output", output)
	if code != 3 {
		t.Fatalf("plan 1.35->1.37 exit %d: %s", code, stderr)
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var plan model.UpgradePlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	if plan.TargetKubernetes != "1.37" || len(plan.Hops) != 2 {
		t.Fatalf("plan target=%s hops=%d", plan.TargetKubernetes, len(plan.Hops))
	}
	if plan.Hops[0].ID != "1.35-to-1.36" || plan.Hops[1].ID != "1.36-to-1.37" {
		t.Fatalf("hops=%s,%s", plan.Hops[0].ID, plan.Hops[1].ID)
	}

	for _, args := range [][]string{
		{"plan", "--from-k8s", "1.36", "--target-k8s", "1.38", "--distribution", "k3s", "--plan-input", inputPath},
		{"plan", "--from-k8s", "1.37", "--target-k8s", "1.38", "--distribution", "k3s", "--plan-input", inputPath},
	} {
		if code, _, _ := cliRun(t, args...); code != 1 {
			t.Fatalf("%v exit %d, want 1", args, code)
		}
	}
}
func TestCheckRemovedHPABetaAPIs(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "hpa.yaml")
	body := "apiVersion: autoscaling/v2beta2\nkind: HorizontalPodAutoscaler\nmetadata:\n  name: hpa-v2beta2\n---\napiVersion: autoscaling/v2beta1\nkind: HorizontalPodAutoscaler\nmetadata:\n  name: hpa-v2beta1\n"
	if err := os.WriteFile(manifest, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"1.36", "1.37", "1.35"} {
		code, _, stderr := cliRun(t, "check", "--manifests", manifest, "--target-k8s", target, "--offline",
			"--cache", filepath.Join(dir, "missing-cache"), "--format", "json",
			"--output", filepath.Join(dir, "hpa-"+target+".json"))
		if code != 2 {
			t.Fatalf("check HPA %s exit %d: %s", target, code, stderr)
		}
		raw, err := os.ReadFile(filepath.Join(dir, "hpa-"+target+".json"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), `"reason": "API_REMOVED"`) || !strings.Contains(string(raw), `"state": "FAIL"`) {
			t.Fatalf("%s missing API_REMOVED blocker: %s", target, raw)
		}
	}
}
