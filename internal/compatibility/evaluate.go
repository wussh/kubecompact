package compatibility

import (
	semver "github.com/Masterminds/semver/v3"
	"github.com/wussh/kubecompact/internal/model"
	"sort"
	"strings"
	"time"
)

func matches(expression, version string) bool {
	c, e := semver.NewConstraint(expression)
	if e != nil {
		return false
	}
	v, e := semver.NewVersion(version)
	return e == nil && c.Check(v)
}
func applicationVersion(c model.Component) string {
	for _, role := range []string{"running", "deployed"} {
		found := ""
		conflict := false
		observe := func(raw string) {
			p, e := semver.StrictNewVersion(strings.TrimPrefix(raw, "v"))
			if e != nil || p.Prerelease() != "" {
				return
			}
			if found != "" && found != p.String() {
				conflict = true
			}
			found = p.String()
		}
		for _, v := range c.Versions {
			field := strings.ToLower(v.Field)
			if v.Role != role || (v.Scheme != "semver" && v.Scheme != "app") || strings.Contains(field, "appversion") || strings.HasPrefix(field, "helm.history") {
				continue
			}
			raw := v.Normalized
			if raw == "" {
				raw = v.Raw
			}
			observe(raw)
		}
		if role == "running" {
			for _, image := range c.Images {
				if image.Role != "running" || !projectImage(c.Project, image.Image) {
					continue
				}
				tag := image.Tag
				if tag == "" {
					raw := strings.Split(image.Image, "@")[0]
					if i := strings.LastIndex(raw, ":"); i > strings.LastIndex(raw, "/") {
						tag = raw[i+1:]
					}
				}
				observe(tag)
			}
		}
		if conflict {
			return ""
		}
		if found != "" {
			return found
		}
	}
	return ""
}

// InstalledVersion resolves only an exact application observation or reviewed upstream running image.
func InstalledVersion(c model.Component) string { return applicationVersion(c) }
func authoritative(r model.Rule, now time.Time) bool {
	return r.Evidence.Fresh(now) && r.Evidence.Priority <= 2 && strings.EqualFold(r.Evidence.Confidence, "HIGH") && r.Evidence.URL != "" && r.Evidence.SourceHash != ""
}
func Evaluate(inv model.Inventory, rules []model.Rule, opts model.CheckOptions) model.Assessment {
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	rules = enrichReleases(rules, now)
	a := model.Assessment{SchemaVersion: model.SchemaVersion, ScannerVersion: model.Version, ScanID: inv.ScanID, TargetKubernetes: opts.TargetKubernetes, AssessedAt: now, UpgradePathStatus: "NOT_EVALUATED", Coverage: inv.Coverage, Components: []model.ComponentAssessment{}, Checks: []model.CheckResult{}, Evidence: []model.Evidence{}}
	a.Diagnostics = inv.Diagnostics
	a.Sources = inv.Sources
	target, e := semver.NewVersion(opts.TargetKubernetes)
	if e != nil || target.Major() != 1 || target.Prerelease() != "" {
		a.Checks = append(a.Checks, model.CheckResult{Domain: "target", State: "FAIL", Severity: "BLOCKER", Reason: "Target must be a stable Kubernetes 1.x semantic version"})
		a.Gate()
		return a
	}
	a.ResolvedTarget = target.String()
	seen := map[string]bool{}
	for _, c := range inv.Components {
		ca := model.ComponentAssessment{Component: c, TargetKubernetes: opts.TargetKubernetes, Verdict: model.Unknown, Reason: "No fresh applicable authoritative runtime support evidence", EvidenceIDs: []string{}}
		version := applicationVersion(c)
		if c.Project == "" || version == "" || strings.EqualFold(c.IdentityConfidence, "LOW") {
			ca.Reason = "Application identity or unambiguous deployed/running semantic version is missing"
			a.Components = append(a.Components, ca)
			continue
		}
		support, exclude, conflict, gap := false, false, false, false
		priority := 99
		var supports []bool
		for _, r := range rules {
			if r.Project != c.Project || !matches(r.ComponentRange, version) {
				continue
			}
			ca.EvidenceIDs = append(ca.EvidenceIDs, r.Evidence.ID)
			if !seen[r.Evidence.ID] {
				a.Evidence = append(a.Evidence, r.Evidence)
				seen[r.Evidence.ID] = true
			}
			if !authoritative(r, now) {
				if r.Kind == "support" || r.Kind == "exclusion" || r.Kind == "knownissue" {
					gap = true
				}
				continue
			}
			applicable := true
			for k := range r.Environment {
				if k == "latestPatch" {
					applicable = applicable && r.Environment[k] == "true" && contains(r.ReleaseVersions, version)
					continue
				}
				applicable = false
			}
			if !applicable {
				gap = true
				continue
			}
			hit := matches(r.KubernetesRange, target.String())
			kind := strings.ToLower(r.Kind)
			check := model.CheckResult{ID: r.ID, Domain: kind, Scope: c.ID, State: "PASS", Severity: "INFO", Reason: r.Evidence.Excerpt, EvidenceIDs: []string{r.Evidence.ID}}
			switch kind {
			case "support":
				if !hit && r.Effect == "positive-only" {
					continue
				}
				if r.Evidence.Priority < priority {
					priority = r.Evidence.Priority
					supports = nil
				}
				if r.Evidence.Priority == priority {
					supports = append(supports, hit && r.Effect != "exclude")
				}
			case "exclusion", "knownissue", "known-issue":
				if hit {
					exclude = true
					check.State = "FAIL"
					check.Severity = "BLOCKER"
				}
			case "tested":
				b := hit
				ca.Tested = &b
			case "lifecycle":
				ca.Lifecycle = r.Effect
			case "chart":
				if !hit {
					check.State = "FAIL"
					check.Severity = "BLOCKER"
					a.Checks = append(a.Checks, check)
				}
			case "requirement":
				if !hit {
					exclude = true
				}
			}
			ca.Checks = append(ca.Checks, check)
			if len(r.Prerequisites) > 0 {
				a.Checks = append(a.Checks, model.CheckResult{ID: model.ID(r.ID, "prerequisites"), Domain: "prerequisites", Scope: c.ID, State: "UNKNOWN", Severity: "WARNING", Reason: strings.Join(r.Prerequisites, "; "), EvidenceIDs: []string{r.Evidence.ID}})
			}
		}
		for i, v := range supports {
			if i > 0 && v != supports[0] {
				conflict = true
			}
			support = support || v
			exclude = exclude || !v
		}
		if conflict {
			ca.Reason = "Comparable authoritative support claims conflict"
		} else if exclude {
			ca.Verdict = model.Incompatible
			ca.Reason = "Authoritative target exclusion or requirement"
			ca.Recommendation = recommend(c, target.String(), rules, now)
			if ca.Recommendation != nil {
				ca.Verdict = model.UpgradeRequired
			}
		} else if support && !gap {
			ca.Verdict = model.Compatible
			ca.Reason = "Fresh official support explicitly covers this application and target"
		} else if !gap && ca.Tested != nil && !*ca.Tested {
			ca.Verdict = model.NotTested
			ca.Reason = "Target is outside the documented tested set; this does not prove incompatibility"
		}
		if ca.Recommendation != nil {
			for _, id := range ca.Recommendation.EvidenceIDs {
				if !contains(ca.EvidenceIDs, id) {
					ca.EvidenceIDs = append(ca.EvidenceIDs, id)
				}
				if !seen[id] {
					for _, rule := range rules {
						if rule.Evidence.ID == id {
							a.Evidence = append(a.Evidence, rule.Evidence)
							seen[id] = true
							break
						}
					}
				}
			}
		}
		a.Components = append(a.Components, ca)
	}
	a.Gate()
	return a
}
func contains(vs []string, v string) bool {
	for _, x := range vs {
		if strings.TrimPrefix(x, "v") == strings.TrimPrefix(v, "v") {
			return true
		}
	}
	return false
}
func Recommend(c model.Component, target string, rules []model.Rule) *model.Recommendation {
	now := time.Now().UTC()
	return recommend(c, target, enrichReleases(rules, now), now)
}
func recommend(c model.Component, target string, rules []model.Rule, now time.Time) *model.Recommendation {
	old, err := semver.StrictNewVersion(applicationVersion(c))
	if err != nil {
		return nil
	}
	var best *semver.Version
	var result *model.Recommendation
	for _, release := range rules {
		if release.Project != c.Project || release.Kind != "release" || !authoritative(release, now) {
			continue
		}
		for _, raw := range release.ReleaseVersions {
			v, err := semver.StrictNewVersion(strings.TrimPrefix(raw, "v"))
			if err != nil || !matches(release.ComponentRange, v.String()) || v.Prerelease() != "" || !v.GreaterThan(old) || (best != nil && !v.LessThan(best)) {
				continue
			}
			proof := candidateSupport(c.Project, v.String(), target, rules, now)
			if len(proof) == 0 {
				continue
			}
			chart := ""
			if c.Chart != nil {
				var mappingProof string
				for _, mapping := range rules {
					if mapping.Project != c.Project || mapping.Kind != "chart-mapping" || !authoritative(mapping, now) || mapping.ChartName != c.Chart.Name || c.Chart.Repository == "" || strings.TrimSuffix(mapping.ChartRepository, "/") != strings.TrimSuffix(c.Chart.Repository, "/") || !matches(mapping.ComponentRange, v.String()) || !matches(mapping.KubernetesRange, target) {
						continue
					}
					version := mapping.ChartVersions[v.String()]
					next, err := semver.StrictNewVersion(strings.TrimPrefix(version, "v"))
					if err != nil || next.Prerelease() != "" {
						continue
					}
					if current, err := semver.StrictNewVersion(strings.TrimPrefix(c.Chart.Version, "v")); err == nil && !next.GreaterThan(current) {
						continue
					}
					if chart == "" {
						chart, mappingProof = version, mapping.Evidence.ID
					} else if current, _ := semver.NewVersion(chart); next.LessThan(current) {
						chart, mappingProof = version, mapping.Evidence.ID
					}
				}
				if chart == "" {
					continue
				}
				proof = append(proof, mappingProof)
			}
			proof = append(proof, release.Evidence.ID)
			sort.Strings(proof)
			proof = uniqueStrings(proof)
			best = v
			result = &model.Recommendation{Version: v.String(), Scheme: "semver", ChartVersion: chart, EvidenceIDs: proof}
		}
	}
	return result
}

func uniqueStrings(sorted []string) []string {
	out := sorted[:0]
	for _, value := range sorted {
		if len(out) == 0 || out[len(out)-1] != value {
			out = append(out, value)
		}
	}
	return out
}

func candidateSupport(project, version, target string, rules []model.Rule, now time.Time) []string {
	priority := 99
	var proof []string
	denied := false
	for _, r := range rules {
		if r.Project != project || !matches(r.ComponentRange, version) || !authoritative(r, now) {
			continue
		}
		hit := matches(r.KubernetesRange, target)
		switch r.Kind {
		case "support":
			if r.Effect == "positive-only" && !hit {
				continue
			}
			if len(r.Prerequisites) > 0 || len(r.Environment) > 0 && !(len(r.Environment) == 1 && r.Environment["latestPatch"] == "true" && contains(r.ReleaseVersions, version)) {
				return nil
			}
			if r.Evidence.Priority < priority {
				priority, proof, denied = r.Evidence.Priority, nil, false
			}
			if r.Evidence.Priority == priority {
				denied = denied || !hit || r.Effect == "exclude"
				proof = append(proof, r.Evidence.ID)
			}
		case "exclusion", "knownissue", "known-issue":
			if hit {
				return nil
			}
		case "requirement":
			if !hit || len(r.Prerequisites) > 0 || len(r.Environment) > 0 {
				return nil
			}
		case "lifecycle":
			if r.Effect == "EOL" {
				return nil
			}
		}
	}
	if denied {
		return nil
	}
	return proof
}
func enrichReleases(rules []model.Rule, now time.Time) []model.Rule {
	out := append([]model.Rule(nil), rules...)
	for i := range out {
		r := &out[i]
		if r.Kind != "support" {
			continue
		}
		r.ReleaseVersions = nil
		for _, release := range rules {
			if release.Project != r.Project || release.Kind != "release" || !authoritative(release, now) {
				continue
			}
			for _, raw := range release.ReleaseVersions {
				v, e := semver.StrictNewVersion(raw)
				if e != nil || v.Prerelease() != "" || !matches(release.ComponentRange, raw) || !matches(r.ComponentRange, raw) {
					continue
				}
				if !contains(r.ReleaseVersions, raw) {
					r.ReleaseVersions = append(r.ReleaseVersions, raw)
				}
			}
		}
		if r.Environment["latestPatch"] == "true" {
			// Bounded release history does not establish a branch's latest patch.
			r.ReleaseVersions = nil
		}
	}
	return out
}

var recognizedImages = map[string][]string{
	"cert-manager": {"quay.io/jetstack/cert-manager-controller", "quay.io/jetstack/cert-manager-webhook", "quay.io/jetstack/cert-manager-cainjector"},
	"argocd":       {"quay.io/argoproj/argocd"}, "metrics-server": {"registry.k8s.io/metrics-server/metrics-server"},
	"cilium": {"quay.io/cilium/cilium"}, "calico": {"docker.io/calico/node", "calico/node", "quay.io/calico/node"},
	"coredns":       {"registry.k8s.io/coredns/coredns", "coredns/coredns", "docker.io/coredns/coredns"},
	"ingress-nginx": {"registry.k8s.io/ingress-nginx/controller"}, "prometheus-operator": {"quay.io/prometheus-operator/prometheus-operator"},
	"velero": {"velero/velero", "docker.io/velero/velero"}, "keda": {"ghcr.io/kedacore/keda"}, "kyverno": {"ghcr.io/kyverno/kyverno"},
	"istio": {"docker.io/istio/pilot", "istio/pilot"}, "longhorn": {"longhornio/longhorn-manager", "docker.io/longhornio/longhorn-manager"},
	"rook-ceph": {"rook/ceph", "docker.io/rook/ceph"},
}

func projectImage(project, image string) bool {
	raw := strings.Split(image, "@")[0]
	if i := strings.LastIndex(raw, ":"); i > strings.LastIndex(raw, "/") {
		raw = raw[:i]
	}
	for _, repo := range recognizedImages[project] {
		if raw == repo {
			return true
		}
	}
	return false
}
