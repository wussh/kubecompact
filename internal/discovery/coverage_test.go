package discovery

import (
	"context"
	"encoding/json"
	"github.com/wussh/kubecompact/internal/model"
	"k8s.io/client-go/rest"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestCoverageTransitions(t *testing.T) {
	for _, tt := range []struct {
		name                string
		code                int
		body, state, reason string
	}{{"forbidden", 403, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Forbidden","code":403}`, "UNREADABLE", "FORBIDDEN"}, {"missing", 404, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"NotFound","code":404}`, "NOT_PRESENT", "NOT_SERVED"}, {"expired", 410, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Expired","code":410}`, "UNREADABLE", "CONTINUE_EXPIRED"}} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.code)
				w.Write([]byte(tt.body))
			}))
			defer server.Close()
			r, e := newReader(&rest.Config{Host: server.URL}, false, "")
			if e != nil {
				t.Fatal(e)
			}
			_, cov := r.list(context.Background(), resourceKind{Version: "v1", Resource: "pods", Kind: "Pod", Namespaced: true}, "private")
			if cov.State != tt.state || cov.Code != tt.reason {
				t.Fatal(cov)
			}
		})
	}
}
func TestPaginationBoundary(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("limit") != "200" {
			t.Error("missing page bound")
		}
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			w.Write([]byte(`{"apiVersion":"v1","kind":"PodList","metadata":{"continue":"next"},"items":[]}`))
		} else {
			if r.URL.Query().Get("continue") != "next" {
				t.Error("lost continue")
			}
			w.Write([]byte(`{"apiVersion":"v1","kind":"PodList","metadata":{},"items":[{"apiVersion":"v1","kind":"Pod","metadata":{"name":"last"}}]}`))
		}
	}))
	defer server.Close()
	r, _ := newReader(&rest.Config{Host: server.URL}, false, "")
	items, cov := r.list(context.Background(), resourceKind{Version: "v1", Resource: "pods", Kind: "Pod", Namespaced: true}, "n")
	if len(items) != 1 || cov.State != "COMPLETE" || calls != 2 {
		t.Fatal(items, cov, calls)
	}
}
func TestKubernetesCancellation(t *testing.T) {
	r, e := newReader(&rest.Config{Host: "http://127.0.0.1:1"}, false, "")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, cov := r.list(ctx, resourceKind{Version: "v1", Resource: "pods", Kind: "Pod", Namespaced: true}, "n")
	if cov.State != "UNREADABLE" || cov.Code != "CANCELLED" {
		t.Fatal(cov)
	}
}
func TestCorrelateMixedFixture(t *testing.T) {
	b, e := os.ReadFile("../../testdata/controllers/mixed.json")
	if e != nil {
		t.Fatal(e)
	}
	var objects []map[string]any
	if e = json.Unmarshal(b, &objects); e != nil {
		t.Fatal(e)
	}
	inv := Correlate(FromObjects(objects, "manifests", "fixture"))
	if len(inv.Components) != 2 {
		t.Fatalf("want release chain plus collision, got %+v", inv.Components)
	}
	found := false
	for _, c := range inv.Components {
		if contains(c.ManagedBy, "flux") {
			found = true
			if len(c.Resources) != 4 {
				t.Fatalf("chain lost descendants: %+v", c.Resources)
			}
			var running bool
			for _, im := range c.Images {
				running = running || im.ImageID != ""
			}
			if !running {
				t.Fatal("lost immutable image ID")
			}
		}
	}
	if !found {
		t.Fatal("lost Flux component")
	}
	_ = model.SchemaVersion
}
