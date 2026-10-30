package discovery

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/wussh/kubecompact/internal/model"
	release "helm.sh/helm/v4/pkg/release/v1"
	apivalidation "k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// StorageNamespace resolves the single explicitly authorized live storage scope.
func StorageNamespace(opts model.ScanOptions) (string, error) {
	if !opts.HelmStorage {
		if opts.HelmStorageNamespace != "" {
			return "", errors.New("HELM_STORAGE_REQUIRES_OPT_IN")
		}
		return "", nil
	}
	if opts.Manifests != "" || opts.GitURL != "" {
		return "", errors.New("HELM_STORAGE_REQUIRES_LIVE_INPUT")
	}
	ns := opts.HelmStorageNamespace
	if ns == "" && !opts.AllNamespaces {
		ns = opts.Namespace
	}
	if len(apivalidation.ValidateNamespaceName(ns, false)) != 0 {
		return "", errors.New("HELM_STORAGE_REQUIRES_VALID_NAMESPACE")
	}
	return ns, nil
}

// Helm's storage decoder is private; mirror its wire format with a hard expansion bound.
func decodeRelease(s string) (*release.Release, error) {
	if len(s) > 24<<20 {
		return nil, errors.New("RELEASE_TOO_LARGE")
	}
	b, e := base64.StdEncoding.DecodeString(s)
	if e != nil {
		return nil, errors.New("RELEASE_DECODE_FAILED")
	}
	if len(b) > 3 && bytes.Equal(b[:3], []byte{31, 139, 8}) {
		r, e := gzip.NewReader(bytes.NewReader(b))
		if e != nil {
			return nil, errors.New("RELEASE_DECODE_FAILED")
		}
		defer r.Close()
		b, e = io.ReadAll(io.LimitReader(r, (16<<20)+1))
		if e != nil || len(b) > 16<<20 {
			return nil, errors.New("RELEASE_TOO_LARGE")
		}
	}
	var rel release.Release
	if json.Unmarshal(b, &rel) != nil || rel.Name == "" || rel.Chart == nil || rel.Chart.Metadata == nil || rel.Info == nil {
		return nil, errors.New("RELEASE_METADATA_INVALID")
	}
	return &rel, nil
}
func (r *reader) helm(ctx context.Context, inv *model.Inventory, ns string) {
	for _, resource := range []string{"secrets", "configmaps"} {
		cov := model.Coverage{Collector: "helm-storage", Scope: ns, Kinds: []string{resource}, State: "COMPLETE"}
		token := ""
		seen := map[string]bool{}
		for range 10000 {
			list, e := r.dynamic.Resource(schema.GroupVersionResource{Version: "v1", Resource: resource}).Namespace(ns).List(ctx, metav1.ListOptions{LabelSelector: "owner=helm", Limit: 200, Continue: token})
			if e != nil {
				cov.State = "UNREADABLE"
				if token != "" {
					cov.State = "PARTIAL"
				}
				cov.Code = "HELM_STORAGE_READ_FAILED"
				break
			}
			for _, o := range list.Items {
				s := text(o.Object, "data", "release")
				if resource == "secrets" {
					b, e := base64.StdEncoding.DecodeString(s)
					if e != nil {
						cov.State = "PARTIAL"
						cov.Code = "RELEASE_DECODE_FAILED"
						continue
					}
					s = string(b)
				}
				rel, e := decodeRelease(s)
				if e != nil {
					cov.State = "PARTIAL"
					cov.Code = "RELEASE_DECODE_FAILED"
					continue
				}
				addRelease(inv, rel, ns)
			}
			token = list.GetContinue()
			if token == "" {
				break
			}
			if seen[token] {
				cov.State = "PARTIAL"
				cov.Code = "CONTINUE_REPEATED"
				break
			}
			seen[token] = true
		}
		inv.Coverage = append(inv.Coverage, cov)
	}
	inv.Coverage = append(inv.Coverage, model.Coverage{Collector: "helm-sql-storage", Scope: ns, State: "NOT_REQUESTED", Code: "SQL_STORAGE_UNSUPPORTED"})
}
func addRelease(inv *model.Inventory, r *release.Release, storage string) {
	cluster := inv.Clusters[0].ID
	id := model.ID(cluster, "helm", storage, r.Name)
	m := r.Chart.Metadata
	c := model.Component{ID: id, Name: r.Name, Namespace: r.Namespace, ClusterID: cluster, ManagedBy: []string{"helm"}, Chart: &model.ChartObservation{Name: m.Name, Version: m.Version, AppVersion: m.AppVersion, KubeVersion: m.KubeVersion, ReleaseName: r.Name, StorageNamespace: storage, Status: string(r.Info.Status)}, Status: string(r.Info.Status), ObservedAt: inv.StartedAt}
	c.Chart.ReleaseNamespace = r.Namespace
	success := c.Status == "deployed" || c.Status == "superseded"
	role := "resolved"
	if success {
		role = "deployed"
	}
	field := "helm.history[" + strconv.Itoa(r.Version) + "]"
	version(&c, role, "chart", m.Version, field, 0, success, inv.StartedAt)
	version(&c, role, "app", m.AppVersion, field, 0, success, inv.StartedAt)
	c.Gaps = append(c.Gaps, "HELM_REPOSITORY_UNKNOWN")
	dec := yaml.NewYAMLOrJSONDecoder(strings.NewReader(r.Manifest), 4096)
	for range 100000 {
		var o map[string]any
		e := dec.Decode(&o)
		if e == io.EOF {
			break
		}
		if e != nil {
			c.Gaps = append(c.Gaps, "HELM_MANIFEST_INVALID")
			break
		}
		if o == nil {
			continue
		}
		projected := FromObjects([]map[string]any{o}, "helm-rendered", id)
		for _, res := range projected.Resources {
			for _, im := range res.Images {
				im.Role = "rendered"
				im.Resource.ClusterID = cluster
				c.Images = append(c.Images, im)
			}
		}
	}
	identify(&c)
	inv.Components = append(inv.Components, c)
}
