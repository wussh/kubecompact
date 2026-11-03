package report

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/wussh/kubecompact/internal/compatmatrix"
)

func renderDataset() *compatmatrix.MatrixDataset {
	cell := func(status string) compatmatrix.MatrixCell {
		return compatmatrix.MatrixCell{
			Status: status, RequiredVersion: "v1.18",
			Source: "https://example.com/source", VerifiedAt: "2026-11-02", Notes: "note for 1.36",
		}
	}
	return &compatmatrix.MatrixDataset{
		SchemaVersion: compatmatrix.MatrixSchemaVersion,
		GeneratedAt:   "2026-11-02T00:00:00Z",
		Kubernetes:    []string{"1.36"},
		K3sReleases: []compatmatrix.MatrixK3sRelease{{
			Kubernetes: "1.36", Release: "v1.36.5+k3s1",
			Source: "https://github.com/k3s-io/k3s/releases/tag/v1.36.5%2Bk3s1", VerifiedAt: "2026-11-02",
		}},
		Components: []compatmatrix.MatrixComponent{
			{ID: "velero", Name: "Velero", Category: "backup", OfficialDocs: "https://velero.io/docs/v1.18/", Kubernetes: map[string]compatmatrix.MatrixCell{"1.36": cell("supported")}},
			{ID: "metallb", Name: "MetalLB", Category: "load-balancer", OfficialDocs: "https://metallb.io/", Kubernetes: map[string]compatmatrix.MatrixCell{"1.36": cell("unknown")}},
			{ID: "legacy", Name: "Legacy", Category: "test", OfficialDocs: "https://example.com/legacy", Kubernetes: map[string]compatmatrix.MatrixCell{"1.36": cell("unsupported")}},
		},
		Digest: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
}

func installationFixture() *compatmatrix.MatrixInstallation {
	reachable := true
	return &compatmatrix.MatrixInstallation{
		Provenance: "inventory",
		ObservedAt: "2026-11-02T09:16:37Z",
		Clusters: []compatmatrix.ClusterInstallation{
			{
				ID: "dev", KubernetesVersion: "1.33.13+k3s2", State: "in-scope", Reachable: &reachable, Source: "inventory",
				Components: []compatmatrix.MatrixComponentInstallation{
					{ID: "velero", State: compatmatrix.InstallInstalled, Version: "1.17.1"},
					{ID: "metallb", State: compatmatrix.InstallAbsent, Reason: "not-observed-with-complete-coverage"},
					{ID: "legacy", State: compatmatrix.InstallAbsent, Reason: "not-observed-with-complete-coverage"},
				},
			},
			{ID: "gpu", State: "out-of-scope", Components: []compatmatrix.MatrixComponentInstallation{
				{ID: "velero", State: compatmatrix.InstallOutOfScope, Reason: "prod"},
				{ID: "metallb", State: compatmatrix.InstallOutOfScope, Reason: "prod"},
				{ID: "legacy", State: compatmatrix.InstallOutOfScope, Reason: "prod"},
			}},
		},
	}
}

var fixedNow = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

func render(t *testing.T, ds *compatmatrix.MatrixDataset, opts MatrixBuildOptions) string {
	t.Helper()
	var buffer bytes.Buffer
	if err := WriteMatrix(&buffer, ds, opts); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buffer.String()
}

func TestMatrixCSVDefaultHeader(t *testing.T) {
	out := render(t, renderDataset(), MatrixBuildOptions{Format: "csv", Now: fixedNow})
	records, err := csv.NewReader(strings.NewReader(out)).ReadAll()
	if err != nil {
		t.Fatalf("csv: %v", err)
	}
	want := []string{"component", "category", "kubernetes", "status", "requiredVersion", "source", "verifiedAt", "notes", "currentVersions"}
	if strings.Join(records[0], ",") != strings.Join(want, ",") {
		t.Fatalf("header: %v", records[0])
	}
	if len(records) != 4 {
		t.Fatalf("expected header + 3 rows, got %d", len(records))
	}
	for _, record := range records[1:] {
		if len(record) != 9 {
			t.Fatalf("default row must have nine columns: %v", record)
		}
		if record[0] == "metallb" && record[3] != "unknown" {
			t.Fatalf("unknown must never be promoted: %v", record)
		}
	}
	if got := records[1][8]; got != "" {
		t.Fatalf("no installation must leave currentVersions empty: %q", got)
	}
}

func TestMatrixCSVExtendedHeaderAndInstallation(t *testing.T) {
	opts := MatrixBuildOptions{Format: "csv", Extended: true, Now: fixedNow, Installation: installationFixture()}
	out := render(t, renderDataset(), opts)
	records, err := csv.NewReader(strings.NewReader(out)).ReadAll()
	if err != nil {
		t.Fatalf("csv: %v", err)
	}
	want := "component,category,kubernetes,status,requiredVersion,source,verifiedAt,notes,currentVersions,installationState,installationDetail,claimType,confidence,evidenceId,stale,adjudication,searchedUrls"
	if strings.Join(records[0], ",") != want {
		t.Fatalf("extended header: %v", records[0])
	}
	if records[1][0] != "velero" || records[1][8] != "dev=1.17.1" {
		t.Fatalf("velero row: %v", records[1])
	}
	if !strings.Contains(records[1][9], "gpu=out-of-scope") || !strings.Contains(records[1][9], "dev=installed") {
		t.Fatalf("installation state: %v", records[1])
	}
	if records[2][0] != "metallb" || !strings.Contains(records[2][9], "dev=absent") {
		t.Fatalf("absent detail: %v", records[2])
	}
	for _, record := range records[1:] {
		if record[11] != "" || record[12] != "" || record[13] != "" || record[16] != "" {
			t.Fatalf("pinned rows must not fabricate extension claims: %v", record)
		}
	}
}

func TestMatrixMarkdownLayout(t *testing.T) {
	scope := &compatmatrix.MatrixScope{InScope: []string{"dev"}, OutOfScope: []compatmatrix.MatrixScopeExclusion{{ID: "gpu", Reason: "production cluster"}}}
	opts := MatrixBuildOptions{Format: "markdown", Now: fixedNow, Scope: scope, Installation: installationFixture()}
	out := render(t, renderDataset(), opts)
	for _, want := range []string{
		"# Kubernetes Compatibility Matrix",
		"## K3s releases",
		"| Kubernetes | K3s release | Source | Verified |",
		"### Velero (`velero`)",
		"| Kubernetes | Status | Required version | Source | Verified | Currently running |",
		"Per-minor notes:",
		"- 1.36: note for 1.36",
		"## Current cluster versions",
		"## Out of scope",
		"`gpu` — production cluster",
		"## Compatibility blockers",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("markdown missing %q\n%s", want, out)
		}
	}
	if !strings.Contains(out, "| 1.36 | UNKNOWN |") {
		t.Fatalf("unknown status must render UNKNOWN: %s", out)
	}
	if !strings.Contains(out, "dev=1.17.1") {
		t.Fatalf("installed version missing: %s", out)
	}
	if strings.Contains(out, "## Out of scope") && !strings.Contains(out, "| Cluster | Kubernetes |") {
		t.Fatalf("current cluster table missing: %s", out)
	}
}

func TestMarkdownAbsentIsNotAClusterBlocker(t *testing.T) {
	opts := MatrixBuildOptions{Format: "markdown", Now: fixedNow, Installation: installationFixture()}
	out := render(t, renderDataset(), opts)
	if !strings.Contains(out, "matrix blocker: not currently installed on dev; unsupported") {
		t.Fatalf("absent blocker wording missing: %s", out)
	}
	if !strings.Contains(out, "dev: absent (not a cluster upgrade blocker") {
		t.Fatalf("absent impact missing: %s", out)
	}
	if strings.Contains(out, "gpu: installed") {
		t.Fatalf("out-of-scope cluster reported as installed: %s", out)
	}
	if !strings.Contains(out, "gpu: out-of-scope") {
		t.Fatalf("out-of-scope impact missing: %s", out)
	}
}

func TestMatrixJSONDefaultShape(t *testing.T) {
	out := render(t, renderDataset(), MatrixBuildOptions{Format: "json", Now: fixedNow})
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("json: %v", err)
	}
	if doc["schemaVersion"] != compatmatrix.MatrixSchemaVersion {
		t.Fatalf("schema: %v", doc["schemaVersion"])
	}
	if _, exists := doc["extensions"]; exists {
		t.Fatal("default output must not carry product-only fields")
	}
	if _, exists := doc["currentState"]; exists {
		t.Fatal("no installation must not invent currentState")
	}
	components := doc["components"].([]any)
	metallb := components[1].(map[string]any)
	cell := metallb["kubernetes"].(map[string]any)["1.36"].(map[string]any)
	if cell["status"] != "unknown" {
		t.Fatalf("unknown promoted: %v", cell)
	}
}

func TestMatrixJSONExtended(t *testing.T) {
	opts := MatrixBuildOptions{Format: "json", Extended: true, Now: fixedNow, DatasetCommit: "c2d562b", Installation: installationFixture()}
	out := render(t, renderDataset(), opts)
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("json: %v", err)
	}
	extensions := doc["extensions"].(map[string]any)
	if extensions["datasetDigest"] != renderDataset().Digest || extensions["datasetCommit"] != "c2d562b" {
		t.Fatalf("provenance: %v", extensions)
	}
	state, exists := doc["currentState"]
	if !exists {
		t.Fatal("extended output must carry the derived currentState")
	}
	clusters := state.(map[string]any)["clusters"].(map[string]any)
	dev := clusters["dev"].(map[string]any)
	if dev["components"].(map[string]any)["velero"] != "1.17.1" {
		t.Fatalf("derived current state: %v", dev)
	}
	blockers := extensions["blockers"].([]any)
	first := blockers[0].(map[string]any)
	rows := first["blockers"].([]any)
	row := rows[0].(map[string]any)
	if row["clusterBlocking"] != false {
		t.Fatalf("absent component must not be cluster-blocking: %v", row)
	}
}

func TestMatrixJSONCopiesEmbeddedCurrentState(t *testing.T) {
	ds := renderDataset()
	ds.CurrentState = json.RawMessage(`{"schemaVersion":"compat-current-state/v1","clusters":{"dev":{"reachable":true}}}`)
	out := render(t, ds, MatrixBuildOptions{Format: "json", Now: fixedNow})
	if !strings.Contains(out, `"currentState": {`) || !strings.Contains(out, `"clusters"`) {
		t.Fatalf("embedded currentState not copied: %s", out)
	}
}

func TestMatrixStaleSignal(t *testing.T) {
	ds := renderDataset()
	cell := ds.Components[0].Kubernetes["1.36"]
	cell.VerifiedAt = "2026-01-01"
	ds.Components[0].Kubernetes["1.36"] = cell
	now := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	out := render(t, ds, MatrixBuildOptions{Format: "markdown", Extended: true, Now: now})
	if !strings.Contains(out, "Stale evidence") || !strings.Contains(out, "velero 1.36") {
		t.Fatalf("stale signal missing: %s", out)
	}
	jsonOut := render(t, ds, MatrixBuildOptions{Format: "json", Extended: true, Now: now})
	if !strings.Contains(jsonOut, `"stale"`) || !strings.Contains(jsonOut, `"component": "velero"`) {
		t.Fatalf("stale extension missing: %s", jsonOut)
	}
	csvOut := render(t, ds, MatrixBuildOptions{Format: "csv", Extended: true, Now: now})
	records, err := csv.NewReader(strings.NewReader(csvOut)).ReadAll()
	if err != nil {
		t.Fatalf("csv: %v", err)
	}
	staleIndex := -1
	for i, header := range records[0] {
		if header == "stale" {
			staleIndex = i
		}
	}
	if staleIndex < 0 {
		t.Fatalf("missing stale column: %v", records[0])
	}
	for _, record := range records[1:] {
		want := "true"
		if record[0] != "velero" {
			want = "false"
		}
		if record[staleIndex] != want {
			t.Fatalf("%s stale column = %q, want %q", record[0], record[staleIndex], want)
		}
	}
	if !strings.Contains(render(t, ds, MatrixBuildOptions{Format: "csv", Now: now}), "2026-01-01") {
		t.Fatal("verifiedAt must be copied verbatim")
	}
}

func TestMarkdownWithoutInstallationReportsUnknownNotAbsent(t *testing.T) {
	out := render(t, renderDataset(), MatrixBuildOptions{Format: "markdown", Now: fixedNow})
	if strings.Contains(out, "not detected") {
		t.Fatalf("zero evidence must not be reported as positive absence: %s", out)
	}
	if !strings.Contains(out, "| 1.36 | supported | v1.18 | [link]") || !strings.Contains(out, "UNKNOWN") {
		t.Fatalf("expected UNKNOWN current state: %s", out)
	}
}
