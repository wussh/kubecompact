package cache

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSQLFailureRollsBackAndRetainsGood(t *testing.T) {
	p := filepath.Join(privateDir(t), "db")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e, r, snap := fixture()
	if err = s.Save(context.Background(), e, r, snap); err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Exec(`CREATE TRIGGER reject_update BEFORE INSERT ON evidence_sets BEGIN SELECT RAISE(ABORT, 'CANARY'); END`)
	if err != nil {
		t.Fatal(err)
	}
	e.ID = "e2"
	e.RetrievedAt = e.RetrievedAt.Add(1)
	r[0].Evidence = e
	if err = s.Save(context.Background(), e, r, snap); err == nil {
		t.Fatal("trigger failure ignored")
	}
	a, _, err := s.Load(context.Background(), "")
	if err != nil || len(a) != 1 || a[0].ID != "e1" {
		t.Fatalf("previous set lost: %v", err)
	}
	info, _ := os.Stat(p)
	if info.Mode().Perm() != 0600 {
		t.Fatal("database permissions")
	}
}
func TestLoadDetectsTampering(t *testing.T) {
	s, err := Open(filepath.Join(privateDir(t), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e, r, snap := fixture()
	if err = s.Save(context.Background(), e, r, snap); err != nil {
		t.Fatal(err)
	}
	s.db.Exec(`UPDATE evidence_sets SET payload='{}'`)
	if _, _, err = s.Load(context.Background(), ""); err == nil {
		t.Fatal("tampered record accepted")
	}
}
func TestProjectIsolation(t *testing.T) {
	s, err := Open(filepath.Join(privateDir(t), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e, r, snap := fixture()
	if err = s.Save(context.Background(), e, r, snap); err != nil {
		t.Fatal(err)
	}
	a, b, err := s.Load(context.Background(), "absent")
	if err != nil || len(a) != 0 || len(b) != 0 {
		t.Fatal("project filter failed")
	}
}
