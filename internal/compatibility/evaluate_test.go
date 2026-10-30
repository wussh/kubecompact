package compatibility

import (
	"github.com/wussh/kubecompact/internal/model"
	"testing"
	"time"
)

func TestEvaluateEvidenceDomains(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	c := model.Component{ID: "c", Project: "example", IdentityConfidence: "HIGH", Versions: []model.VersionObservation{{Role: "running", Scheme: "semver", Raw: "1.2.3", Confidence: "HIGH"}}}
	e := model.Evidence{ID: "e", Project: "example", URL: "https://example.org/matrix", SourceHash: "hash", Valid: true, Priority: 0, Confidence: "HIGH", VerifiedAt: now, RetrievedAt: now, ExpiresAt: now.Add(time.Hour)}
	base := model.Rule{ID: "r", Project: "example", ComponentRange: "1.2.x", KubernetesRange: ">=1.30 <1.34", Kind: "support", Effect: "support", Evidence: e}
	cases := []struct {
		name  string
		rules []model.Rule
		want  model.Verdict
	}{
		{"support", []model.Rule{base}, model.Compatible},
		{"missing", nil, model.Unknown},
		{"chart alone", []model.Rule{func() model.Rule { r := base; r.Kind = "chart"; return r }()}, model.Unknown},
		{"tested alone inside", []model.Rule{func() model.Rule { r := base; r.Kind = "tested"; return r }()}, model.Unknown},
		{"tested outside", []model.Rule{func() model.Rule { r := base; r.Kind = "tested"; r.KubernetesRange = ">=1.20 <1.30"; return r }()}, model.NotTested},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			a := Evaluate(model.Inventory{Components: []model.Component{c}}, tt.rules, model.CheckOptions{TargetKubernetes: "1.33", Now: now})
			if a.Components[0].Verdict != tt.want {
				t.Fatalf("got %s want %s", a.Components[0].Verdict, tt.want)
			}
		})
	}
}
