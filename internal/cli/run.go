package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/wussh/kubecompact/internal/compatibility"
	"github.com/wussh/kubecompact/internal/compatibility/adapters"
	"github.com/wussh/kubecompact/internal/compatibility/cache"
	"github.com/wussh/kubecompact/internal/compatibility/fetch"
	"github.com/wussh/kubecompact/internal/discovery"
	"github.com/wussh/kubecompact/internal/manifests"
	"github.com/wussh/kubecompact/internal/model"
	"github.com/wussh/kubecompact/internal/readiness"
	"github.com/wussh/kubecompact/internal/report"
)

type flags struct {
	scan                                              model.ScanOptions
	check                                             model.CheckOptions
	filters                                           model.Filters
	output, format, inventory, input, mappings, hosts string
	offline, online                                   bool
}

func defaults() flags {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = "."
	}
	return flags{format: "table", check: model.CheckOptions{CachePath: filepath.Join(dir, "kubecompat", "evidence.db")}, scan: model.ScanOptions{Timeout: 5 * time.Minute}}
}
func bind(fs *flag.FlagSet, f *flags) {
	fs.StringVar(&f.scan.Kubeconfig, "kubeconfig", "", "explicit trusted kubeconfig")
	fs.StringVar(&f.scan.Context, "context", "", "explicit Kubernetes context")
	fs.StringVar(&f.scan.Namespace, "namespace", "", "namespace scope (default namespace for live scan)")
	fs.BoolVar(&f.scan.AllNamespaces, "all-namespaces", false, "read all authorized namespaces")
	fs.BoolVar(&f.scan.HelmStorage, "helm-storage", false, "explicitly authorize Helm Secret/ConfigMap release reads in selected namespace")
	fs.StringVar(&f.scan.HelmStorageNamespace, "helm-storage-namespace", "", "single authorized Helm storage namespace (defaults to --namespace)")
	fs.StringVar(&f.scan.Manifests, "manifests", "", "local manifest directory or file (no cluster authentication)")
	fs.StringVar(&f.scan.Render, "render", "", "local render mode: helm or kustomize")
	fs.StringVar(&f.scan.ValuesFile, "values", "", "explicit Helm values file")
	fs.StringVar(&f.scan.GitURL, "git-url", "", "explicit HTTPS Git source")
	fs.StringVar(&f.scan.GitRevision, "git-revision", "", "full immutable Git commit SHA")
	fs.StringVar(&f.scan.GitPath, "git-path", "", "manifest path in Git checkout")
	fs.StringVar(&f.scan.CredentialsFile, "credentials-file", "", "private per-origin source credential configuration")
	fs.DurationVar(&f.scan.Timeout, "timeout", 5*time.Minute, "whole-operation deadline")
	fs.StringVar(&f.check.TargetKubernetes, "target-k8s", "", "target Kubernetes minor or exact patch")
	fs.StringVar(&f.check.CachePath, "cache", f.check.CachePath, "SQLite evidence cache path")
	fs.StringVar(&f.check.CAFile, "ca-file", "", "additional trusted CA file")
	fs.StringVar(&f.check.SchemaPath, "schemas", "", "trusted target schema bundle directory")
	fs.StringVar(&f.check.DeprecatedUsageFile, "deprecated-usage", "", "existing aggregated API request usage JSON (never scrapes logs)")
	fs.BoolVar(&f.online, "online", f.online, "permit configured source fetches")
	fs.BoolVar(&f.offline, "offline", false, "explicitly prohibit source network access (default)")
	fs.StringVar(&f.hosts, "allow-hosts", "", "comma-separated explicit private/public source origins")
	fs.StringVar(&f.mappings, "identity-mappings", "", "reviewed JSON image/project mappings")
	fs.StringVar(&f.output, "output", "", "output file (default stdout)")
	fs.StringVar(&f.format, "format", f.format, "table, json, csv or html")
	fs.StringVar(&f.inventory, "inventory", "", "saved inventory JSON instead of live collection")
	fs.StringVar(&f.input, "input", "", "saved assessment JSON for pure report rendering")
	fs.StringVar(&f.filters.Controller, "controller", "", "management-chain filter")
	fs.StringVar(&f.filters.Component, "component", "", "component/project name filter")
	fs.StringVar(&f.filters.Status, "status", "", "component verdict filter")
	fs.StringVar(&f.filters.EvidenceQuality, "evidence-quality", "", "HIGH, MEDIUM, LOW or NONE")
}
func help(w io.Writer) {
	fmt.Fprintln(w, `KubeCompat — read-only Kubernetes upgrade compatibility scanner

Commands:
  scan      Collect live cluster or local/Git manifest inventory
  check     Evaluate inventory against --target-k8s using cached or online evidence
  report    Render --input assessment.json, or collect and evaluate
  db update Refresh official compatibility evidence (no cluster access)
  version   Print scanner and schema versions

Use COMMAND --help for input, source, security, report and filter options.
No upgrade, apply, sync, reconcile or mutation commands exist.`)
}
func validTarget(s string) bool {
	if s == "" {
		return false
	}
	if strings.ContainsAny(s, " <>|,~^*") {
		return false
	}
	v, err := semver.NewVersion(strings.TrimPrefix(s, "v"))
	return err == nil && v.Major() == 1 && v.Minor() >= 16 && v.Prerelease() == "" && strings.Count(strings.TrimPrefix(s, "v"), ".") >= 1
}
func readJSON(path string, dest any) error {
	file, err := os.Open(path)
	if err != nil {
		return errors.New("input file unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 64<<20 {
		return errors.New("input must be a regular file within 64 MiB")
	}
	d := json.NewDecoder(io.LimitReader(file, 64<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(dest); err != nil {
		return errors.New("invalid input JSON or schema")
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("multiple JSON documents are not supported")
	}
	return nil
}
func output(w io.Writer, path string, write func(io.Writer) error) error {
	if path == "" || path == "-" {
		return write(w)
	}
	dir := filepath.Dir(path)
	info, err := os.Lstat(path)
	if err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return errors.New("output is not a regular file")
	}
	if err != nil && !os.IsNotExist(err) {
		return errors.New("output path unavailable")
	}
	tmp, err := os.CreateTemp(dir, ".kubecompat-report-*")
	if err != nil {
		return errors.New("cannot create output")
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if err := write(tmp); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
func collect(ctx context.Context, f flags) (model.Inventory, error) {
	if f.inventory != "" {
		var inv model.Inventory
		err := readJSON(f.inventory, &inv)
		if err == nil && inv.SchemaVersion != model.SchemaVersion {
			err = errors.New("unsupported inventory schema")
		}
		return inv, err
	}
	if f.scan.Manifests != "" || f.scan.GitURL != "" {
		return manifests.Collect(ctx, f.scan)
	}
	return discovery.Collect(ctx, f.scan)
}
func sourceConfig(f flags) fetch.Config {
	return fetch.Config{Online: f.online, CAFile: f.check.CAFile, AllowedHosts: f.scan.AllowedHosts, CredentialsFile: f.scan.CredentialsFile}
}

// refresh preserves good cache entries when a source or parser fails.
func refresh(ctx context.Context, f flags, store *cache.Store, components []model.Component) ([]model.Coverage, error) {
	client, err := fetch.New(sourceConfig(f))
	if err != nil {
		return nil, err
	}
	coverage := []model.Coverage{}
	for _, adapter := range adapters.All() {
		var requests []fetch.SourceRequest
		if components == nil {
			requests = adapter.Sources(model.Component{Project: adapter.ID()})
		} else {
			seen := map[string]bool{}
			for _, component := range components {
				if component.Project != adapter.ID() {
					continue
				}
				for _, request := range adapter.Sources(component) {
					if !seen[request.URL] {
						seen[request.URL] = true
						requests = append(requests, request)
					}
				}
			}
			if len(requests) == 0 {
				continue
			}
		}
		const maxProjectSources = 24
		discovered := map[string]bool{}
		for _, request := range requests {
			discovered[request.URL] = true
		}
		if len(requests) > maxProjectSources {
			requests = requests[:maxProjectSources]
			coverage = append(coverage, model.Coverage{Collector: "source:" + adapter.ID(), Scope: adapter.ID(), State: "PARTIAL", Code: "SOURCE_DISCOVERY_LIMIT"})
		}
		successful := false
		for index := 0; index < len(requests); index++ {
			req := requests[index]
			if err := ctx.Err(); err != nil {
				return coverage, err
			}
			snapshot, err := client.Fetch(ctx, req)
			if err != nil {
				coverage = append(coverage, model.Coverage{Collector: "source:" + adapter.ID(), Scope: model.SafeURL(req.URL), State: "PARTIAL", Code: "SOURCE_FETCH_FAILED"})
				continue
			}
			for _, request := range adapters.Discover(adapter.ID(), snapshot) {
				if discovered[request.URL] {
					continue
				}
				discovered[request.URL] = true
				if len(requests) == maxProjectSources {
					coverage = append(coverage, model.Coverage{Collector: "source:" + adapter.ID(), Scope: model.SafeURL(request.URL), State: "PARTIAL", Code: "SOURCE_DISCOVERY_LIMIT"})
					continue
				}
				requests = append(requests, request)
			}
			rules, err := adapter.Parse(snapshot)
			if err != nil || len(rules) == 0 {
				code := "EVIDENCE_MISSING"
				if errors.Is(err, adapters.ErrMalformedEvidence) {
					code = "PARSER_FORMAT_CHANGED"
				}
				coverage = append(coverage, model.Coverage{Collector: "source:" + adapter.ID(), Scope: model.SafeURL(req.URL), State: "PARTIAL", Code: code})
				continue
			}
			grouped := map[string][]model.Rule{}
			for _, rule := range rules {
				grouped[rule.Evidence.ID] = append(grouped[rule.Evidence.ID], rule)
			}
			for _, group := range grouped {
				if err := store.Save(ctx, group[0].Evidence, group, snapshot); err != nil {
					return coverage, errors.New("evidence cache write failed")
				}
			}
			successful = true
		}
		if successful {
			coverage = append(coverage, model.Coverage{Collector: "source:" + adapter.ID(), Scope: adapter.ID(), State: "COMPLETE"})
		}
		if len(requests) == 0 {
			coverage = append(coverage, model.Coverage{Collector: "source:" + adapter.ID(), Scope: adapter.ID(), State: "PARTIAL", Code: "NO_SOURCE_REQUESTS"})
		}
	}
	return coverage, nil
}
func assess(ctx context.Context, f flags) (model.Assessment, error) {
	inv, err := collect(ctx, f)
	if err != nil {
		return model.Assessment{}, err
	}
	var rules []model.Rule
	var sourceCoverage []model.Coverage
	if !f.online {
		if _, err := os.Stat(f.check.CachePath); err != nil {
			sourceCoverage = append(sourceCoverage, model.Coverage{Collector: "evidence-cache", Scope: "local", State: "PARTIAL", Code: "CACHE_UNAVAILABLE"})
		} else {
			store, err := cache.Open(f.check.CachePath)
			if err != nil {
				return model.Assessment{}, errors.New("evidence cache unavailable")
			}
			defer store.Close()
			_, rules, err = store.Load(ctx, "")
			if err != nil {
				return model.Assessment{}, errors.New("evidence cache invalid")
			}
		}
	} else {
		store, err := cache.Open(f.check.CachePath)
		if err != nil {
			return model.Assessment{}, errors.New("evidence cache unavailable")
		}
		defer store.Close()
		sourceCoverage, err = refresh(ctx, f, store, inv.Components)
		if err != nil {
			return model.Assessment{}, err
		}
		_, rules, err = store.Load(ctx, "")
		if err != nil {
			return model.Assessment{}, errors.New("evidence cache read failed")
		}
	}
	a := compatibility.Evaluate(inv, rules, f.check)
	a.ResolvedTarget, _ = readiness.ResolveTarget(f.check.TargetKubernetes)
	checks, err := readiness.Check(ctx, inv, f.check)
	if err != nil {
		return model.Assessment{}, err
	}
	a.Checks = append(a.Checks, checks...)
	a.Coverage = append(a.Coverage, sourceCoverage...)
	a.Gate()
	return a, nil
}

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		help(stdout)
		return 0
	}
	if args[0] == "version" {
		fmt.Fprintf(stdout, "kubecompat %s; schema %s\n", model.Version, model.SchemaVersion)
		return 0
	}
	command := args[0]
	rest := args[1:]
	if command == "db" {
		if len(rest) == 0 || rest[0] != "update" {
			fmt.Fprintln(stderr, "usage: kubecompat db update [options]")
			return 1
		}
		rest = rest[1:]
	}
	if command != "scan" && command != "check" && command != "report" && command != "db" {
		fmt.Fprintln(stderr, "unknown command")
		return 1
	}
	f := defaults()
	if command == "scan" {
		f.format = "json"
	}
	if command == "db" {
		f.online = true
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(stderr)
	bind(fs, &f)
	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(stderr, "unexpected positional argument")
		return 1
	}
	fail := func(code int, message string) int { fmt.Fprintln(stderr, message); return code }
	if f.offline && f.online {
		return fail(1, "--online and --offline conflict")
	}
	if f.offline {
		f.online = false
	}
	if f.scan.AllNamespaces && f.scan.Namespace != "" {
		return fail(1, "--namespace and --all-namespaces conflict")
	}
	if f.scan.Timeout <= 0 {
		return fail(1, "timeout must be positive")
	}
	if _, err := discovery.StorageNamespace(f.scan); err != nil {
		return fail(1, "Helm storage requires opt-in, live input and one valid explicit storage namespace")
	}
	if f.scan.HelmStorage && (f.inventory != "" || f.input != "" || command == "db") {
		return fail(1, "Helm storage requires live collection")
	}
	if f.format != "json" && f.format != "table" && f.format != "csv" && f.format != "html" {
		return fail(1, "unsupported output format")
	}
	if f.scan.Manifests != "" && f.scan.GitURL != "" {
		return fail(1, "local manifests and Git source conflict")
	}
	if f.inventory != "" && (f.scan.Manifests != "" || f.scan.GitURL != "") {
		return fail(1, "saved inventory and collection inputs conflict")
	}
	if command == "scan" && (f.input != "" || f.inventory != "") {
		return fail(1, "scan does not accept saved assessment/inventory input")
	}
	if command == "check" && f.input != "" {
		return fail(1, "check uses --inventory, not --input")
	}
	if f.input != "" && (f.online || f.inventory != "" || f.scan.Manifests != "" || f.scan.GitURL != "") {
		return fail(1, "report input cannot trigger collection or online lookup")
	}
	if command == "check" || (command == "report" && f.input == "") {
		if !validTarget(f.check.TargetKubernetes) {
			return fail(1, "a valid --target-k8s release is required")
		}
	}
	if f.check.TargetKubernetes != "" && !validTarget(f.check.TargetKubernetes) {
		return fail(1, "invalid Kubernetes target")
	}
	if f.input == "" && f.check.TargetKubernetes != "" {
		if _, err := readiness.ResolveTarget(f.check.TargetKubernetes); err != nil {
			return fail(1, "target is outside the pinned API/schema catalog")
		}
	}
	f.filters.Namespace = f.scan.Namespace
	if f.hosts != "" {
		for _, host := range strings.Split(f.hosts, ",") {
			if host = strings.TrimSpace(host); host != "" {
				f.scan.AllowedHosts = append(f.scan.AllowedHosts, host)
			}
		}
	}
	f.scan.Online = f.online
	f.check.Online = f.online
	f.scan.CAFile = f.check.CAFile
	f.scan.TargetKubernetes = f.check.TargetKubernetes
	if f.mappings != "" {
		if err := readJSON(f.mappings, &f.scan.IdentityMappings); err != nil {
			return fail(1, "invalid identity mapping file")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, f.scan.Timeout)
	defer cancel()
	if command == "db" {
		if !f.online {
			return fail(1, "db update requires online access")
		}
		store, err := cache.Open(f.check.CachePath)
		if err != nil {
			return fail(4, "cannot open evidence cache")
		}
		defer store.Close()
		coverage, err := refresh(ctx, f, store, nil)
		if err != nil {
			return fail(4, "evidence refresh failed")
		}
		if f.check.SchemaPath != "" {
			target := f.check.TargetKubernetes
			if target == "" {
				target = "1.35"
			}
			client, err := fetch.New(sourceConfig(f))
			if err != nil {
				return fail(4, "cannot initialize schema fetch")
			}
			if err := readiness.AcquireSchemas(ctx, client, f.check.SchemaPath, target); err != nil {
				return fail(4, "cannot refresh pinned schemas")
			}
		}
		if err := output(stdout, f.output, func(w io.Writer) error { return json.NewEncoder(w).Encode(coverage) }); err != nil {
			return fail(4, "cannot write refresh report")
		}
		for _, c := range coverage {
			if c.State == "PARTIAL" {
				return 3
			}
		}
		return 0
	}
	if command == "scan" {
		inv, err := collect(ctx, f)
		if err != nil {
			if errors.Is(err, manifests.ErrInput) {
				return fail(1, "invalid manifest or render input")
			}
			return fail(4, "inventory collection failed; inspect input and read permissions")
		}
		err = output(stdout, f.output, func(w io.Writer) error {
			if f.format == "json" {
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				return enc.Encode(inv)
			}
			a := compatibility.Evaluate(inv, nil, f.check)
			return report.Write(w, a, f.format, f.filters)
		})
		if err != nil {
			return fail(4, "cannot write inventory report")
		}
		if len(inv.Diagnostics) > 0 {
			return 1
		}
		if inv.Incomplete() {
			return 3
		}
		return 0
	}
	var a model.Assessment
	if f.input != "" {
		if err := readJSON(f.input, &a); err != nil {
			return fail(1, "invalid saved assessment")
		}
		if a.SchemaVersion != model.SchemaVersion {
			return fail(1, "unsupported assessment schema")
		}
		if f.check.TargetKubernetes != "" && f.check.TargetKubernetes != a.TargetKubernetes {
			return fail(1, "report target differs from saved assessment")
		}
	} else {
		var err error
		a, err = assess(ctx, f)
		if err != nil {
			if errors.Is(err, manifests.ErrInput) || errors.Is(err, readiness.ErrTarget) {
				return fail(1, "invalid input or unsupported target")
			}
			return fail(4, "assessment failed; inspect input, target catalog, cache and permissions")
		}
	}
	if err := output(stdout, f.output, func(w io.Writer) error { return report.Write(w, a, f.format, f.filters) }); err != nil {
		return fail(4, "cannot write assessment report")
	}
	if command == "report" {
		return 0
	}
	return a.ExitCode
}
