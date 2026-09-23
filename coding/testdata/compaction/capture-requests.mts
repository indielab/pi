// Captures the summarization requests real pi's compact() sends — the oracle
// behind TestSummarizationRequestsMatchPiCapture in
// coding/compaction_request_test.go.
//
//   node capture-requests.mts <compaction-module> <out.json> <version>
//   e.g. node capture-requests.mts ~/.cache/pi-npm/0.87.1/node_modules/@earendil-works/pi-coding-agent/dist/core/compaction/index.js requests-0.87.1.json 0.87.1
//
// <compaction-module> exports compact, createFileOps and
// extractFileOpsFromMessage: the npm build's dist/core/compaction/index.js
// (0.87.1 is the first build that frames the split-turn prefix as
// "# Conversation" / "# Instructions", upstream d192bd6dc).
//
// Each scenario is a transcript whose first historyCount messages are the
// history to summarize and whose next prefixCount messages are the split
// turn's prefix; the rest is kept. The preparation carries those lists, the
// optional previous summary and the file operations of both lists, as
// prepareCompaction builds them. compact() runs with a stub streamFn that
// records each request's system prompt, user text and maxTokens, and answers
// request n with "SUMMARY n". The output keeps the messages as pi reads them,
// the requests in order, and the summary compact() returns.
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";

const [entry, outFile, version] = process.argv.slice(2);
if (!entry || !outFile || !version) {
	console.error("usage: node capture-requests.mts <compaction-module> <out.json> <version>");
	process.exit(2);
}
const { compact, createFileOps, extractFileOpsFromMessage } = await import(pathToFileURL(path.resolve(entry)).href);

const usage = { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, totalTokens: 0, cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 } };
let clock = 0;
const user = (content: string | Array<{ type: "text"; text: string }>) => ({ role: "user", content, timestamp: ++clock });
const assistant = (text: string | null, calls: Array<[string, string, Record<string, unknown>]> = [], stopReason = "stop") => ({
	role: "assistant",
	content: [
		...(text === null ? [] : [{ type: "text", text }]),
		...calls.map(([id, name, args]) => ({ type: "toolCall", id, name, arguments: args })),
	],
	api: "openai-completions",
	provider: "p",
	model: "m",
	usage,
	stopReason,
	timestamp: ++clock,
});
const toolResult = (id: string, name: string, text: string) => ({
	role: "toolResult",
	toolCallId: id,
	toolName: name,
	content: [{ type: "text", text }],
	isError: false,
	timestamp: ++clock,
});

const reserveTokens = 2000;
const model = {
	id: "m",
	name: "m",
	api: "openai-completions",
	provider: "p",
	baseUrl: "http://localhost",
	reasoning: false,
	input: ["text"],
	cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
	contextWindow: reserveTokens,
	maxTokens: 8192,
};

const turnWithTools = () => [
	user([{ type: "text", text: "please refactor the parser" }]),
	assistant("reading it first", [["r1", "read", { path: "/a/parser.go" }]], "toolUse"),
	toolResult("r1", "read", "package parser"),
	assistant("refactored the parser"),
	user([{ type: "text", text: "now fix the tests" }]),
	assistant(null, [["e1", "edit", { path: "/a/parser_test.go", oldText: "a", newText: "b" }]], "toolUse"),
	toolResult("e1", "edit", "ok"),
	assistant("tests fixed"),
];

const scenarios: Array<{ name: string; messages: any[]; historyCount: number; prefixCount: number; previousSummary?: string }> = [
	{
		// agent-session-compaction.test.ts seedCompactableSession under keepRecentTokens 1.
		name: "split-turn-prefix-only",
		messages: [user([{ type: "text", text: "message to compact" }]), assistant("assistant response to compact")],
		historyCount: 0,
		prefixCount: 1,
	},
	{
		// compaction-summary-reasoning.test.ts "preserves the previous summary without
		// an empty history request for a split turn".
		name: "split-turn-previous-summary",
		messages: [user("Summarize this."), assistant("kept")],
		historyCount: 0,
		prefixCount: 1,
		previousSummary: "previous checkpoint",
	},
	{ name: "split-turn-with-history", messages: turnWithTools(), historyCount: 4, prefixCount: 3 },
	{
		name: "split-turn-with-history-and-previous-summary",
		messages: turnWithTools(),
		historyCount: 4,
		prefixCount: 3,
		previousSummary: "## Goal\nearlier work",
	},
	{
		name: "history-only",
		messages: [user("first question"), assistant("first answer"), user("second question")],
		historyCount: 2,
		prefixCount: 0,
	},
	{
		name: "history-only-previous-summary",
		messages: [user("first question"), assistant("first answer"), user("second question")],
		historyCount: 2,
		prefixCount: 0,
		previousSummary: "## Goal\nearlier work",
	},
];

const textOf = (content: unknown): string =>
	typeof content === "string"
		? content
		: (content as Array<{ type: string; text?: string }>)
				.filter((block) => block.type === "text")
				.map((block) => block.text)
				.join("");

const out = { version, reserveTokens, modelMaxTokens: model.maxTokens, scenarios: [] as unknown[] };
for (const { name, messages, historyCount, prefixCount, previousSummary } of scenarios) {
	const messagesToSummarize = messages.slice(0, historyCount);
	const turnPrefixMessages = messages.slice(historyCount, historyCount + prefixCount);
	const fileOps = createFileOps();
	for (const message of [...messagesToSummarize, ...turnPrefixMessages]) extractFileOpsFromMessage(message, fileOps);

	const requests: Array<{ systemPrompt: string; text: string; maxTokens: number }> = [];
	const streamFn = (_model: unknown, context: { messages: Array<{ role: string; content: unknown }> }, options: { maxTokens: number }) => {
		const system = context.messages.filter((message) => message.role === "system");
		const users = context.messages.filter((message) => message.role === "user");
		if (system.length !== 1 || users.length !== 1 || context.messages.length !== 2) {
			throw new Error(`${name}: unexpected request shape ${JSON.stringify(context.messages.map((message) => message.role))}`);
		}
		requests.push({ systemPrompt: textOf(system[0].content), text: textOf(users[0].content), maxTokens: options.maxTokens });
		const response = { ...assistant(`SUMMARY ${requests.length}`), timestamp: 0 };
		return { result: async () => response };
	};

	const result = await compact(
		{
			firstKeptEntryId: "kept",
			messagesToSummarize,
			turnPrefixMessages,
			isSplitTurn: prefixCount > 0,
			tokensBefore: 0,
			previousSummary,
			fileOps,
			settings: { enabled: true, reserveTokens, keepRecentTokens: 1 },
		},
		model,
		"test-key",
		undefined,
		undefined,
		undefined,
		undefined,
		streamFn,
	);
	out.scenarios.push({
		name,
		messages,
		historyCount,
		prefixCount,
		...(previousSummary === undefined ? {} : { previousSummary }),
		requests,
		summary: result.summary,
	});
}
fs.writeFileSync(outFile, `${JSON.stringify(out, null, "\t")}\n`);
