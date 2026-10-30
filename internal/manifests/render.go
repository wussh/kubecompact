package manifests

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"text/template/parse"

	"github.com/Masterminds/semver/v3"
	"github.com/wussh/kubecompact/data"
	"github.com/wussh/kubecompact/internal/model"
	"go.yaml.in/yaml/v3"
	"helm.sh/helm/v4/pkg/chart"
	"helm.sh/helm/v4/pkg/chart/common"
	chartutil "helm.sh/helm/v4/pkg/chart/common/util"
	"helm.sh/helm/v4/pkg/chart/loader"
	"helm.sh/helm/v4/pkg/engine"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/kyaml/filesys"
)

func renderHelm(root string, opts model.ScanOptions) ([]inputDocument, error) {
	if opts.TargetKubernetes == "" {
		return nil, errors.New("Helm requires explicit target Kubernetes")
	}
	ch, err := loader.Load(root)
	if err != nil {
		return nil, errors.New("chart load failed")
	}
	if err = inspectChart(ch); err != nil {
		return nil, err
	}
	values := map[string]any{}
	if opts.ValuesFile != "" {
		p, err := filepath.Abs(opts.ValuesFile)
		if err != nil {
			return nil, errors.New("invalid values path")
		}
		rel, err := filepath.Rel(root, p)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, errors.New("values must be inside chart root")
		}
		info, err := os.Lstat(p)
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxFile {
			return nil, errors.New("unsafe values file")
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, errors.New("values unreadable")
		}
		var node yaml.Node
		if yaml.Unmarshal(b, &node) != nil || validateNode(&node, 0) != nil || node.Decode(&values) != nil {
			return nil, errors.New("invalid values mapping")
		}
	}
	kv, err := common.ParseKubeVersion(opts.TargetKubernetes)
	if err != nil {
		return nil, errors.New("invalid Kubernetes target")
	}
	// Never use the library's build-version-derived API capabilities.
	caps, err := targetCapabilities(opts.TargetKubernetes, *kv)
	if err != nil {
		return nil, err
	}
	a, _ := chart.NewAccessor(ch)
	if constraint, ok := a.MetadataAsMap()["kubeVersion"].(string); ok && constraint != "" {
		c, e := semver.NewConstraint(constraint)
		v, ve := semver.NewVersion(opts.TargetKubernetes)
		if e != nil || ve != nil || !c.Check(v) {
			return nil, errors.New("chart kubeVersion excludes target")
		}
	}
	renderedValues, err := chartutil.ToRenderValuesWithSchemaValidation(ch, values, common.ReleaseOptions{Name: "kubecompat", Namespace: opts.Namespace, Revision: 1, IsInstall: true}, caps, true)
	if err != nil {
		return nil, errors.New("chart values invalid")
	}
	rendered, err := engine.Render(ch, renderedValues)
	if err != nil {
		return nil, errors.New("chart render failed")
	}
	docs := ordered(rendered)
	var out []inputDocument
	var total int
	docs = append(docs, chartCRDs(ch)...)
	for _, doc := range docs {
		if strings.HasSuffix(doc.name, "NOTES.txt") {
			continue
		}
		total += len(doc.body)
		if total > maxTotal {
			return nil, errors.New("render output limit exceeded")
		}
		objects, diags := decode(doc)
		if len(diags) > 0 {
			return nil, errors.New("invalid rendered chart document")
		}
		for _, obj := range objects {
			meta, _ := obj["metadata"].(map[string]any)
			annotations, _ := meta["annotations"].(map[string]any)
			if annotations["helm.sh/hook"] != nil {
				continue
			}
			b, _ := yaml.Marshal(obj)
			out = append(out, inputDocument{doc.name, b})
		}
	}
	return out, nil
}
func inspectChart(ch chart.Charter) error {
	a, err := chart.NewAccessor(ch)
	if err != nil {
		return errors.New("unsupported chart")
	}
	for _, file := range a.Files() {
		if strings.HasSuffix(file.Name, ".tgz") {
			return errors.New("archive dependencies forbidden; use bounded unpacked dependencies")
		}
	}
	if len(a.MetaDependencies()) > len(a.Dependencies()) {
		return errors.New("chart dependencies must be vendored locally")
	}
	for _, file := range a.Templates() { // Parse with placeholder functions, then reject lookup calls at the AST level (including aliased whitespace).
		funcs := template.FuncMap{}
		for _, word := range strings.FieldsFunc(string(file.Data), func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_')
		}) {
			if word != "" {
				funcs[word] = func(...any) any { return nil }
			}
		}
		trees, err := parse.Parse(file.Name, string(file.Data), "{{", "}}", funcs)
		if err != nil {
			return errors.New("invalid chart template")
		}
		for _, tree := range trees {
			if forbiddenLookup(tree.Root) {
				return errors.New("chart lookup forbidden")
			}
		}
	}
	for _, dep := range a.Dependencies() {
		if err := inspectChart(dep); err != nil {
			return err
		}
	}
	return nil
}
func forbiddenLookup(n parse.Node) bool {
	if n == nil {
		return false
	}
	switch x := n.(type) {
	case *parse.ListNode:
		if x == nil {
			return false
		}
		for _, c := range x.Nodes {
			if forbiddenLookup(c) {
				return true
			}
		}
	case *parse.ActionNode:
		return forbiddenLookup(x.Pipe)
	case *parse.PipeNode:
		for _, c := range x.Cmds {
			if forbiddenLookup(c) {
				return true
			}
		}
	case *parse.CommandNode:
		for _, a := range x.Args {
			if forbiddenLookup(a) {
				return true
			}
		}
	case *parse.IdentifierNode:
		return x.Ident == "lookup" || x.Ident == "tpl"
	case *parse.IfNode:
		return forbiddenLookup(x.Pipe) || forbiddenLookup(x.List) || forbiddenLookup(x.ElseList)
	case *parse.RangeNode:
		return forbiddenLookup(x.Pipe) || forbiddenLookup(x.List) || forbiddenLookup(x.ElseList)
	case *parse.WithNode:
		return forbiddenLookup(x.Pipe) || forbiddenLookup(x.List) || forbiddenLookup(x.ElseList)
	}
	return false
}
func renderKustomize(ctx context.Context, root string, files []string, opts model.ScanOptions, dependencies *[]string) ([]inputDocument, error) {
	// Copy only bounded, nonsymlink files into an isolated in-memory filesystem.
	fs := filesys.MakeFsInMemory()
	fs.MkdirAll("/input")
	for _, p := range files {
		rel, err := filepath.Rel(root, p)
		if err != nil || strings.HasPrefix(rel, "..") {
			return nil, errors.New("unsafe render path")
		}
		b, err := safeRead(root, p)
		if err != nil {
			return nil, errors.New("render input unreadable")
		}
		base := filepath.Base(p)
		if base == "kustomization.yaml" || base == "kustomization.yml" || base == "Kustomization" {
			cfg, e := inputMap(b)
			if e != nil {
				return nil, e
			}
			if e = validateKustomizeConfig(cfg); e != nil {
				return nil, e
			}
			if e = resolveKustomizeSources(ctx, cfg, fs, opts, 0, dependencies); e != nil {
				return nil, e
			}
			b, _ = yaml.Marshal(cfg)
		}
		dest := filepath.Join("/input", rel)
		fs.MkdirAll(filepath.Dir(dest))
		if fs.WriteFile(dest, b) != nil {
			return nil, errors.New("render copy failed")
		}
	}
	options := krusty.MakeDefaultOptions()
	k := krusty.MakeKustomizer(options)
	result, err := k.Run(fs, "/input")
	if err != nil {
		return nil, errors.New("Kustomize render failed")
	}
	b, err := result.AsYaml()
	if err != nil || len(b) > maxTotal {
		return nil, errors.New("Kustomize output invalid or too large")
	}
	return []inputDocument{{"kustomize.yaml", b}}, nil
}
func remoteValue(v any) bool {
	switch x := v.(type) {
	case string:
		return strings.Contains(x, "://") || strings.HasPrefix(x, "git::") || strings.Contains(x, "github.com/") || strings.HasPrefix(x, "../") || filepath.IsAbs(x)
	case map[string]any:
		for _, v := range x {
			if remoteValue(v) {
				return true
			}
		}
	case []any:
		for _, v := range x {
			if remoteValue(v) {
				return true
			}
		}
	}
	return false
}
func chartCRDs(ch chart.Charter) []inputDocument {
	a, err := chart.NewAccessor(ch)
	if err != nil {
		return nil
	}
	var out []inputDocument
	for _, f := range a.Files() {
		if strings.HasPrefix(f.Name, "crds/") {
			out = append(out, inputDocument{f.Name, f.Data})
		}
	}
	for _, dep := range a.Dependencies() {
		out = append(out, chartCRDs(dep)...)
	}
	return out
}
func targetCapabilities(target string, kv common.KubeVersion) (*common.Capabilities, error) {
	if target != "1.35" && target != "1.35.0" && target != "v1.35" && target != "v1.35.0" {
		return nil, errors.New("renderer API catalog covers Kubernetes 1.35.0 only")
	}
	schemas, err := data.Schemas()
	if err != nil {
		return nil, errors.New("target capabilities unavailable")
	}
	set := map[string]bool{"v1": true, "apiextensions.k8s.io/v1": true, "apiextensions.k8s.io/v1/CustomResourceDefinition": true}
	for _, b := range schemas {
		var s struct {
			GVK []struct{ Group, Version, Kind string } `json:"x-kubernetes-group-version-kind"`
		}
		if json.Unmarshal(b, &s) != nil {
			continue
		}
		for _, g := range s.GVK {
			api := g.Version
			if g.Group != "" {
				api = g.Group + "/" + api
			}
			set[api] = true
			set[api+"/"+g.Kind] = true
		}
	}
	var versions common.VersionSet
	for v := range set {
		versions = append(versions, v)
	}
	return &common.Capabilities{KubeVersion: kv, APIVersions: versions}, nil
}
