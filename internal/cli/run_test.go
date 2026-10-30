package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileScanDoesNotLoadKubeconfig(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "workload.yaml")
	if err := os.WriteFile(p, []byte("apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: demo\nspec:\n  template:\n    spec:\n      containers:\n      - name: demo\n        image: example.invalid/demo:1.0.0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	code := Run(context.Background(), []string{"scan", "--manifests", p, "--kubeconfig", filepath.Join(dir, "missing"), "--format", "json"}, &out, &errs)
	if code != 0 {
		t.Fatalf("file scan loaded kubeconfig: code %d: %s", code, errs.String())
	}
	if !strings.Contains(out.String(), "demo") || !strings.Contains(out.String(), "kubecompat.io/v1alpha1") {
		t.Fatalf("wrong inventory: %s", out.String())
	}
}
func TestConflictingFlagsAndTargetValidation(t *testing.T) {
	for _, args := range [][]string{{"check", "--target-k8s", "bogus"}, {"check", "--target-k8s", "1.35", "--online", "--offline"}, {"scan", "--all-namespaces", "--namespace", "prod"}, {"report", "--format", "pdf"}} {
		var out, errs bytes.Buffer
		if code := Run(context.Background(), args, &out, &errs); code != 1 {
			t.Fatalf("%v returned %d: %s", args, code, errs.String())
		}
	}
}

func TestMalformedDocumentPreservesSafeDiagnosticsAndInvalidInputExit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "input.yaml")
	if err := os.WriteFile(path, []byte("apiVersion: v1\nkind: Service\nmetadata: {name: safe}\n---\napiVersion: [KUBECOMPAT_ERROR_PAYLOAD_CANARY\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"scan", "check"} {
		args := []string{command, "--manifests", path, "--format", "json"}
		if command == "check" {
			args = append(args, "--target-k8s", "1.35", "--cache", filepath.Join(dir, "missing-cache"))
		}
		var out, errs bytes.Buffer
		code := Run(context.Background(), args, &out, &errs)
		if code != 1 || !strings.Contains(out.String(), "INVALID_YAML") {
			t.Fatalf("%s: exit %d; diagnostics %s", command, code, out.String())
		}
		if strings.Contains(out.String()+errs.String(), "KUBECOMPAT_ERROR_PAYLOAD_CANARY") {
			t.Fatal("malformed document exposed")
		}
	}
}

func TestHelmStorageNamespaceRejectsNonLiveAndUnscopedInputs(t *testing.T) {
	for _, args := range [][]string{
		{"scan", "--helm-storage-namespace", "storage"},
		{"scan", "--helm-storage", "--all-namespaces"},
		{"scan", "--helm-storage", "--helm-storage-namespace", "*"},
		{"scan", "--helm-storage", "--helm-storage-namespace", "one,two"},
		{"scan", "--helm-storage", "--helm-storage-namespace", "Bad_Name"},
		{"scan", "--helm-storage", "--helm-storage-namespace", "storage", "--manifests", "missing.yaml"},
		{"check", "--helm-storage", "--helm-storage-namespace", "storage", "--inventory", "missing.json", "--target-k8s", "1.35"},
		{"report", "--helm-storage", "--helm-storage-namespace", "storage", "--input", "missing.json"},
		{"db", "update", "--helm-storage", "--helm-storage-namespace", "storage"},
	} {
		var out, errs bytes.Buffer
		if code := Run(context.Background(), args, &out, &errs); code != 1 || !strings.Contains(errs.String(), "Helm storage") {
			t.Fatalf("expected pre-collection storage validation: %v: %d %s", args, code, errs.String())
		}
	}
}
