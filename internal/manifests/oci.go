package manifests

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/Masterminds/semver/v3"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/wussh/kubecompact/internal/compatibility/fetch"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry/remote"
)

func fetchOCIChart(ctx context.Context, origin, name, version string, client *fetch.Client) ([]byte, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "oci" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("invalid OCI chart origin")
	}
	exact, err := semver.StrictNewVersion(strings.TrimPrefix(version, "v"))
	if err != nil || exact.Prerelease() != "" {
		return nil, errors.New("OCI dependency requires an exact stable version or Chart.lock")
	}
	repo, err := remote.NewRepository(u.Host + strings.TrimRight(u.Path, "/") + "/" + name)
	if err != nil {
		return nil, errors.New("invalid OCI repository")
	}
	// Never use ORAS's ambient/default authentication client.
	repo.Client = client.HTTPClient()
	repo.ManifestMediaTypes = []string{ocispec.MediaTypeImageManifest}
	repo.MaxMetadataBytes = 1 << 20
	descriptor, err := repo.Resolve(ctx, strings.ReplaceAll(version, "+", "_"))
	if err != nil {
		return nil, errors.New("OCI chart revision unavailable")
	}
	if descriptor.Size <= 0 || descriptor.Size > 1<<20 || descriptor.MediaType != ocispec.MediaTypeImageManifest {
		return nil, errors.New("OCI chart manifest unsafe")
	}
	raw, err := content.FetchAll(ctx, repo, descriptor)
	if err != nil {
		return nil, errors.New("OCI manifest integrity failed")
	}
	var manifest ocispec.Manifest
	if json.Unmarshal(raw, &manifest) != nil || manifest.SchemaVersion != 2 {
		return nil, errors.New("invalid OCI chart manifest")
	}
	var layer *ocispec.Descriptor
	for i := range manifest.Layers {
		d := &manifest.Layers[i]
		if d.MediaType != "application/vnd.cncf.helm.chart.content.v1.tar+gzip" {
			continue
		}
		if layer != nil || d.Size <= 0 || d.Size > 4<<20 {
			return nil, errors.New("OCI chart layer unsafe")
		}
		layer = d
	}
	if layer == nil {
		return nil, errors.New("OCI Helm chart layer missing")
	}
	body, err := content.FetchAll(ctx, repo, *layer)
	if err != nil {
		return nil, errors.New("OCI chart integrity failed")
	}
	return body, nil
}
