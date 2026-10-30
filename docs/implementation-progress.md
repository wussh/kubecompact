# Implementation status

Observed 2026-10-30 before the implementation commit. This is a dated product status receipt, not the implementation plan or an assertion about the current worktree. The approved plan and design remain in the separate infrastructure repository.

The development CLI implements safe live/file/Git inventory, Helm/Kustomize rendering, controller ownership correlation, fifteen compile-time source adapters, bounded HTTPS acquisition, private SQLite evidence, deterministic compatibility/candidate evaluation, API/schema/static checks, and table/JSON/CSV/standalone HTML reports. Implementation is not equivalent to completed release acceptance.

## Exercised behavior

- Existing read-only cluster observation: K3s 1.33.13+k3s2, 5 nodes, 6,578 resources, 940 components and 33 Argo Applications. Helm storage was **not requested**. Flux APIs were absent/not served. No cluster changes or payload publication occurred.
- Actual file CLI smokes cover raw YAML, Helm values, Kustomize, removed APIs, malformed YAML, invalid schema, absent saved-schema validation inputs, deprecated-request summaries and mixed ownership fixtures.
- Actual pinned public Git acquisition and explicitly authenticated local HTTPS Git/OCI dependency acquisition are exercised. The local fixture used a custom CA; missing authentication, redirect handling, timeout/cancellation and offline zero-request boundaries were exercised. Fixture credentials and certificates are not published.
- Offline candidate evaluation against real cached official metrics-server support and release evidence returned `UPGRADE_REQUIRED` and exact candidate 0.4.5 for a **synthetic** 0.3.0 observation. Both candidate proof IDs were exported. This does not claim that version is deployed anywhere.
- Nonempty report provenance parity was checked across JSON/CSV/HTML/table: three synthetic component statuses and three real evidence records, including distinct claims from the same URL. Chromium performed an actual evidence-link click while offline; the intentional navigation was intercepted and aborted, not fetched.
- The latest source refresh had fifteen aggregate successful project records and **three source-level partial records**. See [source health](source-health.md).

## Conservative boundaries

Only target Kubernetes 1.35 is accepted by the pinned API/schema catalog. Bounded GitHub release history does not prove a branch's latest patch. A support claim conditional on latest-patch policy therefore remains `UNKNOWN` when that policy cannot be independently established. Chart index metadata alone is not a runtime support statement.

Input receipts are retained at inventory and assessment level even for Service/PDB/CRD-only inputs without workload components. Local input roots are hashed, not exported; Git identity uses the repository, pinned revision and subpath. Safe copy boundaries re-open untrusted inputs with bounded no-escape reads.

## Not accepted for release

Four real Helm/Argo/Flux/mixed-controller scenarios, least-privileged identity plus audit proof, separately authorized Helm-storage collection, and independent security/source review remain **blocked**. No kind cluster, installation, apply, workload seeding, exec, restart or drain is authorized. There is no fallback to synthetic fixtures for these gates.

Artifact and platform scope is recorded in the dated PRECOMMIT [artifact receipt](artifacts.md). No release, tag, push or publication had occurred at observation time. The baseline did not identify the then-uncommitted implementation bytes; subsequent generated artifact receipts carry their own exact commit metadata without rewriting this historical record.
