package cache

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestLatestSnapshotRetainsEveryClaimGroup(t *testing.T) {
	s, err := Open(filepath.Join(privateDir(t), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e, r, snap := fixture()
	for _, id := range []string{"support", "tested"} {
		e.ID = id
		e.ClaimType = id
		r[0].ID = id
		r[0].Evidence = e
		if err = s.Save(context.Background(), e, r, snap); err != nil {
			t.Fatal(err)
		}
	}
	a, b, err := s.Load(context.Background(), "")
	if err != nil || len(a) != 2 || len(b) != 2 {
		t.Fatalf("lost snapshot claims: %d/%d %v", len(a), len(b), err)
	}
	e.ID = "new"
	e.RetrievedAt = e.RetrievedAt.Add(time.Nanosecond)
	r[0].ID = "new"
	r[0].Evidence = e
	if err = s.Save(context.Background(), e, r, snap); err != nil {
		t.Fatal(err)
	}
	a, b, err = s.Load(context.Background(), "")
	if err != nil || len(a) != 1 || len(b) != 1 || a[0].ID != "new" {
		t.Fatalf("old snapshot not replaced: %v", err)
	}
}
