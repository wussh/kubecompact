package compatibility

import (
	"github.com/wussh/kubecompact/internal/model"
	"testing"
	"time"
)

func TestCandidateRequiresIndependentReleaseAndMatchingChart(t *testing.T) {
	now := time.Now().UTC()
	evidence := func(id string) model.Evidence {
		return model.Evidence{ID: id, URL: "https://official.example/" + id, SourceHash: id, Priority: 2, Confidence: "HIGH", Valid: true, RetrievedAt: now, VerifiedAt: now, ExpiresAt: now.Add(time.Hour)}
	}
	c := model.Component{ID: "component", Project: "p", IdentityConfidence: "HIGH", Chart: &model.ChartObservation{Name: "chart", Repository: "https://charts.example", Version: "1.0.0"}, Versions: []model.VersionObservation{{Role: "running", Raw: "1.0.0", Scheme: "semver"}}}
	support := model.Rule{ID: "support", Project: "p", Kind: "support", Effect: "support", ComponentRange: "1.2.x", KubernetesRange: ">=1.35 <1.36", ReleaseVersions: []string{"1.2.1"}, ChartVersions: map[string]string{"1.2.1": "2.0.0"}, Evidence: evidence("support")}
	release := model.Rule{ID: "release", Project: "p", Kind: "release", ComponentRange: "1.2.1", KubernetesRange: "*", ReleaseVersions: []string{"1.2.1"}, Evidence: evidence("release")}
	mapping := model.Rule{ID: "mapping", Project: "p", Kind: "chart-mapping", ComponentRange: "1.2.1", KubernetesRange: ">=1.30", ChartName: "chart", ChartRepository: "https://charts.example", ChartVersions: map[string]string{"1.2.1": "2.0.0"}, Evidence: evidence("mapping")}
	if got := recommend(c, "1.35", []model.Rule{support}, now); got != nil {
		t.Fatalf("matrix invented a release and mapping: %+v", got)
	}
	if got := recommend(c, "1.35", []model.Rule{support, release}, now); got != nil {
		t.Fatalf("no separately verified chart mapping: %+v", got)
	}
	bad := mapping
	bad.ChartRepository = "https://different.example"
	if got := recommend(c, "1.35", []model.Rule{support, release, bad}, now); got != nil {
		t.Fatalf("foreign chart repository accepted: %+v", got)
	}
	bad = mapping
	bad.Evidence.Priority = 4
	if got := recommend(c, "1.35", []model.Rule{support, release, bad}, now); got != nil {
		t.Fatalf("third-party metadata established mapping: %+v", got)
	}
	got := recommend(c, "1.35", []model.Rule{support, release, mapping}, now)
	if got == nil || got.Version != "1.2.1" || got.ChartVersion != "2.0.0" {
		t.Fatalf("missing proven candidate: %+v", got)
	}
	for _, id := range []string{"support", "release", "mapping"} {
		if !contains(got.EvidenceIDs, id) {
			t.Fatalf("missing %s proof: %+v", id, got)
		}
	}
	installed := support
	installed.ID = "installed-exclusion"
	installed.ComponentRange = "1.0.0"
	installed.KubernetesRange = "<1.35"
	installed.ReleaseVersions = nil
	installed.Evidence = evidence("installed-exclusion")
	a := Evaluate(model.Inventory{Components: []model.Component{c}}, []model.Rule{installed, support, release, mapping}, model.CheckOptions{TargetKubernetes: "1.35", Now: now})
	if a.Components[0].Verdict != model.UpgradeRequired {
		t.Fatalf("verdict %s", a.Components[0].Verdict)
	}
	found := map[string]bool{}
	for _, e := range a.Evidence {
		found[e.ID] = true
	}
	for _, id := range a.Components[0].Recommendation.EvidenceIDs {
		if !found[id] {
			t.Fatalf("candidate references missing exported evidence %s", id)
		}
	}
}

func TestUnknownEnvironmentNeverBecomesApplicable(t *testing.T) {
	now := time.Now().UTC()
	e := model.Evidence{ID: "support", URL: "https://official.example/support", SourceHash: "hash", Priority: 2, Confidence: "HIGH", Valid: true, RetrievedAt: now, VerifiedAt: now, ExpiresAt: now.Add(time.Hour)}
	c := model.Component{ID: "c", Project: "p", IdentityConfidence: "HIGH", Versions: []model.VersionObservation{{Role: "running", Raw: "1.2.1", Scheme: "semver"}}}
	support := model.Rule{Project: "p", Kind: "support", Effect: "support", ComponentRange: "1.2.x", KubernetesRange: "*", Environment: map[string]string{"supportChannel": "LTS", "latestPatch": "true"}, Evidence: e}
	release := model.Rule{Project: "p", Kind: "release", ComponentRange: "1.2.1", ReleaseVersions: []string{"1.2.1"}, Evidence: e}
	for range 1000 {
		a := Evaluate(model.Inventory{Components: []model.Component{c}}, []model.Rule{support, release}, model.CheckOptions{TargetKubernetes: "1.35", Now: now})
		if a.Components[0].Verdict != model.Unknown {
			t.Fatalf("unknown constraint accepted: %s", a.Components[0].Verdict)
		}
	}
}

func TestCandidateRejectsReleaseOutsideItsRange(t *testing.T) {
	now := time.Now().UTC()
	e := model.Evidence{ID: "proof", URL: "https://official.example/proof", SourceHash: "hash", Priority: 2, Confidence: "HIGH", Valid: true, RetrievedAt: now, VerifiedAt: now, ExpiresAt: now.Add(time.Hour)}
	c := model.Component{Project: "p", Versions: []model.VersionObservation{{Role: "running", Raw: "1.0.0", Scheme: "semver"}}}
	rules := []model.Rule{
		{Project: "p", Kind: "support", Effect: "support", ComponentRange: "1.2.x", KubernetesRange: "*", Evidence: e},
		{Project: "p", Kind: "release", ComponentRange: "1.1.0", ReleaseVersions: []string{"1.2.1"}, Evidence: e},
	}
	if got := recommend(c, "1.35", rules, now); got != nil {
		t.Fatalf("mismatched release proof accepted: %+v", got)
	}
	rules[0].Environment = map[string]string{"latestPatch": "true"}
	if got := enrichReleases(rules, now)[0].ReleaseVersions; len(got) != 0 {
		t.Fatalf("mismatched release enriched support: %v", got)
	}
}

func TestBoundedReleaseHistoryDoesNotProveLatestPatch(t *testing.T) {
	now := time.Now().UTC()
	e := model.Evidence{ID: "support", URL: "https://official.example/support", SourceHash: "hash", Priority: 2, Confidence: "HIGH", Valid: true, RetrievedAt: now, VerifiedAt: now, ExpiresAt: now.Add(time.Hour)}
	c := model.Component{ID: "c", Project: "p", IdentityConfidence: "HIGH", Versions: []model.VersionObservation{{Role: "running", Raw: "1.2.1", Scheme: "semver"}}}
	rules := []model.Rule{
		{Project: "p", Kind: "support", Effect: "support", ComponentRange: "1.2.x", KubernetesRange: "*", Environment: map[string]string{"latestPatch": "true"}, Evidence: e},
		{Project: "p", Kind: "release", ComponentRange: "1.2.1", ReleaseVersions: []string{"1.2.1"}, Evidence: e},
	}
	a := Evaluate(model.Inventory{Components: []model.Component{c}}, rules, model.CheckOptions{TargetKubernetes: "1.35", Now: now})
	if a.Components[0].Verdict != model.Unknown {
		t.Fatalf("maximum observed release treated as latest: %s", a.Components[0].Verdict)
	}
}
