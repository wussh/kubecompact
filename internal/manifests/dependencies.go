package manifests

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/wussh/kubecompact/internal/compatibility/fetch"
	"github.com/wussh/kubecompact/internal/model"
	"go.yaml.in/yaml/v3"
	"sigs.k8s.io/kustomize/kyaml/filesys"
)

func inputMap(b []byte) (map[string]any, error) {
	var node yaml.Node
	var m map[string]any
	if yaml.Unmarshal(b, &node) != nil || validateNode(&node, 0) != nil || node.Decode(&m) != nil {
		return nil, errors.New("invalid renderer mapping")
	}
	return m, nil
}
func sourceFetcher(opts model.ScanOptions) (*fetch.Client, error) {
	return fetch.New(fetch.Config{Online: opts.Online, CAFile: opts.CAFile, AllowedHosts: opts.AllowedHosts, CredentialsFile: opts.CredentialsFile})
}
func prepareHelm(ctx context.Context, root string, files []string, opts model.ScanOptions) (string, func(), error) {
	noop := func() {}
	dir, err := os.MkdirTemp("", "kubecompat-chart-")
	if err != nil {
		return "", noop, errors.New("render directory unavailable")
	}
	cleanup := func() { os.RemoveAll(dir) }
	for _, p := range files {
		rel, e := filepath.Rel(root, p)
		if e != nil || rel == "." {
			cleanup()
			return "", noop, errors.New("Helm requires chart directory")
		}
		b, e := safeRead(root, p)
		if e != nil {
			cleanup()
			return "", noop, errors.New("chart file unreadable")
		}
		if strings.HasSuffix(p, ".tgz") {
			if e = extractChart(b, filepath.Join(dir, filepath.Dir(rel))); e != nil {
				cleanup()
				return "", noop, e
			}
			continue
		}
		dest := filepath.Join(dir, rel)
		if os.MkdirAll(filepath.Dir(dest), 0700) != nil || os.WriteFile(dest, b, 0600) != nil {
			cleanup()
			return "", noop, errors.New("chart copy failed")
		}
	}
	fetcher, err := sourceFetcher(opts)
	if err != nil {
		cleanup()
		return "", noop, err
	}
	if err = resolveChartDependencies(ctx, dir, dir, fetcher, opts, 0); err != nil {
		cleanup()
		return "", noop, err
	}
	if _, err = safeFiles(dir); err != nil {
		cleanup()
		return "", noop, err
	}
	return dir, cleanup, nil
}
func resolveChartDependencies(ctx context.Context, root, chartDir string, f *fetch.Client, opts model.ScanOptions, depth int) error {
	if depth > 16 {
		return errors.New("dependency nesting exceeded")
	}
	b, err := os.ReadFile(filepath.Join(chartDir, "Chart.yaml"))
	if err != nil {
		return errors.New("Chart.yaml unavailable")
	}
	meta, err := inputMap(b)
	if err != nil {
		return err
	}
	deps, _ := meta["dependencies"].([]any)
	locked := map[string]string{}
	if lock, e := os.ReadFile(filepath.Join(chartDir, "Chart.lock")); e == nil {
		m, e := inputMap(lock)
		if e != nil {
			return e
		}
		for _, v := range toArray(m["dependencies"]) {
			d := toMap(v)
			locked[toText(d["name"])] = toText(d["version"])
		}
	}
	for _, v := range deps {
		d := toMap(v)
		name, repo, version := toText(d["name"]), toText(d["repository"]), toText(d["version"])
		if name == "" || filepath.Base(name) != name || name == "." || name == ".." {
			return errors.New("invalid chart dependency name")
		}
		if pinned := locked[name]; pinned != "" {
			version = pinned
		}
		target := filepath.Join(chartDir, "charts", name)
		if _, e := os.Stat(filepath.Join(target, "Chart.yaml")); e == nil {
			if err = resolveChartDependencies(ctx, root, target, f, opts, depth+1); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(repo, "file://") {
			p := filepath.Clean(filepath.Join(chartDir, strings.TrimPrefix(repo, "file://")))
			rel, e := filepath.Rel(root, p)
			if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return errors.New("local chart dependency outside root")
			}
			paths, e := safeFiles(p)
			if e != nil {
				return e
			}
			for _, path := range paths {
				rel, _ := filepath.Rel(p, path)
				dest := filepath.Join(target, rel)
				b, e := safeRead(root, path)
				if e != nil {
					return errors.New("dependency unreadable")
				}
				if os.MkdirAll(filepath.Dir(dest), 0700) != nil || os.WriteFile(dest, b, 0600) != nil {
					return errors.New("dependency copy failed")
				}
			}
			if err = resolveChartDependencies(ctx, root, target, f, opts, depth+1); err != nil {
				return err
			}
			continue
		}
		if !opts.Online {
			return errors.New("remote chart dependency prohibited offline")
		}
		if strings.HasPrefix(repo, "oci://") {
			body, e := fetchOCIChart(ctx, repo, name, version, f)
			if e != nil {
				return e
			}
			if e = extractChart(body, filepath.Join(chartDir, "charts")); e != nil {
				return e
			}
			if e = resolveChartDependencies(ctx, root, target, f, opts, depth+1); e != nil {
				return e
			}
			continue
		}
		u, e := url.Parse(repo)
		if e != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("dependency repository requires explicit HTTPS origin")
		}
		indexURL := strings.TrimRight(repo, "/") + "/index.yaml"
		snap, e := f.Fetch(ctx, fetch.SourceRequest{URL: indexURL, Project: "helm-dependency", ParserID: "helm-index", ParserVersion: "1"})
		if e != nil {
			return errors.New("chart index unavailable")
		}
		index, e := inputMap(snap.Body)
		if e != nil {
			return e
		}
		constraint, e := semver.NewConstraint(version)
		if e != nil {
			return errors.New("chart dependency version invalid")
		}
		var selected map[string]any
		var best *semver.Version
		for _, entry := range toArray(toMap(index["entries"])[name]) {
			m := toMap(entry)
			sv, e := semver.NewVersion(toText(m["version"]))
			if e == nil && sv.Prerelease() == "" && constraint.Check(sv) && (best == nil || sv.GreaterThan(best)) {
				selected = m
				best = sv
			}
		}
		if selected == nil {
			return errors.New("chart dependency version unavailable")
		}
		urls := toArray(selected["urls"])
		if len(urls) == 0 {
			return errors.New("chart dependency archive unavailable")
		}
		archiveURL, e := url.Parse(toText(urls[0]))
		if e != nil {
			return errors.New("invalid chart archive URL")
		}
		base, _ := url.Parse(indexURL)
		archiveURL = base.ResolveReference(archiveURL)
		snap, e = f.Fetch(ctx, fetch.SourceRequest{URL: archiveURL.String(), Project: "helm-dependency", ParserID: "helm-archive", ParserVersion: "1"})
		if e != nil {
			return errors.New("chart dependency fetch failed")
		}
		digest := strings.TrimPrefix(toText(selected["digest"]), "sha256:")
		sum := sha256.Sum256(snap.Body)
		if digest == "" || !strings.EqualFold(hex.EncodeToString(sum[:]), digest) {
			return errors.New("chart archive digest mismatch")
		}
		if e = extractChart(snap.Body, filepath.Join(chartDir, "charts")); e != nil {
			return e
		}
		if err = resolveChartDependencies(ctx, root, target, f, opts, depth+1); err != nil {
			return err
		}
	}
	return nil
}
func extractChart(body []byte, dest string) error {
	r, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return errors.New("invalid chart archive")
	}
	defer r.Close()
	tr := tar.NewReader(io.LimitReader(r, maxTotal+1))
	count := 0
	var total int64
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return errors.New("invalid chart tar")
		}
		count++
		total += h.Size
		if count > maxFiles || h.Size < 0 || h.Size > maxFile || total > maxTotal {
			return errors.New("chart archive limit exceeded")
		}
		name := filepath.Clean(filepath.FromSlash(h.Name))
		if name == "." || name == ".." || filepath.IsAbs(name) || strings.HasPrefix(name, ".."+string(filepath.Separator)) || strings.Contains(h.Name, "\\") {
			return errors.New("chart archive path forbidden")
		}
		p := filepath.Join(dest, name)
		if h.Typeflag == tar.TypeDir {
			if os.MkdirAll(p, 0700) != nil {
				return errors.New("chart extract failed")
			}
			continue
		}
		if h.Typeflag != tar.TypeReg {
			return errors.New("chart archive link or special file forbidden")
		}
		if os.MkdirAll(filepath.Dir(p), 0700) != nil {
			return errors.New("chart extract failed")
		}
		f, e := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return errors.New("duplicate chart archive path")
		}
		_, e = io.CopyN(f, tr, h.Size)
		closeErr := f.Close()
		if e != nil || closeErr != nil {
			return errors.New("chart extract failed")
		}
	}
	return nil
}
func toMap(v any) map[string]any { m, _ := v.(map[string]any); return m }
func toArray(v any) []any        { a, _ := v.([]any); return a }
func toText(v any) string        { s, _ := v.(string); return s }
func resolveKustomizeSources(ctx context.Context, cfg map[string]any, fs filesys.FileSystem, opts model.ScanOptions, depth int, dependencies *[]string) error {
	if depth > 16 {
		return errors.New("remote base nesting exceeded")
	}
	for _, key := range []string{"resources", "bases", "components"} {
		values := toArray(cfg[key])
		for i, v := range values {
			s := toText(v)
			if !remoteValue(s) {
				continue
			}
			if !opts.Online {
				return errors.New("remote Kustomize dependency prohibited offline")
			}
			repo, subpath, revision, e := parseKustomizeGit(s)
			if e != nil {
				return e
			}
			remoteOpts := opts
			remoteOpts.GitURL = repo
			remoteOpts.GitRevision = revision
			remoteOpts.GitPath = subpath
			root, cleanup, e := checkout(ctx, remoteOpts)
			if e != nil {
				return e
			}
			paths, e := safeFiles(root)
			if e != nil {
				cleanup()
				return e
			}
			digest, e := digestFiles(root, paths)
			if e != nil {
				cleanup()
				return e
			}
			*dependencies = append(*dependencies, digest)
			id := model.ID(s)
			dest := filepath.Join("/input", ".remote", id)
			for _, p := range paths {
				rel, _ := filepath.Rel(root, p)
				b, e := os.ReadFile(p)
				if e != nil {
					cleanup()
					return errors.New("remote base unreadable")
				}
				base := filepath.Base(p)
				if base == "kustomization.yaml" || base == "kustomization.yml" || base == "Kustomization" {
					nested, e := inputMap(b)
					if e != nil {
						cleanup()
						return e
					}
					if e = validateKustomizeConfig(nested); e != nil {
						cleanup()
						return e
					}
					if e = resolveKustomizeSources(ctx, nested, fs, opts, depth+1, dependencies); e != nil {
						cleanup()
						return e
					}
					b, _ = yaml.Marshal(nested)
				}
				target := filepath.Join(dest, rel)
				fs.MkdirAll(filepath.Dir(target))
				if fs.WriteFile(target, b) != nil {
					cleanup()
					return errors.New("remote base staging failed")
				}
			}
			cleanup()
			values[i] = dest
		}
		if len(values) > 0 {
			cfg[key] = values
		}
	}
	return nil
}
func parseKustomizeGit(s string) (string, string, string, error) {
	s = strings.TrimPrefix(s, "git::")
	u, e := url.Parse(s)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return "", "", "", errors.New("remote base requires pinned HTTPS Git URL")
	}
	query := u.Query()
	revision := query.Get("ref")
	if !commitID.MatchString(revision) || len(query) != 1 {
		return "", "", "", errors.New("remote base requires full SHA ref")
	}
	parts := strings.SplitN(strings.TrimPrefix(u.Path, "/"), "//", 2)
	if len(parts) != 2 {
		return "", "", "", errors.New("remote base requires repo//path?ref=SHA")
	}
	u.Path = "/" + strings.TrimSuffix(parts[0], ".git") + ".git"
	u.RawQuery = ""
	return u.String(), parts[1], revision, nil
}
func validateKustomizeConfig(cfg map[string]any) error {
	for _, k := range []string{"generators", "transformers", "validators", "helmCharts", "helmGlobals"} {
		if cfg[k] != nil {
			return errors.New("external renderer/plugin forbidden")
		}
	}
	for k, v := range cfg {
		if k == "resources" || k == "bases" || k == "components" {
			continue
		}
		if remoteValue(v) {
			return errors.New("remote files outside explicit pinned bases forbidden")
		}
	}
	return nil
}
