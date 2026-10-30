# Changelog

## Unreleased — 2026-10-30

- Add the read-only KubeCompat development CLI: live/file/pinned HTTPS Git inputs, safe Helm/Kustomize rendering, management ownership/version observations, pinned Kubernetes 1.35 API/schema/static checks, fifteen compile-time source adapters, immutable SQLite evidence and portable reports.
- Require independent authoritative support and exact release proof for upgrade candidates; chart-managed candidates also require an official matching chart/repository mapping. Unknown environment constraints remain conjunctive; release claims must match their own version range. Bounded release history does not establish latest-patch completeness.
- Preserve input digests and renderer/source provenance even when inputs contain no workload component. Reopen untrusted render/dependency inputs through bounded no-escape reads.
- Fix immutable shallow Git acquisition and permit only its exact read-only info/refs protocol query. Keep strict source URL, redirect, TLS, credential and offline boundaries.
- Retain malformed-YAML diagnostics, supplied deprecation-usage intervals and missing schema-validation inputs as distinct failures/gaps. Do not label requested images as observed running versions.
- Export exact candidate/evidence references across JSON, CSV, table and HTML. Add actual CLI, offline authenticated transport, browser-link and source-health receipts.
- Add non-root artifact packaging, notices and candidate-only CI workflows. No release or publication is performed. Controller-backed acceptance, least-privilege audit and independent reviews remain blocked; see [support matrix](docs/support-matrix.md).
