package projects

import "testing"

func TestRegistryCoversTrackerComponents(t *testing.T) {
	tracker := []string{
		"velero", "cert-manager", "cilium", "multus", "redis-operator", "openeverest", "argocd",
		"nvidia-gpu-operator", "metallb", "vcluster", "nvidia-network-operator", "kgateway",
		"kyverno", "kai-scheduler", "kueue", "trident", "kubevirt",
	}
	if got := len(All()); got != len(tracker) {
		t.Fatalf("registry has %d entries, want %d", got, len(tracker))
	}
	for _, id := range tracker {
		project, ok := Lookup(id)
		if !ok || project.ID != id {
			t.Fatalf("missing registry entry %q", id)
		}
		if !project.Expected || len(project.Repositories) == 0 || len(project.APIGroups) == 0 {
			t.Fatalf("incomplete registry entry %q: %+v", id, project)
		}
	}
}

func TestRegistryAliases(t *testing.T) {
	for alias, want := range map[string]string{
		"gpu-operator":        "nvidia-gpu-operator",
		"network-operator":    "nvidia-network-operator",
		"NVIDIA-GPU-Operator": "nvidia-gpu-operator",
	} {
		project, ok := Lookup(alias)
		if !ok || project.ID != want {
			t.Fatalf("alias %q resolved to %q", alias, project.ID)
		}
	}
	if _, ok := Lookup("calico"); ok {
		t.Fatal("unregistered project must not resolve")
	}
}

func TestRegistryRepositoryMatching(t *testing.T) {
	for repository, want := range map[string]string{
		"quay.io/cilium/operator":           "cilium",
		"cilium/cilium":                     "cilium",
		"quay.io/metallb/speaker":           "metallb",
		"docker.io/velero/velero":           "velero",
		"ghcr.io/mellanox/network-operator": "nvidia-network-operator",
	} {
		project, ok := MatchRepository(repository)
		if !ok || project.ID != want {
			t.Fatalf("repository %q resolved to %q", repository, project.ID)
		}
	}
	if _, ok := MatchRepository("example.invalid/unknown"); ok {
		t.Fatal("unknown repository must not resolve")
	}
}
