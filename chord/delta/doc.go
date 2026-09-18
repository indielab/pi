// Package delta synchronizes JSON values from an authoritative producer to an
// ordered replica. It mirrors @earendil-works/chord/delta
// (packages/chord/src/delta at 64eeb82a4) and depends on nothing else in the
// port: session storage, the runtime and the facet host consume it, and the
// arrows point that way.
//
// A change is an Op: a JSON tuple for replacing, setting, deleting, updating a
// string, or splicing an array. Producers use a tracker; replicas apply the
// batches it flushes. The first flush is one Replace carrying the complete
// value; each later flush is the ops that transform the previously published
// value into the current one, or nothing when it has not changed.
//
// # Operation vocabulary
//
// A path is an array of object keys and array indices, root first:
//
//	["operation", "message", "content", 0, "text"]
//
// Decoded ops (Op) carry complete inline paths:
//
//	["r", value]                        Replace the complete value.
//	["s", path, value]                  Set a property or array element.
//	["d", path]                         Delete an object property.
//	["a", path, text]                   Append to a string.
//	["t", path, count]                  Remove UTF-16 code units from a string's front.
//	["p", path, index, remove, items]   Splice an array.
//
// Except for "r", every decoded op carries its complete path. "s", "d", "a"
// and "t" cannot address the root; "p" may address a root array.
//
// Wire ops (WireOp) add path interning and omission, and nothing else:
//
//	["r", value]                           Identical to the decoded form.
//	["#", id, path]                        Define a numeric path id.
//	["s", pathRef, value]  ["s", value]    Inline or interned path; or the previous op's path.
//	["d", pathRef]         ["d"]
//	["a", pathRef, text]   ["a", text]
//	["t", pathRef, count]  ["t", count]
//	["p", pathRef, index, remove, items]   ["p", index, remove, items]
//
// The vocabulary is deliberately not RFC 6902. String append and front-truncate
// let a rolling output window ship as two small ops instead of a whole-value
// set; splice carries its items; a whole-value "r" doubles as the recovery
// point a reader replays from; and path interning is stateful across batches.
//
// Encoding is optional for local application. Never pass wire ops to an
// applier: the two grammars overlap, and a two-element ["s", value] read as a
// decoded op has the value as its path. Op and WireOp are distinct types with
// distinct validators (ParseOp, ParseWireOp) for exactly that reason.
//
// # Producing changes
//
// Read and mutate the tracked value through a State cursor: Set, Delete and
// the array methods. Each mutation records an op, and ops are coalesced within
// a flush window where that is cheap and safe — three writes to one property
// publish as one set.
//
// The op sequence is not canonical. Equivalent changes may use different
// verbs, and mutations that cancel out can still produce a nonempty batch: a
// flush guarantees convergence, not a minimal diff. Depend on the resulting
// value, never on the exact tuples or their minimality. Any nonempty batch
// advances a replicated-state sequence number and notifies subscribers, even
// when applying it leaves the value deeply equal to the previous revision.
//
// Replacing a whole object or array is valid and cheap: the outgoing value is
// diffed against the incoming one at assignment time, so unchanged members
// produce no ops and changed nested strings and arrays still ship as append,
// truncate and splice. Do not assign large intermediate values repeatedly
// before one flush — each assignment is diffed immediately.
//
// Sorting, reversing, Fill and CopyWithin have no op of their own and publish
// a snapshot of the array they permuted; cancelling one still publishes.
// Front or middle insertion and removal are recorded directly, and edits
// before and after an index-changing op stay ordered against the array
// generation they addressed. A sufficiently long mutation window collapses to
// a complete base batch automatically, which bounds accumulated log metadata
// — not payload bytes or peak allocation — and trades one full snapshot for
// an additional recovery point.
//
// The value handed to Track is tracker-owned, as is anything later assigned
// into it or inserted into one of its arrays. Mutating such a value outside
// the cursor bypasses tracking and silently diverges from the replica. Object
// identity is not replicated: one container reachable at several paths is
// published at each, and a replica holds a distinct value at each.
//
// # Paths and safety
//
// Path segments are Key and Index. Three keys are reserved as segments —
// ReservedSegments — because a replica in pi applies parent[key] = value, and
// a path is data: a Go producer emitting ["s", ["__proto__", "isAdmin"], true]
// hands a TypeScript replica a prototype-pollution primitive. Producers, wire
// validators and appliers all refuse them. As VALUE keys they are fine; a
// value is written whole and never walked.
//
// # Streams
//
// An encoder and decoder are stateful. Use one pair per ordered stream: the
// encoder assigns ids to paths used across batches, the decoder remembers the
// definitions, and a Replace resets both dictionaries so replay can begin at
// that batch with a fresh decoder. A decode or apply error terminates the
// stream; discard its decoder and replica and recover from a later base batch.
//
// # JSON in Go
//
// A value is the tree encoding/json produces: nil, bool, float64, string,
// []any, map[string]any. Validators do not inspect payloads; they check the
// tuple's shape and the path. Every op marshals to its tuple; ParseOp and
// ParseWireOp classify an any-tree, and UnmarshalOp and UnmarshalWireOp do the
// same from bytes.
package delta
