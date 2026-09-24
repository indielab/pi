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
// segment reaches the SDK as its own network read (its bare-JSON error check
// runs per read). Nothing else is added to the head — no Date, no
// Connection — so the header record the SDK builds is fully determined by the
// scenario. `framing` is "close" (the body runs to EOF) or "chunked" (each
// segment is one HTTP chunk); `gzip` compresses the whole body into one write.
//
// Every scenario runs twice: as separate reads, and with its segments joined
// into one write. `readBoundariesMatter` records whether the two differ; the
// recorded outcome is the separate-read one.
//
// Recorded per scenario: `events`, JSON.stringify of each value the callback
// received (key order is part of the contract); `sameModel`, whether every
// call got the model object pi was called with; `stream`, the assistant
// stream's events as type and delta; and the final message's stopReason,
// errorMessage, responseId, content (as JSON text) and usage token counts.
//
// A scenario with a `divergence` is a measured difference the port carries:
// recorded for the ledger, not replayed.
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
	gzip?: boolean;
	divergence?: string;
	// Throw Error(throwMessage) from the callback on its Nth call (0-based).
	throwOn?: number;
	throwMessage?: string;
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
		note: "parity-sweep-2 F3 recorded that this throws; @google/genai only checks whole network reads that are bare JSON",
		framing: "close",
		headers: eventStream,
		segments: [
			sse(text("before "), '{"error":{"code":429,"message":"quota exceeded","status":"RESOURCE_EXHAUSTED"}}', text("after"), stop),
		],
	},
	{
		name: "an SSE-framed error event as the last event",
		note: "TestGoogleErrorChunkFailsStream's fixture: no throw, so the stream ends without a finish reason",
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
		name: "a gzip body",
		note: "the SDK sees the decoded text; content-encoding stays in the record",
		framing: "close",
		gzip: true,
		headers: [
			["Content-Type", "text/event-stream"],
			["Content-Encoding", "gzip"],
		],
		segments: [sse(text("zipped"), stop)],
	},
	{
		name: "a bare JSON error in its own read throws",
		framing: "close",
		headers: eventStream,
		segments: [sse(text("x")), '{"error":{"code":500,"message":"internal","status":"INTERNAL"}}'],
	},
	{
		name: "a bare JSON error sharing a read with an event does not",
		note: "TestGoogleBareJSONErrorChunkFailsStream's fixture as one write: the read is not JSON, so the tail is an incomplete segment",
		framing: "close",
		headers: eventStream,
		segments: [sse(text("x")) + '{"error":{"code":500,"message":"internal","status":"INTERNAL"}}'],
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
		name: "divergence: an empty data payload",
		divergence: "pi parses every data payload with Response.json() (strict JSON.parse) and fails the stream with V8's SyntaxError text; the port skips an empty payload",
		framing: "close",
		headers: eventStream,
		segments: ["data:\n\n" + sse(stop)],
	},
	{
		name: "divergence: an unparseable data payload",
		divergence: "pi fails the stream with V8's SyntaxError text; the port skips a payload it cannot parse",
		framing: "close",
		headers: eventStream,
		segments: ["data: {not json}\n\n" + sse(stop)],
	},
	{
		name: "divergence: a multi-line data event",
		divergence: "the SDK parses everything after the first \"data:\" as one payload, so pi fails with V8's SyntaxError text; the port skips it",
		framing: "close",
		headers: eventStream,
		segments: [`data: {"candidates":[]}\ndata: {"candidates":[]}\n\n` + sse(stop)],
	},
	{
		name: "divergence: a raw control character in a string",
		divergence: "pi fails with V8's SyntaxError text; the port repairs the string and handles the event",
		framing: "close",
		headers: eventStream,
		segments: ['data: {"candidates":[{"content":{"parts":[{"text":"a\tb"}]}}]}\n\n' + sse(stop)],
	},
	{
		name: "divergence: a field of an unexpected type",
		divergence: "pi reads the fields it can; the port's typed decode drops the whole event",
		framing: "close",
		headers: eventStream,
		segments: [sse('{"responseId":"r","candidates":[{"content":{"parts":[{"text":"t"}]}}],"usageMetadata":{"promptTokenCount":"2"}}', stop)],
	},
];

function serve(s: Scenario, segments: string[]): Promise<{ port: number; close: () => void }> {
	return new Promise((resolve) => {
		const server = net.createServer((sock) => {
			let buf = Buffer.alloc(0);
			let responded = false;
			sock.on("data", async (d) => {
				buf = Buffer.concat([buf, d]);
				const headEnd = buf.indexOf("\r\n\r\n");
				if (responded || headEnd < 0) return;
				const len = Number(/content-length:\s*(\d+)/i.exec(buf.subarray(0, headEnd).toString())?.[1] ?? 0);
				if (buf.length < headEnd + 4 + len) return;
				responded = true;
				const head = ["HTTP/1.1 200 OK", ...s.headers.map(([k, v]) => `${k}: ${v}`)];
				if (s.framing === "chunked") head.push("Transfer-Encoding: chunked");
				sock.write(`${head.join("\r\n")}\r\n\r\n`);
				const writes: Buffer[] = s.gzip
					? [zlib.gzipSync(Buffer.from(segments.join("")))]
					: segments.map((seg) => Buffer.from(seg));
				for (const w of writes) {
					await new Promise((r) => setTimeout(r, 50));
					sock.write(s.framing === "chunked" ? Buffer.concat([Buffer.from(`${w.length.toString(16)}\r\n`), w, Buffer.from("\r\n")]) : w);
				}
				await new Promise((r) => setTimeout(r, 50));
				if (s.framing === "chunked") sock.write("0\r\n\r\n");
				sock.end();
			});
		});
		server.listen(0, "127.0.0.1", () => {
			resolve({ port: (server.address() as net.AddressInfo).port, close: () => server.close() });
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

async function run(s: Scenario, segments: string[]) {
	const { port, close } = await serve(s, segments);
	const model = { ...googleModel, baseUrl: `http://127.0.0.1:${port}` };
	const events: string[] = [];
	let sameModel = true;
	let calls = 0;
	const out = stream(model, context, {
		apiKey: "test-api-key",
		onProviderStreamEvent: async (data: unknown, eventModel: unknown) => {
			if (eventModel !== model) sameModel = false;
			if (s.throwOn === calls++) throw new Error(s.throwMessage);
			events.push(JSON.stringify(data));
		},
	});
	const streamed: Array<{ type: string; delta?: string }> = [];
	for await (const ev of out) streamed.push("delta" in ev ? { type: ev.type, delta: ev.delta } : { type: ev.type });
	const msg = await out.result();
	close();
	return {
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
			totalTokens: msg.usage.totalTokens,
		},
	};
}

const results = [];
for (const s of scenarios) {
	const pi = await run(s, s.segments);
	const joined = await run(s, [s.segments.join("")]);
	const { throwOn, throwMessage, ...scenario } = s;
	results.push({
		...scenario,
		...(throwOn !== undefined && { throwOn, throwMessage }),
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
