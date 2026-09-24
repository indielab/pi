// Captures how the openai SDK's stream decoder and pi's two openai adapters
// read an SSE body — the oracle behind openai_stream_test.go in this package.
//
//   node --experimental-strip-types capture.mts <extraction> <out.json> <sha>
//   e.g. ... capture.mts <dir> openai-stream-002fc8385.json 002fc8385
//
// <extraction> holds packages/ai at <sha> (`git archive <sha> packages/ai` from
// the upstream clone, prefix kept) and a node_modules resolving pi-ai's
// dependencies. The openai package there must be the version package-lock.json
// locks at <sha> — 6.40.0 at 002fc8385, integrity
// sha512-MWtTjd/gQt4jpbji61NTgFWJLoY/PdRJ6wG9/ZDRMYNMlBKrCrSlkLI+KgHP1vR1qT6LKSAyAqIxno6lcK9JiA==,
// which the npm 0.87.1 build's node_modules carries. The version read is
// recorded in the output.
//
// Every body is served from a loopback server, so nothing needs a key or the
// network, and recorded here so the Go test serves the same bytes.
//
//   dispatch  SSE bodies read three ways: `sdk` is what the SDK's stream
//             iterator yields (each item JSON.stringify'd) and what it throws,
//             through chat.completions.create and responses.create alike, and
//             over the body delivered whole and one byte per read (the script
//             fails if any two differ) — and, the same two ways, over the body
//             followed by an aborted read and by a failed one; `completions`
//             and `responses`
//             are pi's adapters over the same body: the provider stream events
//             onProviderStreamEvent observed, and how the stream ended.
//   completions / responses
//             adapter-specific bodies, with the same record as above.
//   hooks     the callback and onResponse failing, a non-2xx response, and
//             a request aborted while the server holds the connection open.
//   pricing   responses' service-tier pricing: which tier a completed
//             response's service_tier leaves in force.
//   lines     the SDK's LineDecoder over a body cut into reads that split a
//             line ending, as the port's line splitter can meet one.
import fs from "node:fs";
import http from "node:http";
import net from "node:net";
import path from "node:path";
import { pathToFileURL } from "node:url";

const [extraction, outFile, sha] = process.argv.slice(2);
if (!extraction || !outFile || !sha) {
	console.error("usage: node --experimental-strip-types capture.mts <extraction> <out.json> <sha>");
	process.exit(2);
}
const src = path.join(extraction, "packages/ai/src");
const load = (file: string) => import(pathToFileURL(path.join(src, file)).href);
const { normalizeContext } = await load("utils/transcript.ts");
const completions = await load("api/openai-completions.ts");
const responses = await load("api/openai-responses.ts");
const openaiDir = path.join(extraction, "node_modules/openai");
const openaiVersion = JSON.parse(fs.readFileSync(path.join(openaiDir, "package.json"), "utf8")).version;
const { default: OpenAI } = await import(pathToFileURL(path.join(openaiDir, "index.mjs")).href);
const { Stream } = await import(pathToFileURL(path.join(openaiDir, "core/streaming.mjs")).href);
const { LineDecoder } = await import(pathToFileURL(path.join(openaiDir, "internal/decoders/line.mjs")).href);

// ---- the loopback server ---------------------------------------------------

// A held reply writes its body and keeps the connection open, so the stream
// can end only through the client giving up on it.
// A body is text, or bytes where it must carry invalid UTF-8.
type Body = string | Buffer;
type Reply = { status: number; contentType: string; body: Body; hold?: boolean };
const replies = new Map<string, Reply>();
const held = new Set<http.ServerResponse>();
let nextRoute = 0;
const server = http.createServer((req, res) => {
	req.resume();
	req.on("end", () => {
		const route = (req.url ?? "").split("/")[1];
		const reply = replies.get(route);
		if (!reply) {
			res.writeHead(404).end();
			return;
		}
		res.writeHead(reply.status, { "content-type": reply.contentType });
		if (reply.hold) {
			res.write(reply.body);
			held.add(res);
			return;
		}
		res.end(reply.body);
	});
});
await new Promise<void>((r) => server.listen(0, "127.0.0.1", () => r()));
const { port } = server.address() as net.AddressInfo;

// route registers a reply and returns the base URL that reaches it.
function route(reply: Reply): string {
	const name = `r${nextRoute++}`;
	replies.set(name, reply);
	return `http://127.0.0.1:${port}/${name}/v1`;
}
const sse = (body: Body) => route({ status: 200, contentType: "text/event-stream", body });

// ---- the SDK's own reading -------------------------------------------------

type Thrown = { name: string; message: string; error?: string };
type SDKReading = { yields: string[]; threw: Thrown | null };
// A dispatch row's `sdk` also records the body read to its end and then
// failing: `aborted` with an AbortError, as a cancelled request's body read
// throws (the SDK's Stream swallows it and simply ends), and `readFailed` with
// any other error (it propagates).
type SDKReadings = SDKReading & { aborted: SDKReading; readFailed: SDKReading };

function thrown(error: unknown): Thrown {
	const e = error as { constructor?: { name?: string }; message?: string; error?: unknown };
	const out: Thrown = { name: e?.constructor?.name ?? typeof error, message: String(e?.message) };
	if (e && typeof e === "object" && "error" in e) out.error = JSON.stringify(e.error);
	return out;
}

async function sdkRead(body: Body, open: (client: any) => Promise<AsyncIterable<unknown>>): Promise<SDKReading> {
	const client = new OpenAI({ apiKey: "k", baseURL: sse(body), maxRetries: 0 });
	const yields: string[] = [];
	try {
		for await (const item of await open(client)) yields.push(JSON.stringify(item));
	} catch (error) {
		return { yields, threw: thrown(error) };
	}
	return { yields, threw: null };
}

// sdkReadStream is the SDK's Stream over the body delivered as a stream of
// reads, whole or one byte per read, and ending cleanly or with a failed read
// (end). A line ending split across reads ("\r" | "\n") is measured rather
// than assumed to read like the whole body.
async function sdkReadStream(body: Body, bytewise: boolean, end?: Error): Promise<SDKReading> {
	const bytes = typeof body === "string" ? new TextEncoder().encode(body) : new Uint8Array(body);
	let at = 0;
	const byteStream = new ReadableStream<Uint8Array>({
		pull(controller) {
			if (at < bytes.length) {
				const next = bytewise ? at + 1 : bytes.length;
				controller.enqueue(bytes.slice(at, next));
				at = next;
			} else if (end) {
				controller.error(end);
			} else {
				controller.close();
			}
		},
	});
	const yields: string[] = [];
	try {
		for await (const item of Stream.fromSSEResponse(new Response(byteStream), new AbortController())) {
			yields.push(JSON.stringify(item));
		}
	} catch (error) {
		return { yields, threw: thrown(error) };
	}
	return { yields, threw: null };
}

// sdkReadEndingIn reads the body whole and one byte per read, ending in end,
// and fails unless the two agree.
async function sdkReadEndingIn(body: Body, end?: () => Error): Promise<SDKReading> {
	const whole = await sdkReadStream(body, false, end?.());
	const bytewise = await sdkReadStream(body, true, end?.());
	if (JSON.stringify(whole) !== JSON.stringify(bytewise)) {
		throw new Error(`the body read whole and one byte at a time differ:\n${JSON.stringify(whole)}\n${JSON.stringify(bytewise)}`);
	}
	return whole;
}

// The SDK logs an unparseable event before rethrowing; keep the capture quiet.
const consoleError = console.error;
async function sdkReading(body: Body): Promise<SDKReadings> {
	console.error = () => {};
	try {
		const chat = await sdkRead(body, (c) => c.chat.completions.create({ model: "m", messages: [], stream: true }));
		const resp = await sdkRead(body, (c) => c.responses.create({ model: "m", input: "hi", stream: true }));
		if (JSON.stringify(chat) !== JSON.stringify(resp)) {
			throw new Error(`chat and responses streams read differently:\n${JSON.stringify(chat)}\n${JSON.stringify(resp)}`);
		}
		const streamed = await sdkReadEndingIn(body);
		if (JSON.stringify(chat) !== JSON.stringify(streamed)) {
			throw new Error(`the body served and streamed read differently:\n${JSON.stringify(chat)}\n${JSON.stringify(streamed)}`);
		}
		return {
			...chat,
			aborted: await sdkReadEndingIn(body, () => new DOMException("This operation was aborted", "AbortError")),
			readFailed: await sdkReadEndingIn(body, () => new TypeError("terminated")),
		};
	} finally {
		console.error = consoleError;
	}
}

// ---- pi's adapters -----------------------------------------------------------

const completionsModel = {
	id: "openrouter/auto",
	name: "OpenRouter Auto",
	api: "openai-completions",
	provider: "openrouter",
	reasoning: false,
	input: ["text"],
	cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
	contextWindow: 200000,
	maxTokens: 8192,
};
const responsesModel = {
	id: "gpt-5-mini",
	name: "GPT-5 Mini",
	api: "openai-responses",
	provider: "openai",
	reasoning: true,
	input: ["text"],
	cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
	contextWindow: 400000,
	maxTokens: 128000,
};
const context = normalizeContext({ systemPrompt: "", messages: [{ role: "user", content: "hi", timestamp: 1 }] });

type Outcome = {
	observed: string[];
	sameModel: boolean;
	onResponseCalls: number;
	stopReason: string;
	errorMessage: string;
	text: string;
	// responseId and rawStopReason are String() of pi's values, which need not
	// be strings: pi assigns what the provider sent. null and undefined are "".
	responseId: string;
	rawStopReason: string;
};

type Hooks = {
	failOnEvent?: "throw" | "abort-then-throw";
	failOnResponse?: boolean;
	// abortOnEvent aborts the request, without throwing, as the observer sees
	// that event (1-based).
	abortOnEvent?: number;
};

async function piRun(api: any, baseModel: Record<string, unknown>, baseUrl: string, hooks: Hooks = {}): Promise<Outcome> {
	const model = { ...baseModel, baseUrl };
	const controller = new AbortController();
	const observed: string[] = [];
	let sameModel = true;
	let onResponseCalls = 0;
	const final = await api
		.streamSimple(model, context, {
			apiKey: "k",
			signal: controller.signal,
			onProviderStreamEvent: (data: unknown, eventModel: unknown) => {
				observed.push(JSON.stringify(data));
				sameModel &&= eventModel === model;
				if (hooks.abortOnEvent === observed.length) controller.abort();
				if (hooks.failOnEvent === "abort-then-throw") controller.abort();
				if (hooks.failOnEvent) throw new Error("observer boom");
			},
			onResponse: () => {
				onResponseCalls++;
				if (hooks.failOnResponse) throw new Error("response veto");
			},
		})
		.result();
	return {
		observed,
		sameModel,
		onResponseCalls,
		stopReason: final.stopReason,
		errorMessage: final.errorMessage ?? "",
		text: final.content
			.filter((b: { type: string }) => b.type === "text")
			.map((b: { text: string }) => b.text)
			.join(""),
		responseId: final.responseId == null ? "" : String(final.responseId),
		rawStopReason: final.rawStopReason == null ? "" : String(final.rawStopReason),
	};
}

// ---- bodies --------------------------------------------------------------------

const J = (v: unknown) => JSON.stringify(v);
const BOM = "\ufeff";
const chunk = (id: string, content: string) => J({ id, choices: [{ index: 0, delta: { content } }] });
const FIN = J({ id: "f", choices: [{ index: 0, delta: {}, finish_reason: "stop" }] });
const A = chunk("a", "x");
const B = chunk("b", "y");
// invalidUTF8Content is a chunk whose content is "x", the bytes, "y".
const invalidUTF8Content = (bytes: number[]) =>
	Buffer.concat([
		Buffer.from(`data: {"id":"u","choices":[{"index":0,"delta":{"content":"x`),
		Buffer.from(bytes),
		Buffer.from(`y"}}]}\n\ndata: ${FIN}\n\n`),
	]);
const errorChunk = (error: unknown) => `data: ${A}\n\ndata: ${J({ error })}\n\ndata: ${FIN}\n\n`;

// Dispatch bodies are read by the SDK and both adapters.
const dispatch: Record<string, Body> = {
	"blank-line-dispatch": `data: ${A}\n\ndata: ${B}\n\ndata: ${FIN}\n\ndata: [DONE]\n\n`,
	"multi-line-data": `data: {"id":"m",\ndata: "choices":[{"index":0,"delta":{"content":"joined"}}]}\n\ndata: ${FIN}\n\n`,
	"unterminated-trailing-event": `data: ${A}\n\ndata: ${FIN}\n`,
	"unterminated-no-newline": `data: ${A}\n\ndata: ${FIN}`,
	"done-prefix": `data: ${A}\n\ndata: ${FIN}\n\ndata: [DONE]extra\n\ndata: ${B}\n\n`,
	"done-exact-first": `data: [DONE]\n\ndata: ${A}\n\ndata: ${FIN}\n\n`,
	"done-mid-line": `data: ${A}\n\ndata: [DONE\n\ndata: ${FIN}\n\n`,
	crlf: `data: ${A}\r\n\r\ndata: ${B}\r\n\r\ndata: ${FIN}\r\n\r\n`,
	"lone-cr": `data: ${A}\r\rdata: ${B}\r\rdata: ${FIN}\r\r`,
	"mixed-line-endings": `data: ${A}\r\n\ndata: ${B}\n\r\ndata: ${FIN}\r\r\n`,
	// "\r\n" is one line ending, so an event's fields stay one event: a second
	// line ending inside it would dispatch the half read so far.
	"crlf-multi-line-data": `data: {"id":"m",\r\ndata: "choices":[{"index":0,"delta":{"content":"joined"}}]}\r\n\r\ndata: ${FIN}\r\n\r\n`,
	"crlf-thread-event": `event: thread.message\r\ndata: {"id":"t"}\r\n\r\ndata: ${A}\r\n\r\ndata: ${FIN}\r\n\r\n`,
	"comments-and-other-fields": `: keepalive\n\nid: 7\nretry: 100\ndata: ${A}\n\n: ping\n\ndata: ${FIN}\n\n`,
	"field-value-spacing": `data:${A}\n\ndata:  ${FIN}\n\n`,
	"data-field-without-colon": `data\ndata: ${A}\n\ndata: ${FIN}\n\n`,
	"bom-at-line-start": `data: ${A}\n\n${BOM}data: ${B}\n\n${BOM}${BOM}data: ${chunk("c", "z")}\n\ndata: ${FIN}\n\n`,
	"event-named-error": `event: error\ndata: ${A}\n\ndata: ${FIN}\n\n`,
	"event-name-empty": `event:\n\ndata: ${A}\n\ndata: ${FIN}\n\n`,
	"thread-event": `event: thread.message\ndata: {"id":"t","error":{"message":"not thrown"}}\n\ndata: ${A}\n\ndata: ${FIN}\n\n`,
	scalars: `data: 5\n\ndata: "s"\n\ndata: [1,{"k":2}]\n\ndata: true\n\ndata: false\n\ndata: 0\n\ndata: ${A}\n\ndata: ${FIN}\n\n`,
	"null-event": `data: ${A}\n\ndata: null\n\ndata: ${FIN}\n\n`,
	"no-choices-chunks": `data: ${A}\n\ndata: ${J({ id: "u", choices: [], usage: { prompt_tokens: 3, completion_tokens: 1, total_tokens: 4 } })}\n\ndata: ${J({ id: "o", openrouter_metadata: { strategy: "direct" } })}\n\ndata: ${FIN}\n\n`,
	"key-order": `data: ${J({ z: 1, id: "k", a: { y: [{ q: 1, b: 2 }], x: null }, choices: [{ index: 0, delta: { content: "<&>" } }] })}\n\ndata: ${FIN}\n\n`,
	// JSON.parse reads a number past float64's range as ±Infinity, which
	// JSON.stringify writes as null; written out, since J() would already
	// have made it null.
	"numbers-past-float64": `data: {"id":"a","x":1e400,"y":-1e400,"z":1e-400,"w":[1e400,{"v":-1e400}],"choices":[{"index":0,"delta":{"content":"x"}}]}\n\ndata: 1e400\n\ndata: -0\n\ndata: ${FIN}\n\n`,
	// Written out, not J(): a JS object literal would already list its
	// array-index keys first, ascending, as JSON.parse's object does.
	"index-keys": `data: {"z":1,"id":"k","2":"x","1":"y","-1":0,"01":2,"4294967295":3,"4294967294":4,"choices":[{"index":0,"delta":{"content":"i"},"logprobs":{"2":1,"1":2}}]}\n\ndata: ${FIN}\n\n`,
	"error-chunk-openrouter": errorChunk({ message: "boom", code: 502, metadata: { raw: "upstream exploded", provider_name: "p" } }),
	"error-chunk-raw-contained": errorChunk({ message: "boom: upstream exploded", metadata: { raw: "upstream exploded" } }),
	"error-chunk-raw-number": errorChunk({ message: "boom", metadata: { raw: 5 } }),
	"error-chunk-raw-object": errorChunk({ message: "boom", metadata: { raw: { a: 1 } } }),
	"error-chunk-raw-array": errorChunk({ message: "boom", metadata: { raw: [1, [2, null], "x"] } }),
	"error-chunk-raw-true": errorChunk({ message: "boom", metadata: { raw: true } }),
	"error-chunk-raw-falsy": errorChunk({ message: "boom", metadata: { raw: "" } }),
	"error-chunk-metadata-string": errorChunk({ message: "boom", metadata: "raw" }),
	"error-chunk-no-message": errorChunk({ code: "x", type: "y" }),
	"error-chunk-message-object": errorChunk({ message: { b: 1, a: 2 } }),
	"error-chunk-message-number": errorChunk({ message: 42 }),
	"error-chunk-message-empty": errorChunk({ message: "", code: 1 }),
	"error-chunk-message-escapes": errorChunk({ message: 'caf\u00e9 </script> "q"' }),
	"error-chunk-string": errorChunk("boom"),
	"error-chunk-number": errorChunk(7),
	"error-chunk-true": errorChunk(true),
	"error-chunk-empty-object": errorChunk({}),
	"error-chunk-empty-array": errorChunk([]),
	"error-chunk-integer-keys": errorChunk({ b: 1, 2: "x", 1: "y", code: 1.5e21 }),
	"error-values-falsy": `data: ${J({ id: "n", error: null })}\n\ndata: ${J({ id: "e", error: "" })}\n\ndata: ${J({ id: "z", error: 0 })}\n\ndata: ${J({ id: "f", error: false })}\n\ndata: ${A}\n\ndata: ${FIN}\n\n`,
	"error-key-case": `data: ${J({ Error: { message: "boom" } })}\n\ndata: ${A}\n\ndata: ${FIN}\n\n`,
	"error-after-done": `data: ${A}\n\ndata: ${FIN}\n\ndata: [DONE]\n\ndata: ${J({ error: { message: "late" } })}\n\n`,
	// pi throws JSON.parse's SyntaxError on these; the port skips the event.
	"unparseable-data": `data: ${A}\n\ndata: {not json\n\ndata: ${FIN}\n\n`,
	"event-without-data": `data: ${A}\n\nevent: ping\n\ndata: ${FIN}\n\n`,
	// Each line is decoded by a TextDecoder, which writes one U+FFFD per
	// maximal subpart of an invalid UTF-8 sequence (WHATWG), not one per byte.
	"utf8-truncated-3": invalidUTF8Content([0xe2, 0x82]),
	"utf8-truncated-4": invalidUTF8Content([0xf0, 0x9f, 0x98]),
	"utf8-overlong": invalidUTF8Content([0xc0, 0xaf]),
	"utf8-surrogate": invalidUTF8Content([0xed, 0xa0, 0x80]),
	"utf8-above-max": invalidUTF8Content([0xf4, 0x90, 0x80, 0x80]),
	"utf8-lone-continuation": invalidUTF8Content([0x80, 0xbf]),
	"utf8-invalid-lead": invalidUTF8Content([0xff, 0xe0, 0x80]),
	"utf8-truncated-then-valid": invalidUTF8Content([0xe2, 0x82, 0xe2, 0x82, 0xac]),
	"utf8-in-event-name": Buffer.concat([
		Buffer.from("event: thread.m"),
		Buffer.from([0xe2, 0x82]),
		Buffer.from(`\ndata: {"id":"t"}\n\ndata: ${A}\n\ndata: ${FIN}\n\n`),
	]),
};

// Adapter-specific bodies.
const completionsBodies: Record<string, string> = {
	// packages/ai/test/openai-completions-provider-stream-event.test.ts
	"openrouter-metadata": `data: ${J({
		id: "chatcmpl-1",
		model: "anthropic/claude-sonnet-4.6",
		choices: [{ index: 0, delta: { content: "hello" } }],
	})}\n\ndata: ${J({
		id: "chatcmpl-1",
		model: "anthropic/claude-sonnet-4.6",
		choices: [{ index: 0, delta: {}, finish_reason: "stop" }],
		usage: { prompt_tokens: 10, completion_tokens: 2, total_tokens: 12, cost: 0.0012, is_byok: false },
		openrouter_metadata: { strategy: "direct", region: "iad" },
	})}\n\ndata: [DONE]\n\n`,
};

const R = (v: Record<string, unknown>) => `data: ${J(v)}\n\n`;
const created = R({ type: "response.created", sequence_number: 0, response: { id: "resp_1" } });
const messageItem = { type: "message", id: "msg_1", role: "assistant", content: [] };
const textEvents =
	R({ type: "response.output_item.added", output_index: 0, item: messageItem }) +
	R({ type: "response.content_part.added", output_index: 0, part: { type: "output_text", text: "" } }) +
	R({ type: "response.output_text.delta", output_index: 0, delta: "hi" }) +
	R({
		type: "response.output_item.done",
		output_index: 0,
		item: { ...messageItem, content: [{ type: "output_text", text: "hi" }] },
	});
const completed = R({
	type: "response.completed",
	response: { id: "resp_1", status: "completed", usage: { input_tokens: 5, output_tokens: 1, total_tokens: 6 } },
});
const errorEvent = (fields: Record<string, unknown>) => created + R({ type: "error", ...fields }) + completed;
const failed = (response: Record<string, unknown>) =>
	created + R({ type: "response.failed", response: { id: "resp_1", status: "failed", ...response } });

const responsesBodies: Record<string, string> = {
	// packages/ai/test/openai-responses-terminal-event.test.ts, as SSE.
	"terminal-missing":
		R({ type: "response.created", sequence_number: 0, response: { id: "resp_wrapper_early_eof" } }) +
		R({
			type: "response.output_item.added",
			sequence_number: 1,
			output_index: 0,
			item: { type: "reasoning", id: "rs_wrapper_early_eof", summary: [] },
		}) +
		R({
			type: "response.reasoning_text.delta",
			sequence_number: 2,
			output_index: 0,
			content_index: 0,
			item_id: "rs_wrapper_early_eof",
			delta: "partial reasoning before the wrapper stream ends",
		}),
	completed: created + textEvents + completed,
	"null-mid-stream": `${created}data: null\n\n${textEvents}${completed}`,
	"scalar-events": `${created}data: 5\n\ndata: "s"\n\ndata: [1]\n\ndata: true\n\ndata: false\n\ndata: 0\n\n${textEvents}${completed}`,
	"unknown-event-types": created + R({ type: "response.in_progress", response: { id: "resp_1" } }) + R({ id: "no-type" }) + textEvents + completed,
	"error-event": errorEvent({ code: "rate_limit_exceeded", message: "slow down", param: null, sequence_number: 1 }),
	"error-event-no-code": errorEvent({ message: "slow down" }),
	"error-event-null-code": errorEvent({ code: null, message: "slow down" }),
	"error-event-number-code": errorEvent({ code: 500, message: "slow down" }),
	"error-event-no-message": errorEvent({ code: "x" }),
	"error-event-object-message": errorEvent({ code: "x", message: { a: 1 } }),
	"error-event-array-code": errorEvent({ code: [1, [2, null]], message: "m" }),
	"error-event-uncoercible-code": errorEvent({ code: { toString: 1 }, message: "m" }),
	"error-event-code-past-float64": `${created}data: {"type":"error","code":1e400,"message":"m"}\n\n${completed}`,
	"response-failed":
		created +
		R({
			type: "response.failed",
			response: { id: "resp_1", status: "failed", error: { code: "server_error", message: "bad" } },
		}),
	"response-failed-number-code": failed({ error: { code: 500, message: "bad" } }),
	"response-failed-true-code": failed({ error: { code: true, message: "bad" } }),
	"response-failed-falsy-members": failed({ error: { code: "", message: 0 } }),
	"response-failed-object-message": failed({ error: { code: "x", message: { a: 1 } } }),
	"response-failed-uncoercible-message": failed({ error: { code: "x", message: { toString: 1 } } }),
	"response-failed-string-error": failed({ error: "boom", incomplete_details: { reason: "max_output_tokens" } }),
	"response-failed-empty-error": failed({ error: {}, incomplete_details: { reason: "max_output_tokens" } }),
	"response-failed-false-error": failed({ error: false, incomplete_details: { reason: "max_output_tokens" } }),
	"response-failed-number-reason": failed({ error: null, incomplete_details: { reason: 5 } }),
	"response-failed-empty-reason": failed({ incomplete_details: { reason: "" } }),
	"response-failed-no-details": failed({}),
	"incomplete-number-reason":
		created +
		textEvents +
		R({
			type: "response.incomplete",
			response: { id: "resp_1", status: "incomplete", incomplete_details: { reason: 5 } },
		}),
	// pi reads each member it uses off the parsed event with JS semantics: the
	// key is exact (a JS property read), a member of any type is read as
	// whatever it is, and reading off a null or absent response throws.
	"error-event-code-key-case": errorEvent({ CODE: "x", message: "m" }),
	"error-event-code-duplicate-case": `${created}data: {"type":"error","code":"a","Code":"b","message":"m"}\n\n${completed}`,
	"type-key-case": `${created}${textEvents}data: {"TYPE":"response.completed","response":{"id":"resp_1","status":"completed"}}\n\n`,
	"type-duplicate-case": `${created}${textEvents}data: {"type":"response.completed","Type":"x","response":{"id":"resp_1","status":"completed"}}\n\n`,
	"response-key-case": `${created}data: {"type":"response.failed","Response":{"error":{"code":"c","message":"m"}}}\n\n`,
	"response-failed-error-key-case": failed({ ERROR: { code: "c", message: "m" } }),
	"response-failed-number-response": created + R({ type: "response.failed", response: 5 }),
	"response-failed-null-response": created + R({ type: "response.failed", response: null }),
	"response-failed-number-status": failed({ status: 5, error: { code: "c", message: "m" } }),
	"response-failed-number-id": failed({ id: 5, error: { code: "c", message: "m" } }),
	"created-null-response": R({ type: "response.created", response: null }) + textEvents + completed,
	"created-no-response": R({ type: "response.created" }) + textEvents + completed,
	"created-number-response":
		R({ type: "response.created", response: 5 }) +
		textEvents +
		R({ type: "response.completed", response: { status: "completed" } }),
	"completed-null-response": created + textEvents + R({ type: "response.completed", response: null }),
	"completed-no-response": created + textEvents + R({ type: "response.completed" }),
	"completed-string-response": created + textEvents + R({ type: "response.completed", response: "x" }),
	"completed-number-id": created + textEvents + R({ type: "response.completed", response: { id: 5, status: "completed" } }),
	"completed-false-id": created + textEvents + R({ type: "response.completed", response: { id: false, status: "completed" } }),
	"completed-number-status": created + textEvents + R({ type: "response.incomplete", response: { id: "resp_1", status: 5 } }),
	"completed-false-status": created + textEvents + R({ type: "response.completed", response: { id: "resp_1", status: false } }),
	"completed-object-status-with-reason":
		created +
		textEvents +
		R({ type: "response.completed", response: { status: { a: 1 }, incomplete_details: { reason: "r" } } }),
	"completed-no-status-with-reason":
		created + textEvents + R({ type: "response.incomplete", response: { id: "resp_1", incomplete_details: { reason: "max_output_tokens" } } }),
	"completed-null-status-with-reason":
		created +
		textEvents +
		R({ type: "response.incomplete", response: { id: "resp_1", status: null, incomplete_details: { reason: "max_output_tokens" } } }),
	"completed-uncoercible-status-with-reason":
		created +
		textEvents +
		R({ type: "response.completed", response: { status: { toString: 1 }, incomplete_details: { reason: "r" } } }),
	"completed-number-output": created + textEvents + R({ type: "response.completed", response: { status: "completed", output: 5 } }),
	"completed-object-output": created + textEvents + R({ type: "response.completed", response: { status: "completed", output: {} } }),
	"completed-string-output": created + textEvents + R({ type: "response.completed", response: { status: "completed", output: "ab" } }),
	"completed-null-output-item": created + textEvents + R({ type: "response.completed", response: { status: "completed", output: [null] } }),
	"completed-scalar-output-items":
		created + textEvents + R({ type: "response.completed", response: { status: "completed", output: [5, "x", true, []] } }),
	"completed-string-usage-member":
		created +
		textEvents +
		R({ type: "response.completed", response: { status: "completed", usage: { input_tokens: "5", output_tokens: 1, total_tokens: 6 } } }),
	"completed-fractional-usage-member":
		created +
		textEvents +
		R({ type: "response.completed", response: { status: "completed", usage: { input_tokens: 1.5, output_tokens: 1, total_tokens: 6 } } }),
	// Members the typed events carry, mistyped on an event that ends the
	// stream, which pi never reads there.
	"completed-mistyped-other-member":
		created + textEvents + R({ type: "response.completed", output_index: "x", item: 5, response: { id: "resp_1", status: "completed" } }),
	"error-event-mistyped-other-member": errorEvent({ code: "c", message: "m", delta: 5, part: "p" }),
	"completed-number-service-tier":
		created + textEvents + R({ type: "response.completed", response: { status: "completed", service_tier: 5 } }),
};

// ---- pricing ---------------------------------------------------------------------

// Pricing bodies end in response.completed with a million input tokens, read
// through responses.stream with the serviceTier option on a model priced at
// 1 per million input tokens: the recorded cost is the tier's multiplier.
// The response's service_tier wins over the option unless it is null or
// absent (`response?.service_tier ?? options.serviceTier`).
const pricedModel = { ...responsesModel, cost: { input: 1, output: 0, cacheRead: 0, cacheWrite: 0 } };
const pricedBody = (serviceTier: Record<string, unknown>) =>
	created +
	R({
		type: "response.completed",
		response: {
			id: "resp_1",
			status: "completed",
			usage: { input_tokens: 1000000, output_tokens: 0, total_tokens: 1000000 },
			...serviceTier,
		},
	});
const pricingBodies: Record<string, [string, string]> = {
	"tier-absent": [pricedBody({}), "priority"],
	"tier-null": [pricedBody({ service_tier: null }), "priority"],
	"tier-empty": [pricedBody({ service_tier: "" }), "priority"],
	"tier-number": [pricedBody({ service_tier: 5 }), "priority"],
	"tier-flex": [pricedBody({ service_tier: "flex" }), "priority"],
	"tier-default": [pricedBody({ service_tier: "default" }), "priority"],
};

// ---- line splitting --------------------------------------------------------------

// The SDK's LineDecoder gets a body in iterSSEChunks' pieces, each ending at
// an event separator, so the dispatch bodies read one byte per read above
// never reach it with a line ending cut in two. The port's line splitter does
// meet one: its scanner's buffer can end inside a piece (an event of 64 KiB
// or more). Each sequence here is decoded one chunk at a time and then
// flushed, as the body's end flushes it.
const lineChunks: Record<string, string[]> = {
	"crlf-split": ["a\r", "\nb"],
	"crlf-split-thrice": ["a", "\r", "\n", "b\r\n"],
	"blank-crlf-split": ["\r", "\n\r", "\n"],
	"crlf-then-crlf-split": ["a\r", "\n", "\r", "\n"],
	"cr-then-text": ["a\r", "b\n"],
	"cr-then-cr": ["a\r", "\r"],
	"cr-cr-then-lf": ["a\r\r", "\nb"],
	"cr-at-end": ["a\r"],
	"cr-text-in-one": ["a\rb"],
	"lf-then-cr": ["a\n", "\rb"],
};

function sdkLines(chunks: string[]): string[] {
	const decoder = new LineDecoder();
	const lines: string[] = chunks.flatMap((c) => decoder.decode(new TextEncoder().encode(c)));
	return [...lines, ...decoder.flush()];
}

async function piPricing(body: string, serviceTier: string) {
	const final = await responses
		.stream({ ...pricedModel, baseUrl: sse(body) }, context, { apiKey: "k", serviceTier })
		.result();
	return { stopReason: final.stopReason, costTotal: final.usage.cost.total };
}

// ---- capture ---------------------------------------------------------------------

// A Row's body is `sse`, or `sseBase64` when it is bytes that are not UTF-8.
type Row = { sse?: string; sseBase64?: string; sdk?: SDKReadings; completions?: Outcome; responses?: Outcome };
const bodyFields = (body: Body) =>
	typeof body === "string" ? { sse: body } : { sseBase64: body.toString("base64") };
const out: {
	sha: string;
	openai: string;
	dispatch: Record<string, Row>;
	completions: Record<string, Row>;
	responses: Record<string, Row>;
	hooks: Record<
		string,
		{ adapter: string; sse: string; status: number; hold: boolean; abortOnEvent: number; outcome: Outcome }
	>;
	pricing: Record<string, { sse: string; serviceTier: string; stopReason: string; costTotal: number }>;
	lines: Record<string, { chunks: string[]; lines: string[] }>;
} = { sha, openai: openaiVersion, dispatch: {}, completions: {}, responses: {}, hooks: {}, pricing: {}, lines: {} };

for (const [name, body] of Object.entries(dispatch)) {
	out.dispatch[name] = {
		...bodyFields(body),
		sdk: await sdkReading(body),
		completions: await piRun(completions, completionsModel, sse(body)),
		responses: await piRun(responses, responsesModel, sse(body)),
	};
}
for (const [name, body] of Object.entries(completionsBodies)) {
	out.completions[name] = { sse: body, completions: await piRun(completions, completionsModel, sse(body)) };
}
for (const [name, body] of Object.entries(responsesBodies)) {
	out.responses[name] = { sse: body, responses: await piRun(responses, responsesModel, sse(body)) };
}

const hookBodies = { completions: dispatch["blank-line-dispatch"], responses: responsesBodies.completed };
const adapters = {
	completions: [completions, completionsModel],
	responses: [responses, responsesModel],
} as const;
// The request is aborted as the observer sees the last event of a held body:
// the SDK's next body read then throws an AbortError, which its Stream
// swallows, so the adapter's loop ends and its post-loop checks decide the
// message. Responses checks for a terminal event before its abort guard.
const eventCount = (body: string) =>
	body.split("\n\n").filter((e) => e.startsWith("data: ") && !e.startsWith("data: [DONE]")).length;
const heldCases: Record<string, Record<string, string>> = {
	completions: {
		"abort-held": `data: ${A}\n\n`,
		"abort-held-after-finish": dispatch["blank-line-dispatch"],
	},
	responses: {
		"abort-held": created + textEvents,
		"abort-held-after-terminal": responsesBodies.completed,
	},
};
for (const [adapter, [api, model]] of Object.entries(adapters)) {
	const body = hookBodies[adapter as keyof typeof hookBodies];
	const cases: Record<string, [Reply, Hooks]> = {
		"callback-throws": [{ status: 200, contentType: "text/event-stream", body }, { failOnEvent: "throw" }],
		"callback-aborts": [{ status: 200, contentType: "text/event-stream", body }, { failOnEvent: "abort-then-throw" }],
		"on-response-throws": [{ status: 200, contentType: "text/event-stream", body }, { failOnResponse: true }],
		"http-400": [{ status: 400, contentType: "application/json", body: J({ error: { message: "bad request" } }) }, {}],
		"http-400-on-response-throws": [
			{ status: 400, contentType: "application/json", body: J({ error: { message: "bad request" } }) },
			{ failOnResponse: true },
		],
	};
	for (const [name, heldBody] of Object.entries(heldCases[adapter])) {
		cases[name] = [
			{ status: 200, contentType: "text/event-stream", body: heldBody, hold: true },
			{ abortOnEvent: eventCount(heldBody) },
		];
	}
	for (const [name, [reply, hooks]] of Object.entries(cases)) {
		out.hooks[`${adapter}/${name}`] = {
			adapter,
			sse: reply.body,
			status: reply.status,
			hold: reply.hold ?? false,
			abortOnEvent: hooks.abortOnEvent ?? 0,
			outcome: await piRun(api, model, route(reply), hooks),
		};
	}
}

for (const [name, [body, serviceTier]] of Object.entries(pricingBodies)) {
	out.pricing[name] = { sse: body, serviceTier, ...(await piPricing(body, serviceTier)) };
}
for (const [name, chunks] of Object.entries(lineChunks)) {
	out.lines[name] = { chunks, lines: sdkLines(chunks) };
}

for (const res of held) res.destroy();
server.close();
fs.writeFileSync(outFile, `${JSON.stringify(out, null, "\t")}\n`);
console.log(`wrote ${outFile}`);
