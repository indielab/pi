package protocol

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"
)

// maxSafeInteger is JavaScript's Number.MAX_SAFE_INTEGER. pi has one number
// type, so an integer field is satisfied by anything Number.isInteger accepts
// within this range — including a float that happens to be integral.
const maxSafeInteger = 1<<53 - 1

// ValidationError is a protocol message that failed structural or constraint
// checking (pi's ProtocolValidationError).
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func invalidf(format string, a ...any) *ValidationError {
	return &ValidationError{Msg: fmt.Sprintf(format, a...)}
}

// Validator is implemented by message types carrying constraints the struct
// shape alone cannot express — TypeBox's minLength, minimum, and literals.
type Validator interface {
	Validate() error
}

// unionDecoders maps a union interface type to the function that resolves a
// decoded CBOR value into one of its members. pi discriminates these unions
// with a literal property; Go dispatches on the same property, registered here
// so the reflective decoder can recurse through interface-typed fields.
var unionDecoders = map[reflect.Type]func(any) (any, error){}

func registerUnion[T any](decode func(any) (T, error)) {
	t := reflect.TypeFor[T]()
	unionDecoders[t] = func(v any) (any, error) { return decode(v) }
}

// decodeInto strictly decodes a CBOR-decoded value into target, which must be a
// non-nil pointer.
//
// Strict means what TypeBox's additionalProperties:false means: an unknown
// property is a rejection, not something to ignore. A peer that can smuggle
// extra fields past the parser can reach code paths the schema was meant to
// gate, so this is a security boundary rather than a tidiness rule.
//
// Validation is not repeated here: decodeStruct validates every struct it fills,
// the top-level one included, so a second Validator check on target would run
// the same constraints twice.
func decodeInto(value any, target any) error {
	rv := reflect.ValueOf(target)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return invalidf("decode target must be a non-nil pointer")
	}
	return decodeValue(value, rv.Elem(), "")
}

func path(prefix, field string) string {
	if prefix == "" {
		return field
	}
	return prefix + "." + field
}

func decodeValue(value any, target reflect.Value, at string) error {
	// A registered union interface resolves by its discriminant before any
	// structural rules apply.
	if decode, ok := unionDecoders[target.Type()]; ok {
		resolved, err := decode(value)
		if err != nil {
			return prefixError(err, at)
		}
		target.Set(reflect.ValueOf(resolved))
		return nil
	}

	switch target.Kind() {
	case reflect.Pointer:
		elem := reflect.New(target.Type().Elem())
		if err := decodeValue(value, elem.Elem(), at); err != nil {
			return err
		}
		target.Set(elem)
		return nil

	case reflect.Interface:
		// An unconstrained interface is JsonValue: anything the CBOR decoder
		// produced is already a legal value.
		if target.NumMethod() == 0 {
			// reflect.ValueOf(nil) is the zero Value and cannot be Set, so a
			// null JsonValue leaves the field at its nil zero value. Missing
			// this is a remotely triggerable panic: any peer could crash the
			// process by sending null for a tool input.
			if value == nil {
				return nil
			}
			target.Set(reflect.ValueOf(value))
			return nil
		}
		return invalidf("no decoder registered for union %s at %s", target.Type(), at)

	case reflect.String:
		s, ok := value.(string)
		if !ok {
			return invalidf("%s must be a string", describe(at))
		}
		target.SetString(s)
		return nil

	case reflect.Bool:
		b, ok := value.(bool)
		if !ok {
			return invalidf("%s must be a boolean", describe(at))
		}
		target.SetBool(b)
		return nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		switch n := value.(type) {
		case int64:
			target.SetInt(n)
			return nil
		case float64:
			// TypeBox checks Number.isInteger, and pi has one number type, so
			// an integral float satisfies an integer field. pi's own encoder
			// folds these to CBOR ints, but a third-party peer may not.
			if n != math.Trunc(n) || math.IsInf(n, 0) || math.Abs(n) > maxSafeInteger {
				return invalidf("%s must be an integer", describe(at))
			}
			target.SetInt(int64(n))
			return nil
		default:
			return invalidf("%s must be an integer", describe(at))
		}

	case reflect.Float32, reflect.Float64:
		// pi has one number type, so an integer on the wire is a legal value
		// for a float field — and Encode writes integral floats as integers,
		// so this is the same value coming back.
		switch n := value.(type) {
		case float64:
			target.SetFloat(n)
		case int64:
			target.SetFloat(float64(n))
		default:
			return invalidf("%s must be a number", describe(at))
		}
		return nil

	case reflect.Slice:
		if target.Type().Elem().Kind() == reflect.Uint8 {
			b, ok := value.([]byte)
			if !ok {
				return invalidf("%s must be a byte string", describe(at))
			}
			target.SetBytes(b)
			return nil
		}
		items, ok := value.([]any)
		if !ok {
			return invalidf("%s must be an array", describe(at))
		}
		out := reflect.MakeSlice(target.Type(), len(items), len(items))
		for i, item := range items {
			if err := decodeValue(item, out.Index(i), fmt.Sprintf("%s[%d]", at, i)); err != nil {
				return err
			}
		}
		target.Set(out)
		return nil

	case reflect.Map:
		// A CBOR object always has string keys, so a map field with any other
		// key type has no decodable form; without this the SetMapIndex below
		// panics on the type mismatch, the way the encoder's guard prevents on
		// the way out.
		keyType := target.Type().Key()
		if keyType.Kind() != reflect.String {
			return invalidf("%s cannot decode into a map keyed by %s: protocol objects have string keys, "+
				"so declare the field as a map with a string key type", describe(at), keyType)
		}
		entries, ok := value.(map[string]any)
		if !ok {
			return invalidf("%s must be an object", describe(at))
		}
		out := reflect.MakeMapWithSize(target.Type(), len(entries))
		for key, item := range entries {
			elem := reflect.New(target.Type().Elem()).Elem()
			if err := decodeValue(item, elem, path(at, key)); err != nil {
				return err
			}
			// Convert: a named string type is still a legal key type, but
			// SetMapIndex demands the map's exact key type.
			out.SetMapIndex(reflect.ValueOf(key).Convert(keyType), elem)
		}
		target.Set(out)
		return nil

	case reflect.Struct:
		return decodeStruct(value, target, at)

	default:
		return invalidf("cannot decode into %s at %s", target.Kind(), at)
	}
}

func decodeStruct(value any, target reflect.Value, at string) error {
	entries, ok := value.(map[string]any)
	if !ok {
		return invalidf("%s must be an object", describe(at))
	}

	fields := structFields(target.Type())
	seen := make(map[string]bool, len(entries))
	for _, f := range fields {
		raw, present := entries[f.name]
		if !present {
			if f.optional {
				continue
			}
			return invalidf("%s is missing required property %q", describe(at), f.name)
		}
		seen[f.name] = true
		if err := decodeValue(raw, target.FieldByIndex(f.index), path(at, f.name)); err != nil {
			return err
		}
	}
	for key := range entries {
		if !seen[key] {
			return invalidf("%s has unexpected property %q", describe(at), key)
		}
	}

	if v, ok := target.Addr().Interface().(Validator); ok {
		if err := v.Validate(); err != nil {
			return prefixError(err, at)
		}
	}
	return nil
}

func describe(at string) string {
	if at == "" {
		return "message"
	}
	return at
}

func prefixError(err error, at string) error {
	if at == "" {
		return err
	}
	return invalidf("%s: %s", at, err.Error())
}

type fieldInfo struct {
	name     string
	index    []int
	optional bool
}

// fieldCache is shared by every connection's decoder, so it must be safe for
// concurrent use.
var fieldCache sync.Map

func structFields(t reflect.Type) []fieldInfo {
	if cached, ok := fieldCache.Load(t); ok {
		return cached.([]fieldInfo)
	}
	fields := make([]fieldInfo, 0, t.NumField())
	for i := range t.NumField() {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}
		tag := sf.Tag.Get("cbor")
		if tag == "-" {
			continue
		}
		name, rest, _ := strings.Cut(tag, ",")
		if name == "" {
			name = sf.Name
		}
		fields = append(fields, fieldInfo{
			name:     name,
			index:    sf.Index,
			optional: strings.Contains(rest, "omitempty"),
		})
	}
	fieldCache.Store(t, fields)
	return fields
}

// discriminant reads a union's tag property, the first thing every union
// decoder needs.
func discriminant(value any, property string) (string, map[string]any, error) {
	entries, ok := value.(map[string]any)
	if !ok {
		return "", nil, invalidf("expected an object")
	}
	raw, present := entries[property]
	if !present {
		return "", nil, invalidf("missing %q", property)
	}
	tag, ok := raw.(string)
	if !ok {
		return "", nil, invalidf("%q must be a string", property)
	}
	return tag, entries, nil
}

// decodeMember decodes value into a fresh T and returns it, for union arms.
func decodeMember[T any](value any) (T, error) {
	var out T
	if err := decodeInto(value, &out); err != nil {
		return out, err
	}
	return out, nil
}
