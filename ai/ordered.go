package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// OrderedField is one key/value pair of an OrderedObject.
type OrderedField struct {
	Key   string
	Value any
}

// OrderedObject marshals key/value pairs in slice order, mirroring
// JSON.stringify of a JS object (insertion order) for byte-exact request bodies.
// Values may nest further OrderedObjects and []any, so the order of an entire
// decoded document survives, not just its top level.
type OrderedObject []OrderedField

func (o OrderedObject) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, f := range o {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, err := json.Marshal(f.Key)
		if err != nil {
			return nil, err
		}
		buf.Write(key)
		buf.WriteByte(':')
		val, err := json.Marshal(f.Value)
		if err != nil {
			return nil, err
		}
		buf.Write(val)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// Plain projects the object onto the map form `encoding/json` would have
// decoded, dropping the recorded order. It is what makes an OrderedObject
// checkable against the map it accompanies.
func (o OrderedObject) Plain() map[string]any {
	m := make(map[string]any, len(o))
	for _, f := range o {
		m[f.Key] = plainValue(f.Value)
	}
	return m
}

func plainValue(v any) any {
	switch t := v.(type) {
	case OrderedObject:
		return t.Plain()
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = plainValue(e)
		}
		return out
	default:
		return v
	}
}

// DecodeOrderedObject decodes a JSON object into both the map `encoding/json`
// would produce and an order-preserving twin. Numbers decode to float64 and
// nulls to nil exactly as json.Unmarshal into `any` does, so the two forms
// differ only in whether key order survives.
//
// Anything that is not a single, complete JSON object is an error, matching
// json.Unmarshal into a map[string]any.
func DecodeOrderedObject(data []byte) (map[string]any, OrderedObject, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, nil, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, nil, fmt.Errorf("expected a JSON object, got %v", tok)
	}
	obj, err := decodeOrderedObjectBody(dec)
	if err != nil {
		return nil, nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, nil, fmt.Errorf("unexpected trailing content after JSON object")
	}
	return obj.Plain(), obj, nil
}

// decodeOrderedObjectBody reads members up to and including the closing brace.
func decodeOrderedObjectBody(dec *json.Decoder) (OrderedObject, error) {
	obj := OrderedObject{}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("object key is not a string")
		}
		value, err := decodeOrderedValue(dec)
		if err != nil {
			return nil, err
		}
		// A repeated key keeps its first position and takes the last value,
		// which is what a JS object literal and json.Unmarshal both do.
		if i := obj.indexOf(key); i >= 0 {
			obj[i].Value = value
		} else {
			obj = append(obj, OrderedField{Key: key, Value: value})
		}
	}
	if _, err := dec.Token(); err != nil { // closing '}'
		return nil, err
	}
	return obj, nil
}

func (o OrderedObject) indexOf(key string) int {
	for i, f := range o {
		if f.Key == key {
			return i
		}
	}
	return -1
}

func decodeOrderedValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return tok, nil // string, float64, bool, or nil
	}
	switch delim {
	case '{':
		return decodeOrderedObjectBody(dec)
	case '[':
		arr := []any{}
		for dec.More() {
			el, err := decodeOrderedValue(dec)
			if err != nil {
				return nil, err
			}
			arr = append(arr, el)
		}
		if _, err := dec.Token(); err != nil { // closing ']'
			return nil, err
		}
		return arr, nil
	default:
		return nil, fmt.Errorf("unexpected delimiter %q", delim)
	}
}
