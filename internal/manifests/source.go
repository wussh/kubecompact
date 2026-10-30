package manifests

import (
	"errors"
	"github.com/wussh/kubecompact/internal/model"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func safeRead(root, path string) ([]byte, error) {
	info, e := os.Stat(root)
	if e != nil {
		return nil, errors.New("input unavailable")
	}
	if !info.IsDir() {
		root = filepath.Dir(root)
	}
	rel, e := filepath.Rel(root, path)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, errors.New("input outside root")
	}
	r, e := os.OpenRoot(root)
	if e != nil {
		return nil, errors.New("input root unavailable")
	}
	defer r.Close()
	f, e := r.Open(rel)
	if e != nil {
		return nil, errors.New("unsafe input file")
	}
	defer f.Close()
	fi, e := f.Stat()
	if e != nil || !fi.Mode().IsRegular() || fi.Size() > maxFile {
		return nil, errors.New("unsafe input file")
	}
	b, e := io.ReadAll(io.LimitReader(f, maxFile+1))
	if e != nil || len(b) > maxFile {
		return nil, errors.New("input read failed or size limit exceeded")
	}
	return b, nil
}
func annotateSources(inv *model.Inventory, root string, opts model.ScanOptions, receipt model.Source) {
	var chart *model.ChartObservation
	if opts.Render == "helm" {
		if b, e := safeRead(root, filepath.Join(root, "Chart.yaml")); e == nil {
			if meta, e := inputMap(b); e == nil {
				chart = &model.ChartObservation{Name: toText(meta["name"]), Version: toText(meta["version"]), AppVersion: toText(meta["appVersion"]), KubeVersion: toText(meta["kubeVersion"]), Status: "rendered-only"}
			}
		}
	}
	receipt.Kind, receipt.Path = "Raw", "."
	if opts.Render == "helm" {
		receipt.Kind = "Helm"
	} else if opts.Render == "kustomize" {
		receipt.Kind = "Kustomize"
	}
	if opts.GitURL != "" {
		receipt.RepoURL = model.SafeURL(opts.GitURL)
		receipt.Path = opts.GitPath
		receipt.Revision, receipt.ResolvedRevision = opts.GitRevision, opts.GitRevision
	}
	inv.Sources = append(inv.Sources, receipt)
	for i := range inv.Components {
		c := &inv.Components[i]
		kind := "Raw"
		if opts.Render == "helm" {
			kind = "Helm"
			c.ManagedBy = append(c.ManagedBy, "local-helm-render")
			if chart != nil {
				observation := *chart
				c.Chart = &observation
				c.Versions = append(c.Versions, model.VersionObservation{Role: "desired", Scheme: "chart", Raw: chart.Version, SourceID: c.ID, Field: "Chart.yaml.version", ObservedAt: inv.StartedAt, Confidence: "declared"})
				if chart.AppVersion != "" {
					c.Versions = append(c.Versions, model.VersionObservation{Role: "desired", Scheme: "app", Raw: chart.AppVersion, SourceID: c.ID, Field: "Chart.yaml.appVersion", ObservedAt: inv.StartedAt, Confidence: "chart-metadata"})
				}
			}
		} else if opts.Render == "kustomize" {
			kind = "Kustomize"
			c.ManagedBy = append(c.ManagedBy, "local-kustomize-render")
		}
		source := receipt
		source.Kind, source.Path = kind, "."
		if opts.GitURL != "" {
			source.RepoURL = model.SafeURL(opts.GitURL)
			source.Path = opts.GitPath
			source.Revision = opts.GitRevision
			source.ResolvedRevision = opts.GitRevision
			c.Versions = append(c.Versions, model.VersionObservation{Role: "resolved", Scheme: "git", Raw: opts.GitRevision, SourceID: c.ID, Field: "git.commit", ObservedAt: inv.StartedAt, Confidence: "observed"})
		}
		c.Sources = append(c.Sources, source)
		c.Gaps = append(c.Gaps, "Local rendering does not establish deployed or running state; controller-specific options are not reproduced")
	}
	if opts.GitURL != "" {
		inv.InputMode = "git"
	}
}
