// Captures how real pi's pi-messages adapter reads a wire stream — the events
// onProviderStreamEvent observes, the events the stream pushes and the final
// message — the oracle behind TestPiMessagesEventsMatchPi (upstream 002fc8385
// plus readPiMessagesEvents' frame rules).
//
//   node --experimental-strip-types capture-pi-messages-events.mts <extraction> <out.json> <sha>
//   e.g. ... capture-pi-messages-events.mts <dir> pi-messages-events-8676a0dcd.json 8676a0dcd
//
// <extraction> holds packages/ai at <sha> (`git archive <sha> packages/ai |
// tar -x -C <dir>` from the upstream clone), with packages/ai/node_modules
// resolving pi-ai's dependencies (the npm build's node_modules serves; this
// adapter calls fetch directly, so no SDK is involved). The adapter runs from
// SOURCE: onProviderStreamEvent is in no published build yet.
//
// The model is the upstream suite's createModel (pi-messages.test.ts), and the
// body is served through options.fetch, so no network is needed. Each row
// records:
//   sse        the exact body, which the Go test replays byte for byte;
//   observed   each value onProviderStreamEvent received, as JSON.stringify
//              text, so key order is part of the expectation;
//   pushed     the type of every event the stream pushed (null: no type);
//   pushedIndex the contentIndex every pushed event carries (null: none);
//   message    stopReason, errorMessage, responseId, content and usage of the
//              result, and the type and details of each diagnostic (not their
//              timestamps);
//   v8Error    the frame data whose JSON.parse failure is the errorMessage:
//              V8's text, which the port does not reproduce.
// A row may make the observer throw ("observer boom") on the observed event at
// index throwAt, after aborting the request when abortFirst is set.
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";

const [extraction, outFile, sha] = process.argv.slice(2);
if (!extraction || !outFile || !sha) {
	console.error("usage: node --experimental-strip-types capture-pi-messages-events.mts <extraction> <out.json> <sha>");
	process.exit(2);
}
const src = path.join(extraction, "packages/ai/src");
const { streamSimple } = await import(pathToFileURL(path.join(src, "api/pi-messages.ts")).href);
const { normalizeContext } = await import(pathToFileURL(path.join(src, "utils/transcript.ts")).href);

// The suite's createModel.
const model = {
	id: "auto",
	name: "Radius Auto",
	api: "pi-messages",
	provider: "radius",
	baseUrl: "http://pi-messages.invalid/v1",
	reasoning: false,
	input: ["text"],
	cost: { input: 1, output: 2, cacheRead: 0.1, cacheWrite: 0.2 },
	contextWindow: 128000,
	maxTokens: 16384,
};

// The suite's usage and upstream's 'forwards parsed wire events in order before
// converting them' wire events.
const usage = {
	input: 10,
	output: 5,
	cacheRead: 0,
	cacheWrite: 0,
	totalTokens: 15,
	cost: { input: 0.1, output: 0.2, cacheRead: 0, cacheWrite: 0, total: 0.3 },
};
const wireEvents = [
	{ type: "start" },
	{ type: "text_start", contentIndex: 0 },
	{ type: "text_delta", contentIndex: 0, delta: "Hello", gatewayField: "upstream-value" },
	{ type: "text_end", contentIndex: 0, content: "Hello" },
	{ type: "done", reason: "stop", usage, responseId: "resp_1" },
];
// The suite's server writes each event as one `data:` frame.
const frame = (data: string) => `data: ${data}\n\n`;
const framed = (...events: unknown[]) => events.map((event) => frame(JSON.stringify(event))).join("");
const [start, textStart, textDelta, textEnd, done] = wireEvents;
// Text JSON.stringify writes as itself where encoding/json escapes it: <, > and
// &, and U+2028 and U+2029 (spelled so no tool decodes an escape).
const markup = `<b>&x> ${String.fromCharCode(0x2028)} ${String.fromCharCode(0x2029)} end`;

type Case = { name: string; sse: string; v8Error?: string; throwAt?: number; abortFirst?: boolean };
const cases: Case[] = [
	{ name: "forwardsWireEventsInOrder", sse: framed(...wireEvents) },
	// `if (event)`: a frame parsing to a JS-falsy value is neither observed nor
	// converted, and neither is a frame with no data line, an empty data line or
	// [DONE].
	{
		name: "falsyFramesAreSkipped",
		sse:
			framed(start) +
			frame("null") +
			frame("false") +
			frame("0") +
			frame("-0") +
			frame('""') +
			frame("") +
			frame("[DONE]") +
			"event: ping\n\n" +
			framed(textStart, textDelta, textEnd, done),
	},
	// A truthy value that is not an object is observed, then converted into an
	// event with no type (pi returns `{...event, partial}`).
	{
		name: "truthyNonObjectsAreObservedAndPushed",
		sse: framed(start) + frame("5") + frame('"x"') + frame("true") + frame("[1]") + framed(textStart, textDelta, textEnd, done),
	},
	// Only a frame's FIRST data line is read, trimmed as String.prototype.trim.
	{
		name: "firstDataLineOnly",
		sse: `data: ${JSON.stringify(start)}\ndata: ${JSON.stringify(textStart)}\n\n` + framed(textStart, textDelta, textEnd) + `data: \t${JSON.stringify(done)}  \n\n`,
	},
	// CRLF framing is normalized, and a trailing frame with no blank line after
	// it is still read.
	{
		name: "crlfAndTrailingFrame",
		sse: framed(start, textStart, textDelta, textEnd).replaceAll("\n", "\r\n") + `data: ${JSON.stringify(done)}`,
	},
	// JSON.parse throws on a frame that is not JSON: the stream fails with V8's
	// SyntaxError text.
	{ name: "unparseableFrameFails", sse: framed(start, textStart) + frame("{oops") + framed(textEnd, done), v8Error: "{oops" },
	// The trailing frame is parsed the same way.
	{ name: "unparseableTrailingFrameFails", sse: framed(start, textStart) + "data: [1,", v8Error: "[1," },
	// Unknown wire event types are observed and converted like any other.
	{
		name: "unknownTypeIsObservedAndPushed",
		sse: framed(start, { type: "gateway_note", note: "n" }, textStart, textDelta, textEnd, done),
	},
	// JSON.parse builds an ordinary object, which enumerates array-index keys
	// first, ascending, then the rest in wire order, at every depth: observed
	// shows it.
	{
		name: "observedKeysEnumerateIndicesFirst",
		sse:
			framed(start, textStart) +
			frame('{"type":"text_delta","contentIndex":0,"delta":"Hello","7":1,"b":{"z":1,"0":2},"1":3}') +
			framed(textEnd, done),
	},
	// JSON.parse reads a number past float64's range as +-Infinity (observed
	// as JSON.stringify's null), which is truthy, and one below the smallest
	// subnormal as 0, which is not: the frame is skipped.
	{
		name: "overflowNumbersAreObserved",
		sse:
			framed(start) +
			frame("1e400") +
			frame("-1e400") +
			frame("1e-400") +
			frame('{"type":"gateway_note","x":1e400}') +
			framed(textStart, textDelta, textEnd, done),
	},
	// observed is JSON.stringify text: <, >, &, U+2028 and U+2029 are written as
	// themselves, however deep in the event they sit.
	{
		name: "observedTextIsNotHTMLEscaped",
		sse: framed(start, textStart, { ...textDelta, delta: markup }, { ...textEnd, content: markup }, done),
	},
	// A member of an unexpected JSON type never drops the event: pi reads each
	// property as whatever it holds. An unknown type with a string contentIndex
	// and an object with no type are observed and pushed; a delta that is not a
	// string is appended as String() writes it (5 as "5", none as "undefined",
	// null as "null", [1,[2,null]] as "1,2,"); counts written 10.0 and 5e0 are
	// the numbers 10 and 5; and a rewrite's members, whatever they hold, are the
	// diagnostic's details.
	{
		name: "mistypedMembersStillConvert",
		sse:
			framed(start) +
			frame('{"type":"gateway_note","contentIndex":"x"}') +
			frame('{"note":1}') +
			framed(textStart) +
			frame('{"type":"text_delta","contentIndex":0,"delta":5}') +
			frame('{"type":"text_delta","contentIndex":0}') +
			frame('{"type":"text_delta","contentIndex":0,"delta":null}') +
			frame('{"type":"text_delta","contentIndex":0,"delta":[1,[2,null]]}') +
			frame(
				'{"type":"done","reason":"stop","usage":{"input":10.0,"output":5e0,"cacheRead":0,"cacheWrite":0,"totalTokens":15.0,' +
					'"cost":{"input":0.1,"output":0.2,"cacheRead":0,"cacheWrite":0,"total":0.3}},"responseId":"resp_1",' +
					'"rewrite":{"policyId":"p","policyVersion":"2","changed":true,"extra":[1]}}',
			),
	},
	// A property is read by its exact name: "Type" and "DELTA" are not the
	// event's type and delta, so the frame converts as an event with no type.
	{
		name: "memberNamesMatchExactly",
		sse: framed(start, textStart) + frame('{"Type":"text_delta","contentIndex":0,"DELTA":"x"}') + framed(textDelta, done),
	},
	// A delta with no string form (an object with its own toString member)
	// makes pi's `+=` throw V8's TypeError, which fails the stream.
	{
		name: "deltaWithNoStringFormFails",
		sse: framed(start, textStart) + frame('{"type":"text_delta","contentIndex":0,"delta":{"toString":1}}') + framed(textEnd, done),
	},
	// contentIndex is a property key of the content array: any value whose
	// String() is a canonical array index addresses that slot ("1", [1] and
	// 1.0 all address slot 1, -0 slot 0), and any other ("01", 1.5, -1, an
	// absent one as "undefined") names an ordinary property, which is not
	// content: the message never holds it, and it never clobbers a slot.
	{
		name: "contentIndexIsAPropertyKey",
		sse:
			framed(start, textStart) +
			frame('{"type":"text_delta","contentIndex":-0,"delta":"a"}') +
			frame('{"type":"text_start","contentIndex":"1"}') +
			frame('{"type":"text_delta","contentIndex":[1],"delta":"b"}') +
			frame('{"type":"text_delta","contentIndex":1.0,"delta":"c"}') +
			frame('{"type":"text_start"}') +
			frame('{"type":"text_delta","delta":"x"}') +
			frame('{"type":"text_start","contentIndex":"01"}') +
			frame('{"type":"text_delta","contentIndex":"01","delta":"y"}') +
			frame('{"type":"thinking_start","contentIndex":1.5}') +
			frame('{"type":"thinking_delta","contentIndex":1.5,"delta":"z"}') +
			frame('{"type":"thinking_end","contentIndex":1.5,"content":"z"}') +
			frame('{"type":"toolcall_start","contentIndex":-1,"id":"t1","toolName":"read"}') +
			frame('{"type":"toolcall_delta","contentIndex":-1,"delta":"{}"}') +
			frame('{"type":"toolcall_end","contentIndex":-1}') +
			framed(done),
	},
	// The partial tool JSON is kept in a Map keyed by the contentIndex VALUE
	// (SameValueZero), so "0" and 0 address one block but two buffers.
	{
		name: "toolJsonIsKeyedByTheIndexValue",
		sse:
			framed(start) +
			frame('{"type":"toolcall_start","contentIndex":0,"id":"t1","toolName":"read"}') +
			frame('{"type":"toolcall_delta","contentIndex":"0","delta":"{\\"a\\":1"}') +
			frame('{"type":"toolcall_delta","contentIndex":0,"delta":"{\\"b\\":2"}') +
			frame('{"type":"toolcall_delta","contentIndex":-0,"delta":",\\"c\\":3}"}') +
			framed(done),
	},
	// toolcall_end is Object.assign: the end event's toolCall members replace
	// the block's, and a member it lacks (here arguments) keeps the value the
	// deltas built. A null member is assigned as null. An end with no toolCall
	// leaves the block as it is.
	{
		name: "toolcallEndMergesIntoTheBlock",
		sse:
			framed(start) +
			frame('{"type":"toolcall_start","contentIndex":0,"id":"t1","toolName":"read"}') +
			frame('{"type":"toolcall_delta","contentIndex":0,"delta":"{\\"path\\":\\"a\\"}"}') +
			frame('{"type":"toolcall_end","contentIndex":0,"toolCall":{"type":"toolCall","name":"read2","thoughtSignature":"sig"}}') +
			frame('{"type":"toolcall_start","contentIndex":1,"id":"t2","toolName":"write"}') +
			frame('{"type":"toolcall_delta","contentIndex":1,"delta":"{\\"x\\":1}"}') +
			frame('{"type":"toolcall_end","contentIndex":1,"toolCall":{"type":"toolCall","id":"t2b","arguments":null}}') +
			frame('{"type":"toolcall_start","contentIndex":2,"id":"t3","toolName":"ls"}') +
			frame('{"type":"toolcall_end","contentIndex":2}') +
			framed(done),
	},
	// text_end and thinking_end assign the event's contentSignature (and
	// redacted) whether or not it carries one: a later end without one leaves
	// the block with none.
	{
		name: "endAssignsAnAbsentSignature",
		sse:
			framed(start, textStart) +
			frame('{"type":"text_end","contentIndex":0,"content":"a","contentSignature":"s1"}') +
			frame('{"type":"text_end","contentIndex":0,"content":"b"}') +
			frame('{"type":"thinking_start","contentIndex":1}') +
			frame('{"type":"thinking_end","contentIndex":1,"content":"t","contentSignature":"s2","redacted":true}') +
			frame('{"type":"thinking_end","contentIndex":1,"content":"u"}') +
			framed(done),
	},
	// An event for a block that was never started reads a property of
	// undefined, and V8's TypeError fails the stream: a delta reads the
	// block's text or thinking, a toolcall_delta sets its arguments, and an end
	// is Object.assign onto it. A hole in the array is undefined too.
	{ name: "textDeltaWithoutBlockFails", sse: framed(start) + frame('{"type":"text_delta","contentIndex":0,"delta":"x"}') + framed(done) },
	{
		name: "thinkingDeltaIntoAHoleFails",
		sse:
			framed(start) +
			frame('{"type":"text_start","contentIndex":2}') +
			frame('{"type":"thinking_delta","contentIndex":1,"delta":"x"}') +
			framed(done),
	},
	{ name: "textEndWithoutBlockFails", sse: framed(start) + frame('{"type":"text_end","contentIndex":3,"content":"x"}') + framed(done) },
	{
		name: "toolcallDeltaWithoutBlockFails",
		sse: framed(start) + frame('{"type":"toolcall_delta","contentIndex":0,"delta":"{}"}') + framed(done),
	},
	{
		name: "toolcallEndWithoutBlockFails",
		sse:
			framed(start) +
			frame('{"type":"toolcall_end","contentIndex":0,"toolCall":{"type":"toolCall","id":"t1","name":"read","arguments":{}}}') +
			framed(done),
	},
	// A throwing observer fails the stream with its message: on the first event,
	{ name: "observerThrowsOnFirstEvent", sse: framed(...wireEvents), throwAt: 0 },
	// on the terminal done event, which then never converts to done,
	{ name: "observerThrowsOnDone", sse: framed(...wireEvents), throwAt: 4 },
	// and, when the request was aborted first, as an aborted stream.
	{ name: "observerThrowsAfterAbort", sse: framed(...wireEvents), throwAt: 2, abortFirst: true },
];

const context = normalizeContext({ messages: [{ role: "user", content: "Hello", timestamp: 1 }] });
const rows = [];
for (const c of cases) {
	const observed: string[] = [];
	const controller = new AbortController();
	const s = streamSimple(model, context, {
		apiKey: "test-key",
		signal: controller.signal,
		fetch: async () => new Response(c.sse, { status: 200, headers: { "content-type": "text/event-stream" } }),
		onProviderStreamEvent: async (event: unknown, eventModel: unknown) => {
			await Promise.resolve();
			if (eventModel !== model) throw new Error(`${c.name}: observer got another model`);
			observed.push(JSON.stringify(event));
			if (c.throwAt === observed.length - 1) {
				if (c.abortFirst) controller.abort();
				throw new Error("observer boom");
			}
		},
	});
	const pushed: Array<string | null> = [];
	const pushedIndex: unknown[] = [];
	for await (const event of s) {
		pushed.push(event.type ?? null);
		pushedIndex.push((event as { contentIndex?: unknown }).contentIndex ?? null);
	}
	const message = await s.result();
	if (c.v8Error !== undefined && !/^(Unexpected|Expected)/.test(message.errorMessage)) {
		throw new Error(`${c.name}: not a JSON.parse failure: ${message.errorMessage}`);
	}
	rows.push({
		name: c.name,
		sse: c.sse,
		...(c.v8Error !== undefined ? { v8Error: c.v8Error } : {}),
		...(c.throwAt !== undefined ? { throwAt: c.throwAt } : {}),
		...(c.abortFirst ? { abortFirst: true } : {}),
		observed,
		pushed,
		pushedIndex,
		message: {
			stopReason: message.stopReason,
			errorMessage: message.errorMessage ?? null,
			responseId: message.responseId ?? null,
			content: message.content,
			usage: message.usage,
			diagnostics: (message.diagnostics ?? []).map((d: { type: string; details?: unknown }) => ({
				type: d.type,
				details: d.details ?? null,
			})),
		},
	});
}
// One row per line, so a re-capture diffs row by row.
const text = rows.map((row) => JSON.stringify(row)).join(",\n");
fs.writeFileSync(outFile, `{"sha":${JSON.stringify(sha)},"rows":[\n${text}\n]}\n`);
console.log(`captured ${rows.length} rows from packages/ai/src at ${sha} -> ${outFile}`);
