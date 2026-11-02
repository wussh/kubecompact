# Usage

Build with the Go version declared in `go.mod`:

```sh
go build -trimpath -o bin/kubecompat ./cmd/kubecompat
bin/kubecompat version
```

## Inventory

```sh
# Explicit trusted context; no Secret or ConfigMap payload reads.
bin/kubecompat scan --context tbs-dev --all-namespaces --output inventory.json

# No Kubernetes authentication or source networking, even with a missing kubeconfig.
KUBECONFIG=/nonexistent bin/kubecompat scan --manifests testdata/manifests/basic.yaml --output inventory.json

bin/kubecompat scan --manifests testdata/manifests/helm --render helm --values testdata/manifests/helm/override.yaml --target-k8s 1.35 --namespace example --output helm-inventory.json
bin/kubecompat scan --manifests testdata/manifests/kustomize --render kustomize --output kustomize-inventory.json
```

An explicit `--kubeconfig` or `--context` selects trusted client-go configuration. Without either, live collection uses in-cluster authentication, not implicit desktop kubeconfig execution. Namespace defaults to `default`; `--all-namespaces` conflicts with `--namespace`.

Standalone Helm release bodies require separate authorization and `--helm-storage --namespace AUTHORIZED_NAMESPACE`. For mixed/all-namespace workload discovery, use `--helm-storage --helm-storage-namespace AUTHORIZED_STORAGE_NAMESPACE --all-namespaces`: only that single storage namespace is queried. The override falls back to `--namespace` when omitted. Empty/broad/invalid resolved storage scopes, override without opt-in, and storage options on file/Git/saved inputs are rejected before collection. Release destination namespace remains distinct from storage namespace. Reads decode in memory; never enable them merely because the credential permits it. See [security](security.md) and [discovery](discovery.md).

File input accepts a regular YAML/JSON file or recursively loaded directory, multi-document streams and Kubernetes Lists. Secret payloads and arbitrary env/annotations never enter exported records. Duplicate keys, aliases, unsafe paths and malformed documents are diagnosed without their contents. Malformed input returns exit 1 even when earlier documents were readable. Controller/owner identities are correlated for file inputs too.

## Evidence and assessment

```sh
# Public source refresh only; no kubeconfig or cluster access.
bin/kubecompat db update --online --cache evidence/evidence.db --timeout 15m --output source-health.json

# check returns its assessment exit code, not always zero.
bin/kubecompat check --inventory inventory.json --target-k8s 1.35 --offline --cache evidence/evidence.db --format json --output assessment.json
bin/kubecompat check --manifests testdata/readiness/removed.yaml --target-k8s 1.35 --offline --format json --output removed-assessment.json
```

Offline evidence evaluation is the default. `--online` explicitly permits reviewed source requests; it does not transmit inventory, manifests or cluster credentials. `db update` is online by default and reports each source failure while retaining prior good records. A successful release-metadata fetch does not imply that the project's support matrix was available: inspect every coverage entry, including PARTIAL entries.

The current API/schema catalog supports `1.33`, `1.34` and `1.35`, including their `v`-prefixed and patch-`0` forms; each minor resolves to the recorded strict schema patch `1.N.0`. Other minors and exact patches after `.0` are rejected before collection. There is no fallback to another target and no master/latest lookup. Final-target support does not prove a supported direct or intermediate upgrade sequence.

Use manifest/live `check` when schema validation matters. Saved sanitized inventory deliberately omits raw documents and CRD schemas; schema checks from saved inventory report `RAW_DOCUMENT_UNAVAILABLE`, never an invented pass. Live GETs do not recover the API versions used by historical clients.

`--schemas DIRECTORY` selects a trusted local schema directory. `db update --schemas DIRECTORY --target-k8s 1.35` explicitly acquires the pinned bundle. Normal checks never fetch schemas.

Optional `--deprecated-usage FILE` accepts existing aggregated JSON, not raw audit logs:

```json
{"start":"2026-01-01T00:00:00Z","end":"2026-01-02T00:00:00Z","complete":true,"requests":[{"group":"policy","version":"v1beta1","kind":"PodDisruptionBudget","count":2}]}
```

Findings apply only to that supplied observation interval. Missing/incomplete input is UNKNOWN. No audit logging is enabled and no logs or metrics are scraped.

## Upgrade plan

```sh
KUBECONFIG=/nonexistent bin/kubecompat plan --from-k8s 1.33 --target-k8s 1.35 --distribution k3s \
  --plan-input plan-input.json --offline --cache evidence/evidence.db \
  --format json --output upgrade-plan.json
bin/kubecompat report --input upgrade-plan.json --format html --output upgrade-plan.html
```

`plan` is a strictly offline, read-only K3s proposal. It accepts source minor `1.32`, `1.33` or `1.34` and a supported strictly greater target minor, and generates every adjacent intermediate hop. Source `1.32` is observation-only, not a supported `check` target: its adjacent `1.32`→`1.33` hop, prerequisites, steps and stop conditions are still emitted, but without a pinned exact `1.32` release identity the source provenance stays UNVERIFIED/UNKNOWN and the plan exits 3. `--distribution` accepts only `k3s`. There is no `--online`, live/Git/manifest/Helm collection, credential/CA/allow-host/refresh or auto-latest flag; unknown or forbidden flags fail before input collection. It reads only `--plan-input`, explicitly referenced bounded sanitized inventories and the local evidence cache.

Plan input is versioned `kubecompat.io/plan-input/v1alpha1` with a cluster ID, source observation time, exact source K3s release, destination releases by minor and optional per-hop sanitized evidence: exact from/to releases, relative inventory file plus SHA256, observedAt/validUntil with an evidence owner, datastore kind, server identities, proposed member order and bounded operator receipts. Inventory paths must stay inside the plan-input directory, reject symlink escape and match the supplied digest. Receipts carry only kind, status, owner, observation/validity times, evidence ID and bounded safe notes; commands, tokens, credentials, backup payloads and raw manifests are not accepted. Freshness is self-attested by the supplied `observedAt`/`validUntil` and owner: the planner rejects stale or missing intervals but applies no independent maximum age, trusted clock source or external validity authority. A post-hop-health receipt must also not predate the hop inventory observation.

The plan emits every ordered hop with prerequisites, descriptive server-first one-at-a-time steps (agents only after all servers), conditional dependencies and explicit stop conditions. An unpinned source `1.32`, missing future inventory/release provenance or missing/stale receipts remain required UNKNOWNs and exit 3; a known blocker dominates with exit 2; malformed/conflicting input exits 1; and unavailable referenced inventory, cache failure or output failure exits 4. A supplied exact release not pinned in the reviewed policy remains unverified rather than being guessed. The plan never fabricates future member versions, never rewrites the supplied inventory and never claims rollback success. No execution, drain, restart, snapshot, upgrade or rollback command exists.

Saved plan output is dispatched by schema: `report --input upgrade-plan.json` validates `kubecompat.io/upgrade-plan/v1alpha1`, rejects assessment JSON and returns rendering exit 0 like a saved assessment. The stored plan and hop decisions remain in the output. Plan CSV uses the ordinary assessment columns plus `plan_decision`, `plan_exit_code`, `hop_id`, `hop_decision` and `hop_exit_code`; `row_type` adds `plan-summary`, `hop` and `step`. Filters only hide component rows and never alter plan/hop gates, global checks or coverage. HTML renders each hop in-document with no iframe, external asset or network request.

## Reports

```sh
bin/kubecompat report --input assessment.json --format table
bin/kubecompat report --input assessment.json --format json --output report.json
bin/kubecompat report --input assessment.json --format csv --output report.csv
bin/kubecompat report --input assessment.json --format html --output report.html
```

Saved-assessment rendering is pure offline and does not open the evidence database. A supplied target must match the saved target. Filters: `--namespace`, `--controller`, `--component`, `--status`, `--evidence-quality`. They never change the full assessment's overall decision/exit code. HTML includes keyboard-accessible local filters and no external assets. CSV encodes lists with `; `, confidence sets with commas, and prefixes formula-leading cells with an apostrophe. JSON retains canonical values. The running-image column includes only observed running images; requested/rendered images remain distinct in JSON.

Output files are written atomically with mode 0600. Their parent directory must already exist. The evidence directory must be private (0700), and its database private (0600); existing permissive directories are rejected, not silently chmodded.

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | No BLOCKER-severity finding and no required UNKNOWN (complete inventory/assessment/plan), or successful saved report/saved-plan rendering. Non-blocking FAIL/WARNING operational findings (for example `API_DEPRECATED` or PDB `disruptionsAllowed=0`) remain visible and are not excluded from review. Not runtime certification or execution permission. |
| 1 | Invalid command, malformed plan input, conflicting identity or unsupported target. |
| 2 | Proven compatibility/readiness/plan blocker; unknown findings remain visible. |
| 3 | Incomplete collection/evidence, unresolved required checks or missing future plan-hop evidence; partial source refresh. |
| 4 | Operational failure, such as unavailable configuration/cache/referenced inventory/output. |

`report --input` returns rendering success/failure; the assessment's or plan's original exit code remains in its output. `scan` returns collection status, not a compatibility verdict. `plan` returns its plan gate: 1 for invalid input, 2 for blockers and 3 for required unknowns.

## Identity, private sources and rendering

`--identity-mappings FILE` is a JSON object mapping exact image repository names to registered project IDs. It asserts identity, not authoritative support. Unknown/custom applications remain in inventory.

Git acquisition requires `--git-url HTTPS_URL --git-revision FULL_40_HEX_SHA --git-path DIRECTORY --online`. Branch/tag discovery, credential helpers, SSH, hooks, LFS and submodules are not invoked. Kustomize remote bases use `https://host/org/repo//directory?ref=FULL_40_HEX_SHA` and the same bounded Git transport; remote inputs are denied offline. Helm requires an explicit target and values inside the chart root; hooks are excluded, lookup/tpl and external plugins/post-renderers are denied. Rendering is not an exact reproduction of controller-specific options or proof of deployed state.

Inventory and assessment carry top-level `sources` receipts even when a Service/PDB/custom-resource input has no workload components. Receipts contain input/dependency digests and renderer versions, not private local paths or manifest payloads. Component source references remain available for ownership/version attribution.

CSV is one typed file: assessment `row_type` is `component`, `summary`, `check` or `coverage`; a plan adds `plan-summary`, `hop` and `step` and prefixes every row with plan/hop decision and exit columns. Existing component columns are retained; appended columns carry domain, stable scope, severity, JSON details, collector, kinds and omissions. Every data row carries the full overall decision/exit code; summary/check/coverage rows exist even with zero components. Read by column name and row type, not by assuming every row is an installation. `evidence_ids` preserves exact references even when evidence metadata is unavailable. Formula-leading cells are neutralized. Component filters never discard global checks or coverage, or recompute the decision. JSON remains the canonical assessment/plan contract.

All report formats collect top-level **and all component-local** checks before component filtering. Only identical records with the same ID/scope are deduplicated; differing reason/severity/details/evidence records remain separate, including conflicting records sharing an ID. This includes runtime support/exclusion/known-issue/tested/lifecycle findings produced under components, not just top-level readiness findings.

**Necessary JSON report projection change:** the existing top-level `checks` array now contains that complete collected set, even when its component is filtered out. Its contents are intentionally richer than the old top-level-only array. The JSON schema/field types and original nested component checks are unchanged; the input assessment is not mutated, no gate is recomputed, and the original overall decision/exit remains authoritative. Consumers should use the full projected check collection for handoff rather than infer its scope from currently visible components.

Table/HTML show a full-assessment inventory denominator and prioritize BLOCKER then required UNKNOWN groups by domain/state/severity/reason. HTML uses keyboard-operable native details for every group's IDs, scoped safe resource identity, supplied evidence IDs and raw details. Table groups show counts and at most three representative findings/scopes/details, with a pointer to JSON/CSV/expanded HTML for all IDs and raw detail; a thousand repeated checks do not produce a raw JSON dump in the default table. Resource identity is projected from already-collected references (API version/kind/namespace/name), never recovered from annotations or raw payloads; old saved assessments without identity keep their original unknown/opaque scope.

Counts are distinct canonical component IDs (installations), distinct collected controller workload references (Deployment, StatefulSet, DaemonSet, ReplicaSet, ReplicationController, Job, CronJob; cluster+UID when supplied, otherwise the existing stable resource key), and distinct observed running image identities (imageID, otherwise digest, otherwise exact image reference). Pods do not create installations/workload counts; these are controller-object counts, not independent application counts. Identical running image identities in separate installations count once as images but do not merge installations. Requested/rendered images are excluded. Counts describe supplied observations, not cluster completeness, and remain unchanged under presentation filters.

Next-evidence text is an explicit reviewed lookup from existing reason/details, not remediation or a new compatibility authority. PASS/NOT_APPLICABLE/INFO and compatible component states receive no missing-evidence prescription. Both table and HTML coverage actions are restricted to PARTIAL/UNREADABLE; COMPLETE records show neither a prescription nor an empty action label even when they retain a recognized gap code. Actionable unmapped reasons explicitly have no mapped action; substring coincidences such as CONVERSION do not create version advice. Replacement APIs appear only when supplied by the pinned catalog. Runtime support, API/schema/static readiness and coverage remain distinct. HTML has no external assets or HTTP requests. Report success exit 0 means rendering succeeded, never that the assessment passed.

`--deprecated-usage` already accepts bounded aggregate intervals/completeness. No concrete approved export has been supplied for a conversion recipe: intentionally no importer, metrics scrape or audit enablement is added. A future approved recipe must retain interval/completeness and must not convert presence gauges into request counts.

Private origins require `--allow-hosts host[:port]`, optional additional `--ca-file`, and explicit mode-0600 `--credentials-file` origin/header configuration. No insecure TLS switch or ambient Git/registry credential helper exists. See [cache and transport](cache.md).

See [readiness](readiness.md), [compatibility sources](compatibility-sources.md), and [tested support](support-matrix.md) for the boundaries behind each result.
