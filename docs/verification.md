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

Followup development verification on 2026-10-30 caught a gap in that earlier fixture: the actual readiness-heavy artifact report had 723px document width in a 390px viewport because ordinary finding IDs/scopes did not wrap. Inherited `overflow-wrap:anywhere` fixes text wrapping without hiding or truncating content, changing fonts, or removing the table's focusable horizontal scroll region. Real Chromium assertions on both the actual readiness assessment and the nonempty-evidence fixture measured document widths equal to the **320px and 390px** viewports; filter/reset and keyboard table scrolling passed. All nine evidence links remain intact. Desktop 1280px geometry for headings, notice, controls, table region and details was identical with and without the new rule. Screenshots were visually inspected; private numeric/visual receipts are under `.work/wrap-proof/`. Affected report/CLI tests, full Go tests, race tests and vet passed with Go 1.27.3. These are development receipts, not independent release approval.

## Existing-controller live acceptance

Current observed receipt: **2026-10-30T18:37Z**, user-approved existing-only Helm/ArgoCD/mixed scenarios on `tbs-dev`, with bounded release-storage reads authorized only in selected namespaces. **Flux DEFERRED**, neither passed nor failed. Private case aliases omit internal names. Runtime evidence uses reviewed ownership-corrected bytes (SHA256 `053233fa98701feba7cab648f7b4ec455f76a9a09b53a27ae34a3c5a9e527d48`); subsequent clean artifact receipts bind their own exact source revision without rewriting this historical live observation.

| Case | Actual collection | Independently asserted observations |
| --- | --- | --- |
| H0 | Namespace H, storage off; scan 0 | One direct installation; one Deployment/ReplicaSet/Pod; no storage requested. |
| H1 | Namespace H, explicit storage opt-in/fallback scope; scan 0 | One direct/Helm installation, no duplicate workload; release/chart/app labels, requested images and Pod running image ID match safe metadata/status projections. One deployed release Secret, zero release ConfigMaps. |
| A | All workload namespaces, storage off; scan 0 | 33 Argo Application identities; selected Application UID, single-source index/path/revision, sync state and other-destination workload UID match independent GETs. Chart rendering does not imply a Helm release. |
| M | All workload namespaces, storage only selected M; scan 0 | Exact independent set of **596** Deployment/StatefulSet/DaemonSet UIDs retained; 33 Argo identities and one decoded Helm installation; ten history records collapse to one installation joining 38 unique resources. Seven direct controller identities match this release; no duplicate component IDs or multi-owner resource UIDs. Other Helm namespaces were not decoded. |

Sequential A/M snapshots contain **942/935** components; transient observations changed between calls, so the entire count delta is **not** attributed solely to joining. All four offline checks against existing cached evidence/target 1.35 exit **2 / BLOCKED**; HTML reports exit **0** retaining that decision. Optional absent APIs remain NOT_PRESENT/NOT_SERVED; SQL storage remains unsupported. These observations establish collector correctness, not an upgrade pass.

Pinned Helm 4.3.1 `install.go:405–411` passes `rel.Namespace` into the metadata visitor; `validate.go:233–235` writes it to the release-namespace ownership annotation ([sources](discovery.md#sources)). Parent review corrected an earlier storage-namespace fixture error; it first failed, then passed after separating storage/history identity from unique release-destination ownership. **Only corrected tests**, not same-namespace live M, prove different storage/destination namespaces. Two storage installations sharing a destination/release tuple remain unmerged with PARTIAL/AMBIGUOUS_MANAGEMENT. Competing Helm/Argo ownership likewise stays unmerged: its regression first observed INVALID_INPUT/1, then passed with PARTIAL coverage and INCOMPLETE/3. Both use a nonempty known-COMPATIBLE Gate baseline (0), isolating ambiguity as the reason for 3. An actual sanitized throwaway saved-inventory CLI conflict check also exits 3; no live collision was seeded.

Focused regression, full Go tests, full race tests and vet passed after the final runtime change. Test-server requests prove namespace-scoped GET Secret/ConfigMap paths, `owner=helm`, limit 200, default zero storage reads, pre-API invalid/broad/nonlive/unauthorized scope rejection and distinct destination identity. They are **not a live HTTP trace or server audit**; neither was available. No telemetry/proxy/trace flags or RBAC were added; existing credential least privilege is unproven.

Safe independent observations projected allowlisted metadata/status **before stdout**; release payload decoding stayed in scanner memory. No values/env/certificates/credentials or raw Secret bodies were exported. Private inventories/reports/projections are 0600 under 0700 `.work/live-acceptance/`; current `ownership-live-proof.json`, binary identity, independent UID set and `ownership-{H0,H1,A,M}` receipts are retained. Earlier namespace-limited/scoped candidate receipts remain private **superseded history**, not current proof.

## Safety and unresolved gates

No cluster mutation, installation/seeding, RBAC change, exec, restart, drain or new source refresh. Flux remains deferred; multi-source/controller transitions/natural collisions, actual server-audit/least-privilege proof, independent security/source review and other native platforms remain unproven. Partial source evidence remains partial. See [source health](source-health.md), [support matrix](support-matrix.md) and [artifacts](artifacts.md).
