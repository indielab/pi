// Package delta synchronizes JSON values from an authoritative producer to an
// ordered replica. It mirrors @earendil-works/chord/delta
// (packages/chord/src/delta at 9a139c62b) and depends on nothing else in the
// port: session storage, the runtime and the facet host consume it, and the
// arrows point that way.
//
// A change is an Op: a JSON tuple for replacing, setting, deleting, updating a
// string, splicing an array, or permuting an array. A producer keeps its value
// in a Tracker as immutable revisions and publishes the ops between one
// revision and the next; a replica applies them.
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
//	["m", path, permutation]            Reorder an array so new[i] = old[permutation[i]].
//
// Except for "r", every decoded op carries its complete path. "s", "d", "a"
// and "t" cannot address the root; "p" and "m" may address a root array.
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
//	["m", pathRef, permutation]            ["m", permutation]
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
// A Tracker holds one committed revision. A change is a transaction against
// it:
//
//	change, err := tracker.BeginChange()
//	state := change.State()                      // the root draft
//	err = state.Set("status", "running")
//	err = state.At("rows").Push(row)
//	prepared, err := change.Prepare()            // or change.Abort()
//	err = tracker.Adopt(prepared)                // commit
//	publish(prepared.Ops())
//
// Nothing is recorded while the draft is mutated. Prepare diffs the draft's
// result against the committed revision (DiffRevisions), so the batch depends
// only on the two revisions, never on the order or number of writes: three
// writes to one property publish as one set, and writes that cancel out
// publish nothing and leave the committed revision's identity intact. Adopt
// commits exactly what was prepared, so a runtime can persist the batch before
// it adopts, or drop it. PrepareReplace prepares a whole new value the same
// way.
//
// A tracker has at most one open change. Adopt refuses a prepared change from
// another tracker, one already adopted or aborted, and one prepared against a
// revision that is no longer committed.
//
// Strings publish as appends and front-truncations where they can; arrays as
// splices and permutations anchored on the elements the two revisions share by
// identity, then by value; objects as sets and deletes of the members that
// differ. A batch of more than 4,096 ops, or a large one costing more than the
// value itself, is published as one Replace. The op sequence is not canonical:
// depend on the resulting value, never on the exact tuples.
//
// # Drafts
//
// Upstream's draft is a Proxy: plain JavaScript reads, writes and array method
// calls on a copy-on-write view. Go has no Proxy, so a *Draft is the handler
// the Proxy would call, and each trap is a method:
//
//	draft.key                        d.Get(key)  (value, present); a member object or array is a *Draft
//	draft.key (an object or array)   d.At(key)   the member's draft, or nil
//	draft.key = value                d.Set(key, value)
//	delete draft.key                 d.Delete(key)
//	key in draft                     d.Has(key)
//	Object.keys(draft)               d.Keys()
//	draft.length                     d.Len(); on an array, Get("length") too
//	draft.length = n                 d.SetLen(n); on an array, Set("length", v) too, v converted as JavaScript does
//	draft.push(...items)             d.Push(items...)
//	draft.pop() / draft.shift()      d.Pop() / d.Shift()   (value, present, err)
//	draft.unshift(...items)          d.Unshift(items...)
//	draft.splice(start, n, ...items) d.Splice(start, n, items...)
//	draft.sort(compare)              d.Sort(compare); nil is JavaScript's default string order
//	draft.reverse()                  d.Reverse()
//	draft.fill(v, start, end)        d.Fill(v, start, end)
//	draft.copyWithin(t, start, end)  d.CopyWithin(t, start, end)
//
// A draft belongs to a container, not a position: reading a member twice
// yields the same *Draft, and a held draft follows its container through
// sorting, reversal and insertion — or out of the tree, after which writes
// through it are dropped. Every value written is checked and deep-copied at
// once; assigning a *Draft copies what that draft holds now. A draft is valid
// until its change is prepared or aborted: after that a write returns
// ErrDraftRevoked, and a read — which has no error to return — panics with it.
//
// Where upstream throws a TypeError from a trap, the Go method returns an
// error carrying upstream's text: deleting an array element, assigning a
// value with no JSON form, or a cycle. An array grown with SetLen or written
// past its end holds holes until they are filled, and Prepare fails if any
// remain, as upstream's does; so does a named (non-index) property set on an
// array, which JavaScript lets a draft array hold and never lets it drop.
//
// # Revisions
//
// Every committed revision — Tracker.Value, and the Base and Value of each
// Prepared — is immutable and shares its unchanged subtrees with the revisions
// around it; the ops' payloads share them too. Upstream freezes them; in Go
// they are immutable by contract, so never modify one. Track and PrepareReplace
// import a deep copy of what they are handed: every number becomes a float64
// (JavaScript's one number type), a nil map or slice an empty one, and an
// object or array reachable twice becomes two independent values.
//
// Object identity is not replicated: a replica holds a distinct value at each
// path. Nor is object key order. JavaScript enumerates an object's keys in
// insertion order, a Go map in none, so the port diffs and marshals an object's
// members in the order of an object whose keys were inserted sorted —
// integer-like keys ascending, then the rest by UTF-16 code unit — and a batch
// can list an object's member ops in a different order than pi would for the
// same change. The replica is the same either way.
//
// # Paths and safety
//
// Path segments are Key and Index. Three keys are reserved as segments —
// ReservedSegments — because a replica in pi applies parent[key] = value, and
// a path is data: a Go producer emitting ["s", ["__proto__", "isAdmin"], true]
// hands a TypeScript replica a prototype-pollution primitive. The diff never
// emits one — an object holding a reserved key is published whole, by its
// parent's path — and wire validators and appliers refuse them. As VALUE keys
// they are fine; a value is written whole and never walked.
//
// # Streams
//
// An encoder and decoder are stateful. Use one pair per ordered stream: the
// encoder assigns ids to paths used across batches, the decoder remembers the
// definitions, and a Replace resets both dictionaries so replay can begin at
// that batch with a fresh decoder. A producer starts a stream — or starts it
// over — with a base batch: []Op{Replace{Value: tracker.Value()}}. A decode or
// apply error terminates the stream; discard its decoder and replica and
// recover from a later base batch.
//
// # JSON in Go
//
// A value is the tree encoding/json produces: nil, bool, float64, string,
// []any, map[string]any. Validators do not inspect payloads; they check the
// tuple's shape and the path. Every op marshals to its tuple; ParseOp and
// ParseWireOp classify an any-tree, and UnmarshalOp and UnmarshalWireOp do the
// same from bytes.
//
// A Tracker, its Changes and their drafts, an Encoder and a Decoder each
// belong to one goroutine at a time; none is safe for concurrent use.
package delta
