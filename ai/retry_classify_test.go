package ai

import "testing"

func TestIsRetryableAssistantError(t *testing.T) {
	cases := []struct {
		name string
		msg  AssistantMessage
		want bool
	}{
		{
			name: "non-error stop reason is not retryable",
			msg:  AssistantMessage{StopReason: StopStop, ErrorMessage: "overloaded"},
			want: false,
		},
		{
			name: "error stop reason with empty error message is not retryable",
			msg:  AssistantMessage{StopReason: StopError, ErrorMessage: ""},
			want: false,
		},
		{
			name: "insufficient_quota is non-retryable",
			msg:  AssistantMessage{StopReason: StopError, ErrorMessage: "insufficient_quota"},
			want: false,
		},
		{
			name: "monthly usage limit is non-retryable",
			msg:  AssistantMessage{StopReason: StopError, ErrorMessage: "Monthly usage limit reached"},
			want: false,
		},
		{
			name: "new #6019: you can retry your request",
			msg:  AssistantMessage{StopReason: StopError, ErrorMessage: "the model is busy; you can retry your request"},
			want: true,
		},
		{
			name: "new #6019: try your request again",
			msg:  AssistantMessage{StopReason: StopError, ErrorMessage: "please try your request again shortly"},
			want: true,
		},
		{
			name: "new #6019: please retry your request",
			msg:  AssistantMessage{StopReason: StopError, ErrorMessage: "transient failure, please retry your request"},
			want: true,
		},
		{
			name: "overloaded is retryable",
			msg:  AssistantMessage{StopReason: StopError, ErrorMessage: "Overloaded"},
			want: true,
		},
		{
			name: "429 is retryable",
			msg:  AssistantMessage{StopReason: StopError, ErrorMessage: "received HTTP 429 from provider"},
			want: true,
		},
		{
			name: "cloudflare 524 timeout is retryable",
			msg:  AssistantMessage{StopReason: StopError, ErrorMessage: "524 status code (no body)"},
			want: true,
		},
		{
			name: "nvidia NIM ResourceExhausted is retryable",
			msg:  AssistantMessage{StopReason: StopError, ErrorMessage: "ResourceExhausted: Worker local total request limit reached (288/48)"},
			want: true,
		},
		{
			name: "bun fetch socket drop is retryable",
			msg:  AssistantMessage{StopReason: StopError, ErrorMessage: "The socket connection was closed unexpectedly. For more information, pass `verbose: true` in the second argument to fetch()"},
			want: true,
		},
		{
			// pi b0c2a90e: OpenAI Responses streams that end before terminal
			// events (message byte-identical to pi's vitest constant).
			name: "openai responses early EOF is retryable",
			msg:  AssistantMessage{StopReason: StopError, ErrorMessage: "OpenAI Responses stream ended before a terminal response event"},
			want: true,
		},
		{
			// pi #6904: DNS transport failures, including the wrapped form
			// bedrock surfaces (message byte-identical to pi's vitest constant).
			name: "wrapped DNS lookup failure is retryable",
			msg:  AssistantMessage{StopReason: StopError, ErrorMessage: "The pending stream has been canceled (caused by: getaddrinfo ENOTFOUND bedrock-runtime.us-east-1.amazonaws.com)"},
			want: true,
		},
		{
			name: "ENOTFOUND is retryable",
			msg:  AssistantMessage{StopReason: StopError, ErrorMessage: "connect ENOTFOUND api.example.com"},
			want: true,
		},
		{
			name: "EAI_AGAIN is retryable",
			msg:  AssistantMessage{StopReason: StopError, ErrorMessage: "EAI_AGAIN api.example.com"},
			want: true,
		},
		{
			name: "getaddrinfo is retryable",
			msg:  AssistantMessage{StopReason: StopError, ErrorMessage: "getaddrinfo failed for api.example.com"},
			want: true,
		},
		{
			name: "non-matching error message is not retryable",
			msg:  AssistantMessage{StopReason: StopError, ErrorMessage: "model refused to answer"},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsRetryableAssistantError(tc.msg); got != tc.want {
				t.Errorf("IsRetryableAssistantError(%q) = %v, want %v", tc.msg.ErrorMessage, got, tc.want)
			}
		})
	}
}
