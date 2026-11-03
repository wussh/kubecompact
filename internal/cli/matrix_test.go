package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func matrixDatasetFile(t *testing.T, dir, verifiedAt string) string {
	t.Helper()
	comps := []map[string]any{
		{
			"id": "velero", "name": "Velero", "category": "backup", "officialDocs": "https://velero.io/docs/v1.18/",
			"kubernetes": map[string]any{"1.36": map[string]any{
				"status": "supported", "requiredVersion": "v1.18", "source": "https://velero.io/docs/v1.18/", "verifiedAt": verifiedAt,
			}},
		},
		{
			"id": "metallb", "name": "MetalLB", "category": "load-balancer", "officialDocs": "https://metallb.io/",
			"kubernetes": map[string]any{"1.36": map[string]any{
				"status": "unknown", "requiredVersion": "none published", "source": "https://metallb.io/", "verifiedAt": verifiedAt,
			}},
		},
	}
	doc := map[string]any{
		"schemaVersion": "compat-matrix/v1",
		"generatedAt":   "2026-11-02T00:00:00Z",
		"kubernetes":    []string{"1.36"},
		"k3sReleases":   []map[string]any{},
		"components":    comps,
	}
	raw, _ := json.Marshal(doc)
	path := filepath.Join(dir, "matrix.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMatrixBuildInvalidDatasetWritesNothing(t *testing.T) {
	dir := t.TempDir()
	dataset := matrixDatasetFile(t, dir, "2026-11-02")
	raw, _ := os.ReadFile(dataset)
	raw = bytes.Replace(raw, []byte(`"status":"supported"`), []byte(`"status":"maybe"`), 1)
	if err := os.WriteFile(dataset, raw, 0600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.json")
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"matrix", "build", "--dataset", dataset, "--format", "json", "--output", out}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("invalid dataset must write nothing")
	}
}

func TestMatrixBuildAllFormats(t *testing.T) {
	dir := t.TempDir()
	dataset := matrixDatasetFile(t, dir, "2026-11-02")
	scope := filepath.Join(dir, "scope.json")
	os.WriteFile(scope, []byte(`{"inScope":["dev"],"outOfScope":[{"id":"gpu","reason":"prod"}]}`), 0600)
	inventory := "../../testdata/matrix/inventory-dev.json"
	cases := map[string][]string{
		"json":     {"--format", "json"},
		"csv":      {"--format", "csv"},
		"markdown": {"--format", "markdown"},
	}
	for name, extra := range cases {
		out := filepath.Join(dir, "out."+name)
		args := append([]string{"matrix", "build", "--dataset", dataset, "--inventory", inventory, "--scope", scope, "--output", out}, extra...)
		var stdout, stderr bytes.Buffer
		if code := Run(context.Background(), args, &stdout, &stderr); code != 0 {
			t.Fatalf("%s: exit %d: %s", name, code, stderr.String())
		}
		raw, err := os.ReadFile(out)
		if err != nil || len(raw) == 0 {
			t.Fatalf("%s: no output: %v", name, err)
		}
		switch name {
		case "json":
			var doc map[string]any
			if err := json.Unmarshal(raw, &doc); err != nil {
				t.Fatalf("json output: %v", err)
			}
			if doc["schemaVersion"] != "compat-matrix/v1" {
				t.Fatalf("schema: %v", doc["schemaVersion"])
			}
		case "csv":
			if !strings.HasPrefix(string(raw), "component,category,kubernetes,status,requiredVersion,source,verifiedAt,notes,currentVersions\n") {
				t.Fatalf("csv header: %q", strings.SplitN(string(raw), "\n", 2)[0])
			}
		case "markdown":
			if !strings.Contains(string(raw), "## Out of scope") || !strings.Contains(string(raw), "## Compatibility blockers") {
				t.Fatalf("markdown layout: %s", raw)
			}
		}
	}
}

func TestMatrixBuildDigestMismatch(t *testing.T) {
	dir := t.TempDir()
	dataset := matrixDatasetFile(t, dir, "2026-11-02")
	out := filepath.Join(dir, "out.json")
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"matrix", "build", "--dataset", dataset, "--expect-digest", "deadbeef", "--output", out}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("digest mismatch must write nothing")
	}
}

func TestMatrixVerifyExitMapping(t *testing.T) {
	dir := t.TempDir()
	fresh := matrixDatasetFile(t, dir, "2026-11-02")
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"matrix", "verify", "--dataset", fresh}, &stdout, &stderr); code != 0 {
		t.Fatalf("fresh offline verify exit %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "STRUCTURAL 2") {
		t.Fatalf("verdict table: %s", stdout.String())
	}
	stale := matrixDatasetFile(t, dir, "2026-01-01")
	stdout.Reset()
	stderr.Reset()
	if code := Run(context.Background(), []string{"matrix", "verify", "--dataset", stale, "--format", "json"}, &stdout, &stderr); code != 2 {
		t.Fatalf("stale verify exit %d: %s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	invalid := filepath.Join(dir, "invalid.json")
	os.WriteFile(invalid, []byte(`{"schemaVersion":"compat-matrix/v1"}`), 0600)
	if code := Run(context.Background(), []string{"matrix", "verify", "--dataset", invalid}, &stdout, &stderr); code != 1 {
		t.Fatalf("invalid verify exit %d", code)
	}
}

func TestMatrixVerifyOnlineWiring(t *testing.T) {
	dir := t.TempDir()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<title>Velero</title><p>supported Kubernetes 1.36</p>`))
	}))
	defer server.Close()
	caFile := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	host := strings.TrimPrefix(server.URL, "https://")
	doc := map[string]any{
		"schemaVersion": "compat-matrix/v1",
		"generatedAt":   "2026-11-02T00:00:00Z",
		"kubernetes":    []string{"1.36"},
		"k3sReleases":   []map[string]any{},
		"components": []map[string]any{{
			"id": "velero", "name": "Velero", "category": "backup", "officialDocs": "https://velero.io/docs/v1.18/",
			"kubernetes": map[string]any{"1.36": map[string]any{
				"status": "supported", "requiredVersion": "v1.18", "source": server.URL + "/page", "verifiedAt": "2026-11-02",
			}},
		}},
	}
	raw, _ := json.Marshal(doc)
	dataset := filepath.Join(dir, "matrix.json")
	os.WriteFile(dataset, raw, 0600)
	var stdout, stderr bytes.Buffer
	args := []string{"matrix", "verify", "--dataset", dataset, "--online", "--allow-hosts", host, "--ca-file", caFile, "--format", "json"}
	if code := Run(context.Background(), args, &stdout, &stderr); code != 0 {
		t.Fatalf("online verify exit %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"verdict": "OK"`) || !strings.Contains(stdout.String(), `"httpStatus": 200`) {
		t.Fatalf("online report: %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run(context.Background(), []string{"matrix", "verify", "--dataset", dataset, "--online"}, &stdout, &stderr); code != 1 {
		t.Fatalf("online without allow-hosts exit %d", code)
	}
}

func TestMatrixUnknownSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"matrix", "render"}, &stdout, &stderr); code != 1 {
		t.Fatalf("unknown subcommand exit %d", code)
	}
}
