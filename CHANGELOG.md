# Changelog

## Unreleased — 2026-11-02

- Make complete evidence reachable without inferring membership: readiness keeps the control-plane observation conservatively UNKNOWN, and a plan replaces it only with a current operator `complete-membership` attestation (`CONTROL_PLANE_MEMBERSHIP_OPERATOR_ATTESTED`) while also binding `post-hop-health` receipts to the hop observation. Document plan-zero attestation, exit0 BLOCKER/UNKNOWN semantics and self-attested receipt freshness.
- Qualify Kubernetes 1.33, 1.34 and 1.35 with individually pinned lifecycle catalogs and strict schema bundles while preserving 1.35 bytes. Selection is explicit per target, lazily verified and never falls back; `resolvedTarget` stays separate from exact distribution release identity.
- Add the read-only, strictly offline K3s `plan` command: adjacent 1.32→1.35 hops, supplied sanitized per-hop inventories, server-first one-at-a-time ordering, HA/kubelet skew, datastore-specific backup and operator-receipt gates with explicit freshness, blocker dominance and required UNKNOWNs for missing future evidence. No execution, drain, upgrade, restore or rollback path exists.
- Add schema-validated saved-plan `report --input` dispatch and full JSON/CSV/table/HTML plan rendering with named CSV columns, per-hop decisions and stop conditions. HTML is self-contained with in-document hop sections instead of iframes; filters never change plan or hop gates.
- Pin only the reviewed K3s release identities `v1.33.0+k3s1`, `v1.34.1+k3s1` and `v1.35.0+k3s1`; an unpinned source such as optional 1.32 remains UNKNOWN instead of being invented. The exact-patch/schema-patch distinction is explicit.

## Unreleased — 2026-10-30

- Preserve zero-component decisions, global findings, scope/details and coverage in a single typed CSV; keep formula escaping and full-assessment decisions under component filtering.
- Add presentation-only installation/workload/running-image denominators and prioritized domain triage with inspectable IDs/details and bounded next-evidence text. Project safe resource identity from collected references without changing finding IDs, collection or runtime gates.
- Add a pinned finite lifecycle conformance corpus and deterministic review-only Pluto comparison; document Kubepug's static field-contract limits, official adjudications, UNKNOWN historical coverage and intentional no-code usage-export condition. Target CLI remains Kubernetes 1.35.
- Preserve producer-generated component-local findings in the full prefilter report collection, including an explicit complete top-level JSON check projection; deduplicate identical ID/scope records without discarding conflicts. Restrict healthy-coverage actions consistently across table/HTML and compare official/reference replacement-introduction boundaries in review reports.

- Add the read-only KubeCompat development CLI: live/file/pinned HTTPS Git inputs, safe Helm/Kustomize rendering, management ownership/version observations, pinned Kubernetes 1.35 API/schema/static checks, fifteen compile-time source adapters, immutable SQLite evidence and portable reports.
- Require independent authoritative support and exact release proof for upgrade candidates; chart-managed candidates also require an official matching chart/repository mapping. Unknown environment constraints remain conjunctive; release claims must match their own version range. Bounded release history does not establish latest-patch completeness.
- Preserve input digests and renderer/source provenance even when inputs contain no workload component. Reopen untrusted render/dependency inputs through bounded no-escape reads.
- Fix immutable shallow Git acquisition and permit only its exact read-only info/refs protocol query. Keep strict source URL, redirect, TLS, credential and offline boundaries.
- Retain malformed-YAML diagnostics, supplied deprecation-usage intervals and missing schema-validation inputs as distinct failures/gaps. Do not label requested images as observed running versions.
- Export exact candidate/evidence references across JSON, CSV, table and HTML. Add actual CLI, offline authenticated transport, browser-link and source-health receipts.
- Fix narrow HTML report overflow from unbroken readiness IDs and scopes by wrapping inherited body text; preserve complete provenance and the focusable horizontal table region. Browser assertions cover readiness-heavy and evidence-bearing reports at 320px, 390px and desktop widths.
- Permit all-namespace workload discovery with one separately authorized `--helm-storage-namespace`; retain namespace fallback, default zero storage reads, pre-API scope/opt-in/live-input rejection, distinct release destination namespace and scoped request/coverage boundaries.
- Separate Helm storage/history identity from release-destination workload ownership using authoritative `chart.releaseNamespace`; preserve ambiguous cross-storage ownership as incomplete instead of guessing a merge. Correct fixtures to pinned Helm ownership-annotation semantics.
- Add non-root artifact packaging, notices and candidate-only CI workflows. No release or publication is performed. Controller-backed acceptance, least-privilege audit and independent reviews remain blocked; see [support matrix](docs/support-matrix.md).
