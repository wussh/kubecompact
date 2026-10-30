package readiness

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/wussh/kubecompact/internal/model"
	"io"
	"os"
	"strconv"
	"time"
)

type usageSummary struct {
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	Complete bool      `json:"complete"`
	Requests []struct {
		Group   string `json:"group"`
		Version string `json:"version"`
		Kind    string `json:"kind"`
		Count   int64  `json:"count"`
	} `json:"requests"`
}

func usageChecks(opts model.CheckOptions, entries map[string]lifecycleEntry, target int) []model.CheckResult {
	unknown := check("client-request-usage", "inventory", "UNKNOWN", "WARNING", "AUDIT_OR_DEPRECATION_METRICS_NOT_PROVIDED")
	if opts.DeprecatedUsageFile == "" {
		return []model.CheckResult{unknown}
	}
	f, e := os.Open(opts.DeprecatedUsageFile)
	if e != nil {
		unknown.Reason = "USAGE_SUMMARY_UNREADABLE"
		return []model.CheckResult{unknown}
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		unknown.Reason = "USAGE_SUMMARY_UNSAFE"
		return []model.CheckResult{unknown}
	}
	b, e := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if e != nil || len(b) > 1<<20 || uniqueJSON(json.NewDecoder(bytes.NewReader(b)), 0) != nil {
		unknown.Reason = "USAGE_SUMMARY_INVALID"
		return []model.CheckResult{unknown}
	}
	var summary usageSummary
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if dec.Decode(&summary) != nil {
		unknown.Reason = "USAGE_SUMMARY_INVALID"
		return []model.CheckResult{unknown}
	}
	if _, err := dec.Token(); err != io.EOF {
		unknown.Reason = "USAGE_SUMMARY_INVALID"
		return []model.CheckResult{unknown}
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	if summary.Start.IsZero() || !summary.End.After(summary.Start) || summary.End.After(now) || summary.Requests == nil {
		unknown.Reason = "USAGE_INTERVAL_INVALID"
		return []model.CheckResult{unknown}
	}
	unknown.Details = map[string]string{"start": summary.Start.UTC().Format(time.RFC3339), "end": summary.End.UTC().Format(time.RFC3339), "scope": "caller-supplied observation interval only"}
	if summary.Complete {
		unknown.State = "PASS"
		unknown.Severity = "INFO"
		unknown.Reason = "NO_DEPRECATED_REQUESTS_IN_SUPPLIED_INTERVAL"
	}
	var out []model.CheckResult
	for _, request := range summary.Requests {
		if request.Count < 0 || request.Version == "" || request.Kind == "" {
			unknown.State = "UNKNOWN"
			unknown.Severity = "WARNING"
			unknown.Reason = "USAGE_REQUEST_INVALID"
			continue
		}
		if request.Count == 0 {
			continue
		}
		entry, ok := entries[request.Group+"/"+request.Version+"/"+request.Kind]
		if !ok {
			unknown.State = "UNKNOWN"
			unknown.Severity = "WARNING"
			unknown.Reason = "USAGE_API_OUTSIDE_CATALOG"
			continue
		}
		if boundary(entry.Removed) > 0 && target >= boundary(entry.Removed) || boundary(entry.Deprecated) > 0 && target >= boundary(entry.Deprecated) {
			r := check("client-request-usage", model.ID(request.Group, request.Version, request.Kind), "FAIL", "WARNING", "DEPRECATED_CLIENT_REQUEST_OBSERVED")
			if boundary(entry.Removed) > 0 && target >= boundary(entry.Removed) {
				r.Severity = "BLOCKER"
				r.Reason = "CLIENT_REQUEST_API_REMOVED_AT_TARGET"
			}
			r.Details = map[string]string{"source": entry.Source, "count": strconv.FormatInt(request.Count, 10), "start": summary.Start.UTC().Format(time.RFC3339), "end": summary.End.UTC().Format(time.RFC3339), "replacement": entry.Replacement}
			out = append(out, r)
			if unknown.State == "PASS" {
				unknown.Reason = "DEPRECATED_REQUESTS_PRESENT_IN_SUPPLIED_INTERVAL"
			}
		}
	}
	return append([]model.CheckResult{unknown}, out...)
}
func uniqueJSON(d *json.Decoder, depth int) error {
	if depth > 64 {
		return errors.New("JSON nesting exceeded")
	}
	token, e := d.Token()
	if e != nil {
		return e
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, e := d.Token()
			if e != nil {
				return e
			}
			s, ok := key.(string)
			if !ok || seen[s] {
				return errors.New("duplicate JSON key")
			}
			seen[s] = true
			if e = uniqueJSON(d, depth+1); e != nil {
				return e
			}
		}
		_, e = d.Token()
		return e
	case '[':
		for d.More() {
			if e = uniqueJSON(d, depth+1); e != nil {
				return e
			}
		}
		_, e = d.Token()
		return e
	}
	return errors.New("invalid JSON delimiter")
}
