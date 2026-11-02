package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestZeroComponentReportsKeepBlockerWithoutConfidentialPayload(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "public.yaml")
	input := `apiVersion: policy/v1beta1
kind: PodDisruptionBudget
metadata:
  name: public-budget
  namespace: example
  annotations:
    private-value: ANNOTATION_PAYLOAD_CANARY
spec:
  minAvailable: 1
  selector:
    matchLabels:
      private-label: RAW_SPEC_PAYLOAD_CANARY
`
	if err := os.WriteFile(manifest, []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBECONFIG", filepath.Join(dir, "nonexistent"))
	saved := filepath.Join(dir, "assessment.json")
	var out, errs bytes.Buffer
	code := Run(context.Background(), []string{"check", "--manifests", manifest, "--target-k8s", "1.35", "--offline", "--cache", filepath.Join(dir, "missing-cache"), "--format", "json", "--output", saved}, &out, &errs)
	if code != 2 {
		t.Fatalf("expected removed API exit2, got %d: %s", code, errs.String())
	}
	for _, format := range []string{"csv", "table", "html", "json"} {
		out.Reset()
		errs.Reset()
		code = Run(context.Background(), []string{"report", "--input", saved, "--format", format}, &out, &errs)
		if code != 0 {
			t.Fatalf("%s render must succeed independently of BLOCKED decision: %d %s", format, code, errs.String())
		}
		for _, want := range []string{"BLOCKED", "API_REMOVED", "public-budget", "example"} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("%s lost %s", format, want)
			}
		}
		for _, canary := range []string{"ANNOTATION_PAYLOAD_CANARY", "RAW_SPEC_PAYLOAD_CANARY"} {
			if strings.Contains(out.String()+errs.String(), canary) {
				t.Errorf("%s exposed %s", format, canary)
			}
		}
	}
}
