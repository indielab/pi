package providers

import (
	"bufio"
	"context"
	"errors"
	"strings"
	"testing"
)

// A line past the reader's limit fails the stream with the port's own error,
// which says the limit is the port's and what to report: pi's reader holds a
// line of any length.
func TestAnthropicStreamLineLimitSaysItIsThePorts(t *testing.T) {
	ignore := func(rawObject) error { return nil }
	body := "event: message_start\ndata: {\"id\":\"" + strings.Repeat("x", maxAnthropicSSELine) + "\"}\n\n"
	err := iterateAnthropicSSE(strings.NewReader(body), context.Background(), nil, ignore)
	want := "an anthropic stream line is longer than the port's 16 MiB limit (pi reads a line of any length); this is a port limit, report it with the provider and model: bufio.Scanner: token too long"
	if err == nil || err.Error() != want || !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("err = %v, want %q wrapping bufio.ErrTooLong", err, want)
	}
	under := "event: message_start\ndata: {\"id\":\"" + strings.Repeat("x", maxAnthropicSSELine-20) + "\"}\n\n"
	if err := iterateAnthropicSSE(strings.NewReader(under), context.Background(), nil, ignore); err != nil {
		t.Fatalf("a line just under the limit: %v", err)
	}
}
