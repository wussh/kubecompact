package report

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"strings"
	"time"

	"github.com/wussh/kubecompact/internal/model"
)

// WritePlan renders an ordered upgrade plan. Component filters are purely
// presentational: they never alter the plan decision, a hop decision, global
// checks, coverage, or component-local checks surfaced by the report.
func WritePlan(w io.Writer, plan model.UpgradePlan, format string, filters model.Filters) error {
	if plan.SchemaVersion != model.PlanSchema {
		return errors.New("unsupported plan schema")
	}
	switch format {
	case "json":
		return writePlanJSON(w, plan, filters)
	case "csv":
		return writePlanCSV(w, plan, filters)
	case "table":
		return writePlanTable(w, plan, filters)
	case "html":
		return writePlanHTML(w, plan, filters)
	default:
		return errors.New("unsupported output format")
	}
}

func writePlanJSON(w io.Writer, plan model.UpgradePlan, filters model.Filters) error {
	projected := plan
	projected.Hops = append([]model.PlanHop(nil), plan.Hops...)
	for i := range projected.Hops {
		projected.Hops[i].Assessment = projectAssessment(projected.Hops[i].Assessment, filters)
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(projected)
}

// projectAssessment mirrors the ordinary assessment projection: aggregate the
// hop and component-local checks, then hide only non-matching component rows.
func projectAssessment(assessment model.Assessment, filters model.Filters) model.Assessment {
	assessment.Checks = reportChecks(assessment)
	evidence := make(map[string]model.Evidence, len(assessment.Evidence))
	for _, item := range assessment.Evidence {
		evidence[item.ID] = item
	}
	filtered := make([]model.ComponentAssessment, 0, len(assessment.Components))
	for _, component := range assessment.Components {
		if matches(component, filters, evidence) {
			filtered = append(filtered, component)
		}
	}
	assessment.Components = filtered
	return assessment
}

func writePlanCSV(w io.Writer, plan model.UpgradePlan, filters model.Filters) error {
	writer := csv.NewWriter(w)
	prefix := []string{"plan_decision", "plan_exit_code", "hop_id", "hop_decision", "hop_exit_code"}
	var header []string
	var columns map[string]int
	wroteHeader := false

	for _, hop := range plan.Hops {
		var buffer bytes.Buffer
		if err := Write(&buffer, hop.Assessment, "csv", filters); err != nil {
			return err
		}
		rows, err := csv.NewReader(&buffer).ReadAll()
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			continue
		}
		baseHeader := rows[0]
		baseColumns := headerIndexes(baseHeader)
		if !wroteHeader {
			header = append(append([]string{}, prefix...), baseHeader...)
			columns = headerIndexes(header)
			if err := writer.Write(header); err != nil {
				return err
			}
			if err := writer.Write(planSummaryRow(header, columns, plan)); err != nil {
				return err
			}
			wroteHeader = true
		} else if !equalStrings(header[len(prefix):], baseHeader) {
			return errors.New("inconsistent hop report columns")
		}
		for _, row := range rows[1:] {
			if len(row) != len(baseHeader) {
				return errors.New("invalid hop report row")
			}
			values := make([]string, 0, len(header))
			values = append(values,
				csvCell(plan.Decision), fmt.Sprint(plan.ExitCode),
				csvCell(hop.ID), csvCell(hop.Assessment.Decision), fmt.Sprint(hop.Assessment.ExitCode))
			values = append(values, row...)
			if index, ok := baseColumns["row_type"]; ok {
				target := len(prefix) + index
				if values[target] == "summary" {
					values[target] = "hop"
				}
			}
			if err := writer.Write(values); err != nil {
				return err
			}
		}
		for _, step := range hop.Steps {
			values := make([]string, len(header))
			setColumn(values, columns, "plan_decision", csvCell(plan.Decision))
			setColumn(values, columns, "plan_exit_code", fmt.Sprint(plan.ExitCode))
			setColumn(values, columns, "hop_id", csvCell(hop.ID))
			setColumn(values, columns, "hop_decision", csvCell(hop.Assessment.Decision))
			setColumn(values, columns, "hop_exit_code", fmt.Sprint(hop.Assessment.ExitCode))
			setColumn(values, columns, "id", csvCell(step.ID))
			setColumn(values, columns, "scope", csvCell(step.Scope))
			setColumn(values, columns, "explanation", csvCell(step.Description))
			setColumn(values, columns, "evidence_ids", csvCell(strings.Join(step.EvidenceIDs, "; ")))
			setColumn(values, columns, "target_kubernetes", csvCell(hop.CompatibilityMinor))
			setColumn(values, columns, "row_type", "step")
			setColumn(values, columns, "domain", "plan-step")
			setColumn(values, columns, "overall_decision", csvCell(hop.Assessment.Decision))
			setColumn(values, columns, "overall_exit_code", fmt.Sprint(hop.Assessment.ExitCode))
			details, err := json.Marshal(map[string]string{"stopCondition": step.StopCondition})
			if err != nil {
				return err
			}
			setColumn(values, columns, "details", csvCell(string(details)))
			if err := writer.Write(values); err != nil {
				return err
			}
		}
	}
	if !wroteHeader {
		var buffer bytes.Buffer
		if err := Write(&buffer, model.Assessment{SchemaVersion: model.SchemaVersion}, "csv", filters); err != nil {
			return err
		}
		rows, err := csv.NewReader(&buffer).ReadAll()
		if err != nil || len(rows) == 0 {
			return errors.New("empty assessment report columns")
		}
		header = append(append([]string{}, prefix...), rows[0]...)
		columns = headerIndexes(header)
		if err := writer.Write(header); err != nil {
			return err
		}
		if err := writer.Write(planSummaryRow(header, columns, plan)); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func planSummaryRow(header []string, columns map[string]int, plan model.UpgradePlan) []string {
	row := make([]string, len(header))
	setColumn(row, columns, "plan_decision", csvCell(plan.Decision))
	setColumn(row, columns, "plan_exit_code", fmt.Sprint(plan.ExitCode))
	setColumn(row, columns, "id", "plan")
	setColumn(row, columns, "target_kubernetes", csvCell(plan.FromKubernetes+" -> "+plan.TargetKubernetes))
	setColumn(row, columns, "assessment", csvCell(plan.Decision))
	setColumn(row, columns, "explanation", "Read-only conditional plan; no execution authority and no rollback guarantee.")
	setColumn(row, columns, "row_type", "plan-summary")
	setColumn(row, columns, "domain", "plan")
	setColumn(row, columns, "scope", csvCell(plan.ClusterID))
	setColumn(row, columns, "overall_decision", csvCell(plan.Decision))
	setColumn(row, columns, "overall_exit_code", fmt.Sprint(plan.ExitCode))
	details, err := json.Marshal(map[string]string{
		"distribution":   plan.Distribution,
		"policyRevision": plan.PolicyRevision,
		"inputSHA256":    plan.InputSHA256,
	})
	if err == nil {
		setColumn(row, columns, "details", csvCell(string(details)))
	}
	return row
}

func writePlanTable(w io.Writer, plan model.UpgradePlan, filters model.Filters) error {
	if _, err := fmt.Fprintf(w, "Upgrade plan: %s (exit %d); %s -> %s; %s; cluster %s\n", plan.Decision, plan.ExitCode, plan.FromKubernetes, plan.TargetKubernetes, plan.Distribution, plan.ClusterID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Assessed: %s; policy: %s; input digest: %s\n", planTime(plan.AssessedAt), plan.PolicyRevision, plan.InputSHA256); err != nil {
		return err
	}
	for _, limitation := range plan.Omissions {
		if _, err := fmt.Fprintln(w, "Limitation:", clean(limitation)); err != nil {
			return err
		}
	}
	for _, hop := range plan.Hops {
		if _, err := fmt.Fprintf(w, "\nHop %s: %s -> %s; compatibility target %s; resolved schema %s; depends on %s\n", hop.ID, releaseOrUnknown(hop.FromRelease), releaseOrUnknown(hop.ToRelease), hop.CompatibilityMinor, hop.ResolvedTarget, dependsOrNone(hop.DependsOn)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "Inventory digest: %s; observed: %s\n", inventoryOrUnknown(hop.InventorySHA256), planTime(hop.ObservedAt)); err != nil {
			return err
		}
		for _, step := range hop.Steps {
			if _, err := fmt.Fprintf(w, "Step [%s] %s\n  STOP: %s\n", clean(step.Scope), clean(step.Description), clean(step.StopCondition)); err != nil {
				return err
			}
		}
		for _, condition := range hop.StopConditions {
			if _, err := fmt.Fprintln(w, "STOP:", clean(condition)); err != nil {
				return err
			}
		}
		if err := Write(w, hop.Assessment, "table", filters); err != nil {
			return err
		}
	}
	return nil
}

type planHopView struct {
	Hop      model.PlanHop
	Total    int
	Rows     []row
	Groups   []findingGroup
	Coverage []model.Coverage
}

type planPage struct {
	Plan      model.UpgradePlan
	Hops      []planHopView
	TotalRows int
}

func writePlanHTML(w io.Writer, plan model.UpgradePlan, filters model.Filters) error {
	page := planPage{Plan: plan, Hops: make([]planHopView, 0, len(plan.Hops))}
	for _, hop := range plan.Hops {
		projected := projectAssessment(hop.Assessment, filters)
		evidence := make(map[string]model.Evidence, len(projected.Evidence))
		for _, item := range projected.Evidence {
			evidence[item.ID] = item
		}
		view := planHopView{
			Hop:      hop,
			Total:    len(hop.Assessment.Components),
			Rows:     rowsFor(projected, evidence),
			Groups:   triageGroups(projected.Checks),
			Coverage: projected.Coverage,
		}
		page.TotalRows += view.Total
		page.Hops = append(page.Hops, view)
	}
	return planHTMLTemplate.Execute(w, page)
}

func planTime(value time.Time) string {
	if value.IsZero() {
		return "unavailable"
	}
	return value.UTC().Format(time.RFC3339)
}

func releaseOrUnknown(value string) string {
	if value == "" {
		return "UNKNOWN"
	}
	return value
}

func inventoryOrUnknown(value string) string {
	if value == "" {
		return "UNKNOWN"
	}
	return value
}

func dependsOrNone(value string) string {
	if value == "" {
		return "none"
	}
	return value
}

func headerIndexes(header []string) map[string]int {
	indexes := make(map[string]int, len(header))
	for index, name := range header {
		indexes[name] = index
	}
	return indexes
}

func setColumn(row []string, columns map[string]int, name, value string) {
	if index, ok := columns[name]; ok {
		row[index] = value
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

var planHTMLTemplate = template.Must(template.New("plan").Funcs(template.FuncMap{
	"planTime":           planTime,
	"releaseOrUnknown":   releaseOrUnknown,
	"inventoryOrUnknown": inventoryOrUnknown,
	"dependsOrNone":      dependsOrNone,
}).Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="referrer" content="no-referrer">
<title>KubeCompat upgrade plan</title>
<style>
body{font:15px/1.5 system-ui,sans-serif;margin:24px;color:#202830;background:#fff;overflow-wrap:anywhere}
h1{margin-bottom:4px}
section{border:1px solid #cbd2d9;border-radius:6px;padding:16px;margin:20px 0}
.notice{border:2px solid #586778}
form{display:flex;flex-wrap:wrap;gap:12px;margin:20px 0}
label{display:flex;flex-direction:column;font-weight:600}
input,select,button{font:inherit;padding:6px}
input,select{min-width:130px}
button{padding:6px 12px}
table{width:100%;border-collapse:collapse}
th,td{padding:8px;text-align:left;vertical-align:top;border:1px solid #cbd2d9}
th{background:#eef1f4}
small{display:block;color:#465568}
details{margin-top:12px}
code{overflow-wrap:anywhere}
summary{cursor:pointer}
input:focus-visible,button:focus-visible,summary:focus-visible,select:focus-visible{outline:3px solid #154d9a;outline-offset:2px}
[hidden]{display:none !important}
</style>
</head>
<body>
<h1>Conditional K3s upgrade plan</h1>
<p>Ordered, read-only hop proposal. This document executes nothing and guarantees no rollback.</p>
<section class="notice" aria-label="Plan decision">
<strong>Plan: {{.Plan.Decision}} — exit {{.Plan.ExitCode}}</strong><br>
{{.Plan.FromKubernetes}} → {{.Plan.TargetKubernetes}}; distribution {{.Plan.Distribution}}; cluster {{.Plan.ClusterID}}<br>
Assessed: {{planTime .Plan.AssessedAt}}; policy revision: {{.Plan.PolicyRevision}}; input digest: {{if .Plan.InputSHA256}}{{.Plan.InputSHA256}}{{else}}unavailable{{end}}
</section>
<section aria-label="Plan limitations">
<h2>Limitations and stop conditions</h2>
<ul>{{range .Plan.Omissions}}<li>{{.}}</li>{{end}}</ul>
<p><strong>Filters below only hide component rows. They never change the plan decision, any hop decision, checks or coverage.</strong></p>
</section>
<form id="filters" aria-label="Filter component rows">
<label>Namespace<input name="ns" autocomplete="off"></label>
<label>Controller<input name="controller" autocomplete="off"></label>
<label>Component<input name="component" autocomplete="off"></label>
<label>Assessment<select name="status"><option value="">All</option><option>COMPATIBLE</option><option>INCOMPATIBLE</option><option>UPGRADE_REQUIRED</option><option>NOT_TESTED</option><option>UNKNOWN</option></select></label>
<label>Evidence quality<input name="quality" autocomplete="off" placeholder="HIGH, MEDIUM, LOW, NONE"></label>
<button type="reset">Reset filters</button>
</form>
<p id="count" role="status">Showing {{.TotalRows}} of {{.TotalRows}} component rows.</p>
{{range .Hops}}{{$hopID := .Hop.ID}}
<section aria-label="Hop {{.Hop.ID}}">
<h2>Hop {{.Hop.ID}}: {{releaseOrUnknown .Hop.FromRelease}} → {{releaseOrUnknown .Hop.ToRelease}}</h2>
<p><strong>Hop decision: {{.Hop.Assessment.Decision}} — exit {{.Hop.Assessment.ExitCode}}</strong><br>
Compatibility target: {{.Hop.CompatibilityMinor}}; resolved schema: {{.Hop.ResolvedTarget}}; depends on: {{dependsOrNone .Hop.DependsOn}}<br>
Inventory digest: {{inventoryOrUnknown .Hop.InventorySHA256}}; observed: {{planTime .Hop.ObservedAt}}</p>
<h3>Prerequisites and ordered steps</h3>
<ol>{{range .Hop.Steps}}<li><strong>{{.Scope}}</strong>: {{.Description}}<br><em>Stop: {{.StopCondition}}</em></li>{{end}}</ol>
<h3>Stop conditions</h3>
<ul>{{range .Hop.StopConditions}}<li>{{.}}</li>{{end}}</ul>
<h3>Domain triage</h3>
{{range .Groups}}<details><summary>{{.Domain}}: {{.State}} [{{.Severity}}] {{.Reason}} — {{len .Checks}} findings</summary>
<p>Next evidence: {{.Action}}</p>
<ul>{{range .Checks}}<li><strong>{{.ID}}</strong> — {{.Scope}}; {{.State}} [{{.Severity}}]; evidence: {{range .EvidenceIDs}}{{.}} {{end}}
{{if .Details}}<dl>{{range $key, $value := .Details}}<dt>{{$key}}</dt><dd>{{$value}}</dd>{{end}}</dl>{{end}}</li>{{end}}</ul>
</details>{{else}}<p>No checks supplied.</p>{{end}}
<h3>Component presentation ({{len .Rows}} of {{.Total}})</h3>
<div style="overflow-x:auto" tabindex="0" role="region" aria-label="Hop component rows">
<table><caption>Components observed for this hop; filters do not alter decisions.</caption>
<thead><tr><th scope="col">Component</th><th scope="col">Assessment</th><th scope="col">Recommendation</th><th scope="col">Running images</th></tr></thead>
<tbody>{{range .Rows}}<tr data-hop="{{$hopID}}" data-ns="{{.Namespace}}" data-controller="{{.Controller}}" data-component="{{.Name}} {{.Project}}" data-status="{{.Status}}" data-quality="{{.Quality}}"><td>{{.Name}}<small>{{.Namespace}} / {{.Project}}</small></td><td><strong>{{.Status}}</strong><br>{{.Explanation}}</td><td>{{.Recommendation}}</td><td>{{.Running}}</td></tr>{{else}}<tr><td colspan="4">No component rows supplied for this hop.</td></tr>{{end}}</tbody>
</table>
</div>
<details><summary>Collection coverage ({{len .Coverage}} records)</summary><ul>{{range .Coverage}}<li><strong>{{.Collector}}: {{.State}}</strong> — {{.Scope}} {{.Code}}{{range .Omissions}}; {{.}}{{end}}</li>{{else}}<li>No coverage records supplied.</li>{{end}}</ul></details>
</section>
{{end}}
<p>Scanner schema: {{.Plan.SchemaVersion}}. This standalone report makes no network requests and loads no external assets.</p>
<script>
"use strict";
var form=document.getElementById("filters");
var rows=Array.prototype.slice.call(document.querySelectorAll("tr[data-hop]"));
var count=document.getElementById("count");
var total={{.TotalRows}};
function apply(){
  var shown=0;
  rows.forEach(function(row){
    var visible=true;
    ["ns","controller","component","status","quality"].forEach(function(key){
      var value=(form.elements[key].value||"").trim().toLowerCase();
      var actual=(row.getAttribute("data-"+key)||"").toLowerCase();
      if(!value){return;}
      var match;
      if(key==="quality"){match=actual.split(",").indexOf(value)>=0;}
      else if(key==="ns"||key==="status"){match=actual===value;}
      else{match=actual.indexOf(value)>=0;}
      if(!match){visible=false;}
    });
    row.hidden=!visible;
    if(visible){shown++;}
  });
  count.textContent="Showing "+shown+" of "+total+" component rows. Plan and hop decisions are unchanged.";
}
form.addEventListener("input",apply);
form.addEventListener("submit",function(event){event.preventDefault();apply();});
form.addEventListener("reset",function(){setTimeout(apply,0);});
apply();
</script>
</body>
</html>
`))
