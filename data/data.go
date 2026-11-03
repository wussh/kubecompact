// Package data contains attributed, version-pinned offline readiness inputs.
package data

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
)

//go:embed api-lifecycle*.json schemas/*.json.gz.b64 schemas/provenance*.json
var files embed.FS

// TargetData owns one immutable target catalog and its lazily decoded schemas.
// Callers must not mutate the returned schema map or its values.
type TargetData struct {
	target, lifecycle, provenance string
	once                          sync.Once
	schemas                       map[string]json.RawMessage
	schemaError                   error
}

var targets = map[string]*TargetData{
	"1.33.0": {target: "1.33.0", lifecycle: "api-lifecycle-1.33.0.json", provenance: "schemas/provenance-1.33.0.json"},
	"1.34.0": {target: "1.34.0", lifecycle: "api-lifecycle-1.34.0.json", provenance: "schemas/provenance-1.34.0.json"},
	"1.35.0": {target: "1.35.0", lifecycle: "api-lifecycle.json", provenance: "schemas/provenance.json"},
	"1.36.0": {target: "1.36.0", lifecycle: "api-lifecycle-1.36.0.json", provenance: "schemas/provenance-1.36.0.json"},
	"1.37.0": {target: "1.37.0", lifecycle: "api-lifecycle-1.37.0.json", provenance: "schemas/provenance-1.37.0.json"},
}

// ForTarget requires an exact supported schema patch; there is no fallback.
func ForTarget(target string) (*TargetData, error) {
	d, ok := targets[target]
	if !ok {
		return nil, errors.New("unsupported readiness dataset target")
	}
	return d, nil
}
func (d *TargetData) Lifecycle() ([]byte, error)  { return files.ReadFile(d.lifecycle) }
func (d *TargetData) Provenance() ([]byte, error) { return files.ReadFile(d.provenance) }
func (d *TargetData) Schemas() (map[string]json.RawMessage, error) {
	d.once.Do(func() {
		b, e := files.ReadFile("schemas/kubernetes-" + d.target + ".json.gz.b64")
		if e != nil {
			d.schemaError = e
			return
		}
		compressed, e := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
		if e != nil {
			d.schemaError = e
			return
		}
		r, e := gzip.NewReader(bytes.NewReader(compressed))
		if e != nil {
			d.schemaError = e
			return
		}
		defer r.Close()
		b, e = io.ReadAll(io.LimitReader(r, (64<<20)+1))
		if e != nil {
			d.schemaError = e
			return
		}
		if len(b) > 64<<20 {
			d.schemaError = errors.New("schema bundle exceeds limit")
			return
		}
		provenance, e := d.Provenance()
		if e != nil {
			d.schemaError = e
			return
		}
		var p struct {
			SHA    string `json:"bundleSHA256"`
			Target string `json:"target"`
		}
		if json.Unmarshal(provenance, &p) != nil || p.Target != d.target {
			d.schemaError = errors.New("invalid bundle provenance")
			return
		}
		h := sha256.Sum256(b)
		if hex.EncodeToString(h[:]) != p.SHA {
			d.schemaError = errors.New("bundle integrity mismatch")
			return
		}
		d.schemaError = json.Unmarshal(b, &d.schemas)
	})
	return d.schemas, d.schemaError
}
