package providers

import (
	"net/http"
	"sort"

	"github.com/sky-valley/pi/ai"
)

// Header plumbing for ai.ProviderHeaders, whose nil values are deletion markers
// (see ai.ProviderHeaders). pi applies those markers two different ways, and
// the difference is observable, so the port keeps both:
//
//   - Adapters that hand the merged headers to a vendor SDK as `defaultHeaders`
//     (openai-completions, openai-responses, anthropic-messages) let the SDK
//     delete on null, which also removes the auth header the SDK itself would
//     have sent. headerObject.applyAsDefaultHeaders is that path.
//   - Adapters that build the request themselves (google-generative-ai,
//     pi-messages) run the merged headers through providerHeadersToRecord
//     (pi utils/headers.ts), which drops null entries. A null there means "not
//     sent", and it cannot remove a header the adapter set literally.
//     headerObject.applyAsRecord is that path.
//
// Two divergences live in here and are recorded in docs/UPSTREAM.md rather than
// papered over: an empty-string value is dropped entirely by net/http on the
// User-Agent header where pi sends it present-and-empty, and @google/genai
// transmits case-variant names comma-joined where this package transmits the
// winner alone.

// headerObject models the ONE header object pi builds per provider request.
//
// It is an ordered map because pi's is one. A JS object keeps its string keys
// in insertion order, and assigning to a key it already holds updates that key
// IN PLACE instead of moving it to the end — so a later source that re-spells a
// name an earlier source already inserted does NOT get promoted past the names
// inserted in between. Two names differing only by case are two distinct keys
// there but one header on the wire, and both the vendor SDKs and net/http
// resolve that collision by taking the LAST of them; which one is last is
// exactly the slot information an http.Header cannot hold.
//
// Folding every source into this before touching the request is what keeps the
// two agreeing. Concretely, on the anthropic OAuth branch pi's object holds
// "User-Agent" (the seeded pi default) at slot 0 and "user-agent"
// (claude-cli/<v>) at a later slot, so a caller spelling the name "User-Agent"
// lands back in slot 0 and loses to the Claude Code identity — while a caller
// spelling it "user-agent" or "USER-AGENT" wins. Writing straight to an
// http.Header made the later WRITE win regardless of spelling, which is not
// what pi does.
type headerObject struct {
	// names in insertion order; every name is a key of values.
	names []string
	// values by exact (non-canonicalized) name; nil is a deletion marker.
	values map[string]*string
}

// set records an adapter-owned literal header, the way pi writes one into the
// object literal it spreads its sources into.
func (o *headerObject) set(name, value string) {
	o.setValue(name, &value)
}

// setValue records name at its existing slot, or appends a new slot for a name
// the object does not hold yet — JS assignment semantics.
func (o *headerObject) setValue(name string, value *string) {
	if o.values == nil {
		o.values = make(map[string]*string, 8)
	}
	if _, held := o.values[name]; !held {
		o.names = append(o.names, name)
	}
	o.values[name] = value
}

// merge folds one ProviderHeaders source in, the way pi spreads one into the
// object (`{...merged, ...source}`): the source overrides an earlier value by
// name, and a nil overrides just as a string does — that is what lets a
// consumer's marker cancel a model or attribution header.
//
// Across sources the order is the caller's call order, which is pi's spread
// order exactly. WITHIN one source it is sorted name order, because a Go map
// has no key order to reproduce: that is the standing tie-break for this
// divergence class (2026-08-04 ruling), shared with mergeHeaders in
// ai/models_runtime.go, and it now only decides ties between two spellings
// inside a single ai.ProviderHeaders literal.
func (o *headerObject) merge(source ai.ProviderHeaders) {
	for _, name := range sortedNames(source) {
		o.setValue(name, source[name])
	}
}

// mergeStrings folds in an adapter-owned bundle that carries no markers
// (attribution defaults, copilot dynamic headers). Same ordering rules as
// merge.
func (o *headerObject) mergeStrings(source map[string]string) {
	for _, name := range sortedNames(source) {
		value := source[name]
		o.setValue(name, &value)
	}
}

// applyAsDefaultHeaders writes the object onto h with a marker DELETING the
// header it names. It is the Go stand-in for passing the merged headers to an
// SDK as `defaultHeaders`, where a null value removes the header the SDK would
// otherwise send — including its own auth header. Header names are
// canonicalized by net/http, so the delete matches case-insensitively, as it
// does on the wire, and slot order decides a case collision the way the SDKs'
// own buildHeaders does (later slot wins; a marker in the last slot deletes).
func (o *headerObject) applyAsDefaultHeaders(h http.Header) {
	for _, name := range o.names {
		if value := o.values[name]; value != nil {
			h.Set(name, *value)
		} else {
			h.Del(name)
		}
	}
}

// applyAsRecord writes the object onto h with a marker DROPPED rather than
// deleting, mirroring pi's providerHeadersToRecord (utils/headers.ts) for the
// adapters that build the request themselves. A null means "this entry is not
// sent"; it cannot remove a header the adapter wrote literally, and it cancels
// an earlier source's value only because the merge already overwrote it.
func (o *headerObject) applyAsRecord(h http.Header) {
	for _, name := range o.names {
		if value := o.values[name]; value != nil {
			h.Set(name, *value)
		}
	}
}

// sortedNames returns m's keys in sorted order, the tie-break every source-local
// header ordering in this package uses.
func sortedNames[V any](m map[string]V) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
