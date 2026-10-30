# Source refresh receipt

Documented 2026-10-30 from the retained actual CLI refresh receipt; cached source retrieval timestamps are 2026-10-29. This is a point-in-time receipt, not a support guarantee.

`kubecompat db update --cache CACHE --output HEALTH_JSON` completed in 122.66 seconds with exit **3** (incomplete evidence). All fifteen project adapters parsed at least one source. A project-level `COMPLETE` record means some source succeeded; it does **not** mean every source passed.

Three source-level partial records remained:

| Project | Source | Observation |
| --- | --- | --- |
| cert-manager | Two SHA256-addressed OCI Helm config blobs at quay.io | `SOURCE_FETCH_FAILED` |
| ingress-nginx | Official GitHub releases endpoint | `EVIDENCE_MISSING` |

The cert-manager official support table yielded 56 claims. Official chart index, supporting Artifact Hub metadata, OCI tag inventory and two OCI manifests were parsed and cached. Chart metadata is not runtime-support evidence; Artifact Hub cannot establish authoritative support or official chart mapping.

The bounded ingress-nginx release page contained chart-prefixed releases, not applicable application release rows. No application versions were manufactured from chart tags, and missing bounded-history evidence is not classified as unsupported runtime.

A bounded header-only HTTP probe documented on 2026-10-30 of one failing OCI blob returned HTTP 302 to **cdn01.quay.io**, with a signed query. The shared fetch policy rejects both unapproved redirect hosts and query-bearing source URLs. No arbitrary CDN allowlist, credential forwarding, signature bypass, or integrity bypass was introduced. Signed redirect URLs are deliberately omitted here. This is a supported-catalog CDN-redirect policy limitation, not evidence that upstream artifacts are absent. These two config blobs remain an explicit source-policy gap; successful manifest discovery does not establish successful blob retrieval.

Successful project records: cert-manager, argocd, metrics-server, cilium, calico, flux, coredns, ingress-nginx, prometheus-operator, velero, keda, kyverno, istio, longhorn and rook-ceph. The sanitized health output contains only public source URLs, collector names and coverage codes. The evidence database is private working material, not a publication artifact.

## Remaining release gates

Four real Helm/Argo/Flux/mixed-controller scenarios, a least-privileged identity plus audit proof, separately authorized Helm release-storage observation, and independent security/source review are **blocked**, not passed. Existing live observation was read-only and is not a substitute for these gates. No kind cluster, installation, workload seeding, mutation or Secret access is authorized.
