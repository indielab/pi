package providers

import (
	"net/http"
	"slices"
	"sort"
	"strings"

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
//     (pi utils/headers.ts), which folds names case-insensitively: the last
//     slot for a name wins, and a null there deletes every earlier spelling
//     of it from the record (upstream a328aa89a). The record is then spread
//     over the adapter's own literal headers, so a null still cannot remove a
//     header the adapter set literally. headerObject.applyAsRecord is that
//     path.
//
// Both paths end in a fetch Headers object in pi — the SDK's, or the one fetch
// builds from pi-messages' plain object — and Headers.append/set normalize
// every value by stripping its leading and trailing HTTP whitespace. net/http
// does not: it fails the request on a CR or LF anywhere in a value, and its
// HTTP/2 encoder sends a value's edges verbatim. So every value is normalized
// here as it is written (setHeader), which makes a key or token read with a
// trailing newline work as it does in pi instead of failing the stream.
//
// Two divergences live in here and are recorded in docs/UPSTREAM.md rather than
// papered over: an empty-string value is dropped entirely by net/http on the
// User-Agent header where pi sends it present-and-empty, and @google/genai
// comma-joins a record entry onto its own User-Agent or x-goog-api-client
// default when the entry spells the name differently, where this package —
// which sends neither default — sends the record's value alone.

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
			setHeader(h, name, *value)
		} else {
			h.Del(name)
		}
	}
}

// recordEntry is one header of pi's providerHeadersToRecord result: the
// spelling that survived for its case-folded name, and its value.
type recordEntry struct{ name, value string }

// record folds the object the way pi's providerHeadersToRecord does since
// upstream a328aa89a. Names fold case-insensitively in slot order: each slot
// deletes whatever an earlier slot left under its folded name, then stores its
// own spelling and value — unless its value is a marker, which leaves the
// folded name deleted. So the LAST slot for a name decides it, a marker there
// removes every earlier spelling instead of merely being skipped, and a
// re-stored name moves to the end, as a JS Map's delete-then-set does.
//
// Header names are ASCII tokens, so strings.ToLower agrees with JavaScript's
// toLowerCase on every name a request can carry.
func (o *headerObject) record() []recordEntry {
	var out []recordEntry
	for _, name := range o.names {
		folded := strings.ToLower(name)
		out = slices.DeleteFunc(out, func(e recordEntry) bool { return strings.ToLower(e.name) == folded })
		if value := o.values[name]; value != nil {
			out = append(out, recordEntry{name: name, value: *value})
		}
	}
	return out
}

// applyAsRecord writes pi's providerHeadersToRecord result onto h, for the
// adapters that build the request themselves. The record is folded before it
// reaches h (see record), so a marker cancels every spelling an earlier slot
// gave its name.
//
// pi spreads the record over the adapter's literal headers — pi-messages'
// `{authorization, accept, "content-type", ...record}`, or @google/genai's own
// defaults — and the transport builds its Headers from that plain object with
// Headers.append. literals are those headers, spelled exactly as pi's object
// spells them, and this replays both steps: a record entry whose name matches
// a literal EXACTLY replaces its value in place (JS spread), while one that
// matches only case-insensitively is a second key there, and Headers.append
// joins the two with ", " in slot order — literal first. Measured with node
// fetch: opts {"Authorization": "x"} on pi-messages sends
// `authorization: Bearer <key>, x`, where {"authorization": "x"} sends `x`.
//
// It never deletes from h: a marker is not part of the record, so it cannot
// remove a literal, nor a header the adapter set on h before calling this
// (google's x-goog-api-key, which genai adds only when the record lacks one —
// a record entry of any spelling replaces it here, as it does there).
func (o *headerObject) applyAsRecord(h http.Header, literals ...recordEntry) {
	object := slices.Clone(literals)
	for _, e := range o.record() {
		if i := slices.IndexFunc(object, func(l recordEntry) bool { return l.name == e.name }); i >= 0 {
			object[i].value = e.value
		} else {
			object = append(object, e)
		}
	}
	appended := make(map[string]bool, len(object))
	for _, e := range object {
		key := http.CanonicalHeaderKey(e.name)
		if appended[key] {
			// Headers.append normalizes the value it appends and joins it onto
			// the one already held with ", ". An empty second value leaves the
			// separator's space at the end: genai re-normalizes the joined value
			// and sends none, and pi-messages' HTTP/1.1 wire carries it as
			// whitespace outside the field value. HTTP/2 forbids a value ending
			// in whitespace, so the joined value is normalized as genai's is.
			h[key] = []string{trimHTTPWhitespace(h[key][0] + ", " + trimHTTPWhitespace(e.value))}
			continue
		}
		setHeader(h, e.name, e.value)
		appended[key] = true
	}
}

// setHeader writes one header value the way a fetch Headers object stores it:
// normalized (see trimHTTPWhitespace). Every header value an adapter writes
// goes through here, because in pi every one goes through a Headers object.
func setHeader(h http.Header, name, value string) { h.Set(name, trimHTTPWhitespace(value)) }

// trimHTTPWhitespace strips what the Fetch standard calls HTTP whitespace —
// space, tab, CR and LF — from both ends, as Headers.append and Headers.set do
// to a value (the Fetch standard's "normalize"). A CR or LF left inside a value
// still fails the request, as Headers.append throws on one.
func trimHTTPWhitespace(v string) string { return strings.Trim(v, " \t\r\n") }

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
