# Pinned dependency contracts

The executable is `kubecompat`; module/repository is `github.com/wussh/kubecompact`. Original code is MIT. `go.mod`/`go.sum` lock the complete graph; Go build metadata records the actual linked modules.

| Dependency | Pin | Exercised contract |
| --- | --- | --- |
| Go | 1.27.3 toolchain | Patched after govulncheck identified GO-2026-6809 in 1.27.2; no claim that older artifacts are release-safe. |
| client-go / apimachinery / api | v0.37.2 | Dynamic served-version GET/LIST, pagination/cancellation and guarded transport; actual read-only K3s 1.33 server scan. |
| Helm | v4.3.1 | Helm 3/4 release SDK JSON decoding; chart loader/accessor, target capabilities, values/schema render and local chart smoke. |
| Kustomize api / kyaml | v0.21.1 / v0.21.2 | In-memory plugin-disabled overlay rendering; actual name/namespace transform. |
| kube-openapi | v0.0.0-20260721132016-d427ff9ee9ad | Compatible structured-merge-diff v6 family required by client-go/Helm. Kustomize api/kyaml 0.21.3 selected an incompatible v7 OpenAPI graph and failed compilation; they are not the shipped pins. |
| Kubeconform | v0.8.0 | Explicit local schema paths, strict `validator.New`, `ValidateWithContext`; invalid/missing/custom/external-ref distinctions. |
| Masterminds SemVer | v3.5.0 | Exact stable release and bounded component/Kubernetes constraints; chart/app/git roles remain separate. |
| go-git | v5.19.3 | Explicit immutable upload-pack request, memory storage with decompressed allocation bounds; no shell Git or automatic auth helpers. |
| ORAS Go | v2.6.2 | Repository Resolve/Fetch through explicit policy client; verified descriptor size/digest; no default auth client. |
| SQLite | modernc.org/sqlite v1.61.0 | Pure-Go private filesystem database, transactions/migrations/integrity and relative-path regression. |
| YAML / HTML | go.yaml.in/yaml/v3 v3.0.5; x/net v0.62.0 | Duplicate/alias/depth-safe manifest mapping; deterministic official HTML table parsing. |

`data/` retains attributed Kubernetes API lifecycle facts, the strict Kubernetes 1.35.0 schema bundle, source revisions/hashes and licenses. No Plural implementation/data is embedded. Tests use synthetic source excerpts and canaries, not production release bodies.

`third_party/licenses/` contains generated redistribution license/notice material from `go-licenses v2.0.1 save ./cmd/kubecompat`. Its warnings about assembly dependencies mean the tool cannot discover dependencies hidden in non-Go code; they are not an independent legal audit. Re-run after dependency changes and include the notices with distribution artifacts.

The support matrix separates compilation, fixture behavior, actual controller observations, OS runtime tests and release gates. A client library version is never advertised as server/controller compatibility proof.
