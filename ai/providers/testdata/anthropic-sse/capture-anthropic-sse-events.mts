// Captures how real pi's anthropic-messages adapter iterates an SSE body — the
// events onProviderStreamEvent observes, the events the stream pushes, the
// final message, and the exact error a malformed event fails with. It is the
// oracle behind TestAnthropicSSEEventsMatchPi (upstream 002fc8385 plus the
// iterateAnthropicEvents parse-failure message).
//
//   node --experimental-strip-types capture-anthropic-sse-events.mts <extraction> <pi npm dir> <out.json> <sha>
//   e.g. ... capture-anthropic-sse-events.mts <dir> ~/.cache/pi-npm/0.87.1 anthropic-sse-events-8676a0dcd.json 8676a0dcd
//
// <extraction> holds packages/ai at <sha> (`git archive <sha> packages/ai |
// tar -x -C <dir>` from the upstream clone), with packages/ai/node_modules
// resolving pi-ai's dependencies. The npm build's node_modules serves once its
// copies match what <sha>'s package-lock.json locks; at 8676a0dcd that is
// @anthropic-ai/sdk 0.124.0 and partial-json 0.1.7, integrity-identical to
// ~/.cache/pi-npm/0.87.1's. The adapter runs from SOURCE: onProviderStreamEvent
// is in no published build yet. The model is the npm build's MODELS entry the
// upstream suite uses (anthropic/claude-haiku-4-5).
//
// Every row streams through the upstream suite's fake client
// (anthropic-sse-parsing.test.ts createFakeAnthropicClient), so neither side
// needs a key or the network, and records:
//   sse        the exact body, which the Go test replays byte for byte;
//   observed   each value onProviderStreamEvent received, as JSON.stringify
//              text, so key order is part of the expectation;
//   pushed     the type of every event the stream pushed (null: no type);
//   message    stopReason, errorMessage, responseId, content and usage of the
//              result;
//   v8Cause    true when errorMessage embeds a V8 JSON.parse message, which
//              the port does not reproduce: only the text around it is pi's.
// A row may make the observer throw ("observer boom") on the observed event at
// index throwAt, after aborting the request when abortFirst is set.
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";

const [extraction, npmDir, outFile, sha] = process.argv.slice(2);
if (!extraction || !npmDir || !outFile || !sha) {
	console.error(
		"usage: node --experimental-strip-types capture-anthropic-sse-events.mts <extraction> <pi npm dir> <out.json> <sha>",
	);
	process.exit(2);
}
const src = path.join(extraction, "packages/ai/src");
const { stream: streamAnthropic } = await import(pathToFileURL(path.join(src, "api/anthropic-messages.ts")).href);
const { normalizeContext } = await import(pathToFileURL(path.join(src, "utils/transcript.ts")).href);
const pkg = path.join(npmDir, "node_modules/@earendil-works/pi-ai");
const version = JSON.parse(fs.readFileSync(path.join(pkg, "package.json"), "utf8")).version;
const { MODELS } = await import(pathToFileURL(path.join(pkg, "dist/models.generated.js")).href);
const model = MODELS.anthropic["claude-haiku-4-5"];
if (!model) throw new Error("catalog has no anthropic/claude-haiku-4-5");

function createFakeAnthropicClient(body: string): any {
	const response = new Response(body, { status: 200, headers: { "content-type": "text/event-stream" } });
	return { beta: { messages: { create: () => ({ asResponse: async () => response }) } } };
}

// A framed event, as the suite's createSseResponse writes one.
const ev = (event: string, data: string) => `event: ${event}\ndata: ${data}\n`;
const frames = (...events: string[]) => events.join("\n");

// The suite's minimalAnthropicEvents.
const messageStart = ev(
	"message_start",
	JSON.stringify({
		type: "message_start",
		message: {
			id: "msg_test",
			usage: { input_tokens: 12, output_tokens: 0, cache_read_input_tokens: 0, cache_creation_input_tokens: 0 },
		},
	}),
);
const blockStart = ev(
	"content_block_start",
	JSON.stringify({ type: "content_block_start", index: 0, content_block: { type: "text", text: "" } }),
);
const textDelta = (text: string) =>
	ev("content_block_delta", JSON.stringify({ type: "content_block_delta", index: 0, delta: { type: "text_delta", text } }));
const blockStop = ev("content_block_stop", JSON.stringify({ type: "content_block_stop", index: 0 }));
const messageDelta = ev(
	"message_delta",
	JSON.stringify({
		type: "message_delta",
		delta: { stop_reason: "end_turn" },
		usage: { input_tokens: 12, output_tokens: 5, cache_read_input_tokens: 0, cache_creation_input_tokens: 0 },
	}),
);
const messageStop = ev("message_stop", JSON.stringify({ type: "message_stop" }));
const minimal = [messageStart, blockStart, textDelta("Hello"), blockStop, messageDelta, messageStop];

type Case = { name: string; sse: string; v8Cause?: boolean; throwAt?: number; abortFirst?: boolean };
const B = String.fromCharCode(92); // a backslash, spelled so no tool decodes an escape
// U+2028 and U+2029, spelled so no tool decodes an escape: JSON.stringify writes
// both literally, as it does <, > and &.
const LS = String.fromCharCode(0x2028);
const PS = String.fromCharCode(0x2029);
const cases: Case[] = [
	// 'forwards parsed provider stream events in order'.
	{ name: "forwardsInOrder", sse: frames(...minimal) },
	// The filter is on the SSE event NAME: ping, an unknown name, a nameless
	// data-only frame and comment-only blocks are never observed, whatever their
	// JSON `type` says.
	{
		name: "skipsPingUnknownAndNameless",
		sse: frames(
			ev("ping", JSON.stringify({ type: "ping" })),
			messageStart,
			": keepalive\n",
			ev("content_block_delta_v2", JSON.stringify({ type: "content_block_delta", index: 0 })),
			`data: ${JSON.stringify({ type: "message_start", message: { id: "nameless" } })}\n`,
			blockStart,
			textDelta("Hello"),
			blockStop,
			messageDelta,
			messageStop,
		),
	},
	// A named event whose data is JSON but not an object is observed, and then
	// matches no branch (its `.type` is undefined): the stream carries on.
	{
		name: "nonObjectEventsAreObservedAndIgnored",
		sse: frames(
			messageStart,
			blockStart,
			ev("content_block_delta", "5"),
			ev("content_block_delta", "[1,2]"),
			ev("content_block_delta", '"x"'),
			ev("content_block_delta", "true"),
			textDelta("Hello"),
			blockStop,
			messageDelta,
			messageStop,
		),
	},
	// parseJsonWithRepair: an invalid escape and a raw tab inside a string are
	// repaired, and the observer sees the REPAIRED value.
	{
		name: "repairedEventIsObservedRepaired",
		sse: frames(
			messageStart,
			blockStart,
			ev(
				"content_block_delta",
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"a${B}qb\tc"}}`,
			),
			blockStop,
			messageDelta,
			messageStop,
		),
	},
	// `data: null` parses, and then iterateAnthropicEvents reads `.type` of it
	// inside its try: the stream fails with that TypeError wrapped, and the
	// observer never sees null.
	{ name: "nullMessageStartFails", sse: frames(ev("message_start", "null")) },
	{ name: "nullAfterStartFails", sse: frames(messageStart, blockStart, ev("content_block_delta", "null"), blockStop) },
	// raw= lists every non-empty line of the event, comments included. A block
	// with neither an event name nor data is not flushed, so its lines stay in
	// the buffer and lead the next event's raw...
	{
		name: "rawKeepsUnflushedLines",
		sse: ": keepalive\n\nid: 3\nretry: 5\n\n" + frames(ev("message_start", "null")),
	},
	// ...while a flushed event, even one that is then skipped, resets it.
	{
		name: "rawResetsAfterSkippedEvent",
		sse: frames(ev("ping", "{}"), ev("message_start", "null")),
	},
	// Lines split on a lone CR too.
	{ name: "rawSplitsOnCR", sse: "event: message_start\rdata: null\r\r" },
	// A JSON syntax error: the message embeds V8's JSON.parse text, then data=
	// (the data lines joined with a newline) and raw= (the lines joined with a
	// literal backslash-n).
	{
		name: "syntaxErrorCarriesDataAndRaw",
		v8Cause: true,
		sse: messageStart + "\n" + ': comment line\nid: 7\nevent: content_block_delta\ndata: {"type":\ndata: oops\n\n',
	},
	// The last event is flushed at EOF without its blank line.
	{
		name: "syntaxErrorAtEOF",
		v8Cause: true,
		sse: messageStart + "\n" + ": c\nevent: message_start\ndata: nul",
	},
	// JSON.parse builds an ordinary object, which enumerates array-index keys
	// first, ascending, then the rest in wire order, at every depth: observed
	// shows it.
	{
		name: "observedKeysEnumerateIndicesFirst",
		sse: frames(
			messageStart,
			blockStart,
			ev(
				"content_block_delta",
				'{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello","1":"x"},"10":1,"2":2,"a":3}',
			),
			blockStop,
			messageDelta,
			messageStop,
		),
	},
	// JSON.parse reads a number past float64's range as Infinity (observed as
	// JSON.stringify's null), inside an event or as the whole event, and the
	// stream carries on.
	{
		name: "overflowNumbersAreObserved",
		sse: frames(
			messageStart,
			blockStart,
			ev("content_block_delta", "1e400"),
			textDelta("Hello"),
			blockStop,
			messageDelta,
			ev("message_stop", '{"type":"message_stop","big":1e400}'),
		),
	},
	// observed is JSON.stringify text: <, >, &, U+2028 and U+2029 are written as
	// themselves, however deep in the event they sit.
	{
		name: "observedTextIsNotHTMLEscaped",
		sse: frames(messageStart, blockStart, textDelta(`<b>&x> ${LS} ${PS} end`), blockStop, messageDelta, messageStop),
	},
	// pi reads each event's properties as whatever they hold, so a member of an
	// unexpected type never fails the event: a count written 5.0 is 5, a
	// cache_creation that is not an object has no 1h count, and a string index
	// is strictly unequal to every block's number, so its delta finds no block.
	// Every event is observed.
	{
		name: "mistypedMembersAreRead",
		sse: frames(
			messageStart,
			blockStart,
			ev("content_block_delta", '{"type":"content_block_delta","index":"0","delta":{"type":"text_delta","text":"x"}}'),
			textDelta("Hello"),
			blockStop,
			ev(
				"message_delta",
				'{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":12,"output_tokens":5.0,"cache_creation":5}}',
			),
			messageStop,
		),
	},
	// A delta's text is appended as String() writes it: a missing text as
	// "undefined", 5 as "5". A tool block starts with the arguments its input
	// holds, which the stream's failure then leaves in the message.
	{
		name: "deltasAppendStringForms",
		sse: frames(
			messageStart,
			blockStart,
			ev("content_block_delta", '{"type":"content_block_delta","index":0,"delta":{"type":"text_delta"}}'),
			ev("content_block_delta", '{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":5}}'),
			blockStop,
			ev(
				"content_block_start",
				'{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"t1","name":"read","input":{"path":"a","2":"b"}}}',
			),
			ev("content_block_delta", "null"),
		),
	},
	// content_block_stop deletes the block's index, so a later event with no
	// index (undefined === undefined) finds that block: its partialJson, also
	// deleted, is appended to as "undefined", which parses to no arguments.
	{
		name: "deletedIndexMatchesAnEventWithoutOne",
		sse: frames(
			messageStart,
			ev("content_block_start", '{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"t1","name":"read"}}'),
			ev("content_block_delta", '{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\\"a\\":1}"}}'),
			ev("content_block_stop", '{"type":"content_block_stop","index":0}'),
			ev("content_block_delta", '{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":""}}'),
			ev("content_block_stop", '{"type":"content_block_stop"}'),
			messageDelta,
			messageStop,
		),
	},
	// message_start reads no reasoning breakdown; message_delta's does.
	{
		name: "reasoningOnlyFromMessageDelta",
		sse: frames(
			ev(
				"message_start",
				'{"type":"message_start","message":{"id":"msg_test","usage":{"input_tokens":12,"output_tokens":1,"output_tokens_details":{"thinking_tokens":1}}}}',
			),
			blockStart,
			textDelta("Hello"),
			blockStop,
			ev("message_delta", '{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}'),
			messageStop,
		),
	},
	// A stop reason that is not a string matches no case: its String() is in
	// the error. A refusal's explanation reaches the error through
	// `new Error(...)`, which takes String() of it too.
	{ name: "nonStringStopReasonIsUnhandled", sse: frames(messageStart, ev("message_delta", '{"type":"message_delta","delta":{"stop_reason":5}}')) },
	{
		name: "refusalExplanationIsStringified",
		sse: frames(
			messageStart,
			ev("message_delta", '{"type":"message_delta","delta":{"stop_reason":"refusal","stop_details":{"explanation":[1,2]}}}'),
			messageStop,
		),
	},
	// A property read from a missing or null sub-object throws V8's TypeError,
	// in pi's read order: message.id before message.usage.input_tokens, the
	// block's type, the delta's type or stop_reason. What was assigned before
	// the throw (the response id) stays in the message.
	{ name: "messageStartWithoutUsageFails", sse: frames(ev("message_start", '{"type":"message_start","message":{"id":"m1"}}')) },
	{ name: "messageStartNullMessageFails", sse: frames(ev("message_start", '{"type":"message_start","message":null}')) },
	{ name: "messageStartWithoutMessageFails", sse: frames(ev("message_start", '{"type":"message_start"}')) },
	{ name: "messageStartScalarMessageFailsOnUsage", sse: frames(ev("message_start", '{"type":"message_start","message":5}')) },
	{ name: "contentBlockStartWithoutBlockFails", sse: frames(messageStart, ev("content_block_start", '{"type":"content_block_start","index":0}')) },
	{ name: "contentBlockDeltaWithoutDeltaFails", sse: frames(messageStart, blockStart, ev("content_block_delta", '{"type":"content_block_delta","index":0}')) },
	{ name: "messageDeltaWithoutDeltaFails", sse: frames(messageStart, ev("message_delta", '{"type":"message_delta","usage":{"output_tokens":5}}')) },
	// A throwing observer fails the stream with its message (pi awaits the
	// callback inside the adapter's try): on the first event...
	{ name: "observerThrowsOnFirstEvent", sse: frames(...minimal), throwAt: 0 },
	// ...mid-stream, before the event it was handed is applied...
	{ name: "observerThrowsMidStream", sse: frames(...minimal), throwAt: 2 },
	// ...and, when the request was aborted first, as an aborted stream.
	{ name: "observerThrowsAfterAbort", sse: frames(...minimal), throwAt: 1, abortFirst: true },
];

const context = normalizeContext({ messages: [{ role: "user", content: "Hello", timestamp: 1 }] });
const rows = [];
for (const c of cases) {
	const observed: string[] = [];
	const controller = new AbortController();
	const s = streamAnthropic(model, context, {
		client: createFakeAnthropicClient(c.sse),
		signal: controller.signal,
		onProviderStreamEvent: async (event: unknown, eventModel: unknown) => {
			await Promise.resolve();
			if (eventModel !== model) throw new Error(`${c.name}: observer got another model`);
			if (event === null) throw new Error(`${c.name}: observer got null`);
			if (c.throwAt === observed.length) {
				observed.push(JSON.stringify(event));
				if (c.abortFirst) controller.abort();
				throw new Error("observer boom");
			}
			observed.push(JSON.stringify(event));
		},
	});
	const pushed: Array<string | null> = [];
	for await (const event of s) pushed.push(event.type ?? null);
	const message = await s.result();
	if (c.v8Cause && !/^Could not parse Anthropic SSE event [a-z_]+: (Unexpected|Expected)/.test(message.errorMessage)) {
		throw new Error(`${c.name}: not a JSON.parse failure: ${message.errorMessage}`);
	}
	rows.push({
		name: c.name,
		sse: c.sse,
		...(c.v8Cause ? { v8Cause: true } : {}),
		...(c.throwAt !== undefined ? { throwAt: c.throwAt } : {}),
		...(c.abortFirst ? { abortFirst: true } : {}),
		observed,
		pushed,
		message: {
			stopReason: message.stopReason,
			errorMessage: message.errorMessage ?? null,
			responseId: message.responseId ?? null,
			content: message.content,
			usage: message.usage,
		},
	});
}
// One row per line, so a re-capture diffs row by row.
const text = rows.map((row) => JSON.stringify(row)).join(",\n");
fs.writeFileSync(
	outFile,
	`{"sha":${JSON.stringify(sha)},"pi-ai":${JSON.stringify(version)},"model":"anthropic/claude-haiku-4-5","rows":[\n${text}\n]}\n`,
);
console.log(`captured ${rows.length} rows from packages/ai/src at ${sha} (model: pi-ai ${version}) -> ${outFile}`);
