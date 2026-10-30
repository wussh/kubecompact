# KubeCompat

Read-only, evidence-backed Kubernetes upgrade assessment. Repository/module: `github.com/wussh/kubecompact`; executable: `kubecompat`.

**Development implementation, not a certified release.** The scanner runs against files and explicitly selected clusters. Release acceptance is still blocked on unexercised controller-backed/least-privilege and platform gates; see the [support matrix](docs/support-matrix.md). No release, cluster upgrade or automated remediation is provided.

## Build and try without a cluster

```sh
go build -trimpath -o bin/kubecompat ./cmd/kubecompat
bin/kubecompat version
KUBECONFIG=/nonexistent bin/kubecompat scan --manifests testdata/manifests/basic.yaml --output inventory.json
bin/kubecompat check --inventory inventory.json --target-k8s 1.35 --offline --format json --output assessment.json
bin/kubecompat report --input assessment.json --format html --output report.html
```

`check` intentionally returns **3** for this fixture: desired images are not observed running application versions, and missing support/usage/schema evidence is not a clean pass. Run the report command separately after handling that assessment exit code. The HTML opens locally without assets or source networking.

## Live and online modes are explicit

```sh
bin/kubecompat scan --context TRUSTED_CONTEXT --all-namespaces --output inventory.json
bin/kubecompat db update --online --cache evidence/evidence.db --timeout 15m --output source-health.json
bin/kubecompat check --inventory inventory.json --target-k8s 1.35 --offline --cache evidence/evidence.db --format json --output assessment.json
```

Base scans never request Secret/ConfigMap payloads. Optional Helm release bodies require separate authorization, namespace scope and `--helm-storage`; see [security](docs/security.md). Failed or absent source/controller access remains visible. Fifteen compile-time adapters retrieve official documentation/release metadata; not every upstream publishes an authoritative runtime matrix.

## What results mean

| Result | Meaning |
| --- | --- |
| COMPATIBLE | Fresh applicable official runtime support, not runtime certification. |
| INCOMPATIBLE | Authoritative exclusion or unmet requirement. |
| UPGRADE_REQUIRED | Exclusion plus an exact released, supported replacement with proven prerequisites/mapping. |
| NOT_TESTED | Outside an official tested set, without proof of incompatibility. |
| UNKNOWN | Identity/version/evidence/freshness/conflict is insufficient. |

API/schema/static readiness findings are independent. The pinned target catalog currently covers **Kubernetes 1.35 / schema 1.35.0 only**. A valid schema, chart constraint or published release never creates runtime support. Final-target support does not prove a direct/multi-hop upgrade path.

Inventory separates desired, attempted, resolved, successfully deployed and observed running versions. Helm chart/app versions are not interchangeable. Argo multi-source/values-only and Flux source/status observations retain provenance; ownership joins use resource/installation identities rather than labels alone.

## Documentation

- [Commands, outputs, private sources and exit codes](docs/usage.md)
- [Discovery, Helm, Argo CD and Flux semantics](docs/discovery.md)
- [API/schema/static readiness and target data](docs/readiness.md)
- [Official sources and compatibility claim boundaries](docs/compatibility-sources.md)
- [Cache, TLS, origin policy and offline behavior](docs/cache.md)
- [Security and authorization](docs/security.md)
- [Dependency contracts](docs/dependency-contracts.md)
- [Exercised support and blocked release gates](docs/support-matrix.md)
- [Actual CLI/browser verification receipts](docs/verification.md)
- [Artifact checksums, SBOMs and vulnerability scope](docs/artifacts.md)
- [Per-source refresh health](docs/source-health.md)
- [Full documentation index](docs/README.md)
- [Third-party notices](THIRD-PARTY-NOTICES.md)

Original code: [MIT](LICENSE). Embedded schemas and dependencies retain their own notices.
