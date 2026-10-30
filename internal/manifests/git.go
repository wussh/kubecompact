package manifests

import (
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/wussh/kubecompact/internal/compatibility/fetch"
	"github.com/wussh/kubecompact/internal/model"
)

var commitID = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)

func checkout(ctx context.Context, opts model.ScanOptions) (string, func(), error) {
	noop := func() {}
	if !opts.Online {
		return "", noop, errors.New("Git fetching prohibited offline")
	}
	if !commitID.MatchString(opts.GitRevision) {
		return "", noop, errors.New("Git revision must be a full immutable commit SHA")
	}
	u, err := url.Parse(opts.GitURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", noop, errors.New("Git requires an HTTPS URL without credentials")
	}
	if opts.GitPath != "" && (filepath.IsAbs(opts.GitPath) || strings.Contains(opts.GitPath, "\\")) {
		return "", noop, errors.New("unsafe Git subpath")
	}
	clean := filepath.Clean(opts.GitPath)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", noop, errors.New("unsafe Git subpath")
	}
	client, err := fetch.New(fetch.Config{Online: opts.Online, CAFile: opts.CAFile, AllowedHosts: opts.AllowedHosts, CredentialsFile: opts.CredentialsFile})
	if err != nil {
		return "", noop, err
	}
	endpoint, err := transport.NewEndpoint(opts.GitURL)
	if err != nil {
		return "", noop, errors.New("invalid Git endpoint")
	}
	session, err := githttp.NewClient(client.HTTPClient()).NewUploadPackSession(endpoint, nil)
	if err != nil {
		return "", noop, errors.New("Git transport unavailable")
	}
	defer session.Close()
	req := packp.NewUploadPackRequest()
	req.Wants = []plumbing.Hash{plumbing.NewHash(opts.GitRevision)}
	req.Depth = packp.DepthCommits(1)
	if err := req.Capabilities.Set(capability.Shallow); err != nil {
		return "", noop, errors.New("Git shallow capability unavailable")
	}
	response, err := session.UploadPack(ctx, req)
	if err != nil {
		return "", noop, errors.New("Git revision unavailable")
	}
	defer response.Close()
	store := memory.NewStorage()
	limited := &boundedStore{Storage: store}
	if err = packfile.UpdateObjectStorage(limited, io.LimitReader(response, maxTotal+1)); err != nil {
		return "", noop, errors.New("Git pack invalid or exceeds limits")
	}
	commit, err := object.GetCommit(store, plumbing.NewHash(opts.GitRevision))
	if err != nil {
		return "", noop, errors.New("Git pinned commit not returned")
	}
	tree, err := commit.Tree()
	if err != nil {
		return "", noop, errors.New("Git tree unavailable")
	}
	dir, err := os.MkdirTemp("", "kubecompat-git-")
	if err != nil {
		return "", noop, errors.New("Git temporary directory unavailable")
	}
	cleanup := func() { os.RemoveAll(dir) }
	count := 0
	var total int64
	err = tree.Files().ForEach(func(f *object.File) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		count++
		if count > maxFiles || f.Size > maxFile || f.Size < 0 {
			return errors.New("Git file limit")
		}
		total += f.Size
		if total > maxTotal {
			return errors.New("Git tree limit")
		}
		if f.Mode != filemode.Regular && f.Mode != filemode.Executable {
			return errors.New("Git links/submodules forbidden")
		}
		p := filepath.Clean(filepath.FromSlash(f.Name))
		if filepath.IsAbs(p) || p == ".." || strings.HasPrefix(p, ".."+string(filepath.Separator)) || strings.Contains(f.Name, "\\") {
			return errors.New("Git path forbidden")
		}
		dest := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			return err
		}
		r, err := f.Reader()
		if err != nil {
			return err
		}
		defer r.Close()
		b, err := io.ReadAll(io.LimitReader(r, maxFile+1))
		if err != nil || len(b) > maxFile {
			return errors.New("Git blob limit")
		}
		return os.WriteFile(dest, b, 0600)
	})
	if err != nil {
		cleanup()
		return "", noop, errors.New("Git checkout unsafe or unavailable")
	}
	root := filepath.Join(dir, clean)
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		cleanup()
		return "", noop, errors.New("Git input directory unavailable")
	}
	return root, cleanup, nil
}

// Bound decompressed objects before they enter the in-memory object database.
type boundedStore struct {
	*memory.Storage
	total int64
}

func (s *boundedStore) SetEncodedObject(o plumbing.EncodedObject) (plumbing.Hash, error) {
	if o.Size() < 0 || o.Size() > maxFile || s.total+o.Size() > maxTotal {
		return plumbing.ZeroHash, errors.New("Git object limit")
	}
	s.total += o.Size()
	return s.Storage.SetEncodedObject(o)
}
func (s *boundedStore) NewEncodedObject() plumbing.EncodedObject { return &boundedObject{store: s} }

type boundedObject struct {
	plumbing.MemoryObject
	store   *boundedStore
	written int64
}

func (o *boundedObject) Writer() (io.WriteCloser, error) {
	if o.Size() < 0 || o.Size() > maxFile {
		return nil, errors.New("Git object size exceeded")
	}
	return o, nil
}
func (o *boundedObject) Write(p []byte) (int, error) {
	if o.written+int64(len(p)) > maxFile || o.store.total+o.written+int64(len(p)) > maxTotal {
		return 0, errors.New("Git object allocation limit")
	}
	n, err := o.MemoryObject.Write(p)
	o.written += int64(n)
	return n, err
}
