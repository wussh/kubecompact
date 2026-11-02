// Package planner evaluates separately supplied per-hop observations.
//
// It is a pure, offline library: it never constructs a Kubernetes client, a
// source fetcher, a cache, or any other I/O. Missing future evidence stays
// UNKNOWN instead of being projected from the initial observation.
package planner

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wussh/kubecompact/internal/model"
)

//go:embed policy.json
var policyJSON []byte

const (
	observedMinMinor = 32
	minTargetMinor   = 33
	maxTargetMinor   = 35
)

var (
	// releasePattern accepts only an exact distribution identity. The schema
	// patch allowlist is separate and never inferred from this value.
	releasePattern = regexp.MustCompile(`^v?1\.(32|33|34|35)\.(0|[1-9][0-9]{0,3})\+k3s([1-9][0-9]{0,3})$`)
	kubePattern    = regexp.MustCompile(`^v?1\.(32|33|34|35)\.([0-9]{1,4})(?:[+-][0-9A-Za-z.+-]+)?$`)
	safeText       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._:/@+-]{0,255}$`)
)

// Evaluator evaluates one hop's supplied inventory against its destination
// minor. The caller owns collection; the planner never refreshes evidence.
type Evaluator func(context.Context, model.Inventory, string) (model.Assessment, error)

type releasePin struct {
	TagRef string `json:"tagRef"`
	Source string `json:"source"`
}

type policyFile struct {
	Revision string                `json:"revision"`
	Sources  map[string]string     `json:"sources"`
	Releases map[string]releasePin `json:"releases"`
}

var (
	planPolicy policyFile
	policyErr  error
)

func init() {
	if err := json.Unmarshal(policyJSON, &planPolicy); err != nil {
		policyErr = errors.New("planner policy integrity failure")
	}
}

// ValidateEndpoints checks the bounded forward path before any input is read.
func ValidateEndpoints(from, to string) error {
	_, _, err := validateEndpoints(from, to)
	return err
}

func validateEndpoints(from, to string) (int, int, error) {
	first, err := endpointMinor(from)
	if err != nil {
		return 0, 0, err
	}
	last, err := endpointMinor(to)
	if err != nil {
		return 0, 0, err
	}
	if first < observedMinMinor || first >= maxTargetMinor {
		return 0, 0, errors.New("source minor must be 1.32, 1.33 or 1.34")
	}
	if last < minTargetMinor || last > maxTargetMinor || last <= first {
		return 0, 0, errors.New("target must be a supported minor strictly greater than the source")
	}
	return first, last, nil
}

// Build returns one ordered hop per adjacent minor. Supplied hop evidence is
// evaluated independently; hops without evidence remain required UNKNOWNs.
func Build(ctx context.Context, in model.PlanInput, from, to string, now time.Time, evaluate Evaluator) (model.UpgradePlan, error) {
	plan := model.UpgradePlan{}
	if ctx == nil {
		return plan, errors.New("plan context is required")
	}
	if policyErr != nil {
		return plan, policyErr
	}
	first, last, err := validateEndpoints(from, to)
	if err != nil {
		return plan, err
	}
	if err := validatePlanInput(in, first, last, now); err != nil {
		return plan, err
	}
	releases, hops, err := validatePath(in, first, last)
	if err != nil {
		return plan, err
	}

	plan = model.UpgradePlan{
		SchemaVersion:    model.PlanSchema,
		AssessedAt:       now,
		Distribution:     "k3s",
		ClusterID:        in.ClusterID,
		FromKubernetes:   minorName(first),
		TargetKubernetes: minorName(last),
		SourceRelease:    releases[first],
		PolicyRevision:   planPolicy.Revision,
		PolicySources:    copyStrings(planPolicy.Sources),
		Hops:             []model.PlanHop{},
		Omissions: []string{
			"Read-only conditional plan; no execution authority, no upgrade command and no rollback guarantee.",
			"Operator attestations are attributed operator evidence, not scanner-verified recoverability.",
			"Saved sanitized inventories contain no raw documents; strict raw-document schema validation remains unavailable.",
			"Upstream minor catalogs do not establish exact distribution patch compatibility; no projected member versions are fabricated.",
		},
	}
	for n := first; n < last; n++ {
		if err := ctx.Err(); err != nil {
			return plan, err
		}
		hop, err := buildHop(ctx, in, releases, hops[n], first, n, now, evaluate)
		if err != nil {
			return plan, err
		}
		plan.Hops = append(plan.Hops, hop)
	}
	aggregate(&plan)
	return plan, nil
}

func buildHop(ctx context.Context, in model.PlanInput, releases map[int]string, h model.HopInput, first, from int, now time.Time, evaluate Evaluator) (model.PlanHop, error) {
	to := from + 1
	scope := fmt.Sprintf("1.%d-to-1.%d", from, to)
	hop := model.PlanHop{
		ID:                 scope,
		FromRelease:        releases[from],
		ToRelease:          releases[to],
		CompatibilityMinor: minorName(to),
		ResolvedTarget:     fmt.Sprintf("1.%d.0", to),
		InventorySHA256:    h.InventorySHA256,
		ObservedAt:         h.ObservedAt,
		Steps:              steps(scope, h),
		StopConditions:     stopConditions(),
	}
	if from > first {
		hop.DependsOn = fmt.Sprintf("1.%d-to-1.%d", from-1, from)
	}
	assessment := model.Assessment{
		SchemaVersion:    model.SchemaVersion,
		TargetKubernetes: hop.CompatibilityMinor,
		ResolvedTarget:   hop.ResolvedTarget,
	}
	if h.Inventory != nil {
		if evaluate == nil {
			return hop, errors.New("inventory evaluator unavailable")
		}
		var err error
		assessment, err = evaluate(ctx, *h.Inventory, hop.CompatibilityMinor)
		if err != nil {
			return hop, err
		}
		if assessment.SchemaVersion == "" {
			assessment.SchemaVersion = model.SchemaVersion
		}
		if assessment.TargetKubernetes == "" {
			assessment.TargetKubernetes = hop.CompatibilityMinor
		}
		if assessment.ResolvedTarget == "" {
			assessment.ResolvedTarget = hop.ResolvedTarget
		}
	}
	if assessment.AssessedAt.IsZero() {
		assessment.AssessedAt = now
	}
	add := func(kind, state, reason string) {
		assessment.Checks = append(assessment.Checks, checked(scope, kind, state, reason))
	}

	identity := "PASS"
	for _, release := range []string{hop.FromRelease, hop.ToRelease} {
		pin, pinned := planPolicy.Releases[canonical(release)]
		if canonical(release) == "" || !pinned {
			identity = "UNKNOWN"
			continue
		}
		check := checked(scope, "release-pin-"+canonical(release), "PASS", "PINNED_OFFICIAL_TAG_REF")
		check.Details = map[string]string{
			"release": release,
			"tagRef":  pin.TagRef,
			"source":  pin.Source,
		}
		assessment.Checks = append(assessment.Checks, check)
	}
	add("release", identity, "EXACT_OFFICIAL_RELEASE_PROVENANCE_"+identity)
	if hop.ToRelease != "" && !strings.HasPrefix(canonical(hop.ToRelease), "v"+hop.ResolvedTarget+"+") {
		add("patch-mapping", "UNKNOWN", "EXACT_DISTRIBUTION_PATCH_NOT_COVERED_BY_SCHEMA_CATALOG")
	}
	if h.Inventory == nil {
		add("inventory", "UNKNOWN", "HOP_OBSERVATION_MISSING")
	} else {
		membershipChecks(&assessment, h, in.ClusterID, scope, from, to, now)
	}
	for _, kind := range []string{"backup", "restore-drill", "token-custody", "configuration-preserved"} {
		receiptCheck(&assessment, h, scope, kind, now)
	}
	datastoreCheck(&assessment, h, scope, now)
	completionCheck(&assessment, h, scope, now)

	assessment.Gate()
	hop.Assessment = assessment
	return hop, nil
}

func datastoreCheck(a *model.Assessment, h model.HopInput, scope string, now time.Time) {
	method := map[string]string{
		"sqlite":        "sqlite-backup",
		"embedded-etcd": "etcd-snapshot",
		"external-db":   "external-db-owner-backup",
	}[h.Datastore]
	if method == "" {
		a.Checks = append(a.Checks, checked(scope, "datastore", "UNKNOWN", "DATASTORE_KIND_UNPROVEN"))
		return
	}
	receiptCheck(a, h, scope, method, now)
	for _, receipt := range h.Receipts {
		if isDatastoreReceipt(receipt.Kind) && receipt.Kind != method {
			a.Checks = append(a.Checks, checked(scope, "datastore", "FAIL", "BACKUP_METHOD_CONFLICTS_WITH_DATASTORE"))
			return
		}
	}
	a.Checks = append(a.Checks, checked(scope, "datastore", "PASS", "DATASTORE_BACKUP_METHOD_MATCHES_OBSERVED_KIND"))
}

func isDatastoreReceipt(kind string) bool {
	switch kind {
	case "sqlite-backup", "etcd-snapshot", "external-db-owner-backup":
		return true
	}
	return false
}

// completionCheck keeps the next hop conditional. A current operator-attested
// post-hop receipt is PASS evidence; missing, stale or failed evidence never
// becomes an implied successful execution.
func completionCheck(a *model.Assessment, h model.HopInput, scope string, now time.Time) {
	result := checked(scope, "completion-boundary", "UNKNOWN", "PENDING_EXECUTION_OR_FRESH_POST_HOP_REOBSERVATION")
	for _, receipt := range h.Receipts {
		if receipt.Kind != "post-hop-health" {
			continue
		}
		if receipt.Status == "FAIL" {
			result = checked(scope, "completion-boundary", "FAIL", "SUPPLIED_POST_HOP_HEALTH_FAILED")
			result.EvidenceIDs = []string{receipt.EvidenceID}
		} else if receipt.Status == "PASS" && current(receipt, now) && !receipt.ObservedAt.Before(h.ObservedAt) {
			result = checked(scope, "completion-boundary", "PASS", "CURRENT_OPERATOR_POST_HOP_HEALTH")
			result.EvidenceIDs = []string{receipt.EvidenceID}
		}
		break
	}
	a.Checks = append(a.Checks, result)
}

// receiptCheck treats freshness as an explicit operator policy. A missing or
// unbound validity interval stays UNKNOWN; an explicit FAIL dominates.
func receiptCheck(a *model.Assessment, h model.HopInput, scope, kind string, now time.Time) {
	result := checked(scope, kind, "UNKNOWN", "OPERATOR_RECEIPT_MISSING_OR_STALE")
	for _, receipt := range h.Receipts {
		if receipt.Kind != kind {
			continue
		}
		details := map[string]string{
			"authority":  "operator-attestation",
			"owner":      receipt.Owner,
			"observedAt": formatTime(receipt.ObservedAt),
			"validUntil": formatTime(receipt.ValidUntil),
		}
		if receipt.Status == "FAIL" {
			result = checked(scope, kind, "FAIL", "OPERATOR_PREREQUISITE_FAILED")
			result.EvidenceIDs = []string{receipt.EvidenceID}
			result.Details = details
			break
		}
		if receipt.Status == "PASS" && current(receipt, now) {
			result = checked(scope, kind, "PASS", "CURRENT_OPERATOR_ATTESTATION")
			result.EvidenceIDs = []string{receipt.EvidenceID}
			result.Details = details
			break
		}
		result.Details = details
	}
	a.Checks = append(a.Checks, result)
}

func current(r model.OperatorReceipt, now time.Time) bool {
	return !r.ObservedAt.IsZero() &&
		!r.ObservedAt.After(now) &&
		r.ValidUntil.After(now) &&
		r.ValidUntil.After(r.ObservedAt)
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// membershipChecks validates one supplied hop observation without rewriting it.
func membershipChecks(a *model.Assessment, h model.HopInput, clusterID, scope string, from, to int, now time.Time) {
	add := func(kind, state, reason string) {
		a.Checks = append(a.Checks, checked(scope, kind, state, reason))
	}
	inv := h.Inventory
	if len(inv.Clusters) != 1 || inv.Clusters[0].ID != clusterID {
		add("membership", "FAIL", "CLUSTER_MEMBERSHIP_CONFLICT")
		return
	}
	cluster := inv.Clusters[0]
	if h.ObservedAt.IsZero() ||
		h.ObservedAt.After(now) ||
		h.ValidUntil.IsZero() ||
		!h.ValidUntil.After(now) ||
		h.EvidenceOwner == "" ||
		cluster.ObservedAt.IsZero() ||
		cluster.ObservedAt.After(now) ||
		!cluster.ObservedAt.Equal(h.ObservedAt) {
		add("observation-time", "UNKNOWN", "EXPLICIT_FRESHNESS_POLICY_AND_MATCHING_OBSERVATION_REQUIRED")
	}
	if minor, err := versionMinor(cluster.KubernetesVersion); err != nil || minor != from {
		add("source-version", "FAIL", "HOP_SOURCE_OBSERVATION_CONFLICT")
	} else if patch, ok := versionPatch(cluster.KubernetesVersion); ok {
		if releasePatch(h.FromRelease) != patch {
			add("source-version", "UNKNOWN", "HOP_SOURCE_PATCH_DIFFERS_FROM_OBSERVATION")
		}
	}
	if !strings.EqualFold(cluster.Distribution, "k3s") {
		add("distribution", "UNKNOWN", "K3S_DISTRIBUTION_OBSERVATION_UNPROVEN")
	}

	servers := make(map[string]bool, len(h.Servers))
	for _, name := range h.Servers {
		servers[name] = true
	}
	nodes := make(map[string]model.NodeObservation, len(cluster.Nodes))
	serverMinors := []int{}
	agentMinors := []int{}
	complete := len(h.Servers) > 0 && len(cluster.Nodes) > 0
	for _, node := range cluster.Nodes {
		if _, duplicate := nodes[node.Name]; duplicate {
			add("membership", "FAIL", "DUPLICATE_NODE_IDENTITY")
			continue
		}
		nodes[node.Name] = node
		if !node.Ready {
			add("health", "FAIL", "MEMBER_NOT_READY")
		}
		if node.ControlPlane != servers[node.Name] {
			add("membership", "FAIL", "SERVER_ROLE_OR_MEMBERSHIP_CONFLICT")
		}
		minor, err := versionMinor(node.KubeletVersion)
		if err != nil {
			complete = false
			continue
		}
		if node.ControlPlane {
			serverMinors = append(serverMinors, minor)
		} else {
			agentMinors = append(agentMinors, minor)
		}
		if minor < from || minor > to {
			add("skew", "FAIL", "MEMBER_VERSION_OUTSIDE_HOP")
		}
		if to-minor > 1 {
			add("skew", "FAIL", "PER_STEP_TRANSITION_SKIPS_MINOR")
		}
	}
	for _, name := range sortedKeys(servers) {
		if _, ok := nodes[name]; !ok {
			complete = false
		}
	}
	if !complete {
		add("membership", "UNKNOWN", "COMPLETE_ROLE_AND_VERSION_MEMBERSHIP_REQUIRED")
	}
	oldestServer := 0
	if len(serverMinors) > 0 {
		oldestServer = serverMinors[0]
		newestServer := serverMinors[0]
		for _, minor := range serverMinors[1:] {
			if minor < oldestServer {
				oldestServer = minor
			}
			if minor > newestServer {
				newestServer = minor
			}
		}
		if newestServer-oldestServer > 1 {
			add("skew", "FAIL", "HA_SERVER_SKEW_EXCEEDS_ONE_MINOR")
		}
	}
	if oldestServer != 0 {
		for _, minor := range agentMinors {
			if minor > oldestServer {
				add("skew", "FAIL", "AGENT_NEWER_THAN_OLDEST_SERVER")
			}
			if oldestServer-minor > 3 {
				add("skew", "FAIL", "AGENT_KUBELET_SKEW_EXCEEDS_THREE_MINORS")
			}
		}
	}
	orderCheck(a, h, nodes, servers, scope)
}

func orderCheck(a *model.Assessment, h model.HopInput, nodes map[string]model.NodeObservation, servers map[string]bool, scope string) {
	add := func(state, reason string) {
		a.Checks = append(a.Checks, checked(scope, "order", state, reason))
	}
	seen := make(map[string]bool, len(h.Order))
	agentsStarted := false
	valid := true
	for _, name := range h.Order {
		node, ok := nodes[name]
		if !ok {
			add("FAIL", "ORDER_REFERENCES_UNKNOWN_MEMBER")
			valid = false
			continue
		}
		seen[name] = true
		if node.ControlPlane {
			if agentsStarted {
				add("FAIL", "SERVER_MUST_PRECEDE_AGENTS")
				valid = false
			}
		} else {
			agentsStarted = true
			for _, server := range sortedKeys(servers) {
				if !seen[server] {
					add("FAIL", "AGENT_PRECEDES_SERVER_COMPLETION")
					valid = false
					break
				}
			}
		}
	}
	if !valid || len(h.Order) != len(nodes) || len(nodes) == 0 {
		add("UNKNOWN", "COMPLETE_ONE_AT_A_TIME_SERVER_FIRST_ORDER_REQUIRED")
		return
	}
	add("PASS", "PROPOSED_ONE_AT_A_TIME_ORDER_NOT_EXECUTED")
}

func steps(scope string, h model.HopInput) []model.PlanStep {
	out := []model.PlanStep{{
		ID:            scope + "-prerequisites",
		Scope:         scope,
		Description:   "Confirm exact releases, complete member observations, datastore backup and restore drill, secure token custody, and preserved server configuration.",
		StopCondition: "Any failed, stale, missing or unattributed prerequisite.",
	}}
	members := h.Order
	if len(members) == 0 {
		members = append([]string(nil), h.Servers...)
		sort.Strings(members)
	}
	for _, member := range members {
		description := "Upgrade this member alone, then re-observe health, quorum and version before the next member."
		if h.Inventory != nil && len(h.Inventory.Clusters) == 1 {
			for _, node := range h.Inventory.Clusters[0].Nodes {
				if node.Name != member {
					continue
				}
				if node.ControlPlane {
					description = "Upgrade this control-plane server alone, then re-observe health, quorum and version before the next member."
				} else {
					description = "Upgrade this agent only after every server has completed and been re-observed healthy."
				}
				break
			}
		}
		out = append(out, model.PlanStep{
			ID:            model.ID(scope, "member", member),
			Scope:         member,
			Description:   description,
			StopCondition: "Any unavailable member or failed health, quorum or version-skew check.",
		})
	}
	out = append(out,
		model.PlanStep{
			ID:            scope + "-agents",
			Scope:         scope,
			Description:   "Consider agents only after every server has completed and been re-observed healthy at the destination release.",
			StopCondition: "Incomplete server completion evidence or agent/server skew.",
		},
		model.PlanStep{
			ID:            scope + "-observe",
			Scope:         scope,
			Description:   "Collect fresh sanitized post-hop inventory and health evidence before considering the next minor.",
			StopCondition: "Missing or failed post-hop health and re-observation.",
		},
	)
	return out
}

func stopConditions() []string {
	return []string{
		"Stop before this hop unless every prerequisite check is PASS; unresolved UNKNOWN evidence is not permission to execute.",
		"Stop on any failed or stale operator receipt, member health, quorum or version-skew check.",
		"Stop before advancing to the next hop until a fresh post-hop observation confirms the destination release and healthy members.",
		"This plan executes nothing and guarantees no rollback; operator action requires separate approval.",
	}
}

func validatePlanInput(in model.PlanInput, first, last int, now time.Time) error {
	if in.SchemaVersion != model.PlanInputSchema {
		return errors.New("invalid plan input schema")
	}
	if !safeText.MatchString(in.ClusterID) {
		return errors.New("invalid or missing plan cluster identity")
	}
	if in.ObservedAt.IsZero() || in.ObservedAt.After(now) {
		return errors.New("plan observation time is missing or in the future")
	}
	if len(in.Hops) > maxTargetMinor-observedMinMinor {
		return errors.New("plan hop evidence exceeds bounds")
	}
	if len(in.Releases) > maxTargetMinor-observedMinMinor {
		return errors.New("plan destination releases exceed bounds")
	}
	if in.SourceRelease != "" {
		minor, err := releaseMinor(in.SourceRelease)
		if err != nil || minor != first {
			return errors.New("source release conflicts with source minor")
		}
	}
	return nil
}

func validatePath(in model.PlanInput, first, last int) (map[int]string, map[int]model.HopInput, error) {
	releases := map[int]string{first: canonical(in.SourceRelease)}
	keys := make([]string, 0, len(in.Releases))
	for key := range in.Releases {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	seenReleases := make(map[int]bool, len(keys))
	for _, key := range keys {
		minor, err := endpointMinor(key)
		if err != nil || minor <= first || minor > last {
			return nil, nil, errors.New("destination release entry outside the ordered path")
		}
		if seenReleases[minor] {
			return nil, nil, errors.New("duplicate destination release entry")
		}
		seenReleases[minor] = true
		release := in.Releases[key]
		if release == "" {
			releases[minor] = ""
			continue
		}
		pinnedMinor, err := releaseMinor(release)
		if err != nil || pinnedMinor != minor {
			return nil, nil, errors.New("release identity conflicts with destination minor")
		}
		releases[minor] = canonical(release)
	}

	hops := make(map[int]model.HopInput, len(in.Hops))
	previous := first - 1
	for _, hop := range in.Hops {
		from, err := releaseMinor(hop.FromRelease)
		if err != nil {
			return nil, nil, errors.New("invalid exact source release identity in hop")
		}
		to, err := releaseMinor(hop.ToRelease)
		if err != nil {
			return nil, nil, errors.New("invalid exact destination release identity in hop")
		}
		if to != from+1 || from < first || to > last {
			return nil, nil, errors.New("hop must be one adjacent minor inside the ordered path")
		}
		if from <= previous {
			return nil, nil, errors.New("hop evidence is duplicated or out of order")
		}
		if hop.ClusterID != in.ClusterID {
			return nil, nil, errors.New("hop cluster identity conflicts with the plan")
		}
		previous = from
		if err := validateHop(hop); err != nil {
			return nil, nil, err
		}
		for _, entry := range []struct {
			minor   int
			release string
		}{{from, hop.FromRelease}, {to, hop.ToRelease}} {
			canon := canonical(entry.release)
			if releases[entry.minor] != "" && releases[entry.minor] != canon {
				return nil, nil, errors.New("conflicting exact hop releases")
			}
			releases[entry.minor] = canon
		}
		hops[from] = hop
	}
	return releases, hops, nil
}

func validateHop(h model.HopInput) error {
	if len(h.Servers) > 1024 || len(h.Order) > 4096 || len(h.Receipts) > 64 {
		return errors.New("hop evidence exceeds bounds")
	}
	if h.EvidenceOwner != "" && !safeText.MatchString(h.EvidenceOwner) {
		return errors.New("invalid evidence owner")
	}
	if (h.InventoryFile == "") != (h.InventorySHA256 == "") {
		return errors.New("hop inventory file and digest must be supplied together")
	}
	seen := make(map[string]bool, len(h.Servers))
	for _, server := range h.Servers {
		if !safeText.MatchString(server) || seen[server] {
			return errors.New("invalid or duplicate server identity")
		}
		seen[server] = true
	}
	seen = make(map[string]bool, len(h.Order))
	for _, member := range h.Order {
		if !safeText.MatchString(member) || seen[member] {
			return errors.New("invalid or duplicate order identity")
		}
		seen[member] = true
	}
	allowed := map[string]bool{
		"backup": true, "restore-drill": true, "token-custody": true,
		"configuration-preserved": true, "post-hop-health": true,
		"sqlite-backup": true, "etcd-snapshot": true, "external-db-owner-backup": true,
	}
	seen = make(map[string]bool, len(h.Receipts))
	for _, receipt := range h.Receipts {
		if !allowed[receipt.Kind] || seen[receipt.Kind] ||
			!safeText.MatchString(receipt.Owner) ||
			!safeText.MatchString(receipt.EvidenceID) ||
			(receipt.Status != "PASS" && receipt.Status != "FAIL" && receipt.Status != "UNKNOWN") ||
			(receipt.Notes != "" && !safeText.MatchString(receipt.Notes)) {
			return errors.New("invalid sanitized operator receipt")
		}
		seen[receipt.Kind] = true
	}
	return nil
}

func aggregate(plan *model.UpgradePlan) {
	plan.Decision = "NO_KNOWN_BLOCKERS"
	plan.ExitCode = 0
	for _, hop := range plan.Hops {
		switch hop.Assessment.ExitCode {
		case 1:
			plan.ExitCode = 1
		case 2:
			if plan.ExitCode != 1 {
				plan.ExitCode = 2
			}
		case 3:
			if plan.ExitCode == 0 {
				plan.ExitCode = 3
			}
		}
	}
	switch plan.ExitCode {
	case 1:
		plan.Decision = "INVALID_INPUT"
	case 2:
		plan.Decision = "BLOCKED"
	case 3:
		plan.Decision = "INCOMPLETE"
	}
}

func checked(scope, kind, state, reason string) model.CheckResult {
	severity := "INFO"
	if state == "UNKNOWN" {
		severity = "WARNING"
	}
	if state == "FAIL" {
		severity = "BLOCKER"
	}
	return model.CheckResult{
		ID:       model.ID("plan", scope, kind),
		Domain:   "plan-" + kind,
		Scope:    scope,
		State:    state,
		Severity: severity,
		Reason:   reason,
	}
}

func endpointMinor(value string) (int, error) {
	normalized := strings.TrimPrefix(value, "v")
	for minor := observedMinMinor; minor <= maxTargetMinor; minor++ {
		if normalized == fmt.Sprintf("1.%d", minor) || normalized == fmt.Sprintf("1.%d.0", minor) {
			return minor, nil
		}
	}
	return 0, errors.New("unsupported plan endpoint; expected 1.32, 1.33, 1.34 or 1.35")
}

func minorName(minor int) string {
	return fmt.Sprintf("1.%d", minor)
}

func releaseMinor(value string) (int, error) {
	match := releasePattern.FindStringSubmatch(value)
	if match == nil {
		return 0, errors.New("invalid exact K3s release identity")
	}
	minor, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, errors.New("invalid exact K3s release identity")
	}
	return minor, nil
}

func versionMinor(value string) (int, error) {
	match := kubePattern.FindStringSubmatch(value)
	if match == nil {
		return 0, errors.New("unparseable Kubernetes version")
	}
	minor, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, errors.New("unparseable Kubernetes version")
	}
	return minor, nil
}

func versionPatch(value string) (int, bool) {
	match := kubePattern.FindStringSubmatch(value)
	if match == nil {
		return 0, false
	}
	patch, err := strconv.Atoi(match[2])
	if err != nil {
		return 0, false
	}
	return patch, true
}

func releasePatch(value string) int {
	match := releasePattern.FindStringSubmatch(value)
	if match == nil {
		return -1
	}
	patch, err := strconv.Atoi(match[2])
	if err != nil {
		return -1
	}
	return patch
}

func canonical(value string) string {
	if value == "" {
		return ""
	}
	return "v" + strings.TrimPrefix(value, "v")
}

func copyStrings(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func sortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
