# Changelog

## Unreleased — 2026-10-30

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
