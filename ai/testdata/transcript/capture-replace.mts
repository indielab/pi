// Captures how real pi replays and serializes system messages carrying
// `replace` — the oracle behind ai/transcript_replace_test.go.
//
//   node --experimental-strip-types capture-replace.mts <extraction> <out.json> <sha>
//   e.g. ... capture-replace.mts <dir> replace-e4c75a732.json e4c75a732
//
// <extraction> holds packages/ai at <sha> (`git archive <sha> packages/ai` from
// the upstream clone) and a node_modules resolving pi-ai's dependencies (the npm
// build's). The npm build 0.85.1 predates upstream e4c75a732, so these are src
// captures: re-verify them against the first build that ships it (the BUILD
// wins).
//
// `upstream` is packages/ai/test/system-message-replay.test.ts's 'a replacement
// discards replayed state and collapses even for native providers': its shared
// `transcript`, `replacement` and `patch`, and what the case asserts on them.
// Every other case pins a rule that case leaves open; each records its messages
// as written (a JS object literal, so JSON.stringify shows the producer's key
// order) and what the replay helpers return for them. `producers` are the key
// orders of the literals pi builds: agent-session.ts
// _preparePromptAndToolLoadout's `{ role: "system", ...desired, replace: true,
// timestamp }`, and the object JSON.parse returns for a session line.
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";

const [extraction, outFile, sha] = process.argv.slice(2);
if (!extraction || !outFile || !sha) {
	console.error("usage: node --experimental-strip-types capture-replace.mts <extraction> <out.json> <sha>");
	process.exit(2);
}
const src = path.join(extraction, "packages/ai/src");
const load = (file: string) => import(pathToFileURL(path.join(src, file)).href);
const { collapseSystemMessages, getCurrentSystemMessage, getCurrentSystemPrompt, getCurrentTools, normalizeContext, resolveTranscript } =
	await load("utils/transcript.ts");
const { Type } = await import(pathToFileURL(path.join(extraction, "node_modules/typebox/build/index.mjs")).href);

function tool(name: string, description = `${name} tool`) {
	return { name, description, parameters: Type.Object({}) };
}

// system-message-replay.test.ts `transcript`.
const transcript = normalizeContext({
	messages: [
		{
			role: "system",
			content: "base",
			sections: { a: "<a>1</a>", b: "<b>1</b>" },
			toolsAdded: [tool("first")],
			timestamp: 10,
		},
		{ role: "user", content: "hello", timestamp: 11 },
		{ role: "system", content: "also do this", timestamp: 12 },
		{ role: "assistant", content: [{ type: "text", text: "ok" }], timestamp: 13 },
		{
			role: "system",
			content: "",
			sections: { a: "<a>2</a>", b: null, c: "<c>1</c>" },
			toolsRemoved: [{ name: "first" }],
			toolsAdded: [tool("second")],
			timestamp: 14,
		},
	],
});

// The case's `replacement` and `patch`.
const replacement = { role: "system", content: "forced", toolsAdded: [tool("third")], replace: true, timestamp: 15 };
const patch = { role: "system", content: "", sections: { d: "<d>1</d>" }, timestamp: 16 };
const context = { messages: [...transcript.messages, replacement, patch] };
const leading = { messages: [replacement, patch] };

const upstream = {
	transcript: transcript.messages,
	replacement,
	patch,
	current: getCurrentSystemMessage(context.messages),
	resolvedNative: resolveTranscript(context, true),
	collapsed: collapseSystemMessages(context),
	transcriptNativeIsSame: resolveTranscript(transcript, true) === transcript,
	leadingNativeIsSame: resolveTranscript(leading, true) === leading,
};

// Rules the upstream case leaves open, each as { messages, replay results }.
const user = (text: string, timestamp: number) => ({ role: "user", content: text, timestamp });
const scenarios: Record<string, unknown[]> = {
	// A replacement's own toolsRemoved runs after the clear and its toolsAdded
	// after that, so it can only add; later deltas apply on top.
	"replacement-then-tool-deltas": [
		{ role: "system", content: "base", toolsAdded: [tool("a"), tool("b")], timestamp: 1 },
		user("u", 2),
		{ role: "system", content: "", toolsRemoved: [{ name: "b" }], toolsAdded: [tool("c"), tool("a")], replace: true, timestamp: 3 },
		{ role: "system", content: "", toolsAdded: [tool("b")], toolsRemoved: [{ name: "c" }], timestamp: 4 },
	],
	// Content and sections after a replacement accumulate on the new baseline;
	// the replayed head keeps the FIRST system message's timestamp.
	"replacement-with-sections-then-content": [
		{ role: "system", content: "base", sections: { a: "<a>1</a>", z: "<z>1</z>" }, timestamp: 7 },
		user("u", 8),
		{ role: "system", content: "", sections: { z: "<z>2</z>", "5": "five", a: "<a>2</a>" }, replace: true, timestamp: 9 },
		{ role: "system", content: "after", sections: { z: null, b: "<b>1</b>" }, timestamp: 10 },
	],
	// A replacement with nothing in it empties the prompt and the tools; the
	// head still exists, with empty content.
	"empty-replacement": [
		{ role: "system", content: "base", sections: { a: "A" }, toolsAdded: [tool("a")], timestamp: 3 },
		user("u", 4),
		{ role: "system", content: "", replace: true, timestamp: 5 },
	],
	// Two replacements: only the last baseline survives.
	"two-replacements": [
		{ role: "system", content: "base", timestamp: 1 },
		{ role: "system", content: "first", toolsAdded: [tool("x")], replace: true, timestamp: 2 },
		user("u", 3),
		{ role: "system", content: "extra", timestamp: 4 },
		{ role: "system", content: [{ type: "text", text: "second" }, { type: "text", text: "line" }], replace: true, timestamp: 5 },
	],
	// `replace: false` is no replacement: replay accumulates and a native
	// provider keeps the transcript in place (resolveTranscript tests `=== true`).
	"replace-false": [
		{ role: "system", content: "base", toolsAdded: [tool("a")], timestamp: 1 },
		user("u", 2),
		{ role: "system", content: "more", replace: false, timestamp: 3 },
	],
	// A leading replacement is the baseline itself: nothing to collapse.
	"leading-replacement": [
		{ role: "system", content: "forced", toolsAdded: [tool("a")], replace: true, timestamp: 1 },
		user("u", 2),
		{ role: "system", content: "more", timestamp: 3 },
	],
	// Index 0 is the only leading position: a replacement after a user message
	// with no system message before it still collapses.
	"replacement-after-leading-user": [
		user("u", 1),
		{ role: "system", content: "forced", replace: true, timestamp: 2 },
		user("v", 3),
	],
};
const replays: Record<string, unknown> = {};
for (const [name, messages] of Object.entries(scenarios)) {
	const ctx = { messages };
	const native = resolveTranscript(ctx, true);
	replays[name] = {
		messages,
		current: getCurrentSystemMessage(messages) ?? null,
		prompt: getCurrentSystemPrompt(messages),
		tools: getCurrentTools(messages).map((t: { name: string }) => t.name),
		nativeIsSame: native === ctx,
		native,
		nonNative: resolveTranscript(ctx, false),
	};
}

// Producer key orders.
const desiredForced = { content: "Exact prompt." };
const desiredSections = { content: "", sections: { preamble: "p", cwd: "<cwd>\n/x\n</cwd>" } };
const producers = {
	replaceForced: JSON.stringify({ role: "system", ...desiredForced, replace: true, timestamp: 5 }),
	replaceSections: JSON.stringify({ role: "system", ...desiredSections, replace: true, timestamp: 5 }),
	// JSON.parse keeps document order, and a false or misplaced replace survives.
	decodedMisplaced: JSON.stringify(JSON.parse('{"replace":true,"timestamp":2,"role":"system","content":"x"}')),
	decodedFalse: JSON.stringify(JSON.parse('{"role":"system","content":"x","replace":false,"timestamp":2}')),
};

fs.writeFileSync(outFile, `${JSON.stringify({ sha, upstream, replays, producers }, null, "\t")}\n`);
