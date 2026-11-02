package readiness

import (
	"context"
	"github.com/wussh/kubecompact/internal/model"
	"testing"
)

func TestChecksCarrySafeResourceIdentityWithoutPayload(t *testing.T) {
	ref := model.ResourceRef{APIVersion: "policy/v1beta1", Group: "policy", Kind: "PodDisruptionBudget", Namespace: "pilot", Name: "removed-budget", UID: "uid"}
	inv := model.Inventory{Resources: []model.Resource{{Ref: ref, Annotations: map[string]string{"secret": "PRIVATE_CANARY"}, Document: []byte("PRIVATE_CANARY")}}}
	checks, err := Check(context.Background(), inv, model.CheckOptions{TargetKubernetes: "1.35"})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range checks {
		if c.Scope == ref.Key() {
			for k, want := range map[string]string{"kind": ref.Kind, "name": ref.Name, "namespace": ref.Namespace, "apiVersion": ref.APIVersion} {
				if c.Details[k] != want {
					t.Errorf("%s lacks %s", c.ID, k)
				}
			}
			for _, v := range c.Details {
				if v == "PRIVATE_CANARY" {
					t.Fatal("payload leaked")
				}
			}
		}
	}
}
