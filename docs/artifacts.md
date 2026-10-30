# Historical PRECOMMIT artifact receipts

Documented 2026-10-30 **before the implementation commit**. These dated PRECOMMIT receipts describe local development candidates, not published releases or the current working-tree state. Exact historical bytes, build metadata and checksums remain verifiable in the private `.work/precommit-artifacts/native.tar` archive. Postcommit `dist/` has its own exact source revision and `SHA256SUMS`, recorded separately in private `.work/postcommit-artifact-receipt.json`; historical hashes below must not be attributed to those newer bytes. Only native **Linux amd64** execution was exercised; Linux arm64, macOS and Windows workflow jobs had not run.

## Native binary and archive

The actual static artifact was built with Go 1.27.3:

```sh
CGO_ENABLED=0 go build -trimpath -buildvcs=true -o dist/kubecompat-linux-amd64 ./cmd/kubecompat
```

Actual `version`, raw scan, Helm values render and Kustomize render exited 0. Offline check of saved synthetic inventory exited 3; pure HTML report exited 0 while preserving its embedded incomplete assessment. All used fixture inputs with a nonexistent kubeconfig, not a cluster.

The preserved PRECOMMIT candidate directory contained:

- `kubecompat-linux-amd64` and `kubecompat-linux-amd64.tar.gz`;
- `build-info.txt` (actual Go module/toolchain build metadata) and `build-metadata.json`;
- genuine Syft 1.55.0 `kubecompat.spdx.json` (**SPDX 2.3, 96 packages**) and `kubecompat.cdx.json` (**CycloneDX 1.7, 96 components**);
- `SHA256SUMS`, generated after SBOM/build metadata and verified with `sha256sum -c SHA256SUMS` (all six entries OK, including both metadata files).

| Candidate | SHA256 |
| --- | --- |
| Linux amd64 binary | `5f85e8939cebf71365b9bb7b74736306c9a849016f4fe0c4e3c82fc70074e852` |
| Binary/notices/SBOM archive | `8967d5e8d830e2f5d8f53dd157bac136e6b94450223b2435fb3b965685a543a2` |
| SPDX JSON | `c89af8e2be2a00a7e13cf4a9e2eac585bf1b0b9ba82ac996069971088f4b2767` |
| CycloneDX JSON | `d054740359508570d949f25534596bd2ad010ae5b74959fb67dd81c2f8806625` |

At this PRECOMMIT observation, build metadata recorded baseline `a561f6eaafa617a6504847441722a170c9426bdc` and **sourceDirty=true**. That revision did not identify the uncommitted implementation bytes. Postcommit artifacts require clean exact-revision metadata and separate receipts; updating those generated outputs does not rewrite these tracked historical hashes.

The archive includes the original MIT license, third-party notices, collected dependency licenses, Go 1.27.3 LICENSE/PATENTS, schema attribution/provenance and API-lifecycle Apache-2.0 license. Automated go-licenses v2.0.1 collection was refreshed after OCI imports; it is not independent legal approval.

## Container

Actual final build command:

```sh
docker build --network host --build-arg SOURCE_REVISION=a561f6eaafa617a6504847441722a170c9426bdc --build-arg SOURCE_DIRTY=true --tag kubecompat:verification .
```

The default build first failed because this workstation's Docker bridge DNS could not resolve the Go proxy; the supervised host-network build succeeded. This build-only network setting does not change runtime permissions or scanner policy.

Observed local image index ID: `sha256:58dfe5f1c5dbc51fa95d4545c160aecebe27999f3a135725243e8fea22e006af`. Actual image config digest: `sha256:1f2c58b8fbc922c38036f681277f87c442dced93059a669dc1aa105160ba903b`. Architecture amd64, configured user **65532:65532**. Runtime base: distroless static Debian 12 nonroot. BuildKit target OS/architecture arguments are used, not hard-coded arm64-mislabelling defaults.

The final image was exercised with `docker run --rm --network none` and read-only public/synthetic fixture mounts:

| Command path | Exit |
| --- | --- |
| version, raw scan, Helm values render, Kustomize render | 0 each |
| saved synthetic inventory offline check | 3 |
| saved assessment HTML report | 0 |
| removed policy/v1beta1 PDB check | 2 |

No external Helm, Argo, Flux or Git executable is needed inside the runtime image. Notices, schema attribution and actual Go/source build metadata are included under `/usr/share/kubecompat/`.

## Vulnerability tools and scope

Pinned analyzer archives were checksum-verified before extracting only their binaries into private workspace tooling:

- Syft 1.55.0: `bb65249542db5fedeeb4d3f9b018b75e7264dbfbbb10486f044581a92f90490d`.
- Grype 0.121.0: `64e07133d3486115de647a66581461dccf5c732748a0a3119077395565f15519`.

Actual **final-image** Grype scan exited 0 with **zero matches**, Debian 12.15, valid database schema v6.1.10 built `2026-10-29T06:23:34Z`. Its source config digest matches the final image above. An earlier scan of the stale Go 1.27.2 image found GO-2026-6809; it is not substituted for this final-image receipt.

Final source govulncheck 1.9.0 on Go 1.27.3 scanned 12 root packages / 94 modules: **zero reachable symbol vulnerabilities and zero imported-package vulnerabilities**. Required-module-only GO-2026-5932 concerns unmaintained `golang.org/x/crypto/openpgp`, has no fixed version, and that package is not imported by the scanner. This is not an all-modules-clean claim. Tool results are bounded by their recorded databases, not independent security approval.

## Candidate workflows

CI now fails fast for ordinary raw/Helm/Kustomize commands and captures only explicitly expected nonzero check exits. Its image path exercises scan/check/report offline. Manual candidate packaging defines four future native jobs, includes both SBOM formats and notices, and computes checksums after SBOM generation. Workflows were locally reviewed/exercised through equivalent commands, **not observed passing on GitHub-hosted runners**. Permissions remain contents:read; no release, tag or image push is performed.

The controller-backed, least-privilege/audit, authorized live Helm-storage and independent-review gates remain blocked; see [support matrix](support-matrix.md) and [verification](verification.md).
