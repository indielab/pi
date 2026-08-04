package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestContentListDiscriminatedJSON(t *testing.T) {
	cl := ContentList{
		TextContent{Text: "hi"},
		ThinkingContent{Thinking: "hmm", ThinkingSignature: "sig"},
		ToolCall{ID: "1", Name: "bash", Arguments: map[string]any{"cmd": "ls"}},
	}
	raw, err := json.Marshal(cl)
	if err != nil {
		t.Fatal(err)
	}
	var back ContentList
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if len(back) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(back))
	}
	if _, ok := back[0].(TextContent); !ok {
		t.Fatalf("block 0 not TextContent: %T", back[0])
	}
	if _, ok := back[1].(ThinkingContent); !ok {
		t.Fatalf("block 1 not ThinkingContent: %T", back[1])
	}
	tc, ok := back[2].(ToolCall)
	if !ok || tc.Name != "bash" {
		t.Fatalf("block 2 not ToolCall: %#v", back[2])
	}
}

func TestMessageRoleRoundTrip(t *testing.T) {
	msgs := []Message{
		NewUserText("hello", 1),
		AssistantMessage{Content: ContentList{TextContent{Text: "hi"}}, Model: "m", StopReason: StopStop, Timestamp: 2},
		ToolResultMessage{ToolCallID: "1", ToolName: "bash", Content: ContentList{TextContent{Text: "ok"}}, Timestamp: 3},
	}
	for _, m := range msgs {
		raw, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		back, err := UnmarshalMessage(raw)
		if err != nil {
			t.Fatalf("unmarshal %s: %v", raw, err)
		}
		if back.MessageRole() != m.MessageRole() {
			t.Fatalf("role mismatch: %s vs %s", back.MessageRole(), m.MessageRole())
		}
	}
}

// TestToolResultAddedToolNamesRoundTrip pins the new addedToolNames field: it is
// omitted when empty (byte-identical to the pre-change payload) and survives a
// marshal/unmarshal round-trip through the custom MarshalJSON alias.
func TestToolResultAddedToolNamesRoundTrip(t *testing.T) {
	plain := ToolResultMessage{ToolCallID: "1", ToolName: "bash", Content: ContentList{TextContent{Text: "ok"}}, Timestamp: 3}
	raw, err := json.Marshal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "addedToolNames") {
		t.Fatalf("empty addedToolNames must be omitted, got %s", raw)
	}

	marked := plain
	marked.AddedToolNames = []string{"late_tool", "other"}
	raw, err = json.Marshal(marked)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"addedToolNames":["late_tool","other"]`) {
		t.Fatalf("addedToolNames not marshaled: %s", raw)
	}
	back, err := UnmarshalMessage(raw)
	if err != nil {
		t.Fatal(err)
	}
	tr, ok := back.(ToolResultMessage)
	if !ok {
		t.Fatalf("round-trip type = %T, want ToolResultMessage", back)
	}
	if len(tr.AddedToolNames) != 2 || tr.AddedToolNames[0] != "late_tool" || tr.AddedToolNames[1] != "other" {
		t.Fatalf("addedToolNames round-trip = %v", tr.AddedToolNames)
	}
}

func TestUserMessageAcceptsStringContent(t *testing.T) {
	var m UserMessage
	if err := json.Unmarshal([]byte(`{"role":"user","content":"plain text","timestamp":5}`), &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(m.Content))
	}
	tc, ok := m.Content[0].(TextContent)
	if !ok || tc.Text != "plain text" {
		t.Fatalf("string content not normalized: %#v", m.Content[0])
	}
}

// TestUserMessageStringContentRoundTrip asserts string-form content is
// re-emitted as a string on marshal (pi: content is string | array, passed
// through untouched), while array-form content stays an array.
func TestUserMessageStringContentRoundTrip(t *testing.T) {
	src := `{"role":"user","content":"plain text","timestamp":5}`
	var m UserMessage
	if err := json.Unmarshal([]byte(src), &m); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != src {
		t.Fatalf("string content round-trip changed:\n got: %s\nwant: %s", out, src)
	}

	// Array-form input must stay an array.
	arr := UserMessage{Content: ContentList{TextContent{Text: "hello"}}, Timestamp: 1}
	raw, err := json.Marshal(arr)
	if err != nil {
		t.Fatal(err)
	}
	var probe struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatal(err)
	}
	if len(probe.Content) == 0 || probe.Content[0] != '[' {
		t.Fatalf("array content serialized as non-array: %s", raw)
	}

	// NewUserText is string-form, like pi's prompt-created user messages
	// (`content` is a plain string on the wire and in session files).
	str, err := json.Marshal(NewUserText("hello", 1))
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"role":"user","content":"hello","timestamp":1}`; string(str) != want {
		t.Fatalf("NewUserText must serialize string-form:\n got: %s\nwant: %s", str, want)
	}
}

// TestUserMessageMissingContentTolerated asserts a missing or null content key
// yields empty content rather than an error (JSON.parse tolerance in pi).
func TestUserMessageMissingContentTolerated(t *testing.T) {
	var m UserMessage
	if err := json.Unmarshal([]byte(`{"role":"user","timestamp":5}`), &m); err != nil {
		t.Fatalf("missing content key errored: %v", err)
	}
	if len(m.Content) != 0 || m.Timestamp != 5 {
		t.Fatalf("missing content: got %#v ts=%d", m.Content, m.Timestamp)
	}
	if err := json.Unmarshal([]byte(`{"role":"user","content":null,"timestamp":5}`), &m); err != nil {
		t.Fatalf("null content errored: %v", err)
	}
	if len(m.Content) != 0 {
		t.Fatalf("null content: got %#v", m.Content)
	}
}

// ConstrainedSamplingConfig mirrors pi's discriminated union, whose "disabled"
// spelling on the wire is the literal `false`.
func TestConstrainedSamplingConfigJSON(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  ConstrainedSamplingConfig
		want string
	}{
		{"disabled", ConstrainedSamplingConfig{}, `false`},
		{
			"json_schema", ConstrainedSamplingConfig{Type: ConstrainedSamplingJSONSchema, Strict: ConstrainedSamplingRequire},
			`{"type":"json_schema","strict":"require"}`,
		},
		{
			"grammar",
			ConstrainedSamplingConfig{Type: ConstrainedSamplingGrammar, Variants: GrammarVariants{OpenAILark: "start: /.+/"}},
			`{"type":"grammar","variants":{"openai_lark":"start: /.+/"}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.cfg)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != tc.want {
				t.Fatalf("marshal = %s, want %s", raw, tc.want)
			}
			var back ConstrainedSamplingConfig
			if err := json.Unmarshal(raw, &back); err != nil {
				t.Fatal(err)
			}
			if back != tc.cfg {
				t.Fatalf("round trip = %#v, want %#v", back, tc.cfg)
			}
		})
	}

	// A tool carrying `false` decodes to the disabled config, not an error.
	var tool Tool
	if err := json.Unmarshal([]byte(`{"name":"t","description":"d","constrainedSampling":false}`), &tool); err != nil {
		t.Fatalf("constrainedSampling:false must decode: %v", err)
	}
	if tool.ConstrainedSampling == nil || tool.ConstrainedSampling.Type != "" {
		t.Fatalf("constrainedSampling:false = %#v", tool.ConstrainedSampling)
	}
}

// TestConstrainedSamplingUnknownTypeRejected: the discriminant is a typed
// string, and an unrecognized value must be a loud error on BOTH sides rather
// than silently round-tripping to `false` — which would quietly drop the
// caller's constrained-sampling request.
func TestConstrainedSamplingUnknownTypeRejected(t *testing.T) {
	t.Run("marshal", func(t *testing.T) {
		_, err := json.Marshal(ConstrainedSamplingConfig{Type: "bogus"})
		if err == nil {
			t.Fatal("marshalling an unknown type must fail, not emit false")
		}
		if !strings.Contains(err.Error(), "bogus") {
			t.Errorf("error should name the offending type, got %v", err)
		}
	})
	t.Run("unmarshal", func(t *testing.T) {
		var c ConstrainedSamplingConfig
		if err := json.Unmarshal([]byte(`{"type":"bogus"}`), &c); err == nil {
			t.Fatal("unmarshalling an unknown type must fail")
		}
	})
	t.Run("unmarshal true leaks no internal type", func(t *testing.T) {
		var c ConstrainedSamplingConfig
		err := json.Unmarshal([]byte(`true`), &c)
		if err == nil {
			t.Fatal("`true` is not a valid constrainedSampling value")
		}
		if strings.Contains(err.Error(), "alias") {
			t.Errorf("error leaks the private shim type: %v", err)
		}
		if !strings.Contains(err.Error(), "json_schema") {
			t.Errorf("error should hint the valid spellings, got %v", err)
		}
	})
	t.Run("empty strict defaults to prefer on the wire", func(t *testing.T) {
		b, err := json.Marshal(ConstrainedSamplingConfig{Type: ConstrainedSamplingJSONSchema})
		if err != nil {
			t.Fatal(err)
		}
		// pi's union admits only "prefer"|"require"; "" is not a value it accepts.
		if got, want := string(b), `{"type":"json_schema","strict":"prefer"}`; got != want {
			t.Errorf("marshal = %s, want %s", got, want)
		}
	})
}

// TestAssistantDeferredHandleRoundTrip locks pi 382aa641c's message-format
// half: a deferred assistant message persists its stop reason and handle, and
// a message without one is byte-identical to what it was before the field
// existed.
func TestAssistantDeferredHandleRoundTrip(t *testing.T) {
	plain := AssistantMessage{
		Content: ContentList{TextContent{Text: "hi"}}, Api: "api", Provider: "p",
		Model: "m", StopReason: StopStop, Timestamp: 5,
	}
	raw, err := json.Marshal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "deferred") {
		t.Fatalf("a message without a handle must not gain a deferred field: %s", raw)
	}

	deferred := plain
	deferred.Content = ContentList{}
	deferred.StopReason = StopDeferred
	deferred.Deferred = &DeferredHandle{
		Provider: "p", ModelID: "m", Api: "api", ID: "resp-1", PollAfterMs: 25,
		Data: map[string]any{"cursor": "abc"},
	}
	raw, err = json.Marshal(deferred)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"stopReason":"deferred"`) {
		t.Fatalf("stop reason not persisted: %s", raw)
	}
	if strings.Contains(string(raw), "expiresAt") {
		t.Fatalf("an unset expiresAt must be omitted: %s", raw)
	}

	back, err := UnmarshalMessage(raw)
	if err != nil {
		t.Fatal(err)
	}
	msg, ok := back.(AssistantMessage)
	if !ok {
		t.Fatalf("round-trip type = %T, want AssistantMessage", back)
	}
	if msg.StopReason != StopDeferred || msg.Deferred == nil {
		t.Fatalf("deferred message did not round-trip: %+v", msg)
	}
	if msg.Deferred.ID != "resp-1" || msg.Deferred.PollAfterMs != 25 || msg.Deferred.ModelID != "m" {
		t.Fatalf("handle round-trip = %+v", *msg.Deferred)
	}
	data, ok := msg.Deferred.Data.(map[string]any)
	if !ok || data["cursor"] != "abc" {
		t.Fatalf("opaque handle data did not survive: %#v", msg.Deferred.Data)
	}

	// Cloning must not alias the handle back onto the original message.
	clone := msg.Clone()
	clone.Deferred.ID = "other"
	if msg.Deferred.ID != "resp-1" {
		t.Fatal("Clone must copy the deferred handle, not alias it")
	}
}
