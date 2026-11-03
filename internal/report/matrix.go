package report

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/wussh/kubecompact/internal/compatmatrix"
)

// MatrixBuildOptions carries validated inputs and product-only extensions.
type MatrixBuildOptions struct {
	Format        string
	Extended      bool
	DatasetCommit string
	Scope         *compatmatrix.MatrixScope
	Installation  *compatmatrix.MatrixInstallation
	Now           time.Time
}

type matrixDocument struct {
	SchemaVersion string                          `json:"schemaVersion"`
	GeneratedAt   string                          `json:"generatedAt"`
	Kubernetes    []string                        `json:"kubernetes"`
	K3sReleases   []compatmatrix.MatrixK3sRelease `json:"k3sReleases"`
	Components    []compatmatrix.MatrixComponent  `json:"components"`
	CurrentState  json.RawMessage                 `json:"currentState,omitempty"`
	Extensions    *matrixExtensions               `json:"extensions,omitempty"`
}

type matrixExtensions struct {
	DatasetDigest      string                           `json:"datasetDigest"`
	DatasetCommit      string                           `json:"datasetCommit,omitempty"`
	DatasetProvenance  string                           `json:"datasetProvenance"`
	StaleThresholdDays int                              `json:"staleThresholdDays"`
	Stale              []matrixStaleCell                `json:"stale,omitempty"`
	Installation       *compatmatrix.MatrixInstallation `json:"installation,omitempty"`
	Blockers           []matrixTargetImpact             `json:"blockers,omitempty"`
}

type matrixStaleCell struct {
	Component  string `json:"component"`
	Kubernetes string `json:"kubernetes"`
	VerifiedAt string `json:"verifiedAt"`
	AgeDays    int    `json:"ageDays"`
}

type matrixTargetImpact struct {
	Kubernetes string            `json:"kubernetes"`
	Blockers   []matrixImpactRow `json:"blockers,omitempty"`
	Unverified []matrixImpactRow `json:"unverified,omitempty"`
}

type matrixImpactRow struct {
	Component       string                `json:"component"`
	Category        string                `json:"category"`
	Status          string                `json:"status"`
	RequiredVersion string                `json:"requiredVersion"`
	Source          string                `json:"source"`
	ClusterBlocking bool                  `json:"clusterBlocking"`
	Impact          []matrixClusterImpact `json:"impact"`
}

type matrixClusterImpact struct {
	Cluster string `json:"cluster"`
	State   string `json:"state"`
	Version string `json:"version,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// WriteMatrix renders the pinned dataset. The default JSON output is the
// tracker document; --extended adds product-only fields under "extensions".
func WriteMatrix(w io.Writer, ds *compatmatrix.MatrixDataset, opts MatrixBuildOptions) error {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	switch opts.Format {
	case "json":
		return writeMatrixJSON(w, ds, opts)
	case "csv":
		return writeMatrixCSV(w, ds, opts)
	case "markdown", "md":
		return writeMatrixMarkdown(w, ds, opts)
	default:
		return errors.New("unsupported matrix format")
	}
}

func (opts MatrixBuildOptions) provenance() string {
	if opts.Installation != nil {
		return opts.Installation.Provenance
	}
	return "tracker"
}

func (opts MatrixBuildOptions) staleCells(ds *compatmatrix.MatrixDataset) []matrixStaleCell {
	var out []matrixStaleCell
	for _, component := range ds.Components {
		for _, minor := range ds.Kubernetes {
			cell := component.Kubernetes[minor]
			if age, stale := compatmatrix.MatrixEvidenceAge(cell.VerifiedAt, opts.Now); stale {
				out = append(out, matrixStaleCell{Component: component.ID, Kubernetes: minor, VerifiedAt: cell.VerifiedAt, AgeDays: age})
			}
		}
	}
	return out
}

func writeMatrixJSON(w io.Writer, ds *compatmatrix.MatrixDataset, opts MatrixBuildOptions) error {
	doc := matrixDocument{
		SchemaVersion: ds.SchemaVersion,
		GeneratedAt:   ds.GeneratedAt,
		Kubernetes:    ds.Kubernetes,
		K3sReleases:   ds.K3sReleases,
		Components:    ds.Components,
	}
	switch {
	case opts.Installation == nil:
		doc.CurrentState = ds.CurrentState
	case opts.Installation.Provenance == "dataset-current-state" && len(ds.CurrentState) > 0:
		// No inventory: preserve the dataset's settled current state verbatim.
		// An explicit --scope still drives the derived installation/impact output.
		doc.CurrentState = ds.CurrentState
	default:
		derived, err := synthesizeCurrentState(ds, opts.Installation, opts.Scope)
		if err != nil {
			return err
		}
		doc.CurrentState = derived
	}
	if opts.Extended {
		doc.Extensions = &matrixExtensions{
			DatasetDigest:      ds.Digest,
			DatasetCommit:      opts.DatasetCommit,
			DatasetProvenance:  opts.provenance(),
			StaleThresholdDays: compatmatrix.MatrixStaleDays,
			Stale:              opts.staleCells(ds),
			Installation:       opts.Installation,
			Blockers:           matrixBlockers(ds, opts.Installation),
		}
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(doc)
}

func synthesizeCurrentState(ds *compatmatrix.MatrixDataset, installation *compatmatrix.MatrixInstallation, scope *compatmatrix.MatrixScope) (json.RawMessage, error) {
	type clusterOut struct {
		KubernetesVersion string            `json:"kubernetesVersion,omitempty"`
		Reachable         bool              `json:"reachable"`
		Components        map[string]string `json:"components"`
		Absent            []string          `json:"absent,omitempty"`
	}
	doc := struct {
		SchemaVersion string                              `json:"schemaVersion"`
		ObservedAt    string                              `json:"observedAt,omitempty"`
		Clusters      map[string]clusterOut               `json:"clusters"`
		InScope       []string                            `json:"inScope,omitempty"`
		OutOfScope    []compatmatrix.MatrixScopeExclusion `json:"outOfScope,omitempty"`
	}{
		SchemaVersion: compatmatrix.MatrixCurrentStateSchemaVersion,
		ObservedAt:    installation.ObservedAt,
		Clusters:      map[string]clusterOut{},
	}
	if scope != nil {
		doc.InScope = scope.InScope
		doc.OutOfScope = scope.OutOfScope
	}
	for _, cluster := range installation.Clusters {
		if cluster.State == "out-of-scope" {
			if scope == nil {
				reason := "out of scope"
				if len(cluster.Components) > 0 && cluster.Components[0].Reason != "" {
					reason = cluster.Components[0].Reason
				}
				doc.OutOfScope = append(doc.OutOfScope, compatmatrix.MatrixScopeExclusion{ID: cluster.ID, Reason: reason})
			}
			continue
		}
		out := clusterOut{KubernetesVersion: cluster.KubernetesVersion, Reachable: cluster.Reachable != nil && *cluster.Reachable, Components: map[string]string{}}
		for _, component := range cluster.Components {
			switch component.State {
			case compatmatrix.InstallInstalled:
				out.Components[component.ID] = component.Version
			case compatmatrix.InstallAbsent:
				out.Absent = append(out.Absent, component.ID)
			}
		}
		if len(out.Absent) == 0 {
			out.Absent = nil
		}
		doc.Clusters[cluster.ID] = out
	}
	return json.Marshal(doc)
}

func writeMatrixCSV(w io.Writer, ds *compatmatrix.MatrixDataset, opts MatrixBuildOptions) error {
	cw := csv.NewWriter(w)
	header := []string{"component", "category", "kubernetes", "status", "requiredVersion", "source", "verifiedAt", "notes", "currentVersions"}
	if opts.Extended {
		header = append(header, "installationState", "installationDetail", "claimType", "confidence", "evidenceId", "stale", "adjudication", "searchedUrls")
	}
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, component := range ds.Components {
		for _, minor := range ds.Kubernetes {
			cell := component.Kubernetes[minor]
			notes := cell.Notes
			if notes == "" {
				notes = component.Notes
			}
			fields := []string{component.ID, component.Category, minor, cell.Status, cell.RequiredVersion, cell.Source, cell.VerifiedAt, notes, ""}
			if opts.Installation != nil {
				fields[8] = installationVersionSummary(opts.Installation, component.ID, ";")
			}
			if opts.Extended {
				state, detail := installationStates(opts.Installation, component.ID)
				_, stale := compatmatrix.MatrixEvidenceAge(cell.VerifiedAt, opts.Now)
				fields = append(fields, state, detail, "", "", "", fmt.Sprint(stale), "", "")
			}
			for i := range fields {
				fields[i] = csvCell(fields[i])
			}
			if err := cw.Write(fields); err != nil {
				return err
			}
		}
	}
	cw.Flush()
	return cw.Error()
}

func installationVersionSummary(installation *compatmatrix.MatrixInstallation, componentID, separator string) string {
	if installation == nil {
		return ""
	}
	var parts []string
	for _, cluster := range installation.Clusters {
		for _, component := range cluster.Components {
			if component.ID == componentID && component.State == compatmatrix.InstallInstalled {
				parts = append(parts, cluster.ID+"="+component.Version)
			}
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, separator)
}

func installationStates(installation *compatmatrix.MatrixInstallation, componentID string) (string, string) {
	if installation == nil {
		return "", ""
	}
	var states, details []string
	for _, cluster := range installation.Clusters {
		for _, component := range cluster.Components {
			if component.ID != componentID {
				continue
			}
			states = append(states, cluster.ID+"="+component.State)
			switch {
			case component.Version != "":
				details = append(details, cluster.ID+"="+component.Version)
			case component.Reason != "":
				details = append(details, cluster.ID+"="+component.Reason)
			}
		}
	}
	return strings.Join(states, "; "), strings.Join(details, "; ")
}

func matrixBlockers(ds *compatmatrix.MatrixDataset, installation *compatmatrix.MatrixInstallation) []matrixTargetImpact {
	if installation == nil {
		return nil
	}
	var out []matrixTargetImpact
	for _, minor := range ds.Kubernetes {
		impact := matrixTargetImpact{Kubernetes: minor}
		for _, component := range ds.Components {
			cell := component.Kubernetes[minor]
			row := matrixImpactRow{
				Component:       component.ID,
				Category:        component.Category,
				Status:          cell.Status,
				RequiredVersion: cell.RequiredVersion,
				Source:          cell.Source,
				ClusterBlocking: clusterBlockingState(installation, component.ID),
				Impact:          clusterImpacts(installation, component.ID),
			}
			switch cell.Status {
			case "unsupported":
				impact.Blockers = append(impact.Blockers, row)
			case "unknown":
				impact.Unverified = append(impact.Unverified, row)
			}
		}
		out = append(out, impact)
	}
	return out
}

// clusterBlockingState reports whether at least one in-scope cluster has the
// component installed. An absent component is never a cluster blocker.
func clusterBlockingState(installation *compatmatrix.MatrixInstallation, componentID string) bool {
	for _, cluster := range installation.Clusters {
		if cluster.State != "in-scope" {
			continue
		}
		for _, component := range cluster.Components {
			if component.ID == componentID && component.State == compatmatrix.InstallInstalled {
				return true
			}
		}
	}
	return false
}

func clusterImpacts(installation *compatmatrix.MatrixInstallation, componentID string) []matrixClusterImpact {
	var out []matrixClusterImpact
	for _, cluster := range installation.Clusters {
		for _, component := range cluster.Components {
			if component.ID != componentID {
				continue
			}
			out = append(out, matrixClusterImpact{Cluster: cluster.ID, State: component.State, Version: component.Version, Reason: component.Reason})
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// markdown
// ---------------------------------------------------------------------------

func matrixCellText(value string) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(value, "|", "\\|"), "\n", " "))
}

func matrixStatusText(status string) string {
	if status == "unknown" {
		return "UNKNOWN"
	}
	return status
}

func writeMatrixMarkdown(w io.Writer, ds *compatmatrix.MatrixDataset, opts MatrixBuildOptions) error {
	var b strings.Builder
	b.WriteString("# Kubernetes Compatibility Matrix\n\n")
	fmt.Fprintf(&b, "Generated: %s\n\n", ds.GeneratedAt)
	fmt.Fprintf(&b, "Dataset: `compat-matrix/v1` (sha256 `%s`, provenance: %s). Regenerate with `kubecompat matrix build --dataset <path>`.\n\n", ds.Digest, opts.provenance())
	b.WriteString("`UNKNOWN` means no official statement was found for that Kubernetes minor. It is never treated as supported.\n\n")
	if stale := opts.staleCells(ds); len(stale) > 0 {
		var parts []string
		for _, cell := range stale {
			parts = append(parts, fmt.Sprintf("%s %s (%d days)", cell.Component, cell.Kubernetes, cell.AgeDays))
		}
		fmt.Fprintf(&b, "Stale evidence (verified more than %d days ago; reviewer signal, never a status change): %s.\n\n", compatmatrix.MatrixStaleDays, strings.Join(parts, ", "))
	}

	b.WriteString("## K3s releases\n\n")
	if len(ds.K3sReleases) > 0 {
		b.WriteString("| Kubernetes | K3s release | Source | Verified |\n|---|---|---|---|\n")
		for _, release := range ds.K3sReleases {
			fmt.Fprintf(&b, "| %s | %s | [%s](%s) | %s |\n", release.Kubernetes, matrixCellText(release.Release), matrixCellText(release.Source), release.Source, release.VerifiedAt)
		}
	} else {
		b.WriteString("_No k3s release is recorded yet._\n")
	}
	b.WriteString("\n## Components\n\n")
	for _, component := range ds.Components {
		fmt.Fprintf(&b, "### %s (`%s`)\n\n", component.Name, component.ID)
		fmt.Fprintf(&b, "Category: %s · Official docs: [%s](%s)\n", matrixCellText(component.Category), matrixCellText(component.OfficialDocs), component.OfficialDocs)
		if component.Notes != "" {
			fmt.Fprintf(&b, "\nNotes: %s\n", matrixCellText(component.Notes))
		}
		b.WriteString("\n| Kubernetes | Status | Required version | Source | Verified | Currently running |\n|---|---|---|---|---|---|\n")
		for _, minor := range ds.Kubernetes {
			cell := component.Kubernetes[minor]
			fmt.Fprintf(&b, "| %s | %s | %s | [link](%s) | %s | %s |\n",
				minor, matrixStatusText(cell.Status), matrixCellText(cell.RequiredVersion), cell.Source, cell.VerifiedAt,
				matrixCellText(matrixCurrentSummary(opts.Installation, component.ID)))
		}
		b.WriteString("\n")
		var notes []string
		for _, minor := range ds.Kubernetes {
			if note := component.Kubernetes[minor].Notes; note != "" {
				notes = append(notes, fmt.Sprintf("- %s: %s", minor, matrixCellText(note)))
			}
		}
		if len(notes) > 0 {
			b.WriteString("Per-minor notes:\n\n")
			b.WriteString(strings.Join(notes, "\n"))
			b.WriteString("\n\n")
		}
	}
	if opts.Installation != nil {
		writeMatrixStateMarkdown(&b, ds, opts)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// matrixCurrentSummary mirrors the tracker's compaction: installed versions,
// else `not detected` for a positive absence, else `UNKNOWN`.
func matrixCurrentSummary(installation *compatmatrix.MatrixInstallation, componentID string) string {
	if installation == nil {
		return "UNKNOWN"
	}
	var versions []string
	absent, unknown := false, false
	clusters := 0
	for _, cluster := range installation.Clusters {
		if cluster.State == "out-of-scope" {
			continue
		}
		clusters++
		for _, component := range cluster.Components {
			if component.ID != componentID {
				continue
			}
			switch component.State {
			case compatmatrix.InstallInstalled:
				versions = append(versions, cluster.ID+"="+component.Version)
			case compatmatrix.InstallAbsent:
				absent = true
			default:
				unknown = true
			}
		}
	}
	if len(versions) > 0 {
		sort.Strings(versions)
		return strings.Join(versions, "; ")
	}
	if clusters == 0 || !absent {
		return "UNKNOWN"
	}
	if unknown {
		return "UNKNOWN"
	}
	return "not detected"
}

func writeMatrixStateMarkdown(b *strings.Builder, ds *compatmatrix.MatrixDataset, opts MatrixBuildOptions) {
	installation := opts.Installation
	b.WriteString("## Current cluster versions\n\n")
	var clusters []compatmatrix.ClusterInstallation
	for _, cluster := range installation.Clusters {
		if cluster.State != "out-of-scope" {
			clusters = append(clusters, cluster)
		}
	}
	if len(clusters) > 0 {
		b.WriteString("| Cluster | Kubernetes |")
		for _, component := range ds.Components {
			fmt.Fprintf(b, " %s |", matrixCellText(component.ID))
		}
		b.WriteString("\n|---|---|")
		for range ds.Components {
			b.WriteString("---|")
		}
		b.WriteString("\n")
		for _, cluster := range clusters {
			version := cluster.KubernetesVersion
			if version == "" {
				version = "UNKNOWN"
			}
			fmt.Fprintf(b, "| %s | %s |", matrixCellText(cluster.ID), matrixCellText(version))
			for _, component := range ds.Components {
				fmt.Fprintf(b, " %s |", matrixCellText(matrixClusterValue(cluster, component.ID)))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	scope := opts.Scope
	if scope == nil {
		scope = ds.EmbeddedScope()
	}
	if scope != nil && len(scope.OutOfScope) > 0 {
		b.WriteString("## Out of scope\n\n")
		b.WriteString("Excluded from this tracker by owner decision; no scan or upgrade evidence is collected there.\n\n")
		for _, item := range scope.OutOfScope {
			fmt.Fprintf(b, "- `%s` — %s\n", matrixCellText(item.ID), matrixCellText(item.Reason))
		}
		b.WriteString("\n")
	}

	b.WriteString("## Compatibility blockers\n\n")
	fmt.Fprintf(b, "Ordered by next upgrade target (%s first). BLOCKER = an official statement excludes that Kubernetes minor; NEEDS VERIFICATION = no official statement found. A component that is not installed on a cluster does not block that cluster's upgrade; absent components may indicate GitOps drift.\n\n", ds.Kubernetes[0])
	for _, impact := range matrixBlockers(ds, installation) {
		fmt.Fprintf(b, "## Kubernetes %s\n\n", impact.Kubernetes)
		if len(impact.Blockers) == 0 && len(impact.Unverified) == 0 {
			fmt.Fprintf(b, "No blocker recorded — every component reports `supported` for %s.\n\n", impact.Kubernetes)
			continue
		}
		for _, row := range impact.Blockers {
			label := "unsupported"
			absent := clustersWithState(row.Impact, compatmatrix.InstallAbsent)
			if len(clustersWithState(row.Impact, compatmatrix.InstallInstalled)) == 0 && len(absent) > 0 {
				label = fmt.Sprintf("matrix blocker: not currently installed on %s; unsupported", strings.Join(absent, "/"))
			}
			fmt.Fprintf(b, "- **BLOCKER** `%s` (%s) — %s on %s; requires %s; source: %s%s\n",
				row.Component, matrixCellText(row.Category), label, impact.Kubernetes, matrixCellText(row.RequiredVersion), row.Source, impactText(row.Impact))
		}
		for _, row := range impact.Unverified {
			fmt.Fprintf(b, "- **NEEDS VERIFICATION** `%s` (%s) — no official statement for %s; source checked: %s%s\n",
				row.Component, matrixCellText(row.Category), impact.Kubernetes, row.Source, impactText(row.Impact))
		}
		b.WriteString("\n")
	}
}

func clustersWithState(impacts []matrixClusterImpact, state string) []string {
	var ids []string
	for _, impact := range impacts {
		if impact.State == state {
			ids = append(ids, impact.Cluster)
		}
	}
	sort.Strings(ids)
	return ids
}

func matrixClusterValue(cluster compatmatrix.ClusterInstallation, componentID string) string {
	for _, component := range cluster.Components {
		if component.ID != componentID {
			continue
		}
		switch component.State {
		case compatmatrix.InstallInstalled:
			return component.Version
		case compatmatrix.InstallAbsent:
			return "not detected"
		}
	}
	return "UNKNOWN"
}

// impactText states each cluster's explicit installation state. An absent
// component is explicitly not a cluster upgrade blocker.
func impactText(impacts []matrixClusterImpact) string {
	if len(impacts) == 0 {
		return ""
	}
	var parts []string
	for _, impact := range impacts {
		switch impact.State {
		case compatmatrix.InstallInstalled:
			parts = append(parts, fmt.Sprintf("%s: installed %s", impact.Cluster, impact.Version))
		case compatmatrix.InstallAbsent:
			parts = append(parts, fmt.Sprintf("%s: absent (not a cluster upgrade blocker; GitOps drift may still exist)", impact.Cluster))
		case compatmatrix.InstallOutOfScope:
			parts = append(parts, fmt.Sprintf("%s: out-of-scope", impact.Cluster))
		default:
			reason := impact.Reason
			if reason == "" {
				reason = "unobserved"
			}
			parts = append(parts, fmt.Sprintf("%s: unknown (%s)", impact.Cluster, reason))
		}
	}
	return "; impact — " + strings.Join(parts, "; ")
}
