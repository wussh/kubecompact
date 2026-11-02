package cli

import (
 "bytes"
 "context"
 "fmt"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestDocumentLimitCLI(t *testing.T) {
 t.Setenv("KUBECONFIG","/nonexistent")
 for _,tc:=range []struct{name string; services int; pdb bool; scan,check int}{{"boundary",4096,false,0,3},{"control",4095,true,0,2},{"overlimit",4096,true,1,1}} {
  t.Run(tc.name,func(t *testing.T){
   var body strings.Builder
   for i:=range tc.services {fmt.Fprintf(&body,"apiVersion: v1\nkind: Service\nmetadata: {name: service-%d}\n---\n",i)}
   if tc.pdb {body.WriteString("apiVersion: policy/v1beta1\nkind: PodDisruptionBudget\nmetadata: {name: removed-tail}\n")} 
   text:=strings.TrimSuffix(body.String(),"---\n")
   dir:=t.TempDir(); path:=filepath.Join(dir,"public.yaml")
   if err:=os.WriteFile(path,[]byte(text),0600);err!=nil{t.Fatal(err)}
   for _,command:=range []string{"scan","check"}{
    args:=[]string{command,"--manifests",path,"--offline","--format","json","--cache",filepath.Join(dir,"missing.db")}
    want:=tc.scan;if command=="check"{args=append(args,"--target-k8s","1.35");want=tc.check}
    var out,errs bytes.Buffer;got:=Run(context.Background(),args,&out,&errs)
    if got!=want{t.Fatalf("%s exit%d want%d: %s",command,got,want,errs.String())}
    limited:=strings.Contains(out.String(),"DOCUMENT_LIMIT_EXCEEDED")
    if limited!=(tc.name=="overlimit"){t.Fatalf("%s limit diagnostic=%v",command,limited)}
    if tc.name=="overlimit" && (!strings.Contains(out.String(),"PARTIAL") || strings.Contains(out.String(),`"state": "COMPLETE"`)){t.Fatal("truncated input falsely complete")}
    if command=="check" && tc.name=="control" && !strings.Contains(out.String(),"API_REMOVED"){t.Fatal("boundary blocker lost")}
   }
  })
 }
}
