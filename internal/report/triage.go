package report

import (
	"encoding/json"
	"fmt"
	"github.com/wussh/kubecompact/internal/model"
	"reflect"
	"sort"
	"strings"
)

type findingGroup struct {
	Domain, State, Severity, Reason, Action string
	Checks                                  []model.CheckResult
}

func reportChecks(a model.Assessment) []model.CheckResult {
	type key struct{ id, scope string }
	total := len(a.Checks)
	for _, c := range a.Components {
		total += len(c.Checks)
	}
	if total == 0 {
		return a.Checks
	}
	checks := make([]model.CheckResult, 0, total)
	byIdentity := make(map[key][]int, total)
	add := func(c model.CheckResult) {
		k := key{c.ID, c.Scope}
		for _, i := range byIdentity[k] {
			if reflect.DeepEqual(checks[i], c) {
				return
			}
		}
		byIdentity[k] = append(byIdentity[k], len(checks))
		checks = append(checks, c)
	}
	for _, c := range a.Checks {
		add(c)
	}
	for _, component := range a.Components {
		for _, c := range component.Checks {
			add(c)
		}
	}
	return checks
}

func coverageAction(c model.Coverage) string {
	if c.State != "PARTIAL" && c.State != "UNREADABLE" {
		return ""
	}
	return nextEvidence(c.Code)
}

func inventorySummary(a model.Assessment) string {
	installations, workloads, images := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, c := range a.Components {
		installations[c.Component.ID] = true
		for _, r := range c.Component.Resources {
			switch r.Kind {
			case "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "ReplicationController", "Job", "CronJob":
				workloads[r.Key()] = true
			}
		}
		for _, im := range c.Component.Images {
			if im.Role != "running" {
				continue
			}
			identity := im.ImageID
			if identity == "" {
				identity = im.Digest
			}
			if identity == "" {
				identity = im.Image
			}
			if identity != "" {
				images[identity] = true
			}
		}
	}
	return fmt.Sprintf("%d installations; %d workloads; %d observed running images", len(installations), len(workloads), len(images))
}

func nextEvidence(reason string) string {
	reason = strings.ToUpper(reason)
	switch reason {
	case "API_REMOVED":
		return "Review the scoped authored API and catalog replacement, if supplied; re-check the original input."
	case "RAW_DOCUMENT_UNAVAILABLE":
		return "Check the original manifest or rendered input; sanitized inventory cannot reconstruct the document."
	case "CACHE_UNAVAILABLE":
		return "Prepare the evidence cache separately using approved sources, then repeat the offline assessment."
	case "AUDIT_OR_DEPRECATION_METRICS_NOT_PROVIDED", "USAGE_UNAVAILABLE":
		return "Supply an approved deprecated-usage aggregate with explicit interval and completeness; do not infer request counts from presence gauges."
	}
	switch reason {
	case "APPLICATION IDENTITY OR UNAMBIGUOUS DEPLOYED/RUNNING SEMANTIC VERSION IS MISSING":
		return "Supply observed deployed/running version evidence and a reviewed identity mapping for this installation; chart metadata alone is insufficient."
	}
	return "No mapped next action; inspect the scoped evidence and details."
}

func componentAction(status, reason string) string {
	if status != string(model.Unknown) && status != string(model.NotTested) {
		return ""
	}
	return nextEvidence(reason)
}

func checkAction(c model.CheckResult) string {
	if c.Severity == "INFO" || (c.State != "FAIL" && c.State != "UNKNOWN") {
		return ""
	}
	return nextEvidence(c.Reason)
}

func groupPreview(g findingGroup) string {
	scopes := make([]string, 0, 3)
	for _, c := range g.Checks {
		if len(scopes) == 3 {
			break
		}
		identity := c.Details["apiVersion"] + " " + c.Details["kind"] + " " + c.Details["namespace"] + "/" + c.Details["name"]
		text := c.ID + " scope " + c.Scope + " " + strings.TrimSpace(identity)
		if replacement := c.Details["replacement"]; replacement != "" {
			text += "; catalog replacement " + replacement
			if availability := c.Details["replacementAvailable"]; availability != "" {
				text += " (available at target: " + availability + ")"
			}
		}
		if len(c.EvidenceIDs) > 0 {
			text += "; evidence " + strings.Join(c.EvidenceIDs, ", ")
		}
		if len(c.Details) > 0 {
			details, _ := json.Marshal(c.Details)
			text += "; details " + string(details)
		}
		scopes = append(scopes, text)
	}
	return strings.Join(scopes, "; ")
}

func triageGroups(checks []model.CheckResult) []findingGroup {
	groups := []findingGroup{}
	indexes := map[string]int{}
	for _, c := range checks {
		key := strings.Join([]string{c.Domain, c.State, c.Severity, c.Reason}, "\x00")
		i, ok := indexes[key]
		if !ok {
			i = len(groups)
			indexes[key] = i
			groups = append(groups, findingGroup{Domain: c.Domain, State: c.State, Severity: c.Severity, Reason: c.Reason, Action: checkAction(c)})
		}
		groups[i].Checks = append(groups[i].Checks, c)
	}
	rank := func(g findingGroup) int {
		if g.State == "FAIL" && g.Severity == "BLOCKER" {
			return 0
		}
		if g.State == "UNKNOWN" && g.Severity != "INFO" {
			return 1
		}
		return 2
	}
	sort.SliceStable(groups, func(i, j int) bool { return rank(groups[i]) < rank(groups[j]) })
	return groups
}
