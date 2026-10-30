package adapters

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	semver "github.com/Masterminds/semver/v3"
	"github.com/wussh/kubecompact/internal/compatibility"
	"github.com/wussh/kubecompact/internal/compatibility/fetch"
	"github.com/wussh/kubecompact/internal/model"
	"golang.org/x/net/html"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var ErrMissingEvidence = errors.New("official source has no applicable structured compatibility evidence")
var ErrMalformedEvidence = errors.New("official compatibility source format changed or contains malformed versions")

type Adapter interface {
	ID() string
	Version() string
	Sources(model.Component) []fetch.SourceRequest
	Parse(fetch.SourceSnapshot) ([]model.Rule, error)
}
type adapter struct{ id, source, repo, branch string }

func (a adapter) ID() string      { return a.id }
func (a adapter) Version() string { return "2" }
func All() []Adapter {
	return []Adapter{
		adapter{"cert-manager", "https://cert-manager.io/docs/releases/", "cert-manager/cert-manager", ""},
		adapter{"argocd", "https://argo-cd.readthedocs.io/en/stable/operator-manual/tested-kubernetes-versions/", "argoproj/argo-cd", ""},
		adapter{"metrics-server", "https://raw.githubusercontent.com/kubernetes-sigs/metrics-server/master/README.md", "kubernetes-sigs/metrics-server", ""},
		adapter{"cilium", "https://docs.cilium.io/en/v1.18/network/kubernetes/compatibility/", "cilium/cilium", "1.18"},
		adapter{"calico", "https://docs.tigera.io/calico/3.33/getting-started/kubernetes/requirements", "projectcalico/calico", "3.33"},
		adapter{"flux", "https://api.github.com/repos/fluxcd/flux2/releases/tags/v2.7.0", "fluxcd/flux2", "2.7.0"},
		adapter{"coredns", "https://raw.githubusercontent.com/coredns/coredns/v1.12.4/plugin/kubernetes/README.md", "coredns/coredns", "1.12.4"},
		adapter{"ingress-nginx", "https://raw.githubusercontent.com/kubernetes/ingress-nginx/main/README.md", "kubernetes/ingress-nginx", ""},
		adapter{"prometheus-operator", "https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/v0.86.1/README.md", "prometheus-operator/prometheus-operator", "0.86.1"},
		adapter{"velero", "https://raw.githubusercontent.com/velero-io/velero/main/README.md", "velero-io/velero", ""},
		adapter{"keda", "https://keda.sh/docs/2.18/operate/cluster/", "kedacore/keda", "2.18"},
		adapter{"kyverno", "https://kyverno.io/docs/installation/releases/", "kyverno/kyverno", ""},
		adapter{"istio", "https://istio.io/latest/docs/releases/supported-releases/", "istio/istio", ""},
		adapter{"longhorn", "https://longhorn.io/docs/archives/1.10.0/deploy/install/", "longhorn/longhorn", "1.10.0"},
		adapter{"rook-ceph", "https://rook.io/docs/rook/v1.18/Getting-Started/Prerequisites/prerequisites/", "rook/rook", "1.18"},
	}
}
func ByID(id string) Adapter {
	for _, a := range All() {
		if a.ID() == id {
			return a
		}
	}
	return nil
}
func (a adapter) Sources(c model.Component) []fetch.SourceRequest {
	source := a.source
	// Only known upstream URL templates are expanded; declarations never become arbitrary fetch targets.
	if s := compatibility.InstalledVersion(c); s != "" {
		p, _ := semver.StrictNewVersion(s)
		minor := fmt.Sprintf("%d.%d", p.Major(), p.Minor())
		switch a.id {
		case "cilium":
			source = "https://docs.cilium.io/en/v" + minor + "/network/kubernetes/compatibility/"
		case "calico":
			source = "https://docs.tigera.io/calico/" + minor + "/getting-started/kubernetes/requirements"
		case "argocd":
			source = "https://raw.githubusercontent.com/argoproj/argo-cd/v" + p.String() + "/docs/operator-manual/tested-kubernetes-versions.md"
		case "keda":
			source = "https://keda.sh/docs/" + minor + "/operate/cluster/"
		case "flux":
			source = "https://api.github.com/repos/fluxcd/flux2/releases/tags/v" + p.String()
		case "prometheus-operator":
			source = "https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/v" + p.String() + "/README.md"
		case "coredns":
			source = "https://raw.githubusercontent.com/coredns/coredns/v" + p.String() + "/plugin/kubernetes/README.md"
		case "rook-ceph":
			source = "https://rook.io/docs/rook/v" + minor + "/Getting-Started/Prerequisites/prerequisites/"
		case "longhorn":
			source = "https://longhorn.io/docs/" + p.String() + "/deploy/install/"
		}
	}
	return append([]fetch.SourceRequest{{URL: source, Project: a.id, ParserID: a.id, ParserVersion: a.Version()}, {URL: "https://api.github.com/repos/" + a.repo + "/releases", Project: a.id, ParserID: a.id, ParserVersion: a.Version()}}, a.metadataSources()...)
}

var versionRE = regexp.MustCompile(`v?\d+\.\d+(?:\.\d+)?(?:\.x)?`)
var linkRE = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)

func clean(s string) string {
	s = linkRE.ReplaceAllString(s, "$1")
	s = strings.ReplaceAll(s, "`", "")
	s = strings.ReplaceAll(s, "**", "")
	return strings.Join(strings.Fields(s), " ")
}
func text(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	s := ""
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		s += text(c)
	}
	switch n.Data {
	case "p", "li", "div", "h1", "h2", "h3":
		s += "\n"
	}
	return s
}
func rows(body string) [][]string {
	var result [][]string
	if strings.Contains(body, "<table") {
		doc, e := html.Parse(strings.NewReader(body))
		if e != nil {
			return nil
		}
		var visit func(*html.Node)
		visit = func(n *html.Node) {
			if n.Type == html.ElementNode && n.Data == "table" {
				result = append(result, nil)
			}
			if n.Type == html.ElementNode && n.Data == "tr" {
				var r []string
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Data == "td" || c.Data == "th" {
						r = append(r, clean(text(c)))
					}
				}
				if len(r) > 0 {
					result = append(result, r)
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				visit(c)
			}
		}
		visit(doc)
		return result
	}
	for _, line := range strings.Split(body, "\n") {
		if !strings.Contains(line, "|") {
			result = append(result, nil)
			continue
		}
		parts := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		var r []string
		separator := true
		for _, p := range parts {
			p = clean(p)
			r = append(r, p)
			if strings.Trim(p, "-: ") != "" {
				separator = false
			}
		}
		if !separator {
			result = append(result, r)
		}
	}
	return result
}
func normalizedRange(s string) (string, error) {
	s = clean(s)
	s = strings.TrimSpace(strings.Split(s, "/")[0])
	if strings.HasPrefix(s, "*1.") {
		s = strings.TrimPrefix(s, "*")
	}
	s = strings.ReplaceAll(s, "v", "")
	s = strings.ReplaceAll(s, ", and ", ", ")
	s = strings.ReplaceAll(s, " and ", ", ")
	s = strings.ReplaceAll(s, "→", "-")
	s = strings.ReplaceAll(s, "–", "-")
	if s == "" || strings.Contains(s, "TBD") {
		return "", ErrMissingEvidence
	}
	if strings.HasSuffix(s, "-latest") {
		s = ">=" + strings.TrimSuffix(s, "-latest")
	}
	if strings.HasSuffix(s, "+") {
		s = ">=" + strings.TrimSuffix(s, "+")
	}
	if strings.Contains(s, ",") {
		var out []string
		for _, v := range strings.Split(s, ",") {
			v = strings.TrimSpace(v)
			if !regexp.MustCompile(`^\d+\.\d+(\.\d+)?$`).MatchString(v) {
				return "", ErrMalformedEvidence
			}
			out = append(out, v)
		}
		s = strings.Join(out, " || ")
	} else if strings.Contains(s, "-") && !strings.Contains(s, ">=") {
		p := strings.Split(s, "-")
		if len(p) != 2 {
			return "", ErrMalformedEvidence
		}
		lo := strings.TrimSpace(p[0])
		hi := strings.TrimSpace(p[1])
		v, e := semver.NewVersion(hi)
		if e != nil {
			return "", ErrMalformedEvidence
		}
		if strings.Count(hi, ".") == 1 {
			s = ">=" + lo + " <" + fmt.Sprintf("%d.%d.0", v.Major(), v.Minor()+1)
		} else {
			s = ">=" + lo + " <=" + hi
		}
	}
	if _, e := semver.NewConstraint(s); e != nil {
		return "", ErrMalformedEvidence
	}
	return s, nil
}
func componentRange(s string) (string, error) {
	s = clean(s)
	s = strings.TrimPrefix(s, "v")
	if !regexp.MustCompile(`^\d+\.\d+(?:\.(?:\d+|x))?$`).MatchString(s) {
		return "", ErrMalformedEvidence
	}
	if strings.Count(s, ".") == 1 {
		s += ".x"
	}
	_, e := semver.NewConstraint(s)
	return s, e
}
func (a adapter) rule(s fetch.SourceSnapshot, component, kube, kind, effect, excerpt string) (model.Rule, error) {
	cr, e := componentRange(component)
	if e != nil {
		return model.Rule{}, e
	}
	kr, e := normalizedRange(kube)
	if e != nil {
		return model.Rule{}, e
	}
	sum := fmt.Sprintf("%x", sha256.Sum256(s.Body))
	if s.SHA256 != "" && s.SHA256 != sum {
		return model.Rule{}, ErrMalformedEvidence
	}
	at := s.RetrievedAt
	if at.IsZero() {
		return model.Rule{}, ErrMalformedEvidence
	}
	id := model.ID(a.id, a.Version(), s.URL, s.FinalURL, sum, at.Format(time.RFC3339Nano), cr, kr, kind, effect, excerpt)
	ev := model.Evidence{ID: id, Project: a.id, URL: model.SafeURL(s.URL), FinalURL: model.SafeURL(s.FinalURL), SourceHash: sum, ClaimType: kind, Priority: 0, ParserID: a.id, ParserVersion: a.Version(), RetrievedAt: at, VerifiedAt: at, ExpiresAt: at.Add(30 * 24 * time.Hour), Confidence: "HIGH", Excerpt: excerpt, Valid: true}
	return model.Rule{ID: id, Project: a.id, ComponentRange: cr, KubernetesRange: kr, Kind: kind, Effect: effect, Evidence: ev}, nil
}
func versionFromURL(raw string) string {
	u, _ := url.Parse(raw)
	if u == nil {
		return ""
	}
	for _, part := range strings.Split(u.Path, "/") {
		v := strings.TrimPrefix(part, "v")
		if regexp.MustCompile(`^\d+\.\d+(?:\.\d+)?$`).MatchString(v) {
			return v
		}
	}
	return ""
}
func (a adapter) Parse(s fetch.SourceSnapshot) ([]model.Rule, error) {
	u, e := url.Parse(s.URL)
	base, _ := url.Parse(a.source)
	if e != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" {
		return nil, ErrMalformedEvidence
	}
	if rules, handled, err := a.parseMetadata(s); handled {
		return rules, err
	}
	official := u.Host == base.Host
	if u.Host == "api.github.com" {
		official = strings.HasPrefix(u.Path, "/repos/"+a.repo+"/releases")
	}
	if u.Host == "raw.githubusercontent.com" {
		official = strings.HasPrefix(u.Path, "/"+a.repo+"/")
	}
	if !official {
		return nil, ErrMalformedEvidence
	}
	if strings.HasSuffix(s.URL, "/releases") && strings.Contains(s.URL, "api.github.com/") {
		return a.parseReleases(s)
	}
	body := string(s.Body)
	component := versionFromURL(s.URL)
	if component == "" {
		component = a.branch
	}
	if a.id == "flux" {
		var release struct {
			Tag               string `json:"tag_name"`
			Body              string `json:"body"`
			Draft, Prerelease bool
		}
		if e := json.Unmarshal(s.Body, &release); e != nil {
			return nil, ErrMalformedEvidence
		}
		if release.Draft || release.Prerelease {
			return nil, ErrMissingEvidence
		}
		component = strings.TrimPrefix(release.Tag, "v")
		body = release.Body
	}
	var result []model.Rule
	add := func(c, k, kind, effect, line string) error {
		r, e := a.rule(s, c, k, kind, effect, line)
		if errors.Is(e, ErrMissingEvidence) {
			return nil
		}
		if e != nil {
			return e
		}
		result = append(result, r)
		return nil
	}
	table := rows(body)
	headers := map[string]int{}
	tableType := ""
	historical := false
	for _, row := range table {
		if len(row) == 0 {
			tableType = ""
			continue
		}
		header := strings.ToLower(strings.Join(row, "|"))
		recognized := ""
		switch a.id {
		case "cert-manager":
			if row[0] == "Release" && strings.Contains(header, "kubernetes") {
				recognized = "cert"
			}
		case "argocd":
			if strings.Contains(header, "argo cd version") && strings.Contains(header, "kubernetes versions") {
				recognized = "argocd"
			}
		case "metrics-server":
			if strings.Contains(header, "metrics server") && strings.Contains(header, "supported kubernetes version") {
				recognized = "metrics"
			}
		case "cilium":
			if strings.Contains(header, "k8s version") && strings.Contains(header, "ciliumnetworkpolicy") {
				recognized = "cilium"
			}
		case "ingress-nginx":
			if strings.Contains(header, "ingress-nginx version") && strings.Contains(header, "k8s supported version") {
				recognized = "ingress"
			}
		case "velero":
			if strings.Contains(header, "velero version") && strings.Contains(header, "expected kubernetes") {
				recognized = "velero"
			}
		case "keda":
			if len(row) == 2 && strings.EqualFold(row[0], "KEDA") && strings.EqualFold(row[1], "Kubernetes") {
				recognized = "keda"
			}
		case "istio":
			if strings.Contains(header, "currently supported") && strings.Contains(header, "supported kubernetes versions") {
				recognized = "istio"
			}
		case "flux":
			if len(row) == 2 && row[0] == "Kubernetes version" && row[1] == "Minimum required" {
				recognized = "flux"
			}
		}
		if recognized != "" {
			tableType = recognized
			historical = strings.Contains(header, "compatible kubernetes")
			headers = map[string]int{}
			for i, h := range row {
				headers[strings.ToLower(h)] = i
			}
			if tableType == "cert" {
				if historical {
					if (len(row) != 4 && len(row) != 5) || row[0] != "Release" || row[1] != "Release Date" || row[2] != "EOL" || row[3] != "Compatible Kubernetes versions" {
						return nil, ErrMalformedEvidence
					}
					if len(row) == 5 && row[4] != "Compatible OpenShift versions" {
						return nil, ErrMalformedEvidence
					}
				} else {
					if len(row) != 5 || row[0] != "Release" || row[1] != "Release Date" || row[2] != "End of Life" || !strings.Contains(strings.ToLower(row[3]), "supported kubernetes") || !strings.Contains(strings.ToLower(row[4]), "tested kubernetes") {
						return nil, ErrMalformedEvidence
					}
				}
			}
			continue
		}
		if tableType == "" {
			continue
		}
		line := strings.Join(row, " | ")
		var e error
		switch tableType {
		case "argocd", "keda":
			if len(row) != 2 {
				return nil, ErrMalformedEvidence
			}
			e = add(row[0], row[1], "tested", "tested", line)
		case "metrics":
			if len(row) != 3 {
				return nil, ErrMalformedEvidence
			}
			e = add(row[0], row[2], "support", "support", line)
		case "cilium":
			if len(row) != 3 {
				return nil, ErrMalformedEvidence
			}
			e = add(component, row[0], "tested", "tested", line)
			if e == nil {
				e = add(component, row[0], "support", "positive-only", line)
			}
			tableType = ""
		case "ingress":
			if len(row) != 6 {
				return nil, ErrMalformedEvidence
			}
			if strings.Contains(row[1], "beta") {
				continue
			}
			e = add(row[1], row[2], "tested", "tested", line)
		case "velero":
			if len(row) != 3 {
				return nil, ErrMalformedEvidence
			}
			e = add(row[0], row[1], "support", "support", line)
			if e == nil {
				e = add(row[0], row[2], "tested", "tested", line)
			}
		case "istio":
			if len(row) != 6 {
				return nil, ErrMalformedEvidence
			}
			if row[0] == "master" {
				continue
			}
			e = add(row[0], row[4], "support", "support", line)
			if e == nil {
				e = add(row[0], row[4]+", "+row[5], "tested", "tested", line)
			}
			if e == nil {
				effect := "SUPPORTED"
				if row[1] == "No" {
					effect = "EOL"
				}
				e = add(row[0], "*", "lifecycle", effect, line)
			}
		case "cert":
			if len(row) != len(headers) {
				return nil, ErrMalformedEvidence
			}
			if strings.Contains(row[3], "TBD") {
				continue
			}
			before := len(result)
			release := strings.TrimSuffix(row[0], " LTS")
			e = add(release, row[3], "support", "support", line)
			if e == nil && len(result) > before && !historical {
				r := &result[len(result)-1]
				if strings.Contains(body, "Only the last patch release") {
					r.Environment = map[string]string{"latestPatch": "true"}
				}
			}
			if e == nil {
				if historical {
					e = add(release, "*", "lifecycle", "EOL", line)
				} else {
					e = add(release, row[4], "tested", "tested", line)
				}
			}
			if release != row[0] {
				for i := before; i < len(result); i++ {
					result[i].Environment = map[string]string{"supportChannel": "LTS"}
				}
			}
		case "flux":
			if len(row) != 2 {
				return nil, ErrMalformedEvidence
			}
			minor := versionRE.FindString(row[0])
			v, er := semver.NewVersion(minor)
			if er != nil {
				return nil, ErrMalformedEvidence
			}
			constraint := row[1] + fmt.Sprintf(" < %d.%d.0", v.Major(), v.Minor()+1)
			e = add(component, constraint, "support", "support", line)
		}
		if e != nil {
			return nil, e
		}
	}
	_ = headers
	plain := body
	if strings.Contains(strings.ToLower(body), "<html") || strings.Contains(body, "<p") || strings.Contains(body, "<li") {
		doc, e := html.Parse(strings.NewReader(body))
		if e == nil {
			plain = clean(text(doc))
		}
	}
	switch a.id {
	case "calico":
		m := regexp.MustCompile(`(?s)We test Calico v([0-9.]+) against the following Kubernetes versions\.(.*?)Newer versions`).FindStringSubmatch(plain)
		if len(m) > 0 {
			vs := versionRE.FindAllString(m[2], -1)
			if len(vs) == 0 {
				return nil, ErrMalformedEvidence
			}
			if e := add(m[1], strings.Join(vs, ","), "tested", "tested", m[0]); e != nil {
				return nil, e
			}
		}
	case "prometheus-operator":
		m := regexp.MustCompile("requires at least Kubernetes version\\s*`?([0-9.]+)").FindStringSubmatch(plain)
		if len(m) > 0 {
			if e := add(component, ">="+m[1], "requirement", "minimum", m[0]); e != nil {
				return nil, e
			}
		}
	case "rook-ceph":
		m := regexp.MustCompile(`Kubernetes versions\s*v?([0-9.]+)\s*through\s*v?([0-9.]+)\s*are supported`).FindStringSubmatch(strings.ReplaceAll(plain, "*", ""))
		if len(m) > 0 {
			if e := add(component, m[1]+" - "+m[2], "support", "support", m[0]); e != nil {
				return nil, e
			}
			result[len(result)-1].Prerequisites = []string{"Ceph storage devices and host kernel prerequisites must be verified"}
		}
	case "longhorn":
		m := regexp.MustCompile(`Kubernetes\s*>=\s*v?([0-9.]+)`).FindStringSubmatch(plain)
		if len(m) > 0 {
			if e := add(component, ">="+m[1], "requirement", "minimum", m[0]); e != nil {
				return nil, e
			}
			result[len(result)-1].Prerequisites = []string{"open-iscsi/iscsid, mount propagation, privileged root access; NFS client for RWX"}
		}
	case "kyverno":
		var release, kube string
		for _, r := range table {
			if len(r) != 2 {
				continue
			}
			if strings.Contains(r[0], "Supported Release:") {
				release = versionRE.FindString(r[1])
			}
			if strings.Contains(r[0], "Kubernetes Versions Supported:") {
				kube = r[1]
			}
		}
		if release != "" && kube != "" {
			if e := add(release, kube, "tested", "tested", "Kyverno community patch support schedule: "+release+" / "+kube); e != nil {
				return nil, e
			}
		}
	case "coredns":
		if strings.Contains(plain, "This plugin implements the") && strings.Contains(plain, "Kubernetes DNS-Based Service Discovery") {
			if e := add(component, "*", "context", "plugin", "Kubernetes plugin implements DNS service discovery; no version compatibility matrix is published in this source"); e != nil {
				return nil, e
			}
		}
	}
	if a.id == "flux" && len(result) > 0 {
		combined := result[0]
		var ranges []string
		for _, r := range result {
			ranges = append(ranges, r.KubernetesRange)
		}
		combined.KubernetesRange = strings.Join(ranges, " || ")
		combined.ID = model.ID(combined.ID, combined.KubernetesRange)
		combined.Evidence.ID = combined.ID
		result = []model.Rule{combined}
	}
	if len(result) == 0 {
		return nil, ErrMissingEvidence
	}
	return result, nil
}
func (a adapter) parseReleases(s fetch.SourceSnapshot) ([]model.Rule, error) {
	var releases []struct {
		Tag        string    `json:"tag_name"`
		Draft      bool      `json:"draft"`
		Prerelease bool      `json:"prerelease"`
		Published  time.Time `json:"published_at"`
	}
	if e := json.Unmarshal(s.Body, &releases); e != nil {
		return nil, ErrMalformedEvidence
	}
	var out []model.Rule
	for _, v := range releases {
		raw := strings.TrimPrefix(v.Tag, "v")
		p, e := semver.StrictNewVersion(raw)
		if e != nil || v.Draft || v.Prerelease || p.Prerelease() != "" || v.Published.IsZero() || v.Published.After(s.RetrievedAt) {
			continue
		}
		r, e := a.rule(s, raw, "*", "release", "released", "Official released stable tag "+v.Tag)
		if e != nil {
			return nil, e
		}
		r.ReleaseVersions = []string{raw}
		r.Evidence.Priority = 2
		out = append(out, r)
	}
	if len(out) == 0 {
		return nil, ErrMissingEvidence
	}
	return out, nil
}
