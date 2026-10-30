package adapters

import (
	"github.com/wussh/kubecompact/internal/compatibility/fetch"
	"github.com/wussh/kubecompact/internal/model"
	"strings"
	"testing"
	"time"
)

func TestSourceUsesApplicationObservation(t *testing.T) {
	a := ByID("cilium")
	cases := []struct {
		name     string
		c        model.Component
		fragment string
	}{
		{"running image", model.Component{Project: "cilium", Images: []model.ImageObservation{{Role: "running", Image: "quay.io/cilium/cilium:v1.17.4"}}}, "/v1.17/"},
		{"app observation", model.Component{Project: "cilium", Versions: []model.VersionObservation{{Role: "deployed", Scheme: "app", Raw: "1.16.3", Field: "release.version"}}}, "/v1.16/"},
		{"chart version not application", model.Component{Project: "cilium", Versions: []model.VersionObservation{{Role: "deployed", Scheme: "chart", Raw: "9.9.9"}}}, "/v1.18/"},
		{"chart appVersion not application", model.Component{Project: "cilium", Versions: []model.VersionObservation{{Role: "deployed", Scheme: "app", Raw: "9.9.9", Field: "helm.history[0]"}}}, "/v1.18/"},
		{"requested image not running", model.Component{Project: "cilium", Images: []model.ImageObservation{{Role: "requested", Image: "quay.io/cilium/cilium:v1.17.4"}}}, "/v1.18/"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := a.Sources(tt.c)[0].URL; !strings.Contains(got, tt.fragment) {
				t.Fatalf("%s lacks %s", got, tt.fragment)
			}
		})
	}
}
func TestCertManagerHistoricalFourColumns(t *testing.T) {
	a := ByID("cert-manager")
	body := "| Release | Release Date | End of Life | Supported Kubernetes / OpenShift Versions | Tested Kubernetes Versions |\n|---|---|---|---|---|\n| 1.20 | Mar 10, 2026 | Release of 1.22 | 1.32 → 1.35 / 4.19 → 4.21 | 1.32 → 1.35 |\n\nOld releases\n\n| Release | Release Date | EOL | Compatible Kubernetes versions |\n|---|---|---|---|\n| 1.19 | Oct 07, 2025 | Jul 08, 2026 | 1.31 → 1.35 |"
	r, e := a.Parse(fetch.SourceSnapshot{URL: a.Sources(model.Component{})[0].URL, Body: []byte(body), RetrievedAt: time.Now()})
	if e != nil {
		t.Fatal(e)
	}
	eol := false
	for _, x := range r {
		if x.ComponentRange == "1.19.x" && x.Kind == "lifecycle" && x.Effect == "EOL" {
			eol = true
		}
	}
	if !eol {
		t.Fatal("missing historical lifecycle")
	}
}

func TestCertManagerUnrelatedKubernetesTablesDoNotEraseSupport(t *testing.T) {
	a := ByID("cert-manager")
	body := "| Release | Release Date | End of Life | Supported Kubernetes / OpenShift Versions | Tested Kubernetes Versions |\n|---|---|---|---|---|\n| 1.20 | Mar 10, 2026 | Release of 1.22 | 1.32 → 1.35 / 4.19 → 4.21 | 1.32 → 1.35 |\n\n| Vendor | Oldest K8s Release | Other Kubernetes Releases |\n|---|---|---|\n| EKS | 1.32 | 1.33, 1.34 |"
	rules, err := a.Parse(fetch.SourceSnapshot{URL: a.Sources(model.Component{})[0].URL, Body: []byte(body), RetrievedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range rules {
		if rule.Kind == "support" && rule.ComponentRange == "1.20.x" {
			return
		}
	}
	t.Fatal("valid support claim was lost")
}

func TestCertManagerLTSDoesNotBecomeCommunitySupport(t *testing.T) {
	a := ByID("cert-manager")
	body := "| Release | Release Date | EOL | Compatible Kubernetes versions | Compatible OpenShift versions |\n|---|---|---|---|---|\n| 1.12 LTS | May 19, 2023 | May 19, 2025 | 1.22 → 1.32 | 4.9 → 4.16 |"
	rules, err := a.Parse(fetch.SourceSnapshot{URL: a.Sources(model.Component{})[0].URL, Body: []byte(body), RetrievedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range rules {
		if rule.Environment["supportChannel"] != "LTS" || rule.ComponentRange != "1.12.x" {
			t.Fatalf("lost LTS applicability: %#v", rule)
		}
	}
}
