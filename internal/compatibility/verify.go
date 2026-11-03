package compatibility

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/url"
	"regexp"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/wussh/kubecompact/internal/compatibility/fetch"
	"github.com/wussh/kubecompact/internal/compatmatrix"
	"github.com/wussh/kubecompact/internal/model"
)

// VerifySchemaVersion is the verify report schema.
const VerifySchemaVersion = "kubecompat-matrix-verify/v1"

// Verify verdicts. They never change the dataset; WEAK, MISMATCH, UNREACHABLE,
// UNVERIFIABLE and STALE are reviewer signals. STRUCTURAL is the offline result:
// schema/rows/dates/URLs were validated, but no page was fetched, so offline
// rows are never reported as OK.
const (
	VerifyOK           = "OK"
	VerifyStructural   = "STRUCTURAL"
	VerifyWeak         = "WEAK"
	VerifyAdjudicated  = "ADJUDICATED"
	VerifyMismatch     = "MISMATCH"
	VerifyUnreachable  = "UNREACHABLE"
	VerifyUnverifiable = "UNVERIFIABLE"
	VerifyStale        = "STALE"
)

// MatrixAdjudication is one reviewed decision that suppresses re-derivation.
type MatrixAdjudication struct {
	Component  string `json:"component"`
	Kubernetes string `json:"kubernetes"`
	Reason     string `json:"reason"`
}

// AdjudicationFile is the optional --adjudications input.
type AdjudicationFile struct {
	Adjudications []MatrixAdjudication `json:"adjudications"`
}

// ParseAdjudications validates an adjudication file. A record only marks a
// cell as ADJUDICATED; it never promotes a status.
func ParseAdjudications(data []byte) (map[string]string, error) {
	var file AdjudicationFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, errors.New("invalid adjudications input")
	}
	out := map[string]string{}
	for _, item := range file.Adjudications {
		if strings.TrimSpace(item.Component) == "" || strings.TrimSpace(item.Kubernetes) == "" {
			return nil, errors.New("invalid adjudications input: component and kubernetes are required")
		}
		key := adjudicationKey(item.Component, item.Kubernetes)
		if _, exists := out[key]; exists {
			return nil, fmt.Errorf("invalid adjudications input: duplicate record for %s %s", item.Component, item.Kubernetes)
		}
		reason := strings.TrimSpace(item.Reason)
		if reason == "" {
			reason = "adjudicated"
		}
		out[key] = reason
	}
	return out, nil
}

func adjudicationKey(component, minor string) string {
	return strings.ToLower(strings.TrimSpace(component)) + "\x00" + strings.TrimSpace(minor)
}

// VerifyOptions bounds an out-of-band matrix verification pass.
type VerifyOptions struct {
	Online          bool
	AllowedHosts    []string
	CAFile          string
	CredentialsFile string
	Adjudications   map[string]string
	Now             time.Time
}

// VerifyRow is one cell verdict.
type VerifyRow struct {
	Component       string `json:"component"`
	Kubernetes      string `json:"kubernetes"`
	Status          string `json:"status"`
	RequiredVersion string `json:"requiredVersion"`
	Source          string `json:"source"`
	VerifiedAt      string `json:"verifiedAt"`
	Verdict         string `json:"verdict"`
	HTTPStatus      int    `json:"httpStatus,omitempty"`
	ContentHash     string `json:"contentHash,omitempty"`
	Detail          string `json:"detail,omitempty"`
}

// VerifyReport is the complete verification result.
type VerifyReport struct {
	SchemaVersion string         `json:"schemaVersion"`
	DatasetDigest string         `json:"datasetDigest"`
	CheckedAt     time.Time      `json:"checkedAt"`
	Online        bool           `json:"online"`
	ExitCode      int            `json:"exitCode"`
	Counts        map[string]int `json:"counts"`
	Rows          []VerifyRow    `json:"rows"`
}

// VerifyMatrix verifies a validated dataset. Offline it performs structural
// verification only (schema, duplicates, dates, URL form are enforced by the
// loader). Online it re-fetches each distinct cited URL through the existing
// transport policy and checks HTTP 200 plus the recorded minor text.
func VerifyMatrix(ctx context.Context, ds *compatmatrix.MatrixDataset, opts VerifyOptions) (VerifyReport, error) {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	rep := VerifyReport{
		SchemaVersion: VerifySchemaVersion,
		DatasetDigest: ds.Digest,
		CheckedAt:     opts.Now.UTC(),
		Online:        opts.Online,
		Counts:        map[string]int{VerifyOK: 0, VerifyStructural: 0, VerifyWeak: 0, VerifyAdjudicated: 0, VerifyMismatch: 0, VerifyUnreachable: 0, VerifyUnverifiable: 0, VerifyStale: 0},
	}
	var client *fetch.Client
	if opts.Online {
		if len(opts.AllowedHosts) == 0 {
			return rep, errors.New("--online requires an explicit --allow-hosts list")
		}
		var err error
		client, err = fetch.New(fetch.Config{Online: true, CAFile: opts.CAFile, AllowedHosts: opts.AllowedHosts, CredentialsFile: opts.CredentialsFile})
		if err != nil {
			return rep, errors.New("invalid transport configuration")
		}
	}
	type fetched struct {
		text   string
		title  string
		hash   string
		err    string
		status int
	}
	cache := map[string]fetched{}
	for _, component := range ds.Components {
		for _, minor := range ds.Kubernetes {
			cell := component.Kubernetes[minor]
			row := VerifyRow{
				Component:       component.ID,
				Kubernetes:      minor,
				Status:          cell.Status,
				RequiredVersion: cell.RequiredVersion,
				Source:          cell.Source,
				VerifiedAt:      cell.VerifiedAt,
			}
			if reason, ok := opts.Adjudications[adjudicationKey(component.ID, minor)]; ok {
				row.Verdict, row.Detail = VerifyAdjudicated, reason
				rep.Rows = append(rep.Rows, row)
				continue
			}
			if !opts.Online {
				age, stale := compatmatrix.MatrixEvidenceAge(cell.VerifiedAt, opts.Now)
				if stale {
					row.Verdict = VerifyStale
					row.Detail = fmt.Sprintf("structural check passed; evidence is %d days old", age)
				} else {
					row.Verdict, row.Detail = VerifyStructural, "structural check passed (offline; no page fetched)"
				}
				rep.Rows = append(rep.Rows, row)
				continue
			}
			key, err := fetchableURL(cell.Source)
			if err != nil {
				row.Verdict, row.Detail = VerifyUnreachable, "source URL is not fetchable under the transport policy"
				rep.Rows = append(rep.Rows, row)
				continue
			}
			result, seen := cache[key]
			if !seen {
				snapshot, err := client.Fetch(ctx, fetch.SourceRequest{URL: key, Project: component.ID, ParserID: "matrix-verify", ParserVersion: "1"})
				if err != nil {
					result = fetched{err: "source unavailable under the transport policy", status: 0}
				} else {
					result = fetched{text: normalizeDocument(snapshot.Body), title: documentTitle(snapshot.Body), hash: snapshot.SHA256, status: 200}
				}
				cache[key] = result
			}
			row.HTTPStatus = result.status
			if result.err != "" {
				row.Verdict, row.Detail = VerifyUnreachable, result.err
				rep.Rows = append(rep.Rows, row)
				continue
			}
			row.ContentHash = result.hash
			row.Verdict, row.Detail = judgeMatrixRow(cell.Status, minor, result.text, result.title)
			rep.Rows = append(rep.Rows, row)
		}
	}
	rep.ExitCode = 2
	clean := true
	for _, row := range rep.Rows {
		rep.Counts[row.Verdict]++
		switch row.Verdict {
		case VerifyOK, VerifyStructural, VerifyAdjudicated:
		default:
			clean = false
		}
	}
	if clean {
		rep.ExitCode = 0
	}
	return rep, nil
}

// fetchableURL strips the fragment (never sent to the server) and rejects URLs
// the transport policy would refuse, so policy denial is not reported as
// unreachable content.
func fetchableURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" {
		return "", errors.New("unfetchable")
	}
	u.Fragment = ""
	u.RawFragment = ""
	return u.String(), nil
}

var (
	markupTag      = regexp.MustCompile(`(?s)<[^>]*>`)
	titleTag       = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	headingTag     = regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`)
	weakMarker     = regexp.MustCompile(`(?i)(-dev\b|dev documentation|developer[- ]preview|pre-release|prerelease|release candidate|\brc[0-9]+\b|\btbd\b|\bnot yet released\b|\bunreleased\b|\bplanned\b)`)
	htmlSpace      = regexp.MustCompile(`\s+`)
	minorLeftEdge  = regexp.MustCompile(`[.0-9]`)
	minorRightEdge = regexp.MustCompile(`[0-9]`)
)

// normalizeDocument reduces a fetched body to lowercase text. It performs no
// semantic extraction: the only question is whether the recorded minor appears.
func normalizeDocument(body []byte) string {
	text := markupTag.ReplaceAllString(string(body), " ")
	text = html.UnescapeString(text)
	text = strings.ToLower(text)
	return htmlSpace.ReplaceAllString(text, " ")
}

// judgeMatrixRow maps the served page onto a verdict. It never promotes a
// status: unknown stays unknown, and any ambiguity yields WEAK for review.
func judgeMatrixRow(status, minor, document, title string) (string, string) {
	present := minorPresent(document, minor)
	weak := weakContext(document, title, minor)
	switch {
	case status == "supported" && !present:
		return VerifyMismatch, "recorded minor does not appear on the served page"
	case status == "supported" && weak:
		return VerifyWeak, "recorded minor appears only in unreleased or development context"
	case status == "supported":
		return VerifyOK, "recorded minor appears on the served page"
	case status == "unknown" && present:
		return VerifyWeak, "recorded minor appears on the served page while the row records no statement"
	case status == "unknown":
		return VerifyOK, "no statement for the recorded minor on the served page"
	default:
		return VerifyUnverifiable, "exclusion statement requires human review; the checker does not parse exclusions"
	}
}

func minorPresent(document, minor string) bool {
	needle := strings.ToLower(minor)
	for offset := 0; ; {
		index := strings.Index(document[offset:], needle)
		if index < 0 {
			return false
		}
		start := offset + index
		end := start + len(needle)
		before := start == 0 || !minorLeftEdge.MatchString(document[start-1:start])
		after := end >= len(document) || !minorRightEdge.MatchString(document[end:end+1])
		if before && after {
			return true
		}
		offset = end
	}
}

// weakContext reports whether a sentence mentioning the minor, or the document
// title, carries an unreleased/development marker.
func weakContext(document, title, minor string) bool {
	for _, sentence := range strings.Split(document, ". ") {
		if strings.Contains(sentence, minor) && weakMarker.MatchString(sentence) {
			return true
		}
	}
	return weakMarker.MatchString(title) && strings.Contains(document, minor)
}

// documentTitle extracts the page title and first heading for channel markers.
func documentTitle(body []byte) string {
	raw := string(body)
	var parts []string
	for _, pattern := range []*regexp.Regexp{titleTag, headingTag} {
		if match := pattern.FindStringSubmatch(raw); len(match) == 2 {
			parts = append(parts, markupTag.ReplaceAllString(html.UnescapeString(match[1]), " "))
		}
	}
	return strings.ToLower(htmlSpace.ReplaceAllString(strings.Join(parts, " "), " "))
}

// WriteVerifyReport renders json or a compact table.
func WriteVerifyReport(w io.Writer, rep VerifyReport, format string) error {
	switch format {
	case "json":
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(rep)
	case "table":
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		if _, err := fmt.Fprintf(tw, "Dataset sha256: %s; checked %s; %s\n", rep.DatasetDigest, rep.CheckedAt.Format(time.RFC3339), onlineLabel(rep.Online)); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(tw, "VERDICT\tCOMPONENT\tKUBERNETES\tSTATUS\tHTTP\tSOURCE\tDETAIL"); err != nil {
			return err
		}
		for _, row := range rep.Rows {
			http := "-"
			if row.HTTPStatus != 0 {
				http = fmt.Sprint(row.HTTPStatus)
			}
			source := safeSource(row.Source)
			if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", row.Verdict, row.Component, row.Kubernetes, row.Status, http, source, row.Detail); err != nil {
				return err
			}
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		var verdicts []string
		for _, verdict := range []string{VerifyOK, VerifyStructural, VerifyWeak, VerifyAdjudicated, VerifyMismatch, VerifyUnreachable, VerifyUnverifiable, VerifyStale} {
			verdicts = append(verdicts, fmt.Sprintf("%s %d", verdict, rep.Counts[verdict]))
		}
		_, err := fmt.Fprintf(w, "Counts: %s; exit %d\n", strings.Join(verdicts, " · "), rep.ExitCode)
		return err
	default:
		return errors.New("unsupported verify format")
	}
}

func onlineLabel(online bool) string {
	if online {
		return "online re-fetch"
	}
	return "offline structural check"
}

func safeSource(raw string) string {
	if safe := model.SafeURL(raw); safe != "" {
		return safe
	}
	return "-"
}
