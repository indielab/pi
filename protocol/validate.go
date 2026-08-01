package protocol

// Constraint helpers mirroring the TypeBox modifiers pi's schemas use:
// IdSchema is String({minLength: 1}), TimestampSchema is Integer({minimum: 0}),
// and so on. Keeping them as named helpers means a schema change upstream maps
// to one call site here rather than an open-coded comparison.

func requireNonEmpty(name, value string) error {
	if value == "" {
		return invalidf("%s must not be empty", name)
	}
	return nil
}

// requireID is String({minLength: 1}) — pi's IdSchema.
func requireID(name, value string) error { return requireNonEmpty(name, value) }

func requireNonNegative(name string, value int64) error {
	if value < 0 {
		return invalidf("%s must be greater than or equal to 0", name)
	}
	return nil
}

func requirePositive(name string, value int64) error {
	if value < 1 {
		return invalidf("%s must be greater than or equal to 1", name)
	}
	return nil
}

func requireNonNegativeFloat(name string, value float64) error {
	if !(value >= 0) { // also rejects NaN
		return invalidf("%s must be greater than or equal to 0", name)
	}
	return nil
}

// requireTimestamp is TimestampSchema: Integer({minimum: 0}).
func requireTimestamp(name string, value int64) error { return requireNonNegative(name, value) }

func requireLiteral(name, got, want string) error {
	if got != want {
		return invalidf("%s must be %q, got %q", name, want, got)
	}
	return nil
}

// maxJSONValueDepth bounds recursion through a JsonValue. The CBOR layer
// already caps nesting, but a JsonValue can also be built in-process before
// encoding, and this keeps validation from recursing without bound on a
// structure that contains a cycle.
const maxJSONValueDepth = 64

// requireJSONValue checks that value is one of pi's JsonValue shapes.
//
// pi's isProtocolValue accepts null, boolean, number, string, arrays, and
// objects whose prototype is Object.prototype — which means a Uint8Array is
// NOT a legal JsonValue. []byte is rejected here for the same reason: pi would
// refuse to encode it, so accepting it in Go would let a Go peer produce a
// message no Node peer would have sent.
func requireJSONValue(name string, value any) error {
	return checkJSONValue(name, value, 0)
}

func checkJSONValue(name string, value any, depth int) error {
	if depth > maxJSONValueDepth {
		return invalidf("%s is nested too deeply", name)
	}
	switch v := value.(type) {
	case nil, bool, string, int64, float64:
		return nil
	case []any:
		for i, item := range v {
			if err := checkJSONValue(name, item, depth+1); err != nil {
				return err
			}
			_ = i
		}
		return nil
	case map[string]any:
		for _, item := range v {
			if err := checkJSONValue(name, item, depth+1); err != nil {
				return err
			}
		}
		return nil
	default:
		return invalidf("%s must be a JSON value", name)
	}
}
