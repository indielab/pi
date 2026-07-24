package providers

import (
	"errors"
	"fmt"
	"strings"

	"github.com/sky-valley/pi/ai"
)

// Port of pi's packages/ai/src/api/constrained-sampling.ts. Every error message
// below reaches the user verbatim, so the literals are byte-for-byte pi's.

// grammarSampling is a tool's resolved grammar-constrained-sampling config.
type grammarSampling struct {
	// format is the OpenAI grammar syntax: "lark" or "regex".
	format     string
	definition string
	// inputProperty is the single required string property of the tool schema
	// that the raw grammar output is stored under.
	inputProperty string
}

// grammarInputBuffer re-synthesizes `{"prop":"..."}`-shaped JSON deltas from the
// raw custom-tool input chunks a provider streams. It merges pi's
// GrammarToolInputJsonBuffer with the invariant `inputProperty` that pi passes
// alongside it on every call.
type grammarInputBuffer struct {
	property string
	input    string
	started  bool
	closed   bool
}

func newGrammarInputBuffer(property string) *grammarInputBuffer {
	return &grammarInputBuffer{property: property}
}

// append advances the buffer to nextInput, which must extend the input seen so
// far, and returns the JSON delta to emit. ok is false when there is nothing to
// emit (pi returns undefined). final closes the synthesized JSON object.
func (b *grammarInputBuffer) append(nextInput string, final bool) (delta string, ok bool, err error) {
	if b.closed {
		if final && nextInput == b.input {
			return "", false, nil
		}
		return "", false, fmt.Errorf("grammar tool input for property %s changed after it was closed", quoteRaw(b.property))
	}
	if !strings.HasPrefix(nextInput, b.input) {
		return "", false, fmt.Errorf("grammar tool input for property %s changed non-monotonically", quoteRaw(b.property))
	}

	inputDelta := nextInput[len(b.input):]
	if !final && inputDelta == "" {
		return "", false, nil
	}

	var sb strings.Builder
	if !b.started {
		sb.WriteByte('{')
		sb.WriteString(jsQuote(b.property))
		sb.WriteString(`:"`)
		b.started = true
	}
	sb.WriteString(jsEscape(inputDelta))
	b.input = nextInput
	if final {
		sb.WriteString(`"}`)
		b.closed = true
	}
	return sb.String(), true, nil
}

// grammarToolInput extracts the raw grammar input from a replayed tool call's
// arguments (port of getGrammarToolInput).
func grammarToolInput(toolName string, arguments map[string]any, inputProperty string) (string, error) {
	input, ok := arguments[inputProperty].(string)
	if !ok {
		return "", fmt.Errorf("Grammar tool call %s requires argument %s to be a string.",
			quoteRaw(toolName), quoteRaw(inputProperty))
	}
	return input, nil
}

// inferGrammarInputProperty finds the single required string property that a
// grammar tool's raw output is carried in (port of inferGrammarInputProperty).
func inferGrammarInputProperty(tool ai.Tool) (string, error) {
	schema := tool.Parameters
	// pi reads the serialized JSON Schema, where a nullable type is an array and
	// therefore never equal to "object"/"string".
	if schema == nil || schema.Type != "object" || schema.Nullable {
		return "", errors.New("grammar constrained sampling requires an object parameter schema")
	}
	if len(schema.Required) != 1 {
		return "", errors.New("grammar constrained sampling requires exactly one required string property")
	}

	inputProperty := schema.Required[0]
	prop := schema.Properties[inputProperty]
	if prop == nil {
		return "", fmt.Errorf("grammar constrained sampling requires a properties entry for %s", inputProperty)
	}
	if prop.Type != "string" || prop.Nullable {
		return "", fmt.Errorf("grammar constrained sampling property %s must have type string", inputProperty)
	}
	return inputProperty, nil
}

// resolveJSONSchemaStrictSampling reports whether a tool should be sent with
// JSON-schema constrained sampling enabled (port of resolveJsonSchemaStrictSampling).
// pi returns true or undefined, never false, so a plain bool suffices.
func resolveJSONSchemaStrictSampling(tool ai.Tool, supportsStrictMode bool) (bool, error) {
	config := tool.ConstrainedSampling
	if config == nil || config.Type != ai.ConstrainedSamplingJSONSchema {
		return false, nil
	}
	if supportsStrictMode {
		return true, nil
	}
	if config.Strict == ai.ConstrainedSamplingRequire {
		return false, fmt.Errorf("Tool %s requires JSON-schema constrained sampling, but strict tools are unsupported.",
			quoteRaw(tool.Name))
	}
	return false, nil
}

// resolveGrammarSampling resolves a tool's grammar-constrained-sampling config,
// returning nil when the tool has none or the provider cannot honor it
// (port of resolveGrammarConstrainedSampling).
func resolveGrammarSampling(tool ai.Tool, supportsOpenAIGrammarTools bool) (*grammarSampling, error) {
	config := tool.ConstrainedSampling
	if config == nil || config.Type != ai.ConstrainedSamplingGrammar {
		return nil, nil
	}
	if !supportsOpenAIGrammarTools {
		return nil, nil
	}

	lark := strings.TrimSpace(config.Variants.OpenAILark)
	regex := strings.TrimSpace(config.Variants.OpenAIRegex)
	if lark == "" && regex == "" {
		return nil, fmt.Errorf("Tool %s cannot use grammar constrained sampling: no supported grammar variant was provided.",
			quoteRaw(tool.Name))
	}

	// pi keeps the UNTRIMMED definition; the trim only decides which variant wins.
	format, definition := "lark", config.Variants.OpenAILark
	if lark == "" {
		format, definition = "regex", config.Variants.OpenAIRegex
	}
	inputProperty, err := inferGrammarInputProperty(tool)
	if err != nil {
		return nil, fmt.Errorf("Tool %s cannot use grammar constrained sampling: %w.", quoteRaw(tool.Name), err)
	}
	return &grammarSampling{format: format, definition: definition, inputProperty: inputProperty}, nil
}

// grammarToolInputProperties maps each grammar tool's name to the schema
// property its raw output is carried in (port of createGrammarToolInputProperties).
func grammarToolInputProperties(tools []ai.Tool, supportsOpenAIGrammarTools bool) (map[string]string, error) {
	var properties map[string]string
	for _, tool := range tools {
		grammar, err := resolveGrammarSampling(tool, supportsOpenAIGrammarTools)
		if err != nil {
			return nil, err
		}
		if grammar == nil {
			continue
		}
		if properties == nil {
			properties = map[string]string{}
		}
		properties[tool.Name] = grammar.inputProperty
	}
	return properties, nil
}

// quoteRaw wraps s in double quotes without escaping, matching a JS template
// literal `"${s}"` (Go's %q would escape the interpolated value).
func quoteRaw(s string) string { return `"` + s + `"` }
