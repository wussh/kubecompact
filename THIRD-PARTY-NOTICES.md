# Third-party notices

Original KubeCompat source is licensed under [MIT](LICENSE). Dependencies retain their own licenses; the Go module graph is pinned in `go.mod` and `go.sum`.

Redistributed dependency license texts and copyright/NOTICE files are retained under [third_party/licenses](third_party/licenses/), generated for the executable with Google go-licenses v2.0.1 after OCI imports. Include this directory and this notice when distributing the binary or container. Go 1.27.3 standard-library [LICENSE](third_party/licenses/go/LICENSE) and [PATENTS](third_party/licenses/go/PATENTS) are redistributed too. Automated collection is not an independent legal review.

## Embedded Kubernetes data

- Kubernetes API lifecycle metadata: Kubernetes API v0.35.0, revision `9afe7de0a56c582b06ca094f7309015bc84657a7`. Apache-2.0. Each entry in `data/api-lifecycle.json` retains its source URL.
- Kubernetes JSON schemas: Gareth Rushgrove and Yanh Hamon, Apache-2.0; Kubernetes 1.35.0 strict standalone schemas at `yannh/kubernetes-json-schema` revision `7464e8eb55dab7063a4e6cb6cd6e2ed19f399580`. [Copyright/license notice](data/schemas/LICENSE), [license text](data/LICENSE-APACHE-2.0), and [hash provenance](data/schemas/provenance.json) are included.
- Official-source regression excerpts are synthetic/minimal attributed parser examples, described by `testdata/sources/official-regression.json`. They do not grant a license to redistribute arbitrary upstream documentation.

The scanner's evidence cache is local operator output, not a shipped compatibility database. No Plural source or dataset is included. Official documentation/release pages remain subject to their upstream terms; retain source attribution when sharing extracted claim receipts.
