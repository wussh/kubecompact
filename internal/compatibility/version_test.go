package compatibility

import (
	"github.com/wussh/kubecompact/internal/model"
	"testing"
)

func TestApplicationVersionIdentity(t *testing.T) {
	cases := []struct {
		name string
		c    model.Component
		want string
	}{
		{"observed upstream tag", model.Component{Project: "metrics-server", Images: []model.ImageObservation{{Role: "running", Image: "registry.k8s.io/metrics-server/metrics-server:v0.8.0"}}}, "0.8.0"},
		{"chart appVersion not runtime", model.Component{Project: "metrics-server", Chart: &model.ChartObservation{AppVersion: "0.8.0"}, Versions: []model.VersionObservation{{Role: "deployed", Scheme: "app", Raw: "0.8.0", Field: "helm.history[0]"}}}, ""},
		{"wrong product", model.Component{Project: "metrics-server", Images: []model.ImageObservation{{Role: "running", Image: "attacker.invalid/metrics-server:v0.8.0"}}}, ""},
		{"digest cannot imply release", model.Component{Project: "metrics-server", Images: []model.ImageObservation{{Role: "running", Image: "registry.k8s.io/metrics-server/metrics-server@sha256:123"}}}, ""},
		{"mixed versions", model.Component{Project: "metrics-server", Images: []model.ImageObservation{{Role: "running", Image: "registry.k8s.io/metrics-server/metrics-server:v0.8.0"}, {Role: "running", Image: "registry.k8s.io/metrics-server/metrics-server:v0.7.2"}}}, ""},
		{"controller not distribution", model.Component{Project: "flux", Images: []model.ImageObservation{{Role: "running", Image: "ghcr.io/fluxcd/helm-controller:v1.4.0"}}}, ""},
		{"requested not running", model.Component{Project: "metrics-server", Images: []model.ImageObservation{{Role: "requested", Image: "registry.k8s.io/metrics-server/metrics-server:v0.8.0"}}}, ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := applicationVersion(tt.c); got != tt.want {
				t.Fatalf("%q != %q", got, tt.want)
			}
		})
	}
}
