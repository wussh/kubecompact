package discovery

import "github.com/wussh/kubecompact/internal/model"

func clusterConsistency(inv *model.Inventory) {
	for i := range inv.Clusters {
		c := &inv.Clusters[i]
		distros := map[string]bool{}
		if c.Distribution != "" && c.Distribution != "unknown" {
			distros[c.Distribution] = true
		}
		versions := map[string]bool{}
		for _, n := range c.Nodes {
			versions[n.KubeletVersion] = true
			if d := distribution(n.KubeletVersion); d != "unknown" {
				distros[d] = true
			}
		}
		if len(versions) > 1 {
			c.Gaps = append(c.Gaps, "MIXED_NODE_VERSIONS")
		}
		if len(distros) > 1 {
			c.Distribution = "unknown"
			c.Confidence = "conflicting"
			c.Gaps = append(c.Gaps, "CONFLICTING_DISTRIBUTION_EVIDENCE")
		}
	}
}
