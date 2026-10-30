# Security and operational boundary

KubeCompat has no apply, upgrade, sync, reconcile, exec, port-forward, watch, dry-run or remediation command. Kubernetes transport permits GET only and denies sensitive subresources. Optional source Git uses the read-only upload-pack protocol (HTTP POST); it is not a Kubernetes write.

## Cluster credentials and RBAC

Select an explicitly trusted `--context`/`--kubeconfig`, or use in-cluster credentials. Client-go can execute a kubeconfig's configured authentication helper; trusting that configuration is the operator's responsibility. Configuration contents and underlying API errors are never printed.

The unbound [base role](../deploy/rbac/base.yaml) enumerates named resources and GET/LIST only. It grants no Secret, ConfigMap, watch or mutation permissions. The scanner never installs/binds roles. Real least-privilege access must be provisioned separately; a scan with an existing privileged credential is not proof the role was tested.

Helm storage is separately opt-in: `--helm-storage --namespace NAME`, or `--helm-storage --helm-storage-namespace NAME` with a different workload namespace or `--all-namespaces`. Exactly one valid explicitly authorized storage namespace is resolved before API/configuration access; wildcard/multi-namespace/invalid scopes and override without opt-in or live input are rejected. Default live collection still performs zero Secret/ConfigMap payload requests. The [storage role](../deploy/rbac/helm-storage.yaml) exposes all Secret/ConfigMap bodies in its bound namespace, not only release metadata: a label selector is query optimization, not authorization. The transport and paginated release queries retain only the resolved storage namespace, never all-namespace payload endpoints. Release workload namespace is not rewritten to storage namespace. SQL storage remains a coverage gap.

## Confidentiality

Allowlisted records exclude raw documents, release bodies, values, env payloads, arbitrary annotations, certificates, keys and authorization headers. Raw objects exist transiently for local schema validation and must not be logged or persisted. Source SQLite stores sanitized public claim excerpts, not raw cluster input or fetched pages. Evidence snapshots do not upload inventory to third parties.

Safe reports still contain operational metadata: namespaces, resource names, repositories, images, revisions and selected status reasons. Keep them private and review before sharing. This is not a general-purpose redactor for a secret intentionally placed in a resource name/status field. Output is atomic mode 0600; evidence storage requires 0700 directory/0600 file. Private paths and canary-bearing live snapshots stay outside published artifacts.

## Untrusted sources

HTTPS is required; all redirects/DNS/dial addresses are validated, with explicit opt-in for private origins. Additional CA trust never disables verification. Credentials are mode-0600 per-origin configuration, in memory only; no credential helpers or cross-redirect Authorization/Cookie forwarding. Source clients enforce deadlines, retries and bounded bodies. See [transport details](cache.md).

Git accepts immutable commits only, with bounded object allocation, depth-one read-only upload-pack, no hooks/submodules/LFS/SSH/helper execution, and no source URL credentials/query. Remote Helm/Kustomize/OCI acquisition uses the same policy client; offline is checked before network access. OCI requires an exact stable chart version or lock, HTTPS registry access, explicit credentials when needed, descriptor size/digest verification and bounded extraction. Ambient registry authentication/challenge helpers are not invoked.

Renderers do not run external programs. Helm lookup/tpl and hooks are excluded; Kustomize exec/alpha plugin/generator paths are rejected. Archives reject traversal, links, special files, excessive sizes/counts. These controls do not certify arbitrary templates against all CPU/memory denial-of-service inputs; use OS-level resource limits for hostile repositories.

Schemas are pinned/local, strict, hash-checked and cannot follow external references. Static readiness does not call admission/conversion services. HTML uses contextual escaping and no network assets; CSV neutralizes spreadsheet formulas. Saved assessment reports are pure rendering, not trusted input for remediation.

## Verification and release boundary

Deterministic tests cover denied writes/default storage access, partial/forbidden reads, canary omission, unsafe source URLs, redirects, local TLS/private CA, digest tampering, offline denial, malicious archives/schema references, cache transactions/corruption and HTML/CSV injection. The actual live scan was default secret-free and read-only. It was not run under newly bound least-privilege roles, and no production audit configuration was changed. Four controller-backed management scenarios and optional live Helm payload access cannot be certified from a cluster without Flux and without separate payload authorization. Those release gates remain blocked rather than manufactured.
