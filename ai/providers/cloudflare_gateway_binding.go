// AI Gateway transport over the Workers AI binding (pi 230029078,
// packages/ai/src/api/cloudflare-gateway-binding.ts).
//
// pi's Cloudflare AI Gateway support speaks HTTPS
// (`gateway.ai.cloudflare.com/v1/{account}/{gateway}/{provider}/...`, see
// cloudflare.go), which needs a Cloudflare API token even when the caller is a
// Worker in the gateway's own account.
//
// NewGatewayBindingDoer returns an [ai.HTTPDoer] that translates requests under
// a gateway HTTPS prefix into calls to the Workers AI binding's universal
// endpoint, `env.AI.gateway(id).run({provider, endpoint, headers, query})`.
// Binding calls are pre-authenticated in-account and return the provider's
// native wire format as a regular (streaming) response, so API implementations
// behave identically over either transport.
//
// The result is the transport for one gateway-bound client, not a
// general-purpose doer: requests it cannot serve — URLs outside the prefix, or
// in-prefix requests the universal endpoint cannot express (non-POST, non-JSON
// body) — fail with a descriptive error. Transport selection is the caller's
// job, per client: route such traffic over HTTPS with real gateway auth
// instead of through this shim.
//
// This surface is LATENT in the Go port: an [AIGatewayBinding] can only come
// from a Cloudflare Workers runtime, which Go does not target, so nothing in
// the port supplies one today. It is ported because it is public SDK surface
// under pi-ai's "./api/*" subpath export — the same reason StreamOptions.Env
// and ProviderRequestOptions.TelemetryContext are ported latent. See the
// 2026-08-11 ruling in docs/UPSTREAM.md.
//
// pi's fetch-input polymorphism has no counterpart here. Its
// createGatewayBindingFetch must reconcile two overlapping sources of truth
// (a Request input and a RequestInit override) across six body types and four
// header shapes, with fetch-spec rules for `body: null` and `signal: null`.
// An *http.Request has already normalized all of that, so the port keeps the
// observable behavior and drops the reconciliation.
package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/sky-valley/pi/ai"
)

// AIGatewayBinding is the Workers AI binding's gateway surface (`env.AI`). pi
// declares it structurally so the module needs no @cloudflare/workers-types
// dependency; the Go equivalent of that is an interface a host satisfies.
type AIGatewayBinding interface {
	Gateway(id string) AIGatewayBindingGateway
}

// AIGatewayBindingGateway issues universal-endpoint requests for one gateway.
//
// pi passes an AbortSignal in a trailing options bag; cancellation here travels
// on the context, as everywhere else in the port.
type AIGatewayBindingGateway interface {
	Run(ctx context.Context, req AIGatewayUniversalRequest) (*http.Response, error)
}

// AIGatewayUniversalRequest is one universal-endpoint request entry, as accepted
// by `AiGateway.run()`.
type AIGatewayUniversalRequest struct {
	Provider string
	Endpoint string
	Headers  map[string]string
	// Query is the request body, decoded from JSON. pi types it `unknown` and
	// passes whatever JSON.parse returned, so any JSON value can appear here.
	Query any
}

// CloudflareGatewayBindingAuthSentinel is the placeholder value for auth headers
// on binding-routed requests. API implementations require an API key or a
// recognized auth header (`authorization`, `x-api-key`, `cf-aig-authorization`)
// before dispatch; binding calls are pre-authenticated, so pass
// `cf-aig-authorization: Bearer ` + CloudflareGatewayBindingAuthSentinel to
// satisfy the check. The shim strips `cf-aig-authorization` before calling the
// binding. Pair it with nil-valued `Authorization` / `x-api-key` entries in
// [ai.ProviderHeaders] so the SDKs' placeholder auth headers never reach the
// gateway, which would treat a request-supplied auth header as a BYOK provider
// key that overrides its stored keys — the same as it would over HTTPS.
const CloudflareGatewayBindingAuthSentinel = "cloudflare-gateway-binding"

// GatewayBindingDoerOptions configures [NewGatewayBindingDoer].
type GatewayBindingDoerOptions struct {
	// Binding is the Workers AI binding (e.g. `env.AI`).
	Binding AIGatewayBinding
	// BaseURL is the gateway HTTPS prefix every request must fall under,
	// without a trailing slash:
	// `https://gateway.ai.cloudflare.com/v1/{accountId}/{gatewayName}`.
	BaseURL string
	// Gateway is the gateway name passed to Binding.Gateway. It must match the
	// BaseURL gateway.
	Gateway string
}

// stripHeaders are never forwarded to the binding: hop-by-hop/derived headers,
// and gateway auth (binding calls are pre-authenticated; the sentinel must not
// reach the wire).
var stripHeaders = map[string]bool{"content-length": true, "host": true, "cf-aig-authorization": true}

// gatewayBindingDoer routes AI Gateway requests through the Workers AI binding.
type gatewayBindingDoer struct {
	binding  AIGatewayBinding
	gateway  string
	origin   string
	basePath string
}

// NewGatewayBindingDoer creates an [ai.HTTPDoer] that routes AI Gateway requests
// through the Workers AI binding. See the package-level docs for behavior and
// composition notes.
//
// pi's constructor throws on an unparseable baseUrl; this returns the error.
func NewGatewayBindingDoer(opts GatewayBindingDoerOptions) (ai.HTTPDoer, error) {
	if opts.Binding == nil {
		return nil, errors.New("NewGatewayBindingDoer: Binding is required; pass the Workers AI binding (env.AI)")
	}
	base, err := url.Parse(opts.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("NewGatewayBindingDoer: BaseURL %q is not a valid URL: %w", opts.BaseURL, err)
	}
	if base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("NewGatewayBindingDoer: BaseURL %q must be absolute, like "+
			"https://gateway.ai.cloudflare.com/v1/{accountId}/{gatewayName}", opts.BaseURL)
	}
	// Prefix matching runs on URL-normalized components (origin + path), not raw
	// strings: dot segments resolve away and fragments drop, matching what a real
	// request would put on the wire, so a lexical variant cannot split
	// provider/endpoint differently than HTTPS would.
	basePath := normalizeURLPath(base.Path)
	if !strings.HasSuffix(basePath, "/") {
		basePath += "/"
	}
	return &gatewayBindingDoer{
		binding:  opts.Binding,
		gateway:  opts.Gateway,
		origin:   base.Scheme + "://" + base.Host,
		basePath: basePath,
	}, nil
}

// normalizeURLPath resolves dot segments the way the WHATWG URL parser does,
// preserving a trailing slash (path.Clean drops it, and whether the path ends in
// a slash decides "missing provider/endpoint" versus "outside the prefix").
func normalizeURLPath(p string) string {
	if p == "" {
		return "/"
	}
	cleaned := path.Clean(p)
	if strings.HasSuffix(p, "/") && !strings.HasSuffix(cleaned, "/") {
		cleaned += "/"
	}
	return cleaned
}

func (d *gatewayBindingDoer) Do(req *http.Request) (*http.Response, error) {
	// pi normalizes `init.method ?? request.method ?? "GET"` and uppercases it;
	// an empty Method is GET here for the same reason.
	method := strings.ToUpper(req.Method)
	if method == "" {
		method = http.MethodGet
	}
	rawURL := ""
	if req.URL != nil {
		rawURL = req.URL.String()
	}

	if req.URL == nil || req.URL.Scheme == "" || req.URL.Host == "" ||
		req.URL.Scheme+"://"+req.URL.Host != d.origin ||
		!strings.HasPrefix(normalizeURLPath(req.URL.Path), d.basePath) {
		return nil, d.outsidePrefix(method, rawURL)
	}
	if method != http.MethodPost {
		return nil, unexpressibleGatewayRequest(method, rawURL, "only POST is supported")
	}

	rest := strings.TrimPrefix(normalizeURLPath(req.URL.Path), d.basePath)
	slash := strings.Index(rest, "/")
	if slash <= 0 {
		return nil, unexpressibleGatewayRequest(method, rawURL, "missing provider/endpoint path")
	}
	provider := rest[:slash]
	// Keep the query string on the endpoint — it is part of what HTTPS would send.
	endpoint := rest[slash+1:]
	if req.URL.RawQuery != "" {
		endpoint += "?" + req.URL.RawQuery
	}

	// An absent body is "missing body"; a present-but-unparseable one (including
	// empty, which JSON.parse also rejects) is "non-JSON body".
	if req.Body == nil || req.Body == http.NoBody {
		return nil, unexpressibleGatewayRequest(method, rawURL, "missing body")
	}
	// Consuming a one-shot body here is fine — unexpressible requests reject
	// rather than replay, so nothing downstream needs it again.
	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, unexpressibleGatewayRequest(method, rawURL, "unreadable body: "+err.Error())
	}
	var query any
	if err := json.Unmarshal(bodyBytes, &query); err != nil {
		return nil, unexpressibleGatewayRequest(method, rawURL, "non-JSON body")
	}

	return d.binding.Gateway(d.gateway).Run(req.Context(), AIGatewayUniversalRequest{
		Provider: provider,
		Endpoint: endpoint,
		Headers:  collectGatewayHeaders(req.Header),
		Query:    query,
	})
}

// outsidePrefix mirrors pi's out-of-prefix throw. Out-of-prefix URLs are a
// configuration bug, not passthrough traffic: silently forwarding would ship the
// auth sentinel to whatever host the URL names.
func (d *gatewayBindingDoer) outsidePrefix(method, rawURL string) error {
	return fmt.Errorf("NewGatewayBindingDoer: %s %s is outside the configured gateway prefix (%s%s); "+
		"this doer only serves its gateway-bound client", method, rawURL, d.origin, d.basePath)
}

// unexpressible mirrors pi's unexpressible throw. In-prefix requests the
// universal endpoint cannot express always reject: forwarding them over HTTPS
// would send the sentinel to the gateway and fail with a misleading auth error
// instead of naming the real problem. Callers that need such endpoints route
// them over HTTPS with real gateway auth themselves.
func unexpressibleGatewayRequest(method, rawURL, reason string) error {
	return fmt.Errorf("NewGatewayBindingDoer: cannot express %s %s as a universal gateway request (%s); "+
		"route it over HTTPS with gateway auth instead", method, rawURL, reason)
}

// collectGatewayHeaders lowercases entry header names so case-variant duplicates
// collapse and stripping is uniform. Multi-value headers join the way a fetch
// Headers iterator yields them.
func collectGatewayHeaders(h http.Header) map[string]string {
	result := make(map[string]string, len(h))
	for key, values := range h {
		name := strings.ToLower(key)
		if stripHeaders[name] {
			continue
		}
		result[name] = strings.Join(values, ", ")
	}
	return result
}
