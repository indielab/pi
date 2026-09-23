// Captures how real pi extends a compaction it finds in a resumed session file —
// the oracle behind TestResumedCompactionMatchesPiCapture in
// coding/compaction_resume_test.go.
//
//   node capture-resume.mts <npm-root> <out.json> <version>
//   e.g. node capture-resume.mts ~/.cache/pi-npm/0.87.1 resume-0.87.1.json 0.87.1
//
// <npm-root> holds node_modules/@earendil-works/{pi-ai,pi-coding-agent} at
// <version>.
//
// Each scenario builds a session with pi's own SessionManager: messages, one
// compaction entry (summary, firstKeptEntryId, details, fromHook, and the
// system message appendCompaction stores), then more messages. The output keeps
// the file's entries as written, so the port resumes the same bytes. pi then
// compacts that branch again, as _runAutoCompaction does: prepareCompaction
// reads the previous summary, the boundary and the file lists from the entry,
// and compact() runs with a stub streamFn that records each request's system
// prompt, user text and maxTokens and answers "SUMMARY n". The new compaction is
// appended with compact()'s result, and the context pi sends next
// (buildSessionContext, then convertToLlm) is recorded one line per message.
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";

const [root, outFile, version] = process.argv.slice(2);
if (!root || !outFile || !version) {
	console.error("usage: node capture-resume.mts <npm-root> <out.json> <version>");
	process.exit(2);
}
const dist = (pkg: string, file: string) =>
	pathToFileURL(path.join(path.resolve(root), "node_modules/@earendil-works", pkg, "dist", file)).href;
const { SessionManager } = await import(dist("pi-coding-agent", "core/session-manager.js"));
const { compact, prepareCompaction } = await import(dist("pi-coding-agent", "core/compaction/index.js"));
const { convertToLlm } = await import(dist("pi-coding-agent", "core/messages.js"));
const { getSystemMessageText } = await import(dist("pi-ai", "utils/text.js"));

const usage = { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, totalTokens: 0, cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 } };
let clock = 1_789_603_200_000;
const user = (text: string) => ({ role: "user", content: [{ type: "text", text }], timestamp: ++clock });
const assistant = (text: string, calls: Array<[string, string, Record<string, unknown>]> = []) => ({
	role: "assistant",
	content: [{ type: "text", text }, ...calls.map(([id, name, args]) => ({ type: "toolCall", id, name, arguments: args }))],
	api: "openai-completions",
	provider: "p",
	model: "m",
	usage,
	stopReason: calls.length > 0 ? "toolUse" : "stop",
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
const system = (preamble: string) => ({ role: "system", content: "", sections: { preamble }, timestamp: ++clock });

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

type Scenario = {
	name: string;
	// Messages before the compaction; the one at keptIndex is its first kept
	// entry. Without one the compaction keeps nothing (appendCompaction then
	// stores its own id as firstKeptEntryId).
	before: any[];
	keptIndex?: number;
	summary: string;
	details?: unknown;
	// Written as given: appendCompaction does not check its type, and pi reads
	// it by truthiness.
	fromHook?: unknown;
	// A context edit that omits the compaction entry. SessionManager refuses
	// to write one (a compaction is not editable), so it is appended raw, as a
	// hand-edited or foreign file would hold it; the projection still honours
	// it, and prepareCompaction then finds no previous compaction.
	omitCompaction?: boolean;
	after: any[];
};

const scenarios: Scenario[] = [
	{
		// A model's empty reply is a summary: the entry holds "", and pi reads it
		// back as a defined previousSummary.
		name: "previous-summary-empty",
		before: [user("q1"), assistant("a1"), user("q2"), assistant("a2")],
		keptIndex: 2,
		summary: "",
		details: { readFiles: [], modifiedFiles: [] },
		after: [user("q3"), assistant("a3"), user("q4")],
	},
	{
		name: "previous-summary-with-files",
		before: [
			user("q1"),
			assistant("reading", [["r1", "read", { path: "/a/old.go" }]]),
			toolResult("r1", "read", "package old"),
			assistant("a1"),
			user("q2"),
			assistant("a2"),
		],
		keptIndex: 4,
		summary: "## Goal\nold work\n\n<read-files>\n/a/old.go\n</read-files>\n\n<modified-files>\n/a/changed.go\n</modified-files>",
		details: { readFiles: ["/a/old.go"], modifiedFiles: ["/a/changed.go"] },
		after: [
			user("q3"),
			assistant("editing", [["e1", "edit", { path: "/a/new.go", oldText: "a", newText: "b" }]]),
			toolResult("e1", "edit", "ok"),
			assistant("a3"),
			user("q4"),
		],
	},
	{
		// An extension's compaction: pi ignores its details.
		name: "previous-summary-from-hook",
		before: [user("q1"), assistant("a1"), user("q2"), assistant("a2")],
		keptIndex: 2,
		summary: "## Goal\nhook work",
		details: { readFiles: ["/a/hook.go"], modifiedFiles: [] },
		fromHook: true,
		after: [user("q3"), assistant("a3"), user("q4")],
	},
	{
		name: "previous-summary-from-hook-truthy",
		before: [user("q1"), assistant("a1"), user("q2"), assistant("a2")],
		keptIndex: 2,
		summary: "## Goal\nhook work",
		details: { readFiles: ["/a/hook.go"], modifiedFiles: [] },
		fromHook: 1,
		after: [user("q3"), assistant("a3"), user("q4")],
	},
	{
		name: "previous-summary-from-hook-falsy",
		before: [user("q1"), assistant("a1"), user("q2"), assistant("a2")],
		keptIndex: 2,
		summary: "## Goal\nown work",
		details: { readFiles: ["/a/own.go"], modifiedFiles: [] },
		fromHook: "",
		after: [user("q3"), assistant("a3"), user("q4")],
	},
	{
		// The entry stores the prompt state; a system message recorded right
		// after the compaction keeps its place and becomes the next replay.
		name: "previous-summary-system-messages",
		before: [system("v1"), user("q1"), assistant("a1"), system("v2"), user("q2"), assistant("a2")],
		keptIndex: 4,
		summary: "## Goal\nprompted work",
		details: { readFiles: [], modifiedFiles: [] },
		after: [system("v3"), user("q3"), assistant("a3"), user("q4")],
	},
	{
		// Ending on an assistant splits the last turn: the history goes to the
		// update prompt, the turn's prefix to its own request.
		name: "previous-summary-split-turn",
		before: [user("q1"), assistant("a1"), user("q2"), assistant("a2")],
		keptIndex: 2,
		summary: "## Goal\nold work",
		details: { readFiles: [], modifiedFiles: [] },
		after: [user("q3"), assistant("a3"), user("q4"), assistant("a4")],
	},
	{
		// A compaction that keeps nothing, then a split turn with no history
		// before it: pi keeps the previous summary as the history part, even an
		// empty one.
		name: "previous-summary-empty-retain-none-split-turn",
		before: [user("q1"), assistant("a1")],
		summary: "",
		details: { readFiles: [], modifiedFiles: [] },
		after: [user("q2"), assistant("a2")],
	},
	{
		name: "compaction-omitted-by-context-edit",
		before: [user("q1"), assistant("a1"), user("q2"), assistant("a2")],
		keptIndex: 2,
		summary: "## Goal\nhidden work",
		details: { readFiles: ["/a/hidden.go"], modifiedFiles: [] },
		omitCompaction: true,
		after: [user("q3"), assistant("a3"), user("q4")],
	},
];

const textOf = (content: unknown): string =>
	typeof content === "string"
		? content
		: (content as Array<{ type: string; text?: string }>)
				.filter((block) => block.type === "text")
				.map((block) => block.text)
				.join("");

// One line per message the model is sent.
const describe = (message: any): string => {
	switch (message.role) {
		case "system":
			return `system:${getSystemMessageText(message)}`;
		case "assistant":
			return `assistant:${textOf(message.content)}${message.content
				.filter((block: any) => block.type === "toolCall")
				.map((block: any) => ` [${block.name} ${JSON.stringify(block.arguments)}]`)
				.join("")}`;
		default:
			return `${message.role}:${textOf(message.content)}`;
	}
};

const out = { version, reserveTokens, contextWindow: model.contextWindow, modelMaxTokens: model.maxTokens, scenarios: [] as unknown[] };
for (const scenario of scenarios) {
	const sm = SessionManager.inMemory("/tmp/capture-resume");
	let keptId: string | undefined;
	scenario.before.forEach((message, i) => {
		const id = sm.appendMessage(message);
		if (i === scenario.keptIndex) keptId = id;
	});
	const compactionId = sm.appendCompaction(scenario.summary, keptId, 100, scenario.details, scenario.fromHook ?? false);
	if (scenario.omitCompaction) {
		sm._appendEntry({
			type: "context_edit",
			id: "0mit0000",
			parentId: sm.getLeafId(),
			timestamp: new Date().toISOString(),
			targetId: compactionId,
			replacement: null,
		});
	}
	for (const message of scenario.after) sm.appendMessage(message);
	const entries = [sm.getHeader(), ...sm.getEntries()];
	const resumed = convertToLlm(sm.buildSessionContext().messages).map(describe);

	const preparation = prepareCompaction(sm.getBranch(), { enabled: true, reserveTokens, keepRecentTokens: 1 });
	const requests: Array<{ systemPrompt: string; text: string; maxTokens: number }> = [];
	const streamFn = (_model: unknown, context: { messages: Array<{ role: string; content: unknown }> }, options: { maxTokens: number }) => {
		const systemMessages = context.messages.filter((message) => message.role === "system");
		const users = context.messages.filter((message) => message.role === "user");
		if (systemMessages.length !== 1 || users.length !== 1 || context.messages.length !== 2) {
			throw new Error(`${scenario.name}: unexpected request shape ${JSON.stringify(context.messages.map((message) => message.role))}`);
		}
		requests.push({ systemPrompt: getSystemMessageText(systemMessages[0]), text: textOf(users[0].content), maxTokens: options.maxTokens });
		const response = { ...assistant(`SUMMARY ${requests.length}`), timestamp: 0 };
		return { result: async () => response };
	};
	const result = await compact(preparation, model, "test-key", undefined, undefined, undefined, undefined, streamFn);
	sm.appendCompaction(result.summary, result.firstKeptEntryId, result.tokensBefore, result.details, false);
	const compacted = convertToLlm(sm.buildSessionContext().messages).map(describe);

	out.scenarios.push({
		name: scenario.name,
		entries,
		resumed,
		previousSummary: preparation.previousSummary,
		requests,
		summary: result.summary,
		details: result.details,
		compacted,
	});
}
fs.writeFileSync(outFile, `${JSON.stringify(out, null, "\t")}\n`);
