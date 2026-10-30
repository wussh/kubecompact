package discovery

import (
	"github.com/wussh/kubecompact/internal/model"
	"time"
)

func controller(o map[string]any, c *model.Component, now time.Time) {
	k := text(o, "kind")
	g := number(o, "metadata", "generation")
	observed := number(o, "status", "observedGeneration")
	ready := false
	for _, cond := range rows(at(o, "status", "conditions")) {
		if text(cond, "type") == "Ready" && text(cond, "status") == "True" && number(cond, "observedGeneration") == g {
			ready = true
		}
	}
	if k == "Application" || k == "ApplicationSet" {
		c.ManagedBy = []string{"argocd"}
		c.Status = text(o, "status", "sync", "status")
		c.Destination = safeURL(text(o, "spec", "destination", "server"))
		if c.Destination == "" {
			c.Destination = text(o, "spec", "destination", "name")
		}
		sources := rows(at(o, "spec", "sources"))
		if len(sources) == 0 {
			if s, ok := at(o, "spec", "source").(map[string]any); ok {
				sources = append(sources, s)
			}
		}
		revs := slice(at(o, "status", "sync", "revisions"))
		for i, s := range sources {
			src := model.Source{Index: i, RepoURL: safeURL(text(s, "repoURL")), Chart: text(s, "chart"), Path: text(s, "path"), Revision: text(s, "targetRevision"), Ref: text(s, "ref")}
			src.ValuesOnly = src.Ref != "" && src.Path == "" && src.Chart == ""
			if i < len(revs) {
				src.ResolvedRevision, _ = revs[i].(string)
			} else if len(sources) == 1 {
				src.ResolvedRevision = text(o, "status", "sync", "revision")
			}
			c.Sources = append(c.Sources, src)
			scheme := "git"
			if src.Chart != "" {
				scheme = "chart"
				c.Chart = &model.ChartObservation{Name: src.Chart, Repository: src.RepoURL, Version: src.Revision}
			}
			version(c, "desired", scheme, src.Revision, "spec.sources.targetRevision", g, false, now)
			version(c, "resolved", scheme, src.ResolvedRevision, "status.sync.revisions", g, false, now)
		}
		// Successful operation history is independent of current sync comparison status.
		if text(o, "status", "operationState", "phase") == "Succeeded" {
			result, _ := at(o, "status", "operationState", "syncResult").(map[string]any)
			rs := slice(result["revisions"])
			if len(rs) == 0 && text(result, "revision") != "" {
				rs = []any{text(result, "revision")}
			}
			for _, r := range rs {
				if s, ok := r.(string); ok {
					version(c, "deployed", "git", s, "status.operationState.syncResult.revisions", 0, true, now)
				}
			}
		}
		return
	}
	switch k {
	case "HelmRelease", "Kustomization", "GitRepository", "HelmRepository", "OCIRepository", "HelmChart":
		c.ManagedBy = []string{"flux"}
		if ready {
			c.Status = "Ready"
		} else {
			c.Status = "NotReady"
		}
		if observed != g {
			c.Gaps = append(c.Gaps, "STALE_CONTROLLER_GENERATION")
		}
		if at(o, "spec", "suspend") == true {
			c.Gaps = append(c.Gaps, "RECONCILIATION_SUSPENDED")
		}
	default:
		return
	}
	switch k {
	case "HelmRelease":
		release := text(o, "spec", "releaseName")
		ns := text(o, "spec", "targetNamespace")
		if ns == "" {
			ns = c.Namespace
		}
		if release == "" {
			release = c.Name
			if ns != c.Namespace {
				release = ns + "-" + c.Name
			}
		}
		storage := text(o, "spec", "storageNamespace")
		if storage == "" {
			storage = c.Namespace
		}
		chart, _ := at(o, "spec", "chart", "spec").(map[string]any)
		c.Chart = &model.ChartObservation{Name: text(chart, "chart"), Version: text(chart, "version"), ReleaseName: release, StorageNamespace: storage, Status: c.Status}
		version(c, "desired", "chart", c.Chart.Version, "spec.chart.spec.version", g, false, now)
		version(c, "resolved", "chart", text(o, "status", "lastAttemptedRevision"), "status.lastAttemptedRevision", observed, false, now)
		var best map[string]any
		for _, h := range rows(at(o, "status", "history")) {
			if text(h, "status") == "deployed" || text(h, "status") == "superseded" {
				if best == nil || number(h, "version") > number(best, "version") {
					best = h
				}
			}
		}
		if best != nil {
			version(c, "deployed", "chart", text(best, "chartVersion"), "status.history.chartVersion", observed, true, now)
			version(c, "deployed", "app", text(best, "appVersion"), "status.history.appVersion", observed, true, now)
		}
		if ref, ok := at(o, "spec", "chartRef").(map[string]any); ok {
			c.Sources = append(c.Sources, model.Source{Kind: text(ref, "kind"), Path: text(ref, "namespace") + "/" + text(ref, "name")})
		} else if ref, ok := chart["sourceRef"].(map[string]any); ok {
			c.Sources = append(c.Sources, model.Source{Kind: text(ref, "kind"), Path: text(ref, "namespace") + "/" + text(ref, "name")})
		}
	case "Kustomization":
		version(c, "resolved", "git", text(o, "status", "lastAttemptedRevision"), "status.lastAttemptedRevision", observed, false, now)
		version(c, "deployed", "git", text(o, "status", "lastAppliedRevision"), "status.lastAppliedRevision", observed, true, now)
		c.Sources = append(c.Sources, model.Source{Kind: text(o, "spec", "sourceRef", "kind"), Path: text(o, "spec", "path"), Ref: text(o, "spec", "sourceRef", "name")})
		c.Destination = "local"
		if at(o, "spec", "kubeConfig") != nil {
			c.Destination = "remote-explicit-kubeconfig"
			c.Gaps = append(c.Gaps, "REMOTE_DESTINATION_NOT_OBSERVED")
		}
	default:
		c.Sources = append(c.Sources, model.Source{Kind: k, RepoURL: safeURL(text(o, "spec", "url")), Chart: text(o, "spec", "chart"), Revision: text(o, "spec", "ref", "tag")})
		version(c, "resolved", "oci", text(o, "status", "artifact", "revision"), "status.artifact.revision", observed, false, now)
		version(c, "desired", "chart", text(o, "spec", "version"), "spec.version", g, false, now)
	}
}
