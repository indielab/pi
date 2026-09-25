// Captures how pi's adapters end a stream whose signal aborts once the
// request is on its way — the oracle behind TestStreamAbortMatchesPi in this
// package.
//
//   node --experimental-strip-types capture.mts <extraction> <out.json> <sha>
//   e.g. ... capture.mts <dir> stream-abort-49681e1b7.json 49681e1b7
//
// <extraction> holds `git archive <sha> packages/ai package-lock.json` from the
// upstream clone, plus a node_modules resolving pi-ai's dependencies (the npm
// build's). The script refuses to write unless each SDK it runs
// (@anthropic-ai/sdk, openai, @google/genai) resolves to the version AND
// integrity the sha's package-lock.json locks; pi-messages calls node's own
// fetch.
//
// A raw TCP server answers every request 200, text/event-stream, chunked, and
// writes each of the run's segments as one HTTP chunk, which reaches the
// adapter as one body read (undici reads one chunk at a time); then it holds
// the connection open. The anthropic-messages and pi-messages adapters run
// every mode; openai-completions, openai-responses and google-generative-ai
// run only "an abort while an error body is read". Modes:
//   - "an abort while a read is pending": the signal aborts 150ms after the
//     first segment is written, while the adapter waits on its next read;
//   - "an abort from the callback with events left in its read": the
//     onProviderStreamEvent callback aborts on the segment's first event;
//   - "an abort from the callback on its read's last event": it aborts on
//     the segment's last one;
//   - "an abort from the callback with the next read already in": both
//     segments go out in one socket write, and the callback aborts on the
//     first segment's first event;
//   - "an abort while an error body is read": the server answers 500,
//     application/json, and writes the start of the error body as one chunk;
//     the signal aborts 150ms after that;
//   - "an already-aborted signal" and "an abort before the response arrives"
//     (pi-messages only; the other four adapters' are in
//     abort-before-response): the server never answers, and the signal is
//     aborted before the call or 150ms into it.
// Recorded per run: the status and segments the server wrote, the event types
// onProviderStreamEvent received, the stream's event types, and the final
// stopReason, errorMessage and content.
import fs from "node:fs";
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
const require = createRequire(path.join(src, "api/anthropic-messages.ts"));
const lock = JSON.parse(fs.readFileSync(path.join(extraction, "package-lock.json"), "utf8"));
const sdks: string[] = [];
for (const name of ["@anthropic-ai/sdk", "openai", "@google/genai"]) {
	let dir = path.dirname(require.resolve(name));
	while (!fs.existsSync(path.join(dir, "package.json")) || JSON.parse(fs.readFileSync(path.join(dir, "package.json"), "utf8")).name !== name) {
		dir = path.dirname(dir);
	}
	const version = JSON.parse(fs.readFileSync(path.join(dir, "package.json"), "utf8")).version as string;
	let root = dir;
	while (path.basename(root) !== "node_modules") root = path.dirname(root);
	const installed = JSON.parse(fs.readFileSync(path.join(root, ".package-lock.json"), "utf8")).packages[`node_modules/${name}`];
	const locked = lock.packages[`node_modules/${name}`];
	if (locked.version !== version || locked.integrity !== installed?.integrity) {
		console.error(`${name} mismatch: ${sha} locks ${locked.version} ${locked.integrity}, resolved ${version} ${installed?.integrity}`);
		process.exit(1);
	}
	sdks.push(`${name} ${version} ${locked.integrity}`);
}

const load = async (file: string) => import(pathToFileURL(path.join(src, file)).href);

type Mode =
	| "an abort while a read is pending"
	| "an abort from the callback with events left in its read"
	| "an abort from the callback on its read's last event"
	| "an abort from the callback with the next read already in"
	| "an abort while an error body is read"
	| "an already-aborted signal"
	| "an abort before the response arrives";
const streamModes: Mode[] = [
	"an abort while a read is pending",
	"an abort from the callback with events left in its read",
	"an abort from the callback on its read's last event",
	"an abort from the callback with the next read already in",
];
const errorBody: Mode = "an abort while an error body is read";
const requestModes: Mode[] = ["an already-aborted signal", "an abort before the response arrives"];

const anthropicEvent = (event: string, data: unknown) => `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`;
const piMessagesEvent = (data: unknown) => `data: ${JSON.stringify(data)}\n\n`;
// Each adapter's segments: the first carries three events, the second one
// more.
const adapters = [
	{
		api: "anthropic-messages",
		provider: "anthropic",
		id: "claude-sonnet-4-5",
		module: await load("api/anthropic-messages.ts"),
		modes: [...streamModes, errorBody],
		segments: [
			anthropicEvent("message_start", {
				type: "message_start",
				message: { id: "msg_1", model: "claude-sonnet-4-5", usage: { input_tokens: 3, output_tokens: 0 } },
			}) +
				anthropicEvent("content_block_start", { type: "content_block_start", index: 0, content_block: { type: "text", text: "" } }) +
				anthropicEvent("content_block_delta", { type: "content_block_delta", index: 0, delta: { type: "text_delta", text: "in hand" } }),
			anthropicEvent("content_block_delta", { type: "content_block_delta", index: 0, delta: { type: "text_delta", text: " and more" } }),
		],
	},
	{
		api: "pi-messages",
		provider: "radius",
		id: "auto",
		module: await load("api/pi-messages.ts"),
		modes: [...streamModes, errorBody, ...requestModes],
		segments: [
			piMessagesEvent({ type: "start" }) +
				piMessagesEvent({ type: "text_start", contentIndex: 0 }) +
				piMessagesEvent({ type: "text_delta", contentIndex: 0, delta: "in hand" }),
			piMessagesEvent({ type: "text_delta", contentIndex: 0, delta: " and more" }),
		],
	},
	{ api: "openai-completions", provider: "openai", id: "gpt-4o", module: await load("api/openai-completions.ts"), modes: [errorBody], segments: [] },
	{ api: "openai-responses", provider: "openai", id: "gpt-5-mini", module: await load("api/openai-responses.ts"), modes: [errorBody], segments: [] },
	{
		api: "google-generative-ai",
		provider: "google",
		id: "gemini-2.5-flash",
		module: await load("api/google-generative-ai.ts"),
		modes: [errorBody],
		segments: [],
	},
];
const firstSegmentEvents = 3;
const errorBodyStart = '{"error":{"message":"cut short';

let status = "";
let segments: string[] = [];
let answer = true;
let onSegmentWritten = () => {};
const chunk = (s: string) => `${Buffer.byteLength(s).toString(16)}\r\n${s}\r\n`;
const server = net.createServer((sock) => {
	let buf = Buffer.alloc(0);
	sock.on("data", (d) => {
		buf = Buffer.concat([buf, d]);
		const headEnd = buf.indexOf("\r\n\r\n");
		if (headEnd < 0) return;
		const len = Number(/content-length:\s*(\d+)/i.exec(buf.subarray(0, headEnd).toString())?.[1] ?? 0);
		if (buf.length < headEnd + 4 + len) return;
		buf = buf.subarray(headEnd + 4 + len);
		if (!answer) return;
		const contentType = status === "200 OK" ? "text/event-stream" : "application/json";
		sock.write(`HTTP/1.1 ${status}\r\nContent-Type: ${contentType}\r\nTransfer-Encoding: chunked\r\n\r\n${segments.map(chunk).join("")}`);
		onSegmentWritten();
	});
	sock.on("error", () => {});
});
await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
const port = (server.address() as net.AddressInfo).port;

const runs = [];
for (const a of adapters) {
	for (const mode of a.modes) {
		status = mode === errorBody ? "500 Internal Server Error" : "200 OK";
		segments =
			mode === errorBody
				? [errorBodyStart]
				: mode === "an abort from the callback with the next read already in"
					? a.segments
					: a.segments.slice(0, 1);
		answer = !requestModes.includes(mode);
		const controller = new AbortController();
		onSegmentWritten =
			mode === "an abort while a read is pending" || mode === errorBody ? () => setTimeout(() => controller.abort(), 150) : () => {};
		if (mode === "an already-aborted signal") controller.abort();
		if (mode === "an abort before the response arrives") setTimeout(() => controller.abort(), 150);
		const model = {
			id: a.id,
			name: a.id,
			api: a.api,
			provider: a.provider,
			baseUrl: `http://127.0.0.1:${port}`,
			reasoning: false,
			input: ["text"],
			cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
			contextWindow: 100000,
			maxTokens: 1000,
		};
		const observed: string[] = [];
		const out = a.module.stream(model, { messages: [{ role: "user", content: "hi", timestamp: 1 }] }, {
			apiKey: "test-api-key",
			signal: controller.signal,
			onProviderStreamEvent: async (event: { type?: string }) => {
				observed.push(event.type ?? "");
				const first = mode === "an abort from the callback with events left in its read" || mode === "an abort from the callback with the next read already in";
				if (first && observed.length === 1) controller.abort();
				if (mode === "an abort from the callback on its read's last event" && observed.length === firstSegmentEvents) controller.abort();
			},
		});
		const events: string[] = [];
		for await (const ev of out) events.push(ev.type);
		const msg = await out.result();
		runs.push({
			api: a.api,
			mode,
			...(answer ? { status: Number.parseInt(status), segments } : {}),
			observed,
			events,
			stopReason: msg.stopReason,
			errorMessage: msg.errorMessage,
			content: JSON.stringify(msg.content),
		});
	}
}
server.close();

fs.writeFileSync(
	outFile,
	`${JSON.stringify({ source: `upstream ${sha} packages/ai/src/api, ${sdks.join(", ")}, node ${process.version} (undici ${process.versions.undici})`, runs }, null, "\t")}\n`,
);
console.log(`wrote ${runs.length} runs to ${outFile}`);
process.exit(0);
