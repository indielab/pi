// Captures the record pi's headersToRecord builds from a real fetch Response —
// the headers every adapter hands onResponse — the oracle behind
// TestFlattenHeadersMatchesPi.
//
//   node --experimental-strip-types capture-response-headers.mts <extraction> <out.json> <sha>
//   e.g. ... capture-response-headers.mts <dir> response-headers-8676a0dcd.json 8676a0dcd
//
// <extraction> holds packages/ai at <sha> (`git archive <sha> packages/ai |
// tar -x -C <dir>` from the upstream clone); only utils/headers.ts is loaded,
// and it imports nothing at runtime. The Response comes from node's own fetch
// (undici) reading a loopback server that writes each row's exact bytes, so the
// Headers object is the one a provider response carries. Each row records the
// raw response and the record: Headers.entries() yields lowercased names in
// sorted order, joins a repeated name's values with ", " — except set-cookie,
// whose values it yields one by one, so the record keeps the last.
//
// The rows avoid what Go's transport rewrites before any adapter sees the
// header map (Transfer-Encoding moves to Response.TransferEncoding; trailing
// whitespace in a value is trimmed): those are transport differences, not
// headersToRecord's.
import fs from "node:fs";
import net from "node:net";
import path from "node:path";
import { pathToFileURL } from "node:url";

const [extraction, outFile, sha] = process.argv.slice(2);
if (!extraction || !outFile || !sha) {
	console.error("usage: node --experimental-strip-types capture-response-headers.mts <extraction> <out.json> <sha>");
	process.exit(2);
}
const { headersToRecord } = await import(
	pathToFileURL(path.join(extraction, "packages/ai/src/utils/headers.ts")).href
);

const response = (headerLines: string[]) =>
	["HTTP/1.1 200 OK", ...headerLines, "Content-Length: 2", "", "ok"].join("\r\n");

const cases: Array<[string, string[]]> = [
	["repeatedNameJoinsInWireOrder", ["Content-Type: text/event-stream", "X-Multi: a", "x-multi: b", "X-MULTI: c"]],
	["setCookieKeepsTheLast", ["Set-Cookie: c=1", "set-cookie: d=2", "Set-Cookie: e=3"]],
	["emptyValuesStillJoin", ["X-Empty:", "X-Empty2: ", "X-Empty2: v", "X-Mixed: v", "X-Mixed:"]],
	["namesAreLowercased", ["X-Request-ID: req_1", "Retry-After: 5", "anthropic-ratelimit-requests-remaining: 9"]],
];

async function capture(raw: string): Promise<Record<string, string>> {
	const server = net.createServer((socket) => {
		socket.once("data", () => socket.end(raw));
	});
	await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
	try {
		const { port } = server.address() as net.AddressInfo;
		const res = await fetch(`http://127.0.0.1:${port}/`);
		await res.text();
		return headersToRecord(res.headers);
	} finally {
		server.close();
	}
}

const rows = [];
for (const [name, lines] of cases) {
	const raw = response(lines);
	rows.push({ name, response: raw, record: await capture(raw) });
}
// One row per line, so a re-capture diffs row by row.
const text = rows.map((row) => JSON.stringify(row)).join(",\n");
fs.writeFileSync(
	outFile,
	`{"sha":${JSON.stringify(sha)},"node":${JSON.stringify(process.version)},"undici":${JSON.stringify(process.versions.undici)},"rows":[\n${text}\n]}\n`,
);
console.log(`captured ${rows.length} rows from packages/ai/src/utils/headers.ts at ${sha} -> ${outFile}`);
