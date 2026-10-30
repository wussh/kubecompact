package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTransientSourceBodyNeverPersisted(t *testing.T) {
	p := filepath.Join(privateDir(t), "db")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	e, r, snapshot := fixture()
	snapshot.Body = []byte("upstream irrelevant password=RAW_SOURCE_CANARY")
	hash := sha256.Sum256(snapshot.Body)
	snapshot.SHA256 = hex.EncodeToString(hash[:])
	e.SourceHash = snapshot.SHA256
	r[0].Evidence = e
	if err = s.Save(context.Background(), e, r, snapshot); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "RAW_SOURCE_CANARY") {
		t.Fatal("raw source page persisted")
	}
}
