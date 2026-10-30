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

Standalone Helm release bodies require separate authorization and `--helm-storage --namespace AUTHORIZED_NAMESPACE`. This reads Secrets and ConfigMaps in memory. Never use it merely because the credential happens to permit it. See [security](security.md) and [discovery](discovery.md).

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

The current API/schema catalog supports only `1.35`, `v1.35`, `1.35.0` and `v1.35.0`; minor input resolves to recorded schema patch `1.35.0`. Other targets are rejected before collection. Final-target support does not prove a supported direct or intermediate upgrade sequence.

Use manifest/live `check` when schema validation matters. Saved sanitized inventory deliberately omits raw documents and CRD schemas; schema checks from saved inventory report `RAW_DOCUMENT_UNAVAILABLE`, never an invented pass. Live GETs do not recover the API versions used by historical clients.

`--schemas DIRECTORY` selects a trusted local schema directory. `db update --schemas DIRECTORY --target-k8s 1.35` explicitly acquires the pinned bundle. Normal checks never fetch schemas.

Optional `--deprecated-usage FILE` accepts existing aggregated JSON, not raw audit logs:

```json
{"start":"2026-01-01T00:00:00Z","end":"2026-01-02T00:00:00Z","complete":true,"requests":[{"group":"policy","version":"v1beta1","kind":"PodDisruptionBudget","count":2}]}
```

Findings apply only to that supplied observation interval. Missing/incomplete input is UNKNOWN. No audit logging is enabled and no logs or metrics are scraped.

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
| 0 | Complete inventory, report rendering success, or assessment with no known blockers and no required unknowns. Not runtime certification. |
| 1 | Invalid command, input or unsupported target. |
| 2 | Proven compatibility/readiness blocker; unknown findings remain visible. |
| 3 | Incomplete collection/evidence or unresolved required checks; partial source refresh. |
| 4 | Operational failure, such as unavailable configuration/cache/output. |

`report --input` returns rendering success/failure; the assessment's original exit code remains in its output. `scan` returns collection status, not a compatibility verdict.

## Identity, private sources and rendering

`--identity-mappings FILE` is a JSON object mapping exact image repository names to registered project IDs. It asserts identity, not authoritative support. Unknown/custom applications remain in inventory.

Git acquisition requires `--git-url HTTPS_URL --git-revision FULL_40_HEX_SHA --git-path DIRECTORY --online`. Branch/tag discovery, credential helpers, SSH, hooks, LFS and submodules are not invoked. Kustomize remote bases use `https://host/org/repo//directory?ref=FULL_40_HEX_SHA` and the same bounded Git transport; remote inputs are denied offline. Helm requires an explicit target and values inside the chart root; hooks are excluded, lookup/tpl and external plugins/post-renderers are denied. Rendering is not an exact reproduction of controller-specific options or proof of deployed state.

Inventory and assessment carry top-level `sources` receipts even when a Service/PDB/custom-resource input has no workload components. Receipts contain input/dependency digests and renderer versions, not private local paths or manifest payloads. Component source references remain available for ownership/version attribution.

CSV appends `evidence_ids`; table output includes exact evidence and component IDs; HTML carries `data-component-id` and `data-evidence-id`. These references distinguish separate claims from the same URL. A report's success exit 0 does not replace the embedded assessment decision.

Private origins require `--allow-hosts host[:port]`, optional additional `--ca-file`, and explicit mode-0600 `--credentials-file` origin/header configuration. No insecure TLS switch or ambient Git/registry credential helper exists. See [cache and transport](cache.md).

See [readiness](readiness.md), [compatibility sources](compatibility-sources.md), and [tested support](support-matrix.md) for the boundaries behind each result.
