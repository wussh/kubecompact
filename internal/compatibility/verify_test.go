package compatibility

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wussh/kubecompact/internal/compatmatrix"
)

type cellSpec struct {
	Status     string
	Source     string
	VerifiedAt string
}

func verifyDataset(t *testing.T, cells map[string]cellSpec) (*compatmatrix.MatrixDataset, []byte) {
	t.Helper()
	components := make([]map[string]any, 0, len(cells))
	for id, spec := range cells {
		verifiedAt := spec.VerifiedAt
		if verifiedAt == "" {
			verifiedAt = "2026-11-02"
		}
		components = append(components, map[string]any{
			"id": id, "name": id, "category": "test", "officialDocs": "https://example.com/" + id,
			"kubernetes": map[string]any{"1.36": map[string]any{
				"status": spec.Status, "requiredVersion": "v1", "source": spec.Source, "verifiedAt": verifiedAt,
			}},
		})
	}
	doc := map[string]any{
		"schemaVersion": compatmatrix.MatrixSchemaVersion,
		"generatedAt":   "2026-11-02T00:00:00Z",
		"kubernetes":    []string{"1.36"},
		"k3sReleases":   []map[string]any{},
		"components":    components,
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	ds, err := compatmatrix.LoadMatrixDataset(raw)
	if err != nil {
		t.Fatalf("dataset: %v", err)
	}
	return ds, raw
}

func verdicts(rep VerifyReport) map[string]string {
	out := map[string]string{}
	for _, row := range rep.Rows {
		out[row.Component] = row.Verdict
	}
	return out
}

func TestVerifyOfflineStructural(t *testing.T) {
	ds, _ := verifyDataset(t, map[string]cellSpec{
		"velero":  {Status: "supported", Source: "https://example.com/velero"},
		"metallb": {Status: "unknown", Source: "https://example.com/metallb"},
	})
	rep, err := VerifyMatrix(context.Background(), ds, VerifyOptions{Now: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if rep.ExitCode != 0 || rep.Counts[VerifyStructural] != 2 || rep.Counts[VerifyOK] != 0 {
		t.Fatalf("unexpected offline report: %+v", rep)
	}
	for _, row := range rep.Rows {
		if row.Verdict != VerifyStructural {
			t.Fatalf("offline row must be STRUCTURAL: %+v", row)
		}
	}

	stale, _ := verifyDataset(t, map[string]cellSpec{
		"velero": {Status: "supported", Source: "https://example.com/velero", VerifiedAt: "2026-01-01"},
	})
	rep, err = VerifyMatrix(context.Background(), stale, VerifyOptions{Now: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if rep.ExitCode != 2 || verdicts(rep)["velero"] != VerifyStale {
		t.Fatalf("stale mapping: %+v", rep)
	}
}

func TestVerifyOnlineVerdicts(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.Write([]byte(`<html><title>Velero docs</title><p>Velero supports Kubernetes 1.36.</p></html>`))
		case "/weak":
			w.Write([]byte(`<html><title>Cilium 1.21.0-dev documentation</title><p>e2e tested: 1.36.</p></html>`))
		case "/mismatch":
			w.Write([]byte(`<html><title>MetalLB</title><p>Fixes an ASN CRD bug.</p></html>`))
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	caFile := filepath.Join(t.TempDir(), "ca.pem")
	block := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(caFile, block, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := x509.ParseCertificate(server.Certificate().Raw); err != nil {
		t.Fatal(err)
	}
	host := strings.TrimPrefix(server.URL, "https://")

	ds, raw := verifyDataset(t, map[string]cellSpec{
		"velero":   {Status: "supported", Source: server.URL + "/ok"},
		"cilium":   {Status: "unknown", Source: server.URL + "/weak"},
		"metallb":  {Status: "supported", Source: server.URL + "/mismatch"},
		"kubevirt": {Status: "supported", Source: server.URL + "/unreachable"},
	})
	before := append([]byte(nil), raw...)
	rep, err := VerifyMatrix(context.Background(), ds, VerifyOptions{
		Online: true, AllowedHosts: []string{host}, CAFile: caFile,
		Now: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"velero":   VerifyOK,
		"cilium":   VerifyWeak,
		"metallb":  VerifyMismatch,
		"kubevirt": VerifyUnreachable,
	}
	got := verdicts(rep)
	for component, verdict := range want {
		if got[component] != verdict {
			t.Fatalf("%s: got %s, want %s (%+v)", component, got[component], verdict, rep.Rows)
		}
	}
	if rep.ExitCode != 2 || rep.Counts[VerifyOK] != 1 || rep.Counts[VerifyWeak] != 1 || rep.Counts[VerifyMismatch] != 1 || rep.Counts[VerifyUnreachable] != 1 {
		t.Fatalf("counts/exit: %+v", rep)
	}
	if string(raw) != string(before) {
		t.Fatal("verify mutated the dataset bytes")
	}
	for _, row := range rep.Rows {
		if row.Verdict == VerifyOK && (row.HTTPStatus != 200 || row.ContentHash == "") {
			t.Fatalf("OK row must record 200 + hash: %+v", row)
		}
	}
}

func TestVerifyAdjudicatedShortCircuit(t *testing.T) {
	ds, raw := verifyDataset(t, map[string]cellSpec{
		"kubevirt": {Status: "supported", Source: "https://127.0.0.1:1/never"},
	})
	before := append([]byte(nil), raw...)
	rep, err := VerifyMatrix(context.Background(), ds, VerifyOptions{
		Online:        true,
		AllowedHosts:  []string{"127.0.0.1:1"},
		Adjudications: map[string]string{adjudicationKey("kubevirt", "1.36"): "EOL accepted"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if verdicts(rep)["kubevirt"] != VerifyAdjudicated || rep.ExitCode != 0 {
		t.Fatalf("adjudicated mapping: %+v", rep)
	}
	if string(raw) != string(before) {
		t.Fatal("verify mutated the dataset bytes")
	}
}

func TestVerifyOnlineRequiresAllowlist(t *testing.T) {
	ds, _ := verifyDataset(t, map[string]cellSpec{"velero": {Status: "supported", Source: "https://example.com/velero"}})
	if _, err := VerifyMatrix(context.Background(), ds, VerifyOptions{Online: true}); err == nil {
		t.Fatal("online without allowlist must fail")
	}
}

func TestParseAdjudications(t *testing.T) {
	good := []byte(`{"adjudications":[{"component":"kubevirt","kubernetes":"1.33","reason":"EOL"}]}`)
	parsed, err := ParseAdjudications(good)
	if err != nil || parsed[adjudicationKey("kubevirt", "1.33")] != "EOL" {
		t.Fatalf("parse: %v %v", parsed, err)
	}
	if _, err := ParseAdjudications([]byte(`{"adjudications":[{"component":"","kubernetes":"1.33"}]}`)); err == nil {
		t.Fatal("empty component must fail")
	}
	if _, err := ParseAdjudications([]byte(`{"adjudications":[{"component":"a","kubernetes":"1.33"},{"component":"a","kubernetes":"1.33"}]}`)); err == nil {
		t.Fatal("duplicate adjudication must fail")
	}
}

func TestMinorPresenceBoundaries(t *testing.T) {
	if !minorPresent("version 1.36.0 and 1.36", "1.36") {
		t.Fatal("patch version must contain the minor")
	}
	if minorPresent("11.36 or 1.360", "1.36") {
		t.Fatal("numeric neighbours must not match")
	}
	if !minorPresent("matrix: 1.33, 1.34, 1.35, 1.36, 1.37", "1.36") {
		t.Fatal("comma-delimited list must match")
	}
}

func TestVerifyOnlineUnsupportedIsUnverifiable(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><title>Legacy</title><p>Kubernetes 1.36 requires version 2.</p></html>`))
	}))
	defer server.Close()
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	host := strings.TrimPrefix(server.URL, "https://")
	ds, _ := verifyDataset(t, map[string]cellSpec{"legacy": {Status: "unsupported", Source: server.URL + "/page"}})
	rep, err := VerifyMatrix(context.Background(), ds, VerifyOptions{Online: true, AllowedHosts: []string{host}, CAFile: caFile})
	if err != nil {
		t.Fatal(err)
	}
	if verdicts(rep)["legacy"] != VerifyUnverifiable || rep.ExitCode != 2 {
		t.Fatalf("unsupported mapping: %+v", rep)
	}
	if !strings.Contains(rep.Rows[0].Detail, "exclusion") {
		t.Fatalf("detail: %q", rep.Rows[0].Detail)
	}
}
