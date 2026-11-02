package data

import (
	"encoding/json"
	"testing"
)

func TestTargetDatasets(t *testing.T) {
	for _, target := range []string{"1.33.0", "1.34.0", "1.35.0"} {
		t.Run(target, func(t *testing.T) {
			d, err := ForTarget(target)
			if err != nil {
				t.Fatal(err)
			}
			lifecycle, err := d.Lifecycle()
			if err != nil {
				t.Fatal(err)
			}
			var catalog struct{ Targets []string }
			if err = json.Unmarshal(lifecycle, &catalog); err != nil {
				t.Fatal(err)
			}
			if len(catalog.Targets) != 1 || catalog.Targets[0] != target[:4] {
				t.Fatalf("wrong catalog: %v", catalog.Targets)
			}
			schemas, err := d.Schemas()
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]int{"1.33.0": 698, "1.34.0": 721, "1.35.0": 726}[target]
			if len(schemas) != want {
				t.Fatalf("schemas%d want%d", len(schemas), want)
			}
			if len(schemas["deployment-apps-v1.json"]) == 0 {
				t.Fatal("Deployment absent")
			}
			prov, err := d.Provenance()
			if err != nil {
				t.Fatal(err)
			}
			var p struct{ Target string }
			if err = json.Unmarshal(prov, &p); err != nil || p.Target != target {
				t.Fatalf("provenance: %v %v", p, err)
			}
		})
	}
	for _, target := range []string{"", "1.33", "1.32.0", "1.34.1", "1.35.1", "1.36.0"} {
		if _, err := ForTarget(target); err == nil {
			t.Fatalf("accepted %q", target)
		}
	}
}
