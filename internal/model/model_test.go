package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestResourceIdentitySeparatesClustersAndNamespaces(t *testing.T) {
	a := ResourceRef{ClusterID: "a", Kind: "Deployment", Namespace: "one", Name: "app", UID: "uid"}
	b := a
	b.ClusterID = "b"
	if a.Key() == b.Key() {
		t.Fatal("different clusters merged")
	}
	b = a
	b.Namespace = "two"
	b.UID = "other"
	if a.Key() == b.Key() {
		t.Fatal("different namespace installations merged")
	}
}

func TestTransientObjectsNeverSerialize(t *testing.T) {
	inv := Inventory{Objects: []map[string]any{{"data": map[string]any{"password": "CONFIDENTIAL_CANARY"}}}}
	data, err := json.Marshal(inv)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "CONFIDENTIAL_CANARY") {
		t.Fatal("raw object escaped into inventory")
	}
}

func TestUnsafeURLCredentialsRemoved(t *testing.T) {
	got := SafeURL("https://name:CONFIDENTIAL_CANARY@example.com/chart?token=CONFIDENTIAL_CANARY#secret")
	if got != "https://example.com/chart" {
		t.Fatalf("unsafe URL projection: %q", got)
	}
}
