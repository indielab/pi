package ai

import (
	"encoding/json"
	"reflect"
	"testing"
)

// Upstream 8562bcf66: a session pi wrote records nested calls on tool results,
// and the port carries them through unchanged — a call that took 0ms keeps its
// durationMs, a call with no arguments keeps {}, and one whose arguments were
// omitted for size keeps argumentsBytes.
func TestNestedToolCallsRoundTrip(t *testing.T) {
	const written = `{"role":"toolResult","toolCallId":"codemode-1","toolName":"codemode","content":[],` +
		`"nestedCalls":{"calls":[` +
		`{"id":"codemode-1/1","name":"read","status":"ok","arguments":{"path":"a.ts"},"durationMs":0},` +
		`{"id":"codemode-1/2","name":"ls","status":"error","arguments":{},"durationMs":3,"error":"boom"},` +
		`{"id":"codemode-1/3","name":"write","status":"unfinished","argumentsBytes":40000}` +
		`],"complete":false},"isError":false,"timestamp":0}`
	msg, err := UnmarshalMessage([]byte(written))
	if err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var want, got any
	if err := json.Unmarshal([]byte(written), &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip changed the message:\n got %s\nwant %s", out, written)
	}
}
