package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestComparisonIsDeterministicAndReportsConflicts(t *testing.T) {
	official := []byte(`{"entries":[{"group":"extensions","version":"v1beta1","kind":"Deployment","deprecated":"1.8","removed":"1.16","replacement":"apps/v1 Deployment"}]}`)
	pluto := []byte("deprecated-versions:\n  - version: extensions/v1beta1\n    kind: Deployment\n    component: k8s\n    deprecated-in: v1.9.0\n    removed-in: v1.16.0\n    replacement-api: apps/v1\n")
	var first []byte
	for i := range 10 {
		result, err := compare(official, pluto)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = raw
		} else if !bytes.Equal(first, raw) {
			t.Fatal("nondeterministic output")
		}
	}
	for _, want := range []string{"deprecated: official=1.8 Pluto=1.9", "official coverage absent: UNKNOWN", "Pluto exact-GVK coverage absent"} {
		if !strings.Contains(string(first), want) {
			t.Errorf("lost difference %q", want)
		}
	}
}

func TestComparisonReportsJoinedReplacementAvailabilityConflicts(t *testing.T) {
	official := []byte(`{"entries":[{"group":"extensions","version":"v1beta1","kind":"Ingress","deprecated":"1.14","removed":"1.22","replacement":"networking.k8s.io/v1 Ingress"},{"group":"networking.k8s.io","version":"v1","kind":"Ingress","introduced":"1.19"}]}`)
	for _, availability := range []string{"v1.18.0", "v1.19.0", ""} {
		pluto := []byte("deprecated-versions:\n  - version: extensions/v1beta1\n    kind: Ingress\n    component: k8s\n    deprecated-in: v1.14.0\n    removed-in: v1.22.0\n    replacement-api: networking.k8s.io/v1\n    replacement-available-in: " + availability + "\n")
		got, err := compare(official, pluto)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(raw, []byte(`"OfficialReplacementIntroduced":"1.19"`)) {
			t.Error("review report omitted official joined replacement introduction")
		}
		hasConflict := bytes.Contains(raw, []byte("replacement-available-in: official=1.19 Pluto=1.18"))
		if hasConflict != (availability == "v1.18.0") {
			t.Errorf("wrong availability conflict for %q", availability)
		}
		if availability == "" && !bytes.Contains(raw, []byte("Pluto replacement availability absent")) {
			t.Error("missing reference availability must be explicit")
		}
	}
}
