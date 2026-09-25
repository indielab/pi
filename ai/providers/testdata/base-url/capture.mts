// Captures what pi's adapters say for a base URL their request URL cannot be
// made from — the oracle behind TestInvalidBaseURLMatchesPi in this package.
//
//   node --experimental-strip-types capture.mts <extraction> <out.json> <sha>
//   e.g. ... capture.mts <dir> base-url-49681e1b7.json 49681e1b7
//
// <extraction> holds `git archive <sha> packages/ai package-lock.json` from the
// upstream clone, plus a node_modules resolving pi-ai's dependencies (the npm
// build's). The script refuses to write unless each SDK it runs
// (@anthropic-ai/sdk, @google/genai) resolves to the version AND integrity the
// sha's package-lock.json locks.
//
// Each adapter streams with each base URL and an onPayload that counts its
// calls; no request reaches a server (the URLs that parse name port 1, which
// refuses). pi's `new URL(...)` — the SDKs' request URL, pi-messages' own,
// built before onPayload — throws TypeError "Invalid URL" for what the WHATWG
// URL parser refuses. A row whose URL the parser takes but Go's net/url does
// not, or the reverse, is tagged `divergence` (the two parsers differ there,
// and the port's is net/url); each row records the stream's event types, its
// stopReason and errorMessage, and how often onPayload ran.
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import { pathToFileURL } from "node:url";

const [extraction, outFile, sha] = process.argv.slice(2);
if (!extraction || !outFile || !sha) {
	console.error("usage: node --experimental-strip-types capture.mts <extraction> <out.json> <sha>");
	process.exit(2);
}

const src = path.join(extraction, "packages/ai/src");
const require = createRequire(path.join(src, "api/google-generative-ai.ts"));
const lock = JSON.parse(fs.readFileSync(path.join(extraction, "package-lock.json"), "utf8"));
const sdks: string[] = [];
for (const name of ["@anthropic-ai/sdk", "@google/genai"]) {
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
const adapters = [
	{ api: "google-generative-ai", provider: "google", id: "gemini-2.5-flash", module: await load("api/google-generative-ai.ts") },
	{ api: "anthropic-messages", provider: "anthropic", id: "claude-sonnet-4-5", module: await load("api/anthropic-messages.ts") },
	{ api: "pi-messages", provider: "radius", id: "auto", module: await load("api/pi-messages.ts") },
];

// [base URL, divergence]: a divergence names how the two parsers differ.
const bases: Array<[string, string?]> = [
	["not a url"],
	["127.0.0.1:1"],
	["api.example.com/v1"],
	["//127.0.0.1:1"],
	["ht!tp://127.0.0.1:1"],
	["http://[::1"],
	["http://exa mple.com"],
	["http://127.0.0.1:99999"],
	["http://127.0.0.1:1x"],
	["http://127.0.0.1:1/%zz", "a malformed percent escape: the WHATWG parser keeps it, net/url refuses it"],
	["http://127.0.0.1:1/a%", "a lone percent sign: the WHATWG parser keeps it, net/url refuses it"],
	[`http://127.0.0.1:1/${String.fromCharCode(0x7f)}`, "a DEL in the path: the WHATWG parser percent-encodes it, net/url refuses it"],
	["http://user:pa ss@127.0.0.1:1", "a space in the userinfo: the WHATWG parser percent-encodes it (fetch then refuses the credentials), net/url refuses it"],
	["http://256.0.0.1:1", "an IPv4 part past 255: the WHATWG parser refuses it, net/url takes it as a host name"],
];

const rows = [];
for (const a of adapters) {
	for (const [baseUrl, divergence] of bases) {
		const model = {
			id: a.id,
			name: a.id,
			api: a.api,
			provider: a.provider,
			baseUrl,
			reasoning: false,
			input: ["text"],
			cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
			contextWindow: 100000,
			maxTokens: 1000,
		};
		let onPayload = 0;
		const out = a.module.stream(model, { messages: [{ role: "user", content: "hi", timestamp: 1 }] }, {
			apiKey: "test-api-key",
			onPayload: () => {
				onPayload++;
				return undefined;
			},
		});
		const events: string[] = [];
		for await (const ev of out) events.push(ev.type);
		const msg = await out.result();
		rows.push({
			api: a.api,
			baseUrl,
			...(divergence ? { divergence } : {}),
			events,
			stopReason: msg.stopReason,
			errorMessage: msg.errorMessage,
			onPayload,
		});
	}
}
fs.writeFileSync(
	outFile,
	`{"source":${JSON.stringify(`upstream ${sha} packages/ai/src/api, ${sdks.join(", ")}, node ${process.version}`)},"rows":[\n${rows.map((r) => JSON.stringify(r)).join(",\n")}\n]}\n`,
);
console.log(`captured ${rows.length} rows -> ${outFile}`);
process.exit(0);
