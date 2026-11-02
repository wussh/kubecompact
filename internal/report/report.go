package report

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"unicode"

	"github.com/wussh/kubecompact/internal/model"
)

type row struct {
	ID, Name, Namespace, Controller, Project, Chart, Application, Running, Target, Status, Recommendation, Explanation, Quality string
	Evidence                                                                                                                    []model.Evidence
	EvidenceIDs                                                                                                                 []string
}
type page struct {
	Assessment model.Assessment
	Rows       []row
	Total      int
	Summary    string
	Groups     []findingGroup
}

func matches(c model.ComponentAssessment, f model.Filters, evidence map[string]model.Evidence) bool {
	if f.Namespace != "" && c.Component.Namespace != f.Namespace {
		return false
	}
	if f.Controller != "" && !strings.Contains(strings.ToLower(strings.Join(c.Component.ManagedBy, " ")), strings.ToLower(f.Controller)) {
		return false
	}
	if f.Component != "" && !strings.Contains(strings.ToLower(c.Component.Name+" "+c.Component.Project), strings.ToLower(f.Component)) {
		return false
	}
	if f.Status != "" && !strings.EqualFold(string(c.Verdict), f.Status) {
		return false
	}
	if f.EvidenceQuality != "" {
		for _, id := range c.EvidenceIDs {
			if strings.EqualFold(evidence[id].Confidence, f.EvidenceQuality) {
				return true
			}
		}
		return strings.EqualFold(f.EvidenceQuality, "NONE") && len(c.EvidenceIDs) == 0
	}
	return true
}
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}
func csvCell(s string) string {
	stripped := strings.TrimLeft(s, " \t\r\n")
	if stripped != "" && strings.ContainsRune("=+-@", rune(stripped[0])) {
		return "'" + s
	}
	if strings.HasPrefix(s, "\t") || strings.HasPrefix(s, "\r") || strings.HasPrefix(s, "\n") {
		return "'" + s
	}
	return s
}
func rowsFor(a model.Assessment, evidence map[string]model.Evidence) []row {
	rows := make([]row, 0, len(a.Components))
	for _, c := range a.Components {
		r := row{ID: c.Component.ID, Name: c.Component.Name, Namespace: c.Component.Namespace, Controller: strings.Join(c.Component.ManagedBy, " > "), Project: c.Component.Project, Target: a.TargetKubernetes, Status: string(c.Verdict), Explanation: c.Reason, Quality: "NONE"}
		r.EvidenceIDs = c.EvidenceIDs
		if c.Component.Chart != nil {
			r.Chart = c.Component.Chart.Name + "@" + c.Component.Chart.Version
			r.Application = c.Component.Chart.AppVersion + " (chart metadata)"
		}
		for _, v := range c.Component.Versions {
			if v.Scheme == "app" && v.Role == "deployed" {
				r.Application = v.Raw + " (deployed)"
			}
		}
		images := make([]string, 0, len(c.Component.Images))
		for _, im := range c.Component.Images {
			if im.Role != "running" {
				continue
			}
			s := im.Image
			if im.ImageID != "" {
				s += " [" + im.ImageID + "]"
			}
			images = append(images, s)
		}
		sort.Strings(images)
		r.Running = strings.Join(images, "; ")
		if c.Recommendation != nil {
			r.Recommendation = c.Recommendation.Version
			if c.Recommendation.ChartVersion != "" {
				r.Recommendation += " (chart " + c.Recommendation.ChartVersion + ")"
			}
			if len(c.Recommendation.Prerequisites) > 0 {
				r.Recommendation += "; requires: " + strings.Join(c.Recommendation.Prerequisites, "; ")
			}
		}
		qualities := map[string]bool{}
		for _, id := range c.EvidenceIDs {
			if e, ok := evidence[id]; ok {
				e.URL = model.SafeURL(e.URL)
				r.Evidence = append(r.Evidence, e)
				if e.Confidence != "" {
					qualities[e.Confidence] = true
				}
			}
		}
		if len(qualities) > 0 {
			values := make([]string, 0, len(qualities))
			for quality := range qualities {
				values = append(values, quality)
			}
			sort.Strings(values)
			r.Quality = strings.Join(values, ",")
		}
		rows = append(rows, r)
	}
	return rows
}

// Write applies presentation filters without changing the full assessment's gating state.
func Write(w io.Writer, a model.Assessment, format string, f model.Filters) error {
	evidence := make(map[string]model.Evidence, len(a.Evidence))
	for _, e := range a.Evidence {
		evidence[e.ID] = e
	}
	a.Checks = reportChecks(a)
	total := len(a.Components)
	var summary string
	var groups []findingGroup
	if format == "table" || format == "html" {
		summary = inventorySummary(a)
		groups = triageGroups(a.Checks)
	}
	filtered := make([]model.ComponentAssessment, 0, total)
	for _, c := range a.Components {
		if matches(c, f, evidence) {
			filtered = append(filtered, c)
		}
	}
	a.Components = filtered
	rows := rowsFor(a, evidence)
	switch format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(a)
	case "csv":
		cw := csv.NewWriter(w)
		if err := cw.Write([]string{"id", "component", "namespace", "managed_by", "project", "chart_version", "application_version", "running_images", "target_kubernetes", "assessment", "recommended_version", "explanation", "evidence_urls", "retrieved_at", "confidence", "overall_decision", "overall_exit_code", "evidence_ids", "row_type", "domain", "scope", "severity", "details", "collector", "kinds", "omissions"}); err != nil {
			return err
		}
		for _, r := range rows {
			urls, dates := []string{}, []string{}
			for _, e := range r.Evidence {
				urls = append(urls, e.URL)
				dates = append(dates, e.RetrievedAt.Format("2006-01-02T15:04:05Z07:00"))
			}
			fields := []string{r.ID, r.Name, r.Namespace, r.Controller, r.Project, r.Chart, r.Application, r.Running, r.Target, r.Status, r.Recommendation, r.Explanation, strings.Join(urls, "; "), strings.Join(dates, "; "), r.Quality, a.Decision, fmt.Sprint(a.ExitCode), strings.Join(r.EvidenceIDs, "; "), "component", "", "", "", "", "", "", ""}
			for i := range fields {
				fields[i] = csvCell(fields[i])
			}
			if err := cw.Write(fields); err != nil {
				return err
			}
		}
		// Summary and global records are independent of component presentation filters.
		writeRecord := func(kind, id, state, reason, domain, scope, severity, details, collector, kinds, omissions string, ids []string) error {
			fields := make([]string, 26)
			fields[0], fields[8], fields[9], fields[11] = id, a.TargetKubernetes, state, reason
			fields[15], fields[16], fields[17], fields[18] = a.Decision, fmt.Sprint(a.ExitCode), strings.Join(ids, "; "), kind
			fields[19], fields[20], fields[21], fields[22] = domain, scope, severity, details
			fields[23], fields[24], fields[25] = collector, kinds, omissions
			for _, id := range ids {
				if e, ok := evidence[id]; ok {
					if fields[12] != "" {
						fields[12] += "; "
					}
					fields[12] += model.SafeURL(e.URL)
				}
			}
			for i := range fields {
				fields[i] = csvCell(fields[i])
			}
			return cw.Write(fields)
		}
		if err := writeRecord("summary", a.ScanID, a.Decision, "", "", "", "", "", "", "", "", nil); err != nil {
			return err
		}
		for _, c := range a.Checks {
			details, err := json.Marshal(c.Details)
			if err != nil {
				return err
			}
			if err := writeRecord("check", c.ID, c.State, c.Reason, c.Domain, c.Scope, c.Severity, string(details), "", "", "", c.EvidenceIDs); err != nil {
				return err
			}
		}
		for _, c := range a.Coverage {
			if err := writeRecord("coverage", "", c.State, c.Code, "", c.Scope, "", "", c.Collector, strings.Join(c.Kinds, "; "), strings.Join(c.Omissions, "; "), nil); err != nil {
				return err
			}
		}
		cw.Flush()
		return cw.Error()
	case "table":
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		if _, err := fmt.Fprintf(tw, "Decision: %s (exit %d); target %s; showing %d/%d components\n", a.Decision, a.ExitCode, a.TargetKubernetes, len(rows), total); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(tw, summary); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(tw, "COMPONENT\tNAMESPACE\tMANAGED BY\tCHART\tAPPLICATION\tRUNNING\tTARGET\tASSESSMENT\tRECOMMENDATION\tEVIDENCE"); err != nil {
			return err
		}
		for _, r := range rows {
			urls := []string{}
			for _, e := range r.Evidence {
				urls = append(urls, e.ID+" "+e.URL+" ["+e.RetrievedAt.Format("2006-01-02")+", "+e.Confidence+"]")
			}
			if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", clean(r.Name), clean(r.Namespace), clean(r.Controller), clean(r.Chart), clean(r.Application), clean(r.Running), r.Target, r.Status, clean(r.Recommendation), clean(strings.Join(urls, "; "))); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(tw, "  [%s] %s\n  Evidence IDs: %s\n  Next evidence: %s\n", clean(r.ID), clean(r.Explanation), clean(strings.Join(r.EvidenceIDs, "; ")), componentAction(r.Status, r.Explanation)); err != nil {
				return err
			}
		}
		for _, c := range a.Coverage {
			action := coverageAction(c)
			if action != "" {
				action = "; next evidence: " + action
			}
			if _, err := fmt.Fprintf(tw, "Coverage: %s %s %s; scope %s; kinds %s; omissions %s%s\n", clean(c.Collector), c.State, clean(c.Code), clean(c.Scope), clean(strings.Join(c.Kinds, ", ")), clean(strings.Join(c.Omissions, "; ")), action); err != nil {
				return err
			}
		}
		for _, g := range groups {
			if _, err := fmt.Fprintf(tw, "%s: %s [%s] %s (%d findings)\n  Next evidence: %s\n  Representative findings (up to 3): %s\n  All IDs/evidence/raw details: JSON/CSV or expanded HTML group.\n", clean(g.Domain), g.State, g.Severity, clean(g.Reason), len(g.Checks), clean(g.Action), clean(groupPreview(g))); err != nil {
				return err
			}
		}
		return tw.Flush()
	case "html":
		return htmlReport.Execute(w, page{Assessment: a, Rows: rows, Total: total, Summary: summary, Groups: groups})
	default:
		return errors.New("unsupported report format")
	}
}

var htmlReport = template.Must(template.New("report").Funcs(template.FuncMap{"coverageAction": coverageAction, "componentAction": componentAction}).Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="referrer" content="no-referrer"><title>KubeCompat assessment</title>
<style>body{overflow-wrap:anywhere}</style>
<style>body{font:15px/1.5 system-ui,sans-serif;margin:24px;color:#202830;background:#fff}h1{margin-bottom:4px}.notice{padding:12px;border:2px solid #586778;border-radius:6px}form{display:flex;flex-wrap:wrap;gap:12px;margin:20px 0}label{display:flex;flex-direction:column;font-weight:600}input,select{font:inherit;padding:6px;min-width:130px}button{font:inherit;padding:6px 12px}.scroll{overflow-x:auto}table{width:100%;border-collapse:collapse}th,td{padding:9px;text-align:left;vertical-align:top;border:1px solid #cbd2d9;min-width:100px;overflow-wrap:anywhere}th{background:#eef1f4}td small{display:block;color:#465568}a{color:#154d9a}details{margin-top:18px}code{overflow-wrap:anywhere}input:focus-visible,button:focus-visible,a:focus-visible,summary:focus-visible{outline:3px solid #2969d5;outline-offset:2px}[hidden]{display:none!important}@media(prefers-color-scheme:dark){body{background:#18212b;color:#e3e9ef}th{background:#283645}td small{color:#bbc8d5}a{color:#99c8ff}input,select,button{background:#253241;color:#e3e9ef;border:1px solid #8293a5}}@media(max-width:600px){body{margin:12px}h1{font-size:24px}}@media print{form{display:none}.scroll{overflow:visible}body{font-size:11px}details{display:block}}</style></head>
<body><h1>KubeCompat</h1><p>Evidence-backed Kubernetes upgrade assessment</p>
<section class="notice" aria-label="Overall assessment"><strong>Overall: {{.Assessment.Decision}} — exit {{.Assessment.ExitCode}}</strong><br>Target Kubernetes: {{.Assessment.TargetKubernetes}}; schema target: {{.Assessment.ResolvedTarget}}<br>Assessed: {{.Assessment.AssessedAt}}; upgrade path: {{.Assessment.UpgradePathStatus}}<br>Documented support is not runtime certification. Filtered views never change this overall decision.</section>
<p>{{.Summary}} (full assessment; unaffected by filters).</p>
<section aria-label="Prioritized findings"><h2>Domain triage</h2>{{range .Groups}}<details><summary>{{.Domain}}: {{.State}} [{{.Severity}}] {{.Reason}} — {{len .Checks}} findings</summary><p>Next evidence: {{.Action}}</p><ul>{{range .Checks}}<li><strong>{{.ID}}</strong> — {{.Scope}}; {{.State}} [{{.Severity}}]; evidence: {{range .EvidenceIDs}}{{.}} {{end}}<dl>{{range $key, $value := .Details}}<dt>{{$key}}</dt><dd>{{$value}}</dd>{{end}}</dl></li>{{end}}</ul></details>{{else}}<p>No checks supplied.</p>{{end}}</section>
<form id="filters" aria-label="Filter components"><label>Namespace<input name="ns" autocomplete="off"></label><label>Controller<input name="controller" autocomplete="off"></label><label>Component<input name="component" autocomplete="off"></label><label>Assessment<select name="status"><option value="">All</option><option>COMPATIBLE</option><option>INCOMPATIBLE</option><option>UPGRADE_REQUIRED</option><option>NOT_TESTED</option><option>UNKNOWN</option></select></label><label>Evidence quality<input name="quality" autocomplete="off" placeholder="HIGH, MEDIUM, LOW, NONE"></label><button type="reset">Reset filters</button></form>
<p id="count" role="status">Showing {{len .Rows}} of {{.Total}} components.</p>
<div class="scroll" tabindex="0" role="region" aria-label="Component compatibility table"><table><caption>Component inventory, documented support and evidence</caption><thead><tr><th scope="col">Component</th><th scope="col">Management</th><th scope="col">Versions</th><th scope="col">Assessment</th><th scope="col">Recommendation</th><th scope="col">Evidence</th></tr></thead><tbody>
{{range .Rows}}<tr data-component-id="{{.ID}}" data-ns="{{.Namespace}}" data-controller="{{.Controller}}" data-component="{{.Name}} {{.Project}}" data-status="{{.Status}}" data-quality="{{.Quality}}"><td>{{.Name}}<small>{{.Namespace}} / {{.Project}}</small></td><td>{{.Controller}}</td><td>Chart: {{.Chart}}<br>Application: {{.Application}}<br>Running: {{.Running}}<small>Target: {{.Target}}</small></td><td><strong>{{.Status}}</strong><br>{{.Explanation}}{{with componentAction .Status .Explanation}}<br>Next evidence: {{.}}{{end}}</td><td>{{.Recommendation}}</td><td>{{range .Evidence}}{{if .URL}}<a data-evidence-id="{{.ID}}" href="{{.URL}}" rel="noreferrer noopener">{{.URL}}</a>{{end}}<small>Evidence: {{.ID}}; retrieved {{.RetrievedAt}}; verified {{.VerifiedAt}}; expires {{.ExpiresAt}}; {{.Confidence}}; P{{.Priority}}; {{.ClaimType}}</small><small>Parser: {{.ParserID}}@{{.ParserVersion}}; source: {{.SourceHash}}</small>{{else}}No authoritative evidence.{{end}}</td></tr>{{else}}<tr><td colspan="6">No matching components. Check coverage below; an empty inventory is not proof of compatibility.</td></tr>{{end}}
</tbody></table></div>
<details><summary>Component evidence references</summary><ul>{{range .Rows}}<li>{{.ID}}: {{range .EvidenceIDs}}{{.}} {{end}}</li>{{end}}</ul></details>
<details open><summary>Collection coverage</summary><ul>{{range .Assessment.Coverage}}<li><strong>{{.Collector}}: {{.State}}</strong> — {{.Scope}} {{.Code}} {{range .Omissions}}<span>{{.}}; </span>{{end}}{{with coverageAction .}} Next evidence: {{.}}{{end}}</li>{{else}}<li>No coverage records supplied.</li>{{end}}</ul></details>
<p>Schema: {{.Assessment.SchemaVersion}}; scanner: {{.Assessment.ScannerVersion}}; scan ID: <code>{{.Assessment.ScanID}}</code>. This standalone report performs no network requests.</p>
<script>"use strict";const form=document.getElementById("filters"),rows=Array.from(document.querySelectorAll("tbody tr[data-component]")),count=document.getElementById("count");function filter(){let n=0;for(const row of rows){let show=true;for(const key of ["ns","controller","component","status","quality"]){const value=form.elements[key].value.trim().toLowerCase(),actual=row.dataset[key].toLowerCase();const match=key==="quality"?actual.split(",").includes(value):(["ns","status"].includes(key)?actual===value:actual.includes(value));if(value&&!match){show=false;break;}}row.hidden=!show;if(show)n++;}count.textContent="Showing "+n+" of {{.Total}} components. Overall decision is unchanged.";}form.addEventListener("input",filter);form.addEventListener("submit",event=>event.preventDefault());form.addEventListener("reset",()=>setTimeout(filter,0));</script></body></html>`))
