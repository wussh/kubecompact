package discovery

import (
	"context"
	"github.com/wussh/kubecompact/internal/model"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// Negotiate only the finite supported controller resources; never enumerate CR instances.
func (r *reader) servedList(ctx context.Context, k resourceKind, namespace string) ([]map[string]any, model.Coverage) {
	if k.Group != "argoproj.io" && k.Group != "helm.toolkit.fluxcd.io" && k.Group != "kustomize.toolkit.fluxcd.io" && k.Group != "source.toolkit.fluxcd.io" {
		return r.list(ctx, k, namespace)
	}
	versions := []string{k.Version}
	switch k.Group {
	case "helm.toolkit.fluxcd.io":
		versions = append(versions, "v2beta2", "v2beta1")
	case "kustomize.toolkit.fluxcd.io":
		versions = append(versions, "v1beta2", "v1beta1")
	case "source.toolkit.fluxcd.io":
		versions = append(versions, "v1beta2", "v1beta1")
	}
	for _, v := range versions {
		if ctx.Err() != nil {
			return nil, model.Coverage{Collector: "kubernetes", Scope: namespace, Kinds: []string{k.Kind}, State: "UNREADABLE", Code: "CANCELLED"}
		}
		resources, e := r.discovery.ServerResourcesForGroupVersion(k.Group + "/" + v)
		if e != nil {
			if apierrors.IsNotFound(e) {
				continue
			}
			return nil, model.Coverage{Collector: "api-discovery", Scope: namespace, Kinds: []string{k.Kind}, State: "UNREADABLE", Code: "API_DISCOVERY_FAILED"}
		}
		for _, resource := range resources.APIResources {
			if resource.Name == k.Resource {
				k.Version = v
				return r.list(ctx, k, namespace)
			}
		}
	}
	return nil, model.Coverage{Collector: "api-discovery", Scope: namespace, Kinds: []string{k.Kind}, State: "NOT_PRESENT", Code: "NOT_SERVED"}
}
