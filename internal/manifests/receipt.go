package manifests

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"

	"github.com/wussh/kubecompact/internal/model"
)

// sourceIdentity never includes a temporary checkout location for Git inputs.
func sourceIdentity(root string, opts model.ScanOptions) string {
	if opts.GitURL != "" {
		return model.ID("git", opts.GitURL, strings.ToLower(opts.GitRevision), path.Clean("/"+opts.GitPath))
	}
	return model.ID("local", filepath.Clean(root))
}

func inputReceipt(root string, files []string, opts model.ScanOptions) (model.Source, error) {
	digest, err := digestFiles(root, files)
	if err != nil {
		return model.Source{}, err
	}
	renderer, module := opts.Render, "go.yaml.in/yaml/v3"
	switch renderer {
	case "", "raw":
		renderer = "raw"
	case "helm":
		module = "helm.sh/helm/v4"
	case "kustomize":
		module = "sigs.k8s.io/kustomize/api"
	}
	values := ""
	if opts.ValuesFile != "" {
		absolute, e := filepath.Abs(opts.ValuesFile)
		if e != nil {
			return model.Source{}, errors.New("invalid values path")
		}
		values, e = filepath.Rel(root, absolute)
		if e != nil || values == ".." || strings.HasPrefix(values, ".."+string(filepath.Separator)) {
			return model.Source{}, errors.New("values must be inside chart root")
		}
	}
	// Selection and target affect rendering even when file bytes are unchanged.
	selection, _ := json.Marshal([]string{digest, renderer, opts.TargetKubernetes, opts.Namespace, filepath.ToSlash(values)})
	return model.Source{ID: sourceIdentity(root, opts), Renderer: renderer, RendererVersion: compiledVersion(module), TargetKubernetes: opts.TargetKubernetes, InputDigest: digestBytes(selection)}, nil
}

func compiledVersion(module string) string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range info.Deps {
			if dep.Path == module {
				if dep.Replace != nil {
					// Local replacement paths are private, not reproducible versions.
					if dep.Replace.Version == "" {
						return "unavailable-local-replacement"
					}
					return dep.Replace.Version
				}
				return dep.Version
			}
		}
	}
	return "unavailable-build-info"
}

func digestBytes(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Hash relative names and bytes with unambiguous framing; never serialize either.
func digestFiles(root string, files []string) (string, error) {
	paths := append([]string(nil), files...)
	sort.Strings(paths)
	h := sha256.New()
	enc := json.NewEncoder(h)
	for _, file := range paths {
		rel, err := filepath.Rel(root, file)
		if err != nil {
			return "", errors.New("invalid receipt input")
		}
		body, err := safeRead(root, file)
		if err != nil {
			return "", err
		}
		if err = enc.Encode([]string{filepath.ToSlash(rel), digestBytes(body)}); err != nil {
			return "", errors.New("input receipt failed")
		}
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}
