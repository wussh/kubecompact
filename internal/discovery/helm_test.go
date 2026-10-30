package discovery

import (
	"encoding/base64"
	"encoding/json"
	"github.com/wussh/kubecompact/internal/model"
	"strings"
	"testing"
)

func TestHelmFailedLatest(t *testing.T) {
	inv := model.NewInventory("live", "test")
	inv.Clusters = []model.ClusterObservation{{ID: "c"}}
	for _, s := range []string{`{"name":"r","namespace":"app","version":1,"info":{"status":"superseded"},"chart":{"metadata":{"name":"chart","version":"1","appVersion":"10"}},"config":{"password":"CANARY"}}`, `{"name":"r","namespace":"app","version":2,"info":{"status":"failed"},"chart":{"metadata":{"name":"chart","version":"2","appVersion":"20"}}}`} {
		rel, e := decodeRelease(base64.StdEncoding.EncodeToString([]byte(s)))
		if e != nil {
			t.Fatal(e)
		}
		addRelease(&inv, rel, "storage")
	}
	inv = Correlate(inv)
	if len(inv.Components) != 1 {
		t.Fatal(inv.Components)
	}
	var deployed string
	for _, v := range inv.Components[0].Versions {
		if v.Role == "deployed" && v.Scheme == "chart" {
			deployed = v.Raw
		}
	}
	if deployed != "1" {
		t.Fatal(inv.Components)
	}
	b, _ := json.Marshal(inv)
	if strings.Contains(string(b), "CANARY") {
		t.Fatal("release values exported")
	}
}

func TestHelmWorkloadOwnershipUsesUniqueReleaseDestination(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stores []string
		flux   bool
		want   int
	}{
		{"unique destination", []string{"storage-a"}, false, 1},
		{"ambiguous destination", []string{"storage-a", "storage-b"}, false, 3},
		{"Flux storage identity", []string{"storage-a"}, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			objects := []map[string]any{object(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"workload","namespace":"workloads","uid":"workload-uid","annotations":{"meta.helm.sh/release-name":"release","meta.helm.sh/release-namespace":"workloads"}}}`)}
			if tc.flux {
				objects = append(objects, object(`{"apiVersion":"helm.toolkit.fluxcd.io/v2","kind":"HelmRelease","metadata":{"name":"controller","namespace":"controllers","uid":"flux-uid"},"spec":{"releaseName":"release","targetNamespace":"workloads","storageNamespace":"storage-a","chart":{"spec":{"chart":"chart","version":"1.0.0"}}}}`))
			}
			inv := FromObjects(objects, "live", "test")
			for _, storage := range tc.stores {
				rel, err := decodeRelease(base64.StdEncoding.EncodeToString([]byte(`{"name":"release","namespace":"workloads","version":1,"info":{"status":"deployed"},"chart":{"metadata":{"name":"chart","version":"1.0.0"}}}`)))
				if err != nil {
					t.Fatal(err)
				}
				addRelease(&inv, rel, storage)
			}
			inv = Correlate(inv)
			if len(inv.Components) != tc.want {
				t.Fatalf("ownership merged incorrectly: %d components", len(inv.Components))
			}
			ambiguous := false
			for _, coverage := range inv.Coverage {
				ambiguous = ambiguous || (coverage.Code == "AMBIGUOUS_MANAGEMENT" && coverage.State == "PARTIAL")
			}
			if ambiguous != (tc.want == 3) {
				t.Fatal("ownership ambiguity was not preserved", inv.Coverage)
			}
			if tc.want == 3 {
				a := model.Assessment{Components: []model.ComponentAssessment{{Verdict: model.Compatible}}, Diagnostics: inv.Diagnostics}
				a.Gate()
				if a.ExitCode != 0 || a.Decision != "NO_KNOWN_BLOCKERS" {
					t.Fatal("compatible baseline was not complete", a.Decision, a.ExitCode)
				}
				a.Coverage = inv.Coverage
				a.Gate()
				if a.ExitCode != 3 || a.Decision != "INCOMPLETE" {
					t.Fatal("ambiguity became malformed input or a pass", a.Decision, a.ExitCode)
				}
			}
			if tc.want == 1 {
				c := inv.Components[0]
				if c.Chart.ReleaseNamespace != "workloads" || c.Chart.StorageNamespace != "storage-a" {
					t.Fatal("namespace identities conflated", c.Chart)
				}
				found := false
				for _, r := range c.Resources {
					found = found || r.UID == "workload-uid"
				}
				if !found || (tc.flux && !contains(c.ManagedBy, "flux")) {
					t.Fatal("workload/Flux identity was lost", c.ManagedBy)
				}
			}
		})
	}
}

func TestCompetingHelmAndArgoOwnershipIsIncomplete(t *testing.T) {
	inv := FromObjects([]map[string]any{
		object(`{"apiVersion":"argoproj.io/v1alpha1","kind":"Application","metadata":{"name":"application","namespace":"controllers","uid":"argo-uid"},"spec":{"destination":{"server":"https://kubernetes.default.svc","namespace":"workloads"},"source":{"repoURL":"https://example.org/repo","path":"app","targetRevision":"main"}}}`),
		object(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"workload","namespace":"workloads","uid":"workload-uid","annotations":{"meta.helm.sh/release-name":"release","meta.helm.sh/release-namespace":"workloads","argocd.argoproj.io/tracking-id":"application:apps/Deployment:workloads/workload"}}}`),
	}, "live", "test")
	rel, err := decodeRelease(base64.StdEncoding.EncodeToString([]byte(`{"name":"release","namespace":"workloads","version":1,"info":{"status":"deployed"},"chart":{"metadata":{"name":"chart","version":"1.0.0"}}}`)))
	if err != nil {
		t.Fatal(err)
	}
	addRelease(&inv, rel, "storage")
	inv = Correlate(inv)
	if len(inv.Components) != 3 {
		t.Fatal("conflicting managers guessed a merge", len(inv.Components))
	}
	a := model.Assessment{Components: []model.ComponentAssessment{{Verdict: model.Compatible}}}
	a.Gate()
	if a.ExitCode != 0 {
		t.Fatal("compatible baseline incomplete", a.Decision)
	}
	a.Coverage, a.Diagnostics = inv.Coverage, inv.Diagnostics
	a.Gate()
	if a.ExitCode != 3 || a.Decision != "INCOMPLETE" {
		t.Fatal("ownership conflict is not malformed input", a.Decision, a.ExitCode)
	}
	found := false
	for _, c := range inv.Coverage {
		found = found || (c.State == "PARTIAL" && c.Code == "AMBIGUOUS_MANAGEMENT")
	}
	if !found {
		t.Fatal("missing ambiguity coverage")
	}
}
