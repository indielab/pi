package delta

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"unicode/utf8"
)

// ─── Immutable JSON revisions ────────────────────────────────────────────────
//
// Upstream's delta/value.ts: a JsonRevisionStore that owns and freezes the
// revisions a tracker publishes and reuses already-owned subtrees. Go has no
// Object.freeze, so a revision is immutable by contract — Tracker.Value,
// Prepared.Value, Prepared.Base and every op payload share its containers, and
// none of them may be modified — and ownership needs no bookkeeping: the
// tracker's containers are the ones importValue and the draft allocate, and a
// draft never places one container twice (every insertion clones), so the
// store's repeated-placement expansion and its owned set have nothing to do.
// What remains is the import: a deep, validating copy into the representation
// every revision uses.
//
// That representation is the tree encoding/json decodes — nil, bool, float64,
// string, []any, map[string]any — with two guarantees the draft and the diff
// rely on:
//
//   - every number is a float64, the one number type JavaScript has, whatever
//     Go numeric kind (or json.Number) it was handed as;
//   - every array owns a distinct backing array of capacity at least one, so a
//     container's identity — the thing upstream's === compares — is its
//     address: a map's header, a slice's first element and length. Two empty Go
//     slices made by make([]any, 0) share one zero-size address, which is why
//     an empty revision array still allocates a slot — and why an empty slice
//     with no capacity, which is every [] encoding/json decodes, is identical
//     to nothing (see anonymous).

// identity is a container's JavaScript object identity: the map header's
// address, or a slice's backing array together with its length (a shorter
// view of the same backing array is a different array). len is -1 for a map.
type identity struct {
	ptr uintptr
	len int
}

// identityOf reports v's identity when v is a container.
func identityOf(v any) (identity, bool) {
	switch c := v.(type) {
	case map[string]any:
		return identity{ptr: reflect.ValueOf(c).Pointer(), len: -1}, true
	case []any:
		return identity{ptr: reflect.ValueOf(c).Pointer(), len: len(c)}, true
	}
	return identity{}, false
}

// anonymous reports whether v is an array with no identity of its own: an
// empty slice with no capacity. Go gives every such slice one address, so
// the []s encoding/json decodes — each its own array in JavaScript, since
// JSON.parse makes a new one for each — would otherwise all be ONE array, and
// anchor alignments pi never makes. Treating them as identical to nothing is
// exact for decoded values; the tracker's own arrays always have capacity, so
// a caller that means to share an empty array between revisions gives it one
// too (make([]any, 0, 1)).
func anonymous(v any) bool {
	xs, ok := v.([]any)
	return ok && cap(xs) == 0
}

func isContainer(v any) bool {
	switch v.(type) {
	case map[string]any, []any:
		return true
	}
	return false
}

// newArray allocates an array the tracker owns: length n, and a backing array
// of its own even when n is 0, so that its identity is its own.
func newArray(n int) []any {
	return make([]any, n, max(n, 1))
}

// cloneArray is a shallow copy with a backing array of its own.
func cloneArray(xs []any) []any {
	out := newArray(len(xs))
	copy(out, xs)
	return out
}

// same is upstream's `left === right` on two JSON values: scalars by value —
// numbers of any Go kind as the one JavaScript number, so 0 === -0 — and
// containers by identity, which an anonymous array does not have.
func same(a, b any) bool {
	switch x := a.(type) {
	case nil:
		return b == nil
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case string:
		y, ok := b.(string)
		return ok && x == y
	case map[string]any, []any:
		if anonymous(x) {
			// An anonymous b needs no check of its own: no container with an
			// identity has its zero-size address.
			return false
		}
		ia, _ := identityOf(x)
		ib, ok := identityOf(b)
		return ok && ia == ib
	}
	if fa, ok := number(a); ok {
		fb, ok := number(b)
		return ok && fa == fb
	}
	return false
}

// objectIs is Object.is: same, except that 0 and -0 differ (and NaN, which no
// JSON value holds, would equal itself).
func objectIs(a, b any) bool {
	fa, oka := number(a)
	fb, okb := number(b)
	if oka && okb {
		return fa == fb && math.Signbit(fa) == math.Signbit(fb) || fa != fa && fb != fb
	}
	return same(a, b)
}

// valueKey is a JSON value as a key of upstream's Map<JsonValue, …>, which
// compares with SameValueZero: scalars by value, containers by identity.
type valueKey struct {
	kind byte // 'n'ull, 'b'ool, 'f'loat, 's'tring, 'c'ontainer
	b    bool
	f    float64
	s    string
	id   identity
}

// keyOf is v's map key; false for a value that is not JSON, or an anonymous
// array, which matches nothing.
func keyOf(v any) (valueKey, bool) {
	switch x := v.(type) {
	case nil:
		return valueKey{kind: 'n'}, true
	case bool:
		return valueKey{kind: 'b', b: x}, true
	case string:
		return valueKey{kind: 's', s: x}, true
	case map[string]any, []any:
		if anonymous(x) {
			return valueKey{}, false
		}
		id, _ := identityOf(x)
		return valueKey{kind: 'c', id: id}, true
	}
	if f, ok := number(v); ok {
		// A Go map compares float64 keys with ==, under which -0 is 0: the
		// SameValueZero upstream's Map applies.
		return valueKey{kind: 'f', f: f}, true
	}
	return valueKey{}, false
}

// number reads any Go numeric kind as the one JSON number it is, json.Number
// included — a UseNumber decoder yields those, and pi holds every JSON number
// in one type.
func number(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return float64(rv.Uint()), true
	case reflect.Float32, reflect.Float64:
		return rv.Float(), true
	}
	return 0, false
}

// jsonEqual is upstream's equalJson: same, or two containers of one kind whose
// members are pairwise equalJson.
func jsonEqual(a, b any) bool {
	if same(a, b) {
		return true
	}
	switch x := a.(type) {
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !jsonEqual(x[i], y[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			w, ok := y[k]
			if !ok || !jsonEqual(v, w) {
				return false
			}
		}
		return true
	}
	return false
}

// ─── Key order ───────────────────────────────────────────────────────────────

// keyOrder is the order an object's members are enumerated in: integer-like
// keys first, ascending — JavaScript's own rule for them — then the rest by
// UTF-16 code unit. JavaScript enumerates the rest in insertion order, which a
// Go map does not keep; this is the order of an object whose members were
// inserted sorted (ledger Divergences, "chord/delta immutable tracking").
func keyOrder(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, compareKeys)
	return keys
}

func compareKeys(a, b string) int {
	ia, oka := canonicalIndex(a)
	ib, okb := canonicalIndex(b)
	switch {
	case oka && okb:
		return ia - ib
	case oka:
		return -1
	case okb:
		return 1
	}
	return compareUTF16(a, b)
}

// canonicalIndex reports whether s is an array index as JavaScript spells
// one: the decimal form of an integer in [0, 2^32-2], without sign or
// leading zeros.
func canonicalIndex(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || uint64(n) > maxArrayIndex || strconv.Itoa(n) != s {
		return 0, false
	}
	return n, true
}

// maxArrayIndex is the largest index a JavaScript array has: 2^32 - 2.
const maxArrayIndex = 1<<32 - 2

// compareUTF16 orders two strings by UTF-16 code unit, as JavaScript's < does.
// It differs from Go's byte order only between an astral rune and a BMP rune
// above the surrogate range, which UTF-8 puts first and UTF-16 last.
func compareUTF16(a, b string) int {
	for a != "" && b != "" {
		ra, na := utf8.DecodeRuneInString(a)
		rb, nb := utf8.DecodeRuneInString(b)
		if ra != rb {
			ua, ub := ra, rb
			if ua >= 0x10000 {
				ua = 0xD800 + (ua-0x10000)>>10
			}
			if ub >= 0x10000 {
				ub = 0xD800 + (ub-0x10000)>>10
			}
			if ua != ub {
				return int(ua) - int(ub)
			}
			// Same high surrogate: the low surrogates order as the runes do.
			return int(ra) - int(rb)
		}
		a, b = a[na:], b[nb:]
	}
	return len(a) - len(b)
}

// ─── Import ──────────────────────────────────────────────────────────────────

// cloneRules names the checks a deep copy makes, in the words of the upstream
// site that makes them: the store's import (value.ts) or a draft assignment
// (draft.ts's cloneAssigned). Each message is upstream's TypeError text, then
// what to do about it.
type cloneRules struct {
	cycle, strict, plain string
	// dense and defined are assertDenseArray's two texts (see denseError).
	dense, defined string
}

var (
	importRules = cloneRules{
		cycle:   "Replicated state cannot contain cycles",
		strict:  "Replicated state values must be strict JSON",
		plain:   "Replicated state containers must be plain objects or arrays",
		dense:   "Replicated state arrays must be dense and contain only indexed entries",
		defined: "Replicated state arrays must contain enumerable indexed data properties with defined values",
	}
	assignRules = cloneRules{
		cycle:   "Assigned JSON values cannot contain cycles",
		strict:  "Assigned values must be strict JSON values",
		plain:   "Assigned JSON containers must be plain objects or arrays",
		dense:   "Draft arrays must be dense and contain only indexed entries",
		defined: "Draft arrays must contain enumerable indexed data properties with defined values",
	}
)

// undefinedMessage is upstream's text for a hole copied as a value.
const undefinedMessage = "Assigned values cannot be undefined"

// ValueError reports a value the tracker cannot hold: a cycle, a number that
// is not finite, a Go type with no JSON form, or a draft array with a hole.
// Message is upstream's TypeError text.
type ValueError struct {
	Message string
	// Value is the offending Go value, for a type or number error; nil for a
	// cycle or a hole.
	Value any
}

func (e *ValueError) Error() string {
	switch e.Message {
	case importRules.cycle, assignRules.cycle:
		return "delta: " + e.Message + " (a container reaches itself; break the cycle before handing the value over)"
	case importRules.dense, assignRules.dense, importRules.defined, assignRules.defined:
		return "delta: " + e.Message + " (a JSON array holds values at indices 0..n-1 and nothing else: fill every slot SetLen or a write past the end left empty, and address array elements by index)"
	case undefinedMessage:
		return "delta: " + e.Message + " (the source range holds a slot SetLen or a write past the end left empty; fill it first)"
	}
	var got string
	if f, ok := number(e.Value); ok {
		got = jsNumber(f)
		if math.IsNaN(f) {
			got = "NaN"
		} else if math.IsInf(f, 0) {
			got = strconv.FormatFloat(f, 'g', -1, 64)
		}
	} else {
		got = fmt.Sprintf("%T", e.Value)
	}
	return fmt.Sprintf("delta: %s: got %s (build values from nil, bool, string, a finite number, []any and map[string]any, as encoding/json decodes them)", e.Message, got)
}

// cloner is one deep, validating copy: upstream's #clone and cloneAssigned.
// ancestors holds the containers on the current path, which is how a cycle is
// told apart from a container that merely occurs twice (an alias, which the
// copy expands into two independent values).
type cloner struct {
	rules     cloneRules
	ancestors map[identity]bool
	// fresh, when set, records every array the copy allocates: the draft's
	// transaction-owned arrays.
	fresh map[identity]bool
}

// importValue is the store's import: a deep copy of v in the tracker's
// representation, with aliases expanded and every value checked.
func importValue(v any) (any, error) {
	c := cloner{rules: importRules}
	return c.clone(v, nil)
}

// clone copies v. tx, when set, is the draft transaction v was read through:
// a container that has a draft state there is copied as the draft currently
// holds it, which is what reading a live draft through its proxy yields.
func (c *cloner) clone(v any, tx *transaction) (any, error) {
	switch x := v.(type) {
	case *Draft:
		s, err := x.live()
		if err != nil {
			return nil, err
		}
		if err := s.checkDense(c.rules); err != nil {
			return nil, err
		}
		return c.clone(s.current(), s.txn)
	case map[string]any, []any:
		if tx != nil {
			if s := tx.stateOf(x); s != nil {
				if err := s.checkDense(c.rules); err != nil {
					return nil, err
				}
				v = s.current()
			}
		}
		return c.container(v, tx)
	case nil, bool, string:
		return x, nil
	case float64:
		return c.finite(x, x)
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return nil, &ValueError{Message: c.rules.strict, Value: x}
		}
		return c.finite(f, x)
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Bool:
		return rv.Bool(), nil
	case reflect.String:
		return rv.String(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		f, _ := number(v)
		return c.finite(f, v)
	case reflect.Map, reflect.Slice, reflect.Array, reflect.Struct, reflect.Pointer:
		// typeof "object" in JavaScript: a container, but not a plain one.
		return nil, &ValueError{Message: c.rules.plain, Value: v}
	}
	return nil, &ValueError{Message: c.rules.strict, Value: v}
}

// finite is a number as the tracker holds it: f, which v was converted to,
// unless it is NaN or infinite — upstream's Number.isFinite, which every Go
// numeric kind and json.Number must pass (ParseFloat reads "NaN" and "Inf"
// without an error).
func (c *cloner) finite(f float64, v any) (any, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil, &ValueError{Message: c.rules.strict, Value: v}
	}
	return f, nil
}

// container copies an object or array, refusing a cycle.
func (c *cloner) container(v any, tx *transaction) (any, error) {
	id, _ := identityOf(v)
	if c.ancestors[id] && id.ptr != 0 {
		return nil, &ValueError{Message: c.rules.cycle}
	}
	if c.ancestors == nil {
		c.ancestors = map[identity]bool{}
	}
	c.ancestors[id] = true
	defer delete(c.ancestors, id)

	switch x := v.(type) {
	case []any:
		out := newArray(len(x))
		if c.fresh != nil {
			id, _ := identityOf(out)
			c.fresh[id] = true
		}
		for i, item := range x {
			if isHole(item) {
				return nil, &ValueError{Message: c.rules.dense}
			}
			clone, err := c.clone(item, tx)
			if err != nil {
				return nil, err
			}
			out[i] = clone
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(x))
		// In enumeration order, so that of several bad members the one
		// reported is always the same.
		for _, k := range keyOrder(x) {
			clone, err := c.clone(x[k], tx)
			if err != nil {
				return nil, err
			}
			out[k] = clone
		}
		return out, nil
	}
	panic("unreachable: container called on a scalar")
}

// errNotContainer is the error for a tracked root that is not a container.
func errNotContainer(v any) error {
	return fmt.Errorf("delta: tracked state must be a JSON object (map[string]any) or array ([]any), got %s (decode the value with encoding/json, or build it from those two types)", describe(v))
}
