package cache

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/wussh/kubecompact/internal/compatibility/fetch"
	"github.com/wussh/kubecompact/internal/model"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxRows = 10000
const maxRecord = 1 << 20

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	if path == "" || path == ":memory:" {
		return nil, errors.New("cache requires private filesystem path")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, errors.New("cache path unavailable")
	}
	dir := filepath.Dir(path)
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, errors.New("cache directory unavailable")
	}
	fi, e := os.Lstat(dir)
	if e != nil || !fi.IsDir() || fi.Mode().Perm()&0077 != 0 {
		return nil, errors.New("cache directory must be private")
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if e == nil {
		f.Close()
	} else if !os.IsExist(e) {
		return nil, errors.New("cache file unavailable")
	}
	fi, e = os.Lstat(path)
	if e != nil || !fi.Mode().IsRegular() || fi.Mode().Perm()&0077 != 0 || fi.Size() > 256<<20 {
		return nil, errors.New("cache file must be private regular bounded file")
	}
	db, e := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: path}).String())
	if e != nil {
		return nil, errors.New("cache open failed")
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if e = s.initialize(); e != nil {
		db.Close()
		return nil, e
	}
	return s, nil
}
func (s *Store) initialize() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var integrity string
	if e := s.db.QueryRowContext(ctx, "PRAGMA integrity_check(1)").Scan(&integrity); e != nil || integrity != "ok" {
		return errors.New("cache integrity check failed")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return errors.New("cache migration failed")
	}
	defer tx.Rollback()
	var version int
	if e = tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); e != nil || version > 1 {
		return errors.New("unsupported cache schema")
	}
	if version == 0 {
		for _, q := range []string{`CREATE TABLE evidence_sets (id TEXT PRIMARY KEY, project TEXT NOT NULL, source TEXT NOT NULL, retrieved TEXT NOT NULL, payload BLOB NOT NULL, rules BLOB NOT NULL, digest TEXT NOT NULL, snapshot TEXT NOT NULL)`, `CREATE INDEX evidence_project ON evidence_sets(project,source,retrieved)`, `PRAGMA user_version=1`} {
			if _, e = tx.ExecContext(ctx, q); e != nil {
				return errors.New("cache migration failed")
			}
		}
	}
	if e = tx.Commit(); e != nil {
		return errors.New("cache migration failed")
	}
	return nil
}
func safeURL(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}
func safeText(s string) bool {
	lower := strings.ToLower(s)
	for _, needle := range []string{"-----begin ", "authorization:", "bearer ", "password=", "token=", "client_secret", "stringdata:", "\x00"} {
		if strings.Contains(lower, needle) {
			return false
		}
	}
	return true
}
func (s *Store) Save(ctx context.Context, e model.Evidence, rules []model.Rule, snapshot fetch.SourceSnapshot) error {
	if !e.Valid || e.ID == "" || e.Project == "" || e.ParserID == "" || e.ParserVersion == "" || len(rules) == 0 || len(rules) > 1000 || !safeURL(e.URL) || !safeURL(snapshot.URL) || !safeURL(snapshot.FinalURL) || e.URL != snapshot.URL || e.SourceHash != snapshot.SHA256 || e.RetrievedAt.IsZero() || e.VerifiedAt.IsZero() || e.ExpiresAt.IsZero() || e.RetrievedAt.After(time.Now().Add(time.Minute)) || e.VerifiedAt.After(time.Now().Add(time.Minute)) {
		return errors.New("invalid evidence update")
	}
	if e.FinalURL != "" && !safeURL(e.FinalURL) {
		return errors.New("invalid evidence origin")
	}
	hash := sha256.Sum256(snapshot.Body)
	if hex.EncodeToString(hash[:]) != e.SourceHash || len(snapshot.Body) > 4<<20 {
		return errors.New("snapshot integrity mismatch")
	}
	rules = append([]model.Rule(nil), rules...)
	ids := map[string]bool{}
	for i := range rules {
		r := &rules[i]
		if r.ID == "" || ids[r.ID] || r.Project != e.Project || r.ComponentRange == "" || r.KubernetesRange == "" || r.Evidence.ID != e.ID {
			return errors.New("invalid rule set")
		}
		ids[r.ID] = true
		r.Evidence = e
	}
	payload, err := json.Marshal(e)
	if err != nil {
		return errors.New("invalid evidence")
	}
	encoded, err := json.Marshal(rules)
	if err != nil {
		return errors.New("invalid rules")
	}
	if len(payload) > maxRecord || len(encoded) > maxRecord || !safeText(string(payload)) || !safeText(string(encoded)) {
		return errors.New("evidence rejected by sanitization policy")
	}
	digest := sha256.Sum256(append(append([]byte(nil), payload...), encoded...))
	sum := hex.EncodeToString(digest[:])
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.New("cache transaction failed")
	}
	defer tx.Rollback()
	var old string
	err = tx.QueryRowContext(ctx, "SELECT digest FROM evidence_sets WHERE id=?", e.ID).Scan(&old)
	if err == nil {
		if old != sum {
			return errors.New("immutable evidence conflict")
		}
		return nil
	}
	if err != sql.ErrNoRows {
		return errors.New("cache query failed")
	}
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM evidence_sets").Scan(&count); err != nil || count >= maxRows {
		return errors.New("cache capacity reached")
	}
	retrieved := e.RetrievedAt.UTC().Format("2006-01-02T15:04:05.000000000Z")
	_, err = tx.ExecContext(ctx, "INSERT INTO evidence_sets(id,project,source,retrieved,payload,rules,digest,snapshot) VALUES(?,?,?,?,?,?,?,?)", e.ID, e.Project, e.URL, retrieved, payload, encoded, sum, e.Excerpt)
	if err != nil {
		return errors.New("cache write failed")
	}
	if err = tx.Commit(); err != nil {
		return errors.New("cache commit failed")
	}
	return nil
}
func (s *Store) Load(ctx context.Context, project string) ([]model.Evidence, []model.Rule, error) {
	q := `SELECT payload,rules,digest FROM evidence_sets e WHERE (?='' OR project=?) AND NOT EXISTS (SELECT 1 FROM evidence_sets newer WHERE newer.project=e.project AND newer.source=e.source AND newer.retrieved>e.retrieved) ORDER BY project,source,id LIMIT 10001`
	rows, err := s.db.QueryContext(ctx, q, project, project)
	if err != nil {
		return nil, nil, errors.New("cache load failed")
	}
	defer rows.Close()
	evidence := []model.Evidence{}
	rules := []model.Rule{}
	for rows.Next() {
		if len(evidence) >= maxRows {
			return nil, nil, errors.New("cache load limit")
		}
		var p, r []byte
		var digest string
		if err = rows.Scan(&p, &r, &digest); err != nil || len(p) > maxRecord || len(r) > maxRecord {
			return nil, nil, errors.New("cache record invalid")
		}
		hash := sha256.Sum256(append(append([]byte(nil), p...), r...))
		if hex.EncodeToString(hash[:]) != digest {
			return nil, nil, errors.New("cache record integrity mismatch")
		}
		var e model.Evidence
		var rr []model.Rule
		if json.Unmarshal(p, &e) != nil || json.Unmarshal(r, &rr) != nil || !e.Valid || !safeURL(e.URL) || !safeText(string(p)) || !safeText(string(r)) {
			return nil, nil, errors.New("cache record invalid")
		}
		evidence = append(evidence, e)
		rules = append(rules, rr...)
		if len(rules) > maxRows {
			return nil, nil, errors.New("cache rule load limit")
		}
	}
	if rows.Err() != nil {
		return nil, nil, errors.New("cache load interrupted")
	}
	return evidence, rules, nil
}
func (s *Store) Close() error {
	if e := s.db.Close(); e != nil {
		return errors.New("cache close failed")
	}
	return nil
}
