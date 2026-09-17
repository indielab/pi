// Captures real pi's session context reconstruction — the oracle behind
// coding/session_tree_parity_test.go. For every scenario given, it runs
// buildSessionContext followed by convertToLlm over the scenario's entries and
// writes <scenario>.golden.json beside it.
//
//   node --experimental-strip-types capture.mts <extraction> <scenario.json>...
//   e.g. ... capture.mts <dir> *_*.json (skip the .golden.json files)
//
// <extraction> holds packages/ai, packages/agent and packages/coding-agent at
// the capture sha (`git archive <sha> packages/ai packages/agent
// packages/coding-agent` from the upstream clone), a node_modules resolving
// their dependencies (the npm build's), and
// packages/coding-agent/node_modules/@earendil-works/{pi-ai,pi-agent-core}
// resolving to packages/{ai,agent}/src.
//
// Scenarios 1-8 were captured from the npm build of pi-coding-agent; running
// this script over them at 9e05370b2 reproduces their goldens byte for byte.
// Scenarios 9-13 (system messages, compaction systemMessage) are src captures
// at 9e05370b2, which the npm build 0.85.1 predates: re-verify them against the
// first build that ships it (the BUILD wins).
//
// Projection: role and the text of the content (a string as is; blocks joined
// with no separator, an image as <image>, any other block as <other>). A system
// message also carries the whole message as pi returns it, so its key order,
// sections and tool fields are pinned too.
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";

const [extraction, ...scenarios] = process.argv.slice(2);
if (!extraction || scenarios.length === 0) {
	console.error("usage: node --experimental-strip-types capture.mts <extraction> <scenario.json>...");
	process.exit(2);
}
const core = (file: string) =>
	import(pathToFileURL(path.join(extraction, "packages/coding-agent/src/core", file)).href);
const { buildSessionContext } = await core("session-manager.ts");
const { convertToLlm } = await core("messages.ts");

function text(content: unknown): string {
	if (typeof content === "string") return content;
	if (!Array.isArray(content)) return "";
	return content
		.map((block) => (block.type === "text" ? block.text : block.type === "image" ? "<image>" : "<other>"))
		.join("");
}

for (const file of scenarios) {
	const scenario = JSON.parse(fs.readFileSync(file, "utf8"));
	const context = buildSessionContext(scenario.entries, scenario.leafId);
	const out = {
		thinkingLevel: context.thinkingLevel,
		model: context.model,
		messages: convertToLlm(context.messages).map((message: any) =>
			message.role === "system"
				? { role: message.role, text: text(message.content), message }
				: { role: message.role, text: text(message.content) },
		),
	};
	fs.writeFileSync(file.replace(/\.json$/, ".golden.json"), JSON.stringify(out, null, 2));
}
