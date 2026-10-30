package discovery

import (
	"github.com/wussh/kubecompact/internal/model"
	"strings"
)

func resolveSources(inv *model.Inventory) {
	byRef := map[string]model.Component{}
	for _, c := range inv.Components {
		if c.Controller != nil {
			byRef[model.ID(c.ClusterID, c.Controller.Kind, c.Namespace, c.Name)] = c
		}
	}
	for i := range inv.Components {
		c := &inv.Components[i]
		if !contains(c.ManagedBy, "flux") {
			continue
		}
		for j := range c.Sources {
			s := &c.Sources[j]
			if s.RepoURL != "" {
				continue
			}
			namespace, name := c.Namespace, s.Ref
			if s.Path != "" {
				parts := strings.SplitN(s.Path, "/", 2)
				if len(parts) == 2 {
					if parts[0] != "" {
						namespace = parts[0]
					}
					name = parts[1]
				}
			}
			source, ok := byRef[model.ID(c.ClusterID, s.Kind, namespace, name)]
			if !ok {
				continue
			}
			if len(source.Sources) > 0 {
				s.RepoURL = source.Sources[0].RepoURL
			}
			for _, v := range source.Versions {
				if v.Role == "resolved" {
					s.ResolvedRevision = v.Raw
				}
			}
			if c.Chart != nil && s.Kind == "HelmRepository" {
				c.Chart.Repository = s.RepoURL
			}
		}
	}
}
