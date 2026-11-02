package planner

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wussh/kubecompact/internal/model"
)

var testNow = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

func baseInput(from, to string) model.PlanInput {
	in := model.PlanInput{
		SchemaVersion: model.PlanInputSchema,
		ClusterID:     "synthetic-lab",
		ObservedAt:    testNow.Add(-time.Hour),
		Releases:      map[string]string{},
	}
	chainMinors := map[string]int{"1.33": 33, "1.34": 34, "1.35": 35}
	chain := map[string]string{"1.33": "v1.33.0+k3s1", "1.34": "v1.34.1+k3s1", "1.35": "v1.35.0+k3s1"}
	source := map[string]string{"1.33": "v1.33.0+k3s1", "1.34": "v1.34.1+k3s1"}
	in.SourceRelease = source[from]
	targetMinor := chainMinors[to]
	for key, release := range chain {
		if chainMinors[key] > 33 && chainMinors[key] <= targetMinor && key != from {
			in.Releases[key] = release
		}
		if from == "1.32" && chainMinors[key] == 33 {
			in.Releases[key] = release
		}
	}
	return in
}

func evalOK(calls *int) Evaluator {
	return func(_ context.Context, inventory model.Inventory, target string) (model.Assessment, error) {
		if calls != nil {
			*calls++
		}
		return model.Assessment{SchemaVersion: model.SchemaVersion, TargetKubernetes: target}, nil
	}
}

func node(name, version string, server, ready bool) model.NodeObservation {
	return model.NodeObservation{Name: name, KubeletVersion: version, Architecture: "amd64", Ready: ready, ControlPlane: server}
}

func inventory(clusterID, version string, nodes ...model.NodeObservation) model.Inventory {
	return model.Inventory{
		SchemaVersion: model.SchemaVersion,
		Clusters: []model.ClusterObservation{{
			ID:                clusterID,
			KubernetesVersion: version,
			Distribution:      "k3s",
			Nodes:             nodes,
			ObservedAt:        testNow.Add(-time.Hour),
		}},
	}
}

func receipt(kind, status string) model.OperatorReceipt {
	return model.OperatorReceipt{
		Kind:       kind,
		Status:     status,
		Owner:      "synthetic-operator",
		ObservedAt: testNow.Add(-time.Hour),
		ValidUntil: testNow.Add(time.Hour),
		EvidenceID: "EV-" + kind,
	}
}

func hopFromTo(fromRelease, toRelease string, inv *model.Inventory, servers, order []string) model.HopInput {
	hop := model.HopInput{
		FromRelease:   fromRelease,
		ToRelease:     toRelease,
		ClusterID:     "synthetic-lab",
		ObservedAt:    testNow.Add(-time.Hour),
		ValidUntil:    testNow.Add(time.Hour),
		EvidenceOwner: "synthetic-operator",
		Datastore:     "sqlite",
		Servers:       servers,
		Order:         order,
		Receipts: []model.OperatorReceipt{
			receipt("backup", "PASS"),
			receipt("restore-drill", "PASS"),
			receipt("token-custody", "PASS"),
			receipt("configuration-preserved", "PASS"),
			receipt("sqlite-backup", "PASS"),
		},
		Inventory: inv,
	}
	return hop
}

func planChecks(a model.Assessment, domain string) []model.CheckResult {
	var out []model.CheckResult
	for _, check := range a.Checks {
		if check.Domain == domain {
			out = append(out, check)
		}
	}
	return out
}

func hasCheck(a model.Assessment, domain, state, reason string) bool {
	for _, check := range a.Checks {
		if check.Domain == domain && check.State == state && check.Reason == reason {
			return true
		}
	}
	return false
}

func mustBuild(t *testing.T, in model.PlanInput, from, to string, evaluate Evaluator) model.UpgradePlan {
	t.Helper()
	plan, err := Build(context.Background(), in, from, to, testNow, evaluate)
	if err != nil {
		t.Fatalf("Build(%s->%s): %v", from, to, err)
	}
	return plan
}

func TestAdjacentOrderedHops(t *testing.T) {
	cases := []struct {
		from, to string
		members  []string
		targets  []string
	}{
		{"1.33", "1.35", []string{"1.33-to-1.34", "1.34-to-1.35"}, []string{"1.34", "1.35"}},
		{"1.32", "1.33", []string{"1.32-to-1.33"}, []string{"1.33"}},
		{"1.32", "1.35", []string{"1.32-to-1.33", "1.33-to-1.34", "1.34-to-1.35"}, []string{"1.33", "1.34", "1.35"}},
	}
	for _, tc := range cases {
		t.Run(tc.from+"->"+tc.to, func(t *testing.T) {
			plan := mustBuild(t, baseInput(tc.from, tc.to), tc.from, tc.to, evalOK(nil))
			if len(plan.Hops) != len(tc.members) {
				t.Fatalf("hops=%d want=%d", len(plan.Hops), len(tc.members))
			}
			for i, hop := range plan.Hops {
				if hop.ID != tc.members[i] {
					t.Fatalf("hop[%d]=%s want=%s", i, hop.ID, tc.members[i])
				}
				if hop.CompatibilityMinor != tc.targets[i] {
					t.Fatalf("hop[%d] minor=%s want=%s", i, hop.CompatibilityMinor, tc.targets[i])
				}
				if hop.ResolvedTarget != tc.targets[i]+".0" {
					t.Fatalf("hop[%d] resolved=%s", i, hop.ResolvedTarget)
				}
				if i == 0 && hop.DependsOn != "" {
					t.Fatalf("first hop depends on %q", hop.DependsOn)
				}
				if i > 0 && hop.DependsOn != tc.members[i-1] {
					t.Fatalf("hop[%d] dependency=%q want=%q", i, hop.DependsOn, tc.members[i-1])
				}
				if len(hop.Steps) == 0 || len(hop.StopConditions) == 0 {
					t.Fatalf("hop[%d] lacks steps or stop conditions", i)
				}
			}
			if plan.ExitCode != 3 || plan.Decision != "INCOMPLETE" {
				t.Fatalf("missing evidence should be INCOMPLETE/3: %s/%d", plan.Decision, plan.ExitCode)
			}
			if tc.from == "1.32" {
				if plan.Hops[0].FromRelease != "" {
					t.Fatalf("optional 1.32 source invented release %q", plan.Hops[0].FromRelease)
				}
				if !hasCheck(plan.Hops[0].Assessment, "plan-release", "UNKNOWN", "EXACT_OFFICIAL_RELEASE_PROVENANCE_UNKNOWN") {
					t.Fatal("unpinned 1.32 source did not remain UNKNOWN")
				}
			}
		})
	}
}

func TestEndpointValidation(t *testing.T) {
	for _, pair := range [][2]string{
		{"1.33", "1.33"}, {"1.34", "1.33"}, {"1.32", "1.32"}, {"1.31", "1.35"},
		{"1.35", "1.36"}, {"1.36", "1.37"}, {"bogus", "1.35"}, {"1.33", "bogus"},
		{"1.33.1", "1.35"}, {"1.33", "1.35.1"},
	} {
		if err := ValidateEndpoints(pair[0], pair[1]); err == nil {
			t.Fatalf("ValidateEndpoints accepted %v", pair)
		}
		if _, err := Build(context.Background(), baseInput(pair[0], pair[1]), pair[0], pair[1], testNow, nil); err == nil {
			t.Fatalf("Build accepted %v", pair)
		}
	}
	for _, pair := range [][2]string{{"1.32", "1.33"}, {"1.32", "1.35"}, {"1.33", "1.35"}, {"1.34", "1.35"}, {"v1.33", "v1.35"}} {
		if err := ValidateEndpoints(pair[0], pair[1]); err != nil {
			t.Fatalf("ValidateEndpoints rejected %v: %v", pair, err)
		}
	}
}

func TestMalformedInputRejected(t *testing.T) {
	validHop := func() model.HopInput {
		inv := inventory("synthetic-lab", "v1.33.0+k3s1", node("s1", "v1.33.0+k3s1", true, true), node("a1", "v1.33.0+k3s1", false, true))
		return hopFromTo("v1.33.0+k3s1", "v1.34.1+k3s1", &inv, []string{"s1"}, []string{"s1", "a1"})
	}
	cases := []struct {
		name   string
		mutate func(*model.PlanInput)
	}{
		{"wrong schema", func(in *model.PlanInput) { in.SchemaVersion = "other" }},
		{"empty cluster", func(in *model.PlanInput) { in.ClusterID = "" }},
		{"unsafe cluster", func(in *model.PlanInput) { in.ClusterID = "token=canary" }},
		{"zero observation", func(in *model.PlanInput) { in.ObservedAt = time.Time{} }},
		{"future observation", func(in *model.PlanInput) { in.ObservedAt = testNow.Add(time.Hour) }},
		{"source release conflict", func(in *model.PlanInput) { in.SourceRelease = "v1.34.1+k3s1" }},
		{"release outside path", func(in *model.PlanInput) { in.Releases["1.32"] = "v1.32.0+k3s1" }},
		{"release wrong minor", func(in *model.PlanInput) { in.Releases["1.34"] = "v1.35.0+k3s1" }},
		{"release malformed", func(in *model.PlanInput) { in.Releases["1.34"] = "1.34.1" }},
		{"duplicate normalized release", func(in *model.PlanInput) { in.Releases["1.34.0"] = "v1.34.1+k3s1" }},
		{"too many releases", func(in *model.PlanInput) {
			in.Releases["1.32"] = ""
			in.Releases["1.33"] = ""
			in.Releases["1.36"] = ""
		}},
		{"hop malformed source", func(in *model.PlanInput) {
			hop := validHop()
			hop.FromRelease = "1.33.0"
			in.Hops = append(in.Hops, hop)
		}},
		{"hop malformed target", func(in *model.PlanInput) {
			hop := validHop()
			hop.ToRelease = "v1.34.1"
			in.Hops = append(in.Hops, hop)
		}},
		{"hop skips minor", func(in *model.PlanInput) {
			hop := validHop()
			hop.ToRelease = "v1.35.0+k3s1"
			in.Hops = append(in.Hops, hop)
		}},
		{"hop outside path", func(in *model.PlanInput) {
			hop := validHop()
			hop.FromRelease = "v1.35.0+k3s1"
			hop.ToRelease = "v1.35.1+k3s1"
			in.Hops = append(in.Hops, hop)
		}},
		{"hop out of order", func(in *model.PlanInput) {
			in.Releases["1.35"] = "v1.35.0+k3s1"
			second := validHop()
			second.FromRelease = "v1.34.1+k3s1"
			second.ToRelease = "v1.35.0+k3s1"
			in.Hops = append(in.Hops, second, validHop())
		}},
		{"duplicate hop", func(in *model.PlanInput) { in.Hops = append(in.Hops, validHop(), validHop()) }},
		{"hop cluster conflict", func(in *model.PlanInput) {
			hop := validHop()
			hop.ClusterID = "other"
			in.Hops = append(in.Hops, hop)
		}},
		{"inventory without digest", func(in *model.PlanInput) {
			hop := validHop()
			hop.InventorySHA256 = ""
			hop.InventoryFile = "inventory.json"
			in.Hops = append(in.Hops, hop)
		}},
		{"digest without inventory", func(in *model.PlanInput) {
			hop := validHop()
			hop.InventoryFile = ""
			hop.InventorySHA256 = strings.Repeat("a", 64)
			in.Hops = append(in.Hops, hop)
		}},
		{"invalid receipt kind", func(in *model.PlanInput) {
			hop := validHop()
			hop.Receipts = append(hop.Receipts, model.OperatorReceipt{Kind: "commands", Status: "PASS", Owner: "owner", EvidenceID: "id"})
			in.Hops = append(in.Hops, hop)
		}},
		{"duplicate receipt kind", func(in *model.PlanInput) {
			hop := validHop()
			hop.Receipts = append(hop.Receipts, receipt("backup", "PASS"))
			in.Hops = append(in.Hops, hop)
		}},
		{"invalid receipt status", func(in *model.PlanInput) {
			hop := validHop()
			hop.Receipts[0].Status = "DONE"
			in.Hops = append(in.Hops, hop)
		}},
		{"invalid receipt owner", func(in *model.PlanInput) {
			hop := validHop()
			hop.Receipts[0].Owner = "<secret>"
			in.Hops = append(in.Hops, hop)
		}},
		{"invalid receipt notes", func(in *model.PlanInput) {
			hop := validHop()
			hop.Receipts[0].Notes = "token=canary"
			in.Hops = append(in.Hops, hop)
		}},
		{"duplicate server", func(in *model.PlanInput) {
			hop := validHop()
			hop.Servers = append(hop.Servers, "s1")
			in.Hops = append(in.Hops, hop)
		}},
		{"duplicate order", func(in *model.PlanInput) {
			hop := validHop()
			hop.Order = append(hop.Order, "s1")
			in.Hops = append(in.Hops, hop)
		}},
		{"unsafe order identity", func(in *model.PlanInput) {
			hop := validHop()
			hop.Order = append(hop.Order, "<script>")
			in.Hops = append(in.Hops, hop)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := baseInput("1.33", "1.35")
			tc.mutate(&in)
			if _, err := Build(context.Background(), in, "1.33", "1.35", testNow, evalOK(nil)); err == nil {
				t.Fatal("Build accepted malformed input")
			}
		})
	}
}

func TestMissingFutureEvidenceStaysPartial(t *testing.T) {
	in := baseInput("1.33", "1.35")
	inv := inventory("synthetic-lab", "v1.33.0+k3s1", node("s1", "v1.33.0+k3s1", true, true), node("a1", "v1.33.0+k3s1", false, true))
	in.Hops = []model.HopInput{hopFromTo("v1.33.0+k3s1", "v1.34.1+k3s1", &inv, []string{"s1"}, []string{"s1", "a1"})}
	calls := 0
	plan := mustBuild(t, in, "1.33", "1.35", evalOK(&calls))
	if calls != 1 {
		t.Fatalf("evaluator calls=%d want=1", calls)
	}
	if len(plan.Hops) != 2 {
		t.Fatalf("hops=%d", len(plan.Hops))
	}
	second := plan.Hops[1]
	if !hasCheck(second.Assessment, "plan-inventory", "UNKNOWN", "HOP_OBSERVATION_MISSING") {
		t.Fatal("missing future inventory was not UNKNOWN")
	}
	if second.InventorySHA256 != "" {
		t.Fatal("missing inventory digest was fabricated")
	}
	if plan.ExitCode != 3 {
		t.Fatalf("partial plan exit=%d", plan.ExitCode)
	}
}

func TestServerFirstOrdering(t *testing.T) {
	nodes := []model.NodeObservation{
		node("s1", "v1.33.0+k3s1", true, true),
		node("s2", "v1.33.0+k3s1", true, true),
		node("a1", "v1.33.0+k3s1", false, true),
	}
	cases := []struct {
		name, state, reason string
		order               []string
	}{
		{"server first", "PASS", "PROPOSED_ONE_AT_A_TIME_ORDER_NOT_EXECUTED", []string{"s1", "s2", "a1"}},
		{"agent first", "FAIL", "AGENT_PRECEDES_SERVER_COMPLETION", []string{"a1", "s1", "s2"}},
		{"server after agent", "FAIL", "SERVER_MUST_PRECEDE_AGENTS", []string{"s1", "a1", "s2"}},
		{"incomplete order", "UNKNOWN", "COMPLETE_ONE_AT_A_TIME_SERVER_FIRST_ORDER_REQUIRED", []string{"s1"}},
		{"unknown member", "FAIL", "ORDER_REFERENCES_UNKNOWN_MEMBER", []string{"s1", "s2", "a1", "ghost"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := baseInput("1.33", "1.34")
			inv := inventory("synthetic-lab", "v1.33.0+k3s1", nodes...)
			in.Hops = []model.HopInput{hopFromTo("v1.33.0+k3s1", "v1.34.1+k3s1", &inv, []string{"s1", "s2"}, tc.order)}
			plan := mustBuild(t, in, "1.33", "1.34", evalOK(nil))
			if !hasCheck(plan.Hops[0].Assessment, "plan-order", tc.state, tc.reason) {
				t.Fatalf("missing %s/%s in %+v", tc.state, tc.reason, planChecks(plan.Hops[0].Assessment, "plan-order"))
			}
		})
	}
}

func TestSkewAndMembershipFailures(t *testing.T) {
	cases := []struct {
		name, state, reason string
		from, to            string
		nodes               []model.NodeObservation
		servers, order      []string
	}{
		{"HA servers exceed one minor", "FAIL", "HA_SERVER_SKEW_EXCEEDS_ONE_MINOR", "1.32", "1.33",
			[]model.NodeObservation{node("s1", "v1.32.1+k3s1", true, true), node("s2", "v1.34.0+k3s1", true, true)},
			[]string{"s1", "s2"}, []string{"s1", "s2"}},
		{"agent newer than oldest server", "FAIL", "AGENT_NEWER_THAN_OLDEST_SERVER", "1.33", "1.34",
			[]model.NodeObservation{node("s1", "v1.33.0+k3s1", true, true), node("s2", "v1.34.0+k3s1", true, true), node("a1", "v1.34.0+k3s1", false, true)},
			[]string{"s1", "s2"}, []string{"s1", "s2", "a1"}},
		{"per-step transition skips minor", "FAIL", "PER_STEP_TRANSITION_SKIPS_MINOR", "1.33", "1.34",
			[]model.NodeObservation{node("s1", "v1.33.0+k3s1", true, true), node("a1", "v1.32.0+k3s1", false, true)},
			[]string{"s1"}, []string{"s1", "a1"}},
		{"member not ready", "FAIL", "MEMBER_NOT_READY", "1.33", "1.34",
			[]model.NodeObservation{node("s1", "v1.33.0+k3s1", true, false), node("a1", "v1.33.0+k3s1", false, true)},
			[]string{"s1"}, []string{"s1", "a1"}},
		{"duplicate node", "FAIL", "DUPLICATE_NODE_IDENTITY", "1.33", "1.34",
			[]model.NodeObservation{node("s1", "v1.33.0+k3s1", true, true), node("s1", "v1.33.0+k3s1", true, true)},
			[]string{"s1"}, []string{"s1"}},
		{"role mismatch", "FAIL", "SERVER_ROLE_OR_MEMBERSHIP_CONFLICT", "1.33", "1.34",
			[]model.NodeObservation{node("s1", "v1.33.0+k3s1", false, true), node("a1", "v1.33.0+k3s1", false, true)},
			[]string{"s1"}, []string{"s1", "a1"}},
		{"missing server observation", "UNKNOWN", "COMPLETE_ROLE_AND_VERSION_MEMBERSHIP_REQUIRED", "1.33", "1.34",
			[]model.NodeObservation{node("s1", "v1.33.0+k3s1", true, true), node("a1", "v1.33.0+k3s1", false, true)},
			[]string{"s1", "s2"}, []string{"s1", "a1"}},
		{"unparseable member version", "UNKNOWN", "COMPLETE_ROLE_AND_VERSION_MEMBERSHIP_REQUIRED", "1.33", "1.34",
			[]model.NodeObservation{node("s1", "not-a-version", true, true), node("a1", "v1.33.0+k3s1", false, true)},
			[]string{"s1"}, []string{"s1", "a1"}},
		{"unexpected cluster count", "FAIL", "CLUSTER_MEMBERSHIP_CONFLICT", "1.33", "1.34",
			nil, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := baseInput(tc.from, tc.to)
			var inv *model.Inventory
			if tc.nodes != nil {
				value := inventory("synthetic-lab", tc.nodes[0].KubeletVersion, tc.nodes...)
				inv = &value
			} else {
				value := model.Inventory{SchemaVersion: model.SchemaVersion, Clusters: []model.ClusterObservation{
					{ID: "synthetic-lab", KubernetesVersion: "v1.33.0+k3s1", Distribution: "k3s", ObservedAt: testNow.Add(-time.Hour)},
					{ID: "other", KubernetesVersion: "v1.33.0+k3s1", Distribution: "k3s", ObservedAt: testNow.Add(-time.Hour)},
				}}
				inv = &value
			}
			fromRelease := in.SourceRelease
			if fromRelease == "" {
				fromRelease = "v1.32.1+k3s1"
			}
			hop := hopFromTo(fromRelease, in.Releases[tc.to], inv, tc.servers, tc.order)
			hop.ClusterID = "synthetic-lab"
			hop.ObservedAt = testNow.Add(-time.Hour)
			hop.ValidUntil = testNow.Add(time.Hour)
			in.Hops = []model.HopInput{hop}
			plan := mustBuild(t, in, tc.from, tc.to, evalOK(nil))
			if !hasCheck(plan.Hops[0].Assessment, "plan-"+checkDomain(tc.reason), tc.state, tc.reason) {
				// Membership reasons share the membership domain; fall back to any domain.
				found := false
				for _, check := range plan.Hops[0].Assessment.Checks {
					if check.State == tc.state && check.Reason == tc.reason {
						found = true
					}
				}
				if !found {
					t.Fatalf("missing %s/%s in %+v", tc.state, tc.reason, plan.Hops[0].Assessment.Checks)
				}
			}
		})
	}
}

func checkDomain(reason string) string {
	switch reason {
	case "HA_SERVER_SKEW_EXCEEDS_ONE_MINOR", "AGENT_NEWER_THAN_OLDEST_SERVER", "PER_STEP_TRANSITION_SKIPS_MINOR", "MEMBER_VERSION_OUTSIDE_HOP":
		return "skew"
	case "DUPLICATE_NODE_IDENTITY", "SERVER_ROLE_OR_MEMBERSHIP_CONFLICT", "COMPLETE_ROLE_AND_VERSION_MEMBERSHIP_REQUIRED", "CLUSTER_MEMBERSHIP_CONFLICT":
		return "membership"
	case "MEMBER_NOT_READY":
		return "health"
	case "ORDER_REFERENCES_UNKNOWN_MEMBER", "SERVER_MUST_PRECEDE_AGENTS", "AGENT_PRECEDES_SERVER_COMPLETION", "COMPLETE_ONE_AT_A_TIME_SERVER_FIRST_ORDER_REQUIRED", "PROPOSED_ONE_AT_A_TIME_ORDER_NOT_EXECUTED":
		return "order"
	}
	return "membership"
}

func TestMembershipFreshnessAndIdentity(t *testing.T) {
	cases := []struct {
		name, state, reason string
		mutate              func(*model.HopInput, *model.Inventory)
	}{
		{"missing owner", "UNKNOWN", "EXPLICIT_FRESHNESS_POLICY_AND_MATCHING_OBSERVATION_REQUIRED", func(h *model.HopInput, _ *model.Inventory) { h.EvidenceOwner = "" }},
		{"missing validity", "UNKNOWN", "EXPLICIT_FRESHNESS_POLICY_AND_MATCHING_OBSERVATION_REQUIRED", func(h *model.HopInput, _ *model.Inventory) { h.ValidUntil = time.Time{} }},
		{"expired validity", "UNKNOWN", "EXPLICIT_FRESHNESS_POLICY_AND_MATCHING_OBSERVATION_REQUIRED", func(h *model.HopInput, _ *model.Inventory) { h.ValidUntil = testNow.Add(-time.Minute) }},
		{"future observation", "UNKNOWN", "EXPLICIT_FRESHNESS_POLICY_AND_MATCHING_OBSERVATION_REQUIRED", func(h *model.HopInput, _ *model.Inventory) { h.ObservedAt = testNow.Add(time.Hour) }},
		{"cluster observation mismatch", "UNKNOWN", "EXPLICIT_FRESHNESS_POLICY_AND_MATCHING_OBSERVATION_REQUIRED", func(_ *model.HopInput, inv *model.Inventory) {
			inv.Clusters[0].ObservedAt = testNow.Add(-2 * time.Hour)
		}},
		{"cluster identity mismatch", "FAIL", "CLUSTER_MEMBERSHIP_CONFLICT", func(_ *model.HopInput, inv *model.Inventory) { inv.Clusters[0].ID = "other" }},
		{"source minor mismatch", "FAIL", "HOP_SOURCE_OBSERVATION_CONFLICT", func(_ *model.HopInput, inv *model.Inventory) { inv.Clusters[0].KubernetesVersion = "v1.34.0+k3s1" }},
		{"source patch mismatch", "UNKNOWN", "HOP_SOURCE_PATCH_DIFFERS_FROM_OBSERVATION", func(_ *model.HopInput, inv *model.Inventory) { inv.Clusters[0].KubernetesVersion = "v1.33.5+k3s1" }},
		{"distribution unproven", "UNKNOWN", "K3S_DISTRIBUTION_OBSERVATION_UNPROVEN", func(_ *model.HopInput, inv *model.Inventory) { inv.Clusters[0].Distribution = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := baseInput("1.33", "1.34")
			inv := inventory("synthetic-lab", "v1.33.0+k3s1", node("s1", "v1.33.0+k3s1", true, true), node("a1", "v1.33.0+k3s1", false, true))
			hop := hopFromTo("v1.33.0+k3s1", "v1.34.1+k3s1", &inv, []string{"s1"}, []string{"s1", "a1"})
			tc.mutate(&hop, &inv)
			hop.Inventory = &inv
			in.Hops = []model.HopInput{hop}
			plan := mustBuild(t, in, "1.33", "1.34", evalOK(nil))
			if !hasCheck(plan.Hops[0].Assessment, "plan-"+domainForReason(tc.reason), tc.state, tc.reason) {
				t.Fatalf("missing %s/%s in %+v", tc.state, tc.reason, plan.Hops[0].Assessment.Checks)
			}
		})
	}
}

func domainForReason(reason string) string {
	switch reason {
	case "EXPLICIT_FRESHNESS_POLICY_AND_MATCHING_OBSERVATION_REQUIRED":
		return "observation-time"
	case "HOP_SOURCE_OBSERVATION_CONFLICT", "HOP_SOURCE_PATCH_DIFFERS_FROM_OBSERVATION":
		return "source-version"
	case "K3S_DISTRIBUTION_OBSERVATION_UNPROVEN":
		return "distribution"
	case "CLUSTER_MEMBERSHIP_CONFLICT":
		return "membership"
	}
	return "membership"
}

func TestDatastoreAndReceipts(t *testing.T) {
	cases := []struct {
		name, datastore, domain, state, reason string
		receipts                               []model.OperatorReceipt
		wantExit                               int
	}{
		{"current prerequisites", "sqlite", "plan-sqlite-backup", "PASS", "CURRENT_OPERATOR_ATTESTATION",
			[]model.OperatorReceipt{receipt("backup", "PASS"), receipt("restore-drill", "PASS"), receipt("token-custody", "PASS"), receipt("configuration-preserved", "PASS"), receipt("sqlite-backup", "PASS")}, 3},
		{"missing backup", "sqlite", "plan-backup", "UNKNOWN", "OPERATOR_RECEIPT_MISSING_OR_STALE", nil, 3},
		{"failed backup blocks", "sqlite", "plan-backup", "FAIL", "OPERATOR_PREREQUISITE_FAILED",
			[]model.OperatorReceipt{receipt("backup", "FAIL")}, 2},
		{"stale backup", "sqlite", "plan-backup", "UNKNOWN", "OPERATOR_RECEIPT_MISSING_OR_STALE",
			[]model.OperatorReceipt{func() model.OperatorReceipt {
				r := receipt("backup", "PASS")
				r.ValidUntil = testNow.Add(-time.Minute)
				return r
			}()}, 3},
		{"future receipt", "sqlite", "plan-backup", "UNKNOWN", "OPERATOR_RECEIPT_MISSING_OR_STALE",
			[]model.OperatorReceipt{func() model.OperatorReceipt {
				r := receipt("backup", "PASS")
				r.ObservedAt = testNow.Add(time.Hour)
				r.ValidUntil = testNow.Add(2 * time.Hour)
				return r
			}()}, 3},
		{"no validity policy", "sqlite", "plan-backup", "UNKNOWN", "OPERATOR_RECEIPT_MISSING_OR_STALE",
			[]model.OperatorReceipt{func() model.OperatorReceipt {
				r := receipt("backup", "PASS")
				r.ValidUntil = time.Time{}
				return r
			}()}, 3},
		{"conflicting datastore method", "sqlite", "plan-datastore", "FAIL", "BACKUP_METHOD_CONFLICTS_WITH_DATASTORE",
			[]model.OperatorReceipt{receipt("etcd-snapshot", "PASS")}, 2},
		{"embedded etcd match", "embedded-etcd", "plan-datastore", "PASS", "DATASTORE_BACKUP_METHOD_MATCHES_OBSERVED_KIND",
			[]model.OperatorReceipt{receipt("etcd-snapshot", "PASS")}, 3},
		{"external db match", "external-db", "plan-datastore", "PASS", "DATASTORE_BACKUP_METHOD_MATCHES_OBSERVED_KIND",
			[]model.OperatorReceipt{receipt("external-db-owner-backup", "PASS")}, 3},
		{"unknown datastore", "", "plan-datastore", "UNKNOWN", "DATASTORE_KIND_UNPROVEN", nil, 3},
		{"failed post hop", "sqlite", "plan-completion-boundary", "FAIL", "SUPPLIED_POST_HOP_HEALTH_FAILED",
			[]model.OperatorReceipt{receipt("post-hop-health", "FAIL")}, 2},
		{"current post hop", "sqlite", "plan-completion-boundary", "PASS", "CURRENT_OPERATOR_POST_HOP_HEALTH",
			[]model.OperatorReceipt{receipt("post-hop-health", "PASS")}, 3},
		{"stale post hop", "sqlite", "plan-completion-boundary", "UNKNOWN", "PENDING_EXECUTION_OR_FRESH_POST_HOP_REOBSERVATION",
			[]model.OperatorReceipt{func() model.OperatorReceipt {
				r := receipt("post-hop-health", "PASS")
				r.ValidUntil = testNow.Add(-time.Minute)
				return r
			}()}, 3},
		{"post-hop older than observation", "sqlite", "plan-completion-boundary", "UNKNOWN", "PENDING_EXECUTION_OR_FRESH_POST_HOP_REOBSERVATION",
			[]model.OperatorReceipt{func() model.OperatorReceipt {
				r := receipt("post-hop-health", "PASS")
				r.ObservedAt = testNow.Add(-3 * time.Hour)
				return r
			}()}, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := baseInput("1.33", "1.34")
			inv := inventory("synthetic-lab", "v1.33.0+k3s1", node("s1", "v1.33.0+k3s1", true, true), node("a1", "v1.33.0+k3s1", false, true))
			hop := hopFromTo("v1.33.0+k3s1", "v1.34.1+k3s1", &inv, []string{"s1"}, []string{"s1", "a1"})
			hop.Datastore = tc.datastore
			hop.Receipts = tc.receipts
			in.Hops = []model.HopInput{hop}
			plan := mustBuild(t, in, "1.33", "1.34", evalOK(nil))
			if !hasCheck(plan.Hops[0].Assessment, tc.domain, tc.state, tc.reason) {
				t.Fatalf("missing %s/%s/%s in %+v", tc.domain, tc.state, tc.reason, plan.Hops[0].Assessment.Checks)
			}
			if tc.wantExit == 2 && plan.ExitCode != 2 {
				t.Fatalf("blocker did not dominate: plan exit=%d", plan.ExitCode)
			}
		})
	}
}

func TestBlockerDominatesMissingEvidence(t *testing.T) {
	in := baseInput("1.33", "1.35")
	inv := inventory("synthetic-lab", "v1.33.0+k3s1", node("s1", "v1.33.0+k3s1", true, true))
	hop := hopFromTo("v1.33.0+k3s1", "v1.34.1+k3s1", &inv, []string{"s1"}, []string{"s1"})
	hop.Receipts = []model.OperatorReceipt{receipt("backup", "FAIL")}
	in.Hops = []model.HopInput{hop}
	plan := mustBuild(t, in, "1.33", "1.35", evalOK(nil))
	if plan.ExitCode != 2 || plan.Decision != "BLOCKED" {
		t.Fatalf("blocker dominance failed: %s/%d", plan.Decision, plan.ExitCode)
	}
	if plan.Hops[1].Assessment.ExitCode != 3 {
		t.Fatalf("missing future hop no longer incomplete: %d", plan.Hops[1].Assessment.ExitCode)
	}
}

func TestDeterministicAndNonMutating(t *testing.T) {
	in := baseInput("1.33", "1.34")
	inv := inventory("synthetic-lab", "v1.33.0+k3s1", node("s1", "v1.33.0+k3s1", true, true), node("a1", "v1.33.0+k3s1", false, true))
	hop := hopFromTo("v1.33.0+k3s1", "v1.34.1+k3s1", &inv, []string{"s1"}, []string{"s1", "a1"})
	hop.Receipts = append(hop.Receipts, receipt("post-hop-health", "PASS"))
	hop.Receipts[0].Notes = "TOKEN_CANARY_ANNOTATION_ENV_SECRET"
	in.Hops = []model.HopInput{hop}
	beforeCluster := inv.Clusters[0].KubernetesVersion
	beforeNodes := []string{inv.Clusters[0].Nodes[0].KubeletVersion, inv.Clusters[0].Nodes[1].KubeletVersion}

	first := mustBuild(t, in, "1.33", "1.34", evalOK(nil))
	second := mustBuild(t, in, "1.33", "1.34", evalOK(nil))
	if !reflect.DeepEqual(first, second) {
		t.Fatal("identical inputs and time produced different plans")
	}
	if inv.Clusters[0].KubernetesVersion != beforeCluster ||
		inv.Clusters[0].Nodes[0].KubeletVersion != beforeNodes[0] ||
		inv.Clusters[0].Nodes[1].KubeletVersion != beforeNodes[1] {
		t.Fatal("planner mutated the supplied inventory")
	}
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	for _, canary := range []string{"TOKEN_CANARY", "ANNOTATION_ENV_SECRET"} {
		if strings.Contains(string(encoded), canary) {
			t.Fatalf("plan output leaked %q", canary)
		}
	}
}

func TestFullyAttestedHopReachesPlanZero(t *testing.T) {
	observed := testNow.Add(-2 * time.Hour)
	postObserved := testNow.Add(-time.Hour)
	membershipAt := func(at time.Time) *model.OperatorReceipt {
		item := receipt("complete-membership", "PASS")
		item.ObservedAt = at
		return &item
	}
	freshMembership := func() *model.OperatorReceipt { return membershipAt(testNow.Add(-time.Hour)) }
	staleMembership := func() *model.OperatorReceipt {
		item := membershipAt(testNow.Add(-time.Hour))
		item.ValidUntil = testNow.Add(-time.Minute)
		return item
	}
	build := func(postHopAt time.Time, membership *model.OperatorReceipt) model.UpgradePlan {
		inv := model.Inventory{
			SchemaVersion: model.SchemaVersion,
			Clusters: []model.ClusterObservation{{
				ID:                "synthetic-lab",
				KubernetesVersion: "v1.34.1+k3s1",
				Distribution:      "k3s",
				ObservedAt:        observed,
				Nodes: []model.NodeObservation{
					{Name: "s1", KubeletVersion: "v1.34.1+k3s1", Architecture: "amd64", Ready: true, ControlPlane: true},
					{Name: "a1", KubeletVersion: "v1.34.1+k3s1", Architecture: "amd64", Ready: true},
				},
			}},
		}
		hop := model.HopInput{
			FromRelease: "v1.34.1+k3s1", ToRelease: "v1.35.0+k3s1", ClusterID: "synthetic-lab",
			ObservedAt: observed, ValidUntil: testNow.Add(time.Hour), EvidenceOwner: "synthetic-operator",
			Datastore: "sqlite", Servers: []string{"s1"}, Order: []string{"s1", "a1"}, Inventory: &inv,
		}
		for _, kind := range []string{"backup", "restore-drill", "token-custody", "configuration-preserved", "sqlite-backup", "post-hop-health"} {
			item := receipt(kind, "PASS")
			if kind == "post-hop-health" {
				item.ObservedAt = postHopAt
			}
			hop.Receipts = append(hop.Receipts, item)
		}
		if membership != nil {
			hop.Receipts = append(hop.Receipts, *membership)
		}
		in := baseInput("1.34", "1.35")
		in.Hops = []model.HopInput{hop}
		evaluate := func(_ context.Context, _ model.Inventory, target string) (model.Assessment, error) {
			return model.Assessment{
				SchemaVersion:    model.SchemaVersion,
				TargetKubernetes: target,
				Components: []model.ComponentAssessment{{
					Component: model.Component{ID: "compatible-app", Name: "compatible-app"},
					Verdict:   model.Compatible,
				}},
				Checks: []model.CheckResult{{
					ID: "control-plane-conservative", Domain: "control-plane", Scope: "synthetic-lab",
					State: "UNKNOWN", Severity: "WARNING", Reason: "CONTROL_PLANE_MEMBERS_NOT_FULLY_OBSERVED",
				}},
			}, nil
		}
		return mustBuild(t, in, "1.34", "1.35", evaluate)
	}

	missing := build(postObserved, nil)
	if missing.ExitCode != 3 || missing.Decision != "INCOMPLETE" {
		t.Fatalf("missing membership attestation plan=%s/%d", missing.Decision, missing.ExitCode)
	}
	if !hasCheck(missing.Hops[0].Assessment, "control-plane", "UNKNOWN", "CONTROL_PLANE_MEMBERS_NOT_FULLY_OBSERVED") {
		t.Fatal("missing complete-membership receipt did not stay UNKNOWN")
	}

	stale := build(postObserved, staleMembership())
	if stale.ExitCode != 3 || !hasCheck(stale.Hops[0].Assessment, "control-plane", "UNKNOWN", "CONTROL_PLANE_MEMBERS_NOT_FULLY_OBSERVED") {
		t.Fatalf("stale membership attestation did not stay UNKNOWN: %s/%d", stale.Decision, stale.ExitCode)
	}

	predating := build(postObserved, membershipAt(observed.Add(-time.Minute)))
	if predating.ExitCode != 3 || !hasCheck(predating.Hops[0].Assessment, "control-plane", "UNKNOWN", "CONTROL_PLANE_MEMBERS_NOT_FULLY_OBSERVED") {
		t.Fatalf("predating membership attestation did not stay UNKNOWN: %s/%d", predating.Decision, predating.ExitCode)
	}

	equalMembership := build(postObserved, membershipAt(observed))
	if equalMembership.ExitCode != 0 || !hasCheck(equalMembership.Hops[0].Assessment, "control-plane", "PASS", "CONTROL_PLANE_MEMBERSHIP_OPERATOR_ATTESTED") {
		t.Fatalf("equal-time membership attestation did not PASS: %s/%d", equalMembership.Decision, equalMembership.ExitCode)
	}

	complete := build(postObserved, freshMembership())
	if complete.ExitCode != 0 || complete.Decision != "NO_KNOWN_BLOCKERS" {
		t.Fatalf("fully attested plan=%s/%d checks=%+v", complete.Decision, complete.ExitCode, complete.Hops[0].Assessment.Checks)
	}
	if !hasCheck(complete.Hops[0].Assessment, "control-plane", "PASS", "CONTROL_PLANE_MEMBERSHIP_OPERATOR_ATTESTED") {
		t.Fatal("fresh complete-membership receipt did not replace the shared UNKNOWN")
	}
	if hasCheck(complete.Hops[0].Assessment, "control-plane", "UNKNOWN", "CONTROL_PLANE_MEMBERS_NOT_FULLY_OBSERVED") {
		t.Fatal("shared conservative UNKNOWN was not replaced")
	}
	if !hasCheck(complete.Hops[0].Assessment, "plan-completion-boundary", "PASS", "CURRENT_OPERATOR_POST_HOP_HEALTH") {
		t.Fatal("complete post-hop receipt did not reach PASS")
	}

	preObservation := build(observed.Add(-time.Hour), freshMembership())
	if preObservation.ExitCode != 3 || preObservation.Decision != "INCOMPLETE" {
		t.Fatalf("pre-observation post-hop plan=%s/%d", preObservation.Decision, preObservation.ExitCode)
	}
	if !hasCheck(preObservation.Hops[0].Assessment, "plan-completion-boundary", "UNKNOWN", "PENDING_EXECUTION_OR_FRESH_POST_HOP_REOBSERVATION") {
		t.Fatal("pre-observation post-hop receipt incorrectly reached PASS")
	}

	equal := build(observed, freshMembership())
	if !hasCheck(equal.Hops[0].Assessment, "plan-completion-boundary", "PASS", "CURRENT_OPERATOR_POST_HOP_HEALTH") || equal.ExitCode != 0 {
		t.Fatalf("equal-time post-hop receipt did not remain PASS: %s/%d", equal.Decision, equal.ExitCode)
	}
}

func TestEvaluatorRequiredOnlyForInventory(t *testing.T) {
	in := baseInput("1.33", "1.34")
	if _, err := Build(context.Background(), in, "1.33", "1.34", testNow, nil); err != nil {
		t.Fatalf("nil evaluator without inventory: %v", err)
	}
	inv := inventory("synthetic-lab", "v1.33.0+k3s1", node("s1", "v1.33.0+k3s1", true, true))
	hop := hopFromTo("v1.33.0+k3s1", "v1.34.1+k3s1", &inv, []string{"s1"}, []string{"s1"})
	in.Hops = []model.HopInput{hop}
	if _, err := Build(context.Background(), in, "1.33", "1.34", testNow, nil); err == nil {
		t.Fatal("nil evaluator accepted inventory")
	}
}

func TestCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Build(ctx, baseInput("1.33", "1.34"), "1.33", "1.34", testNow, evalOK(nil)); err == nil {
		t.Fatal("canceled context was ignored")
	}
}

func TestHopSuppliedSourceReleaseFillsPlan(t *testing.T) {
	in := baseInput("1.34", "1.35")
	in.SourceRelease = ""
	inv := inventory("synthetic-lab", "v1.34.1+k3s1", node("s1", "v1.34.1+k3s1", true, true))
	hop := hopFromTo("v1.34.1+k3s1", "v1.35.0+k3s1", &inv, []string{"s1"}, []string{"s1"})
	in.Hops = []model.HopInput{hop}
	plan := mustBuild(t, in, "1.34", "1.35", evalOK(nil))
	if plan.SourceRelease != "v1.34.1+k3s1" || plan.Hops[0].FromRelease != "v1.34.1+k3s1" {
		t.Fatalf("source release not derived from hop: %q / %q", plan.SourceRelease, plan.Hops[0].FromRelease)
	}
}

func TestPolicyPinsAreApprovedOnly(t *testing.T) {
	if planPolicy.Revision != "911393ceaf029778b356593cc8b6fab977293e96" {
		t.Fatalf("policy revision=%q", planPolicy.Revision)
	}
	if len(planPolicy.Sources) != 2 {
		t.Fatalf("policy sources=%d", len(planPolicy.Sources))
	}
	want := map[string]string{
		"v1.33.0+k3s1": "63ab8e534cdfce2a60f4b016dfedb4f8d74ae8ec",
		"v1.34.1+k3s1": "24fc436e6ea59c56ebc37727baa4e6c9a201ee01",
		"v1.35.0+k3s1": "a6c6cd15c0c42ec9fce21f8ad5f42aa74fddb4f2",
	}
	if len(planPolicy.Releases) != len(want) {
		t.Fatalf("releases=%d want=%d", len(planPolicy.Releases), len(want))
	}
	for release, tagRef := range want {
		pin, ok := planPolicy.Releases[release]
		if !ok || pin.TagRef != tagRef || !strings.HasPrefix(pin.Source, "https://github.com/k3s-io/k3s/releases/tag/") {
			t.Fatalf("release %s pin=%+v", release, pin)
		}
	}
	for _, unverified := range []string{"v1.34.0+k3s1", "v1.34.2+k3s1", "v1.32.0+k3s1"} {
		if _, ok := planPolicy.Releases[unverified]; ok {
			t.Fatalf("unverified release identity %s is pinned", unverified)
		}
	}
}

func TestReleaseIdentityAndPatchMapping(t *testing.T) {
	in := baseInput("1.33", "1.34")
	plan := mustBuild(t, in, "1.33", "1.34", evalOK(nil))
	hop := plan.Hops[0]
	if !hasCheck(hop.Assessment, "plan-release", "PASS", "EXACT_OFFICIAL_RELEASE_PROVENANCE_PASS") {
		t.Fatal("approved release pair was not PASS")
	}
	if !hasCheck(hop.Assessment, "plan-patch-mapping", "UNKNOWN", "EXACT_DISTRIBUTION_PATCH_NOT_COVERED_BY_SCHEMA_CATALOG") {
		t.Fatal("1.34.1 versus schema 1.34.0 was not UNKNOWN")
	}
	if !hasCheck(hop.Assessment, "plan-release-pin-v1.34.1+k3s1", "PASS", "PINNED_OFFICIAL_TAG_REF") {
		t.Fatal("approved 1.34.1 pin absent")
	}
}

func TestOversizedEvidenceBounds(t *testing.T) {
	in := baseInput("1.33", "1.34")
	hop := model.HopInput{FromRelease: "v1.33.0+k3s1", ToRelease: "v1.34.1+k3s1", ClusterID: "synthetic-lab", EvidenceOwner: "operator"}
	for i := range 1025 {
		hop.Servers = append(hop.Servers, "s"+strings.Repeat("x", 3)+string(rune('a'+i%26))+string(rune('a'+(i/26)%26)))
	}
	in.Hops = []model.HopInput{hop}
	if _, err := Build(context.Background(), in, "1.33", "1.34", testNow, nil); err == nil {
		t.Fatal("oversized server evidence accepted")
	}
}
