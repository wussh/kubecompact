package adapters

import (
	"crypto/sha256"
	"fmt"
	"github.com/wussh/kubecompact/internal/compatibility/fetch"
	"github.com/wussh/kubecompact/internal/model"
	"testing"
	"time"
)

func TestOfficialMatrixParsers(t *testing.T) {
	cases := []struct{ id, body string }{
		{"metrics-server", "Metrics Server | Metrics API group/version | Supported Kubernetes version\n---|---|---\n0.8.x | metrics.k8s.io/v1beta1 | 1.31+"},
		{"argocd", "| Argo CD version | Kubernetes versions |\n|---|---|\n| 3.3 | v1.35, v1.34, v1.33, v1.32 |"},
		{"keda", "| KEDA | Kubernetes |\n|---|---|\n| v2.18 | v1.31 - v1.33 |"},
		{"istio", "| Version | Currently Supported | Release Date | End of Life | Supported Kubernetes Versions | Tested, but not supported |\n|---|---|---|---|---|---|\n| 1.29 | No | Feb 16, 2026 | Oct 15, 2026 | 1.31, 1.32, 1.33 | 1.28, 1.29 |"},
		{"ingress-nginx", "| Supported | Ingress-NGINX version | k8s supported version | Alpine Version | Nginx Version | Helm Chart Version |\n|---|---|---|---|---|---|\n| yes | v1.14.0 | 1.32, 1.33 | 3.22 | 1.27 | 4.14.0 |"},
		{"velero", "| Velero version | Expected Kubernetes version compatibility | Tested on Kubernetes version |\n|---|---|---|\n| 1.17 | 1.18-latest | 1.31.7, 1.32.3 |"},
	}
	for _, tt := range cases {
		t.Run(tt.id, func(t *testing.T) {
			a := ByID(tt.id)
			s := fetch.SourceSnapshot{URL: a.Sources(model.Component{})[0].URL, Body: []byte(tt.body), RetrievedAt: time.Now().UTC()}
			s.SHA256 = fmt.Sprintf("%x", sha256.Sum256(s.Body))
			rules, e := a.Parse(s)
			if e != nil || len(rules) == 0 {
				t.Fatalf("rules=%d error=%v", len(rules), e)
			}
			for _, r := range rules {
				if r.Evidence.SourceHash != s.SHA256 || r.Evidence.URL != s.URL {
					t.Fatal("lost provenance")
				}
			}
			s.Body = []byte("no matrix")
			if _, e = a.Parse(s); e == nil {
				t.Fatal("missing table must be visible")
			}
		})
	}
}
func TestAllRegistered(t *testing.T) {
	if len(All()) != 15 {
		t.Fatal(len(All()))
	}
	for _, a := range All() {
		if len(a.Sources(model.Component{})) == 0 {
			t.Fatal(a.ID())
		}
	}
}
