// Package manifests loads untrusted input without invoking external programs.
package manifests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wussh/kubecompact/internal/discovery"
	"github.com/wussh/kubecompact/internal/model"
	"go.yaml.in/yaml/v3"
)

const maxFile = 8 << 20
const maxTotal = 64 << 20
const maxFiles = 4096

var ErrInput = errors.New("invalid manifest input")

func Collect(ctx context.Context, opts model.ScanOptions) (model.Inventory, error) {
	inv, err := collect(ctx, opts)
	if err != nil && opts.GitURL == "" && ctx.Err() == nil {
		return inv, fmt.Errorf("%w: %s", ErrInput, err.Error())
	}
	return inv, err
}
func collect(ctx context.Context, opts model.ScanOptions) (model.Inventory, error) {
	root := opts.Manifests
	cleanup := func() {}
	if opts.GitURL != "" {
		var err error
		root, cleanup, err = checkout(ctx, opts)
		if err != nil {
			return model.Inventory{}, err
		}
	}
	defer cleanup()
	if root == "" {
		return model.Inventory{}, errors.New("manifest input required")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return model.Inventory{}, errors.New("invalid input path")
	}
	files, err := safeFiles(root)
	if err != nil {
		return model.Inventory{}, err
	}
	receipt, err := inputReceipt(root, files, opts)
	if err != nil {
		return model.Inventory{}, err
	}
	var docs []inputDocument
	switch opts.Render {
	case "", "raw":
		for _, p := range files {
			ext := strings.ToLower(filepath.Ext(p))
			if ext == ".yaml" || ext == ".yml" || ext == ".json" {
				b, e := safeRead(root, p)
				if e != nil {
					return model.Inventory{}, errors.New("input unreadable")
				}
				rel, _ := filepath.Rel(root, p)
				docs = append(docs, inputDocument{rel, b})
			}
		}
	case "helm":
		renderRoot, cleanupRender, e := prepareHelm(ctx, root, files, opts)
		if e != nil {
			return model.Inventory{}, e
		}
		defer cleanupRender()
		resolvedFiles, e := safeFiles(renderRoot)
		if e != nil {
			return model.Inventory{}, e
		}
		digest, e := digestFiles(renderRoot, resolvedFiles)
		if e != nil {
			return model.Inventory{}, e
		}
		receipt.DependencyDigests = append(receipt.DependencyDigests, digest)
		renderOpts := opts
		if opts.ValuesFile != "" {
			p, e := filepath.Abs(opts.ValuesFile)
			if e != nil {
				return model.Inventory{}, errors.New("invalid values path")
			}
			rel, e := filepath.Rel(root, p)
			if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return model.Inventory{}, errors.New("values must be inside chart root")
			}
			renderOpts.ValuesFile = filepath.Join(renderRoot, rel)
		}
		docs, err = renderHelm(renderRoot, renderOpts)
	case "kustomize":
		docs, err = renderKustomize(ctx, root, files, opts, &receipt.DependencyDigests)
	default:
		return model.Inventory{}, errors.New("unsupported renderer")
	}
	if err != nil {
		return model.Inventory{}, err
	}
	var objects []map[string]any
	var diagnostics []model.Diagnostic
	for _, doc := range docs {
		if err := ctx.Err(); err != nil {
			return model.Inventory{}, err
		}
		parsed, ds := decode(doc)
		for i, o := range parsed {
			o["_document"] = fmt.Sprintf("%s#%d", doc.name, i+1)
		}
		objects = append(objects, parsed...)
		diagnostics = append(diagnostics, ds...)
	}
	inv := discovery.Correlate(discovery.FromObjects(objects, "manifests", receipt.ID))
	inv.Diagnostics = append(inv.Diagnostics, diagnostics...)
	sort.Strings(receipt.DependencyDigests)
	annotateSources(&inv, root, opts, receipt)
	state := "COMPLETE"
	if len(diagnostics) > 0 {
		state = "PARTIAL"
	}
	inv.Coverage = append(inv.Coverage, model.Coverage{Collector: "manifests", Scope: receipt.ID, State: state})
	inv.Finish()
	return inv, nil
}

type inputDocument struct {
	name string
	body []byte
}

func safeFiles(root string) ([]string, error) {
	real, err := filepath.EvalSymlinks(root)
	if err != nil || real != root {
		return nil, errors.New("input symlink path forbidden")
	}
	var paths []string
	var total int64
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return errors.New("input unreadable")
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("symlink input forbidden")
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		info, e := d.Info()
		if e != nil || !info.Mode().IsRegular() {
			return errors.New("nonregular input forbidden")
		}
		total += info.Size()
		if info.Size() > maxFile || total > maxTotal || len(paths) >= maxFiles {
			return errors.New("input size limit exceeded")
		}
		paths = append(paths, p)
		return nil
	})
	return paths, err
}
func decode(doc inputDocument) ([]map[string]any, []model.Diagnostic) {
	dec := yaml.NewDecoder(bytes.NewReader(doc.body))
	var out []map[string]any
	var ds []model.Diagnostic
	for n := 1; n <= maxFiles; n++ {
		var node yaml.Node
		err := dec.Decode(&node)
		if err == io.EOF {
			break
		}
		scope := fmt.Sprintf("%s#%d", filepath.Base(doc.name), n)
		bad := func(code string) {
			ds = append(ds, model.Diagnostic{Code: code, Scope: scope, Message: "Document could not be safely loaded"})
		}
		if err != nil {
			bad("INVALID_YAML")
			break
		}
		if len(node.Content) == 0 {
			continue
		}
		if err = validateNode(&node, 0); err != nil {
			bad("INVALID_MAPPING")
			continue
		}
		var value any
		if err = node.Decode(&value); err != nil {
			bad("INVALID_DOCUMENT")
			continue
		}
		if value == nil {
			continue
		}
		b, err := json.Marshal(value)
		if err != nil {
			bad("INVALID_DOCUMENT")
			continue
		}
		var obj map[string]any
		if json.Unmarshal(b, &obj) != nil {
			bad("INVALID_DOCUMENT")
			continue
		}
		flat, err := flatten(obj, 0)
		if err != nil {
			bad("INVALID_RESOURCE")
			continue
		}
		out = append(out, flat...)
	}
	return out, ds
}
func validateNode(n *yaml.Node, depth int) error {
	if depth > 100 || n.Kind == yaml.AliasNode {
		return errors.New("aliases or depth forbidden")
	}
	if n.Kind == yaml.MappingNode {
		keys := map[string]bool{}
		for i := 0; i < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Kind != yaml.ScalarNode || k.Tag != "!!str" || keys[k.Value] || k.Value == "<<" {
				return errors.New("invalid mapping")
			}
			keys[k.Value] = true
		}
	}
	for _, c := range n.Content {
		if err := validateNode(c, depth+1); err != nil {
			return err
		}
	}
	return nil
}
func flatten(obj map[string]any, depth int) ([]map[string]any, error) {
	if depth > 32 {
		return nil, errors.New("list nesting")
	}
	if obj["kind"] == "List" {
		items, ok := obj["items"].([]any)
		if !ok {
			return nil, errors.New("invalid list")
		}
		var out []map[string]any
		for _, item := range items {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, errors.New("invalid item")
			}
			v, e := flatten(m, depth+1)
			if e != nil {
				return nil, e
			}
			out = append(out, v...)
		}
		return out, nil
	}
	if _, ok := obj["apiVersion"].(string); !ok {
		return nil, errors.New("missing apiVersion")
	}
	if _, ok := obj["kind"].(string); !ok {
		return nil, errors.New("missing kind")
	}
	return []map[string]any{obj}, nil
}
func ordered(m map[string]string) []inputDocument {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []inputDocument
	for _, k := range keys {
		out = append(out, inputDocument{k, []byte(m[k])})
	}
	return out
}
