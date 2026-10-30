package manifests

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"github.com/wussh/kubecompact/internal/model"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalHelmCapabilitiesAndHookRemoval(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "templates"), 0700)
	os.WriteFile(filepath.Join(dir, "Chart.yaml"), []byte("apiVersion: v2\nname: demo\nversion: 1.0.0\nappVersion: 2.3.4\n"), 0600)
	os.WriteFile(filepath.Join(dir, "templates", "deployment.yaml"), []byte("{{ if .Capabilities.APIVersions.Has \"apps/v1/Deployment\" }}\napiVersion: apps/v1\nkind: Deployment\nmetadata: {name: demo}\nspec: {replicas: 2}\n{{ end }}"), 0600)
	os.WriteFile(filepath.Join(dir, "templates", "hook.yaml"), []byte("apiVersion: batch/v1\nkind: Job\nmetadata:\n  name: hook\n  annotations: {helm.sh/hook: pre-install}\n"), 0600)
	inv, e := Collect(context.Background(), model.ScanOptions{Manifests: dir, Render: "helm", TargetKubernetes: "1.35"})
	if e != nil {
		t.Fatal(e)
	}
	if len(inv.Resources) != 1 || inv.Resources[0].Ref.Kind != "Deployment" {
		t.Fatalf("resources=%v", inv.Resources)
	}
}
func TestLocalKustomize(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "kustomization.yaml"), []byte("resources: [deployment.yaml]\nnamePrefix: test-\n"), 0600)
	os.WriteFile(filepath.Join(dir, "deployment.yaml"), []byte("apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: demo}\nspec:\n  replicas: 2\n"), 0600)
	inv, e := Collect(context.Background(), model.ScanOptions{Manifests: dir, Render: "kustomize"})
	if e != nil {
		t.Fatal(e)
	}
	if len(inv.Resources) != 1 || inv.Resources[0].Ref.Name != "test-demo" {
		t.Fatalf("resources=%v", inv.Resources)
	}
}
func TestArchiveRejectsTraversalAndLinks(t *testing.T) {
	for _, header := range []*tar.Header{{Name: "../escape", Mode: 0600, Size: 1, Typeflag: tar.TypeReg}, {Name: "chart/link", Linkname: "/etc/passwd", Typeflag: tar.TypeSymlink}, {Name: "chart/huge", Size: maxFile + 1, Typeflag: tar.TypeReg}} {
		var b bytes.Buffer
		gz := gzip.NewWriter(&b)
		tw := tar.NewWriter(gz)
		tw.WriteHeader(header)
		if header.Size == 1 {
			tw.Write([]byte("x"))
		}
		tw.Close()
		gz.Close()
		if e := extractChart(b.Bytes(), t.TempDir()); e == nil {
			t.Fatalf("accepted archive %q", header.Name)
		}
	}
}
func TestRemoteBaseRequiresPinnedRevision(t *testing.T) {
	repo, path, sha, e := parseKustomizeGit("https://github.com/example/repo//base?ref=0123456789abcdef0123456789abcdef01234567")
	if e != nil || repo != "https://github.com/example/repo.git" || path != "base" || len(sha) != 40 {
		t.Fatalf("parse=%s/%s/%s %v", repo, path, sha, e)
	}
	for _, s := range []string{"https://github.com/example/repo//base?ref=main", "ssh://example.org/repo//base?ref=0123456789abcdef0123456789abcdef01234567"} {
		if _, _, _, e := parseKustomizeGit(s); e == nil {
			t.Fatalf("accepted %q", s)
		}
	}
}
func TestGitObjectAllocationBound(t *testing.T) {
	s := &boundedStore{}
	o := &boundedObject{store: s}
	o.SetSize(maxFile + 1)
	if _, e := o.Writer(); e == nil {
		t.Fatal("oversized object writer accepted")
	}
	o.SetSize(0)
	o.written = maxFile
	if _, e := o.Write([]byte("x")); e == nil {
		t.Fatal("object bound bypassed")
	}
}
