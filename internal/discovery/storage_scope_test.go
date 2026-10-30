package discovery

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wussh/kubecompact/internal/model"
)

func TestExplicitStorageNamespaceWithAllWorkloads(t *testing.T) {
	var paths []string
	release := base64.StdEncoding.EncodeToString([]byte(`{"name":"release","namespace":"workloads","version":1,"info":{"status":"deployed"},"chart":{"metadata":{"name":"chart","version":"1.0.0"}},"config":{"password":"CANARY"}}`))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/version":
			w.Write([]byte(`{"gitVersion":"v1.35.0"}`))
		case "/apis/apps/v1":
			w.Write([]byte(`{"apiVersion":"v1","kind":"APIResourceList","groupVersion":"apps/v1","resources":[{"name":"deployments","namespaced":true,"kind":"Deployment","verbs":["get","list"]}]}`))
		case "/apis/apps/v1/deployments":
			w.Write([]byte(`{"apiVersion":"apps/v1","kind":"DeploymentList","items":[{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"workload","namespace":"workloads","uid":"workload-uid","annotations":{"meta.helm.sh/release-name":"release","meta.helm.sh/release-namespace":"workloads"}}}]}`))
		case "/api/v1/namespaces/storage/secrets", "/api/v1/namespaces/storage/configmaps":
			if r.Method != "GET" || r.URL.Query().Get("labelSelector") != "owner=helm" || r.URL.Query().Get("limit") != "200" {
				t.Error("unbounded storage request")
			}
			items := []any{}
			if strings.HasSuffix(r.URL.Path, "/secrets") {
				items = append(items, map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]any{"name": "release", "namespace": "storage"}, "data": map[string]any{"release": base64.StdEncoding.EncodeToString([]byte(release))}})
			}
			json.NewEncoder(w).Encode(map[string]any{"apiVersion": "v1", "kind": "List", "items": items})
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"apiVersion":"v1","kind":"Status","reason":"NotFound","code":404}`))
		}
	}))
	defer server.Close()
	config := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(config, []byte("apiVersion: v1\nkind: Config\nclusters:\n- name: test\n  cluster:\n    server: "+server.URL+"\ncontexts:\n- name: test\n  context:\n    cluster: test\ncurrent-context: test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	inv, err := Collect(context.Background(), model.ScanOptions{Kubeconfig: config, AllNamespaces: true, HelmStorage: true, HelmStorageNamespace: "storage", Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Components) != 1 || inv.Components[0].Namespace != "workloads" || inv.Components[0].Chart.StorageNamespace != "storage" {
		t.Fatal("storage namespace replaced release destination", inv.Components)
	}
	if len(inv.Components[0].Resources) != 1 || inv.Components[0].Resources[0].UID != "workload-uid" {
		t.Fatal("all-namespace workload was not joined to scoped release", inv.Components)
	}
	seen := 0
	for _, p := range paths {
		if strings.Contains(p, "secrets") || strings.Contains(p, "configmaps") {
			seen++
			if p != "GET /api/v1/namespaces/storage/secrets" && p != "GET /api/v1/namespaces/storage/configmaps" {
				t.Fatal("storage scope escaped", p)
			}
		}
	}
	if seen != 2 {
		t.Fatal("storage query count", paths)
	}
	for _, c := range inv.Coverage {
		if c.Collector == "helm-storage" && c.Scope != "storage" {
			t.Fatal("incorrect storage coverage", c)
		}
	}
	b, _ := json.Marshal(inv)
	if strings.Contains(string(b), "CANARY") {
		t.Fatal("release values exported")
	}
	paths = nil
	if _, err := Collect(context.Background(), model.ScanOptions{Kubeconfig: config, AllNamespaces: true, Timeout: 10 * time.Second}); err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		if strings.Contains(p, "secrets") || strings.Contains(p, "configmaps") {
			t.Fatal("default collection read storage", p)
		}
	}
}

func TestStorageScopeValidationBeforeConfiguration(t *testing.T) {
	cases := []model.ScanOptions{
		{HelmStorage: true},
		{HelmStorage: true, AllNamespaces: true},
		{HelmStorage: true, HelmStorageNamespace: "*"},
		{HelmStorage: true, HelmStorageNamespace: "one,two"},
		{HelmStorage: true, HelmStorageNamespace: "Bad_Name"},
		{HelmStorage: true, Namespace: "*"},
		{HelmStorageNamespace: "storage"},
		{HelmStorage: true, HelmStorageNamespace: "storage", Manifests: "fixture.yaml"},
		{HelmStorage: true, HelmStorageNamespace: "storage", GitURL: "https://example.invalid/repo"},
	}
	for _, opts := range cases {
		_, err := Collect(context.Background(), opts)
		if err == nil || !strings.HasPrefix(err.Error(), "HELM_STORAGE_") {
			t.Fatalf("scope was not rejected before configuration: %+v: %v", opts, err)
		}
	}
}
