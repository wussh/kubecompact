# API, schema and static readiness

Readiness findings do not set a component's documented runtime-support verdict. A compatible application can still have a removed manifest API or an unsafe upgrade path.

## Pinned target data

Exactly Kubernetes **1.33, 1.34 and 1.35** are accepted. `1.N`, `v1.N`, `1.N.0` and `v1.N.0` resolve to the strict schema patch `1.N.0`, recorded as `resolvedTarget`. Other minors, exact patches after `.0`, prereleases and suffixes fail before collection. There is no fallback to 1.35, no default master/latest schema and no inferred target from a distribution release.

| Target | Lifecycle source (Kubernetes API) | Strict schema files | Bundle SHA256 |
| --- | --- | --- | --- |
| 1.33.0 | `v0.33.0` commit `82c0029eaa2f5bdbfbbbce9305349a893e6c86dc` | 698 | `453c52378dab91a54eb09657eb45564dd4ba30fa55f4c42a01ff225e4d715ecc` |
| 1.34.0 | `v0.34.0` commit `ae6c3b911ef17a4e2b10922c745c6f35bcf9b78e` | 721 | `720756beb922c2ed5f834fef4dc818f195314eec5e4d8b3b1114f205fc31350e` |
| 1.35.0 | existing `v0.35.0` lifecycle pin `9afe7de0a56c582b06ca094f7309015bc84657a7` | 726 | existing pinned bundle, unchanged |

- API catalogs: `data/api-lifecycle-1.33.0.json`, `data/api-lifecycle-1.34.0.json` and the existing 1.35 catalog, each with per-entry upstream lifecycle-source URLs and replacements, Apache-2.0 license and immutable source revision. Explicit CRD/APIService and Endpoints policy entries supplement generated metadata; Endpoints deprecation at 1.33 is not API removal.
- Strict schemas: `data/schemas/kubernetes-1.33.0.json.gz.b64` and `kubernetes-1.34.0.json.gz.b64`, selected from kubernetes-json-schema revision `7464e8eb55dab7063a4e6cb6cd6e2ed19f399580`. Matching `provenance-1.33.0.json` / `provenance-1.34.0.json` records the target, upstream source, complete selected-file inventory digest, bundle digest and every selected content hash. The existing 1.35 catalog, provenance and both licenses are byte-identical to the baseline.
- Each target is lazily loaded and integrity-checked at most once. A corrupt, wrong-target or over-limit bundle fails that target only; it never falls back to another dataset. Returned schema maps are read-only to callers.
- Kubeconform v0.8.0 validates only staged, explicitly selected local schemas in strict mode with the resolved target version. Normal checks have no schema HTTP fallback. External references and schema ID redirection are rejected. Local schema directories must contain the matching `v1.N.0-standalone-strict` tree and an exact content match with the embedded target bundle.

Lifecycle and schema availability is static API readiness, not runtime support for the exact distribution patch. A K3s release identity is separate from the upstream schema patch: for example, the first K3s 1.34 release (`v1.34.1+k3s1`) is compared against the pinned 1.34.0 schema catalog, so exact-patch coverage remains UNKNOWN rather than being inferred. The read-only plan in [usage](usage.md#upgrade-plan) keeps this distinction explicit.

Custom resources use the matching observed CRD served-version schema; a missing/mismatched schema remains UNKNOWN. Unknown catalog kinds are not presumed available. Schema errors are sanitized reason codes, never raw confidential values. Saved inventories omit raw documents/schemas and therefore cannot reproduce schema validation; use original files or explicit live `check` when required.

Lifecycle check `details` retain the catalog replacement and project its target availability from the existing pinned replacement GVK's introduced boundary: `replacementAvailable` is `true`, `false`, `unknown` (unmapped/missing introduction), or `not-applicable` (no replacement). Known mappings include `replacementIntroduced` and `replacementSource`. These are evidence metadata in the existing string map, not a new verdict or an automatic migration recommendation; finding IDs/scopes and gates remain unchanged.

## Static observations

Checks inspect CRD served/storage/storedVersions and conversion Service references; APIService availability; admission review versions, rules and service availability; Service/EndpointSlice relationships; PDB disruption observations; workload generation/readiness; target node skew and direct control-plane upgrade distance. PDB findings concern worker disruption, not proof a control-plane-only operation drains nodes.

No admission/conversion webhook, CEL expression, server dry-run or application code is executed. No drain, upgrade, exec, metrics scrape or audit-log configuration occurs. A single server version does not prove the versions/health of every control-plane member, and a supplied node list is not proof of complete membership; readiness keeps control-plane completeness UNKNOWN. Missing observations stay UNKNOWN.

Optional deprecated-client request summaries are bounded to 1 MiB and a caller-supplied start/end interval. They distinguish absent/incomplete input, APIs outside catalog coverage, deprecated requests and requests to removed APIs. A removed request is a BLOCKER even when inventory is empty. A complete zero-request interval certifies only the supplied observation window, not all historical or future clients.

## Exercised behavior

The actual CLI returned exit 2 for `testdata/readiness/removed.yaml` at target 1.35 and retained `API_REMOVED` independently of missing schema/usage evidence. Regression tests exercise API boundaries, missing/invalid/local/custom schemas, external references, CRD/webhook/PDB observations and usage intervals. A read-only saved-live-inventory check produced findings with raw-document validation correctly unavailable; it did not certify an upgrade.

## Finite lifecycle conformance references

`internal/readiness/testdata/lifecycle-conformance.json` is a hand-reviewed, finite facts corpus, not a second engine or a replacement catalog. It pins official Kubernetes source/catalog digest, Pluto `9495152d614581a988e31414fa64e8b82474d956` (`versions.yaml`) and Kubepug `5a18f77bbd6cadd5ce2469cd98e142fbf0bae497` (`generator/deprecations/gen.go`), input SHA256s and generator revision `lifecycle-conformance/v1`. Upstream URLs are `https://github.com/FairwindsOps/pluto` and `https://github.com/kubepug/kubepug`. Pure evaluator cases cover introduced, deprecated/served, before/at removal, replacement availability and no replacement against the pinned 1.35 catalog; the conformance corpus does not by itself broaden or approve target CLI minors.

Pluto supplies no introduced field. Kubepug is a **static generator-contract reference**, not an executed generated dataset: its `APIDeprecation` fields are introduced/deprecated/removed/replacement, with no replacement-availability field. Replacement availability is independently joined to the official replacement GVK introduced boundary; it is not attributed to Kubepug. Current official v0.35 coverage omits PodSecurityPolicy, so its Pluto historical entry remains UNKNOWN rather than being inserted into runtime data. DeploymentRollback has no replacement function and no Pluto exact-GVK overlap. Pluto's extensions Deployment deprecation 1.9 conflicts with official generated metadata 1.8: the pinned official lifecycle tags adjudicate 1.8; GA/replacement availability is not deprecation timing. Official adjudication: `https://kubernetes.io/docs/reference/using-api/deprecation-guide/`, plus each catalog entry's immutable source URL.

Regenerate the deterministic **review report**, never the runtime catalog, from separately acquired pinned inputs:

```sh
go run ./tools/lifecycleconformance PATH_TO_PINNED_PLUTO/versions.yaml PATH_TO_PINNED_KUBEPUG/generator/deprecations/gen.go
go test ./internal/readiness ./tools/lifecycleconformance
```

The generator verifies recorded input digests, sorts exact selected GVKs and emits both source fields, the official joined replacement introduction, and explicit coverage/conflict records. It compares Pluto's normalized replacement-available-in with that official boundary; missing reference data or official replacement mappings remain explicit rather than becoming agreement. Repeating identical inputs is deterministic. Save/diff its output when reviewing a reference update; additions/removals/field conflicts require explicit adjudication and corpus edits, never automatic repinning or runtime override. Normal tests/CI use checked-in finite facts and the embedded catalog only, without local clones/network/upstream CLIs. The official catalog digest assertion deliberately fails on drift until reviewed. References are Apache-2.0; see [notices](../THIRD-PARTY-NOTICES.md). This finite overlap is not full-catalog certification or addon runtime support.
