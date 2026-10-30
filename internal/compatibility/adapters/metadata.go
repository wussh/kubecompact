package adapters

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	semver "github.com/Masterminds/semver/v3"
	"github.com/wussh/kubecompact/internal/compatibility/fetch"
	"github.com/wussh/kubecompact/internal/model"
	"go.yaml.in/yaml/v3"
)

// These endpoints are linked by cert-manager's official Helm installation page.
const certChartIndex = "https://charts.jetstack.io/index.yaml"
const certArtifactHub = "https://artifacthub.io/api/v1/packages/helm/cert-manager/cert-manager"
const certOCI = "https://quay.io/v2/jetstack/charts/cert-manager/"

func (a adapter) metadataSources() []fetch.SourceRequest {
	if a.id != "cert-manager" {
		return nil
	}
	var out []fetch.SourceRequest
	for _, raw := range []string{certChartIndex, certArtifactHub, certOCI + "tags/list"} {
		out = append(out, fetch.SourceRequest{URL: raw, Project: a.id, ParserID: a.id, ParserVersion: a.Version()})
	}
	return out
}

var digestRE = regexp.MustCompile(`^[0-9a-f]{64}$`)
var sourceLinkRE = regexp.MustCompile(`https://[^\s"'<>\)]+`)

func stableVersion(raw string) string {
	v, err := semver.StrictNewVersion(strings.TrimPrefix(raw, "v"))
	if err != nil || v.Prerelease() != "" {
		return ""
	}
	return v.String()
}

func (a adapter) mapping(s fetch.SourceSnapshot, app, chart, repository, kube, digest string, priority int) (model.Rule, error) {
	app, chart = stableVersion(app), stableVersion(chart)
	if app == "" || chart == "" || (digest != "" && !digestRE.MatchString(digest)) {
		return model.Rule{}, ErrMalformedEvidence
	}
	if kube == "" {
		kube = "*"
	}
	r, err := a.rule(s, app, kube, "chart-mapping", "metadata", fmt.Sprintf("cert-manager chart %s maps to application %s; artifact SHA256 %s; metadata is not runtime support", chart, app, digest))
	if err != nil {
		return model.Rule{}, err
	}
	r.ChartVersions = map[string]string{app: chart}
	r.ChartName, r.ChartRepository = "cert-manager", repository
	r.Evidence.Priority = priority
	return r, nil
}

func (a adapter) parseMetadata(s fetch.SourceSnapshot) ([]model.Rule, bool, error) {
	if a.id != "cert-manager" {
		return nil, false, nil
	}
	var out []model.Rule
	switch {
	case s.URL == certChartIndex:
		var index struct {
			Entries map[string][]struct {
				Name, Version, Digest string
				AppVersion            string `yaml:"appVersion"`
				KubeVersion           string `yaml:"kubeVersion"`
				Created               time.Time
				Deprecated            bool
			} `yaml:"entries"`
		}
		if yaml.Unmarshal(s.Body, &index) != nil || index.Entries == nil {
			return nil, true, ErrMalformedEvidence
		}
		for _, entry := range index.Entries["cert-manager"] {
			if entry.Deprecated || entry.Name != "cert-manager" || stableVersion(entry.AppVersion) == "" || stableVersion(entry.Version) == "" || entry.Created.IsZero() || entry.Created.After(s.RetrievedAt) || !digestRE.MatchString(entry.Digest) {
				continue
			}
			r, err := a.mapping(s, entry.AppVersion, entry.Version, "https://charts.jetstack.io", entry.KubeVersion, entry.Digest, 2)
			if err != nil {
				return nil, true, err
			}
			out = append(out, r)
		}
	case s.URL == certArtifactHub:
		var p struct {
			Name     string `json:"name"`
			Versions []struct {
				Version    string `json:"version"`
				AppVersion string `json:"app_version"`
				Prerelease bool   `json:"prerelease"`
			} `json:"available_versions"`
		}
		if json.Unmarshal(s.Body, &p) != nil || p.Name != "cert-manager" {
			return nil, true, ErrMalformedEvidence
		}
		for _, version := range p.Versions {
			if version.Prerelease || stableVersion(version.Version) == "" || stableVersion(version.AppVersion) == "" {
				continue
			}
			r, err := a.mapping(s, version.AppVersion, version.Version, "https://charts.jetstack.io", "*", "", 4)
			if err != nil {
				return nil, true, err
			}
			out = append(out, r)
		}
	case s.URL == certOCI+"tags/list", strings.HasPrefix(s.URL, certOCI+"manifests/"):
		var p struct {
			Name          string   `json:"name"`
			Tags          []string `json:"tags"`
			SchemaVersion int      `json:"schemaVersion"`
			Config        struct {
				MediaType string `json:"mediaType"`
				Digest    string `json:"digest"`
				Size      int64  `json:"size"`
			} `json:"config"`
		}
		if json.Unmarshal(s.Body, &p) != nil {
			return nil, true, ErrMalformedEvidence
		}
		if s.URL == certOCI+"tags/list" {
			if p.Name != "jetstack/charts/cert-manager" || p.Tags == nil || len(p.Tags) > 4096 {
				return nil, true, ErrMalformedEvidence
			}
		} else if p.SchemaVersion != 2 || p.Config.MediaType != "application/vnd.cncf.helm.config.v1+json" || p.Config.Size <= 0 || p.Config.Size > 4<<20 || !strings.HasPrefix(p.Config.Digest, "sha256:") || !digestRE.MatchString(strings.TrimPrefix(p.Config.Digest, "sha256:")) {
			return nil, true, ErrMalformedEvidence
		}
		r, err := a.rule(s, "0.0.0", "*", "artifact", "metadata", "Official OCI discovery metadata; not a release or Kubernetes support claim")
		if err != nil {
			return nil, true, err
		}
		r.Evidence.Priority = 2
		out = append(out, r)
	case strings.HasPrefix(s.URL, certOCI+"blobs/sha256:"):
		digest := strings.TrimPrefix(s.URL, certOCI+"blobs/sha256:")
		sum := sha256.Sum256(s.Body)
		if !digestRE.MatchString(digest) || digest != hex.EncodeToString(sum[:]) {
			return nil, true, ErrMalformedEvidence
		}
		var p struct {
			Name        string `json:"name"`
			Version     string `json:"version"`
			AppVersion  string `json:"appVersion"`
			KubeVersion string `json:"kubeVersion"`
		}
		if json.Unmarshal(s.Body, &p) != nil || p.Name != "cert-manager" {
			return nil, true, ErrMalformedEvidence
		}
		r, err := a.mapping(s, p.AppVersion, p.Version, "oci://quay.io/jetstack/charts", p.KubeVersion, digest, 2)
		if err != nil {
			return nil, true, err
		}
		out = append(out, r)
	default:
		return nil, false, nil
	}
	if len(out) == 0 {
		return nil, true, ErrMissingEvidence
	}
	return out, true, nil
}

// Discover follows at most two reviewed-origin links per document. The caller also caps the complete project queue.
func Discover(project string, s fetch.SourceSnapshot) []fetch.SourceRequest {
	a, ok := ByID(project).(adapter)
	if !ok {
		return nil
	}
	var links []string
	if project == "cert-manager" && s.URL == certOCI+"tags/list" {
		var p struct {
			Tags []string `json:"tags"`
		}
		if json.Unmarshal(s.Body, &p) == nil && len(p.Tags) <= 4096 {
			for _, tag := range p.Tags {
				if stableVersion(tag) != "" {
					links = append(links, tag)
				}
			}
			sort.Slice(links, func(i, j int) bool {
				x, _ := semver.NewVersion(links[i])
				y, _ := semver.NewVersion(links[j])
				return x.GreaterThan(y)
			})
			if len(links) > 2 {
				links = links[:2]
			}
			for i, tag := range links {
				links[i] = certOCI + "manifests/" + tag
			}
		}
	} else if project == "cert-manager" && strings.HasPrefix(s.URL, certOCI+"manifests/") {
		var p struct {
			Config struct {
				MediaType string `json:"mediaType"`
				Digest    string `json:"digest"`
				Size      int64  `json:"size"`
			} `json:"config"`
		}
		if json.Unmarshal(s.Body, &p) == nil && p.Config.MediaType == "application/vnd.cncf.helm.config.v1+json" && p.Config.Size > 0 && p.Config.Size <= 4<<20 && strings.HasPrefix(p.Config.Digest, "sha256:") && digestRE.MatchString(strings.TrimPrefix(p.Config.Digest, "sha256:")) {
			links = append(links, certOCI+"blobs/"+p.Config.Digest)
		}
	} else {
		base, _ := url.Parse(a.source)
		origin, _ := url.Parse(s.URL)
		if origin == nil || origin.Host != base.Host {
			return nil
		}
		seen := map[string]bool{}
		for _, raw := range sourceLinkRE.FindAllString(string(s.Body), 64) {
			u, err := url.Parse(raw)
			if err != nil || u.Scheme != "https" || u.Host != base.Host || u.User != nil || u.RawQuery != "" || u.Fragment != "" || versionFromURL(raw) == "" || seen[raw] {
				continue
			}
			path := strings.ToLower(u.Path)
			if !strings.Contains(path, "compatib") && !strings.Contains(path, "supported") && !strings.Contains(path, "release-notes") {
				continue
			}
			seen[raw] = true
			links = append(links, raw)
		}
		sort.Strings(links)
		if len(links) > 2 {
			links = links[:2]
		}
	}
	var out []fetch.SourceRequest
	for _, raw := range links {
		out = append(out, fetch.SourceRequest{URL: raw, Project: project, ParserID: project, ParserVersion: a.Version()})
	}
	return out
}
