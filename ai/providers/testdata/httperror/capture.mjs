// Captures what real pi surfaces as `errorMessage` for a non-2xx HTTP response,
// per adapter, over a table of bodies — the oracle behind the HTTP-error
// formatting tests in this package (TestResponsesHTTPErrorMatchesPi).
//
//   node capture.mjs <pi-ai npm dir> <out.json>
//   e.g. node capture.mjs ~/.cache/pi-npm/0.85.1 pi-ai-0.85.1.json
//
// Each adapter streams against a loopback server that answers with the given
// status and body, so neither side needs a key or the network. The capture is
// from the PUBLISHED build, which is what the goldens in this repo come from
// (.claude/skills/pi-parity-review/SKILL.md: when source and build disagree,
// the build wins). The "openai-responses/xai" column drives the same adapter
// as provider xai: the 0.85.1 build predates upstream 0c7bb7c5c and labels it
// "OpenAI" too, so the provider-label rule is source-only until the next
// release and TestResponsesHTTPErrorNamesProvider asserts it from source; the
// first capture from a build that ships 0c7bb7c5c makes that column the oracle.
import http from "node:http";
import fs from "node:fs";
import path from "node:path";

const [npmDir, outFile] = process.argv.slice(2);
if (!npmDir || !outFile) {
	console.error("usage: node capture.mjs <pi-ai npm dir> <out.json>");
	process.exit(2);
}
const root = path.join(npmDir, "node_modules/@earendil-works/pi-ai/dist/");
const version = JSON.parse(fs.readFileSync(path.join(npmDir, "node_modules/@earendil-works/pi-ai/package.json"), "utf8")).version;
const responses = await import(root + "api/openai-responses.js");
const adapters = {
	"openai-responses": ["openai", responses],
	"openai-responses/xai": ["xai", responses],
	"openai-completions": ["openai", await import(root + "api/openai-completions.js")],
	"anthropic-messages": ["anthropic", await import(root + "api/anthropic-messages.js")],
	"google-generative-ai": ["google", await import(root + "api/google-generative-ai.js")],
};

const J = (o) => JSON.stringify(o);
const long = "x".repeat(4500);
// [name, status, body]
const cases = [
	["obj-message", 403, J({ error: { message: "blocked" } })],
	["obj-message-429", 429, J({ error: { message: "slow down" } })],
	["obj-multi-null", 403, J({ error: { message: "slow down", type: "rate_limit_error", code: null, param: null } })],
	["obj-no-message-keyorder", 403, J({ error: { type: "zeta", code: "alpha" } })],
	["obj-nested-message", 403, J({ error: { message: { nested: 1 } } })],
	["obj-escapes", 403, J({ error: { message: "café   </script> \"q\" \\ \t  \u{1F600}", z: 1, a: 2 } })],
	["obj-long-message", 403, J({ error: { message: long } })],
	["obj-short-message-long-sibling", 403, J({ error: { message: "short", details: long } })],
	["obj-astral-long", 403, J({ error: { message: "\u{1F600}".repeat(3000) } })],
	["obj-message-contains-body", 403, J({ error: { message: "{\"a\":1}", a: 1 } })],
	["str-error", 403, J({ error: "boom" })],
	["str-error-400", 400, J({ error: "boom" })],
	["str-error-long", 403, J({ error: long })],
	["empty-obj-error", 403, J({ error: {} })],
	["null-error", 403, J({ error: null })],
	["num-error", 403, J({ error: 0 })],
	["no-error-key", 403, J({ message: "no error key" })],
	["array", 403, J([1, 2])],
	["text", 403, "oops"],
	["text-500", 500, "oops"],
	["empty", 403, ""],
	["empty-503", 503, ""],
	["whitespace", 503, "   "],
	["padded", 403, "  " + J({ error: { message: "padded" } }) + "  "],
	["anthropic-shape", 403, J({ type: "error", error: { type: "authentication_error", message: "invalid x-api-key" } })],
	["google-shape", 403, J({ error: { code: 403, message: "PERMISSION_DENIED: no", status: "PERMISSION_DENIED" } })],
	// Valid JSON whose parsed value is falsy: the openai client passes the raw
	// text as the message (`errJSON ? undefined : errText`).
	["scalar-null", 403, "null"],
	["scalar-zero", 403, "0"],
	["scalar-false", 403, "false"],
	["scalar-emptystr", 403, '""'],
	["scalar-padded-zero", 403, "  0  "],
	// Numbers JSON.parse turns into Infinity (truthy; JSON.stringify -> null).
	["num-error-huge", 403, '{"error":1e400}'],
	["obj-num-huge", 403, '{"error":{"message":"x","n":1e400}}'],
	// A UTF-8 BOM: fetch's text() strips it before the client parses.
	["bom-obj", 403, "\uFEFF" + J({ error: { message: "bom" } })],
	["bom-text", 403, "\uFEFFoops"],
];

async function run(api, provider, mod, status, body) {
	const srv = http.createServer((req, res) => {
		res.writeHead(status, { "content-type": /^\s*[\[{]/.test(body) ? "application/json" : "text/plain" });
		res.end(body);
	});
	await new Promise((r) => srv.listen(0, "127.0.0.1", r));
	const port = srv.address().port;
	const model = { id: "m", name: "m", api, provider, baseUrl: `http://127.0.0.1:${port}`, reasoning: false, input: ["text"], cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 }, contextWindow: 100000, maxTokens: 1000 };
	let msg;
	try {
		const s = mod.stream(model, { messages: [{ role: "user", content: "hi", timestamp: 1 }] }, { apiKey: "sk", maxRetries: 0 });
		msg = (await s.result()).errorMessage;
	} catch (e) {
		msg = "THREW: " + e.message;
	}
	srv.close();
	return msg;
}

const rows = [];
for (const [name, status, body] of cases) {
	const row = { name, status, body, errorMessage: {} };
	for (const [api, [provider, mod]] of Object.entries(adapters)) {
		row.errorMessage[api] = await run(api, provider, mod, status, body);
	}
	rows.push(row);
}
fs.writeFileSync(outFile, JSON.stringify({ "pi-ai": version, rows }, null, 1) + "\n");
console.log(`captured ${rows.length} rows from @earendil-works/pi-ai ${version} -> ${outFile}`);
