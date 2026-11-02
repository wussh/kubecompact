package manifests

import (
 "fmt"
 "strings"
 "testing"
)

func TestDocumentLimitRequiresStreamExhaustion(t *testing.T) {
 var body strings.Builder
 for i:=0;i<maxFiles;i++ { fmt.Fprintf(&body,"apiVersion: v1\nkind: Service\nmetadata: {name: service-%d}\n---\n",i) }
 // The separator starts an additional empty document, which also exceeds the bound.
 base:=strings.TrimSuffix(body.String(),"---\n")
 for _,tail:=range []string{"", "---\napiVersion: policy/v1beta1\nkind: PodDisruptionBudget\nmetadata: {name: tail}\n", "---\n", "---\n[invalid"} {
  objects,ds:=decode(inputDocument{name:"public.yaml",body:[]byte(base+tail)})
  if len(objects)!=maxFiles { t.Fatalf("retained %d documents",len(objects)) }
  if tail=="" { if len(ds)!=0 {t.Fatalf("exact boundary: %v",ds)};continue }
  if len(ds)!=1 || ds[0].Code!="DOCUMENT_LIMIT_EXCEEDED" || ds[0].Scope!="public.yaml#4097" {t.Fatalf("unread suffix not diagnosed safely: %v",ds)}
 }
}
