package readiness

import (
	"context"
	"github.com/wussh/kubecompact/internal/model"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUsageIntervalAndRemovedClient(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.json")
	os.WriteFile(path, []byte(`{"start":"2026-01-01T00:00:00Z","end":"2026-01-02T00:00:00Z","complete":true,"requests":[{"group":"policy","version":"v1beta1","kind":"PodDisruptionBudget","count":2}]}`), 0600)
	checks, e := Check(context.Background(), model.Inventory{}, model.CheckOptions{TargetKubernetes: "1.35", DeprecatedUsageFile: path, Now: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)})
	if e != nil {
		t.Fatal(e)
	}
	a := model.Assessment{Checks: checks}
	a.Gate()
	if a.ExitCode != 2 {
		t.Fatalf("usage gate=%v", a)
	}
}
func TestIncompleteUsageIsUnknown(t *testing.T) {
	p := filepath.Join(t.TempDir(), "usage.json")
	os.WriteFile(p, []byte(`{"start":"2026-01-01T00:00:00Z","end":"2026-01-02T00:00:00Z","complete":false,"requests":[]}`), 0600)
	checks, e := Check(context.Background(), model.Inventory{}, model.CheckOptions{TargetKubernetes: "1.35", DeprecatedUsageFile: p, Now: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)})
	if e != nil {
		t.Fatal(e)
	}
	if !find(checks, "client-request-usage", "UNKNOWN") {
		t.Fatal("partial usage passed")
	}
}

func TestUsageRejectsTrailingDocuments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	if err := os.WriteFile(path, []byte(`{"start":"2026-01-01T00:00:00Z","end":"2026-01-02T00:00:00Z","complete":true,"requests":[]} {}`), 0600); err != nil {
		t.Fatal(err)
	}
	checks, err := Check(context.Background(), model.Inventory{}, model.CheckOptions{TargetKubernetes: "1.35", DeprecatedUsageFile: path, Now: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range checks {
		if check.Domain == "client-request-usage" && check.Reason == "USAGE_SUMMARY_INVALID" && check.State == "UNKNOWN" {
			return
		}
	}
	t.Fatal("ambiguous trailing usage document was accepted")
}
