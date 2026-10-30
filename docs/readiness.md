# API, schema and static readiness

Readiness findings do not set a component's documented runtime-support verdict. A compatible application can still have a removed manifest API or an unsafe upgrade path.

## Pinned target data

Only Kubernetes 1.35 / 1.35.0 is currently accepted. Minor requests resolve to schema patch 1.35.0, recorded as `resolvedTarget`. Other minors and exact patches fail before collection; there is no default master/latest schema.

- API catalog: `data/api-lifecycle.json`, generated from Kubernetes API v0.35.0 source revision `9afe7de0a56c582b06ca094f7309015bc84657a7`, with per-entry upstream lifecycle-source URLs and replacements.
- Explicit CRD/APIService and Endpoints policy entries supplement generated metadata. Endpoints deprecation at 1.33 is not API removal.
- 726 strict standalone schema files: Kubernetes 1.35.0, kubernetes-json-schema revision `7464e8eb55dab7063a4e6cb6cd6e2ed19f399580`. `data/schemas/provenance.json` records the bundle and individual content hashes. Apache-2.0 notices are retained under `data/`.
- Kubeconform v0.8.0 validates only staged, explicitly selected local schemas in strict mode. Normal checks have no schema HTTP fallback. External references and schema ID redirection are rejected.

Custom resources use the matching observed CRD served-version schema; a missing/mismatched schema remains UNKNOWN. Unknown catalog kinds are not presumed available. Schema errors are sanitized reason codes, never raw confidential values. Saved inventories omit raw documents/schemas and therefore cannot reproduce schema validation; use original files or explicit live `check` when required.

## Static observations

Checks inspect CRD served/storage/storedVersions and conversion Service references; APIService availability; admission review versions, rules and service availability; Service/EndpointSlice relationships; PDB disruption observations; workload generation/readiness; target node skew and direct control-plane upgrade distance. PDB findings concern worker disruption, not proof a control-plane-only operation drains nodes.

No admission/conversion webhook, CEL expression, server dry-run or application code is executed. No drain, upgrade, exec, metrics scrape or audit-log configuration occurs. A single server version does not prove the versions/health of every control-plane member. Missing observations stay UNKNOWN.

Optional deprecated-client request summaries are bounded to 1 MiB and a caller-supplied start/end interval. They distinguish absent/incomplete input, APIs outside catalog coverage, deprecated requests and requests to removed APIs. A removed request is a BLOCKER even when inventory is empty. A complete zero-request interval certifies only the supplied observation window, not all historical or future clients.

## Exercised behavior

The actual CLI returned exit 2 for `testdata/readiness/removed.yaml` at target 1.35 and retained `API_REMOVED` independently of missing schema/usage evidence. Regression tests exercise API boundaries, missing/invalid/local/custom schemas, external references, CRD/webhook/PDB observations and usage intervals. A read-only saved-live-inventory check produced findings with raw-document validation correctly unavailable; it did not certify an upgrade.
