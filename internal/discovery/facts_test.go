package discovery

import "testing"

func TestReadinessFactsStorageAndAdmission(t *testing.T) {
	crd := facts(object(`{"kind":"CustomResourceDefinition","status":{"storedVersions":["v1beta1","v1"]},"spec":{"versions":[{"name":"v1","served":true,"storage":true,"schema":{"openAPIV3Schema":{"description":"DO-NOT-EXPORT"}}}]}}`))
	if len(slice(at(crd, "status", "storedVersions"))) != 2 {
		t.Fatal(crd)
	}
	if at(crd, "spec", "versions") == nil {
		t.Fatal(crd)
	}
	w := facts(object(`{"kind":"ValidatingWebhookConfiguration","webhooks":[{"name":"w","matchPolicy":"Equivalent","clientConfig":{"caBundle":"DO-NOT-EXPORT"}}]}`))
	if text(rows(w["webhooks"])[0], "matchPolicy") != "Equivalent" {
		t.Fatal(w)
	}
}
