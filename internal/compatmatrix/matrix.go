package compatmatrix

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/wussh/kubecompact/internal/compatibility/projects"
	"github.com/wussh/kubecompact/internal/model"
)

// MatrixSchemaVersion is the tracker's pinned dataset schema. The default JSON
// output of `matrix build` reuses it unchanged; product-only fields live under
// "extensions" and require --extended.
const MatrixSchemaVersion = "compat-matrix/v1"

// MatrixCurrentStateSchemaVersion is the optional current-state block schema.
const MatrixCurrentStateSchemaVersion = "compat-current-state/v1"

// MatrixStaleDays is the tracker's evidence freshness threshold (PROCESS.md §7).
const MatrixStaleDays = 90

// Installation states. `absent` requires positive evidence; `out-of-scope`
// requires an explicit scope entry; `unreachable` requires explicit
// reachability=false. Nothing else may produce them.
const (
	InstallInstalled  = "installed"
	InstallAbsent     = "absent"
	InstallUnknown    = "unknown"
	InstallOutOfScope = "out-of-scope"
)

var matrixMinorPattern = regexp.MustCompile(`^1\.[0-9]+$`)
var matrixDatePattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)

type MatrixCell struct {
	Status          string `json:"status"`
	RequiredVersion string `json:"requiredVersion"`
	Source          string `json:"source"`
	VerifiedAt      string `json:"verifiedAt"`
	Notes           string `json:"notes,omitempty"`
}

type MatrixComponent struct {
	ID           string                `json:"id"`
	Name         string                `json:"name"`
	Category     string                `json:"category"`
	Group        string                `json:"group,omitempty"`
	OfficialDocs string                `json:"officialDocs"`
	Notes        string                `json:"notes,omitempty"`
	Kubernetes   map[string]MatrixCell `json:"kubernetes"`
}

type MatrixK3sRelease struct {
	Kubernetes string `json:"kubernetes"`
	Release    string `json:"release"`
	Source     string `json:"source"`
	VerifiedAt string `json:"verifiedAt"`
}

type MatrixDataset struct {
	SchemaVersion string             `json:"schemaVersion"`
	GeneratedAt   string             `json:"generatedAt"`
	Kubernetes    []string           `json:"kubernetes"`
	K3sReleases   []MatrixK3sRelease `json:"k3sReleases"`
	Components    []MatrixComponent  `json:"components"`
	CurrentState  json.RawMessage    `json:"currentState,omitempty"`

	Digest string `json:"-"`
	state  *matrixState
}

// MatrixScopeExclusion is one explicitly excluded cluster.
type MatrixScopeExclusion struct {
	ID     string `json:"id"`
	Reason string `json:"reason,omitempty"`
}

// MatrixScope mirrors the tracker's scope block (compat-current-state/v1).
type MatrixScope struct {
	InScope    []string               `json:"inScope,omitempty"`
	OutOfScope []MatrixScopeExclusion `json:"outOfScope,omitempty"`
}

// MatrixUnreachable is one observed unreachable context.
type MatrixUnreachable struct {
	Context string `json:"context"`
	Reason  string `json:"reason,omitempty"`
}

// LoadMatrixDataset validates a pinned compat-matrix/v1 dataset. Validation
// mirrors the tracker validator: status enum, https source, ISO dates, an exact
// per-component minor set, unique components/releases/minors and no duplicate
// JSON keys. It never rewrites the input.
func LoadMatrixDataset(data []byte) (*MatrixDataset, error) {
	if err := rejectDuplicateKeys(data); err != nil {
		return nil, err
	}
	var ds MatrixDataset
	if err := matrixDecodeStrict(data, &ds); err != nil {
		return nil, fmt.Errorf("invalid dataset: %w", err)
	}
	if err := ds.validate(); err != nil {
		return nil, fmt.Errorf("invalid dataset: %w", err)
	}
	sum := sha256.Sum256(data)
	ds.Digest = hex.EncodeToString(sum[:])
	return &ds, nil
}

func matrixDecodeStrict(data []byte, dest any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		return errors.New("invalid JSON or schema")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("multiple JSON documents are not supported")
	}
	return nil
}

// rejectDuplicateKeys fails on repeated object keys, which encoding/json would
// otherwise resolve last-wins. A repeated minor key would silently drop a cell.
func rejectDuplicateKeys(data []byte) error {
	type frame struct {
		keys      map[string]bool
		expectKey bool
	}
	var stack []*frame
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return errors.New("invalid dataset: invalid JSON")
		}
		if len(stack) == 0 {
			if delim, ok := token.(json.Delim); ok && (delim == '{' || delim == '[') {
				stack = append(stack, &frame{keys: map[string]bool{}, expectKey: delim == '{'})
			}
			continue
		}
		top := stack[len(stack)-1]
		switch value := token.(type) {
		case json.Delim:
			switch value {
			case '{', '[':
				stack = append(stack, &frame{keys: map[string]bool{}, expectKey: value == '{'})
			case '}', ']':
				stack = stack[:len(stack)-1]
				if len(stack) > 0 {
					stack[len(stack)-1].expectKey = stack[len(stack)-1].keys != nil
				}
			}
		default:
			if top.keys == nil {
				continue
			}
			if top.expectKey {
				key, ok := token.(string)
				if !ok {
					return errors.New("invalid dataset: invalid JSON object key")
				}
				if top.keys[key] {
					return fmt.Errorf("invalid dataset: duplicate JSON key %q", key)
				}
				top.keys[key] = true
				top.expectKey = false
			} else {
				top.expectKey = true
			}
		}
	}
}

func (d *MatrixDataset) validate() error {
	if d.SchemaVersion != MatrixSchemaVersion {
		return fmt.Errorf("schemaVersion must be %s", MatrixSchemaVersion)
	}
	if _, err := time.Parse(time.RFC3339, d.GeneratedAt); err != nil {
		return errors.New("generatedAt must be an RFC3339 timestamp")
	}
	if len(d.Kubernetes) == 0 {
		return errors.New("kubernetes minor list is required")
	}
	minorSet := map[string]bool{}
	for _, minor := range d.Kubernetes {
		if !matrixMinorPattern.MatchString(minor) {
			return fmt.Errorf("invalid kubernetes minor %q", minor)
		}
		if minorSet[minor] {
			return fmt.Errorf("duplicate kubernetes minor %q", minor)
		}
		minorSet[minor] = true
	}
	releaseMinor := map[string]bool{}
	for _, release := range d.K3sReleases {
		if !minorSet[release.Kubernetes] {
			return fmt.Errorf("k3s release for undeclared kubernetes minor %q", release.Kubernetes)
		}
		if releaseMinor[release.Kubernetes] {
			return fmt.Errorf("duplicate k3s release for kubernetes %s", release.Kubernetes)
		}
		releaseMinor[release.Kubernetes] = true
		if strings.TrimSpace(release.Release) == "" {
			return fmt.Errorf("k3s release for %s requires a release", release.Kubernetes)
		}
		if !matrixHTTPS(release.Source) {
			return fmt.Errorf("k3s release %s source must be an https URL", release.Kubernetes)
		}
		if !matrixISODate(release.VerifiedAt) {
			return fmt.Errorf("k3s release %s verifiedAt must be YYYY-MM-DD", release.Kubernetes)
		}
	}
	if len(d.Components) == 0 {
		return errors.New("at least one component is required")
	}
	seen := map[string]bool{}
	for _, component := range d.Components {
		if err := component.validate(minorSet); err != nil {
			return err
		}
		if seen[component.ID] {
			return fmt.Errorf("duplicate component %q", component.ID)
		}
		seen[component.ID] = true
	}
	if len(d.CurrentState) > 0 {
		state, err := parseMatrixState(d.CurrentState)
		if err != nil {
			return err
		}
		d.state = state
	}
	return nil
}

func (c MatrixComponent) validate(minorSet map[string]bool) error {
	for _, field := range []struct{ name, value string }{{"id", c.ID}, {"name", c.Name}, {"category", c.Category}, {"officialDocs", c.OfficialDocs}} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("component %q requires %s", c.ID, field.name)
		}
	}
	if !matrixHTTPS(c.OfficialDocs) {
		return fmt.Errorf("component %q officialDocs must be an https URL", c.ID)
	}
	if len(c.Kubernetes) != len(minorSet) {
		return fmt.Errorf("component %q must declare exactly the dataset minor set", c.ID)
	}
	for minor := range c.Kubernetes {
		if !minorSet[minor] {
			return fmt.Errorf("component %q has undeclared kubernetes minor %q", c.ID, minor)
		}
	}
	for minor, cell := range c.Kubernetes {
		where := fmt.Sprintf("component %s [%s]", c.ID, minor)
		switch cell.Status {
		case "supported", "unsupported", "unknown":
		default:
			return fmt.Errorf("%s: status must be supported, unsupported or unknown", where)
		}
		if strings.TrimSpace(cell.RequiredVersion) == "" {
			return fmt.Errorf("%s: requiredVersion is required", where)
		}
		if !matrixHTTPS(cell.Source) {
			return fmt.Errorf("%s: source must be an https URL", where)
		}
		if !matrixISODate(cell.VerifiedAt) {
			return fmt.Errorf("%s: verifiedAt must be YYYY-MM-DD", where)
		}
	}
	return nil
}

func matrixHTTPS(value string) bool {
	if value == "" || strings.ContainsAny(value, " \t\r\n") {
		return false
	}
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil
}

func matrixISODate(value string) bool {
	if !matrixDatePattern.MatchString(value) {
		return false
	}
	_, err := time.Parse("2006-01-02", value)
	return err == nil
}

// EmbeddedScope returns the dataset's explicit scope block, if any.
func (d *MatrixDataset) EmbeddedScope() *MatrixScope {
	if d.state == nil {
		return nil
	}
	return d.state.Scope
}

// ParseMatrixScope accepts either the raw tracker scope block or a document
// wrapping it under "scope". Unknown fields are ignored so a full current-state
// file can be supplied directly.
func ParseMatrixScope(data []byte) (*MatrixScope, error) {
	var probe struct {
		Scope      *MatrixScope           `json:"scope"`
		InScope    []string               `json:"inScope"`
		OutOfScope []MatrixScopeExclusion `json:"outOfScope"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, errors.New("invalid scope input")
	}
	scope := probe.Scope
	if scope == nil {
		scope = &MatrixScope{InScope: probe.InScope, OutOfScope: probe.OutOfScope}
	}
	if scope.InScope == nil && scope.OutOfScope == nil {
		return nil, errors.New("invalid scope input: no inScope/outOfScope block")
	}
	seen := map[string]bool{}
	for i, id := range scope.InScope {
		if strings.TrimSpace(id) == "" {
			return nil, errors.New("invalid scope input: empty inScope id")
		}
		if seen[id] {
			return nil, fmt.Errorf("invalid scope input: duplicate inScope id %q", id)
		}
		seen[id] = true
		scope.InScope[i] = strings.TrimSpace(id)
	}
	excluded := map[string]bool{}
	for i := range scope.OutOfScope {
		id := strings.TrimSpace(scope.OutOfScope[i].ID)
		if id == "" {
			return nil, errors.New("invalid scope input: empty outOfScope id")
		}
		if excluded[id] {
			return nil, fmt.Errorf("invalid scope input: duplicate outOfScope id %q", id)
		}
		excluded[id] = true
		scope.OutOfScope[i].ID = id
		if strings.TrimSpace(scope.OutOfScope[i].Reason) == "" {
			scope.OutOfScope[i].Reason = "out of scope"
		}
	}
	return scope, nil
}

// ---------------------------------------------------------------------------
// current-state parsing
// ---------------------------------------------------------------------------

type matrixState struct {
	SchemaVersion string
	ObservedAt    string
	Clusters      []matrixStateCluster
	Unreachable   []MatrixUnreachable
	Scope         *MatrixScope
}

type matrixStateCluster struct {
	ID                string
	KubernetesVersion string
	Reachable         bool
	Components        []matrixStateComponent
	Absent            []string
}

type matrixStateComponent struct {
	ID      string
	Version string
}

type matrixStateClusterRaw struct {
	ID                string          `json:"id"`
	Name              string          `json:"name"`
	Context           string          `json:"context"`
	KubernetesVersion string          `json:"kubernetesVersion"`
	Kubernetes        string          `json:"kubernetes"`
	Reachable         *bool           `json:"reachable"`
	Components        json.RawMessage `json:"components"`
	NotFound          []string        `json:"notFound"`
	Absent            []string        `json:"absent"`
}

func parseMatrixState(raw json.RawMessage) (*matrixState, error) {
	var doc struct {
		SchemaVersion string                 `json:"schemaVersion"`
		ObservedAt    string                 `json:"observedAt"`
		Clusters      json.RawMessage        `json:"clusters"`
		Unreachable   []MatrixUnreachable    `json:"unreachable"`
		Scope         *MatrixScope           `json:"scope"`
		InScope       []string               `json:"inScope"`
		OutOfScope    []MatrixScopeExclusion `json:"outOfScope"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, errors.New("currentState is not an object")
	}
	if doc.SchemaVersion != "" && doc.SchemaVersion != MatrixCurrentStateSchemaVersion {
		return nil, fmt.Errorf("currentState schemaVersion must be %s", MatrixCurrentStateSchemaVersion)
	}
	state := &matrixState{SchemaVersion: doc.SchemaVersion, ObservedAt: doc.ObservedAt, Unreachable: doc.Unreachable}
	if doc.Scope != nil {
		state.Scope = doc.Scope
	}
	if state.Scope == nil && (doc.InScope != nil || doc.OutOfScope != nil) {
		state.Scope = &MatrixScope{InScope: doc.InScope, OutOfScope: doc.OutOfScope}
	}
	clusters, err := parseStateClusters(doc.Clusters)
	if err != nil {
		return nil, err
	}
	state.Clusters = clusters
	seen := map[string]bool{}
	for _, cluster := range clusters {
		if cluster.ID == "" {
			return nil, errors.New("currentState cluster id is required")
		}
		if seen[cluster.ID] {
			return nil, fmt.Errorf("currentState duplicate cluster %q", cluster.ID)
		}
		seen[cluster.ID] = true
		for _, component := range cluster.Components {
			if component.ID == "" || strings.TrimSpace(component.Version) == "" {
				return nil, fmt.Errorf("currentState cluster %q component id/version required", cluster.ID)
			}
		}
	}
	if state.Scope != nil {
		if err := validateScope(state.Scope); err != nil {
			return nil, err
		}
	}
	return state, nil
}

func validateScope(scope *MatrixScope) error {
	seen := map[string]bool{}
	for _, id := range scope.InScope {
		if strings.TrimSpace(id) == "" {
			return errors.New("currentState scope inScope id is required")
		}
		seen[id] = true
	}
	excluded := map[string]bool{}
	for _, item := range scope.OutOfScope {
		if strings.TrimSpace(item.ID) == "" {
			return errors.New("currentState scope outOfScope id is required")
		}
		if excluded[item.ID] {
			return fmt.Errorf("currentState duplicate outOfScope id %q", item.ID)
		}
		excluded[item.ID] = true
	}
	return nil
}

func parseStateClusters(raw json.RawMessage) ([]matrixStateCluster, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var list []matrixStateClusterRaw
	if err := json.Unmarshal(raw, &list); err == nil {
		return normalizeStateClusters(list)
	}
	var byID map[string]matrixStateClusterRaw
	if err := json.Unmarshal(raw, &byID); err != nil {
		return nil, errors.New("currentState clusters must be a list or an object")
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	ordered := make([]matrixStateClusterRaw, 0, len(ids))
	for _, id := range ids {
		item := byID[id]
		if item.ID == "" {
			item.ID = id
		}
		ordered = append(ordered, item)
	}
	return normalizeStateClusters(ordered)
}

func normalizeStateClusters(list []matrixStateClusterRaw) ([]matrixStateCluster, error) {
	out := make([]matrixStateCluster, 0, len(list))
	for _, item := range list {
		cluster := matrixStateCluster{
			ID:                firstNonEmpty(item.ID, item.Name, item.Context),
			KubernetesVersion: strings.TrimSpace(firstNonEmpty(item.KubernetesVersion, item.Kubernetes)),
			Reachable:         item.Reachable == nil || *item.Reachable,
			Absent:            dedupeStrings(append(append([]string{}, item.NotFound...), item.Absent...)),
		}
		components, err := parseStateComponents(item.Components)
		if err != nil {
			return nil, err
		}
		cluster.Components = components
		out = append(out, cluster)
	}
	return out, nil
}

func parseStateComponents(raw json.RawMessage) ([]matrixStateComponent, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var list []struct {
		ID      string `json:"id"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &list); err == nil {
		out := make([]matrixStateComponent, 0, len(list))
		for _, item := range list {
			out = append(out, matrixStateComponent{ID: item.ID, Version: strings.TrimSpace(item.Version)})
		}
		sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		return out, nil
	}
	var versions map[string]string
	if err := json.Unmarshal(raw, &versions); err != nil {
		return nil, errors.New("currentState components must be a list or an object")
	}
	ids := make([]string, 0, len(versions))
	for id := range versions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]matrixStateComponent, 0, len(ids))
	for _, id := range ids {
		if version := strings.TrimSpace(versions[id]); version != "" {
			out = append(out, matrixStateComponent{ID: id, Version: version})
		}
	}
	return out, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func dedupeStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------------------
// installation state
// ---------------------------------------------------------------------------

// ClusterInstallation is one cluster's explicit per-component state.
type ClusterInstallation struct {
	ID                string                        `json:"id"`
	KubernetesVersion string                        `json:"kubernetesVersion,omitempty"`
	State             string                        `json:"state"`
	Reachable         *bool                         `json:"reachable,omitempty"`
	Source            string                        `json:"source"`
	Components        []MatrixComponentInstallation `json:"components"`
}

// MatrixComponentInstallation is one component's state on one cluster.
type MatrixComponentInstallation struct {
	ID      string `json:"id"`
	State   string `json:"state"`
	Version string `json:"version,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// MatrixInstallation is the derived per-cluster installation report.
type MatrixInstallation struct {
	Provenance string                `json:"provenance"`
	ObservedAt string                `json:"observedAt,omitempty"`
	Clusters   []ClusterInstallation `json:"clusters"`
}

// DeriveInstallation computes explicit installation state. The inventory wins
// over the dataset's embedded current state. `absent` is emitted only from
// positive evidence, `out-of-scope` only from an explicit scope entry and
// `unreachable` only from an explicit reachable=false.
func DeriveInstallation(ds *MatrixDataset, inv *model.Inventory, scope *MatrixScope) *MatrixInstallation {
	effective := scope
	if effective == nil {
		effective = ds.EmbeddedScope()
	}
	if inv != nil {
		return deriveFromInventory(ds, inv, effective)
	}
	if ds.state != nil {
		return deriveFromState(ds, ds.state, effective)
	}
	if effective != nil && len(effective.OutOfScope) > 0 {
		installation := &MatrixInstallation{Provenance: "scope-only"}
		installation.Clusters = append(installation.Clusters, outOfScopeClusters(ds, effective)...)
		return installation
	}
	return nil
}

func deriveFromState(ds *MatrixDataset, state *matrixState, scope *MatrixScope) *MatrixInstallation {
	installation := &MatrixInstallation{Provenance: "dataset-current-state", ObservedAt: state.ObservedAt}
	inScope := map[string]bool{}
	if scope != nil {
		for _, id := range scope.InScope {
			inScope[id] = true
		}
	}
	excluded := map[string]bool{}
	for _, item := range scopeOutOfScope(scope) {
		excluded[item.ID] = true
	}
	for _, cluster := range state.Clusters {
		if len(inScope) > 0 && !inScope[cluster.ID] {
			continue
		}
		if excluded[cluster.ID] {
			continue
		}
		installed := map[string]string{}
		for _, component := range cluster.Components {
			id := resolveStateComponent(component.ID)
			if id == "" || id == "vcluster" {
				continue
			}
			installed[id] = component.Version
		}
		absent := map[string]bool{}
		for _, id := range cluster.Absent {
			if resolved := resolveStateComponent(id); resolved != "" && resolved != "vcluster" {
				absent[resolved] = true
			}
		}
		clusterState := "in-scope"
		if !cluster.Reachable {
			clusterState = "unreachable"
		}
		reachable := cluster.Reachable
		entry := ClusterInstallation{
			ID:                cluster.ID,
			KubernetesVersion: cluster.KubernetesVersion,
			State:             clusterState,
			Reachable:         &reachable,
			Source:            "dataset-current-state",
		}
		for _, component := range ds.Components {
			cell := MatrixComponentInstallation{ID: component.ID}
			switch {
			case !cluster.Reachable:
				cell.State, cell.Reason = InstallUnknown, "cluster-unreachable"
			case component.ID == "vcluster":
				cell.State, cell.Reason = InstallUnknown, "platform-only"
			case installed[component.ID] != "":
				cell.State, cell.Version = InstallInstalled, installed[component.ID]
			case absent[component.ID]:
				cell.State, cell.Reason = InstallAbsent, "reported-not-detected"
			default:
				cell.State, cell.Reason = InstallUnknown, "not-observed"
			}
			entry.Components = append(entry.Components, cell)
		}
		installation.Clusters = append(installation.Clusters, entry)
	}
	installation.Clusters = append(installation.Clusters, outOfScopeClusters(ds, scope)...)
	sort.SliceStable(installation.Clusters, func(i, j int) bool { return installation.Clusters[i].ID < installation.Clusters[j].ID })
	return installation
}

func deriveFromInventory(ds *MatrixDataset, inv *model.Inventory, scope *MatrixScope) *MatrixInstallation {
	installation := &MatrixInstallation{Provenance: "inventory"}
	if !inv.FinishedAt.IsZero() {
		installation.ObservedAt = inv.FinishedAt.UTC().Format(time.RFC3339)
	}
	inScope := map[string]bool{}
	if scope != nil {
		for _, id := range scope.InScope {
			inScope[id] = true
		}
	}
	excluded := map[string]bool{}
	for _, item := range scopeOutOfScope(scope) {
		excluded[item.ID] = true
	}
	// Coverage records are not cluster-scoped; only a single-cluster inventory
	// can prove absence.
	coverageAttributable := len(inv.Clusters) == 1
	for _, cluster := range inv.Clusters {
		if len(inScope) > 0 && !inScope[cluster.ID] {
			continue
		}
		if excluded[cluster.ID] {
			continue
		}
		reachable := cluster.Reachable == nil || *cluster.Reachable
		entry := ClusterInstallation{
			ID:                cluster.ID,
			KubernetesVersion: cluster.KubernetesVersion,
			State:             "in-scope",
			Reachable:         &reachable,
			Source:            "inventory",
		}
		if !reachable {
			entry.State = "unreachable"
		}
		identities := inventoryIdentities(inv, cluster.ID)
		workloads, crds := false, false
		if coverageAttributable && reachable {
			workloads, crds = completeCoverage(inv)
		}
		observedGroups := crdGroups(inv)
		for _, component := range ds.Components {
			cell := MatrixComponentInstallation{ID: component.ID}
			if !reachable {
				cell.State, cell.Reason = InstallUnknown, "cluster-unreachable"
				entry.Components = append(entry.Components, cell)
				continue
			}
			if component.ID == "vcluster" && identities.platform != "" {
				cell.State, cell.Reason = InstallUnknown, "platform-only"
				entry.Components = append(entry.Components, cell)
				continue
			}
			if identities.ambiguous[component.ID] {
				cell.State, cell.Reason = InstallUnknown, "identity-ambiguous"
				entry.Components = append(entry.Components, cell)
				continue
			}
			if version, ok := identities.installed[component.ID]; ok {
				cell.State, cell.Version = InstallInstalled, version
				entry.Components = append(entry.Components, cell)
				continue
			}
			if identities.observed[component.ID] {
				cell.State, cell.Reason = InstallUnknown, "version-unavailable"
				entry.Components = append(entry.Components, cell)
				continue
			}
			project, registered := projects.Lookup(component.ID)
			if !registered || !project.Expected {
				cell.State, cell.Reason = InstallUnknown, "unregistered-project"
				entry.Components = append(entry.Components, cell)
				continue
			}
			if groupMatches(project.APIGroups, observedGroups) {
				cell.State, cell.Reason = InstallUnknown, "workloads-not-observed"
				entry.Components = append(entry.Components, cell)
				continue
			}
			if workloads && (crds || len(project.APIGroups) == 0) {
				cell.State, cell.Reason = InstallAbsent, "not-observed-with-complete-coverage"
				entry.Components = append(entry.Components, cell)
				continue
			}
			cell.State, cell.Reason = InstallUnknown, "coverage-incomplete"
			entry.Components = append(entry.Components, cell)
		}
		installation.Clusters = append(installation.Clusters, entry)
	}
	for _, item := range scopeOutOfScope(scope) {
		found := false
		for _, cluster := range installation.Clusters {
			if cluster.ID == item.ID {
				found = true
			}
		}
		if !found {
			installation.Clusters = append(installation.Clusters, singleOutOfScope(ds, item, "inventory"))
		}
	}
	sort.SliceStable(installation.Clusters, func(i, j int) bool { return installation.Clusters[i].ID < installation.Clusters[j].ID })
	return installation
}

type inventoryIdentity struct {
	installed map[string]string
	observed  map[string]bool
	ambiguous map[string]bool
	platform  string
}

// inventoryIdentities resolves saved components to registry projects. Repeated
// installs are order-independent: conflicting versions for one project are
// ambiguous rather than last-wins (multiple controllers sharing a version are
// one installation). A declared project that disagrees with its observed image
// origins is ambiguous too.
func inventoryIdentities(inv *model.Inventory, clusterID string) inventoryIdentity {
	out := inventoryIdentity{installed: map[string]string{}, observed: map[string]bool{}, ambiguous: map[string]bool{}}
	versions := map[string]map[string]bool{}
	mark := func(projectID, version string) {
		out.observed[projectID] = true
		if versions[projectID] == nil {
			versions[projectID] = map[string]bool{}
		}
		if version != "" {
			versions[projectID][version] = true
		}
	}
	for _, component := range inv.Components {
		if component.ClusterID != clusterID {
			continue
		}
		project, resolved := projects.Lookup(component.Project)
		imageProjects := map[string]bool{}
		for _, image := range component.Images {
			if matched, found := projects.MatchRepository(image.Repository); found {
				imageProjects[matched.ID] = true
			}
		}
		if resolved && len(imageProjects) > 0 && !imageProjects[project.ID] {
			out.ambiguous[project.ID] = true
			out.observed[project.ID] = true
			for id := range imageProjects {
				out.ambiguous[id] = true
				out.observed[id] = true
			}
			continue
		}
		if !resolved {
			if len(imageProjects) > 1 {
				for id := range imageProjects {
					out.ambiguous[id] = true
					out.observed[id] = true
				}
				continue
			}
			for id := range imageProjects {
				project, resolved = projects.Lookup(id)
			}
		}
		if !resolved {
			if matched, found := projects.MatchProject(component.Name); found {
				project, resolved = matched, true
			}
		}
		if !resolved {
			continue
		}
		version := inventoryVersion(component)
		if project.ID == "vcluster" {
			out.platform = version
			continue
		}
		mark(project.ID, version)
	}
	for projectID, set := range versions {
		// Multiple controllers of one product share a version; only conflicting
		// versions (or a declared/image-origin conflict above) are ambiguous.
		if len(set) > 1 {
			out.ambiguous[projectID] = true
			continue
		}
		for version := range set {
			out.installed[projectID] = version
		}
	}
	return out
}

// inventoryVersion prefers a deployed app version, then a deployed chart
// version, then the running image tag. Values are copied verbatim.
func inventoryVersion(component model.Component) string {
	type preference struct {
		role, scheme string
	}
	for _, want := range []preference{{"deployed", "app"}, {"deployed", "chart"}, {"deployed", ""}, {"desired", "chart"}, {"desired", "app"}} {
		for _, version := range component.Versions {
			if version.Raw == "" {
				continue
			}
			if want.role != "" && version.Role != want.role {
				continue
			}
			if want.scheme != "" && version.Scheme != want.scheme {
				continue
			}
			return strings.TrimSpace(version.Raw)
		}
	}
	for _, image := range component.Images {
		if image.Role == "running" && strings.TrimSpace(image.Tag) != "" {
			return strings.TrimSpace(image.Tag)
		}
	}
	return ""
}

func completeCoverage(inv *model.Inventory) (workloads, crds bool) {
	workloadKinds := map[string]bool{"Deployment": true, "DaemonSet": true, "StatefulSet": true, "Pod": true}
	workloadSeen, workloadBlocked := false, false
	crdSeen, crdBlocked := false, false
	for _, coverage := range inv.Coverage {
		for _, kind := range coverage.Kinds {
			if workloadKinds[kind] {
				if coverage.State == "COMPLETE" {
					workloadSeen = true
				} else {
					workloadBlocked = true
				}
			}
			if kind == "CustomResourceDefinition" {
				if coverage.State == "COMPLETE" {
					crdSeen = true
				} else {
					crdBlocked = true
				}
			}
		}
	}
	return workloadSeen && !workloadBlocked, crdSeen && !crdBlocked
}

func crdGroups(inv *model.Inventory) map[string]bool {
	out := map[string]bool{}
	for _, resource := range inv.Resources {
		if resource.Ref.Kind != "CustomResourceDefinition" {
			continue
		}
		name := strings.ToLower(resource.Ref.Name)
		if index := strings.Index(name, "."); index > 0 && index < len(name)-1 {
			out[name[index+1:]] = true
			continue
		}
		if group := strings.ToLower(resource.Ref.Group); group != "" && group != "apiextensions.k8s.io" {
			out[group] = true
		}
	}
	return out
}

func groupMatches(groups []string, observed map[string]bool) bool {
	for _, group := range groups {
		if observed[strings.ToLower(group)] {
			return true
		}
	}
	return false
}

func resolveStateComponent(id string) string {
	if project, ok := projects.Lookup(id); ok {
		return project.ID
	}
	return ""
}

func scopeOutOfScope(scope *MatrixScope) []MatrixScopeExclusion {
	if scope == nil {
		return nil
	}
	return scope.OutOfScope
}

func outOfScopeClusters(ds *MatrixDataset, scope *MatrixScope) []ClusterInstallation {
	var out []ClusterInstallation
	for _, item := range scopeOutOfScope(scope) {
		out = append(out, singleOutOfScope(ds, item, "scope"))
	}
	return out
}

func singleOutOfScope(ds *MatrixDataset, item MatrixScopeExclusion, source string) ClusterInstallation {
	entry := ClusterInstallation{ID: item.ID, State: "out-of-scope", Source: source}
	for _, component := range ds.Components {
		entry.Components = append(entry.Components, MatrixComponentInstallation{ID: component.ID, State: InstallOutOfScope, Reason: item.Reason})
	}
	return entry
}

// MatrixEvidenceAge returns the evidence age in days and whether it exceeds the
// tracker's freshness threshold. It is a reviewer signal, never a status change.
func MatrixEvidenceAge(verifiedAt string, now time.Time) (int, bool) {
	date, err := time.Parse("2006-01-02", verifiedAt)
	if err != nil {
		return 0, false
	}
	age := int(now.Sub(date).Hours() / 24)
	return age, age > MatrixStaleDays
}
