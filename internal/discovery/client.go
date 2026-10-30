package discovery

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/wussh/kubecompact/internal/model"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type readOnlyTransport struct {
	next      http.RoundTripper
	helm      bool
	namespace string
}

func (t readOnlyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method != "GET" {
		return nil, errors.New("KUBERNETES_METHOD_DENIED")
	}
	p := r.URL.Path
	for _, s := range []string{"exec", "attach", "portforward", "log", "proxy", "token"} {
		if strings.HasSuffix(p, "/"+s) || strings.Contains(p, "/"+s+"/") {
			return nil, errors.New("KUBERNETES_SUBRESOURCE_DENIED")
		}
	}
	for _, s := range []string{"secrets", "configmaps"} {
		if strings.Contains(p, "/"+s) {
			if !t.helm || t.namespace == "" || !strings.Contains(p, "/namespaces/"+t.namespace+"/"+s) {
				return nil, errors.New("HELM_STORAGE_NOT_AUTHORIZED")
			}
		}
	}
	if r.URL.Query().Get("watch") == "true" {
		return nil, errors.New("WATCH_DENIED")
	}
	return t.next.RoundTrip(r)
}

type resourceKind struct {
	Group, Version, Resource, Kind string
	Namespaced                     bool
}

var kinds = []resourceKind{
	{"", "v1", "nodes", "Node", false}, {"", "v1", "namespaces", "Namespace", false}, {"", "v1", "pods", "Pod", true}, {"", "v1", "services", "Service", true},
	{"apps", "v1", "deployments", "Deployment", true}, {"apps", "v1", "daemonsets", "DaemonSet", true}, {"apps", "v1", "statefulsets", "StatefulSet", true}, {"apps", "v1", "replicasets", "ReplicaSet", true}, {"batch", "v1", "jobs", "Job", true}, {"batch", "v1", "cronjobs", "CronJob", true}, {"discovery.k8s.io", "v1", "endpointslices", "EndpointSlice", true}, {"policy", "v1", "poddisruptionbudgets", "PodDisruptionBudget", true},
	{"apiextensions.k8s.io", "v1", "customresourcedefinitions", "CustomResourceDefinition", false}, {"apiregistration.k8s.io", "v1", "apiservices", "APIService", false}, {"admissionregistration.k8s.io", "v1", "mutatingwebhookconfigurations", "MutatingWebhookConfiguration", false}, {"admissionregistration.k8s.io", "v1", "validatingwebhookconfigurations", "ValidatingWebhookConfiguration", false},
	{"argoproj.io", "v1alpha1", "applications", "Application", true}, {"argoproj.io", "v1alpha1", "applicationsets", "ApplicationSet", true}, {"helm.toolkit.fluxcd.io", "v2", "helmreleases", "HelmRelease", true}, {"kustomize.toolkit.fluxcd.io", "v1", "kustomizations", "Kustomization", true}, {"source.toolkit.fluxcd.io", "v1", "gitrepositories", "GitRepository", true}, {"source.toolkit.fluxcd.io", "v1", "helmrepositories", "HelmRepository", true}, {"source.toolkit.fluxcd.io", "v1", "helmcharts", "HelmChart", true}, {"source.toolkit.fluxcd.io", "v1", "ocirepositories", "OCIRepository", true},
}

type reader struct {
	dynamic   dynamic.Interface
	discovery *discovery.DiscoveryClient
}

func newReader(config *rest.Config, helm bool, namespace string) (*reader, error) {
	c := rest.CopyConfig(config)
	c.QPS = 5
	c.Burst = 10
	c.Timeout = 30 * time.Second
	prior := c.WrapTransport
	c.WrapTransport = func(rt http.RoundTripper) http.RoundTripper {
		if prior != nil {
			rt = prior(rt)
		}
		return readOnlyTransport{next: rt, helm: helm, namespace: namespace}
	}
	d, e := dynamic.NewForConfig(c)
	if e != nil {
		return nil, errors.New("CLIENT_CONFIGURATION_FAILED")
	}
	disc, e := discovery.NewDiscoveryClientForConfig(c)
	if e != nil {
		return nil, errors.New("CLIENT_CONFIGURATION_FAILED")
	}
	return &reader{dynamic: d, discovery: disc}, nil
}
func (r *reader) list(ctx context.Context, k resourceKind, namespace string) ([]map[string]any, model.Coverage) {
	cov := model.Coverage{Collector: "kubernetes", Scope: namespace, Kinds: []string{k.Kind}, State: "COMPLETE"}
	gvr := schema.GroupVersionResource{Group: k.Group, Version: k.Version, Resource: k.Resource}
	var out []map[string]any
	token := ""
	seen := map[string]bool{}
	for range 10000 {
		seconds := int64(30)
		opts := metav1.ListOptions{Limit: 200, Continue: token, TimeoutSeconds: &seconds}
		client := r.dynamic.Resource(gvr)
		var listErr error
		var objects []map[string]any
		var next string
		if k.Namespaced {
			l, e := client.Namespace(namespace).List(ctx, opts)
			listErr = e
			if e == nil {
				next = l.GetContinue()
				for _, i := range l.Items {
					objects = append(objects, i.Object)
				}
			}
		} else {
			l, e := client.List(ctx, opts)
			listErr = e
			if e == nil {
				next = l.GetContinue()
				for _, i := range l.Items {
					objects = append(objects, i.Object)
				}
			}
		}
		if listErr != nil {
			cov.State = "UNREADABLE"
			cov.Code = "READ_FAILED"
			switch {
			case apierrors.IsForbidden(listErr):
				cov.Code = "FORBIDDEN"
			case apierrors.IsNotFound(listErr):
				cov.State = "NOT_PRESENT"
				cov.Code = "NOT_SERVED"
				if e, ok := listErr.(*apierrors.StatusError); ok && e.ErrStatus.Details != nil && e.ErrStatus.Details.Kind == "namespaces" {
					cov.State = "UNREADABLE"
					cov.Code = "NAMESPACE_UNAVAILABLE"
				}
			case apierrors.IsResourceExpired(listErr):
				cov.Code = "CONTINUE_EXPIRED"
			case ctx.Err() != nil:
				cov.Code = "CANCELLED"
			}
			if len(out) > 0 {
				cov.State = "PARTIAL"
			}
			return out, cov
		}
		out = append(out, objects...)
		if next == "" {
			return out, cov
		}
		if seen[next] {
			cov.State = "PARTIAL"
			cov.Code = "CONTINUE_REPEATED"
			return out, cov
		}
		seen[next] = true
		token = next
	}
	cov.State = "PARTIAL"
	cov.Code = "PAGE_LIMIT"
	return out, cov
}
func Collect(ctx context.Context, opts model.ScanOptions) (model.Inventory, error) {
	if opts.HelmStorage && (opts.Namespace == "" || opts.AllNamespaces) {
		return model.Inventory{}, errors.New("HELM_STORAGE_REQUIRES_EXCLUSIVE_NAMESPACE")
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var config *rest.Config
	var err error
	if opts.Kubeconfig != "" || opts.Context != "" {
		rules := clientcmd.NewDefaultClientConfigLoadingRules()
		rules.ExplicitPath = opts.Kubeconfig
		overrides := &clientcmd.ConfigOverrides{CurrentContext: opts.Context}
		config, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
	} else {
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		return model.Inventory{}, errors.New("KUBERNETES_CONFIGURATION_UNAVAILABLE")
	}
	r, err := newReader(config, opts.HelmStorage, opts.Namespace)
	if err != nil {
		return model.Inventory{}, err
	}
	namespace := opts.Namespace
	if opts.AllNamespaces {
		namespace = ""
	} else if namespace == "" {
		namespace = "default"
	}
	var objects []map[string]any
	var coverage []model.Coverage
	for _, k := range kinds {
		scope := namespace
		if !k.Namespaced {
			scope = ""
		}
		items, cov := r.servedList(ctx, k, scope)
		objects = append(objects, items...)
		coverage = append(coverage, cov)
	}
	cluster := model.ID("cluster", config.Host)
	inv := FromObjects(objects, "live", cluster)
	inv.Clusters[0].ID = cluster
	for i := range inv.Resources {
		inv.Resources[i].Ref.ClusterID = cluster
		for j := range inv.Resources[i].Images {
			inv.Resources[i].Images[j].Resource.ClusterID = cluster
		}
	}
	for i := range inv.Components {
		c := &inv.Components[i]
		c.ClusterID = cluster
		if c.Controller != nil {
			c.Controller.ClusterID = cluster
			c.ID = c.Controller.Key()
		}
		for j := range c.Resources {
			c.Resources[j].ClusterID = cluster
		}
		for j := range c.Images {
			c.Images[j].Resource.ClusterID = cluster
		}
		for j := range c.Versions {
			c.Versions[j].SourceID = c.ID
		}
	}
	if v, e := r.discovery.ServerVersion(); e == nil {
		inv.Clusters[0].KubernetesVersion = v.GitVersion
		inv.Clusters[0].Distribution = distribution(v.GitVersion)
		inv.Clusters[0].Confidence = "server-version"
	} else {
		coverage = append(coverage, model.Coverage{Collector: "server-version", State: "UNREADABLE", Code: "VERSION_UNAVAILABLE"})
	}
	inv.Clusters[0].Gaps = append(inv.Clusters[0].Gaps, "CONTROL_PLANE_MEMBERS_NOT_ENUMERATED")
	inv.Coverage = coverage
	if opts.HelmStorage {
		r.helm(ctx, &inv, opts.Namespace)
	} else {
		inv.Coverage = append(inv.Coverage, model.Coverage{Collector: "helm-storage", State: "NOT_REQUESTED"})
	}
	for i := range inv.Components {
		for _, im := range inv.Components[i].Images {
			p, ok := opts.IdentityMappings[im.Image]
			if !ok {
				p, ok = opts.IdentityMappings[im.Repository]
			}
			if ok {
				inv.Components[i].Project = p
				inv.Components[i].IdentityConfidence = "explicit-mapping"
			}
		}
	}
	clusterConsistency(&inv)
	return Correlate(inv), nil
}
func distribution(v string) string {
	for _, s := range []string{"k3s", "rke2", "eks", "gke", "openshift"} {
		if strings.Contains(strings.ToLower(v), s) {
			return s
		}
	}
	return "unknown"
}
