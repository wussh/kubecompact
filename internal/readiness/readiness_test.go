package readiness

import (
	"context"
	"encoding/json"
	"github.com/wussh/kubecompact/internal/discovery"
	"github.com/wussh/kubecompact/internal/model"
	"strings"
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
	for _, target := range []string{"1.33", "v1.33", "1.33.0", "v1.33.0", "1.34", "v1.34", "1.34.0", "v1.34.0", "1.35", "v1.35", "1.35.0", "v1.35.0", "1.36", "v1.36", "1.36.0", "v1.36.0", "1.37", "v1.37", "1.37.0", "v1.37.0"} {
		resolved, e := ResolveTarget(target)
		minor := strings.TrimPrefix(target, "v")[:4]
		if e != nil || resolved != minor+".0" {
			t.Fatalf("resolve %q=%q,%v", target, resolved, e)
		}
	}
	for _, target := range []string{"1.35.1", "1.33.1", "1.34.1", "1.36.1", "1.37.1", "1.32", "1.38", "1.38.0", "master", "1.36.0-rc.1", "1.37.0-rc.1", "1.33.00"} {
		if _, e := ResolveTarget(target); e == nil {
			t.Fatalf("accepted %q", target)
		}
	}
}

func TestControlPlaneObservationStaysUnknown(t *testing.T) {
	cases := []struct {
		name    string
		version string
		nodes   []model.NodeObservation
		state   string
		reason  string
	}{
		{"complete-looking observation still unproven", "v1.34.1", []model.NodeObservation{
			{Name: "s1", KubeletVersion: "v1.34.1", ControlPlane: true, Ready: true},
			{Name: "a1", KubeletVersion: "v1.34.1", Ready: true},
		}, "UNKNOWN", "CONTROL_PLANE_MEMBERS_NOT_FULLY_OBSERVED"},
		{"no node observations", "v1.34.1", nil, "UNKNOWN", "CONTROL_PLANE_MEMBERS_NOT_FULLY_OBSERVED"},
		{"no server observation", "v1.34.1", []model.NodeObservation{
			{Name: "a1", KubeletVersion: "v1.34.1", Ready: true},
		}, "UNKNOWN", "CONTROL_PLANE_MEMBERS_NOT_FULLY_OBSERVED"},
		{"unparseable member version", "v1.34.1", []model.NodeObservation{
			{Name: "s1", KubeletVersion: "not-a-version", ControlPlane: true, Ready: true},
		}, "UNKNOWN", "CONTROL_PLANE_MEMBERS_NOT_FULLY_OBSERVED"},
		{"unparseable cluster version", "not-a-version", []model.NodeObservation{
			{Name: "s1", KubeletVersion: "v1.34.1", ControlPlane: true, Ready: true},
		}, "UNKNOWN", "CONTROL_PLANE_MEMBERS_NOT_FULLY_OBSERVED"},
		{"skew failure dominates", "v1.33.0", []model.NodeObservation{
			{Name: "s1", KubeletVersion: "v1.33.0", ControlPlane: true, Ready: true},
		}, "FAIL", "DIRECT_UPGRADE_SEQUENCE_UNSUPPORTED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inv := model.Inventory{SchemaVersion: model.SchemaVersion, Clusters: []model.ClusterObservation{{
				ID: "synthetic", KubernetesVersion: tc.version, Distribution: "k3s", Nodes: tc.nodes,
			}}}
			results, err := Check(context.Background(), inv, model.CheckOptions{TargetKubernetes: "1.35"})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, result := range results {
				if result.Domain == "control-plane" && result.State == tc.state && result.Reason == tc.reason {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing %s/%s control-plane observation in %+v", tc.state, tc.reason, results)
			}
		})
	}
}
func TestStrictSchemaPerNewTarget(t *testing.T) {
	valid := `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"ok"},"spec":{"selector":{"matchLabels":{"app":"x"}},"template":{"metadata":{"labels":{"app":"x"}},"spec":{"containers":[{"name":"c","image":"example.invalid/c:1"}]}}}}`
	unknownField := `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"bad"},"spec":{"selector":{"matchLabels":{"app":"x"}},"template":{"metadata":{"labels":{"app":"x"}},"spec":{"containers":[{"name":"c","image":"example.invalid/c:1"}]}}},"bogusField":true}`
	absentGVK := `{"apiVersion":"example.org/v1","kind":"Widget","metadata":{"name":"w"},"spec":{"size":1}}`
	for _, target := range []string{"1.36", "1.37"} {
		t.Run(target, func(t *testing.T) {
			check := func(body string) []model.CheckResult {
				inv := discovery.FromObjects([]map[string]any{object(body)}, "manifests", "local")
				results, err := Check(context.Background(), inv, model.CheckOptions{TargetKubernetes: target})
				if err != nil {
					t.Fatal(err)
				}
				return results
			}
			if !find(check(valid), "schema", "PASS") {
				t.Fatalf("%s valid Deployment did not pass strict schema", target)
			}
			if !find(check(unknownField), "schema", "FAIL") {
				t.Fatalf("%s unknown field did not fail strict schema", target)
			}
			if !find(check(absentGVK), "schema", "UNKNOWN") {
				t.Fatalf("%s absent GVK did not stay UNKNOWN", target)
			}
		})
	}
}

func TestLifecycleBoundaries136And137(t *testing.T) {
	api := func(body, target, state, reason string) {
		t.Helper()
		inv := discovery.FromObjects([]map[string]any{object(body)}, "manifests", "local")
		results, err := Check(context.Background(), inv, model.CheckOptions{TargetKubernetes: target})
		if err != nil {
			t.Fatal(err)
		}
		for _, result := range results {
			if result.Domain == "api" && result.State == state && result.Reason == reason {
				return
			}
		}
		t.Fatalf("%s on %s: missing api %s/%s in %+v", body, target, state, reason, results)
	}
	// MutatingAdmissionPolicy v1 is introduced in the pinned 1.36 catalog.
	policy := `{"apiVersion":"admissionregistration.k8s.io/v1","kind":"MutatingAdmissionPolicy","metadata":{"name":"p"},"spec":{}}`
	api(policy, "1.36", "PASS", "API_AVAILABLE_AT_TARGET")
	// storage v1beta1 VolumeAttributesClass is deprecated at 1.34 and removed at 1.37.
	vac := `{"apiVersion":"storage.k8s.io/v1beta1","kind":"VolumeAttributesClass","metadata":{"name":"v"},"driverName":"example.invalid"}`
	api(vac, "1.36", "FAIL", "API_DEPRECATED")
	api(vac, "1.37", "FAIL", "API_REMOVED")

	// Boundary arithmetic is exercised directly because each pinned catalog only
	// carries entries known at its own release.
	notYet := lifecycleResult(lifecycleEntry{Introduced: "1.37"}, true, "synthetic", 36, map[string]lifecycleEntry{})
	if notYet.State != "FAIL" || notYet.Reason != "API_NOT_YET_AVAILABLE" {
		t.Fatalf("introduced-later boundary=%+v", notYet)
	}
	removed := lifecycleResult(lifecycleEntry{Introduced: "1.30", Removed: "1.37"}, true, "synthetic", 37, map[string]lifecycleEntry{})
	if removed.State != "FAIL" || removed.Reason != "API_REMOVED" {
		t.Fatalf("removed boundary=%+v", removed)
	}
	deprecated := lifecycleResult(lifecycleEntry{Introduced: "1.30", Deprecated: "1.36"}, true, "synthetic", 36, map[string]lifecycleEntry{})
	if deprecated.State != "FAIL" || deprecated.Reason != "API_DEPRECATED" {
		t.Fatalf("deprecated boundary=%+v", deprecated)
	}
}
func TestRemovedAutoscalingBetaAPIsStayBlocked(t *testing.T) {
	bodies := []string{
		`{"apiVersion":"autoscaling/v2beta2","kind":"HorizontalPodAutoscaler","metadata":{"name":"hpa-v2beta2"}}`,
		`{"apiVersion":"autoscaling/v2beta1","kind":"HorizontalPodAutoscaler","metadata":{"name":"hpa-v2beta1"}}`,
	}
	for _, target := range []string{"1.36", "1.37", "1.35"} {
		t.Run(target, func(t *testing.T) {
			for _, body := range bodies {
				inv := discovery.FromObjects([]map[string]any{object(body)}, "manifests", "local")
				results, err := Check(context.Background(), inv, model.CheckOptions{TargetKubernetes: target})
				if err != nil {
					t.Fatal(err)
				}
				var api *model.CheckResult
				for i := range results {
					if results[i].Domain == "api" {
						api = &results[i]
					}
				}
				if api == nil || api.State != "FAIL" || api.Severity != "BLOCKER" || api.Reason != "API_REMOVED" {
					t.Fatalf("%s %s: api=%+v", target, body, api)
				}
				source := api.Details["source"]
				if target == "1.35" {
					if !strings.Contains(source, "github.com/kubernetes/api/blob/") {
						t.Fatalf("1.35 catalog source was replaced: %q", source)
					}
				} else if !strings.Contains(source, "deprecation-guide") {
					t.Fatalf("%s supplement source=%q", target, source)
				}
			}
		})
	}
}
