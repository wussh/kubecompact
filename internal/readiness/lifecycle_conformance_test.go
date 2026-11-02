package readiness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/wussh/kubecompact/data"
	"os"
	"testing"
)

func TestPinnedLifecycleConformance(t *testing.T) {
	raw, err := os.ReadFile("testdata/lifecycle-conformance.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		OfficialRevision, OfficialDigest, PlutoRevision, PlutoDigest, KubepugRevision, KubepugDigest, GeneratorRevision string
		Entries                                                                                                         []struct {
			Group, Version, Kind, Introduced, Deprecated, Removed, Replacement, ReplacementIntroduced, PlutoDeprecated, PlutoRemoved, PlutoReplacement, Difference string
			Absent                                                                                                                                                 bool
		}
		Cases []struct {
			Entry, Target        int
			Reason               string
			ReplacementAvailable *bool
		}
	}
	if err = json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data.Lifecycle())
	if hex.EncodeToString(sum[:]) != corpus.OfficialDigest {
		t.Fatal("official catalog changed: regenerate and review additions/removals/conflicts, do not silently repin")
	}
	if corpus.GeneratorRevision != "lifecycle-conformance/v1" || len(corpus.PlutoRevision) != 40 || len(corpus.KubepugRevision) != 40 || len(corpus.PlutoDigest) != 64 || len(corpus.KubepugDigest) != 64 {
		t.Fatal("reference provenance missing")
	}
	var catalog struct {
		SourceRevision string
		Entries        []lifecycleEntry
	}
	if err = json.Unmarshal(data.Lifecycle(), &catalog); err != nil {
		t.Fatal(err)
	}
	if catalog.SourceRevision != corpus.OfficialRevision {
		t.Fatal("official source revision drift")
	}
	index := map[string]lifecycleEntry{}
	for _, e := range catalog.Entries {
		index[e.Group+"/"+e.Version+"/"+e.Kind] = e
	}
	for _, e := range corpus.Entries {
		got, ok := index[e.Group+"/"+e.Version+"/"+e.Kind]
		if e.Absent {
			if ok {
				t.Errorf("previously absent %s now needs adjudication", e.Kind)
			}
			continue
		}
		if !ok || got.Introduced != e.Introduced || got.Deprecated != e.Deprecated || got.Removed != e.Removed || got.Replacement != e.Replacement {
			t.Errorf("official fields changed for %s: %+v", e.Kind, got)
		}
		if e.PlutoRemoved != "" && (e.PlutoRemoved != e.Removed || e.PlutoReplacement != e.Replacement) {
			t.Errorf("unadjudicated Pluto field difference for %s", e.Kind)
		}
		if e.PlutoDeprecated != "" && e.PlutoDeprecated != e.Deprecated && e.Difference == "" {
			t.Errorf("missing deprecation adjudication for %s", e.Kind)
		}
		if e.ReplacementIntroduced != "" {
			found := false
			for _, replacement := range catalog.Entries {
				if replacement.Group+"/"+replacement.Version+" "+replacement.Kind == e.Replacement {
					found = true
					if replacement.Introduced != e.ReplacementIntroduced {
						t.Errorf("replacement introduction changed for %s", e.Kind)
					}
				}
			}
			if !found {
				t.Errorf("replacement unavailable in official catalog for %s", e.Kind)
			}
		}
	}
	for _, c := range corpus.Cases {
		e := corpus.Entries[c.Entry]
		entry, known := index[e.Group+"/"+e.Version+"/"+e.Kind]
		got := lifecycleResult(entry, known, "fixture", c.Target, index)
		if got.Reason != c.Reason {
			t.Errorf("%s at 1.%d: got %s want %s", e.Kind, c.Target, got.Reason, c.Reason)
		}
		if c.ReplacementAvailable != nil {
			want := "false"
			if *c.ReplacementAvailable {
				want = "true"
			}
			if got.Details["replacementAvailable"] != want {
				t.Errorf("replacement availability mismatch for %s", e.Kind)
			}
		}
		if known && entry.Replacement == "" && got.Details["replacementAvailable"] != "not-applicable" {
			t.Errorf("no-replacement case invented availability for %s", e.Kind)
		}
	}
	// The current official metadata names this replacement but has no matching replacement GVK.
	unresolved := index["apps/v1beta1/DeploymentRollback"]
	unknownReplacement := lifecycleResult(unresolved, true, "unresolved-replacement", 35, index)
	if unknownReplacement.Details["replacementAvailable"] != "unknown" {
		t.Fatal("unmapped replacement must not become available")
	}
	if _, err := ResolveTarget("1.24"); err == nil {
		t.Fatal("conformance must not broaden CLI targets")
	}
}
