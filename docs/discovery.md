# Discovery

`discovery.Collect(ctx, ScanOptions)` collects only named Kubernetes kinds using client-go v0.37.2. `FromObjects(objects, mode, scope)` supplies the same projection to manifest callers; `Correlate` joins observations without using project identity as an installation key. Input scope contributes to the file-only cluster identity. Live identity is an opaque hash of the API endpoint, not the endpoint itself.

## Access and bounds

Base collection does not read Secrets or ConfigMaps. `HelmStorageNamespace` / `--helm-storage-namespace` selects one explicit authorized storage namespace independently of workload scope; absent the override, storage falls back to `--namespace`. The shared resolver rejects missing/invalid/broad scopes, override without opt-in and nonlive input before configuration/API access. With explicit storage scope, `--all-namespaces` may discover workloads while the existing GET-only transport and Helm Secret/ConfigMap queries stay scoped to that one namespace. Coverage records the actual storage namespace; release destination namespace and ownership stay unchanged. The transport rejects watch/sensitive subresources. QPS 5, burst 10, request timeout 30 seconds, default scan timeout five minutes, page size 200/max pages 10,000 remain unchanged. Cancellation propagates; forbidden access is UNREADABLE, failure after a page is PARTIAL retaining prior objects, absent endpoints are NOT_PRESENT. Collection remains serial.

`deploy/rbac/base.yaml` is an unbound ClusterRole. Namespace-limited requests do not prove visibility into other namespaces. The separate `helm-storage.yaml` Role must be bound only in the authorized storage namespace: its permissions expose **all Secret and ConfigMap bodies in that namespace** to the credential. A label selector is not authorization. Neither role grants watch or write access. The scanner does not install roles or bindings.

## Export boundary

Resource records retain identity, controller UID references, a fixed management label/annotation allowlist, container image references and readiness facts. Arbitrary annotations, env, values, Secret/ConfigMap bodies, credentials, certificate bundles and raw upstream errors are excluded. URLs lose userinfo, query and fragment. Transient `Inventory.Objects` are not JSON-serialized and must never be logged or cached. Schema validation needs those transient originals; loading a saved inventory cannot recover the authored document.

Facts are nested Kubernetes-shaped objects: `metadata.generation`; workload replica counts and observed generation/condition type/status/reason; Service selector/ports/type/clusterIP; EndpointSlice target references, ports and readiness conditions; PDB limits and disruptionsAllowed; CRD group/names/version served/storage flags and conversion strategy/service/review versions; APIService group/version/service/conditions; webhook names/service/rules/review versions/failure policy/side effects/timeouts. No webhook CA or URL payload is retained. Full CRD schemas remain transient rather than exported.

Requested images come from Pod/workload specs. `running` images and immutable IDs come only from Pod status. Helm manifest images are `rendered`, never `running`; chart appVersion never supplies an observed running version.

## Controller state

Argo sources preserve index/order, values-only refs, target and resolved revisions. Deployment observations require a successful operation result, rather than merely a target revision or failed latest sync. Remote destinations remain declarations: collection never connects to them. Flux HelmRelease desired/attempted revisions are separate from successful deployed/superseded history; stale observed generation and suspension are explicit gaps. Kustomization lastAppliedRevision is a historical successful observation, not proof of current health. Source artifact revisions remain resolved, not deployed workload versions.

Helm 3/4 release JSON is decoded into the pinned Helm 4.3.1 release SDK record after bounded base64/gzip decoding. Only chart metadata, release state, version provenance and rendered images leave the decoder. Repository is unknown unless a controller supplies it; chart home/source URLs do not establish the chart repository. Successful older history survives failed newer revisions. SQL release storage extraction is unsupported and reported separately.

Resources join through cluster/UID owner references. Helm **storage identity** (cluster, storage namespace, actual release name) joins history and Flux release records. Workload ownership is separate: cluster + observed `chart.releaseNamespace` + actual release name, resolved only to a unique canonical installation after storage aliases. `releaseNamespace` comes only from Helm `release.Namespace` or Flux's documented targetNamespace/default, never storage namespace or Argo chart-render declarations. Multiple storage installations sharing the ownership tuple remain separate; affected workload ownership is PARTIAL/AMBIGUOUS_MANAGEMENT, not malformed input or an arbitrary merge. Argo tracking still requires exact target annotations/matching installation IDs; colliding labels/recreated UIDs/different clusters never establish identity.

## Validation boundary

Synthetic regression tests cover projection confidentiality, ordered Argo sources, failed Flux/Helm attempts, UID ownership, cross-cluster collisions, transport-denied mutations/default Secret access, and forbidden pagination after a successful page. They are authored for integration verification, not reported as executed here. The parent integration run may exercise explicitly selected `tbs-dev` with read-only credentials only. Installed-management coverage must be distinguished from unexercised controller scenarios. Do not install controllers, fixtures or RBAC, or mutate workloads on that cluster to manufacture acceptance evidence.

## Sources

- [Argo multiple sources](https://argo-cd.readthedocs.io/en/stable/user-guide/multiple_sources/)
- [Argo resource tracking](https://argo-cd.readthedocs.io/en/stable/user-guide/resource_tracking/)
- [Argo Helm rendering](https://argo-cd.readthedocs.io/en/stable/user-guide/helm/)
- [Flux HelmRelease status](https://fluxcd.io/flux/components/helm/helmreleases/)
- [Helm storage decoder source](https://github.com/helm/helm/blob/v4.3.1/pkg/storage/driver/util.go)
- [Helm release record](https://github.com/helm/helm/blob/v4.3.1/pkg/release/v1/release.go)
- [Pinned Helm install metadata visitor call](https://github.com/helm/helm/blob/v4.3.1/pkg/action/install.go#L405-L411): passes `rel.Name`, `rel.Namespace`.
- [Pinned Helm ownership annotations](https://github.com/helm/helm/blob/v4.3.1/pkg/action/validate.go#L209-L235): writes `meta.helm.sh/release-namespace` from that release namespace.
