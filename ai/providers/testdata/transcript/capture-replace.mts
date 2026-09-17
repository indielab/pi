// Captures the request bodies real pi builds when a transcript carries a
// system message with `replace` — the oracle behind
// transcript_replace_test.go in this package.
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
// Upstream e4c75a732 makes resolveTranscript collapse whenever a system
// message after the leading one has `replace: true`, even on models that take
// system messages in place. `forcedPrompt` is the transcript
// packages/coding-agent/test/system-prompt-updates.test.ts's 'a forced prompt
// replaces the prompt and tool state…' sends on its third request (a sections
// declaration, then a forced replacement carrying the full tool set) on the
// native Anthropic model its comment names; the other cases pin what that test
// leaves open: a patch after the replacement, a leading replacement and
// `replace: false` (both stay in place), the system-messages-only Anthropic
// model, and the two other adapters that keep system messages in place.
// compat.ts streamSimple is normalizeContext followed by the api's
// streamSimple, which is called directly here; bodies come from onPayload.
// Every case records its model and its context as JSON, and pi must build the
// same body from that JSON as from the fixture itself.
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
const { normalizeContext } = await load("utils/transcript.ts");
const apis: Record<string, any> = {
	"anthropic-messages": await load("api/anthropic-messages.ts"),
	"openai-completions": await load("api/openai-completions.ts"),
	"openai-responses": await load("api/openai-responses.ts"),
};
const { Type } = await import(pathToFileURL(path.join(extraction, "node_modules/typebox/build/index.mjs")).href);

function tool(name: string) {
	return { name, description: `${name} tool`, parameters: Type.Object({}) };
}

// transcript-tool-changes.test.ts `modelBase` and its models.
const modelBase = {
	baseUrl: "http://127.0.0.1:9",
	reasoning: true,
	input: ["text"],
	cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
	contextWindow: 100000,
	maxTokens: 1000,
};
const anthropic = (compat: Record<string, unknown>) => ({
	...modelBase,
	id: "claude-opus-5",
	name: "Claude Opus 5",
	api: "anthropic-messages",
	provider: "anthropic",
	compat,
});
const anthropicNative = anthropic({ supportsMidConvoSystemMessages: true, supportsMidConvoToolChanges: true });
const kimiK3 = {
	...modelBase,
	id: "kimi-k3",
	name: "Kimi K3",
	api: "openai-completions",
	provider: "moonshotai",
	compat: { supportsMidConvoSystemMessages: true, supportsMidConvoToolAdditions: true },
};
const gpt54 = {
	...modelBase,
	id: "gpt-5.4",
	name: "GPT-5.4",
	api: "openai-responses",
	provider: "openai",
	compat: { supportsMidConvoSystemMessages: true, supportsToolSearch: true },
};

const usage = {
	input: 0,
	output: 0,
	cacheRead: 0,
	cacheWrite: 0,
	totalTokens: 0,
	cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 },
};
const reply = (model: { api: string; provider: string; id: string }, text: string, timestamp: number) => ({
	role: "assistant",
	content: [{ type: "text", text }],
	api: model.api,
	provider: model.provider,
	model: model.id,
	usage,
	stopReason: "stop",
	timestamp,
});
const user = (text: string, timestamp: number) => ({ role: "user", content: text, timestamp });

const read = tool("read");
const bash = tool("bash");
const declaration = {
	role: "system",
	content: "",
	sections: { preamble: "You are pi.", cwd: "<cwd>\n/proj\n</cwd>" },
	toolsAdded: [read, bash],
	timestamp: 1,
};
// The agent loop fills the forced replacement's tools with the full set.
const forced = { role: "system", content: "Exact prompt.", replace: true, timestamp: 3, toolsAdded: [read, bash] };

function forcedPrompt(model: { api: string; provider: string; id: string }) {
	return {
		messages: [declaration, user("one", 2), reply(model, "first", 2), forced, user("two", 4)],
	};
}

const cases: Record<string, { model: any; context: any }> = {
	"anthropic-native-forced-prompt": { model: anthropicNative, context: forcedPrompt(anthropicNative) },
	// Later messages after a replacement fold into the collapsed head too, and
	// native tool changes then anchor on the head's tools.
	"anthropic-native-replacement-then-patch": {
		model: anthropicNative,
		context: {
			messages: [
				declaration,
				user("one", 2),
				reply(anthropicNative, "first", 2),
				{ role: "system", content: "forced", toolsAdded: [tool("late_tool")], replace: true, timestamp: 3 },
				user("two", 4),
				reply(anthropicNative, "second", 5),
				{ role: "system", content: "", sections: { d: "<d>1</d>" }, toolsAdded: [tool("extra")], timestamp: 6 },
				user("three", 7),
			],
		},
	},
	// A leading replacement is the prompt itself: the update stays in place.
	"anthropic-native-leading-replacement": {
		model: anthropicNative,
		context: {
			messages: [
				{ role: "system", content: "forced", toolsAdded: [read], replace: true, timestamp: 1 },
				user("one", 2),
				{ role: "system", content: "guidance", toolsAdded: [bash], timestamp: 3 },
			],
		},
	},
	// Only `replace: true` collapses.
	"anthropic-native-replace-false": {
		model: anthropicNative,
		context: {
			messages: [
				declaration,
				user("one", 2),
				{ role: "system", content: "guidance", toolsAdded: [tool("late_tool")], replace: false, timestamp: 3 },
			],
		},
	},
	"anthropic-system-messages-only-forced-prompt": {
		model: anthropic({ supportsMidConvoSystemMessages: true }),
		context: forcedPrompt(anthropicNative),
	},
	"completions-native-forced-prompt": { model: kimiK3, context: forcedPrompt(kimiK3) },
	"responses-native-forced-prompt": { model: gpt54, context: forcedPrompt(gpt54) },
};

async function capturePayload(model: any, ctx: unknown): Promise<unknown> {
	let captured: unknown;
	const stream = apis[model.api].streamSimple(model, normalizeContext(ctx), {
		apiKey: "test-key",
		onPayload: (payload: unknown) => {
			captured = payload;
			throw new Error("payload captured");
		},
	});
	const final = await stream.result();
	if (captured === undefined) throw new Error(`no payload: ${final.errorMessage}`);
	return JSON.parse(JSON.stringify(captured));
}

const out: Record<string, unknown> = { sha };
for (const [name, { model, context: ctx }] of Object.entries(cases)) {
	const body = await capturePayload(model, ctx);
	const recordedModel = JSON.parse(JSON.stringify(model));
	const recordedContext = JSON.parse(JSON.stringify(ctx));
	if (JSON.stringify(await capturePayload(recordedModel, recordedContext)) !== JSON.stringify(body)) {
		throw new Error(`${name}: the JSON-decoded fixture builds a different body`);
	}
	out[name] = { model: recordedModel, context: recordedContext, body };
}
fs.writeFileSync(outFile, `${JSON.stringify(out, null, "\t")}\n`);
