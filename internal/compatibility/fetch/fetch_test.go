package fetch

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func localClient(t *testing.T, h http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	s := httptest.NewTLSServer(h)
	t.Cleanup(s.Close)
	p := filepath.Join(t.TempDir(), "ca.pem")
	os.WriteFile(p, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s.Certificate().Raw}), 0600)
	c, e := New(Config{Online: true, AllowedHosts: []string{strings.TrimPrefix(s.URL, "https://")}, CAFile: p})
	if e != nil {
		t.Fatal(e)
	}
	return c, s
}

func TestGitDiscoveryQueryIsProtocolOnly(t *testing.T) {
	calls := 0
	c, s := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.RawQuery != "service=git-upload-pack" {
			t.Fatal("protocol query was changed")
		}
		w.Write([]byte("fixture"))
	})
	resp, err := c.HTTPClient().Get(s.URL + "/repo/info/refs?service=git-upload-pack")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	for _, path := range []string{"/repo/info/refs?service=git-upload-pack&token=CANARY", "/repo/info/refs?service=git-receive-pack", "/other?service=git-upload-pack"} {
		if _, err := c.HTTPClient().Get(s.URL + path); err == nil {
			t.Fatal("accepted nonprotocol query")
		}
	}
	if _, err := c.Fetch(context.Background(), SourceRequest{URL: s.URL + "/repo/info/refs?service=git-upload-pack"}); err == nil || calls != 1 {
		t.Fatal("documentation fetch query policy changed")
	}
}
func TestOfflineAndSSRF(t *testing.T) {
	c, _ := New(Config{})
	if _, e := c.Fetch(context.Background(), SourceRequest{URL: "https://unresolvable.invalid/schema"}); e != ErrOffline {
		t.Fatalf("offline: %v", e)
	}
	c, _ = New(Config{Online: true})
	for _, u := range []string{"http://github.com/a", "https://127.0.0.1/a", "https://github.com@127.0.0.1/a", "https://github.com/a?token=CANARY"} {
		if _, e := c.Fetch(context.Background(), SourceRequest{URL: u}); e == nil || strings.Contains(e.Error(), "CANARY") {
			t.Fatalf("unsafe %s: %v", u, e)
		}
	}
}
func TestConditionalRetainsProvenance(t *testing.T) {
	calls := 0
	c, s := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("If-None-Match") == "v1" {
			w.WriteHeader(304)
			return
		}
		w.Header().Set("ETag", "v1")
		w.Write([]byte("supported"))
	})
	a, e := c.Fetch(context.Background(), SourceRequest{URL: s.URL})
	if e != nil {
		t.Fatal(e)
	}
	b, e := c.Fetch(context.Background(), SourceRequest{URL: s.URL})
	if e != nil || string(b.Body) != "supported" || !a.RetrievedAt.Equal(b.RetrievedAt) || calls != 2 {
		t.Fatalf("conditional mismatch %v", e)
	}
}
func TestLimitsAndRedirect(t *testing.T) {
	c, s := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "https://169.254.169.254/latest", 302)
			return
		}
		w.Write([]byte(strings.Repeat("x", maxBody+1)))
	})
	for _, p := range []string{"/redirect", "/large"} {
		if _, e := c.Fetch(context.Background(), SourceRequest{URL: s.URL + p}); e == nil {
			t.Fatal("accepted unsafe response")
		}
	}
}
func TestCredentialsPermissions(t *testing.T) {
	p := filepath.Join(t.TempDir(), "credentials")
	os.WriteFile(p, []byte(`{"https://github.com":{"authorization":"Bearer CANARY"}}`), 0644)
	if _, e := New(Config{CredentialsFile: p}); e == nil || strings.Contains(e.Error(), "CANARY") {
		t.Fatal("credentials not protected")
	}
}
func TestRedirectDropsCredentialsAndRetry(t *testing.T) {
	seen := []string{}
	attempts := 0
	c, s := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/end", 302)
		case "/retry":
			attempts++
			if attempts < 3 {
				w.WriteHeader(503)
				return
			}
			w.Write([]byte("ok"))
		default:
			w.Write([]byte("ok"))
		}
	})
	c.credentials[s.URL] = credential{Authorization: "Bearer CANARY"}
	if _, e := c.Fetch(context.Background(), SourceRequest{URL: s.URL + "/start"}); e != nil {
		t.Fatal(e)
	}
	if len(seen) != 2 || seen[0] != "Bearer CANARY" || seen[1] != "" {
		t.Fatalf("redirect leaked credentials: %d", len(seen))
	}
	if _, e := c.Fetch(context.Background(), SourceRequest{URL: s.URL + "/retry"}); e != nil || attempts != 3 {
		t.Fatalf("retry: %v, %d", e, attempts)
	}
}
func TestBodyless304AndFailedUpdate(t *testing.T) {
	fail := false
	c, s := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/empty" {
			w.WriteHeader(304)
			return
		}
		if fail {
			w.WriteHeader(400)
			w.Write([]byte("password=CANARY"))
			return
		}
		w.Header().Set("ETag", "first")
		w.Write([]byte("original"))
	})
	if _, e := c.Fetch(context.Background(), SourceRequest{URL: s.URL + "/empty"}); e == nil {
		t.Fatal("accepted bodyless 304")
	}
	a, e := c.Fetch(context.Background(), SourceRequest{URL: s.URL})
	if e != nil {
		t.Fatal(e)
	}
	fail = true
	if _, e = c.Fetch(context.Background(), SourceRequest{URL: s.URL}); e == nil || strings.Contains(e.Error(), "CANARY") {
		t.Fatal("failed response exposed")
	}
	if string(c.documents[s.URL].snapshot.Body) != "original" || c.documents[s.URL].snapshot.SHA256 != a.SHA256 {
		t.Fatal("failed fetch replaced previous body")
	}
}
func TestLibraryClientOffline(t *testing.T) {
	c, _ := New(Config{})
	r, _ := http.NewRequest("GET", "https://unresolvable.invalid/schema", nil)
	if _, e := c.HTTPClient().Do(r); e == nil {
		t.Fatal("library client egress offline")
	}
}
