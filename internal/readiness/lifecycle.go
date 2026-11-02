package readiness

import (
	"strings"

	"github.com/wussh/kubecompact/internal/model"
)

// lifecycleResult evaluates pinned metadata; target validation remains at the CLI boundary.
func lifecycleResult(e lifecycleEntry, known bool, scope string, target int, entries map[string]lifecycleEntry) model.CheckResult {
	api := check("api", scope, "UNKNOWN", "WARNING", "API_NOT_IN_LIFECYCLE_CATALOG")
	if !known {
		return api
	}
	api.State, api.Severity, api.Reason = "PASS", "INFO", "API_AVAILABLE_AT_TARGET"
	api.Details = map[string]string{"source": e.Source, "catalog": "kubernetes-api-v0.35.0", "replacement": e.Replacement}
	api.Details["replacementAvailable"] = "not-applicable"
	if e.Replacement != "" {
		api.Details["replacementAvailable"] = "unknown"
		if replacement, ok := entries[strings.Replace(e.Replacement, " ", "/", 1)]; ok && boundary(replacement.Introduced) > 0 {
			api.Details["replacementAvailable"] = "false"
			if target >= boundary(replacement.Introduced) {
				api.Details["replacementAvailable"] = "true"
			}
			api.Details["replacementIntroduced"] = replacement.Introduced
			api.Details["replacementSource"] = replacement.Source
		}
	}
	if boundary(e.Introduced) > target {
		api.State, api.Severity, api.Reason = "FAIL", "BLOCKER", "API_NOT_YET_AVAILABLE"
	} else if boundary(e.Removed) > 0 && target >= boundary(e.Removed) {
		api.State, api.Severity, api.Reason = "FAIL", "BLOCKER", "API_REMOVED"
		api.Details["removed"] = e.Removed
	} else if boundary(e.Deprecated) > 0 && target >= boundary(e.Deprecated) {
		api.State, api.Severity, api.Reason = "FAIL", "WARNING", "API_DEPRECATED"
		api.Details["deprecated"] = e.Deprecated
	}
	return api
}
