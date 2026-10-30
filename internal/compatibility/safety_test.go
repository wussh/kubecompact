package compatibility

import (
	"github.com/wussh/kubecompact/internal/model"
	"testing"
	"time"
)

func TestVerdictSafetyCases(t *testing.T) {
	now := time.Now().UTC()
	c := model.Component{ID: "c", Project: "p", IdentityConfidence: "HIGH", Versions: []model.VersionObservation{{Role: "running", Scheme: "semver", Raw: "1.2.3"}}}
	e := model.Evidence{ID: "e", URL: "https://official.example/matrix", SourceHash: "sha", Priority: 0, Confidence: "HIGH", Valid: true, RetrievedAt: now, VerifiedAt: now, ExpiresAt: now.Add(time.Hour)}
	r := model.Rule{ID: "r", Project: "p", ComponentRange: "1.2.x", KubernetesRange: ">=1.30 <1.34", Kind: "support", Effect: "support", Evidence: e}
	tests := []struct {
		name   string
		mutate func() []model.Rule
		want   model.Verdict
	}{
		{"stale", func() []model.Rule { x := r; x.Evidence.ExpiresAt = now.Add(-time.Second); return []model.Rule{x} }, model.Unknown},
		{"conflict", func() []model.Rule { x := r; x.KubernetesRange = ">=1.35"; return []model.Rule{r, x} }, model.Unknown},
		{"known issue not erased", func() []model.Rule {
			x := r
			x.Kind = "knownissue"
			x.KubernetesRange = "1.33.x"
			x.Evidence.Priority = 2
			return []model.Rule{r, x}
		}, model.Incompatible},
		{"third party", func() []model.Rule { x := r; x.Evidence.Priority = 4; return []model.Rule{x} }, model.Unknown},
		{"lifecycle independent", func() []model.Rule { x := r; x.Kind = "lifecycle"; x.Effect = "EOL"; return []model.Rule{r, x} }, model.Compatible},
		{"supported untested", func() []model.Rule {
			x := r
			x.Kind = "tested"
			x.KubernetesRange = "1.32.x"
			return []model.Rule{r, x}
		}, model.Compatible},
		{"patch minimum", func() []model.Rule {
			x := r
			x.Kind = "requirement"
			x.KubernetesRange = ">=1.33.1"
			return []model.Rule{r, x}
		}, model.Incompatible},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(model.Inventory{Components: []model.Component{c}}, tt.mutate(), model.CheckOptions{TargetKubernetes: "1.33.0", Now: now})
			if got.Components[0].Verdict != tt.want {
				t.Fatalf("%s != %s", got.Components[0].Verdict, tt.want)
			}
		})
	}
}
