// Package projects is the reviewed project registry used to interpret saved
// inventory identities for the compatibility matrix. Entries are identity
// hints only: they never assert Kubernetes support and never promote a status.
package projects

import "strings"

// Project is one expected component identity.
type Project struct {
	ID    string
	Alias []string
	Name  string
	// Category mirrors the compatibility tracker's component category.
	Category string
	// Expected marks a project whose absence may be reported once coverage is complete.
	Expected bool
	// Repositories are lowercase image repository prefixes (registry included).
	Repositories []string
	// APIGroups are lowercase CRD API groups the project owns.
	APIGroups []string
}

// All returns the registry in tracker component order.
func All() []Project {
	return []Project{
		{ID: "velero", Name: "Velero", Category: "backup", Expected: true,
			Repositories: []string{"velero/velero", "docker.io/velero/velero"}, APIGroups: []string{"velero.io"}},
		{ID: "cert-manager", Name: "cert-manager", Category: "certificates", Expected: true,
			Repositories: []string{"quay.io/jetstack/cert-manager-controller", "quay.io/jetstack/cert-manager-webhook", "quay.io/jetstack/cert-manager-cainjector", "quay.io/jetstack/cert-manager-acmesolver", "quay.io/jetstack/cert-manager-startupapicheck"},
			APIGroups:    []string{"cert-manager.io", "acme.cert-manager.io"}},
		{ID: "cilium", Name: "Cilium", Category: "cni", Expected: true,
			Repositories: []string{"quay.io/cilium/", "docker.io/cilium/"}, APIGroups: []string{"cilium.io"}},
		{ID: "multus", Name: "Multus CNI", Category: "cni", Expected: true,
			Repositories: []string{"ghcr.io/k8snetworkplumbingwg/multus-cni", "docker.io/rancher/hardened-multus-cni", "docker.io/intel/multus-cni"}, APIGroups: []string{"k8s.cni.cncf.io"}},
		{ID: "redis-operator", Name: "Redis Operator", Category: "database", Expected: true,
			Repositories: []string{"ghcr.io/redis/redis-operator", "docker.io/redis/redis-operator", "ghcr.io/spotahome/redis-operator", "docker.io/spotahome/redis-operator"},
			APIGroups:    []string{"redis.redis.opstreelabs.in", "app.redislabs.com", "databases.spotahome.com"}},
		{ID: "openeverest", Name: "OpenEverest", Category: "database-platform", Expected: true,
			Repositories: []string{"ghcr.io/openeverest/", "docker.io/percona/everest-operator", "docker.io/percona/everest"}, APIGroups: []string{"everest.percona.com"}},
		{ID: "argocd", Name: "Argo CD", Category: "gitops", Expected: true,
			Repositories: []string{"quay.io/argoproj/argocd", "ghcr.io/argoproj/argocd"}, APIGroups: []string{"argoproj.io"}},
		{ID: "nvidia-gpu-operator", Alias: []string{"gpu-operator"}, Name: "NVIDIA GPU Operator", Category: "gpu", Expected: true,
			Repositories: []string{"nvcr.io/nvidia/gpu-operator", "docker.io/nvidia/gpu-operator"}, APIGroups: []string{"nvidia.com"}},
		{ID: "metallb", Name: "MetalLB", Category: "load-balancer", Expected: true,
			Repositories: []string{"quay.io/metallb/", "ghcr.io/metallb/"}, APIGroups: []string{"metallb.io"}},
		{ID: "vcluster", Name: "vCluster", Category: "multi-tenancy", Expected: true,
			Repositories: []string{"ghcr.io/loft-sh/", "docker.io/loftsh/"}, APIGroups: []string{"management.loft.sh", "storage.loft.sh", "cluster.loft.sh"}},
		{ID: "nvidia-network-operator", Alias: []string{"network-operator"}, Name: "NVIDIA Network Operator", Category: "network", Expected: true,
			Repositories: []string{"ghcr.io/mellanox/network-operator", "nvcr.io/nvidia/network-operator"}, APIGroups: []string{"mellanox.com"}},
		{ID: "kgateway", Name: "kgateway", Category: "networking", Expected: true,
			Repositories: []string{"ghcr.io/kgateway-dev/", "docker.io/kgateway-dev/"}, APIGroups: []string{"gateway.kgateway.dev", "kgateway.dev"}},
		{ID: "kyverno", Name: "Kyverno", Category: "policy", Expected: true,
			Repositories: []string{"ghcr.io/kyverno/", "docker.io/kyverno/"}, APIGroups: []string{"kyverno.io", "wgpolicyk8s.io"}},
		{ID: "kai-scheduler", Name: "KAI Scheduler", Category: "scheduling", Expected: true,
			Repositories: []string{"ghcr.io/kai-scheduler/", "gcr.io/kai-scheduler/"}, APIGroups: []string{"kai.scheduler", "scheduling.run.ai"}},
		{ID: "kueue", Name: "Kueue", Category: "scheduling", Expected: true,
			Repositories: []string{"registry.k8s.io/kueue/", "gcr.io/k8s-staging-kueue/"}, APIGroups: []string{"kueue.x-k8s.io"}},
		{ID: "trident", Name: "NetApp Trident", Category: "storage", Expected: true,
			Repositories: []string{"docker.io/netapp/trident-operator", "docker.io/netapp/trident"}, APIGroups: []string{"trident.netapp.io"}},
		{ID: "kubevirt", Name: "KubeVirt", Category: "virtualization", Expected: true,
			Repositories: []string{"quay.io/kubevirt/", "registry.k8s.io/kubevirt/"}, APIGroups: []string{"kubevirt.io"}},
	}
}

// Lookup resolves a tracker or inventory component id, including the documented
// vendor-prefix aliases, to its registry entry.
func Lookup(id string) (Project, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		return Project{}, false
	}
	for _, p := range All() {
		if p.ID == id {
			return p, true
		}
		for _, alias := range p.Alias {
			if alias == id {
				return p, true
			}
		}
	}
	// Inventory ids drop the "nvidia-" vendor prefix.
	if strings.HasPrefix(id, "nvidia-") {
		return Lookup(strings.TrimPrefix(id, "nvidia-"))
	}
	return Project{}, false
}

// MatchRepository reports the project whose repository prefix contains repo.
// Inventory may retain or omit the registry host, so both forms are compared.
func MatchRepository(repo string) (Project, bool) {
	repo = normalizeRepository(repo)
	if repo == "" {
		return Project{}, false
	}
	for _, p := range All() {
		for _, prefix := range p.Repositories {
			if strings.HasPrefix(repo, normalizeRepository(prefix)) {
				return p, true
			}
		}
	}
	return Project{}, false
}

func normalizeRepository(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	first, rest, ok := strings.Cut(value, "/")
	if ok && rest != "" && (strings.ContainsAny(first, ".:") || first == "localhost") {
		return rest
	}
	return value
}

// MatchProject reports the project for an inventory project or component name.
func MatchProject(name string) (Project, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return Project{}, false
	}
	for _, p := range All() {
		if strings.ToLower(p.Name) == name {
			return p, true
		}
	}
	return Lookup(name)
}
