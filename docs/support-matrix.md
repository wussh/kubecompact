# Exercised support and release gates

Date: 2026-10-30, observed before the implementation commit. This is a dated implementation verification record, not a product release or upgrade certification. The approved plan remains in the separate infrastructure repository. Original product baseline: `a561f6eaafa617a6504847441722a170c9426bdc`; implementation branch: `feat/scanner`. That baseline did not identify the implementation bytes then uncommitted; generated postcommit artifacts have independent exact-revision receipts.

## Observed environments

| Surface | Evidence | Boundary |
| --- | --- | --- |
| Linux amd64 CLI | Built and exercised on Linux/WSL; raw file, Helm override, Kustomize, live scan, saved-inventory check and all report formats. | Other operating systems/architectures are not runtime-certified by cross-compilation. |
| Kubernetes | Actual default-secret-free read-only scan of existing `tbs-dev`, K3s `v1.33.13+k3s2`; five nodes, 6,578 resource observations, 940 components. | Existing credential, no newly provisioned base Role or audit policy. Resource counts are a point-in-time receipt, not a performance target. |
| Argo CD | 33 Application objects observed through served `argoproj.io/v1alpha1` APIs; ApplicationSet API present. | This is an existing-controller observation, not the full isolated multi-source/controller transition acceptance scenario. |
| Flux | HelmRelease/Kustomization/source APIs reported NOT_PRESENT / NOT_SERVED. | No Flux controller-backed end-to-end pass. No controller/CRD was installed to manufacture one. |
| Helm releases | Synthetic Helm 3/4 SDK decoding/status/confidentiality regressions; actual local Helm SDK render with values override. | Live `--helm-storage` was not authorized/exercised. No Secret or ConfigMap payload reads. |
| Target readiness | Kubernetes 1.35 minor resolves to pinned strict schema 1.35.0; removed policy/v1beta1 PDB produced actual CLI exit 2. | No other target minor/exact patch is accepted; no application execution/admission/drain. |
| Saved-live assessment | Actual offline check: 940 components, 16,602 readiness observations, BLOCKED / exit 2. | Does not certify the cluster upgrade. Raw-document schema checks are unavailable from sanitized saved inventory. |
| Reports | Exact component IDs, nonempty evidence IDs, verdicts and decision parity in JSON/CSV/HTML/table. Three distinct real evidence records were attached to a synthetic interaction fixture. Existing filter/mobile/focus checks were supplemented by an actual offline evidence-link click, with intentional navigation intercepted and aborted. | Synthetic compatibility labels are not official findings. No external evidence page was fetched by the click proof. |
| Source/cache | Actual fifteen-project public refresh and immutable SQLite writes; partial source health retained and reported nonzero. | Inspect every source-health record; release metadata success is not a support-matrix success. Online changes require fresh receipt. |
| Security dependency scan | Initial Go 1.27.2 binary affected by GO-2026-6809; toolchain moved to 1.27.3. Patched-source govulncheck: zero reachable symbol/package vulnerabilities, one required-module-only advisory. | This is not a container OS image scan or independent security approval. |

Real inventory/assessment files remain in ignored private `.work/live/`; internal names, values and endpoints are not published here. Test fixtures and public evidence are separate from live cluster data.

## Deterministic checks and executable smoke

- Full source tests, internal race suite, vet and actual binary build are rerun after source changes; latest receipts are in [verification](verification.md).
- Actual CLI Git acquisition used an official full 40-hex metrics-server commit. A local TLS fixture exercised explicit authentication, custom CA, direct OCI dependency blobs, missing authentication, redirect rejection, timeout/cancellation and zero offline requests.
- Actual raw smoke preserved main/init image observations and omitted Secret/env canaries; Helm override yielded two replicas; Kustomize produced the expected name prefix/namespace without loading kubeconfig.
- Removed API findings retain their blocker even alongside missing usage/schema observations.
- Local TLS/source transport, request guards, pagination/access errors, Helm/Argo/Flux state regressions, cache rollback/corruption, safe schemas and report injection are fixture proofs, not substitutes for the missing controller-backed acceptance scenarios.

Final test/race/vet/artifact receipts must match the source being packaged; older successful builds are not treated as proof for later edits. Candidate workflows create no release/tag/container publication.

## Release-blocking gates

1. Four unskipped controller-backed modes: Helm-only, Argo-only, Flux-only and mixed. Existing `tbs-dev` has no Flux and current authorization forbids installing controllers/fixtures/RBAC or seeding workloads. Synthetic CRs cannot substitute for running controllers.
2. Optional live Helm payload access and actual scanner-identity least-privilege/audit request proof. An existing privileged read-only scan cannot establish these permissions.
3. Independent source-conclusion/security review. Earlier delegated review attempts failed without findings; no independent approval is claimed.
4. Native OS/architecture execution beyond the exercised Linux amd64 environment. See [artifact receipts and limits](artifacts.md) for the separate container, SBOM, checksum and vulnerability gates.

No cluster mutation, upgrade, drain, restart, exec, release/tag or shared-branch push was performed. An unavailable required gate stays blocked, never skipped-to-pass.
