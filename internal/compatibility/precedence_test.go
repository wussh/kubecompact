package compatibility

import (
	"github.com/wussh/kubecompact/internal/model"
	"testing"
)

func TestRunningImagePrecedesDeployedApplication(t *testing.T) {
	c := model.Component{Project: "cilium", Versions: []model.VersionObservation{{Role: "deployed", Scheme: "app", Raw: "1.16.3", Field: "release.version"}}, Images: []model.ImageObservation{{Role: "running", Image: "quay.io/cilium/cilium:v1.17.4"}}}
	if got := InstalledVersion(c); got != "1.17.4" {
		t.Fatal(got)
	}
	c.Versions = append(c.Versions, model.VersionObservation{Role: "running", Scheme: "app", Raw: "1.18.0", Field: "status.version"})
	if got := InstalledVersion(c); got != "" {
		t.Fatalf("conflicting running observations: %q", got)
	}
}
