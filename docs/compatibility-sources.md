# Compatibility sources

KubeCompat has fifteen compile-time registered official-source adapters. `adapters.All()` exposes the registry; `Sources(component)` expands only reviewed upstream URL templates for an observed application semantic version. `Parse` is deterministic and performs no I/O. The CLI refresh queue uses the shared policy-enforcing fetcher and transactional store, including bounded reviewed-link discovery. Offline evaluation never invokes adapters' fetch paths.

## Claim boundaries

| Project | Official source / parser | Extracted claim |
| --- | --- | --- |
| cert-manager | [supported releases](https://cert-manager.io/docs/releases/) | Supported and tested ranges separately; historical rows preserve EOL. Current branches conditional on latest-patch policy remain UNKNOWN: the maximum of bounded release history is not proof of the latest patch. |
| Argo CD | [tested matrix](https://argo-cd.readthedocs.io/en/stable/operator-manual/tested-kubernetes-versions/) / version-tagged upstream Markdown | Tested only; table inclusion is not vendor runtime support. |
| metrics-server | [official README](https://github.com/kubernetes-sigs/metrics-server#compatibility-matrix) | Minimum Kubernetes version preserves an open upper bound. |
| Cilium | [versioned compatibility](https://docs.cilium.io/en/v1.18/network/kubernetes/compatibility/) | Guaranteed compatible and tested list; outside list is untested, not automatically incompatible. |
| Calico Open Source | [versioned requirements](https://docs.tigera.io/calico/3.33/getting-started/kubernetes/requirements) | Explicit Calico version and tested Kubernetes list; not Tigera enterprise/operator identity. |
| Flux distribution | [v2.7.0 release](https://github.com/fluxcd/flux2/releases/tag/v2.7.0) / GitHub release JSON | Exact distribution version compatibility; per-minor minimum patches (including 1.34.1). Does not apply distribution versions to individual controllers. |
| CoreDNS | [versioned Kubernetes plugin](https://github.com/coredns/coredns/blob/v1.12.4/plugin/kubernetes/README.md) | Plugin context only: this source publishes no Kubernetes version support matrix. Never infer support from plugin existence or Go dependencies. |
| ingress-nginx | [official supported versions](https://github.com/kubernetes/ingress-nginx#supported-versions-table) | E2E tested set, not blanket support. Other ingress implementations excluded. |
| Prometheus Operator | [v0.86.1 prerequisites](https://github.com/prometheus-operator/prometheus-operator/blob/v0.86.1/README.md#prerequisites) | Minimum requirement, not sufficient proof of runtime support; kube-prometheus-stack chart version is not operator version. |
| Velero | [official compatibility matrix](https://github.com/velero-io/velero#velero-compatibility-matrix) | Expected supported range and exact tested versions independently. |
| KEDA | [versioned cluster requirements](https://keda.sh/docs/2.18/operate/cluster/) | Tested matrix only; TBD rows are absent evidence. |
| Kyverno | [release schedule](https://kyverno.io/docs/installation/releases/) | Explicit release and tested window; prose makes no guarantees outside the tested window. |
| Istio | [supported releases](https://istio.io/latest/docs/releases/supported-releases/) | Supported Kubernetes versions, tested union, and maintenance/EOL independently. Development master excluded. |
| Longhorn | [versioned installation requirements](https://longhorn.io/docs/archives/1.10.0/deploy/install/) | Minimum Kubernetes requirement plus host prerequisite explanation, not complete runtime certification. |
| Rook-Ceph | [versioned prerequisites](https://rook.io/docs/rook/v1.18/Getting-Started/Prerequisites/prerequisites/) | Explicit supported Kubernetes interval; storage/kernel prerequisites remain independently unverified. |

All source statements above were directly inspected on 2026-10-29. The Flux GitHub HTML release was accessible; a direct GitHub API inspection returned HTTP 403 in this research environment. Actual scanner update health must reflect its own fetch result rather than treating this receipt as a successful API refresh. The original Longhorn non-archive install URL returned 404; the archived versioned URL was accessible. Calico version URLs may redirect to current docs; the parser uses the product version stated in the document, never a redirected URL's assumed version.

## Provenance and format failures

Rules retain canonical project, application constraint, Kubernetes constraint, distinct claim type, original row/excerpt, original/final URL, SHA-256 of fetched bytes, retrieval/verification/expiry timestamps, parser ID/version and confidence. Evidence expires after 30 days. Dates in the future and stale evidence cannot establish support. Synthetic regression excerpts are attributed in `testdata/sources/official-regression.json`; they are not fetched production evidence or an exhaustive copy of upstream documentation.

Markdown and HTML table paths require recognized project-specific headers and expected column counts. Invalid versions produce `PARSER_FORMAT_CHANGED`; an absent table/claim produces `EVIDENCE_MISSING`; CLI fetch failures produce `SOURCE_FETCH_FAILED`. Failed extraction never saves new verification timestamps. Empty extraction never becomes an empty supported range.

GitHub release metadata is fetched separately from `/repos/{owner}/{repository}/releases`, using the default bounded first page (normally 30 records). Only exact stable published, non-draft, non-prerelease tags count as releases. This is deliberately not an exhaustive release history. Releases alone cannot establish compatibility. A recommendation requires independent authoritative target support, a released exact version, and no unresolved prerequisites. A Helm-managed component additionally requires a proven application-to-chart mapping; absence suppresses the recommendation rather than guessing from chart appVersion.

## Evaluation

Runtime support, tested coverage, lifecycle, chart installation constraints and known issues remain separate. Equal-priority contradictory support claims return UNKNOWN. Specific known issues survive generic matrix support. Chart/schema success cannot establish runtime support. Environment prerequisites not observed by the inventory remain explicit unknown checks. Final-target support does not establish a multi-hop upgrade sequence (`upgradePathStatus: NOT_EVALUATED`).

Actual online source-refresh and offline candidate receipts are recorded in [source health](source-health.md) and [verification](verification.md). Existing live validation was read-only; absent controllers remain coverage gaps, not invitations to install fixtures. Deterministic parser fixtures are not controller-backed acceptance proof.
