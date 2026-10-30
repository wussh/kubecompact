package discovery

import (
	"github.com/wussh/kubecompact/internal/model"
	"sort"
	"strings"
)

func Correlate(inv model.Inventory) model.Inventory {
	sort.SliceStable(inv.Components, func(i, j int) bool { return inv.Components[i].ID < inv.Components[j].ID })
	resources := map[string]model.Resource{}
	uids := map[string]string{}
	for _, r := range inv.Resources {
		resources[r.Ref.Key()] = r
		if r.Ref.UID != "" {
			uids[model.ID(r.Ref.ClusterID, r.Ref.UID)] = r.Ref.Key()
		}
	}
	components := map[string]model.Component{}
	resourceComponent := map[string]string{}
	helm := map[string]string{}
	aliases := map[string]string{}
	resolve := func(id string) string {
		seen := map[string]bool{}
		for aliases[id] != "" && !seen[id] {
			seen[id] = true
			id = aliases[id]
		}
		return id
	}
	for _, c := range inv.Components {
		if old, ok := components[c.ID]; ok {
			merge(&old, c)
			components[c.ID] = old
		} else {
			components[c.ID] = c
		}
		for _, r := range c.Resources {
			resourceComponent[r.Key()] = c.ID
		}
		if c.Chart != nil && c.Chart.ReleaseName != "" {
			key := model.ID(c.ClusterID, c.Chart.StorageNamespace, c.Chart.ReleaseName)
			if prev, ok := helm[key]; ok && prev != c.ID {
				if contains(c.ManagedBy, "flux") {
					aliases[prev] = c.ID
					helm[key] = c.ID
				} else {
					aliases[c.ID] = prev
				}
			} else {
				helm[key] = c.ID
			}
		}
	}
	parent := map[string]string{}
	for key, r := range resources {
		for _, o := range r.Owners {
			if !o.Controller || o.UID == "" {
				continue
			}
			if owner, ok := uids[model.ID(r.Ref.ClusterID, o.UID)]; ok {
				if prior, exists := parent[key]; exists && prior != owner {
					inv.Diagnostics = append(inv.Diagnostics, model.Diagnostic{Code: "AMBIGUOUS_OWNER", Scope: key, Message: "Conflicting controller UID references"})
					delete(parent, key)
					break
				}
				parent[key] = owner
			}
		}
	}
	// Determine direct controller-to-management joins before walking any descendants.
	for key, r := range resources {
		direct := resourceComponent[key]
		if direct == "" {
			continue
		}
		candidate := ""
		if name := r.Annotations["meta.helm.sh/release-name"]; name != "" {
			candidate = helm[model.ID(r.Ref.ClusterID, r.Annotations["meta.helm.sh/release-namespace"], name)]
		}
		if tracking := r.Annotations["argocd.argoproj.io/tracking-id"]; tracking != "" {
			parts := strings.SplitN(tracking, ":", 2)
			if len(parts) == 2 {
				for id, c := range components {
					if c.ClusterID != r.Ref.ClusterID || !contains(c.ManagedBy, "argocd") || c.Name != parts[0] || c.Controller == nil || c.Controller.Kind != "Application" {
						continue
					}
					if c.Destination != "" && c.Destination != "https://kubernetes.default.svc" && c.Destination != "in-cluster" {
						continue
					}
					expected := r.Ref.Group + "/" + r.Ref.Kind + ":" + r.Ref.Namespace + "/" + r.Ref.Name
					if parts[1] != expected {
						continue
					}
					cr := resources[c.Controller.Key()]
					if cr.Annotations["argocd.argoproj.io/installation-id"] != r.Annotations["argocd.argoproj.io/installation-id"] {
						continue
					}
					if candidate != "" && candidate != id {
						inv.Diagnostics = append(inv.Diagnostics, model.Diagnostic{Code: "AMBIGUOUS_MANAGEMENT", Scope: key, Message: "Conflicting management identities"})
						candidate = ""
						break
					}
					candidate = id
				}
			}
		}
		if candidate != "" && candidate != direct && contains(components[direct].ManagedBy, "direct") {
			aliases[direct] = candidate
		}
	}
	for key := range resources {
		root := key
		seen := map[string]bool{}
		for parent[root] != "" && !seen[root] {
			seen[root] = true
			root = parent[root]
		}
		child := resourceComponent[key]
		owner := resourceComponent[root]
		if child != "" && owner != "" && child != owner && contains(components[child].ManagedBy, "direct") {
			aliases[child] = resolve(owner)
		}
	}
	merged := map[string]model.Component{}
	for _, c := range inv.Components {
		id := resolve(c.ID)
		if _, ok := merged[id]; !ok {
			merged[id] = components[id]
		}
		if c.ID != id {
			target := merged[id]
			merge(&target, c)
			merged[id] = target
		}
	}
	for key, r := range resources {
		root := key
		seen := map[string]bool{}
		for parent[root] != "" && !seen[root] {
			seen[root] = true
			root = parent[root]
		}
		id := resolve(resourceComponent[root])
		c, ok := merged[id]
		if !ok {
			continue
		}
		c.Resources = appendRef(c.Resources, r.Ref)
		for _, im := range r.Images {
			found := false
			for _, existing := range c.Images {
				if existing.Resource.Key() == im.Resource.Key() && existing.Container == im.Container && existing.Role == im.Role && existing.Image == im.Image && existing.ImageID == im.ImageID {
					found = true
				}
			}
			if !found {
				c.Images = append(c.Images, im)
			}
		}
		merged[id] = c
	}
	inv.Components = nil
	for _, c := range merged {
		identify(&c)
		for _, im := range c.Images {
			if im.Role == "running" {
				exists := false
				for _, v := range c.Versions {
					if v.Role == "running" && v.Raw == im.Image {
						exists = true
					}
				}
				if !exists {
					version(&c, "running", "image", im.Image, "status.containerStatuses", 0, false, c.ObservedAt)
				}
			}
		}
		sort.Strings(c.ManagedBy)
		sort.Slice(c.Resources, func(i, j int) bool { return c.Resources[i].Key() < c.Resources[j].Key() })
		sort.SliceStable(c.Images, func(i, j int) bool {
			a, b := c.Images[i], c.Images[j]
			return a.Resource.Key()+a.Container+a.Role+a.Image+a.ImageID < b.Resource.Key()+b.Container+b.Role+b.Image+b.ImageID
		})
		sort.SliceStable(c.Versions, func(i, j int) bool {
			a, b := c.Versions[i], c.Versions[j]
			return a.Role+a.Scheme+a.Field+a.Raw < b.Role+b.Scheme+b.Field+b.Raw
		})
		inv.Components = append(inv.Components, c)
	}
	resolveSources(&inv)
	stable(&inv)
	return inv
}
func contains(a []string, s string) bool {
	for _, v := range a {
		if v == s {
			return true
		}
	}
	return false
}
func appendRef(a []model.ResourceRef, r model.ResourceRef) []model.ResourceRef {
	for _, v := range a {
		if v.Key() == r.Key() {
			return a
		}
	}
	return append(a, r)
}
func merge(a *model.Component, b model.Component) {
	for _, m := range b.ManagedBy {
		if !contains(a.ManagedBy, m) {
			a.ManagedBy = append(a.ManagedBy, m)
		}
	}
	for _, r := range b.Resources {
		a.Resources = appendRef(a.Resources, r)
	}
	a.Images = append(a.Images, b.Images...)
	a.Versions = append(a.Versions, b.Versions...)
	a.Gaps = append(a.Gaps, b.Gaps...)
	if a.Chart == nil {
		a.Chart = b.Chart
	}
	if a.Project == "" {
		a.Project = b.Project
		a.IdentityConfidence = b.IdentityConfidence
	}
	a.Sources = append(a.Sources, b.Sources...)
}
