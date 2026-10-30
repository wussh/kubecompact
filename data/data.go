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

//go:embed api-lifecycle.json schemas/kubernetes-1.35.0.json.gz.b64 schemas/provenance.json
var files embed.FS

func Lifecycle() []byte  { b, _ := files.ReadFile("api-lifecycle.json"); return b }
func Provenance() []byte { b, _ := files.ReadFile("schemas/provenance.json"); return b }

var once sync.Once
var schemas map[string]json.RawMessage
var schemaError error

func Schemas() (map[string]json.RawMessage, error) {
	once.Do(func() {
		b, e := files.ReadFile("schemas/kubernetes-1.35.0.json.gz.b64")
		if e != nil {
			schemaError = e
			return
		}
		compressed, e := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
		if e != nil {
			schemaError = e
			return
		}
		r, e := gzip.NewReader(bytes.NewReader(compressed))
		if e != nil {
			schemaError = e
			return
		}
		defer r.Close()
		b, e = io.ReadAll(io.LimitReader(r, 64<<20))
		if e != nil {
			schemaError = e
			return
		}
		var p struct {
			SHA string `json:"bundleSHA256"`
		}
		if json.Unmarshal(Provenance(), &p) != nil {
			schemaError = errors.New("invalid bundle provenance")
			return
		}
		h := sha256.Sum256(b)
		if hex.EncodeToString(h[:]) != p.SHA {
			schemaError = errors.New("bundle integrity mismatch")
			return
		}
		schemaError = json.Unmarshal(b, &schemas)
	})
	return schemas, schemaError
}
