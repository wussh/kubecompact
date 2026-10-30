package adapters

import (
	"encoding/json"
	"github.com/wussh/kubecompact/internal/compatibility/fetch"
	"os"
	"testing"
	"time"
)

func TestFixtureAttribution(t *testing.T) {
	b, e := os.ReadFile("../../../testdata/sources/official-regression.json")
	if e != nil {
		t.Fatal(e)
	}
	var f struct {
		CapturedAt time.Time                                `json:"capturedAt"`
		Sources    []struct{ Project, URL, License string } `json:"sources"`
	}
	if e = json.Unmarshal(b, &f); e != nil {
		t.Fatal(e)
	}
	if len(f.Sources) != 15 || f.CapturedAt.IsZero() {
		t.Fatal("fixture metadata incomplete")
	}
	for _, s := range f.Sources {
		if ByID(s.Project) == nil || s.URL == "" || s.License == "" {
			t.Fatal(s)
		}
	}
}
func TestForeignSourceRejected(t *testing.T) {
	_, e := ByID("metrics-server").Parse(fetch.SourceSnapshot{URL: "https://attacker.invalid/matrix", Body: []byte("Metrics Server | Metrics API group/version | Supported Kubernetes version\n---|---|---\n0.8.x | metrics | 1.31+"), RetrievedAt: time.Now()})
	if e == nil {
		t.Fatal("foreign evidence accepted")
	}
}
func TestReleasedStableMetadata(t *testing.T) {
	a := ByID("metrics-server")
	r, e := a.Parse(fetch.SourceSnapshot{URL: "https://api.github.com/repos/kubernetes-sigs/metrics-server/releases", Body: []byte(`[{"tag_name":"v0.8.0","draft":false,"prerelease":false,"published_at":"2025-01-01T00:00:00Z"},{"tag_name":"v0.9.0-rc.1","prerelease":true,"published_at":"2025-01-01T00:00:00Z"}]`), RetrievedAt: time.Now()})
	if e != nil || len(r) != 1 || r[0].Kind != "release" {
		t.Fatalf("%v %#v", e, r)
	}
}
