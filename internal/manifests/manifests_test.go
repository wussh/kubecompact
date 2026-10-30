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

func TestMixedOwnershipThroughManifestCollection(t *testing.T) {
	body, err := os.ReadFile("../../testdata/controllers/mixed.json")
	if err != nil {
		t.Fatal(err)
	}
	var objects []json.RawMessage
	if err := json.Unmarshal(body, &objects); err != nil {
		t.Fatal(err)
	}
	documents := make([]string, len(objects))
	for i, object := range objects {
		documents[i] = string(object)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mixed.yaml"), []byte(strings.Join(documents, "\n---\n")), 0600); err != nil {
		t.Fatal(err)
	}
	inv, err := Collect(context.Background(), model.ScanOptions{Manifests: dir})
	if err != nil || len(inv.Components) != 2 {
		t.Fatalf("manifest ownership was not correlated: %d, %v", len(inv.Components), err)
	}
}

func TestRawDocuments(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		count      int
		bad        bool
	}{
		{"list", "apiVersion: v1\nkind: List\nitems:\n- apiVersion: apps/v1beta1\n  kind: Deployment\n  metadata: {name: old}\n", 1, false},
		{"duplicate", "apiVersion: v1\nkind: Pod\nmetadata: {name: secret, name: leaked}\n", 0, true},
		{"invalid", "apiVersion: [unclosed", 0, true},
		{"multi", "apiVersion: v1\nkind: Service\nmetadata: {name: a}\n---\napiVersion: v1\nkind: Pod\nmetadata: {name: b}\n", 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "x.yaml"), []byte(tc.body), 0600)
			inv, err := Collect(context.Background(), model.ScanOptions{Manifests: dir})
			if err != nil {
				t.Fatal(err)
			}
			if len(inv.Resources) != tc.count {
				t.Fatalf("resources=%d", len(inv.Resources))
			}
			if (len(inv.Diagnostics) > 0) != tc.bad {
				t.Fatalf("diagnostics=%v", inv.Diagnostics)
			}
			for _, d := range inv.Diagnostics {
				if strings.Contains(d.Message, "leaked") {
					t.Fatal("payload exposed")
				}
			}
			if tc.name == "list" && inv.Resources[0].Ref.APIVersion != "apps/v1beta1" {
				t.Fatal("authored version lost")
			}
		})
	}
}
func TestSymlinkRejected(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "x.yaml")
	os.WriteFile(outside, []byte("kind: Secret\n"), 0600)
	os.Symlink(outside, filepath.Join(dir, "x.yaml"))
	if _, err := Collect(context.Background(), model.ScanOptions{Manifests: dir}); err == nil {
		t.Fatal("accepted symlink")
	}
}
func TestOfflineGitRejected(t *testing.T) {
	if _, err := Collect(context.Background(), model.ScanOptions{GitURL: "https://example.com/repo.git", GitRevision: strings.Repeat("a", 40)}); err == nil {
		t.Fatal("offline Git accepted")
	}
}
func TestKustomizeExecRejected(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "kustomization.yaml"), []byte("generators:\n- exec.yaml\n"), 0600)
	if _, err := Collect(context.Background(), model.ScanOptions{Manifests: dir, Render: "kustomize"}); err == nil {
		t.Fatal("generator accepted")
	}
}
func TestHelmLookupRejected(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "templates"), 0700)
	os.WriteFile(filepath.Join(dir, "Chart.yaml"), []byte("apiVersion: v2\nname: example\nversion: 1.0.0\n"), 0600)
	os.WriteFile(filepath.Join(dir, "templates", "x.yaml"), []byte(`{{ lookup "v1" "Secret" "" "" }}`), 0600)
	if _, err := Collect(context.Background(), model.ScanOptions{Manifests: dir, Render: "helm", TargetKubernetes: "1.35.0"}); err == nil {
		t.Fatal("lookup accepted")
	}
}
