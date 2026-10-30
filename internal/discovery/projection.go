package discovery

import (
	"github.com/wussh/kubecompact/internal/model"
	"net/url"
	"sort"
	"strings"
	"time"
)

func at(o map[string]any, path ...string) any {
	var v any = o
	for _, p := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[p]
	}
	return v
}
func text(o map[string]any, path ...string) string { v, _ := at(o, path...).(string); return v }
func number(o map[string]any, path ...string) int64 {
	switch v := at(o, path...).(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	}
	return 0
}
func rows(v any) []map[string]any {
	var out []map[string]any
	for _, v := range slice(v) {
		if m, ok := v.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}
func slice(v any) []any { a, _ := v.([]any); return a }
func safeURL(s string) string {
	u, e := url.Parse(s)
	if e != nil {
		return ""
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
func selected(v any, keys ...string) map[string]any {
	m, _ := v.(map[string]any)
	out := map[string]any{}
	for _, k := range keys {
		if x, ok := m[k]; ok {
			out[k] = x
		}
	}
	return out
}
func insert(m map[string]any, path []string, v any) {
	if v == nil {
		return
	}
	for _, p := range path[:len(path)-1] {
		n, ok := m[p].(map[string]any)
		if !ok {
			n = map[string]any{}
			m[p] = n
		}
		m = n
	}
	m[path[len(path)-1]] = v
}
func facts(o map[string]any) map[string]any {
	f := map[string]any{}
	for _, p := range []string{"metadata.generation", "spec.replicas", "spec.minAvailable", "spec.maxUnavailable", "spec.serviceName", "spec.type", "spec.clusterIP", "spec.publishNotReadyAddresses", "status.replicas", "status.readyReplicas", "status.availableReplicas", "status.updatedReplicas", "status.numberReady", "status.desiredNumberScheduled", "status.disruptionsAllowed", "status.observedGeneration", "status.phase"} {
		parts := strings.Split(p, ".")
		insert(f, parts, at(o, parts...))
	}
	for _, p := range []string{"spec.selector", "spec.service", "spec.ports", "endpoints", "ports"} {
		parts := strings.Split(p, ".")
		v := at(o, parts...)
		switch p {
		case "spec.selector":
			if m, ok := v.(map[string]any); ok {
				clean := map[string]any{}
				for k, x := range m {
					if s, ok := x.(string); ok {
						clean[k] = s
					}
				}
				if ml, ok := m["matchLabels"].(map[string]any); ok {
					clean["matchLabels"] = ml
				}
				insert(f, parts, clean)
			}
		case "spec.service":
			insert(f, parts, selected(v, "name", "namespace", "port"))
		case "endpoints":
			var a []any
			for _, e := range rows(v) {
				a = append(a, map[string]any{"conditions": selected(e["conditions"], "ready", "serving", "terminating"), "targetRef": selected(e["targetRef"], "kind", "name", "namespace", "uid")})
			}
			insert(f, parts, a)
		default:
			var a []any
			for _, x := range rows(v) {
				a = append(a, selected(x, "name", "port", "protocol", "targetPort"))
			}
			insert(f, parts, a)
		}
	}
	var conditions []any
	for _, c := range rows(at(o, "status", "conditions")) {
		conditions = append(conditions, selected(c, "type", "status", "reason", "observedGeneration", "lastTransitionTime"))
	}
	if len(conditions) > 0 {
		insert(f, []string{"status", "conditions"}, conditions)
	}
	if text(o, "kind") == "CustomResourceDefinition" {
		var versions []any
		for _, v := range slice(at(o, "status", "storedVersions")) {
			if s, ok := v.(string); ok {
				versions = append(versions, s)
			}
		}
		insert(f, []string{"status", "storedVersions"}, versions)
	}
	switch text(o, "kind") {
	case "CustomResourceDefinition":
		insert(f, []string{"spec", "group"}, at(o, "spec", "group"))
		insert(f, []string{"spec", "names"}, selected(at(o, "spec", "names"), "kind", "plural", "singular"))
		var vs []any
		for _, v := range rows(at(o, "spec", "versions")) {
			vs = append(vs, selected(v, "name", "served", "storage"))
		}
		insert(f, []string{"spec", "versions"}, vs)
		conv := selected(at(o, "spec", "conversion"), "strategy")
		conv["webhook"] = map[string]any{"clientConfig": map[string]any{"service": selected(at(o, "spec", "conversion", "webhook", "clientConfig", "service"), "name", "namespace", "path", "port")}, "conversionReviewVersions": at(o, "spec", "conversion", "webhook", "conversionReviewVersions")}
		insert(f, []string{"spec", "conversion"}, conv)
	case "APIService":
		for _, k := range []string{"group", "version"} {
			insert(f, []string{"spec", k}, at(o, "spec", k))
		}
	case "MutatingWebhookConfiguration", "ValidatingWebhookConfiguration":
		var ws []any
		for _, w := range rows(o["webhooks"]) {
			v := selected(w, "name", "admissionReviewVersions", "failurePolicy", "sideEffects", "timeoutSeconds", "matchPolicy")
			v["clientConfig"] = map[string]any{"service": selected(at(w, "clientConfig", "service"), "name", "namespace", "path", "port")}
			var rules []any
			for _, r := range rows(w["rules"]) {
				rules = append(rules, selected(r, "apiGroups", "apiVersions", "operations", "resources", "scope"))
			}
			v["rules"] = rules
			ws = append(ws, v)
		}
		f["webhooks"] = ws
	}
	return f
}

var labelKeys = []string{"app.kubernetes.io/name", "app.kubernetes.io/instance", "app.kubernetes.io/managed-by", "app.kubernetes.io/version", "helm.sh/chart", "argocd.argoproj.io/instance", "argocd.argoproj.io/installation-id", "helm.toolkit.fluxcd.io/name", "helm.toolkit.fluxcd.io/namespace", "kustomize.toolkit.fluxcd.io/name", "kustomize.toolkit.fluxcd.io/namespace", "kubernetes.io/service-name"}
var annotationKeys = []string{"meta.helm.sh/release-name", "meta.helm.sh/release-namespace", "argocd.argoproj.io/tracking-id", "argocd.argoproj.io/installation-id"}

func stringsOnly(v any, keys []string) map[string]string {
	out := map[string]string{}
	m, _ := v.(map[string]any)
	for _, k := range keys {
		if s, ok := m[k].(string); ok {
			out[k] = s
		}
	}
	return out
}
func FromObjects(objects []map[string]any, mode, scope string) model.Inventory {
	inv := model.NewInventory(mode, scope)
	cluster := model.ID("input", mode, scope)
	inv.Clusters = append(inv.Clusters, model.ClusterObservation{ID: cluster, ObservedAt: inv.StartedAt})
	for _, o := range objects {
		kind := text(o, "kind")
		if kind == "Secret" || kind == "ConfigMap" {
			continue
		}
		ref := model.ResourceRef{ClusterID: cluster, APIVersion: text(o, "apiVersion"), Kind: kind, Name: text(o, "metadata", "name"), Namespace: text(o, "metadata", "namespace"), UID: text(o, "metadata", "uid")}
		if p := strings.Split(ref.APIVersion, "/"); len(p) == 2 {
			ref.Group = p[0]
		}
		if ref.UID == "" {
			ref.Document = text(o, "_document")
		}
		r := model.Resource{Ref: ref, Labels: stringsOnly(at(o, "metadata", "labels"), labelKeys), Annotations: stringsOnly(at(o, "metadata", "annotations"), annotationKeys), Facts: facts(o)}
		for _, owner := range rows(at(o, "metadata", "ownerReferences")) {
			controller, _ := owner["controller"].(bool)
			r.Owners = append(r.Owners, model.OwnerRef{APIVersion: text(owner, "apiVersion"), Kind: text(owner, "kind"), Name: text(owner, "name"), UID: text(owner, "uid"), Controller: controller})
		}
		spec, _ := o["spec"].(map[string]any)
		if kind == "CronJob" {
			spec, _ = at(o, "spec", "jobTemplate", "spec", "template", "spec").(map[string]any)
		} else if kind != "Pod" {
			if s, ok := at(o, "spec", "template", "spec").(map[string]any); ok {
				spec = s
			}
		}
		for _, key := range []string{"containers", "initContainers", "ephemeralContainers"} {
			for _, c := range rows(spec[key]) {
				r.Images = append(r.Images, image(ref, text(c, "name"), "requested", text(c, "image"), ""))
			}
		}
		if kind == "Pod" {
			for _, key := range []string{"containerStatuses", "initContainerStatuses", "ephemeralContainerStatuses"} {
				for _, c := range rows(at(o, "status", key)) {
					r.Images = append(r.Images, image(ref, text(c, "name"), "running", text(c, "image"), text(c, "imageID")))
				}
			}
		}
		inv.Resources = append(inv.Resources, r)
		inv.Objects = append(inv.Objects, o)
		if kind == "Node" {
			n := model.NodeObservation{Name: ref.Name, KubeletVersion: text(o, "status", "nodeInfo", "kubeletVersion"), Architecture: text(o, "status", "nodeInfo", "architecture"), OS: text(o, "status", "nodeInfo", "operatingSystem"), Runtime: text(o, "status", "nodeInfo", "containerRuntimeVersion")}
			for _, c := range rows(at(o, "status", "conditions")) {
				if text(c, "type") == "Ready" && text(c, "status") == "True" {
					n.Ready = true
				}
			}
			labels, _ := at(o, "metadata", "labels").(map[string]any)
			_, n.ControlPlane = labels["node-role.kubernetes.io/control-plane"]
			inv.Clusters[0].Nodes = append(inv.Clusters[0].Nodes, n)
		}
		if componentKind(kind) {
			c := model.Component{ID: ref.Key(), Name: ref.Name, Namespace: ref.Namespace, ClusterID: cluster, ManagedBy: []string{"direct"}, Controller: &ref, Resources: []model.ResourceRef{ref}, Images: r.Images, ObservedAt: inv.StartedAt}
			controller(o, &c, inv.StartedAt)
			identify(&c)
			inv.Components = append(inv.Components, c)
		}
	}
	inv.Finish()
	return inv
}
func componentKind(k string) bool {
	switch k {
	case "Deployment", "DaemonSet", "StatefulSet", "ReplicaSet", "Pod", "Job", "CronJob", "Application", "ApplicationSet", "HelmRelease", "Kustomization", "GitRepository", "HelmRepository", "OCIRepository", "HelmChart":
		return true
	}
	return false
}
func image(r model.ResourceRef, c, role, s, id string) model.ImageObservation {
	v := model.ImageObservation{Resource: r, Container: c, Role: role, Image: s, ImageID: id}
	repo := s
	if p := strings.SplitN(repo, "@", 2); len(p) == 2 {
		repo = p[0]
		v.Digest = p[1]
	}
	if i := strings.LastIndex(repo, ":"); i > strings.LastIndex(repo, "/") {
		v.Tag = repo[i+1:]
		repo = repo[:i]
	}
	v.Repository = repo
	if p := strings.SplitN(repo, "/", 2); len(p) == 2 && (strings.ContainsAny(p[0], ".:") || p[0] == "localhost") {
		v.Registry = p[0]
	}
	return v
}
func identify(c *model.Component) {
	if c.IdentityConfidence == "explicit-mapping" {
		return
	}
	known := map[string]string{"quay.io/cilium/": "cilium", "docker.io/calico/": "calico", "quay.io/jetstack/cert-manager-controller": "cert-manager", "quay.io/jetstack/cert-manager-webhook": "cert-manager", "quay.io/jetstack/cert-manager-cainjector": "cert-manager", "quay.io/argoproj/argocd": "argocd", "ghcr.io/fluxcd/": "flux", "registry.k8s.io/coredns/": "coredns", "registry.k8s.io/ingress-nginx/": "ingress-nginx", "registry.k8s.io/metrics-server/": "metrics-server", "quay.io/prometheus-operator/": "prometheus-operator", "velero/velero": "velero", "ghcr.io/kedacore/": "keda", "ghcr.io/kyverno/": "kyverno", "docker.io/istio/": "istio", "longhornio/": "longhorn", "rook/ceph": "rook-ceph"}
	projects := map[string]bool{}
	for _, i := range c.Images {
		for prefix, p := range known {
			if strings.HasSuffix(prefix, "/") && strings.HasPrefix(i.Repository, prefix) || i.Repository == prefix {
				projects[p] = true
			}
		}
	}
	if len(projects) == 1 {
		for p := range projects {
			c.Project = p
		}
		c.IdentityConfidence = "reviewed-image-origin"
	} else if len(projects) > 1 {
		c.Project = ""
		c.IdentityConfidence = "ambiguous-multiple-projects"
	}
}
func version(c *model.Component, role, scheme, raw, field string, generation int64, success bool, now time.Time) {
	if raw == "" {
		return
	}
	c.Versions = append(c.Versions, model.VersionObservation{Role: role, Scheme: scheme, Raw: raw, SourceID: c.ID, Field: field, ObservedGeneration: generation, Successful: success, ObservedAt: now, Confidence: "observed"})
}
func stable(inv *model.Inventory) {
	sort.Slice(inv.Resources, func(i, j int) bool { return inv.Resources[i].Ref.Key() < inv.Resources[j].Ref.Key() })
	inv.Finish()
}
