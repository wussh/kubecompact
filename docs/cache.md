# Evidence cache and transport

The evidence client is offline by default. `Config.Online` must explicitly enable requests; the offline check runs before URL parsing, DNS or HTTP, including when the client is reused for schemas. Only HTTPS GET is supported. Built-in official project hosts or explicitly configured `AllowedHosts` are accepted. An allowed entry is an exact hostname or hostname:port, never a suffix or wildcard. Explicit allowlisting permits private addresses for that origin; TLS verification remains required. Custom roots augment system roots.

Every redirect is revalidated, all resolved addresses must satisfy policy, and dialing pins a validated address rather than repeating unchecked DNS resolution. Redirects never carry Authorization or Cookie headers, even to the same origin. Standard environment proxies are supported; their dial addresses also undergo address policy checks. A private proxy therefore needs an explicit host allowlist entry. Proxy credentials are supplied only through standard proxy configuration, not source credential forwarding.

Requests have a 45-second overall deadline, 30-second HTTP timeout, at most three attempts, five redirects, 64 KiB response headers, and 4 MiB decompressed responses. Requests are serialized with at least 100 ms spacing. Retryable server failures and rate limiting honor `Retry-After` delta-seconds or HTTP dates, bounded to five seconds per retry and the remaining request deadline. Invalid/past values use normal bounded backoff. Errors deliberately omit URLs, server bodies and underlying network errors.

## Explicit credentials

A regular, non-symlink mode-0600 JSON file maps exact origins to explicit authorization headers:

```json
{"https://private.example.org":{"authorization":"Bearer example-only"}}
```

There is no automatic credential discovery. Userinfo, query strings and fragments in source URLs are rejected, not persisted. The configured origin requires a trusted certificate and explicit host allowlisting if not built in. Credentials stay in memory and are never included in returned snapshots.

The library HTTP transport has one query exception for Git protocol discovery: GET paths ending `/info/refs` with the exact query `service=git-upload-pack`. Validation removes that fixed query from a cloned URL for origin/DNS/TLS checks, but forwards the original request. Extra keys, receive-pack, other paths and documentation fetch queries remain rejected. Signed public OCI CDN redirects are not generally allowed; see [source health](source-health.md).

## SQLite storage

`cache.Open(path)` requires a private directory and a regular private database file, creating them as 0700/0600 when absent. Existing permissive paths, symlinks at the database/immediate directory boundary, oversized files, failed SQLite integrity checks and unsupported future schemas are rejected. Migrations and evidence/rule insertion are transactional. SQLite uses the pure-Go modernc driver.

`Save(ctx, evidence, rules, snapshot)` is called only after successful parsing. It requires valid dated evidence, parser identity, nonempty rules, safe HTTPS origins, and a matching source-body SHA-256. Duplicate rule IDs and mismatched evidence are rejected before insertion. Evidence IDs are immutable: changed content or provenance needs a new ID. An identical write is idempotent. Save does not change expiry or verification dates. Failed fetches/parsers never call Save; failed saves cannot replace the previous good set.

Raw fetched pages are **not persisted**. The retained sanitized snapshot is the parser's bounded evidence excerpt plus the hash of the transient fetched bytes. Rule/evidence JSON is size bounded and screened for sensitive payload markers. Callers must construct evidence solely from public compatibility claims, never cluster inputs, credentials or arbitrary server error content. The cache cannot determine whether arbitrary prose was secretly supplied by a caller; it is not a general secret-redaction service.

`Load(ctx, project)` returns every claim group belonging to the latest successfully committed snapshot timestamp for each source/project; an empty project loads all projects. Parsers must retain the same snapshot retrieval timestamp across support, tested-version and other evidence groups, rather than generating a timestamp per claim. Reads verify record hashes and bound evidence/rule counts. Stale records remain available unchanged for explanation, not implicitly refreshed. Up to 10,000 immutable sets can be stored; use a new cache file when the bound is reached. There is no destructive automatic eviction.

HTTP validators are held in process memory only. A 304 requires a previously retained body and returns that body's original retrieval date/hash rather than manufacturing new claim freshness. Restarting the process performs a full fetch; it cannot accept a bodyless 304. Parsing failures retain the previous SQLite evidence, independently of transport validators.

## Tests

The package tests use disposable local TLS servers and private temporary files. They cover offline denial, public/private address policy, URL credential canaries, bounds, conditional response provenance, invalid credential permissions, transaction rejection, immutable/stale evidence and corrupt/future database schemas. They do not contact production Kubernetes clusters.
