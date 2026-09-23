// Captures pi's defaultModelPerProvider table — the oracle behind
// TestDefaultModelPerProviderMatchesPi in coding/resolve_test.go.
//
//   node capture.mjs <pi npm dir> <out.json>
//   e.g. node capture.mjs ~/.cache/pi-npm/0.87.1 default-models-0.87.1.json
//
// The table is read from the PUBLISHED build (dist/core/model-resolver.js).
// Re-capture it at every re-pin: upstream re-points entries in commits whose
// model-resolver.ts hunk sits among host-only changes, and a missed one only
// shows as a custom model id cloned from the wrong template.
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";

const [npmDir, outFile] = process.argv.slice(2);
if (!npmDir || !outFile) {
	console.error("usage: node capture.mjs <pi npm dir> <out.json>");
	process.exit(2);
}
const pkg = path.join(npmDir, "node_modules/@earendil-works/pi-coding-agent");
const version = JSON.parse(fs.readFileSync(path.join(pkg, "package.json"), "utf8")).version;
const { defaultModelPerProvider } = await import(pathToFileURL(path.join(pkg, "dist/core/model-resolver.js")).href);
fs.writeFileSync(outFile, `${JSON.stringify({ "pi-coding-agent": version, defaultModelPerProvider }, null, "\t")}\n`);
console.log(`captured ${Object.keys(defaultModelPerProvider).length} defaults from @earendil-works/pi-coding-agent ${version} -> ${outFile}`);
