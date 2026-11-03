package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/wussh/kubecompact/internal/compatibility"
	"github.com/wussh/kubecompact/internal/compatmatrix"
	"github.com/wussh/kubecompact/internal/model"
	"github.com/wussh/kubecompact/internal/report"
)

func matrixHelp(w io.Writer) {
	fmt.Fprintln(w, `Usage: kubecompat matrix build|verify [options]

matrix build  Render the pinned compat-matrix/v1 dataset offline.
  --dataset PATH        required pinned compat-matrix/v1 JSON dataset
  --inventory PATH      saved inventory JSON (kubecompat.io/v1alpha1)
  --scope PATH          explicit scope JSON (tracker scope block or current-state file)
  --format FORMAT       json (default), csv or markdown
  --output PATH         output file (default stdout; atomic 0600)
  --extended            add product-only fields under "extensions" / extra CSV columns
  --dataset-commit SHA  optional tracker commit recorded in --extended provenance
  --expect-digest HEX   fail unless the dataset SHA-256 matches

matrix verify  Verify the pinned dataset; the dataset is never modified.
  --dataset PATH        required pinned compat-matrix/v1 JSON dataset
  --online              re-fetch cited URLs (requires --allow-hosts)
  --allow-hosts h1,h2   comma-separated hosts; extends the built-in official
                        host set and does not restrict fetching to these hosts
  --ca-file PATH        additional trusted CA for transport tests
  --credentials-file PATH  private per-origin credentials (mode 0600)
  --adjudications PATH  reviewed adjudication file (marks cells ADJUDICATED)
  --format FORMAT       table (default) or json
  --output PATH         output file (default stdout; atomic 0600)
  --timeout DURATION    whole-operation deadline (default 5m)

Verdicts: OK, STRUCTURAL, WEAK, ADJUDICATED, MISMATCH, UNREACHABLE,
          UNVERIFIABLE, STALE. Offline rows are STRUCTURAL (validated but no page
          fetched) or STALE; OK is only emitted after an online fetch.
Exit codes: build 0 rendered, 1 invalid input, 4 output unavailable.
            verify 0 every row OK/ADJUDICATED, 1 invalid input,
                   2 findings (WEAK/MISMATCH/UNREACHABLE/STALE), 4 output unavailable.
No status is ever promoted by build or verify; no network is used offline.`)
}

func runMatrix(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		matrixHelp(stdout)
		return 0
	}
	switch args[0] {
	case "build":
		return runMatrixBuild(args[1:], stdout, stderr)
	case "verify":
		return runMatrixVerify(ctx, args[1:], stdout, stderr)
	default:
		fmt.Fprintln(stderr, "usage: kubecompat matrix build|verify [options]")
		return 1
	}
}

func runMatrixBuild(args []string, stdout, stderr io.Writer) int {
	fail := func(code int, message string) int { fmt.Fprintln(stderr, message); return code }
	fs := flag.NewFlagSet("matrix build", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var datasetPath, inventoryPath, scopePath, format, outputPath, datasetCommit, expectDigest string
	var extended bool
	fs.StringVar(&datasetPath, "dataset", "", "pinned compat-matrix/v1 dataset")
	fs.StringVar(&inventoryPath, "inventory", "", "saved inventory JSON")
	fs.StringVar(&scopePath, "scope", "", "explicit scope JSON")
	fs.StringVar(&format, "format", "json", "json, csv or markdown")
	fs.StringVar(&outputPath, "output", "", "output file")
	fs.BoolVar(&extended, "extended", false, "add product-only fields")
	fs.StringVar(&datasetCommit, "dataset-commit", "", "optional tracker commit")
	fs.StringVar(&expectDigest, "expect-digest", "", "expected dataset SHA-256")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() > 0 {
		return fail(1, "unexpected positional argument")
	}
	if datasetPath == "" {
		return fail(1, "matrix build requires --dataset")
	}
	switch format {
	case "json", "csv", "markdown", "md":
	default:
		return fail(1, "unsupported matrix format")
	}
	data, digest, err := readBounded(datasetPath)
	if err != nil {
		return fail(1, "dataset file unavailable")
	}
	dataset, err := compatmatrix.LoadMatrixDataset(data)
	if err != nil {
		return fail(1, err.Error())
	}
	if expectDigest != "" && !strings.EqualFold(strings.TrimPrefix(expectDigest, "sha256:"), digest) {
		return fail(1, "dataset digest does not match --expect-digest")
	}
	var inventory *model.Inventory
	if inventoryPath != "" {
		var view model.Inventory
		if err := readJSON(inventoryPath, &view); err != nil {
			return fail(1, "invalid saved inventory")
		}
		if view.SchemaVersion != model.SchemaVersion {
			return fail(1, "unsupported inventory schema")
		}
		inventory = &view
	}
	var scope *compatmatrix.MatrixScope
	if scopePath != "" {
		raw, _, err := readBounded(scopePath)
		if err != nil {
			return fail(1, "scope file unavailable")
		}
		scope, err = compatmatrix.ParseMatrixScope(raw)
		if err != nil {
			return fail(1, err.Error())
		}
	}
	installation := compatmatrix.DeriveInstallation(dataset, inventory, scope)
	effectiveScope := scope
	if effectiveScope == nil {
		effectiveScope = dataset.EmbeddedScope()
	}
	err = output(stdout, outputPath, func(w io.Writer) error {
		return report.WriteMatrix(w, dataset, report.MatrixBuildOptions{
			Format:        format,
			Extended:      extended,
			DatasetCommit: datasetCommit,
			Scope:         effectiveScope,
			Installation:  installation,
		})
	})
	if err != nil {
		return fail(4, "cannot write matrix output")
	}
	return 0
}

func runMatrixVerify(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fail := func(code int, message string) int { fmt.Fprintln(stderr, message); return code }
	fs := flag.NewFlagSet("matrix verify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var datasetPath, outputPath, format, hosts, caFile, credentialsFile, adjudicationsPath string
	var online bool
	var timeout time.Duration
	fs.StringVar(&datasetPath, "dataset", "", "pinned compat-matrix/v1 dataset")
	fs.BoolVar(&online, "online", false, "re-fetch cited URLs")
	fs.StringVar(&hosts, "allow-hosts", "", "comma-separated host allowlist")
	fs.StringVar(&caFile, "ca-file", "", "additional trusted CA file")
	fs.StringVar(&credentialsFile, "credentials-file", "", "private per-origin credentials")
	fs.StringVar(&adjudicationsPath, "adjudications", "", "adjudication input")
	fs.StringVar(&format, "format", "table", "table or json")
	fs.StringVar(&outputPath, "output", "", "output file")
	fs.DurationVar(&timeout, "timeout", 5*time.Minute, "whole-operation deadline")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() > 0 {
		return fail(1, "unexpected positional argument")
	}
	if datasetPath == "" {
		return fail(1, "matrix verify requires --dataset")
	}
	if format != "table" && format != "json" {
		return fail(1, "unsupported verify format")
	}
	if timeout <= 0 {
		return fail(1, "timeout must be positive")
	}
	if online && strings.TrimSpace(hosts) == "" {
		return fail(1, "--online requires an explicit --allow-hosts list")
	}
	data, _, err := readBounded(datasetPath)
	if err != nil {
		return fail(1, "dataset file unavailable")
	}
	dataset, err := compatmatrix.LoadMatrixDataset(data)
	if err != nil {
		return fail(1, err.Error())
	}
	var adjudications map[string]string
	if adjudicationsPath != "" {
		raw, _, err := readBounded(adjudicationsPath)
		if err != nil {
			return fail(1, "adjudications file unavailable")
		}
		adjudications, err = compatibility.ParseAdjudications(raw)
		if err != nil {
			return fail(1, err.Error())
		}
	}
	var allowed []string
	for _, host := range strings.Split(hosts, ",") {
		if host = strings.TrimSpace(host); host != "" {
			allowed = append(allowed, host)
		}
	}
	verifyCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	rep, err := compatibility.VerifyMatrix(verifyCtx, dataset, compatibility.VerifyOptions{
		Online:          online,
		AllowedHosts:    allowed,
		CAFile:          caFile,
		CredentialsFile: credentialsFile,
		Adjudications:   adjudications,
	})
	if err != nil {
		return fail(1, err.Error())
	}
	if err := output(stdout, outputPath, func(w io.Writer) error {
		return compatibility.WriteVerifyReport(w, rep, format)
	}); err != nil {
		return fail(4, "cannot write verify report")
	}
	return rep.ExitCode
}
