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
			// pi "keeps provider limit errors non-retryable": the limit pattern
			// wins over a retryable status in the same message.
			name: "429 quota exceeded is non-retryable",
			msg:  AssistantMessage{StopReason: StopError, ErrorMessage: "429 quota exceeded"},
			want: false,
		},
		{
			// pi "classifies assistant error messages": fauxAssistantMessage("not an error").
			name: "non-error message without error text is not retryable",
			msg:  AssistantMessage{StopReason: StopStop},
			want: false,
		},
		{
			// pi "matches explicit provider retry guidance" (openAIExplicitRetryMessage).
			name: "openai explicit retry guidance is retryable",
			msg:  AssistantMessage{StopReason: StopError, ErrorMessage: "An error occurred while processing your request. You can retry your request, or contact us through our help center at help.openai.com if the error persists. Please include the request ID req_******** in your message."},
			want: true,
		},
		{
			// pi "matches explicit provider retry guidance" (bedrockExplicitRetryMessage).
			name: "bedrock explicit retry guidance is retryable",
			msg:  AssistantMessage{StopReason: StopError, ErrorMessage: `{"message":"The system encountered an unexpected error during processing. Try your request again."}`},
			want: true,
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
			// pi "classifies assistant error messages".
			name: "overloaded_error is retryable",
			msg:  AssistantMessage{StopReason: StopError, ErrorMessage: "overloaded_error"},
			want: true,
		},
		{
			name: "429 is retryable",
			msg:  AssistantMessage{StopReason: StopError, ErrorMessage: "received HTTP 429 from provider"},
			want: true,
		},
		{
			// pi e5d18382a (#9627): Cloudflare's unknown-error status
			// (message byte-identical to pi's vitest literal).
			name: "cloudflare 520 unknown error is retryable",
			msg:  AssistantMessage{StopReason: StopError, ErrorMessage: "520 status code (no body)"},
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
			// pi fe10558eb: proxies that give up buffering a retried upstream
			// request (message byte-identical to pi's vitest constant).
			name: "upstream request buffer exhaustion is retryable",
			msg:  AssistantMessage{StopReason: StopError, ErrorMessage: "Error: exceeded request buffer limit while retrying upstream"},
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
