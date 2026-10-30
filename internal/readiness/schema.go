package readiness

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/wussh/kubecompact/data"
	"github.com/wussh/kubecompact/internal/compatibility/fetch"
	"github.com/wussh/kubecompact/internal/model"
	"github.com/yannh/kubeconform/pkg/validator"
)

const schemaSourceRevision = "7464e8eb55dab7063a4e6cb6cd6e2ed19f399580"

func schemaName(kind, api string) string {
	parts := strings.Split(api, "/")
	suffix := strings.Split(parts[0], ".")[0]
	if len(parts) > 1 {
		suffix += "-" + parts[1]
	}
	return strings.ToLower(kind + "-" + suffix + ".json")
}
func schemaChecks(ctx context.Context, inv model.Inventory, opts model.CheckOptions) ([]model.CheckResult, error) {
	schemas, err := data.Schemas()
	if err != nil {
		return nil, errors.New("embedded schema integrity failed")
	}
	custom := map[string]json.RawMessage{}
	for _, o := range inv.Objects {
		if o["kind"] != "CustomResourceDefinition" {
			continue
		}
		spec := mapping(o["spec"])
		group := text(spec["group"])
		kind := text(mapping(spec["names"])["kind"])
		for _, v := range array(spec["versions"]) {
			version := mapping(v)
			if version["served"] != true {
				continue
			}
			schema := mapping(mapping(version["schema"])["openAPIV3Schema"])
			if len(schema) > 0 {
				b, _ := json.Marshal(schema)
				custom[group+"/"+text(version["name"])+"/"+kind] = b
			}
		}
	}
	objects := map[string]map[string]any{}
	for _, o := range inv.Objects {
		meta := mapping(o["metadata"])
		key := text(o["apiVersion"]) + "/" + text(o["kind"]) + "/" + text(meta["namespace"]) + "/" + text(meta["name"])
		objects[key] = o
	}
	dir, err := os.MkdirTemp("", "kubecompat-schemas-")
	if err != nil {
		return nil, errors.New("schema temporary directory unavailable")
	}
	defer os.RemoveAll(dir)
	var results []model.CheckResult
	for _, r := range inv.Resources {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		scope := r.Ref.Key()
		result := check("schema", scope, "UNKNOWN", "WARNING", "RAW_DOCUMENT_UNAVAILABLE")
		result.Details = map[string]string{"schemaTarget": "1.35.0", "validator": "kubeconform/0.8.0", "sourceRevision": schemaSourceRevision}
		api := r.Ref.APIVersion
		key := api + "/" + r.Ref.Kind + "/" + r.Ref.Namespace + "/" + r.Ref.Name
		o, ok := objects[key]
		if !ok {
			results = append(results, result)
			continue
		}
		name := schemaName(r.Ref.Kind, api)
		var b []byte
		if raw, ok := custom[api+"/"+r.Ref.Kind]; ok {
			b = raw
			result.Details["schemaSource"] = "observed matching CRD served version"
		} else if opts.SchemaPath != "" {
			b, err = readLocalSchema(opts.SchemaPath, name)
			if err != nil {
				result.Reason = "LOCAL_SCHEMA_UNAVAILABLE"
				results = append(results, result)
				continue
			}
		} else {
			b = schemas[name]
		}
		if len(b) == 0 {
			result.Reason = "TARGET_SCHEMA_MISSING"
			results = append(results, result)
			continue
		}
		var schema any
		if json.Unmarshal(b, &schema) != nil || !localReferences(schema) {
			result.Reason = "SCHEMA_UNSAFE_OR_INVALID"
			results = append(results, result)
			continue
		}
		hash := sha256.Sum256(b)
		path := filepath.Join(dir, hex.EncodeToString(hash[:])+".json")
		if os.WriteFile(path, b, 0600) != nil {
			return nil, errors.New("schema staging failed")
		}
		val, e := validator.New([]string{path}, validator.Opts{KubernetesVersion: "1.35.0", Strict: true})
		if e != nil {
			result.Reason = "SCHEMA_UNSUPPORTED"
			results = append(results, result)
			continue
		}
		clean := make(map[string]any, len(o))
		for k, v := range o {
			if k != "_document" {
				clean[k] = v
			}
		}
		raw, e := json.Marshal(clean)
		if e != nil {
			result.Reason = "DOCUMENT_INVALID"
			results = append(results, result)
			continue
		}
		checks := val.ValidateWithContext(ctx, "document.json", io.NopCloser(bytes.NewReader(raw)))
		result.State = "UNKNOWN"
		result.Reason = "SCHEMA_VALIDATION_UNAVAILABLE"
		if len(checks) == 1 {
			switch checks[0].Status {
			case validator.Valid:
				result.State = "PASS"
				result.Severity = "INFO"
				result.Reason = "TARGET_SCHEMA_VALID"
			case validator.Invalid:
				result.State = "FAIL"
				result.Severity = "BLOCKER"
				result.Reason = "TARGET_SCHEMA_INVALID"
			}
		}
		results = append(results, result)
	}
	return results, nil
}
func localReferences(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, v := range x {
			if k == "$ref" {
				s, ok := v.(string)
				if !ok || !strings.HasPrefix(s, "#") {
					return false
				}
			}
			if k == "$id" || k == "id" {
				if _, ok := v.(string); ok {
					return false
				}
			}
			if !localReferences(v) {
				return false
			}
		}
	case []any:
		for _, v := range x {
			if !localReferences(v) {
				return false
			}
		}
	}
	return true
}
func readLocalSchema(root, name string) ([]byte, error) {
	if filepath.Base(name) != name {
		return nil, errors.New("invalid schema name")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil || real != abs {
		return nil, errors.New("schema symlink forbidden")
	}
	p := filepath.Join(abs, "v1.35.0-standalone-strict", name)
	info, err := os.Lstat(p)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 8<<20 {
		return nil, errors.New("schema unavailable")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(p))
	if err != nil || parent != filepath.Dir(p) {
		return nil, errors.New("schema symlink forbidden")
	}
	return os.ReadFile(p)
}

// AcquireSchemas refreshes the exact pinned bundle only, never a mutable master.
// The caller must explicitly supply an online policy-enforcing fetcher.
func AcquireSchemas(ctx context.Context, f fetch.Fetcher, dir, target string) error {
	if _, err := targetMinor(target); err != nil {
		return err
	}
	var p struct {
		Schemas map[string]string `json:"schemas"`
	}
	if json.Unmarshal(data.Provenance(), &p) != nil {
		return errors.New("schema provenance invalid")
	}
	dest := filepath.Join(dir, "v1.35.0-standalone-strict")
	if err := os.MkdirAll(dest, 0700); err != nil {
		return errors.New("schema cache unavailable")
	}
	for name, digest := range p.Schemas {
		if err := ctx.Err(); err != nil {
			return err
		}
		snapshot, err := f.Fetch(ctx, fetch.SourceRequest{URL: "https://raw.githubusercontent.com/yannh/kubernetes-json-schema/" + schemaSourceRevision + "/v1.35.0-standalone-strict/" + name, Project: "kubernetes", ParserID: "schema-bundle", ParserVersion: "1"})
		if err != nil {
			return errors.New("schema download unavailable")
		}
		sum := sha256.Sum256(snapshot.Body)
		if hex.EncodeToString(sum[:]) != digest {
			return errors.New("schema digest mismatch")
		}
		var schema any
		if json.Unmarshal(snapshot.Body, &schema) != nil || !localReferences(schema) {
			return errors.New("schema references unsafe")
		}
		tmp, err := os.CreateTemp(dest, ".schema-")
		if err != nil {
			return errors.New("schema cache write failed")
		}
		tmpName := tmp.Name()
		_, err = tmp.Write(snapshot.Body)
		closeErr := tmp.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Rename(tmpName, filepath.Join(dest, name))
		}
		if err != nil {
			os.Remove(tmpName)
			return errors.New("schema cache write failed")
		}
	}
	return os.WriteFile(filepath.Join(dir, "provenance.json"), data.Provenance(), 0600)
}
