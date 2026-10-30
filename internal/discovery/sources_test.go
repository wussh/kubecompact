package discovery

import (
	"github.com/wussh/kubecompact/internal/model"
	"testing"
)

func TestFluxSourceNamespaceIdentity(t *testing.T) {
	inv := FromObjects([]map[string]any{object(`{"apiVersion":"helm.toolkit.fluxcd.io/v2","kind":"HelmRelease","metadata":{"name":"r","namespace":"n"},"spec":{"chart":{"spec":{"chart":"c","version":"1","sourceRef":{"kind":"HelmRepository","name":"repo"}}}}}`), object(`{"apiVersion":"source.toolkit.fluxcd.io/v1","kind":"HelmRepository","metadata":{"name":"repo","namespace":"n"},"spec":{"url":"https://user:password@charts.example.org/path?token=secret"}}`), object(`{"apiVersion":"source.toolkit.fluxcd.io/v1","kind":"HelmRepository","metadata":{"name":"repo","namespace":"other"},"spec":{"url":"https://wrong.example.org"}}`)}, "manifests", "test")
	inv = Correlate(inv)
	for _, c := range inv.Components {
		if c.Chart != nil && c.Chart.Repository != "https://charts.example.org/path" {
			t.Fatal(c.Chart)
		}
	}
}
func TestKubernetesMixedNodeVersions(t *testing.T) {
	inv := model.NewInventory("live", "test")
	inv.Clusters = []model.ClusterObservation{{Distribution: "k3s", Nodes: []model.NodeObservation{{KubeletVersion: "v1.33.13+k3s2"}, {KubeletVersion: "v1.32.13+rke2"}}}}
	clusterConsistency(&inv)
	if inv.Clusters[0].Distribution != "unknown" || len(inv.Clusters[0].Gaps) != 2 {
		t.Fatal(inv.Clusters)
	}
}
func TestIdentityDoesNotMatchLookalike(t *testing.T) {
	c := model.Component{Images: []model.ImageObservation{{Repository: "quay.io/argoproj/argocd-malicious"}}}
	identify(&c)
	if c.Project != "" {
		t.Fatal(c.Project)
	}
	c.Images[0].Repository = "quay.io/argoproj/argocd"
	identify(&c)
	if c.Project != "argocd" {
		t.Fatal(c.Project)
	}
}
