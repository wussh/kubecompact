package cache

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestLatestGoodSetAndOriginalExpiry(t *testing.T) {
	s, err := Open(filepath.Join(privateDir(t), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e, r, snapshot := fixture()
	e.RetrievedAt = time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	r[0].Evidence = e
	if err = s.Save(context.Background(), e, r, snapshot); err != nil {
		t.Fatal(err)
	}
	expiry := e.ExpiresAt
	e.ID = "second"
	e.RetrievedAt = e.RetrievedAt.Add(time.Nanosecond)
	r[0].ID = "second-rule"
	r[0].Evidence = e
	if err = s.Save(context.Background(), e, r, snapshot); err != nil {
		t.Fatal(err)
	}
	a, b, err := s.Load(context.Background(), e.Project)
	if err != nil || len(a) != 1 || len(b) != 1 || a[0].ID != "second" || !a[0].ExpiresAt.Equal(expiry) {
		t.Fatalf("latest set/expiry mismatch: %v", err)
	}
}
