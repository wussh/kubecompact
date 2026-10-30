package discovery

import (
	"encoding/base64"
	"encoding/json"
	"github.com/wussh/kubecompact/internal/model"
	"strings"
	"testing"
)

func TestHelmFailedLatest(t *testing.T) {
	inv := model.NewInventory("live", "test")
	inv.Clusters = []model.ClusterObservation{{ID: "c"}}
	for _, s := range []string{`{"name":"r","namespace":"app","version":1,"info":{"status":"superseded"},"chart":{"metadata":{"name":"chart","version":"1","appVersion":"10"}},"config":{"password":"CANARY"}}`, `{"name":"r","namespace":"app","version":2,"info":{"status":"failed"},"chart":{"metadata":{"name":"chart","version":"2","appVersion":"20"}}}`} {
		rel, e := decodeRelease(base64.StdEncoding.EncodeToString([]byte(s)))
		if e != nil {
			t.Fatal(e)
		}
		addRelease(&inv, rel, "storage")
	}
	inv = Correlate(inv)
	if len(inv.Components) != 1 {
		t.Fatal(inv.Components)
	}
	var deployed string
	for _, v := range inv.Components[0].Versions {
		if v.Role == "deployed" && v.Scheme == "chart" {
			deployed = v.Raw
		}
	}
	if deployed != "1" {
		t.Fatal(inv.Components)
	}
	b, _ := json.Marshal(inv)
	if strings.Contains(string(b), "CANARY") {
		t.Fatal("release values exported")
	}
}
