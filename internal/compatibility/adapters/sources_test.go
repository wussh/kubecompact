package adapters

import (
	"encoding/json"
	"errors"
	"github.com/wussh/kubecompact/internal/compatibility/fetch"
	"github.com/wussh/kubecompact/internal/model"
	"testing"
	"time"
)

func TestVersionedRequirementsAndContext(t *testing.T) {
	cases := []struct{ id, body, kind string }{
		{"cert-manager", "Only the last patch release of each branch is supported.\n| Release | Release Date | End of Life | Supported Kubernetes / OpenShift Versions | Tested Kubernetes Versions |\n|---|---|---|---|---|\n| 1.20 | Mar 10, 2026 | Release of 1.22 | 1.32 → 1.35 / 4.19 → 4.21 | 1.32 → 1.35 |", "support"},
		{"cilium", "| k8s Version | k8s NetworkPolicy API | CiliumNetworkPolicy |\n|---|---|---|\n| 1.30, 1.31, 1.32, 1.33 | networking.k8s.io/v1 | cilium.io/v2 |", "support"},
		{"calico", "We test Calico v3.33 against the following Kubernetes versions. Other versions may work, but we are not actively testing them.\n- 1.35\n- 1.36\n- 1.37\nNewer versions may also work", "tested"},
		{"coredns", "This plugin implements the Kubernetes DNS-Based Service Discovery Specification.", "context"},
		{"prometheus-operator", "The Prometheus Operator requires at least Kubernetes version `1.16.0`.", "requirement"},
		{"kyverno", "| | |\n|---|---|\n| Supported Release: | v1.19 (released: Aug 2026) |\n| Kubernetes Versions Supported: | v1.33 - v1.35 |", "tested"},
		{"longhorn", "Kubernetes >= v1.25", "requirement"},
		{"rook-ceph", "Kubernetes versions **v1.29** through **v1.34** are supported.", "support"},
	}
	for _, tt := range cases {
		t.Run(tt.id, func(t *testing.T) {
			a := ByID(tt.id)
			s := fetch.SourceSnapshot{URL: a.Sources(model.Component{})[0].URL, Body: []byte(tt.body), RetrievedAt: time.Now().UTC()}
			rs, e := a.Parse(s)
			if e != nil || len(rs) == 0 {
				t.Fatalf("%d %v", len(rs), e)
			}
			found := false
			for _, rule := range rs {
				if rule.Kind == tt.kind {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing %s evidence: %#v", tt.kind, rs)
			}
		})
	}
}
func TestFluxPatchSpecificRelease(t *testing.T) {
	a := ByID("flux")
	b, _ := json.Marshal(map[string]any{"tag_name": "v2.7.0", "body": "This release is compatible with the following Kubernetes versions:\n| Kubernetes version | Minimum required |\n|---|---|\n| v1.32 | >= 1.32.0 |\n| v1.33 | >= 1.33.0 |\n| v1.34 | >= 1.34.1 |"})
	r, e := a.Parse(fetch.SourceSnapshot{URL: "https://api.github.com/repos/fluxcd/flux2/releases/tags/v2.7.0", Body: b, RetrievedAt: time.Now()})
	if e != nil || len(r) != 1 {
		t.Fatalf("%v %#v", e, r)
	}
}
func TestMalformedMatrixRejected(t *testing.T) {
	a := ByID("metrics-server")
	_, e := a.Parse(fetch.SourceSnapshot{URL: a.Sources(model.Component{})[0].URL, Body: []byte("Metrics Server | Metrics API group/version | Supported Kubernetes version\n---|---|---\n0.8.x | metrics | banana"), RetrievedAt: time.Now()})
	if !errors.Is(e, ErrMalformedEvidence) {
		t.Fatal(e)
	}
}
func TestHTMLMatrix(t *testing.T) {
	a := ByID("argocd")
	r, e := a.Parse(fetch.SourceSnapshot{URL: a.Sources(model.Component{})[0].URL, Body: []byte("<table><tr><th>Argo CD version</th><th>Kubernetes versions</th></tr><tr><td>3.3</td><td>v1.33, v1.34</td></tr></table>"), RetrievedAt: time.Now()})
	if e != nil || len(r) != 1 {
		t.Fatalf("%v %#v", e, r)
	}
}
