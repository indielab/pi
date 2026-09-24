package providers

import (
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

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
// Before it normalizes anything, a Headers object converts each name and value
// it is given to a WebIDL ByteString (see byteString): a character above U+00FF
// fails the request before it is sent, and U+0080..U+00FF go out as one Latin-1
// byte each, where net/http would send the UTF-8 bytes of any string. Every
// name and value is converted here, in the order pi's Headers sees them, so the
// same input fails with the same message and is sent as the same bytes.
//
// Two divergences live in here and are recorded in docs/UPSTREAM.md rather than
// papered over: an empty or whitespace-only User-Agent value is dropped
// entirely where pi sends the header present-and-empty (setHeader normalizes a
// whitespace-only value to empty, and net/http omits an empty User-Agent), and
// @google/genai comma-joins a record entry onto its own User-Agent or
// x-goog-api-client default when the entry spells the name differently, where
// this package — which sends neither default — sends the record's value alone.

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
	// sdkAuth holds the values setSDKAuth recorded, in call order.
	sdkAuth []string
}

// set records an adapter-owned literal header, the way pi writes one into the
// object literal it spreads its sources into.
func (o *headerObject) set(name, value string) {
	o.setValue(name, &value)
}

// setSDKAuth records the auth header a vendor SDK builds from its own client
// options (`apiKey`, `authToken`) rather than from `defaultHeaders`. It takes a
// slot as set does, so a later source overrides it on the wire or deletes it
// with a marker, as the SDK's buildHeaders lets `defaultHeaders` do. But the
// SDK appends its auth header to its Headers before any `defaultHeaders`
// entry, so the value is converted even when a later slot spelled exactly like
// it replaces the one it holds here: applyAsDefaultHeaders converts every value
// recorded this way first. Measured against openai 6.40.0 and
// @anthropic-ai/sdk 0.124.0: a key holding a character above U+00FF fails the
// request even under a consumer `authorization` override or marker.
func (o *headerObject) setSDKAuth(name, value string) {
	o.sdkAuth = append(o.sdkAuth, value)
	o.set(name, value)
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
//
// It fails where the SDK's buildHeaders throws, before anything is sent: on
// the SDK's own auth value first (see setSDKAuth), then slot by slot, on the
// name (buildHeaders deletes a name from its Headers before it first appends
// one, and a marker is that delete alone) and then on the value.
func (o *headerObject) applyAsDefaultHeaders(h http.Header) error {
	for _, value := range o.sdkAuth {
		if _, err := headerValue(value); err != nil {
			return err
		}
	}
	for _, name := range o.names {
		if err := checkHeaderName(name); err != nil {
			return err
		}
		value := o.values[name]
		if value == nil {
			h.Del(name)
			continue
		}
		if err := setHeader(h, name, *value); err != nil {
			return err
		}
	}
	return nil
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
// A name that reaches the wire is a Latin-1 ByteString (see byteString), and on
// those strings.ToLower agrees with JavaScript's toLowerCase.
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
// remove a literal. Nor does it touch a header the adapter sets on h after
// calling this (google's x-goog-api-key, which genai appends only when the
// Headers holds none yet).
//
// The Headers pi builds converts each entry's name and value to ByteStrings
// (see byteString) in object order, so the first entry that does not convert
// fails the request before it is sent.
func (o *headerObject) applyAsRecord(h http.Header, literals ...recordEntry) error {
	appended := make(map[string]bool, len(literals))
	for _, e := range o.spread(literals) {
		if err := checkHeaderName(e.name); err != nil {
			return err
		}
		value, err := headerValue(e.value)
		if err != nil {
			return err
		}
		key := http.CanonicalHeaderKey(e.name)
		if appended[key] {
			// Headers.append normalizes the value it appends and joins it onto
			// the one already held with ", ". An empty second value leaves the
			// separator's space at the end: genai re-normalizes the joined value
			// and sends none, and pi-messages' HTTP/1.1 wire carries it as
			// whitespace outside the field value. HTTP/2 forbids a value ending
			// in whitespace, so the joined value is normalized as genai's is.
			h[key] = []string{trimHTTPWhitespace(h[key][0] + ", " + value)}
			continue
		}
		h.Set(e.name, value)
		appended[key] = true
	}
	return nil
}

// spread is the plain object pi hands to its Headers: the literals, with the
// record spread over them. A record entry whose name matches a literal exactly
// replaces its value in place; any other entry is appended.
func (o *headerObject) spread(literals []recordEntry) []recordEntry {
	object := slices.Clone(literals)
	for _, e := range o.record() {
		if i := slices.IndexFunc(object, func(l recordEntry) bool { return l.name == e.name }); i >= 0 {
			object[i].value = e.value
		} else {
			object = append(object, e)
		}
	}
	return object
}

// setHeader writes one header value the way a fetch Headers object stores it
// (see headerValue), failing as Headers.append throws. Every header value an
// adapter writes goes through here or through headerValue, because in pi every
// one goes through a Headers object. A whitespace-only User-Agent normalizes to
// empty, which net/http then omits: docs/UPSTREAM.md D11, which pi sends
// present and empty.
func setHeader(h http.Header, name, value string) error {
	wire, err := headerValue(value)
	if err != nil {
		return err
	}
	h.Set(name, wire)
	return nil
}

// headerValue is what a fetch Headers object makes of a value it is given: the
// value converted to a ByteString, whose bytes are what the wire carries, then
// normalized (see trimHTTPWhitespace).
func headerValue(value string) (string, error) {
	wire, err := byteString(value)
	if err != nil {
		return "", err
	}
	return trimHTTPWhitespace(wire), nil
}

// checkHeaderName fails on a header name a fetch Headers object refuses to
// convert (see byteString).
func checkHeaderName(name string) error {
	_, err := byteString(name)
	return err
}

// byteString converts s the way WebIDL's ByteString conversion converts a
// Headers argument, which is the only form a header name or value takes in pi.
// Each UTF-16 code unit becomes one byte: a string of code units up to 0xFF
// returns their bytes, so U+0080..U+00FF become single Latin-1 bytes where the
// Go string holds two UTF-8 bytes. The first code unit above 0xFF fails the
// conversion with the TypeError text node's fetch throws, which pi's adapters
// report as the stream's error message; its index and value are counted in
// UTF-16 code units, so a character outside the BMP reports its high
// surrogate. A Go string is text here, as a JS string is: an invalid UTF-8
// byte reads as U+FFFD, and fails.
func byteString(s string) (string, error) {
	var latin1 []byte // nil until the first byte that is not ASCII
	for i, r := range s {
		if r < utf8.RuneSelf {
			if latin1 != nil {
				latin1 = append(latin1, byte(r))
			}
			continue
		}
		if r > 0xFF {
			// Every character before i was one code unit (at most 0xFF).
			unit := utf8.RuneCountInString(s[:i])
			value := r
			if r > 0xFFFF {
				value, _ = utf16.EncodeRune(r)
			}
			return "", fmt.Errorf("Cannot convert argument to a ByteString because the character at index %d has a value of %d which is greater than 255.", unit, value)
		}
		if latin1 == nil {
			latin1 = append(make([]byte, 0, len(s)), s[:i]...)
		}
		latin1 = append(latin1, byte(r))
	}
	if latin1 == nil {
		return s, nil
	}
	return string(latin1), nil
}

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
