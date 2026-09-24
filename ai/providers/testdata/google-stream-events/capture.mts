// Captures what pi's google-generative-ai adapter hands onProviderStreamEvent,
// and how the stream ends, for a table of raw HTTP responses — the oracle
// behind TestGoogleStreamEventsMatchPi and TestGoogleSSEReadChunksMatchPi in
// this package.
//
//   node --experimental-strip-types capture.mts <extraction> <out.json> <sha>
//   e.g. ... capture.mts <dir> google-stream-events-8676a0dcd.json 8676a0dcd
//
// <extraction> holds `git archive <sha> packages/ai package-lock.json` from the
// upstream clone, plus a node_modules resolving pi-ai's dependencies (the npm
// build's). onProviderStreamEvent landed upstream in 002fc8385, after the 0.87.1
// build, so these are src captures: re-verify them against the first build that
// ships it (the BUILD wins).
//
// The data pi observes is what @google/genai yields, so the oracle is that SDK
// at the version the sha's package-lock.json locks. The script refuses to
// write unless the resolved @google/genai matches the lockfile's version AND
// integrity.
//
// Each scenario is served by a raw TCP server that writes the response head
// and then each body segment as its own socket write, 50ms apart, so each
// segment reaches the SDK as its own body read (its bare-JSON error check
// runs per read). Nothing else is added to the head — no Date, no
// Connection — so the header record the SDK builds is fully determined by the
// scenario. `framing` is "close" (the body runs to EOF) or "chunked" (each
// segment is one HTTP chunk). `encode` turns the whole body into the bytes
// written in one write (a gzip, deflate or brotli body, whole or damaged),
// recorded as `encodedBody` so a replay serves the same bytes;
// `contentLength` adds a Content-Length for what is written; `oneWrite` sends
// every segment as its own HTTP chunk but all in one socket write;
// `abruptEnd` destroys the socket after the segments instead of ending the
// body. `requestHeaders` are the options.headers pi's caller passes, and
// `acceptEncoding` records the accept-encoding the server received.
//
// Every scenario runs twice: as separate reads, and with its segments joined
// into one write. `readBoundariesMatter` records whether the two differ; the
// recorded outcome is the separate-read one.
//
// The callback records what it receives, and throws or aborts the request's
// signal on the call a scenario names (`throwOn`, `abortOn`).
//
// Recorded per scenario: `events`, JSON.stringify of each value the callback
// received (key order is part of the contract); `sameModel`, whether every
// call got the model object pi was called with; `stream`, the assistant
// stream's events as type and delta; and the final message's stopReason,
// errorMessage, responseId, content (as JSON text), and usage: the token
// counts, reasoning, and the cost calculateCost left.
//
// A scenario with a `divergence` is a measured difference the port carries,
// recorded for the ledger. Its `divergentFields` name the parts of the
// outcome where the port differs (the Go replay's keys: stream, events,
// sameModel, stop, responseId, content, usage.<field>, usage.cost.<field>,
// acceptEncoding); TestGoogleDivergencesStillDiffer requires exactly those to
// differ and every other part to be pi's.
import fs from "node:fs";
import zlib from "node:zlib";
import net from "node:net";
import path from "node:path";
import { createRequire } from "node:module";
import { pathToFileURL } from "node:url";

const [extraction, outFile, sha] = process.argv.slice(2);
if (!extraction || !outFile || !sha) {
	console.error("usage: node --experimental-strip-types capture.mts <extraction> <out.json> <sha>");
	process.exit(2);
}

const src = path.join(extraction, "packages/ai/src");
const require = createRequire(path.join(src, "api/google-generative-ai.ts"));
// The package does not export ./package.json: walk up from its entry point.
let genaiDir = path.dirname(require.resolve("@google/genai"));
while (!fs.existsSync(path.join(genaiDir, "package.json"))) genaiDir = path.dirname(genaiDir);
const genaiVersion = JSON.parse(fs.readFileSync(path.join(genaiDir, "package.json"), "utf8")).version as string;
const lock = JSON.parse(fs.readFileSync(path.join(extraction, "package-lock.json"), "utf8"));
const locked = lock.packages["node_modules/@google/genai"];
const installed = JSON.parse(
	fs.readFileSync(path.join(genaiDir, "..", "..", ".package-lock.json"), "utf8"),
).packages["node_modules/@google/genai"];
if (locked.version !== genaiVersion || locked.integrity !== installed?.integrity) {
	console.error(
		`@google/genai mismatch: ${sha} locks ${locked.version} ${locked.integrity}, resolved ${genaiVersion} ${installed?.integrity}`,
	);
	process.exit(1);
}

const { stream } = await import(pathToFileURL(path.join(src, "api/google-generative-ai.ts")).href);

type Scenario = {
	name: string;
	note?: string;
	framing: "close" | "chunked";
	headers: Array<[string, string]>;
	segments: string[];
	// The bytes written for the whole body, in one write.
	encode?: (body: Buffer) => Buffer;
	contentLength?: boolean;
	oneWrite?: boolean;
	abruptEnd?: boolean;
	requestHeaders?: Record<string, string>;
	divergence?: string;
	divergentFields?: string[];
	// Throw Error(throwMessage) from the callback on its Nth call (0-based).
	throwOn?: number;
	throwMessage?: string;
	// Abort the request's signal from the callback on its Nth call (0-based).
	abortOn?: number;
};

const sse = (...events: unknown[]) => events.map((e) => `data: ${typeof e === "string" ? e : JSON.stringify(e)}\n\n`).join("");
const eventStream: Array<[string, string]> = [["Content-Type", "text/event-stream"]];
const text = (t: string, extra: Record<string, unknown> = {}) => ({ candidates: [{ content: { parts: [{ text: t }], role: "model" }, ...extra }] });
const stop = { candidates: [{ content: { parts: [{ text: "!" }], role: "model" }, finishReason: "STOP" }] };

const scenarios: Scenario[] = [
	{
		// google-raw-stop-reason.test.ts › 'forwards each SDK chunk in order before
		// normalizing it' (002fc8385), Generative AI case, with the chunks on the wire.
		name: "upstream two chunks",
		framing: "chunked",
		headers: eventStream,
		segments: [
			sse({ responseId: "resp_google", candidates: [{ content: { parts: [{ text: "hello" }] } }] }),
			sse({
				candidates: [{ finishReason: "STOP" }],
				usageMetadata: { promptTokenCount: 2, candidatesTokenCount: 1, totalTokenCount: 3 },
			}),
		],
	},
	{
		name: "converter keeps its own fields in its own order",
		note: "wire order scrambled; unknown fields at every level; null dropped, falsy kept; citationSources renamed; a wire sdkHttpResponse keeps the first slot",
		framing: "close",
		headers: eventStream,
		segments: [
			sse(
				'{"usageMetadata":{"totalTokenCount":5,"promptTokenCount":2,"futureCount":{"z":1,"a":2}},"unknownTop":{"b":1,"a":2},"responseId":"","candidates":[{"unknownCand":1,"urlContextMetadata":{"u":1},"safetyRatings":[{"category":"C","probability":"LOW"}],"logprobsResult":{"x":[1]},"index":0,"avgLogprobs":0,"groundingMetadata":{"g":true},"finishReason":null,"tokenCount":0,"citationMetadata":{"other":1,"citationSources":[{"startIndex":1,"uri":"u"}]},"content":{"role":"model","parts":[{"thoughtSignature":"c2ln","text":"a","extraPart":true}]}}],"modelStatus":{"m":1},"promptFeedback":{"blockReason":null,"safetyRatings":[]},"modelVersion":"gemini-x","error":{"code":1}}',
				'{"sdkHttpResponse":{"wire":true},"candidates":[{"content":{"parts":[{"text":"b"}]},"citationMetadata":{"citationSources":null,"other":2}}],"responseId":null}',
				'{"candidates":[{"content":{"parts":[{"text":"c"}]},"citationMetadata":"not an object"}]}',
				stop,
			),
		],
	},
	{
		name: "candidates that are not an array or not objects",
		note: "a non-array candidates is copied as-is; each non-object candidate converts to {}; pi normalizes only candidates[0]",
		framing: "close",
		headers: eventStream,
		segments: [
			sse(
				'{"candidates":{"only":{"content":{"parts":[{"text":"hidden"}]}}}}',
				'{"candidates":"str"}',
				'{"candidates":[null,"s",5,true,[1],{}]}',
				'{"candidates":[{"content":{"parts":[{"text":"x"}]}},{"content":{"parts":[{"text":"second candidate"}]}}],"responseId":"r1"}',
				stop,
			),
		],
	},
	{
		name: "non-object data values",
		note: "null, scalars and arrays convert to an object holding only sdkHttpResponse",
		framing: "close",
		headers: eventStream,
		segments: [sse("null", "5", '"s"', "true", "false", '[{"candidates":[]}]', "{}", stop)],
	},
	{
		name: "an SSE-framed error event is observed and the stream continues",
		note: "parity-sweep-2 F3 once recorded that this throws; @google/genai only checks whole body reads that are bare JSON",
		framing: "close",
		headers: eventStream,
		segments: [
			sse(text("before "), '{"error":{"code":429,"message":"quota exceeded","status":"RESOURCE_EXHAUSTED"}}', text("after"), stop),
		],
	},
	{
		name: "an SSE-framed error event as the last event",
		note: "an error event with nothing after it: no throw, so the stream ends without a finish reason",
		framing: "close",
		headers: eventStream,
		segments: [
			sse(text("partial"), '{"error":{"code":429,"message":"quota exceeded","status":"RESOURCE_EXHAUSTED"}}'),
		],
	},
	{
		name: "response header record",
		note: "lowercase names in sorted order; repeats joined with ', '; set-cookie keeps the last; empty values kept; bytes read as latin1; no content-type means the per-chunk Response injects text/plain",
		framing: "chunked",
		headers: [
			["X-Multi", "a"],
			["x-multi", "b"],
			["Set-Cookie", "c=1"],
			["Set-Cookie", "d=2"],
			["X-Empty", ""],
			["Alt-Svc", "h3=\":443\""],
			["X-Goog-Trace", "  padded  "],
			["Server-Timing", "gfet4t7; dur=100"],
			["X-Latin", "café"],
		],
		segments: [sse(text("h")), sse(stop)],
	},
	{
		name: "response content-type is kept as sent",
		framing: "close",
		headers: [
			["content-type", "text/event-stream; charset=UTF-8"],
			["Vary", "Origin"],
			["vary", "X-Origin"],
			["Vary", "Referer"],
		],
		segments: [sse(stop)],
	},
	{
		name: "thinking, text and a function call",
		note: "nested key order survives in the observed value, the tool-call delta and the arguments",
		framing: "chunked",
		headers: eventStream,
		segments: [
			sse({ candidates: [{ content: { parts: [{ text: "plan", thought: true, thoughtSignature: "dGhpbms=" }], role: "model" } }] }),
			sse(
				'{"candidates":[{"content":{"parts":[{"text":"calling"},{"functionCall":{"id":"call_1","name":"lookup","args":{"zeta":1,"alpha":{"y":[{"b":1,"a":2}],"x":null},"mid":"m"}},"thoughtSignature":"c2ln"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"thoughtsTokenCount":2,"cachedContentTokenCount":1,"totalTokenCount":18}}',
			),
		],
	},
	{
		name: "array-index keys come first at every depth",
		note: "a JS object lists its array-index keys first, ascending: in the observed value, the tool-call delta and the arguments",
		framing: "close",
		headers: eventStream,
		segments: [
			sse(
				'{"candidates":[{"content":{"parts":[{"functionCall":{"id":"c1","name":"f","args":{"b":1,"0":2,"a":{"z":1,"1":2},"10":[{"y":1,"3":0}],"2":"x"}}}],"role":"model","7":"seven"},"finishReason":"STOP"}],"usageMetadata":{"totalTokenCount":3,"0":9,"promptTokenCount":2}}',
			),
		],
	},
	{
		name: "a number past float64's range",
		note: "JSON.parse reads it as Infinity, which JSON.stringify writes null: in the observed value, the tool-call delta and the arguments",
		framing: "close",
		headers: eventStream,
		segments: [
			sse(
				'{"responseId":"r1","candidates":[{"content":{"parts":[{"text":"hi"}]}}],"usageMetadata":{"promptTokenCount":2,"futureCount":1e400}}',
				'{"candidates":[{"content":{"parts":[{"functionCall":{"id":"c1","name":"f","args":{"x":1e400,"y":-1e400,"z":1e-400}}}]},"finishReason":"STOP"}]}',
			),
		],
	},
	{
		name: "a gzip body",
		note: "the SDK sees the decoded text; content-encoding stays in the record",
		framing: "close",
		encode: (b) => zlib.gzipSync(b),
		headers: [
			["Content-Type", "text/event-stream"],
			["Content-Encoding", "gzip"],
		],
		segments: [sse(text("zipped"), stop)],
	},
	{
		name: "a gzip body with a content-length",
		note: "fetch undoes the coding and keeps content-encoding and content-length in the record",
		framing: "close",
		encode: (b) => zlib.gzipSync(b),
		contentLength: true,
		headers: [
			["Content-Type", "text/event-stream"],
			["Content-Encoding", "gzip"],
		],
		segments: [sse(text("zipped"), stop)],
	},
	{
		name: "a gzip body when the caller names its own accept-encoding",
		note: "fetch sends the caller's accept-encoding and still undoes the coding",
		framing: "chunked",
		encode: (b) => zlib.gzipSync(b),
		requestHeaders: { "Accept-Encoding": "gzip" },
		headers: [
			["Content-Type", "text/event-stream"],
			["Content-Encoding", "gzip"],
		],
		segments: [sse(text("zipped"), stop)],
	},
	{
		name: "an x-gzip body",
		framing: "close",
		encode: (b) => zlib.gzipSync(b),
		headers: [
			["Content-Type", "text/event-stream"],
			["Content-Encoding", "x-gzip"],
		],
		segments: [sse(text("zipped"), stop)],
	},
	{
		name: "a zlib deflate body",
		framing: "close",
		encode: (b) => zlib.deflateSync(b),
		headers: [
			["Content-Type", "text/event-stream"],
			["Content-Encoding", "deflate"],
		],
		segments: [sse(text("deflated"), stop)],
	},
	{
		name: "a raw deflate body",
		note: "undici tells raw deflate from zlib by the first byte",
		framing: "close",
		encode: (b) => zlib.deflateRawSync(b),
		headers: [
			["Content-Type", "text/event-stream"],
			["Content-Encoding", "Deflate"],
		],
		segments: [sse(text("raw"), stop)],
	},
	{
		name: "two codings are undone last first",
		framing: "close",
		encode: (b) => zlib.gzipSync(zlib.deflateSync(b)),
		headers: [
			["Content-Type", "text/event-stream"],
			["Content-Encoding", "deflate"],
			["Content-Encoding", " GZIP "],
		],
		segments: [sse(text("twice"), stop)],
	},
	{
		name: "an unknown coding leaves the body as sent",
		framing: "close",
		headers: [
			["Content-Type", "text/event-stream"],
			["Content-Encoding", "identity"],
		],
		segments: [sse(text("plain"), stop)],
	},
	{
		name: "six content codings fail the request",
		note: "undici rejects the fetch past five codings",
		framing: "close",
		encode: (b) => [1, 2, 3, 4, 5, 6].reduce((x) => zlib.gzipSync(x), b),
		headers: [
			["Content-Type", "text/event-stream"],
			["Content-Encoding", "gzip, gzip, gzip, gzip, gzip, gzip"],
		],
		segments: [sse(text("never"), stop)],
	},
	{
		name: "a truncated gzip body reads what it decodes",
		note: "undici's gunzip finishes with Z_SYNC_FLUSH, so a cut-off stream ends without an error",
		framing: "close",
		encode: (b) => zlib.gzipSync(b).subarray(0, -12),
		headers: [
			["Content-Type", "text/event-stream"],
			["Content-Encoding", "gzip"],
		],
		segments: [sse(text("kept"), stop, text("tail"))],
	},
	{
		name: "a gzip body with a bad checksum fails the read",
		framing: "close",
		encode: (b) => {
			const z = zlib.gzipSync(b);
			z[z.length - 8] ^= 1; // the CRC-32's low byte
			return z;
		},
		headers: [
			["Content-Type", "text/event-stream"],
			["Content-Encoding", "gzip"],
		],
		segments: [sse(text("checked"))],
	},
	{
		name: "the connection drops mid-body",
		note: "the body read rejects with undici's TypeError: terminated",
		framing: "chunked",
		abruptEnd: true,
		headers: eventStream,
		segments: [sse(text("before the drop"))],
	},
	{
		name: "connection close on a chunked body",
		framing: "chunked",
		headers: [...eventStream, ["Connection", "close"]],
		segments: [sse(stop)],
	},
	{
		name: "a trailer on a chunked body",
		framing: "chunked",
		headers: [...eventStream, ["Trailer", "X-T"]],
		segments: [sse(stop)],
	},
	{
		name: "divergence: connection close on a close-delimited body",
		divergence: "net/http deletes a Connection header carrying close, and a close-delimited body closes either way, so the port cannot tell the header was there: pi's record has connection: close, the port's does not",
		divergentFields: ["events"],
		framing: "close",
		headers: [...eventStream, ["Connection", "close"]],
		segments: [sse(stop)],
	},
	{
		name: "divergence: a trailer named in lowercase",
		divergence: "net/http moves the Trailer header into Response.Trailer under canonical names, so the port's record has trailer: X-T where pi's keeps the text sent, x-t",
		divergentFields: ["events"],
		framing: "chunked",
		headers: [...eventStream, ["Trailer", "x-t"]],
		segments: [sse(stop)],
	},
	{
		name: "divergence: a brotli body the caller asked for",
		divergence: "undici undoes br (and zstd); Go's standard library has no brotli or zstd decoder, so the port fails the stream where pi reads it",
		divergentFields: ["stream", "events", "stop", "content"],
		framing: "close",
		encode: (b) => zlib.brotliCompressSync(b),
		requestHeaders: { "Accept-Encoding": "br" },
		headers: [
			["Content-Type", "text/event-stream"],
			["Content-Encoding", "br"],
		],
		segments: [sse(text("brotli"), stop)],
	},
	{
		name: "a bare JSON error in its own read throws",
		framing: "close",
		headers: eventStream,
		segments: [sse(text("x")), '{"error":{"code":500,"message":"internal","status":"INTERNAL"}}'],
	},
	{
		name: "a bare JSON error sharing a read with an event does not",
		note: "an event and a bare JSON error in one write: the read is not JSON, so the tail is an incomplete segment",
		framing: "close",
		headers: eventStream,
		segments: [sse(text("x")) + '{"error":{"code":500,"message":"internal","status":"INTERNAL"}}'],
	},
	{
		name: "divergence: HTTP chunks that arrive in one read",
		divergence: "undici hands the SDK one HTTP chunk per body read even when several arrive in one TCP segment, so pi checks the bare-JSON chunk alone and throws ApiError; Go's chunked reader returns every buffered chunk in one Read, so the port checks the event and the error together, finds no JSON and ends with \"Incomplete JSON segment at the end\"",
		divergentFields: ["stop"],
		framing: "chunked",
		oneWrite: true,
		headers: eventStream,
		segments: [sse(text("x")), '{"error":{"code":500,"message":"internal","status":"INTERNAL"}}'],
	},
	{
		name: "a bare JSON error is quoted re-serialized",
		note: "the message carries JSON.stringify of the parsed read, not its text",
		framing: "close",
		headers: eventStream,
		segments: ['{\n  "error": {\n    "code": 503,\n    "message": "over\\u006coaded",\n    "status": "UNAVAILABLE"\n  },\n  "extra": [1.50, 1e2]\n}\n'],
	},
	{
		name: "a bare JSON error with a string code throws",
		note: "code >= 400 && code < 600 coerces the string",
		framing: "close",
		headers: eventStream,
		segments: [sse(text("x")), '{"error":{"code":"500","status":"INTERNAL"}}'],
	},
	{
		name: "a bare JSON error without a status",
		framing: "close",
		headers: eventStream,
		segments: [sse(text("x")), '{"error":{"code":404}}'],
	},
	{
		name: "a bare JSON error with a non-string status",
		framing: "close",
		headers: eventStream,
		segments: [sse(text("x")), '{"error":{"code":[429],"status":{"s":1}}}'],
	},
	{
		name: "a bare JSON error is quoted in JSON.parse's key order",
		note: "JSON.stringify(chunkJson) lists array-index keys first, ascending, and a repeated key once",
		framing: "close",
		headers: eventStream,
		segments: [sse(text("x")), '{"error":{"code":500,"status":"INTERNAL","9":1,"message":"m","message":"last"},"b":1,"10":2,"2":3}'],
	},
	{
		name: "a bare JSON error quotes a number past float64's range as null",
		framing: "close",
		headers: eventStream,
		segments: [sse(text("x")), '{"error":{"code":500,"status":"S"},"z":1e400,"a":[-1e400,1e-400]}'],
	},
	{
		name: "a bare JSON error reads its status after a JSON round trip",
		note: "status comes from JSON.parse(JSON.stringify(chunkJson.error)), where Infinity is already null",
		framing: "close",
		headers: eventStream,
		segments: [sse(text("x")), '{"error":{"code":500,"status":1e400}}'],
	},
	{
		name: "a bare JSON error reads an array status after a JSON round trip",
		framing: "close",
		headers: eventStream,
		segments: [sse(text("x")), '{"error":{"code":"503","status":[1e400,"s",2]}}'],
	},
	{
		name: "a bare JSON error with code 399 does not throw",
		framing: "close",
		headers: eventStream,
		segments: [sse(text("x")), '{"error":{"code":399,"status":"S"}}'],
	},
	{
		name: "a bare JSON error with code 400 throws",
		framing: "close",
		headers: eventStream,
		segments: [sse(text("x")), '{"error":{"code":400,"status":"S"}}'],
	},
	{
		name: "a bare JSON error with code 599 throws",
		framing: "close",
		headers: eventStream,
		segments: [sse(text("x")), '{"error":{"code":599,"status":"S"}}'],
	},
	{
		name: "a bare JSON error with code 600 does not throw",
		framing: "close",
		headers: eventStream,
		segments: [sse(text("x")), '{"error":{"code":600,"status":"S"}}'],
	},
	{
		name: "a bare JSON error outside 400..599 does not throw",
		framing: "close",
		headers: eventStream,
		segments: [sse(text("x")), '{"error":{"code":200,"status":"OK"}}'],
	},
	{
		name: "a bare JSON read with a null error does not throw",
		framing: "close",
		headers: eventStream,
		segments: [sse(text("x")), '{"error":null}'],
	},
	{
		name: "a bare JSON read that is an array does not throw",
		framing: "close",
		headers: eventStream,
		segments: [sse(text("x")), '[{"error":{"code":500,"status":"INTERNAL"}}]'],
	},
	{
		name: "a bare JSON error followed by a delimiter",
		note: "JSON.parse accepts the trailing blank line; the throw precedes buffering",
		framing: "close",
		headers: eventStream,
		segments: [sse(text("x")), '{"error":{"code":429,"status":"RESOURCE_EXHAUSTED"}}\n\n'],
	},
	{
		name: "a truncated stream fails",
		note: "TestGoogleTruncatedStreamFails's fixture",
		framing: "close",
		headers: eventStream,
		segments: [sse(text("x")) + 'data: {"candidates":[{"content":{"par'],
	},
	{
		name: "an unknown finish reason fails inside the loop",
		note: "the throw leaves the open text block without a text_end",
		framing: "close",
		headers: eventStream,
		segments: [sse(text("t", { finishReason: "SOMETHING_NEW" }))],
	},
	{
		name: "an abort from the callback fails the next read",
		note: "the chunk in hand is still normalized; the next read rejects with undici's AbortError",
		framing: "close",
		headers: eventStream,
		segments: [sse(text("in hand")), sse(stop)],
		abortOn: 0,
	},
	{
		name: "the callback's error fails the stream",
		framing: "close",
		headers: eventStream,
		segments: [sse(text("never seen"), stop)],
		throwOn: 0,
		throwMessage: "observer boom",
	},
	{
		name: "the callback's error mid-stream keeps what came before",
		framing: "close",
		headers: eventStream,
		segments: [sse(text("kept"), text(" dropped"), stop)],
		throwOn: 1,
		throwMessage: "second event refused",
	},
	{
		name: "an empty data payload fails the stream",
		note: "pi reads every data: payload with Response.json() (strict JSON.parse); its SyntaxError fails the stream",
		framing: "close",
		headers: eventStream,
		segments: [sse(text("before")) + "data:\n\n" + sse(stop)],
	},
	{
		name: "an unparseable data payload fails the stream",
		framing: "close",
		headers: eventStream,
		segments: ["data: {not json}\n\n" + sse(stop)],
	},
	{
		name: "a multi-line data event fails the stream",
		note: "the SDK parses everything after the first \"data:\" as one payload",
		framing: "close",
		headers: eventStream,
		segments: [`data: {"candidates":[]}\ndata: {"candidates":[]}\n\n` + sse(stop)],
	},
	{
		name: "a raw control character in a string fails the stream",
		framing: "close",
		headers: eventStream,
		segments: ['data: {"candidates":[{"content":{"parts":[{"text":"a\tb"}]}}]}\n\n' + sse(stop)],
	},
	{
		name: "a long unparseable payload quotes the text around the error",
		framing: "close",
		headers: eventStream,
		segments: [sse(text("kept")) + 'data: {"candidates":[{"content":{"parts":[{"text":"x"}]}}],"usageMetadata":{"promptTokenCount":NaN}}\n\n' + sse(stop)],
	},
	{
		name: "a payload with a bad unicode escape on its second line fails the stream",
		framing: "close",
		headers: eventStream,
		segments: ['data: {"candidates":[{"content":{"parts":[\n{"text":"\\u12x4"}]}}]}\n\n' + sse(stop)],
	},
	{
		name: "a truncated payload fails the stream",
		framing: "close",
		headers: eventStream,
		segments: ['data: {"candidates":[{"content":{"parts":[{"text":"x"}\n\n' + sse(stop)],
	},
	{
		name: "a string token count is read as a number",
		note: "pi reads each chunk untyped: \"2\" - 0 is 2, and the rest of the chunk still counts",
		framing: "close",
		headers: eventStream,
		segments: [sse('{"responseId":"r","candidates":[{"content":{"parts":[{"text":"t"}]}}],"usageMetadata":{"promptTokenCount":"2"}}', stop)],
	},
	{
		name: "boolean and array token counts, then a usage that is not an object",
		note: "true - 0 is 1 and [3] + 0 is \"30\"; a later truthy non-object usage reads every count as 0",
		framing: "close",
		headers: eventStream,
		segments: [sse('{"candidates":[{"content":{"parts":[{"text":"t"}]}}],"usageMetadata":{"promptTokenCount":true,"candidatesTokenCount":[3]}}', '{"candidates":[{"finishReason":"STOP"}],"usageMetadata":"str"}')],
	},
	{
		name: "a falsy usage keeps the previous one",
		framing: "close",
		headers: eventStream,
		segments: [sse('{"candidates":[{"content":{"parts":[{"text":"t"}]}}],"usageMetadata":{"promptTokenCount":4,"totalTokenCount":4}}', '{"candidates":[{"finishReason":"STOP"}],"usageMetadata":0}')],
	},
	{
		name: "an empty response id gives way to a later one",
		note: "output.responseId ||= chunk.responseId",
		framing: "close",
		headers: eventStream,
		segments: [sse('{"responseId":"","candidates":[{"content":{"parts":[{"text":"t"}]}}]}', '{"responseId":"r2","candidates":[{"content":{"parts":[{"text":"!"}]},"finishReason":"STOP"}]}', '{"responseId":"r3"}')],
	},
	{
		name: "a candidates object is read by its 0 key",
		note: "the SDK copies a non-array candidates as it is, and chunk.candidates?.[0] reads its \"0\" member",
		framing: "close",
		headers: eventStream,
		segments: [sse('{"candidates":{"0":{"content":{"parts":[{"text":"via key"}]},"finishReason":"STOP"}}}')],
	},
	{
		name: "candidates, content and parts of other types read nothing",
		note: "a string candidates' [0] is a letter, a string content has no parts, a string parts iterates letters, and parts that are not objects carry no text",
		framing: "close",
		headers: eventStream,
		segments: [
			sse(
				'{"candidates":"str"}',
				'{"candidates":[{"content":"x"}]}',
				'{"candidates":[{"content":{"parts":"ab"}}]}',
				'{"candidates":[{"content":{"parts":["s",5,true,[1]]}}]}',
				stop,
			),
		],
	},
	{
		name: "parts that is an object is not iterable",
		framing: "close",
		headers: eventStream,
		segments: [sse(text("a"), '{"candidates":[{"content":{"parts":{"text":"x"}}}]}', stop)],
	},
	{
		name: "a null part fails reading its text",
		framing: "close",
		headers: eventStream,
		segments: [sse(text("a"), '{"candidates":[{"content":{"parts":[null]}}]}', stop)],
	},
	{
		name: "text with its own toString fails after the block opens",
		note: "text_start is pushed before block.text += part.text throws",
		framing: "close",
		headers: eventStream,
		segments: [sse('{"candidates":[{"content":{"parts":[{"text":{"toString":1}}]}}]}', stop)],
	},
	{
		name: "a thought that is not true and a thoughtSignature that is not a string",
		note: "isThinkingPart is part.thought === true; retainThoughtSignature keeps only a non-empty string",
		framing: "close",
		headers: eventStream,
		segments: [sse('{"candidates":[{"content":{"parts":[{"text":"a","thought":"true","thoughtSignature":5}]}}]}', stop)],
	},
	{
		name: "a later part without a signature keeps the block's signature",
		note: "retainThoughtSignature keeps the last non-empty string: for thinking as for text",
		framing: "close",
		headers: eventStream,
		segments: [
			sse(
				'{"candidates":[{"content":{"parts":[{"text":"a","thought":true,"thoughtSignature":"c2ln"}]}}]}',
				'{"candidates":[{"content":{"parts":[{"text":"b","thought":true},{"text":"c","thought":true,"thoughtSignature":""}]}}]}',
				'{"candidates":[{"content":{"parts":[{"text":"x","thoughtSignature":"dHh0"},{"text":"y"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":3,"thoughtsTokenCount":2,"cachedContentTokenCount":4,"totalTokenCount":12}}',
			),
		],
	},
	{
		name: "a finish reason that is a number",
		framing: "close",
		headers: eventStream,
		segments: [sse('{"candidates":[{"content":{"parts":[{"text":"t"}]},"finishReason":5}]}')],
	},
	{
		name: "a finish reason that is an array",
		note: "mapStopReason compares with ===, then the template literal joins the array",
		framing: "close",
		headers: eventStream,
		segments: [sse('{"candidates":[{"content":{"parts":[{"text":"t"}]},"finishReason":["STOP"]}]}')],
	},
	{
		name: "a finish reason with its own toString",
		framing: "close",
		headers: eventStream,
		segments: [sse('{"candidates":[{"content":{"parts":[{"text":"t"}]},"finishReason":{"toString":1}}]}')],
	},
	{
		name: "divergence: token counts pi keeps as strings",
		divergence: "pi's usage keeps output \"21\" (\"2\" + 1), cacheRead \"1\" and totalTokens \"8\" as strings; an ai.Usage count is an int, holding 21, 1 and 8 (the costs pi computes from those strings are numbers, and match)",
		divergentFields: ["usage.output", "usage.cacheRead", "usage.totalTokens"],
		framing: "close",
		headers: eventStream,
		segments: [sse('{"candidates":[{"content":{"parts":[{"text":"t"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"cachedContentTokenCount":"1","candidatesTokenCount":"2","thoughtsTokenCount":1,"totalTokenCount":"8"}}')],
	},
	{
		name: "divergence: fractional and infinite token counts",
		divergence: "pi's usage keeps input Infinity (JSON null) and totalTokens 3.7, so its cost.input and cost.total are Infinity too; an ai.Usage count is an int, holding 0 and 3, and the port's costs are finite",
		divergentFields: ["usage.input", "usage.totalTokens", "usage.cost.input", "usage.cost.total"],
		framing: "close",
		headers: eventStream,
		segments: [sse('{"candidates":[{"content":{"parts":[{"text":"t"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1e400,"candidatesTokenCount":1,"totalTokenCount":3.7}}')],
	},
	{
		name: "divergence: a numeric response id",
		divergence: "pi's responseId is the number 5; the port's ResponseID string holds \"5\"",
		divergentFields: ["responseId"],
		framing: "close",
		headers: eventStream,
		segments: [sse('{"responseId":5,"candidates":[{"content":{"parts":[{"text":"t"}]}}]}', '{"responseId":"later","candidates":[{"content":{"parts":[{"text":"!"}]},"finishReason":"STOP"}]}')],
	},
	{
		name: "divergence: text deltas that are not strings",
		divergence: "pi's text_delta carries part.text as it came (null, 5, an array); the port's Delta string holds the text appended (\"null\", \"5\", \"1,2,,[object Object]\"), which the content matches",
		divergentFields: ["stream"],
		framing: "close",
		headers: eventStream,
		segments: [sse('{"candidates":[{"content":{"parts":[{"text":null},{"text":5},{"text":[1,[2,null],{}]}]}}]}', stop)],
	},
	{
		name: "divergence: tool-call fields of other types",
		divergence: "pi's tool call keeps id 7, name 5, thoughtSignature 7 and arguments [1,{\"b\":2}] / \"s\" as they came; the port's strings hold \"7\", \"5\" and \"7\" and its Arguments map is {} (the toolcall_delta text matches)",
		divergentFields: ["content"],
		framing: "close",
		headers: eventStream,
		segments: [
			sse(
				'{"candidates":[{"content":{"parts":[{"functionCall":{"id":7,"name":5,"args":[1,{"b":2}]},"thoughtSignature":7},{"functionCall":{"id":"c2","name":"f","args":"s"}},{"functionCall":{"id":"c3","name":"","args":null}}]},"finishReason":"STOP"}]}',
			),
		],
	},
];

function serve(
	s: Scenario,
	segments: string[],
	encoded: Buffer | undefined,
): Promise<{ port: number; close: () => void; acceptEncoding: () => string | undefined }> {
	let acceptEncoding: string | undefined;
	return new Promise((resolve) => {
		const server = net.createServer((sock) => {
			let buf = Buffer.alloc(0);
			let responded = false;
			sock.on("data", async (d) => {
				buf = Buffer.concat([buf, d]);
				const headEnd = buf.indexOf("\r\n\r\n");
				if (responded || headEnd < 0) return;
				const requestHead = buf.subarray(0, headEnd).toString();
				const len = Number(/content-length:\s*(\d+)/i.exec(requestHead)?.[1] ?? 0);
				if (buf.length < headEnd + 4 + len) return;
				responded = true;
				acceptEncoding = /^accept-encoding:[ \t]*(.*?)[ \t]*$/im.exec(requestHead)?.[1];
				const writes: Buffer[] = encoded ? [encoded] : segments.map((seg) => Buffer.from(seg));
				const head = ["HTTP/1.1 200 OK", ...s.headers.map(([k, v]) => `${k}: ${v}`)];
				if (s.contentLength) head.push(`Content-Length: ${writes.reduce((n, w) => n + w.length, 0)}`);
				if (s.framing === "chunked") head.push("Transfer-Encoding: chunked");
				sock.write(`${head.join("\r\n")}\r\n\r\n`);
				const frame = (w: Buffer) =>
					s.framing === "chunked" ? Buffer.concat([Buffer.from(`${w.length.toString(16)}\r\n`), w, Buffer.from("\r\n")]) : w;
				for (const w of s.oneWrite ? [Buffer.concat(writes.map(frame))] : writes.map(frame)) {
					await new Promise((r) => setTimeout(r, 50));
					sock.write(w);
				}
				await new Promise((r) => setTimeout(r, 50));
				if (s.abruptEnd) {
					sock.destroy();
					return;
				}
				if (s.framing === "chunked") sock.write("0\r\n\r\n");
				sock.end();
			});
		});
		server.listen(0, "127.0.0.1", () => {
			resolve({
				port: (server.address() as net.AddressInfo).port,
				close: () => server.close(),
				acceptEncoding: () => acceptEncoding,
			});
		});
	});
}

// A literal model, so the capture does not need the generated catalog; the
// Go tests build the same one.
const googleModel = {
	id: "gemini-2.5-flash",
	name: "Gemini 2.5 Flash",
	api: "google-generative-ai",
	provider: "google",
	reasoning: true,
	input: ["text", "image"],
	cost: { input: 0.3, output: 2.5, cacheRead: 0.03, cacheWrite: 0 },
	contextWindow: 1048576,
	maxTokens: 65536,
};
const context = { messages: [{ role: "user", content: "hi", timestamp: 1 }] };

async function run(s: Scenario, segments: string[], encoded: Buffer | undefined) {
	const { port, close, acceptEncoding } = await serve(s, segments, encoded);
	const model = { ...googleModel, baseUrl: `http://127.0.0.1:${port}` };
	const events: string[] = [];
	let sameModel = true;
	let calls = 0;
	const controller = new AbortController();
	const out = stream(model, context, {
		apiKey: "test-api-key",
		signal: controller.signal,
		...(s.requestHeaders && { headers: s.requestHeaders }),
		onProviderStreamEvent: async (data: unknown, eventModel: unknown) => {
			const call = calls++;
			if (eventModel !== model) sameModel = false;
			if (s.throwOn === call) throw new Error(s.throwMessage);
			events.push(JSON.stringify(data));
			if (s.abortOn === call) controller.abort();
		},
	});
	const streamed: Array<{ type: string; delta?: string }> = [];
	for await (const ev of out) streamed.push("delta" in ev ? { type: ev.type, delta: ev.delta } : { type: ev.type });
	const msg = await out.result();
	close();
	return {
		acceptEncoding: acceptEncoding(),
		events,
		sameModel,
		stream: streamed,
		stopReason: msg.stopReason,
		...(msg.errorMessage !== undefined && { errorMessage: msg.errorMessage }),
		...(msg.responseId !== undefined && { responseId: msg.responseId }),
		content: JSON.stringify(msg.content),
		usage: {
			input: msg.usage.input,
			output: msg.usage.output,
			cacheRead: msg.usage.cacheRead,
			cacheWrite: msg.usage.cacheWrite,
			reasoning: msg.usage.reasoning,
			totalTokens: msg.usage.totalTokens,
			cost: msg.usage.cost,
		},
	};
}

const results = [];
for (const s of scenarios) {
	const encoded = s.encode?.(Buffer.from(s.segments.join("")));
	const pi = await run(s, s.segments, encoded);
	const joined = await run(s, [s.segments.join("")], encoded);
	const { throwOn, throwMessage, abortOn, encode, ...scenario } = s;
	results.push({
		...scenario,
		...(encoded && { encodedBody: encoded.toString("base64") }),
		...(throwOn !== undefined && { throwOn, throwMessage }),
		...(abortOn !== undefined && { abortOn }),
		readBoundariesMatter: JSON.stringify(pi) !== JSON.stringify(joined),
		pi,
	});
}

const out = {
	source: `upstream ${sha} packages/ai/src/api/google-generative-ai.ts (onProviderStreamEvent, 002fc8385), @google/genai ${genaiVersion} ${locked.integrity}, node ${process.version}`,
	scenarios: results,
};
fs.writeFileSync(outFile, `${JSON.stringify(out, null, "\t")}\n`);
console.log(`wrote ${results.length} scenarios to ${outFile}`);
