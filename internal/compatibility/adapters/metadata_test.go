package adapters

import (
	"crypto/sha256"
	"fmt"
	"github.com/wussh/kubecompact/internal/compatibility"
	"github.com/wussh/kubecompact/internal/compatibility/fetch"
	"github.com/wussh/kubecompact/internal/model"
	"strings"
	"testing"
	"time"
)

func TestOfficialChartMappingIsNotRuntimeSupport(t *testing.T) {
	a := ByID("cert-manager")
	now := time.Now().UTC()
	body := "apiVersion: v1\nentries:\n  cert-manager:\n  - name: cert-manager\n    version: v2.0.0\n    appVersion: v1.2.3\n    created: 2025-01-01T00:00:00Z\n    digest: " + strings.Repeat("a", 64) + "\n    kubeVersion: '>=1.30.0-0'\n  - name: cert-manager\n    version: 2.0.1-rc.1\n    appVersion: v1.2.3\n    created: 2025-01-01T00:00:00Z\n    digest: " + strings.Repeat("b", 64) + "\n"
	rules, err := a.Parse(fetch.SourceSnapshot{URL: certChartIndex, Body: []byte(body), RetrievedAt: now})
	if err != nil || len(rules) != 1 {
		t.Fatalf("%v %v", rules, err)
	}
	r := rules[0]
	if r.Kind != "chart-mapping" || r.ChartVersions["1.2.3"] != "2.0.0" || r.ChartRepository != "https://charts.jetstack.io" || r.ChartName != "cert-manager" {
		t.Fatalf("wrong app/chart identity: %+v", r)
	}
	c := model.Component{Project: "cert-manager", IdentityConfidence: "HIGH", Versions: []model.VersionObservation{{Role: "running", Raw: "1.2.3", Scheme: "semver"}}}
	assessed := compatibility.Evaluate(model.Inventory{Components: []model.Component{c}}, rules, model.CheckOptions{TargetKubernetes: "1.35", Now: now})
	if assessed.Components[0].Verdict != model.Unknown {
		t.Fatal("chart mapping invented runtime support")
	}
}

func TestArtifactHubRemainsSupportingEvidence(t *testing.T) {
	a := ByID("cert-manager")
	rules, err := a.Parse(fetch.SourceSnapshot{URL: certArtifactHub, Body: []byte(`{"name":"cert-manager","available_versions":[{"version":"2.0.0","app_version":"v1.2.3","prerelease":false}]}`), RetrievedAt: time.Now().UTC()})
	if err != nil || len(rules) != 1 || rules[0].Evidence.Priority != 4 || rules[0].Kind != "chart-mapping" {
		t.Fatalf("%+v %v", rules, err)
	}
}

func TestOCIConfigDigestAndDiscoveryBoundaries(t *testing.T) {
	a := ByID("cert-manager")
	body := []byte(`{"name":"cert-manager","version":"v2.0.0","appVersion":"v1.2.3","kubeVersion":">=1.30.0-0"}`)
	digest := fmt.Sprintf("%x", sha256.Sum256(body))
	snapshot := fetch.SourceSnapshot{URL: certOCI + "blobs/sha256:" + digest, Body: body, RetrievedAt: time.Now().UTC()}
	rules, err := a.Parse(snapshot)
	if err != nil || rules[0].ChartVersions["1.2.3"] != "2.0.0" {
		t.Fatalf("%+v %v", rules, err)
	}
	snapshot.Body = append(snapshot.Body, ' ')
	if _, err := a.Parse(snapshot); err == nil {
		t.Fatal("changed OCI config accepted under original digest")
	}
	manifest := fetch.SourceSnapshot{URL: certOCI + "manifests/v2.0.0", Body: []byte(`{"config":{"mediaType":"application/vnd.cncf.helm.config.v1+json","digest":"sha256:` + digest + `","size":100}}`)}
	next := Discover("cert-manager", manifest)
	if len(next) != 1 || next[0].URL != certOCI+"blobs/sha256:"+digest {
		t.Fatalf("wrong digest follow-up: %+v", next)
	}
	tags := fetch.SourceSnapshot{URL: certOCI + "tags/list", Body: []byte(`{"tags":["v1.0.0","v1.2.3","v1.3.0","v2.0.0-rc.1","../../escape"]}`)}
	next = Discover("cert-manager", tags)
	if len(next) != 2 || next[0].URL != certOCI+"manifests/v1.3.0" || next[1].URL != certOCI+"manifests/v1.2.3" {
		t.Fatalf("unbounded/unstable discovery: %+v", next)
	}
	foreign := fetch.SourceSnapshot{URL: "https://attacker.invalid/page", Body: []byte(`https://cert-manager.io/v1.2/compatibility/`)}
	if len(Discover("cert-manager", foreign)) != 0 {
		t.Fatal("untrusted discovery expanded sources")
	}
	docs := fetch.SourceSnapshot{URL: "https://docs.cilium.io/en/v1.18/network/kubernetes/compatibility/", Body: []byte(`https://docs.cilium.io/en/v1.19/network/kubernetes/compatibility/ https://attacker.invalid/v1.20/compatibility/ https://docs.cilium.io/en/v1.19/network/kubernetes/compatibility/?token=CANARY`)}
	next = Discover("cilium", docs)
	if len(next) != 1 || strings.Contains(next[0].URL, "CANARY") {
		t.Fatalf("unsafe document follow-up %+v", next)
	}
}
