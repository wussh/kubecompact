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
}
type page struct {
	Assessment model.Assessment
	Rows       []row
	Total      int
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
	total := len(a.Components)
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
		if err := cw.Write([]string{"id", "component", "namespace", "managed_by", "project", "chart_version", "application_version", "running_images", "target_kubernetes", "assessment", "recommended_version", "explanation", "evidence_urls", "retrieved_at", "confidence", "overall_decision", "overall_exit_code", "evidence_ids"}); err != nil {
			return err
		}
		for _, r := range rows {
			urls, dates, ids := []string{}, []string{}, []string{}
			for _, e := range r.Evidence {
				urls = append(urls, e.URL)
				dates = append(dates, e.RetrievedAt.Format("2006-01-02T15:04:05Z07:00"))
				ids = append(ids, e.ID)
			}
			fields := []string{r.ID, r.Name, r.Namespace, r.Controller, r.Project, r.Chart, r.Application, r.Running, r.Target, r.Status, r.Recommendation, r.Explanation, strings.Join(urls, "; "), strings.Join(dates, "; "), r.Quality, a.Decision, fmt.Sprint(a.ExitCode), strings.Join(ids, "; ")}
			for i := range fields {
				fields[i] = csvCell(fields[i])
			}
			if err := cw.Write(fields); err != nil {
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
			if _, err := fmt.Fprintf(tw, "  [%s] %s\n", clean(r.ID), clean(r.Explanation)); err != nil {
				return err
			}
		}
		for _, c := range a.Coverage {
			if c.State == "PARTIAL" || c.State == "UNREADABLE" {
				if _, err := fmt.Fprintf(tw, "Coverage: %s %s %s\n", clean(c.Collector), c.State, clean(c.Code)); err != nil {
					return err
				}
			}
		}
		for _, c := range a.Checks {
			if _, err := fmt.Fprintf(tw, "Check %s: %s [%s] %s\n", clean(c.ID), c.State, c.Severity, clean(c.Reason)); err != nil {
				return err
			}
		}
		return tw.Flush()
	case "html":
		return htmlReport.Execute(w, page{Assessment: a, Rows: rows, Total: total})
	default:
		return errors.New("unsupported report format")
	}
}

var htmlReport = template.Must(template.New("report").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="referrer" content="no-referrer"><title>KubeCompat assessment</title>
<style>body{overflow-wrap:anywhere}</style>
<style>body{font:15px/1.5 system-ui,sans-serif;margin:24px;color:#202830;background:#fff}h1{margin-bottom:4px}.notice{padding:12px;border:2px solid #586778;border-radius:6px}form{display:flex;flex-wrap:wrap;gap:12px;margin:20px 0}label{display:flex;flex-direction:column;font-weight:600}input,select{font:inherit;padding:6px;min-width:130px}button{font:inherit;padding:6px 12px}.scroll{overflow-x:auto}table{width:100%;border-collapse:collapse}th,td{padding:9px;text-align:left;vertical-align:top;border:1px solid #cbd2d9;min-width:100px;overflow-wrap:anywhere}th{background:#eef1f4}td small{display:block;color:#465568}a{color:#154d9a}details{margin-top:18px}code{overflow-wrap:anywhere}input:focus-visible,button:focus-visible,a:focus-visible,summary:focus-visible{outline:3px solid #2969d5;outline-offset:2px}[hidden]{display:none!important}@media(prefers-color-scheme:dark){body{background:#18212b;color:#e3e9ef}th{background:#283645}td small{color:#bbc8d5}a{color:#99c8ff}input,select,button{background:#253241;color:#e3e9ef;border:1px solid #8293a5}}@media(max-width:600px){body{margin:12px}h1{font-size:24px}}@media print{form{display:none}.scroll{overflow:visible}body{font-size:11px}details{display:block}}</style></head>
<body><h1>KubeCompat</h1><p>Evidence-backed Kubernetes upgrade assessment</p>
<section class="notice" aria-label="Overall assessment"><strong>Overall: {{.Assessment.Decision}} — exit {{.Assessment.ExitCode}}</strong><br>Target Kubernetes: {{.Assessment.TargetKubernetes}}; schema target: {{.Assessment.ResolvedTarget}}<br>Assessed: {{.Assessment.AssessedAt}}; upgrade path: {{.Assessment.UpgradePathStatus}}<br>Documented support is not runtime certification. Filtered views never change this overall decision.</section>
<form id="filters" aria-label="Filter components"><label>Namespace<input name="ns" autocomplete="off"></label><label>Controller<input name="controller" autocomplete="off"></label><label>Component<input name="component" autocomplete="off"></label><label>Assessment<select name="status"><option value="">All</option><option>COMPATIBLE</option><option>INCOMPATIBLE</option><option>UPGRADE_REQUIRED</option><option>NOT_TESTED</option><option>UNKNOWN</option></select></label><label>Evidence quality<input name="quality" autocomplete="off" placeholder="HIGH, MEDIUM, LOW, NONE"></label><button type="reset">Reset filters</button></form>
<p id="count" role="status">Showing {{len .Rows}} of {{.Total}} components.</p>
<div class="scroll" tabindex="0" role="region" aria-label="Component compatibility table"><table><caption>Component inventory, documented support and evidence</caption><thead><tr><th scope="col">Component</th><th scope="col">Management</th><th scope="col">Versions</th><th scope="col">Assessment</th><th scope="col">Recommendation</th><th scope="col">Evidence</th></tr></thead><tbody>
{{range .Rows}}<tr data-component-id="{{.ID}}" data-ns="{{.Namespace}}" data-controller="{{.Controller}}" data-component="{{.Name}} {{.Project}}" data-status="{{.Status}}" data-quality="{{.Quality}}"><td>{{.Name}}<small>{{.Namespace}} / {{.Project}}</small></td><td>{{.Controller}}</td><td>Chart: {{.Chart}}<br>Application: {{.Application}}<br>Running: {{.Running}}<small>Target: {{.Target}}</small></td><td><strong>{{.Status}}</strong><br>{{.Explanation}}</td><td>{{.Recommendation}}</td><td>{{range .Evidence}}{{if .URL}}<a data-evidence-id="{{.ID}}" href="{{.URL}}" rel="noreferrer noopener">{{.URL}}</a>{{end}}<small>Evidence: {{.ID}}; retrieved {{.RetrievedAt}}; verified {{.VerifiedAt}}; expires {{.ExpiresAt}}; {{.Confidence}}; P{{.Priority}}; {{.ClaimType}}</small><small>Parser: {{.ParserID}}@{{.ParserVersion}}; source: {{.SourceHash}}</small>{{else}}No authoritative evidence.{{end}}</td></tr>{{else}}<tr><td colspan="6">No matching components. Check coverage below; an empty inventory is not proof of compatibility.</td></tr>{{end}}
</tbody></table></div>
<details open><summary>Collection coverage</summary><ul>{{range .Assessment.Coverage}}<li><strong>{{.Collector}}: {{.State}}</strong> — {{.Scope}} {{.Code}} {{range .Omissions}}<span>{{.}}; </span>{{end}}</li>{{else}}<li>No coverage records supplied.</li>{{end}}</ul></details>
<details open><summary>API, schema and static readiness findings</summary><ul>{{range .Assessment.Checks}}<li><strong>{{.ID}}: {{.State}} [{{.Severity}}]</strong> — {{.Scope}}: {{.Reason}}</li>{{else}}<li>No checks supplied.</li>{{end}}</ul></details>
<p>Schema: {{.Assessment.SchemaVersion}}; scanner: {{.Assessment.ScannerVersion}}; scan ID: <code>{{.Assessment.ScanID}}</code>. This standalone report performs no network requests.</p>
<script>"use strict";const form=document.getElementById("filters"),rows=Array.from(document.querySelectorAll("tbody tr[data-component]")),count=document.getElementById("count");function filter(){let n=0;for(const row of rows){let show=true;for(const key of ["ns","controller","component","status","quality"]){const value=form.elements[key].value.trim().toLowerCase(),actual=row.dataset[key].toLowerCase();const match=key==="quality"?actual.split(",").includes(value):(["ns","status"].includes(key)?actual===value:actual.includes(value));if(value&&!match){show=false;break;}}row.hidden=!show;if(show)n++;}count.textContent="Showing "+n+" of {{.Total}} components. Overall decision is unchanged.";}form.addEventListener("input",filter);form.addEventListener("submit",event=>event.preventDefault());form.addEventListener("reset",()=>setTimeout(filter,0));</script></body></html>`))
