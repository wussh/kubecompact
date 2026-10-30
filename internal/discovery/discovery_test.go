package discovery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wussh/kubecompact/internal/model"
	"k8s.io/client-go/rest"
)

func object(s string) map[string]any {
	var o map[string]any
	if err := json.Unmarshal([]byte(s), &o); err != nil {
		panic(err)
	}
	return o
}
func TestProjectionSecretsAndImages(t *testing.T) {
	inv := FromObjects([]map[string]any{object(`{"apiVersion":"v1","kind":"Pod","metadata":{"name":"p","uid":"p","annotations":{"password":"CANARY"}},"spec":{"containers":[{"name":"c","image":"registry.test/a:v1","env":[{"name":"PASSWORD","value":"CANARY"}]}]},"status":{"containerStatuses":[{"name":"c","image":"registry.test/a:v1","imageID":"containerd://sha256:abc"}]}}`), object(`{"apiVersion":"v1","kind":"Secret","metadata":{"name":"s"},"data":{"key":"CANARY"}}`)}, "manifests", "test")
	b, _ := json.Marshal(inv)
	if strings.Contains(string(b), "CANARY") {
		t.Fatal("sensitive data exported")
	}
	if len(inv.Resources) != 1 || len(inv.Resources[0].Images) != 2 {
		t.Fatalf("images/resources: %+v", inv.Resources)
	}
}
func TestArgoMultiSource(t *testing.T) {
	inv := FromObjects([]map[string]any{object(`{"apiVersion":"argoproj.io/v1alpha1","kind":"Application","metadata":{"name":"a","generation":2},"spec":{"sources":[{"repoURL":"https://u:p@example.org/repo?token=secret","chart":"a","targetRevision":"1"},{"repoURL":"https://example.org/values","ref":"values","targetRevision":"main"}],"destination":{"server":"https://remote.example"}},"status":{"sync":{"status":"OutOfSync","revisions":["old","hash"]},"operationState":{"phase":"Failed"}}}`)}, "manifests", "test")
	c := inv.Components[0]
	if len(c.Sources) != 2 || !c.Sources[1].ValuesOnly || c.Sources[0].Index != 0 {
		t.Fatal(c.Sources)
	}
	for _, v := range c.Versions {
		if v.Role == "deployed" {
			t.Fatal("failed sync fabricated deployment")
		}
	}
	b, _ := json.Marshal(inv)
	if strings.Contains(string(b), "u:p") || strings.Contains(string(b), "token=") {
		t.Fatal("unsafe URL")
	}
}
func TestFluxFailedLatestPreservesHistory(t *testing.T) {
	inv := FromObjects([]map[string]any{object(`{"apiVersion":"helm.toolkit.fluxcd.io/v2","kind":"HelmRelease","metadata":{"name":"a","namespace":"apps","generation":4},"spec":{"chart":{"spec":{"chart":"a","version":"2"}}},"status":{"observedGeneration":4,"lastAttemptedRevision":"2","conditions":[{"type":"Ready","status":"False","observedGeneration":4}],"history":[{"version":2,"chartVersion":"2","status":"failed"},{"version":1,"chartVersion":"1","status":"deployed"}]}}`)}, "manifests", "test")
	var deployed string
	for _, v := range inv.Components[0].Versions {
		if v.Role == "deployed" {
			deployed = v.Raw
		}
	}
	if deployed != "1" {
		t.Fatal(inv.Components[0].Versions)
	}
}
func TestCorrelateUIDAndClusterCollisions(t *testing.T) {
	a := object(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"same","namespace":"n","uid":"a"}}`)
	b := object(`{"apiVersion":"v1","kind":"Pod","metadata":{"name":"p","namespace":"n","uid":"b","ownerReferences":[{"kind":"Deployment","name":"same","uid":"a","controller":true}]}}`)
	inv := Correlate(FromObjects([]map[string]any{b, a}, "manifests", "one"))
	if len(inv.Components) != 1 || len(inv.Components[0].Resources) != 2 {
		t.Fatal(inv.Components)
	}
	other := FromObjects([]map[string]any{a}, "manifests", "two")
	inv.Resources = append(inv.Resources, other.Resources...)
	inv.Components = append(inv.Components, other.Components...)
	inv = Correlate(inv)
	if len(inv.Components) != 2 {
		t.Fatal("cross-cluster merge", inv.Components)
	}
}
func TestReadOnlyTransport(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(200) }))
	defer server.Close()
	guard := readOnlyTransport{next: http.DefaultTransport}
	for _, method := range []string{"POST", "DELETE", "PATCH", "PUT"} {
		req, _ := http.NewRequest(method, server.URL+"/api/v1/pods", nil)
		if _, err := guard.RoundTrip(req); err == nil {
			t.Fatal("mutation permitted")
		}
	}
	req, _ := http.NewRequest("GET", server.URL+"/api/v1/namespaces/n/secrets", nil)
	if _, err := guard.RoundTrip(req); err == nil {
		t.Fatal("secret permitted")
	}
	if called {
		t.Fatal("guard dispatched denied request")
	}
}
func TestPaginationForbiddenRetainsPage(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			w.Write([]byte(`{"apiVersion":"v1","kind":"PodList","metadata":{"continue":"next"},"items":[{"apiVersion":"v1","kind":"Pod","metadata":{"name":"one"}}]}`))
			return
		}
		w.WriteHeader(403)
		w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Forbidden","code":403}`))
	}))
	defer server.Close()
	reader, err := newReader(&rest.Config{Host: server.URL}, false, "")
	if err != nil {
		t.Fatal(err)
	}
	objects, cov := reader.list(context.Background(), resourceKind{Version: "v1", Resource: "pods", Kind: "Pod", Namespaced: true}, "n")
	if len(objects) != 1 || cov.State != "PARTIAL" || cov.Code != "FORBIDDEN" {
		t.Fatalf("%d %+v", len(objects), cov)
	}
}
func TestDefaultKindsNoSecrets(t *testing.T) {
	for _, k := range kinds {
		if k.Resource == "secrets" || k.Resource == "configmaps" {
			t.Fatal(k)
		}
	}
	_ = model.SchemaVersion
}
