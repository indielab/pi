package providers

import (
	"net/http"

	"github.com/sky-valley/pi/ai"
)

// Header plumbing for ai.ProviderHeaders, whose nil values are deletion markers
// (see ai.ProviderHeaders). pi applies those markers two different ways, and
// the difference is observable, so the port keeps both:
//
//   - Adapters that hand the merged headers to a vendor SDK as `defaultHeaders`
//     (openai-completions, openai-responses, anthropic-messages) let the SDK
//     delete on null, which also removes the auth header the SDK itself would
//     have sent. applyProviderHeaders is that path.
//   - Adapters that build the request themselves (google-generative-ai,
//     pi-messages) run the merged headers through providerHeadersToRecord
//     (pi utils/headers.ts), which drops null entries. A null there means "not
//     sent", and it cannot remove a header the adapter set literally.

// providerHeadersToRecord drops deletion markers, mirroring pi's
// providerHeadersToRecord (utils/headers.ts). Callers merge every
// ProviderHeaders source first and convert once, because a marker must be able
// to cancel a value an earlier source supplied.
func providerHeadersToRecord(headers ai.ProviderHeaders) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	record := make(map[string]string, len(headers))
	for name, value := range headers {
		if value != nil {
			record[name] = *value
		}
	}
	return record
}

// mergeProviderHeaders folds sources left to right the way pi spreads them into
// one object (`{...a, ...b}`): a later source overrides an earlier one by name,
// and a nil value overrides just as a string does — that is what lets a
// consumer's marker cancel a model or attribution header.
func mergeProviderHeaders(sources ...ai.ProviderHeaders) ai.ProviderHeaders {
	var merged ai.ProviderHeaders
	for _, source := range sources {
		for name, value := range source {
			if merged == nil {
				merged = ai.ProviderHeaders{}
			}
			merged[name] = value
		}
	}
	return merged
}

// applyProviderHeaders writes headers onto h, deleting the header a marker
// names instead of setting it. It is the Go stand-in for passing the merged
// headers to an SDK as `defaultHeaders`, where a null value removes the header
// the SDK would otherwise send — including its own auth header. Header names
// are canonicalized by net/http, so the delete matches case-insensitively, as
// it does on the wire.
func applyProviderHeaders(h http.Header, headers ai.ProviderHeaders) {
	for name, value := range headers {
		if value == nil {
			h.Del(name)
			continue
		}
		h.Set(name, *value)
	}
}

// stringHeaders lifts adapter-owned string headers (copilot dynamic headers,
// attribution defaults) into ProviderHeaders so they can take part in a merge
// that carries markers.
func stringHeaders(headers map[string]string) ai.ProviderHeaders {
	if len(headers) == 0 {
		return nil
	}
	lifted := make(ai.ProviderHeaders, len(headers))
	for name := range headers {
		value := headers[name]
		lifted[name] = &value
	}
	return lifted
}
