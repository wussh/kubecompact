package manifests

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wussh/kubecompact/internal/compatibility/fetch"
)

func TestOCIChartDigestAndOfflineBoundary(t *testing.T) {
	body := []byte("synthetic chart archive")
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(body))
	manifest, _ := json.Marshal(map[string]any{"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json", "layers": []any{map[string]any{"mediaType": "application/vnd.cncf.helm.chart.content.v1.tar+gzip", "digest": digest, "size": len(body)}}})
	manifestDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(manifest))
	requests := 0
	corrupt := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != "GET" && r.Method != "HEAD" {
			t.Errorf("unexpected write: %s", r.Method)
			w.WriteHeader(405)
			return
		}
		var data []byte
		if strings.Contains(r.URL.Path, "/manifests/") {
			data = manifest
			w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
			w.Header().Set("Docker-Content-Digest", manifestDigest)
		} else if strings.Contains(r.URL.Path, "/blobs/") {
			data = body
			if corrupt {
				data = []byte("tampered chart archive!")
			}
			w.Header().Set("Content-Type", "application/octet-stream")
		} else {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		if r.Method != "HEAD" {
			w.Write(data)
		}
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	client, err := fetch.New(fetch.Config{Online: true, CAFile: ca, AllowedHosts: []string{u.Host}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := fetchOCIChart(context.Background(), "oci://"+u.Host+"/charts", "demo", "1.2.3", client)
	if err != nil || string(got) != string(body) {
		t.Fatalf("verified pull failed: %v", err)
	}
	corrupt = true
	if _, err = fetchOCIChart(context.Background(), "oci://"+u.Host+"/charts", "demo", "1.2.3", client); err == nil {
		t.Fatal("tampered chart accepted")
	}
	offline, err := fetch.New(fetch.Config{AllowedHosts: []string{u.Host}, CAFile: ca})
	if err != nil {
		t.Fatal(err)
	}
	before := requests
	if _, err = fetchOCIChart(context.Background(), "oci://"+u.Host+"/charts", "demo", "1.2.3", offline); err == nil || requests != before {
		t.Fatal("offline registry access")
	}
}
