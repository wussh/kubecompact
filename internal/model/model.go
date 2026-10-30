package model

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strings"
	"time"
)

const SchemaVersion = "kubecompat.io/v1alpha1"
const Version = "0.1.0-dev"

type Verdict string

const (
	Compatible      Verdict = "COMPATIBLE"
	Incompatible    Verdict = "INCOMPATIBLE"
	UpgradeRequired Verdict = "UPGRADE_REQUIRED"
	NotTested       Verdict = "NOT_TESTED"
	Unknown         Verdict = "UNKNOWN"
)

type ScanOptions struct {
	Kubeconfig, Context, Namespace                        string
	HelmStorageNamespace                                  string
	AllNamespaces, HelmStorage                            bool
	Manifests, GitURL, GitRevision, GitPath, Render       string
	Timeout                                               time.Duration
	TargetKubernetes, ValuesFile, CAFile, CredentialsFile string
	Online                                                bool
	AllowedHosts                                          []string
	IdentityMappings                                      map[string]string
}

type CheckOptions struct {
	TargetKubernetes              string
	Online                        bool
	CachePath, CAFile, SchemaPath string
	DeprecatedUsageFile           string
	Now                           time.Time
}

type Filters struct{ Namespace, Controller, Component, Status, EvidenceQuality string }

type ResourceRef struct {
	ClusterID  string `json:"clusterId"`
	APIVersion string `json:"apiVersion,omitempty"`
	Group      string `json:"group,omitempty"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name"`
	UID        string `json:"uid,omitempty"`
	Document   string `json:"document,omitempty"`
}

func (r ResourceRef) Key() string {
	if r.UID != "" {
		return ID(r.ClusterID, r.UID)
	}
	return ID(r.ClusterID, r.Group, r.Kind, r.Namespace, r.Name, r.Document)
}

type OwnerRef struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	UID        string `json:"uid,omitempty"`
	Controller bool   `json:"controller,omitempty"`
}

type ImageObservation struct {
	Resource   ResourceRef `json:"resource"`
	Container  string      `json:"container"`
	Role       string      `json:"role"`
	Image      string      `json:"image"`
	Registry   string      `json:"registry,omitempty"`
	Repository string      `json:"repository,omitempty"`
	Tag        string      `json:"tag,omitempty"`
	Digest     string      `json:"digest,omitempty"`
	ImageID    string      `json:"imageId,omitempty"`
}

type ChartObservation struct {
	Name             string `json:"name"`
	Repository       string `json:"repository,omitempty"`
	Version          string `json:"version,omitempty"`
	AppVersion       string `json:"appVersion,omitempty"`
	KubeVersion      string `json:"kubeVersion,omitempty"`
	ReleaseName      string `json:"releaseName,omitempty"`
	ReleaseNamespace string `json:"releaseNamespace,omitempty"`
	StorageNamespace string `json:"storageNamespace,omitempty"`
	Status           string `json:"status,omitempty"`
}

type VersionObservation struct {
	Role               string    `json:"role"`
	Scheme             string    `json:"scheme"`
	Raw                string    `json:"raw"`
	Normalized         string    `json:"normalized,omitempty"`
	SourceID           string    `json:"sourceId"`
	Field              string    `json:"field"`
	ObservedGeneration int64     `json:"observedGeneration,omitempty"`
	Successful         bool      `json:"successful,omitempty"`
	ObservedAt         time.Time `json:"observedAt"`
	Confidence         string    `json:"confidence"`
}

type Source struct {
	Index             int      `json:"index"`
	Kind              string   `json:"kind,omitempty"`
	RepoURL           string   `json:"repoUrl,omitempty"`
	Path              string   `json:"path,omitempty"`
	Chart             string   `json:"chart,omitempty"`
	Revision          string   `json:"revision,omitempty"`
	ResolvedRevision  string   `json:"resolvedRevision,omitempty"`
	Ref               string   `json:"ref,omitempty"`
	ValuesOnly        bool     `json:"valuesOnly,omitempty"`
	ID                string   `json:"id,omitempty"`
	Renderer          string   `json:"renderer,omitempty"`
	RendererVersion   string   `json:"rendererVersion,omitempty"`
	TargetKubernetes  string   `json:"targetKubernetes,omitempty"`
	InputDigest       string   `json:"inputDigest,omitempty"`
	DependencyDigests []string `json:"dependencyDigests,omitempty"`
}

type Resource struct {
	Ref         ResourceRef        `json:"ref"`
	Labels      map[string]string  `json:"labels,omitempty"`
	Annotations map[string]string  `json:"annotations,omitempty"`
	Owners      []OwnerRef         `json:"owners,omitempty"`
	Images      []ImageObservation `json:"images,omitempty"`
	// Facts contains only the extractor's documented readiness/status allowlist.
	Facts    map[string]any `json:"facts,omitempty"`
	Document []byte         `json:"-"`
}

type Component struct {
	ID                 string               `json:"id"`
	Name               string               `json:"name"`
	Namespace          string               `json:"namespace,omitempty"`
	ClusterID          string               `json:"clusterId"`
	Project            string               `json:"project,omitempty"`
	ProjectRepository  string               `json:"projectRepository,omitempty"`
	IdentityConfidence string               `json:"identityConfidence,omitempty"`
	ManagedBy          []string             `json:"managedBy"`
	Controller         *ResourceRef         `json:"controller,omitempty"`
	Resources          []ResourceRef        `json:"resources"`
	Dependencies       []string             `json:"dependencies,omitempty"`
	Chart              *ChartObservation    `json:"chart,omitempty"`
	Sources            []Source             `json:"sources,omitempty"`
	Versions           []VersionObservation `json:"versions,omitempty"`
	Images             []ImageObservation   `json:"images,omitempty"`
	Status             string               `json:"status,omitempty"`
	Destination        string               `json:"destination,omitempty"`
	ObservedAt         time.Time            `json:"observedAt"`
	Gaps               []string             `json:"gaps,omitempty"`
}

type ClusterObservation struct {
	ID                string            `json:"id"`
	KubernetesVersion string            `json:"kubernetesVersion,omitempty"`
	Distribution      string            `json:"distribution,omitempty"`
	Confidence        string            `json:"confidence,omitempty"`
	Nodes             []NodeObservation `json:"nodes,omitempty"`
	ObservedAt        time.Time         `json:"observedAt"`
	Gaps              []string          `json:"gaps,omitempty"`
}
type NodeObservation struct {
	Name           string `json:"name"`
	KubeletVersion string `json:"kubeletVersion"`
	Architecture   string `json:"architecture"`
	OS             string `json:"os,omitempty"`
	Runtime        string `json:"runtime,omitempty"`
	Ready          bool   `json:"ready"`
	ControlPlane   bool   `json:"controlPlane,omitempty"`
}

type Coverage struct {
	Collector string   `json:"collector"`
	Scope     string   `json:"scope"`
	State     string   `json:"state"`
	Kinds     []string `json:"kinds,omitempty"`
	Code      string   `json:"code,omitempty"`
	Omissions []string `json:"omissions,omitempty"`
}
type Diagnostic struct {
	Code    string `json:"code"`
	Scope   string `json:"scope"`
	Message string `json:"message"`
}

type Inventory struct {
	SchemaVersion  string               `json:"schemaVersion"`
	ScannerVersion string               `json:"scannerVersion"`
	ScanID         string               `json:"scanId"`
	StartedAt      time.Time            `json:"startedAt"`
	FinishedAt     time.Time            `json:"finishedAt"`
	InputMode      string               `json:"inputMode"`
	Scope          string               `json:"scope"`
	Clusters       []ClusterObservation `json:"clusters"`
	Resources      []Resource           `json:"resources"`
	Components     []Component          `json:"components"`
	Sources        []Source             `json:"sources,omitempty"`
	Coverage       []Coverage           `json:"coverage"`
	Diagnostics    []Diagnostic         `json:"diagnostics,omitempty"`
	Objects        []map[string]any     `json:"-"`
}

func NewInventory(mode, scope string) Inventory {
	now := time.Now().UTC()
	return Inventory{SchemaVersion: SchemaVersion, ScannerVersion: Version, ScanID: ID(now.Format(time.RFC3339Nano), mode, scope), StartedAt: now, InputMode: mode, Scope: scope, Clusters: []ClusterObservation{}, Resources: []Resource{}, Components: []Component{}, Coverage: []Coverage{}}
}
func (i Inventory) Incomplete() bool {
	for _, c := range i.Coverage {
		if c.State == "PARTIAL" || c.State == "UNREADABLE" {
			return true
		}
	}
	return false
}
func (i *Inventory) Finish() {
	i.FinishedAt = time.Now().UTC()
	sort.Slice(i.Resources, func(a, b int) bool { return i.Resources[a].Ref.Key() < i.Resources[b].Ref.Key() })
	sort.Slice(i.Components, func(a, b int) bool { return i.Components[a].ID < i.Components[b].ID })
}

type Evidence struct {
	ID             string    `json:"id"`
	Project        string    `json:"project"`
	URL            string    `json:"url"`
	FinalURL       string    `json:"finalUrl,omitempty"`
	SourceHash     string    `json:"sourceHash"`
	SourceRevision string    `json:"sourceRevision,omitempty"`
	ClaimType      string    `json:"claimType"`
	Priority       int       `json:"priority"`
	ParserID       string    `json:"parserId"`
	ParserVersion  string    `json:"parserVersion"`
	RetrievedAt    time.Time `json:"retrievedAt"`
	VerifiedAt     time.Time `json:"verifiedAt"`
	ExpiresAt      time.Time `json:"expiresAt"`
	Confidence     string    `json:"confidence"`
	Excerpt        string    `json:"excerpt"`
	License        string    `json:"license,omitempty"`
	Valid          bool      `json:"valid"`
}

func (e Evidence) Fresh(now time.Time) bool {
	return e.Valid && !e.VerifiedAt.IsZero() && !e.VerifiedAt.After(now) && !e.RetrievedAt.After(now) && !e.ExpiresAt.IsZero() && now.Before(e.ExpiresAt)
}

type Rule struct {
	ID              string            `json:"id"`
	Project         string            `json:"project"`
	ComponentRange  string            `json:"componentRange"`
	KubernetesRange string            `json:"kubernetesRange"`
	Kind            string            `json:"kind"`
	Effect          string            `json:"effect"`
	Prerequisites   []string          `json:"prerequisites,omitempty"`
	Environment     map[string]string `json:"environment,omitempty"`
	ReleaseVersions []string          `json:"releaseVersions,omitempty"`
	ChartVersions   map[string]string `json:"chartVersions,omitempty"`
	ChartName       string            `json:"chartName,omitempty"`
	ChartRepository string            `json:"chartRepository,omitempty"`
	Evidence        Evidence          `json:"evidence"`
}

type Recommendation struct {
	Version       string   `json:"version"`
	Scheme        string   `json:"scheme"`
	ChartVersion  string   `json:"chartVersion,omitempty"`
	Prerequisites []string `json:"prerequisites,omitempty"`
	EvidenceIDs   []string `json:"evidenceIds"`
}
type CheckResult struct {
	ID          string            `json:"id"`
	Domain      string            `json:"domain"`
	Scope       string            `json:"scope"`
	State       string            `json:"state"`
	Severity    string            `json:"severity"`
	Reason      string            `json:"reason"`
	EvidenceIDs []string          `json:"evidenceIds,omitempty"`
	Details     map[string]string `json:"details,omitempty"`
}
type ComponentAssessment struct {
	Component        Component       `json:"component"`
	TargetKubernetes string          `json:"targetKubernetes"`
	Verdict          Verdict         `json:"verdict"`
	Reason           string          `json:"reason"`
	EvidenceIDs      []string        `json:"evidenceIds"`
	Recommendation   *Recommendation `json:"recommendation,omitempty"`
	Tested           *bool           `json:"tested,omitempty"`
	Lifecycle        string          `json:"lifecycle,omitempty"`
	Checks           []CheckResult   `json:"checks,omitempty"`
}
type Assessment struct {
	SchemaVersion     string                `json:"schemaVersion"`
	ScannerVersion    string                `json:"scannerVersion"`
	ScanID            string                `json:"scanId"`
	TargetKubernetes  string                `json:"targetKubernetes"`
	ResolvedTarget    string                `json:"resolvedTarget,omitempty"`
	UpgradePathStatus string                `json:"upgradePathStatus"`
	AssessedAt        time.Time             `json:"assessedAt"`
	Components        []ComponentAssessment `json:"components"`
	Checks            []CheckResult         `json:"checks"`
	Evidence          []Evidence            `json:"evidence"`
	Coverage          []Coverage            `json:"coverage"`
	Sources           []Source              `json:"sources,omitempty"`
	Diagnostics       []Diagnostic          `json:"diagnostics,omitempty"`
	Decision          string                `json:"decision"`
	ExitCode          int                   `json:"exitCode"`
}

func (a *Assessment) Gate() {
	blocker, unknown := false, len(a.Components) == 0
	for _, c := range a.Components {
		blocker = blocker || c.Verdict == Incompatible || c.Verdict == UpgradeRequired
		unknown = unknown || c.Verdict == Unknown || c.Verdict == NotTested
	}
	for _, c := range a.Coverage {
		unknown = unknown || c.State == "PARTIAL" || c.State == "UNREADABLE"
	}
	for _, c := range a.Checks {
		blocker = blocker || (c.State == "FAIL" && c.Severity == "BLOCKER")
		unknown = unknown || (c.State == "UNKNOWN" && c.Severity != "INFO")
	}
	a.Decision = "NO_KNOWN_BLOCKERS"
	a.ExitCode = 0
	if unknown {
		a.Decision = "INCOMPLETE"
		a.ExitCode = 3
	}
	if blocker {
		a.Decision = "BLOCKED"
		a.ExitCode = 2
	}
	if len(a.Diagnostics) > 0 {
		a.Decision = "INVALID_INPUT"
		a.ExitCode = 1
	}
}
func ID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}
func SafeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "oci") {
		return ""
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
