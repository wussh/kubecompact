# Verification receipts

Documented 2026-10-30. These are observed local commands, not controller-backed release certification. Working outputs are ignored private artifacts; live names, endpoints, certificates and credentials are not included here.

## Source verification

Go 1.27.3, client-go/API/apimachinery 0.37.2, Helm 4.3.1, Kubeconform 0.8, Kustomize API 0.21.1 / kyaml 0.21.2.

Final integrated source command after evaluator, provenance, safe-copy, Git capability/query and report-reference changes:

```sh
go test ./...
go test -race ./internal/...
go vet ./...
go build -o bin/kubecompat ./cmd/kubecompat
CGO_ENABLED=0 go build -trimpath -buildvcs=true -o dist/kubecompat-linux-amd64 ./cmd/kubecompat
```

Observed: **144 tests passed across 12 packages**, **144 race tests across 10 internal packages**, vet exit 0 and both builds exit 0. The environment-order and mismatched-release regressions both failed before their fixes. Latest-patch policy does not infer completeness from the maximum observed release. Additional tests preserve zero-component source receipts, correlate actual manifest collection and retain distinct evidence IDs when URLs are shared.

## Actual CLI paths

| Input / operation | Observed outcome |
| --- | --- |
| Service-only raw YAML | scan exit 0, zero components, one top-level source receipt with raw renderer v3.0.5 and SHA256 input digest |
| Valid document followed by malformed YAML | scan exit 1 with safe diagnostics, not partial-success exit 0 |
| Service port with invalid string type | check target 1.35 exit 2, schema failure |
| Sanitized saved inventory without raw schema-validation inputs | check target 1.35 exit 3, missing validation input is UNKNOWN |
| Raw synthetic custom resource without CRD/schema | check target 1.35 exit 3, schema UNKNOWN / TARGET_SCHEMA_MISSING and API UNKNOWN / API_NOT_IN_LIFECYCLE_CATALOG; not a clean schema pass |
| Target 1.34 | exit 1, outside pinned catalog, not silently assessed |
| Supplied one-hour synthetic policy/v1beta1 PDB request summary | check target 1.35 exit 2, removed client API blocker scoped to the supplied interval |
| Synthetic mixed Flux/Helm/workload ownership documents | scan exit 0; two components: four-resource release chain and separate namespace collision; env canary absent |
| Synthetic metrics-server 0.3.0 observation with real cached official evidence | offline check exit 2, UPGRADE_REQUIRED, exact 0.4.5 recommendation; both independent release/support proof IDs exported |
| Synthetic cert-manager 1.18.0 chart observation with real cached official evidence | offline check exit 2, INCOMPATIBLE at target 1.35, no fabricated replacement |
| Pure report from saved assessment | exit 0; embedded BLOCKED decision and assessment exit code remain unchanged |

The public Git smoke used `https://github.com/kubernetes-sigs/metrics-server` pinned to commit `d66279c6426c6581d9656fe3d42bc52db7c29597`, subpath `manifests/base`. The commit was resolved from the official v0.8.0 annotated-tag API. Actual scan exit 0. An initial actual failure exposed a missing shallow protocol capability; adding the required capability fixed the root cause without invoking a Git executable in scanner code.

A throwaway local TLS fixture then invoked the actual CLI for authenticated Git and an OCI Helm dependency. Git and OCI scans exited 0. Missing auth, redirect rejection and 50ms whole-operation timeout exited 4. Offline Git exited 4 and offline OCI exited 1; the fixture counted **zero additional requests** during both offline operations. The OCI archive digest was verified. Requests were limited to Git upload-pack POST and OCI manifest HEAD/GET plus blob GET; synthetic certificates and explicit mode-0600 credentials were private. The fixture program and diagnostic probe were removed after proof. This direct-blob registry proof does not cover public signed-CDN redirects.

## Reports and browser

Actual table/JSON/CSV/HTML commands rendered a synthetic three-status fixture carrying **three real cached official metrics-server evidence records**. Exact component IDs, evidence IDs, verdicts and overall decision were checked across all formats; two distinct support records share a URL, so this is not an empty-provenance parity check. CSV retains formula neutralization and appends `evidence_ids`; HTML uses component/evidence data attributes.

Existing desktop/mobile filters, reset and keyboard-focus checks were supplemented using existing Chromium with an explicit 1100×800 viewport. Nine evidence links referenced three distinct evidence IDs. While offline, an **actual click** attempted one navigation to the official README URL; interception aborted it before external network access. This proves safe URL intent, not successful evidence-page retrieval. It does not claim zero attempted navigation after clicking.

The current evidence-bearing report was also exercised at **390×844**, offline: COMPATIBLE selected exactly one `synthetic-0` row (not INCOMPATIBLE); Namespace focus plus Tab reached Controller; nine links retained three distinct IDs. Narrow viewport screenshots were inspected for stacked controls, visible focus and scroll-reachable wrapped evidence columns. No evidence navigation was attempted in this mobile check.

## Safety and unresolved gates

No fresh live scan was performed merely to reconfirm earlier results. No cluster mutation, kind installation, workload seeding, Secret/Helm-storage payload access, exec, restart or drain occurred. Four real controller scenarios, least-privileged identity/audit proof, separately authorized live Helm storage, independent security/source review, and unexercised native platforms remain blocked. See [source health](source-health.md), [support matrix](support-matrix.md) and [artifacts](artifacts.md).
