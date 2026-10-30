// Package readiness evaluates API, schema and operational observations separately
// from component runtime compatibility. It never contacts Kubernetes or a network.
package readiness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/wussh/kubecompact/data"
	"github.com/wussh/kubecompact/internal/model"
)

var ErrTarget = errors.New("unsupported Kubernetes target; catalog covers 1.35 only")

type lifecycleEntry struct{ Group, Version, Kind, Introduced, Deprecated, Removed, Replacement, Source string }

func targetMinor(target string) (int, error) {
	s := strings.TrimPrefix(target, "v")
	parts := strings.Split(s, ".")
	if len(parts) < 2 || len(parts) > 3 || parts[0] != "1" || parts[1] != "35" {
		return 0, ErrTarget
	}
	if len(parts) == 3 {
		n, e := strconv.Atoi(parts[2])
		if e != nil || n != 0 {
			return 0, ErrTarget
		}
	}
	return 35, nil
}

// ResolveTarget rejects unsupported exact patches before any input collection.
func ResolveTarget(target string) (string, error) {
	if _, err := targetMinor(target); err != nil {
		return "", err
	}
	return "1.35.0", nil
}
func Check(ctx context.Context, inv model.Inventory, opts model.CheckOptions) ([]model.CheckResult, error) {
	target, err := targetMinor(opts.TargetKubernetes)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	var catalog struct{ Entries []lifecycleEntry }
	if json.Unmarshal(data.Lifecycle(), &catalog) != nil {
		return nil, errors.New("API lifecycle catalog invalid")
	}
	entries := map[string]lifecycleEntry{}
	for _, e := range catalog.Entries {
		entries[e.Group+"/"+e.Version+"/"+e.Kind] = e
	}
	// Endpoints deprecation is a policy announcement, not generated API removal metadata.
	entries["/v1/Endpoints"] = lifecycleEntry{Group: "", Version: "v1", Kind: "Endpoints", Introduced: "1.0", Deprecated: "1.33", Replacement: "discovery.k8s.io/v1 EndpointSlice", Source: "https://kubernetes.io/blog/2025/04/24/endpoints-deprecation/"}
	for _, e := range []lifecycleEntry{{Group: "apiextensions.k8s.io", Version: "v1beta1", Kind: "CustomResourceDefinition", Deprecated: "1.16", Removed: "1.22", Replacement: "apiextensions.k8s.io/v1 CustomResourceDefinition"}, {Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition", Introduced: "1.16"}, {Group: "apiregistration.k8s.io", Version: "v1beta1", Kind: "APIService", Deprecated: "1.19", Removed: "1.22", Replacement: "apiregistration.k8s.io/v1 APIService"}, {Group: "apiregistration.k8s.io", Version: "v1", Kind: "APIService", Introduced: "1.10"}} {
		e.Source = "https://kubernetes.io/docs/reference/using-api/deprecation-guide/"
		entries[e.Group+"/"+e.Version+"/"+e.Kind] = e
	}
	var results []model.CheckResult
	for _, r := range inv.Resources {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		scope := r.Ref.Key()
		version := r.Ref.APIVersion
		if parts := strings.Split(version, "/"); len(parts) > 1 {
			version = parts[1]
		}
		e, known := entries[r.Ref.Group+"/"+version+"/"+r.Ref.Kind]
		api := check("api", scope, "UNKNOWN", "WARNING", "API_NOT_IN_LIFECYCLE_CATALOG")
		if known {
			api.State = "PASS"
			api.Severity = "INFO"
			api.Reason = "API_AVAILABLE_AT_TARGET"
			api.Details = map[string]string{"source": e.Source, "catalog": "kubernetes-api-v0.35.0", "replacement": e.Replacement}
			if boundary(e.Introduced) > target {
				api.State = "FAIL"
				api.Severity = "BLOCKER"
				api.Reason = "API_NOT_YET_AVAILABLE"
			} else if boundary(e.Removed) > 0 && target >= boundary(e.Removed) {
				api.State = "FAIL"
				api.Severity = "BLOCKER"
				api.Reason = "API_REMOVED"
				api.Details["removed"] = e.Removed
			} else if boundary(e.Deprecated) > 0 && target >= boundary(e.Deprecated) {
				api.State = "FAIL"
				api.Severity = "WARNING"
				api.Reason = "API_DEPRECATED"
				api.Details["deprecated"] = e.Deprecated
			}
		}
		results = append(results, api)
		switch r.Ref.Kind {
		case "CustomResourceDefinition":
			results = append(results, crdChecks(r, inv)...)
		case "APIService":
			results = append(results, apiServiceChecks(r, inv)...)
		case "MutatingWebhookConfiguration", "ValidatingWebhookConfiguration":
			results = append(results, webhookChecks(r, inv)...)
		case "PodDisruptionBudget":
			results = append(results, pdbCheck(r))
		case "Deployment", "StatefulSet", "DaemonSet", "Pod", "Job":
			results = append(results, workloadCheck(r))
		}
	}
	schemas, err := schemaChecks(ctx, inv, opts)
	if err != nil {
		return nil, err
	}
	results = append(results, schemas...)
	for _, cluster := range inv.Clusters {
		for _, node := range cluster.Nodes {
			r := check("node", cluster.ID+"/"+node.Name, "UNKNOWN", "WARNING", "NODE_VERSION_UNPARSEABLE")
			minor, ok := versionMinor(node.KubeletVersion)
			if ok {
				r.State = "PASS"
				r.Severity = "INFO"
				r.Reason = "KUBELET_WITHIN_TARGET_SKEW"
				if minor > target || target-minor > 3 {
					r.State = "FAIL"
					r.Severity = "BLOCKER"
					r.Reason = "KUBELET_TARGET_SKEW_EXCEEDED"
				}
			}
			if !node.Ready {
				r.State = "FAIL"
				r.Severity = "BLOCKER"
				r.Reason = "NODE_NOT_READY"
			}
			r.Details = map[string]string{"source": "https://kubernetes.io/releases/version-skew-policy/", "scope": "target control plane; drain not executed"}
			results = append(results, r)
		}
		cp := check("control-plane", cluster.ID, "UNKNOWN", "WARNING", "CONTROL_PLANE_MEMBERS_NOT_FULLY_OBSERVED")
		current, ok := versionMinor(cluster.KubernetesVersion)
		if ok && (target-current > 1 || target < current) {
			cp.State = "FAIL"
			cp.Severity = "BLOCKER"
			cp.Reason = "DIRECT_UPGRADE_SEQUENCE_UNSUPPORTED"
		}
		results = append(results, cp)
	}
	results = append(results, usageChecks(opts, entries, target)...)
	results = append(results, check("runtime-validation", "inventory", "NOT_APPLICABLE", "INFO", "STATIC_CHECKS_DO_NOT_EXECUTE_ADMISSION_CEL_CONVERSION_OR_APPLICATIONS"))
	return results, nil
}
func check(domain, scope, state, severity, reason string) model.CheckResult {
	return model.CheckResult{ID: model.ID("readiness/v1", domain, scope, reason), Domain: domain, Scope: scope, State: state, Severity: severity, Reason: reason}
}
func boundary(v string) int {
	_, n := versionMinor(v)
	if !n {
		return 0
	}
	p := strings.Split(strings.TrimPrefix(v, "v"), ".")
	minor, _ := strconv.Atoi(p[1])
	return minor
}
func versionMinor(v string) (int, bool) {
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) < 2 || parts[0] != "1" {
		return 0, false
	}
	n, err := strconv.Atoi(parts[1])
	return n, err == nil && n >= 0
}
func mapping(v any) map[string]any { m, _ := v.(map[string]any); return m }
func array(v any) []any            { a, _ := v.([]any); return a }
func text(v any) string            { s, _ := v.(string); return s }
func integer(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), n == float64(int64(n))
	case int:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case json.Number:
		i, e := n.Int64()
		return i, e == nil
	}
	return 0, false
}
func field(m map[string]any, path ...string) any {
	var v any = m
	for _, p := range path {
		v = mapping(v)[p]
	}
	return v
}
func condition(f map[string]any, name string) (string, bool) {
	for _, v := range array(field(f, "status", "conditions")) {
		m := mapping(v)
		if text(m["type"]) == name {
			return text(m["status"]), true
		}
	}
	return "", false
}
func crdChecks(r model.Resource, inv model.Inventory) []model.CheckResult {
	f := r.Facts
	scope := r.Ref.Key()
	versions := array(field(f, "spec", "versions"))
	storage := 0
	served := map[string]bool{}
	known := map[string]bool{}
	for _, v := range versions {
		m := mapping(v)
		n := text(m["name"])
		known[n] = true
		if m["served"] == true {
			served[n] = true
		}
		if m["storage"] == true {
			storage++
		}
	}
	result := check("crd", scope, "PASS", "INFO", "CRD_VERSION_STORAGE_CONSISTENT")
	if len(versions) == 0 {
		result.State = "UNKNOWN"
		result.Severity = "WARNING"
		result.Reason = "CRD_VERSIONS_UNAVAILABLE"
	} else if storage != 1 || len(served) == 0 {
		result.State = "FAIL"
		result.Severity = "BLOCKER"
		result.Reason = "CRD_REQUIRES_ONE_STORAGE_AND_SERVED_VERSION"
	}
	for _, v := range array(field(f, "status", "storedVersions")) {
		if !known[text(v)] {
			result.State = "FAIL"
			result.Severity = "BLOCKER"
			result.Reason = "CRD_STORED_VERSION_NOT_DECLARED"
		}
	}
	out := []model.CheckResult{result}
	if text(field(f, "spec", "conversion", "strategy")) == "Webhook" {
		conversion := mapping(field(f, "spec", "conversion", "webhook"))
		review := false
		for _, v := range array(conversion["conversionReviewVersions"]) {
			if v == "v1" || v == "v1beta1" {
				review = true
			}
		}
		if !review {
			out = append(out, check("crd-conversion", scope, "FAIL", "BLOCKER", "CONVERSION_REVIEW_VERSION_UNSUPPORTED"))
		}
		out = append(out, serviceCheck("crd-conversion", scope, r.Ref.ClusterID, mapping(field(conversion, "clientConfig", "service")), inv))
	}
	return out
}
func apiServiceChecks(r model.Resource, inv model.Inventory) []model.CheckResult {
	state, ok := condition(r.Facts, "Available")
	result := check("apiservice", r.Ref.Key(), "UNKNOWN", "WARNING", "APISERVICE_AVAILABILITY_UNOBSERVED")
	if ok {
		result.State = "PASS"
		result.Severity = "INFO"
		result.Reason = "APISERVICE_AVAILABLE"
		if state != "True" {
			result.State = "FAIL"
			result.Severity = "BLOCKER"
			result.Reason = "APISERVICE_UNAVAILABLE"
		}
	}
	out := []model.CheckResult{result}
	if svc := mapping(field(r.Facts, "spec", "service")); len(svc) > 0 {
		out = append(out, serviceCheck("apiservice-service", r.Ref.Key(), r.Ref.ClusterID, svc, inv))
	}
	return out
}
func webhookChecks(r model.Resource, inv model.Inventory) []model.CheckResult {
	var out []model.CheckResult
	webhooks := array(r.Facts["webhooks"])
	if len(webhooks) == 0 {
		return []model.CheckResult{check("webhook", r.Ref.Key(), "UNKNOWN", "WARNING", "WEBHOOK_CONFIGURATION_UNAVAILABLE")}
	}
	for i, v := range webhooks {
		w := mapping(v)
		scope := fmt.Sprintf("%s/%d", r.Ref.Key(), i)
		valid := false
		for _, review := range array(w["admissionReviewVersions"]) {
			if review == "v1" || review == "v1beta1" {
				valid = true
			}
		}
		rview := check("webhook-review", scope, "PASS", "INFO", "ADMISSION_REVIEW_VERSION_SUPPORTED")
		if !valid {
			rview.State = "FAIL"
			rview.Severity = "BLOCKER"
			rview.Reason = "ADMISSION_REVIEW_VERSION_UNSUPPORTED"
		}
		out = append(out, rview)
		policy := text(w["failurePolicy"])
		p := check("webhook-policy", scope, "PASS", "INFO", "FAILURE_POLICY_FAIL_CLOSED")
		if policy == "Ignore" {
			p.Reason = "FAILURE_POLICY_FAIL_OPEN"
			p.Severity = "WARNING"
		} else if policy != "" && policy != "Fail" {
			p.State = "FAIL"
			p.Severity = "BLOCKER"
			p.Reason = "FAILURE_POLICY_INVALID"
		}
		out = append(out, p)
		rules := check("webhook-rules", scope, "PASS", "INFO", "ADMISSION_RULES_SCOPED")
		if len(array(w["rules"])) == 0 {
			rules.State = "UNKNOWN"
			rules.Severity = "WARNING"
			rules.Reason = "ADMISSION_RULES_UNAVAILABLE"
		}
		for _, rule := range array(w["rules"]) {
			for _, key := range []string{"apiGroups", "apiVersions", "resources"} {
				for _, v := range array(mapping(rule)[key]) {
					if v == "*" || v == "*/*" {
						rules.Severity = "WARNING"
						rules.Reason = "BROAD_ADMISSION_RULES_REVIEW_REQUIRED"
					}
				}
			}
		}
		out = append(out, rules)
		out = append(out, serviceCheck("webhook-service", scope, r.Ref.ClusterID, mapping(field(w, "clientConfig", "service")), inv))
	}
	return out
}
func serviceCheck(domain, scope, cluster string, svc map[string]any, inv model.Inventory) model.CheckResult {
	r := check(domain, scope, "UNKNOWN", "WARNING", "REFERENCED_SERVICE_OR_ENDPOINTS_UNOBSERVED")
	name, namespace := text(svc["name"]), text(svc["namespace"])
	if name == "" || namespace == "" {
		r.Reason = "EXTERNAL_ENDPOINT_NOT_PROBED"
		return r
	}
	found := false
	for _, resource := range inv.Resources {
		if resource.Ref.ClusterID != cluster || resource.Ref.Namespace != namespace {
			continue
		}
		if resource.Ref.Kind == "Service" && resource.Ref.Name == name {
			found = true
		}
	}
	if !found {
		return r
	}
	for _, resource := range inv.Resources {
		if resource.Ref.ClusterID != cluster || resource.Ref.Namespace != namespace || resource.Ref.Kind != "EndpointSlice" || resource.Labels["kubernetes.io/service-name"] != name {
			continue
		}
		for _, e := range array(resource.Facts["endpoints"]) {
			conditions := mapping(mapping(e)["conditions"])
			if conditions["ready"] == true {
				r.State = "PASS"
				r.Severity = "INFO"
				r.Reason = "REFERENCED_SERVICE_HAS_READY_ENDPOINT"
				return r
			}
		}
	}
	r.Reason = "REFERENCED_SERVICE_HAS_NO_OBSERVED_READY_ENDPOINT"
	return r
}
func pdbCheck(r model.Resource) model.CheckResult {
	out := check("pdb", r.Ref.Key(), "UNKNOWN", "WARNING", "PDB_STATUS_UNOBSERVED")
	if n, ok := integer(field(r.Facts, "status", "disruptionsAllowed")); ok {
		out.State = "PASS"
		out.Severity = "INFO"
		out.Reason = "PDB_ALLOWS_DISRUPTION"
		if n == 0 {
			out.State = "FAIL"
			out.Severity = "WARNING"
			out.Reason = "PDB_BLOCKS_VOLUNTARY_DISRUPTION"
		}
	}
	out.Details = map[string]string{"scope": "worker drain implication only; no drain performed"}
	return out
}
func workloadCheck(r model.Resource) model.CheckResult {
	out := check("workload", r.Ref.Key(), "UNKNOWN", "WARNING", "WORKLOAD_STATUS_UNOBSERVED")
	f := r.Facts
	gen, hasGen := integer(field(f, "metadata", "generation"))
	observed, hasObserved := integer(field(f, "status", "observedGeneration"))
	if hasGen && (!hasObserved || observed < gen) && r.Ref.Kind != "Pod" {
		out.Reason = "WORKLOAD_STATUS_GENERATION_STALE"
		return out
	}
	if r.Ref.Kind == "Pod" {
		state, ok := condition(f, "Ready")
		if !ok {
			return out
		}
		out.State = "PASS"
		out.Severity = "INFO"
		out.Reason = "POD_READY"
		if state != "True" {
			out.State = "FAIL"
			out.Severity = "BLOCKER"
			out.Reason = "POD_NOT_READY"
		}
		return out
	}
	if r.Ref.Kind == "Job" {
		state, ok := condition(f, "Complete")
		if ok && state == "True" {
			out.State = "PASS"
			out.Severity = "INFO"
			out.Reason = "JOB_COMPLETE"
		}
		if state, ok = condition(f, "Failed"); ok && state == "True" {
			out.State = "FAIL"
			out.Severity = "BLOCKER"
			out.Reason = "JOB_FAILED"
		}
		return out
	}
	desired := int64(1)
	actualKey := "readyReplicas"
	if n, ok := integer(field(f, "spec", "replicas")); ok {
		desired = n
	}
	if r.Ref.Kind == "Deployment" {
		actualKey = "availableReplicas"
	}
	if r.Ref.Kind == "DaemonSet" {
		var ok bool
		desired, ok = integer(field(f, "status", "desiredNumberScheduled"))
		if !ok {
			return out
		}
		actualKey = "numberReady"
	}
	actual, ok := integer(field(f, "status", actualKey))
	if !ok {
		return out
	}
	out.State = "PASS"
	out.Severity = "INFO"
	out.Reason = "WORKLOAD_REPLICAS_READY"
	if actual < desired {
		out.State = "FAIL"
		out.Severity = "BLOCKER"
		out.Reason = "WORKLOAD_REPLICAS_UNREADY"
	}
	return out
}
