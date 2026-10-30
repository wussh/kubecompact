package manifests

import (
	"context"
	"encoding/json"
	"github.com/wussh/kubecompact/internal/model"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceOnlyRetainsInputReceipt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "service.yaml"), []byte("apiVersion: v1\nkind: Service\nmetadata: {name: example}\nspec: {ports: [{port: 80}]}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	inv, err := Collect(context.Background(), model.ScanOptions{Manifests: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Components) != 0 || len(inv.Sources) != 1 || inv.Sources[0].InputDigest == "" || inv.Sources[0].Renderer != "raw" {
		t.Fatalf("input provenance lost without workload: %+v", inv.Sources)
	}
	body, err := json.Marshal(inv)
	if err != nil || !strings.Contains(string(body), `"sources"`) {
		t.Fatal("saved inventory omits input receipt")
	}
}

func TestLocalSourceIdentityAndInputChange(t *testing.T) {
	dirs := []string{t.TempDir(), t.TempDir()}
	inventories := make([]model.Inventory, 2)
	body := []byte("apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: same}\nspec: {replicas: 1}\n---\napiVersion: v1\nkind: Secret\nmetadata: {name: hidden}\nstringData: {token: RECEIPT_SECRET_CANARY}\n")
	for i, dir := range dirs {
		if err := os.WriteFile(filepath.Join(dir, "input.yaml"), body, 0600); err != nil {
			t.Fatal(err)
		}
		inv, err := Collect(context.Background(), model.ScanOptions{Manifests: dir})
		if err != nil {
			t.Fatal(err)
		}
		inventories[i] = inv
	}
	a, b := inventories[0].Components[0], inventories[1].Components[0]
	if a.ID == b.ID || a.Sources[0].ID == b.Sources[0].ID {
		t.Fatal("same-name resources in different roots collided")
	}
	if a.Sources[0].InputDigest != b.Sources[0].InputDigest {
		t.Fatal("identical inputs depend on private root path")
	}
	repeated, err := Collect(context.Background(), model.ScanOptions{Manifests: dirs[0]})
	if err != nil {
		t.Fatal(err)
	}
	if repeated.Components[0].ID != a.ID || repeated.Components[0].Sources[0].InputDigest != a.Sources[0].InputDigest {
		t.Fatal("unchanged input identity or receipt changed")
	}
	if err := os.WriteFile(filepath.Join(dirs[0], "input.yaml"), []byte(strings.ReplaceAll(string(body), "replicas: 1", "replicas: 2")), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := Collect(context.Background(), model.ScanOptions{Manifests: dirs[0]})
	if err != nil {
		t.Fatal(err)
	}
	if changed.Components[0].ID != a.ID || changed.Components[0].Sources[0].InputDigest == a.Sources[0].InputDigest {
		t.Fatal("input change lost stable identity or unchanged digest")
	}
	encoded, err := json.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{dirs[0], "RECEIPT_SECRET_CANARY"} {
		if strings.Contains(string(encoded), private) {
			t.Fatal("private receipt input leaked")
		}
	}
	opts := model.ScanOptions{GitURL: "https://github.com/example/project.git", GitRevision: strings.Repeat("a", 40), GitPath: "charts/app"}
	if sourceIdentity(dirs[0], opts) != sourceIdentity(dirs[1], opts) {
		t.Fatal("immutable Git identity depends on temporary checkout")
	}
	first := sourceIdentity(dirs[0], opts)
	opts.GitRevision = strings.Repeat("b", 40)
	if first == sourceIdentity(dirs[0], opts) {
		t.Fatal("different Git revisions collided")
	}
}

func TestHelmReceiptTracksNamespaceAndResolvedInputs(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "templates"), 0700); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{"Chart.yaml": "apiVersion: v2\nname: demo\nversion: 1.0.0\n", "templates/deployment.yaml": "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: demo\n  namespace: {{ .Release.Namespace }}\nspec:\n  replicas: 1\n"} {
		if err := os.WriteFile(filepath.Join(dir, path), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var invs []model.Inventory
	for _, ns := range []string{"alpha", "beta"} {
		inv, err := Collect(context.Background(), model.ScanOptions{Manifests: dir, Render: "helm", TargetKubernetes: "1.35", Namespace: ns})
		if err != nil {
			t.Fatal(err)
		}
		invs = append(invs, inv)
	}
	if invs[0].Resources[0].Ref.Namespace != "alpha" || invs[1].Resources[0].Ref.Namespace != "beta" {
		t.Fatal("render namespace did not affect resource")
	}
	a, b := invs[0].Components[0].Sources[0], invs[1].Components[0].Sources[0]
	if a.InputDigest == b.InputDigest {
		t.Fatal("different namespace render has identical provenance receipt")
	}
	if a.Renderer != "helm" || a.TargetKubernetes != "1.35" || len(a.DependencyDigests) != 1 {
		t.Fatalf("missing actual renderer receipt %+v", a)
	}
}
