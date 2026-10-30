package readiness

import (
	"context"
	"encoding/json"
	"github.com/wussh/kubecompact/internal/discovery"
	"github.com/wussh/kubecompact/internal/model"
	"testing"
)

func object(body string) map[string]any {
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		panic(err)
	}
	return m
}
func find(results []model.CheckResult, domain, state string) bool {
	for _, r := range results {
		if r.Domain == domain && r.State == state {
			return true
		}
	}
	return false
}
func TestRemovedAPIAndMissingSchema(t *testing.T) {
	inv := discovery.FromObjects([]map[string]any{object(`{"apiVersion":"policy/v1beta1","kind":"PodDisruptionBudget","metadata":{"name":"old"},"spec":{"minAvailable":1}}`)}, "manifests", "local")
	results, err := Check(context.Background(), inv, model.CheckOptions{TargetKubernetes: "1.35"})
	if err != nil {
		t.Fatal(err)
	}
	if !find(results, "api", "FAIL") {
		t.Fatal("removed API not blocked")
	}
	if !find(results, "schema", "UNKNOWN") {
		t.Fatal("missing schema must not pass")
	}
}
func TestCRDStorageAndCustomSchema(t *testing.T) {
	inv := discovery.FromObjects([]map[string]any{object(`{"apiVersion":"apiextensions.k8s.io/v1","kind":"CustomResourceDefinition","metadata":{"name":"widgets.example.org"},"spec":{"group":"example.org","names":{"kind":"Widget","plural":"widgets"},"versions":[{"name":"v1","served":true,"storage":false,"schema":{"openAPIV3Schema":{"type":"object","properties":{"apiVersion":{"type":"string"},"kind":{"type":"string"},"metadata":{"type":"object"},"spec":{"type":"object","required":["size"],"properties":{"size":{"type":"integer"}}}},"required":["spec"]}}}]}}`), object(`{"apiVersion":"example.org/v1","kind":"Widget","metadata":{"name":"broken"},"spec":{"size":"not-an-integer"}}`)}, "manifests", "local")
	results, err := Check(context.Background(), inv, model.CheckOptions{TargetKubernetes: "1.35"})
	if err != nil {
		t.Fatal(err)
	}
	if !find(results, "crd", "FAIL") || !find(results, "schema", "FAIL") {
		t.Fatalf("results=%v", results)
	}
}
func TestSavedInventorySchemaUnknown(t *testing.T) {
	inv := discovery.FromObjects([]map[string]any{object(`{"apiVersion":"v1","kind":"Service","metadata":{"name":"svc"}}`)}, "manifests", "local")
	b, _ := json.Marshal(inv)
	var saved model.Inventory
	json.Unmarshal(b, &saved)
	r, e := Check(context.Background(), saved, model.CheckOptions{TargetKubernetes: "1.35"})
	if e != nil {
		t.Fatal(e)
	}
	if !find(r, "schema", "UNKNOWN") {
		t.Fatal("saved inventory schema passed")
	}
}
func TestUnsupportedTarget(t *testing.T) {
	if _, e := Check(context.Background(), model.Inventory{}, model.CheckOptions{TargetKubernetes: "1.99"}); e == nil {
		t.Fatal("unsupported catalog target accepted")
	}
}
func TestPDBAndWorkloadState(t *testing.T) {
	inv := discovery.FromObjects([]map[string]any{object(`{"apiVersion":"policy/v1","kind":"PodDisruptionBudget","metadata":{"name":"blocked"},"status":{"disruptionsAllowed":0}}`), object(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"unready","generation":2},"spec":{"replicas":3},"status":{"observedGeneration":2,"availableReplicas":1}}`)}, "live", "local")
	r, e := Check(context.Background(), inv, model.CheckOptions{TargetKubernetes: "1.35"})
	if e != nil {
		t.Fatal(e)
	}
	if !find(r, "pdb", "FAIL") || !find(r, "workload", "FAIL") {
		t.Fatalf("results=%v", r)
	}
}
func TestRemovedAPIGatesAssessment(t *testing.T) {
	inv := discovery.FromObjects([]map[string]any{object(`{"apiVersion":"apps/v1beta1","kind":"Deployment","metadata":{"name":"old"}}`)}, "manifests", "local")
	r, e := Check(context.Background(), inv, model.CheckOptions{TargetKubernetes: "1.35"})
	if e != nil {
		t.Fatal(e)
	}
	a := model.Assessment{Checks: r}
	a.Gate()
	if a.ExitCode != 2 || a.Decision != "BLOCKED" {
		t.Fatalf("gate=%s/%d", a.Decision, a.ExitCode)
	}
}
func TestSchemaReferenceNeverLoadsNetwork(t *testing.T) {
	for _, s := range []any{map[string]any{"$ref": "https://example.org/schema.json"}, map[string]any{"properties": map[string]any{"x": map[string]any{"$ref": "file:///etc/passwd"}}}, map[string]any{"$id": "https://example.org/root", "$ref": "#/definitions/x"}} {
		if localReferences(s) {
			t.Fatal("external reference/base accepted")
		}
	}
	if !localReferences(map[string]any{"$ref": "#/definitions/x"}) {
		t.Fatal("local reference rejected")
	}
}
func TestTargetPatchIsExplicit(t *testing.T) {
	for _, target := range []string{"1.35", "1.35.0", "v1.35.0"} {
		resolved, e := ResolveTarget(target)
		if e != nil || resolved != "1.35.0" {
			t.Fatalf("resolve %q=%q,%v", target, resolved, e)
		}
	}
	for _, target := range []string{"1.35.1", "1.34", "master", "1.35.0-rc.1"} {
		if _, e := ResolveTarget(target); e == nil {
			t.Fatalf("accepted %q", target)
		}
	}
}
