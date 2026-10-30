package fetch

import (
	"errors"
	"io"
	"net/http"
	"strings"
)

// HTTPClient applies the same origin, DNS, TLS and offline policy to library HTTP integrations.
// The caller owns protocol parsing and must bound any decoded archive or checkout.
func (c *Client) HTTPClient() *http.Client {
	client := *c.http
	client.Transport = policyTransport{client: c, base: c.http.Transport}
	return &client
}

type policyTransport struct {
	client *Client
	base   http.RoundTripper
}

func (t policyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	u := *req.URL
	if req.Method == http.MethodGet && strings.HasSuffix(u.Path, "/info/refs") && u.RawQuery == "service=git-upload-pack" {
		u.RawQuery = ""
	}
	if err := t.client.validate(req.Context(), &u); err != nil {
		return nil, err
	}
	if req.Method != http.MethodGet && req.Method != http.MethodHead && req.Method != http.MethodPost {
		return nil, errors.New("source method prohibited")
	}
	r := req.Clone(req.Context())
	r.Header = req.Header.Clone()
	r.Header.Del("Authorization")
	r.Header.Del("Cookie")
	if req.Response == nil {
		if auth := t.client.credentials[req.URL.Scheme+"://"+req.URL.Host].Authorization; auth != "" {
			r.Header.Set("Authorization", auth)
		}
	}
	resp, err := t.base.RoundTrip(r)
	if err != nil {
		return nil, errors.New("source transport failed")
	}
	resp.Body = &boundedBody{ReadCloser: resp.Body, remaining: maxBody}
	return resp, nil
}

type boundedBody struct {
	io.ReadCloser
	remaining int64
}

func (b *boundedBody) Read(p []byte) (int, error) {
	if int64(len(p)) > b.remaining+1 {
		p = p[:b.remaining+1]
	}
	n, e := b.ReadCloser.Read(p)
	b.remaining -= int64(n)
	if b.remaining < 0 {
		return 0, errors.New("source response exceeds bounds")
	}
	return n, e
}
