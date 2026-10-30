package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/wussh/kubecompact/internal/compatibility/fetch"
	"github.com/wussh/kubecompact/internal/model"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func privateDir(t *testing.T) string {
	t.Helper()
	path := t.TempDir()
	if err := os.Chmod(path, 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func fixture() (model.Evidence, []model.Rule, fetch.SourceSnapshot) {
	now := time.Now().UTC()
	h := sha256.Sum256([]byte("public support table"))
	hash := hex.EncodeToString(h[:])
	e := model.Evidence{ID: "e1", Project: "test", URL: "https://github.com/test", SourceHash: hash, ParserID: "table", ParserVersion: "1", RetrievedAt: now, VerifiedAt: now, ExpiresAt: now.Add(-time.Hour), Valid: true, Excerpt: "supports Kubernetes"}
	return e, []model.Rule{{ID: "r1", Project: "test", ComponentRange: "1.0.0", KubernetesRange: "1.33", Evidence: e}}, fetch.SourceSnapshot{URL: e.URL, FinalURL: e.URL, SHA256: hash, Body: []byte("public support table"), RetrievedAt: now}
}
func TestImmutableStaleAndFailedUpdate(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "cache", "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ev, r, snap := fixture()
	if e = s.Save(context.Background(), ev, r, snap); e != nil {
		t.Fatal(e)
	}
	ev.Valid = false
	if e = s.Save(context.Background(), ev, nil, snap); e == nil {
		t.Fatal("accepted failed parse")
	}
	a, b, e := s.Load(context.Background(), "")
	if e != nil || len(a) != 1 || len(b) != 1 || !a[0].ExpiresAt.Equal(r[0].Evidence.ExpiresAt) || a[0].Fresh(time.Now()) {
		t.Fatalf("stale state lost %v", e)
	}
	ev.Valid = true
	ev.Excerpt = "changed"
	if e = s.Save(context.Background(), ev, r, snap); e == nil {
		t.Fatal("mutated immutable evidence")
	}
}
func TestRollbackAndCanary(t *testing.T) {
	s, e := Open(filepath.Join(privateDir(t), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ev, r, snap := fixture()
	r = append(r, r[0])
	if e = s.Save(context.Background(), ev, r, snap); e == nil {
		t.Fatal("duplicate rules accepted")
	}
	a, _, e := s.Load(context.Background(), "")
	if e != nil || len(a) != 0 {
		t.Fatal("failed save partially committed")
	}
	ev.URL = "https://user:CANARY@github.com/test"
	if e = s.Save(context.Background(), ev, r[:1], snap); e == nil {
		t.Fatal("secret accepted")
	}
}
func TestCorruptionAndVersion(t *testing.T) {
	p := filepath.Join(privateDir(t), "db")
	os.WriteFile(p, []byte("not sqlite"), 0600)
	if s, e := Open(p); e == nil {
		s.Close()
		t.Fatal("corrupt db accepted")
	}
	p = filepath.Join(privateDir(t), "db")
	s, e := Open(p)
	if e != nil {
		t.Fatal(e)
	}
	s.db.Exec("PRAGMA user_version=999")
	s.Close()
	if s, e = Open(p); e == nil {
		s.Close()
		t.Fatal("future schema accepted")
	}
}

func TestRelativeCachePathPersistsEvidence(t *testing.T) {
	t.Chdir(privateDir(t))
	s, err := Open(filepath.Join("cache", "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ev, rules, snapshot := fixture()
	if err := s.Save(context.Background(), ev, rules, snapshot); err != nil {
		t.Fatal(err)
	}
	evidence, loaded, err := s.Load(context.Background(), "test")
	if err != nil || len(evidence) != 1 || len(loaded) != 1 || loaded[0].ID != rules[0].ID {
		t.Fatalf("relative cache round trip failed: %v", err)
	}
}
