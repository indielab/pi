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
//             over the body delivered one byte per read (the script fails if
//             any two differ); `completions` and `responses`
//             are pi's adapters over the same body: the provider stream events
//             onProviderStreamEvent observed, and how the stream ended.
//   completions / responses
//             adapter-specific bodies, with the same record as above.
//   hooks     the callback and onResponse failing, and a non-2xx response.
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

// ---- the loopback server ---------------------------------------------------

type Reply = { status: number; contentType: string; body: string };
const replies = new Map<string, Reply>();
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
const sse = (body: string) => route({ status: 200, contentType: "text/event-stream", body });

// ---- the SDK's own reading -------------------------------------------------

type Thrown = { name: string; message: string; error?: string };
type SDKReading = { yields: string[]; threw: Thrown | null };

function thrown(error: unknown): Thrown {
	const e = error as { constructor?: { name?: string }; message?: string; error?: unknown };
	const out: Thrown = { name: e?.constructor?.name ?? typeof error, message: String(e?.message) };
	if (e && typeof e === "object" && "error" in e) out.error = JSON.stringify(e.error);
	return out;
}

async function sdkRead(body: string, open: (client: any) => Promise<AsyncIterable<unknown>>): Promise<SDKReading> {
	const client = new OpenAI({ apiKey: "k", baseURL: sse(body), maxRetries: 0 });
	const yields: string[] = [];
	try {
		for await (const item of await open(client)) yields.push(JSON.stringify(item));
	} catch (error) {
		return { yields, threw: thrown(error) };
	}
	return { yields, threw: null };
}

// sdkReadBytewise is the SDK's Stream over the body delivered one byte per
// read, so a line ending split across reads ("\r" | "\n") is measured rather
// than assumed to read like the whole body.
async function sdkReadBytewise(body: string): Promise<SDKReading> {
	const bytes = new TextEncoder().encode(body);
	let at = 0;
	const byteStream = new ReadableStream<Uint8Array>({
		pull(controller) {
			if (at < bytes.length) controller.enqueue(bytes.slice(at, ++at));
			else controller.close();
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

// The SDK logs an unparseable event before rethrowing; keep the capture quiet.
const consoleError = console.error;
async function sdkReading(body: string): Promise<SDKReading> {
	console.error = () => {};
	try {
		const chat = await sdkRead(body, (c) => c.chat.completions.create({ model: "m", messages: [], stream: true }));
		const resp = await sdkRead(body, (c) => c.responses.create({ model: "m", input: "hi", stream: true }));
		if (JSON.stringify(chat) !== JSON.stringify(resp)) {
			throw new Error(`chat and responses streams read differently:\n${JSON.stringify(chat)}\n${JSON.stringify(resp)}`);
		}
		const bytewise = await sdkReadBytewise(body);
		if (JSON.stringify(chat) !== JSON.stringify(bytewise)) {
			throw new Error(`the body read whole and one byte at a time differ:\n${JSON.stringify(chat)}\n${JSON.stringify(bytewise)}`);
		}
		return chat;
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
	responseId: string;
};

type Hooks = {
	failOnEvent?: "throw" | "abort-then-throw";
	failOnResponse?: boolean;
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
		responseId: final.responseId ?? "",
	};
}

// ---- bodies --------------------------------------------------------------------

const J = (v: unknown) => JSON.stringify(v);
const BOM = "\ufeff";
const chunk = (id: string, content: string) => J({ id, choices: [{ index: 0, delta: { content } }] });
const FIN = J({ id: "f", choices: [{ index: 0, delta: {}, finish_reason: "stop" }] });
const A = chunk("a", "x");
const B = chunk("b", "y");
const errorChunk = (error: unknown) => `data: ${A}\n\ndata: ${J({ error })}\n\ndata: ${FIN}\n\n`;

// Dispatch bodies are read by the SDK and both adapters.
const dispatch: Record<string, string> = {
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
};

// ---- capture ---------------------------------------------------------------------

type Row = { sse: string; sdk?: SDKReading; completions?: Outcome; responses?: Outcome };
const out: {
	sha: string;
	openai: string;
	dispatch: Record<string, Row>;
	completions: Record<string, Row>;
	responses: Record<string, Row>;
	hooks: Record<string, { adapter: string; sse: string; status: number; outcome: Outcome }>;
} = { sha, openai: openaiVersion, dispatch: {}, completions: {}, responses: {}, hooks: {} };

for (const [name, body] of Object.entries(dispatch)) {
	out.dispatch[name] = {
		sse: body,
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
	for (const [name, [reply, hooks]] of Object.entries(cases)) {
		out.hooks[`${adapter}/${name}`] = {
			adapter,
			sse: reply.body,
			status: reply.status,
			outcome: await piRun(api, model, route(reply), hooks),
		};
	}
}

server.close();
fs.writeFileSync(outFile, `${JSON.stringify(out, null, "\t")}\n`);
console.log(`wrote ${outFile}`);
