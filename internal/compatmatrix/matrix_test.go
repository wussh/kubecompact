package compatmatrix

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wussh/kubecompact/internal/compatibility/projects"
	"github.com/wussh/kubecompact/internal/model"
)

func matrixFixture(ids ...string) []byte {
	components := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		components = append(components, map[string]any{
			"id": id, "name": id, "category": "test", "group": "core",
			"officialDocs": "https://example.com/" + id,
			"kubernetes": map[string]any{
				"1.36": map[string]any{
					"status": "supported", "requiredVersion": "v1", "source": "https://example.com/" + id,
					"verifiedAt": "2026-11-02", "notes": "fixture",
				},
			},
		})
	}
	doc := map[string]any{
		"schemaVersion": MatrixSchemaVersion,
		"generatedAt":   "2026-11-02T00:00:00Z",
		"kubernetes":    []string{"1.36"},
		"k3sReleases": []map[string]any{{
			"kubernetes": "1.36", "release": "v1.36.5+k3s1",
			"source": "https://github.com/k3s-io/k3s/releases/tag/v1.36.5%2Bk3s1", "verifiedAt": "2026-11-02",
		}},
		"components": components,
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		panic(err)
	}
	return raw
}

func loadFixture(t *testing.T, raw []byte) *MatrixDataset {
	t.Helper()
	ds, err := LoadMatrixDataset(raw)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return ds
}

func TestLoadMatrixDatasetValid(t *testing.T) {
	ds := loadFixture(t, matrixFixture("velero", "metallb"))
	if ds.Digest == "" || len(ds.Digest) != 64 {
		t.Fatalf("missing dataset digest: %q", ds.Digest)
	}
	if ds.Components[0].Kubernetes["1.36"].Status != "supported" {
		t.Fatalf("unexpected cell: %+v", ds.Components[0])
	}
}

func TestLoadMatrixDatasetRejects(t *testing.T) {
	cases := map[string]func([]byte) []byte{
		"bad status": func(raw []byte) []byte {
			return []byte(strings.Replace(string(raw), `"status":"supported"`, `"status":"maybe"`, 1))
		},
		"http source": func(raw []byte) []byte {
			return []byte(strings.Replace(string(raw), `"source":"https://example.com/velero"`, `"source":"http://example.com/velero"`, 1))
		},
		"bad date": func(raw []byte) []byte {
			return []byte(strings.Replace(string(raw), `"verifiedAt":"2026-11-02"`, `"verifiedAt":"2026-13-01"`, 1))
		},
		"missing minor": func(raw []byte) []byte {
			return []byte(strings.Replace(string(raw), `"kubernetes":{"1.36"`, `"kubernetes":{"1.35"`, 1))
		},
		"undeclared minor": func(raw []byte) []byte {
			return []byte(strings.Replace(string(raw), `"kubernetes":["1.36"]`, `"kubernetes":["1.35"]`, 1))
		},
		"duplicate component": func(raw []byte) []byte {
			var doc map[string]any
			json.Unmarshal(raw, &doc)
			components := doc["components"].([]any)
			doc["components"] = append(components, components[0])
			out, _ := json.Marshal(doc)
			return out
		},
		"duplicate minor list": func(raw []byte) []byte {
			return []byte(strings.Replace(string(raw), `"kubernetes":["1.36"]`, `"kubernetes":["1.36","1.36"]`, 1))
		},
		"duplicate json key": func(raw []byte) []byte {
			return []byte(strings.Replace(string(raw),
				`"1.36":{"notes":"fixture","requiredVersion":"v1",`,
				`"1.36":{"status":"unknown","requiredVersion":"v1","source":"https://example.com/x","verifiedAt":"2026-11-02"},"1.36":{"notes":"fixture","requiredVersion":"v1",`, 1))
		},
		"bad schema": func(raw []byte) []byte {
			return []byte(strings.Replace(string(raw), MatrixSchemaVersion, "compat-matrix/v2", 1))
		},
		"bad docs url": func(raw []byte) []byte {
			return []byte(strings.Replace(string(raw), `"officialDocs":"https://example.com/velero"`, `"officialDocs":"ftp://example.com/velero"`, 1))
		},
		"empty required version": func(raw []byte) []byte {
			return []byte(strings.Replace(string(raw), `"requiredVersion":"v1"`, `"requiredVersion":" "`, 1))
		},
	}
	base := matrixFixture("velero")
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadMatrixDataset(mutate(append([]byte(nil), base...))); err == nil {
				t.Fatalf("expected validation failure for %s", name)
			}
		})
	}
}

func TestParseMatrixScope(t *testing.T) {
	nested, err := ParseMatrixScope([]byte(`{"scope":{"inScope":["dev"],"outOfScope":[{"id":"gpu","reason":"prod"}]}}`))
	if err != nil || len(nested.InScope) != 1 || nested.OutOfScope[0].ID != "gpu" {
		t.Fatalf("nested scope: %+v %v", nested, err)
	}
	direct, err := ParseMatrixScope([]byte(`{"inScope":["dev"],"outOfScope":[{"id":"mgmt"}]}`))
	if err != nil || direct.OutOfScope[0].Reason != "out of scope" {
		t.Fatalf("direct scope: %+v %v", direct, err)
	}
	if _, err := ParseMatrixScope([]byte(`{}`)); err == nil {
		t.Fatal("expected empty scope rejection")
	}
	if _, err := ParseMatrixScope([]byte(`{"outOfScope":[{"id":"gpu"},{"id":"gpu"}]}`)); err == nil {
		t.Fatal("expected duplicate exclusion rejection")
	}
}

func inventoryFixture(t *testing.T) *model.Inventory {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/matrix/inventory-dev.json")
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	var inv model.Inventory
	if err := json.Unmarshal(raw, &inv); err != nil {
		t.Fatalf("fixture decode: %v", err)
	}
	return &inv
}

func componentState(installation *MatrixInstallation, clusterID, componentID string) MatrixComponentInstallation {
	for _, cluster := range installation.Clusters {
		if cluster.ID != clusterID {
			continue
		}
		for _, component := range cluster.Components {
			if component.ID == componentID {
				return component
			}
		}
	}
	return MatrixComponentInstallation{}
}

func TestInstallationStatesFromInventory(t *testing.T) {
	ids := []string{"velero", "cilium", "multus", "openeverest", "nvidia-gpu-operator", "kyverno", "vcluster", "kubevirt", "trident", "metallb", "cert-manager", "kueue"}
	ds := loadFixture(t, matrixFixture(ids...))
	scope := &MatrixScope{InScope: []string{"dev"}, OutOfScope: []MatrixScopeExclusion{{ID: "gpu", Reason: "prod"}}}
	installation := DeriveInstallation(ds, inventoryFixture(t), scope)
	if installation.Provenance != "inventory" {
		t.Fatalf("provenance: %s", installation.Provenance)
	}
	expect := map[string]struct {
		state   string
		version string
		reason  string
	}{
		"velero":              {InstallInstalled, "1.17.1", ""},
		"cilium":              {InstallInstalled, "v1.15.7", ""},
		"multus":              {InstallInstalled, "v4.2.1-build20250607", ""},
		"openeverest":         {InstallInstalled, "1.16.2", ""},
		"nvidia-gpu-operator": {InstallInstalled, "25.10.1", ""},
		"kyverno":             {InstallUnknown, "", "version-unavailable"},
		"vcluster":            {InstallUnknown, "", "platform-only"},
		"kubevirt":            {InstallUnknown, "", "workloads-not-observed"},
		"trident":             {InstallUnknown, "", "workloads-not-observed"},
		"metallb":             {InstallAbsent, "", "not-observed-with-complete-coverage"},
		"cert-manager":        {InstallAbsent, "", "not-observed-with-complete-coverage"},
		"kueue":               {InstallAbsent, "", "not-observed-with-complete-coverage"},
	}
	for id, want := range expect {
		got := componentState(installation, "dev", id)
		if got.State != want.state || got.Version != want.version || got.Reason != want.reason {
			t.Errorf("%s: got %+v, want %+v", id, got, want)
		}
	}
	if got := componentState(installation, "gpu", "velero"); got.State != InstallOutOfScope {
		t.Errorf("out-of-scope cluster: %+v", got)
	}
}

func TestAbsenceRequiresCompleteCoverage(t *testing.T) {
	ds := loadFixture(t, matrixFixture("metallb"))

	noCRDs := inventoryFixture(t)
	var workloadOnly []model.Coverage
	for _, coverage := range noCRDs.Coverage {
		if len(coverage.Kinds) == 1 && coverage.Kinds[0] == "CustomResourceDefinition" {
			continue
		}
		workloadOnly = append(workloadOnly, coverage)
	}
	noCRDs.Coverage = workloadOnly
	installation := DeriveInstallation(ds, noCRDs, nil)
	if got := componentState(installation, "dev", "metallb"); got.State != InstallUnknown || got.Reason != "coverage-incomplete" {
		t.Fatalf("missing CRD coverage must be unknown: %+v", got)
	}

	noWorkloads := inventoryFixture(t)
	var crdOnly []model.Coverage
	for _, coverage := range noWorkloads.Coverage {
		if len(coverage.Kinds) > 1 {
			continue
		}
		crdOnly = append(crdOnly, coverage)
	}
	noWorkloads.Coverage = crdOnly
	installation = DeriveInstallation(ds, noWorkloads, nil)
	if got := componentState(installation, "dev", "metallb"); got.State != InstallUnknown || got.Reason != "coverage-incomplete" {
		t.Fatalf("missing workload coverage must be unknown: %+v", got)
	}
}

func TestAbsenceNotInferredForMultipleClusters(t *testing.T) {
	ds := loadFixture(t, matrixFixture("metallb"))
	inv := inventoryFixture(t)
	inv.Clusters = append(inv.Clusters, model.ClusterObservation{ID: "other"})
	installation := DeriveInstallation(ds, inv, nil)
	for _, cluster := range installation.Clusters {
		if cluster.ID != "dev" && cluster.ID != "other" {
			continue
		}
		if got := componentState(installation, cluster.ID, "metallb"); got.State == InstallAbsent {
			t.Fatalf("ambiguous coverage must not produce absent: %+v", got)
		}
	}
}

func TestUnreachableOnlyFromExplicitFalse(t *testing.T) {
	ds := loadFixture(t, matrixFixture("velero"))
	inv := &model.Inventory{SchemaVersion: model.SchemaVersion, Clusters: []model.ClusterObservation{{ID: "dev", KubernetesVersion: "1.33.0"}}}
	installation := DeriveInstallation(ds, inv, nil)
	cluster := installation.Clusters[0]
	if cluster.State != "in-scope" || cluster.Reachable == nil || !*cluster.Reachable {
		t.Fatalf("missing reachability must stay in-scope: %+v", cluster)
	}
	unreachable := false
	inv.Clusters[0].Reachable = &unreachable
	installation = DeriveInstallation(ds, inv, nil)
	if installation.Clusters[0].State != "unreachable" {
		t.Fatalf("explicit false must be unreachable: %+v", installation.Clusters[0])
	}
}

func TestStateDerivedInstallation(t *testing.T) {
	raw := matrixFixture("velero", "metallb", "vcluster", "cilium")
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	doc["currentState"] = map[string]any{
		"schemaVersion": MatrixCurrentStateSchemaVersion,
		"observedAt":    "2026-11-02T09:16:37Z",
		"clusters": map[string]any{
			"dev": map[string]any{
				"kubernetesVersion": "1.33.13+k3s2", "reachable": true,
				"components": map[string]any{"velero": "1.17.1", "vcluster": "4.11.3"},
				"absent":     []string{"metallb"}, "platform": "4.11.3",
			},
		},
		"inScope":    []string{"dev"},
		"outOfScope": []map[string]any{{"id": "gpu", "reason": "prod"}},
	}
	encoded, _ := json.Marshal(doc)
	ds := loadFixture(t, encoded)
	installation := DeriveInstallation(ds, nil, nil)
	if installation.Provenance != "dataset-current-state" {
		t.Fatalf("provenance: %s", installation.Provenance)
	}
	if got := componentState(installation, "dev", "velero"); got.State != InstallInstalled || got.Version != "1.17.1" {
		t.Fatalf("velero: %+v", got)
	}
	if got := componentState(installation, "dev", "metallb"); got.State != InstallAbsent {
		t.Fatalf("metallb: %+v", got)
	}
	if got := componentState(installation, "dev", "vcluster"); got.State != InstallUnknown || got.Reason != "platform-only" {
		t.Fatalf("vcluster: %+v", got)
	}
	if got := componentState(installation, "dev", "cilium"); got.State != InstallUnknown {
		t.Fatalf("cilium: %+v", got)
	}
	if got := componentState(installation, "gpu", "velero"); got.State != InstallOutOfScope {
		t.Fatalf("gpu: %+v", got)
	}
	if scope := ds.EmbeddedScope(); scope == nil || len(scope.OutOfScope) != 1 {
		t.Fatalf("embedded scope: %+v", scope)
	}
}

func TestMatrixEvidenceAge(t *testing.T) {
	now, _ := time.Parse("2006-01-02", "2026-11-02")
	if age, stale := MatrixEvidenceAge("2026-11-02", now); stale || age != 0 {
		t.Fatalf("fresh evidence flagged stale: %d %v", age, stale)
	}
	if age, stale := MatrixEvidenceAge("2026-01-01", now); !stale || age < 90 {
		t.Fatalf("old evidence not flagged: %d %v", age, stale)
	}
	if _, stale := MatrixEvidenceAge("not-a-date", now); stale {
		t.Fatal("invalid date must not be flagged stale")
	}
}

func TestRepeatedInstallIsAmbiguousNotLastWins(t *testing.T) {
	ds := loadFixture(t, matrixFixture("multus"))

	build := func(first, second string) *model.Inventory {
		return &model.Inventory{
			SchemaVersion: model.SchemaVersion,
			Clusters:      []model.ClusterObservation{{ID: "dev"}},
			Components: []model.Component{
				{ID: "daemonset/kube-system/multus-a", ClusterID: "dev", Project: "multus",
					Versions: []model.VersionObservation{{Role: "deployed", Scheme: "app", Raw: first}},
					Images:   []model.ImageObservation{{Role: "running", Repository: "ghcr.io/k8snetworkplumbingwg/multus-cni", Tag: first}}},
				{ID: "daemonset/kube-system/multus-b", ClusterID: "dev", Project: "multus",
					Versions: []model.VersionObservation{{Role: "deployed", Scheme: "app", Raw: second}},
					Images:   []model.ImageObservation{{Role: "running", Repository: "ghcr.io/k8snetworkplumbingwg/multus-cni", Tag: second}}},
			},
		}
	}
	for _, order := range [][2]string{{"v4.2.1-build", "v4.1.0"}, {"v4.1.0", "v4.2.1-build"}} {
		installation := DeriveInstallation(ds, build(order[0], order[1]), nil)
		got := componentState(installation, "dev", "multus")
		if got.State != InstallUnknown || got.Reason != "identity-ambiguous" {
			t.Fatalf("order %v: got %+v, want unknown identity-ambiguous", order, got)
		}
	}
}

func TestDeclaredProjectConflictingWithImagesIsAmbiguous(t *testing.T) {
	ds := loadFixture(t, matrixFixture("velero", "cilium"))
	inv := &model.Inventory{
		SchemaVersion: model.SchemaVersion,
		Clusters:      []model.ClusterObservation{{ID: "dev"}},
		Components: []model.Component{{
			ID: "deployment/velero/velero", ClusterID: "dev", Project: "velero",
			Versions: []model.VersionObservation{{Role: "deployed", Scheme: "app", Raw: "1.17.1"}},
			Images:   []model.ImageObservation{{Role: "running", Repository: "quay.io/cilium/cilium", Tag: "v1.15.7"}},
		}},
	}
	installation := DeriveInstallation(ds, inv, nil)
	if got := componentState(installation, "dev", "velero"); got.State != InstallUnknown || got.Reason != "identity-ambiguous" {
		t.Fatalf("velero: %+v", got)
	}
	if got := componentState(installation, "dev", "cilium"); got.State != InstallUnknown || got.Reason != "identity-ambiguous" {
		t.Fatalf("cilium: %+v", got)
	}
}

func TestEveryRegisteredProjectResolvesItsRepository(t *testing.T) {
	all := projects.All()
	ids := make([]string, 0, len(all))
	for _, project := range all {
		ids = append(ids, project.ID)
	}
	ds := loadFixture(t, matrixFixture(ids...))
	var components []model.Component
	for _, project := range all {
		components = append(components, model.Component{
			ID: "deployment/" + project.ID + "/workload", ClusterID: "dev",
			Images: []model.ImageObservation{{Role: "running", Repository: project.Repositories[0], Tag: "9.9.9"}},
		})
	}
	inv := &model.Inventory{SchemaVersion: model.SchemaVersion, Clusters: []model.ClusterObservation{{ID: "dev"}}, Components: components}
	installation := DeriveInstallation(ds, inv, nil)
	for _, project := range all {
		got := componentState(installation, "dev", project.ID)
		if project.ID == "vcluster" {
			if got.State != InstallUnknown || got.Reason != "platform-only" {
				t.Fatalf("vcluster: %+v", got)
			}
			continue
		}
		if got.State != InstallInstalled || got.Version != "9.9.9" {
			t.Fatalf("%s (%s): %+v", project.ID, project.Repositories[0], got)
		}
	}
}
func TestMultiControllerProductWithOneVersionIsInstalled(t *testing.T) {
	cases := []struct {
		project    string
		version    string
		components []model.Component
	}{
		{
			project: "cert-manager", version: "v1.21.1",
			components: []model.Component{
				{ID: "deployment/cert-manager/cert-manager", ClusterID: "dev", Project: "cert-manager",
					Versions: []model.VersionObservation{{Role: "deployed", Scheme: "app", Raw: "v1.21.1"}},
					Images:   []model.ImageObservation{{Role: "running", Repository: "quay.io/jetstack/cert-manager-controller", Tag: "v1.21.1"}}},
				{ID: "deployment/cert-manager/cert-manager-webhook", ClusterID: "dev", Project: "cert-manager",
					Versions: []model.VersionObservation{{Role: "deployed", Scheme: "app", Raw: "v1.21.1"}},
					Images:   []model.ImageObservation{{Role: "running", Repository: "quay.io/jetstack/cert-manager-webhook", Tag: "v1.21.1"}}},
				{ID: "deployment/cert-manager/cert-manager-cainjector", ClusterID: "dev", Project: "cert-manager",
					Versions: []model.VersionObservation{{Role: "deployed", Scheme: "app", Raw: "v1.21.1"}},
					Images:   []model.ImageObservation{{Role: "running", Repository: "quay.io/jetstack/cert-manager-cainjector", Tag: "v1.21.1"}}},
			},
		},
		{
			project: "trident", version: "24.06.0",
			components: []model.Component{
				{ID: "deployment/trident-operator/trident-operator", ClusterID: "dev", Project: "trident",
					Versions: []model.VersionObservation{{Role: "deployed", Scheme: "app", Raw: "24.06.0"}},
					Images:   []model.ImageObservation{{Role: "running", Repository: "docker.io/netapp/trident-operator", Tag: "24.06.0"}}},
				{ID: "deployment/trident/trident-controller", ClusterID: "dev", Project: "trident",
					Versions: []model.VersionObservation{{Role: "deployed", Scheme: "app", Raw: "24.06.0"}},
					Images:   []model.ImageObservation{{Role: "running", Repository: "docker.io/netapp/trident", Tag: "24.06.0"}}},
			},
		},
	}
	for _, tc := range cases {
		ds := loadFixture(t, matrixFixture(tc.project))
		for _, components := range [][]model.Component{tc.components, reverseComponents(tc.components)} {
			inv := &model.Inventory{SchemaVersion: model.SchemaVersion, Clusters: []model.ClusterObservation{{ID: "dev"}}, Components: components}
			installation := DeriveInstallation(ds, inv, nil)
			got := componentState(installation, "dev", tc.project)
			if got.State != InstallInstalled || got.Version != tc.version {
				t.Fatalf("%s: got %+v, want installed %s", tc.project, got, tc.version)
			}
		}
	}
}

func reverseComponents(in []model.Component) []model.Component {
	out := make([]model.Component, len(in))
	for i := range in {
		out[len(in)-1-i] = in[i]
	}
	return out
}
