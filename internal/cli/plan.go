package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/wussh/kubecompact/internal/model"
	"github.com/wussh/kubecompact/internal/planner"
	"github.com/wussh/kubecompact/internal/report"
)

var (
	errPlanInput      = errors.New("invalid plan input")
	errPlanIO         = errors.New("referenced plan inventory is unavailable")
	errPlanEvaluation = errors.New("plan evaluation failed")
)

var inventoryDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

// runPlan dispatches the offline planner. The flag set deliberately exposes no
// collection, credential, refresh or online option: unknown flags are rejected
// by flag.Parse before any input is read.
func runPlan(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	f := defaults()
	f.format = "table"
	var from, to, distribution, inputPath string
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&from, "from-k8s", "", "source Kubernetes minor: 1.32, 1.33 or 1.34")
	fs.StringVar(&to, "target-k8s", "", "supported destination minor strictly greater than the source")
	fs.StringVar(&distribution, "distribution", "k3s", "distribution (only k3s is supported)")
	fs.StringVar(&inputPath, "plan-input", "", "bounded sanitized plan input JSON")
	fs.StringVar(&f.format, "format", f.format, "table, json, csv or html")
	fs.StringVar(&f.output, "output", "", "output file (default stdout)")
	fs.StringVar(&f.check.CachePath, "cache", f.check.CachePath, "SQLite evidence cache path")
	fs.BoolVar(&f.offline, "offline", false, "explicitly prohibit source network access (always on)")
	fs.StringVar(&f.filters.Controller, "controller", "", "management-chain filter")
	fs.StringVar(&f.filters.Component, "component", "", "component/project name filter")
	fs.StringVar(&f.filters.Status, "status", "", "component verdict filter")
	fs.StringVar(&f.filters.EvidenceQuality, "evidence-quality", "", "HIGH, MEDIUM, LOW or NONE")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), `usage: kubecompat plan --from-k8s 1.33 --target-k8s 1.35 --distribution k3s --plan-input plan-input.json [options]

Builds an ordered, conditional K3s plan between adjacent Kubernetes minors.
The planner is strictly offline: it reads only the supplied plan input, its
explicitly referenced sanitized inventories and the local evidence cache.
No cluster, Git, manifest, Helm, credential, CA or online source is contacted.
Missing future hop evidence renders a required UNKNOWN and exit 3.`)
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	fail := func(code int, message string) int {
		fmt.Fprintln(stderr, message)
		return code
	}
	if fs.NArg() > 0 {
		return fail(1, "unexpected positional argument")
	}
	if from == "" || to == "" {
		return fail(1, "plan requires --from-k8s and --target-k8s")
	}
	if !strings.EqualFold(distribution, "k3s") {
		return fail(1, "plan supports --distribution k3s only")
	}
	if inputPath == "" {
		return fail(1, "plan requires --plan-input")
	}
	if f.format != "json" && f.format != "table" && f.format != "csv" && f.format != "html" {
		return fail(1, "unsupported output format")
	}
	if err := planner.ValidateEndpoints(from, to); err != nil {
		return fail(1, "invalid plan path: "+err.Error())
	}
	raw, digest, err := readBounded(inputPath)
	if err != nil {
		return fail(1, "invalid plan input")
	}
	var input model.PlanInput
	if err := decodeStrict(raw, &input); err != nil {
		return fail(1, "invalid plan input")
	}
	if input.SchemaVersion != model.PlanInputSchema {
		return fail(1, "unsupported plan input schema")
	}
	if err := attachInventories(filepath.Dir(inputPath), input.Hops); err != nil {
		if errors.Is(err, errPlanIO) {
			return fail(4, "referenced plan inventory is unavailable")
		}
		return fail(1, "invalid plan input")
	}
	evaluate := func(ctx context.Context, inventory model.Inventory, target string) (model.Assessment, error) {
		hopFlags := f
		hopFlags.check.TargetKubernetes = target
		hopFlags.check.Online = false
		assessment, err := assessInventory(ctx, hopFlags, inventory)
		if err != nil {
			return model.Assessment{}, fmt.Errorf("%w: %v", errPlanEvaluation, err)
		}
		return assessment, nil
	}
	plan, err := planner.Build(ctx, input, from, to, time.Now().UTC(), evaluate)
	if err != nil {
		if errors.Is(err, errPlanEvaluation) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return fail(4, "plan evaluation failed; inspect the evidence cache and inputs")
		}
		return fail(1, "invalid plan input or unsupported path")
	}
	plan.InputSHA256 = digest
	if err := output(stdout, f.output, func(w io.Writer) error {
		return report.WritePlan(w, plan, f.format, f.filters)
	}); err != nil {
		return fail(4, "cannot write upgrade plan")
	}
	return plan.ExitCode
}

// attachInventories resolves referenced files relative to the plan input.
// Digests are mandatory so the plan can identify exactly which observation
// was evaluated.
func attachInventories(dir string, hops []model.HopInput) error {
	for i := range hops {
		hop := &hops[i]
		if hop.InventoryFile == "" && hop.InventorySHA256 == "" {
			continue
		}
		if hop.InventoryFile == "" || hop.InventorySHA256 == "" {
			return fmt.Errorf("%w: hop inventory file and digest must be supplied together", errPlanInput)
		}
		inventory, err := loadHopInventory(dir, hop.InventoryFile, hop.InventorySHA256)
		if err != nil {
			return err
		}
		hop.Inventory = &inventory
	}
	return nil
}

func loadHopInventory(dir, name, digest string) (model.Inventory, error) {
	var inventory model.Inventory
	if !inventoryDigest.MatchString(digest) {
		return inventory, fmt.Errorf("%w: inventory digest", errPlanInput)
	}
	if strings.Contains(name, "://") || filepath.IsAbs(name) || filepath.VolumeName(name) != "" {
		return inventory, fmt.Errorf("%w: inventory path must be relative", errPlanInput)
	}
	clean := filepath.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return inventory, fmt.Errorf("%w: inventory path escapes the plan directory", errPlanInput)
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return inventory, fmt.Errorf("%w: plan directory", errPlanIO)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, clean))
	if err != nil {
		return inventory, fmt.Errorf("%w: inventory file", errPlanIO)
	}
	if resolved != root && !strings.HasPrefix(resolved, root+string(filepath.Separator)) {
		return inventory, fmt.Errorf("%w: inventory path escapes the plan directory", errPlanInput)
	}
	info, err := os.Lstat(resolved)
	if err != nil {
		return inventory, fmt.Errorf("%w: inventory file", errPlanIO)
	}
	if !info.Mode().IsRegular() || info.Size() > maxInputBytes {
		return inventory, fmt.Errorf("%w: inventory must be a regular file within 64 MiB", errPlanInput)
	}
	data, sum, err := readBounded(resolved)
	if err != nil {
		return inventory, fmt.Errorf("%w: inventory file", errPlanIO)
	}
	if sum != digest {
		return inventory, fmt.Errorf("%w: inventory digest mismatch", errPlanInput)
	}
	if err := decodeStrict(data, &inventory); err != nil {
		return inventory, fmt.Errorf("%w: inventory JSON", errPlanInput)
	}
	if inventory.SchemaVersion != model.SchemaVersion {
		return inventory, fmt.Errorf("%w: unsupported inventory schema", errPlanInput)
	}
	return inventory, nil
}

const maxInputBytes = 64 << 20

// readBounded returns the raw JSON and its SHA256 without following anything
// beyond the explicit local path.
func readBounded(path string) ([]byte, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, "", errors.New("input file unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxInputBytes {
		return nil, "", errors.New("input must be a regular file within 64 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxInputBytes+1))
	if err != nil || len(data) > maxInputBytes {
		return nil, "", errors.New("input must be a regular file within 64 MiB")
	}
	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:]), nil
}

func decodeStrict(data []byte, dest any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		return errors.New("invalid input JSON or schema")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("multiple JSON documents are not supported")
	}
	return nil
}
