package fetch

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxBody = 4 << 20

var ErrOffline = errors.New("network access disabled offline")

type Config struct {
	Online          bool
	CAFile          string
	AllowedHosts    []string
	CredentialsFile string
}
type SourceRequest struct{ URL, Project, ParserID, ParserVersion string }
type SourceSnapshot struct {
	URL, FinalURL, SHA256, MediaType string
	Body                             []byte
	RetrievedAt                      time.Time
}
type Fetcher interface {
	Fetch(context.Context, SourceRequest) (SourceSnapshot, error)
}
type credential struct {
	Authorization string `json:"authorization"`
}
type cached struct {
	snapshot       SourceSnapshot
	etag, modified string
}
type Client struct {
	online      bool
	http        *http.Client
	allowed     map[string]bool
	credentials map[string]credential
	mu          sync.Mutex
	documents   map[string]cached
	last        time.Time
}

var knownHosts = strings.Fields("github.com api.github.com raw.githubusercontent.com kubernetes.io docs.k3s.io docs.cilium.io cilium.io docs.tigera.io cert-manager.io argo-cd.readthedocs.io fluxcd.io coredns.io kubernetes.github.io prometheus-operator.dev velero.io keda.sh kyverno.io istio.io rook.io longhorn.io docs.nvidia.com kubevirt.io docs.projectcalico.org projectcalico.docs.tigera.io charts.jetstack.io charts.helm.sh artifacthub.io quay.io")

func New(cfg Config) (*Client, error) {
	c := &Client{online: cfg.Online, allowed: map[string]bool{}, credentials: map[string]credential{}, documents: map[string]cached{}}
	for _, h := range cfg.AllowedHosts {
		h = strings.ToLower(h)
		if strings.ContainsAny(h, "/?#@") || h == "" {
			return nil, errors.New("invalid allowed host")
		}
		c.allowed[h] = true
	}
	roots, e := x509.SystemCertPool()
	if e != nil {
		roots = x509.NewCertPool()
	}
	if cfg.CAFile != "" {
		b, e := os.ReadFile(cfg.CAFile)
		if e != nil || !roots.AppendCertsFromPEM(b) {
			return nil, errors.New("invalid custom CA")
		}
	}
	if cfg.CredentialsFile != "" {
		fi, e := os.Lstat(cfg.CredentialsFile)
		if e != nil || !fi.Mode().IsRegular() || fi.Mode().Perm() != 0600 || fi.Size() > 65536 {
			return nil, errors.New("credentials require regular mode0600 file")
		}
		b, e := os.ReadFile(cfg.CredentialsFile)
		if e != nil || json.Unmarshal(b, &c.credentials) != nil {
			return nil, errors.New("invalid credentials file")
		}
		for origin, v := range c.credentials {
			u, e := url.Parse(origin)
			if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || strings.ContainsAny(v.Authorization, "\r\n") {
				return nil, errors.New("invalid credential origin")
			}
		}
	}
	tr := &http.Transport{Proxy: http.ProxyFromEnvironment, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second, MaxResponseHeaderBytes: 65536, DisableCompression: false}
	tr.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(address)
		if e != nil {
			return nil, errors.New("invalid network address")
		}
		ips, e := net.DefaultResolver.LookupIPAddr(ctx, host)
		if e != nil {
			return nil, errors.New("source DNS unavailable")
		}
		explicit := c.allowed[strings.ToLower(address)] || c.allowed[strings.ToLower(host)]
		for _, ip := range ips {
			if !explicit && !publicIP(ip.IP) {
				return nil, errors.New("source address prohibited")
			}
		}
		for _, ip := range ips {
			conn, e := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if e == nil {
				return conn, nil
			}
		}
		return nil, errors.New("source connection unavailable")
	}
	c.http = &http.Client{Transport: tr, Timeout: 30 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("redirect limit")
		}
		req.Header.Del("Authorization")
		req.Header.Del("Cookie")
		return c.validate(req.Context(), req.URL)
	}}
	return c, nil
}
func publicIP(ip net.IP) bool {
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsUnspecified() && !net.ParseIP("100.64.0.0").Equal(ip) && !inCIDR(ip, "100.64.0.0/10") && !inCIDR(ip, "192.0.0.0/24") && !inCIDR(ip, "198.18.0.0/15")
}
func inCIDR(ip net.IP, s string) bool { _, n, _ := net.ParseCIDR(s); return n.Contains(ip) }
func (c *Client) validate(ctx context.Context, u *url.URL) error {
	if !c.online {
		return ErrOffline
	}
	if u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("unsafe source URL")
	}
	h := strings.ToLower(u.Hostname())
	explicit := c.allowed[strings.ToLower(u.Host)] || c.allowed[h]
	known := false
	for _, k := range knownHosts {
		if h == k {
			known = true
			break
		}
	}
	if !known && !explicit {
		return errors.New("source host not allowed")
	}
	if !explicit && u.Port() != "" && u.Port() != "443" {
		return errors.New("source port not allowed")
	}
	ips, e := net.DefaultResolver.LookupIPAddr(ctx, h)
	if e != nil {
		return errors.New("source DNS unavailable")
	}
	if len(ips) == 0 {
		return errors.New("source DNS empty")
	}
	for _, ip := range ips {
		if !explicit && !publicIP(ip.IP) {
			return errors.New("source address prohibited")
		}
	}
	return nil
}
func (c *Client) Fetch(ctx context.Context, r SourceRequest) (SourceSnapshot, error) {
	if !c.online {
		return SourceSnapshot{}, ErrOffline
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	u, e := url.Parse(r.URL)
	if e != nil {
		return SourceSnapshot{}, errors.New("invalid source URL")
	}
	if e = c.validate(ctx, u); e != nil {
		return SourceSnapshot{}, e
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	previous := c.documents[r.URL]
	var serverDelay time.Duration
	for attempt := range 3 {
		delay := time.Until(c.last.Add(100 * time.Millisecond))
		if attempt > 0 {
			delay = time.Duration(attempt) * 200 * time.Millisecond
		}
		if serverDelay > delay {
			delay = serverDelay
		}
		serverDelay = 0
		if delay > 0 {
			select {
			case <-ctx.Done():
				return SourceSnapshot{}, errors.New("source deadline reached")
			case <-time.After(delay):
			}
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, r.URL, nil)
		if strings.HasPrefix(u.Path, "/v2/") && strings.Contains(u.Path, "/manifests/") {
			req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json")
		}
		if auth := c.credentials[u.Scheme+"://"+u.Host].Authorization; auth != "" {
			req.Header.Set("Authorization", auth)
		}
		if previous.etag != "" {
			req.Header.Set("If-None-Match", previous.etag)
		}
		if previous.modified != "" {
			req.Header.Set("If-Modified-Since", previous.modified)
		}
		c.last = time.Now()
		resp, e := c.http.Do(req)
		if e != nil {
			if attempt < 2 && ctx.Err() == nil {
				continue
			}
			return SourceSnapshot{}, errors.New("source request failed")
		}
		if resp.StatusCode == 304 {
			resp.Body.Close()
			if previous.snapshot.Body == nil {
				return SourceSnapshot{}, errors.New("source returned 304 without cached body")
			}
			s := previous.snapshot
			s.Body = append([]byte(nil), s.Body...)
			return s, nil
		}
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			serverDelay = retryAfter(resp.Header.Get("Retry-After"), time.Now())
			resp.Body.Close()
			if attempt < 2 {
				continue
			}
			return SourceSnapshot{}, errors.New("source temporarily unavailable")
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			return SourceSnapshot{}, errors.New("source HTTP response rejected")
		}
		b, e := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
		resp.Body.Close()
		if e != nil || len(b) > maxBody {
			return SourceSnapshot{}, errors.New("source response exceeds bounds")
		}
		sum := sha256.Sum256(b)
		s := SourceSnapshot{URL: r.URL, FinalURL: resp.Request.URL.String(), SHA256: hex.EncodeToString(sum[:]), MediaType: resp.Header.Get("Content-Type"), Body: b, RetrievedAt: time.Now().UTC()}
		// Validators are an in-process optimization, not an unbounded document archive.
		if len(c.documents) >= 16 {
			for key := range c.documents {
				delete(c.documents, key)
				break
			}
		}
		c.documents[r.URL] = cached{snapshot: s, etag: resp.Header.Get("ETag"), modified: resp.Header.Get("Last-Modified")}
		s.Body = append([]byte(nil), b...)
		return s, nil
	}
	return SourceSnapshot{}, errors.New("source unavailable")
}
func retryAfter(value string, now time.Time) time.Duration {
	var delay time.Duration
	if seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
		if seconds <= 0 {
			return 0
		}
		if seconds >= 5 {
			return 5 * time.Second
		}
		delay = time.Duration(seconds) * time.Second
	} else if date, err := http.ParseTime(value); err == nil {
		delay = date.Sub(now)
	}
	if delay < 0 {
		return 0
	}
	if delay > 5*time.Second {
		return 5 * time.Second
	}
	return delay
}
