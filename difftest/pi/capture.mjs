// pi side of the differential: build each scenario's request body with REAL pi
// and write it out canonically. No API key and no network — `onPayload` grabs
// the body and throws, which aborts the stream before the first byte is sent.
//
// Two backends, because the port is ahead of the last npm release:
//   "dist" -> the published @earendil-works/pi-ai build (strongest ground truth)
//   "src"  -> upstream TypeScript at the synced sha, run directly under Node's
//             type stripping (the ONLY ground truth for changes ported before
//             they shipped; the published build does not contain them)

import { readdir, readFile, mkdir, writeFile } from "node:fs/promises";
import { existsSync } from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { canonicalize } from "./canon.mjs";

const HERE = path.dirname(new URL(import.meta.url).pathname);
const ROOT = path.dirname(HERE);
const SCENARIOS = path.join(ROOT, "scenarios");
const OUT = path.join(ROOT, "out", "pi");

const NPM_DIR = process.env.PI_NPM_DIR;
const SRC_DIR = path.join(ROOT, "pisrc", process.env.PI_UPSTREAM_SHA ?? "");

function moduleFor(backend, api) {
	if (backend === "dist") {
		return path.join(NPM_DIR, "node_modules/@earendil-works/pi-ai/dist/api", `${api}.js`);
	}
	if (backend === "src") return path.join(SRC_DIR, "src/api", `${api}.ts`);
	throw new Error(`scenario backend must be "dist" or "src", got ${JSON.stringify(backend)}`);
}

/** Map scenario options (pi-shaped, camelCase) onto the pi options object. */
function buildOptions(scenario, capture) {
	const o = { ...(scenario.options ?? {}) };
	o.onPayload = capture;
	// Never let a real request escape even if onPayload were to be bypassed.
	o.maxRetries = 0;
	return o;
}

async function captureScenario(scenario) {
	const modPath = moduleFor(scenario.backend, scenario.api);
	if (!existsSync(modPath)) {
		throw new Error(
			`pi backend module missing: ${modPath}\n` +
				(scenario.backend === "dist"
					? `Install it: (cd ${NPM_DIR} && npm i)`
					: `Extract it: run.sh does this from $PI_UPSTREAM_DIR at $PI_UPSTREAM_SHA`),
		);
	}
	const mod = await import(pathToFileURL(modPath).href);
	const entry = scenario.entry === "streamSimple" ? mod.streamSimple : mod.stream;
	if (typeof entry !== "function") {
		throw new Error(`module ${modPath} has no ${scenario.entry ?? "stream"} export`);
	}

	let captured;
	const halt = Symbol("halt");
	const options = buildOptions(scenario, (payload) => {
		captured = payload;
		throw halt;
	});

	const final = await entry(scenario.model, scenario.context, options).result();
	if (captured === undefined) {
		throw new Error(
			`payload was never built (stream ended ${final?.stopReason}: ${final?.errorMessage ?? ""})`,
		);
	}
	return captured;
}

/**
 * Restrict a payload to the declared top-level paths ("$.contents", ...).
 *
 * This exists for ONE reason: google-generative-ai's onPayload observes a
 * different layer on each side. pi hands the @google/genai SDK a call-params
 * object ({model, contents, config}) and the SDK builds the REST body inside
 * itself — pi explicitly refuses a custom fetch, so the real REST body is not
 * observable from pi at all. The Go port has no such SDK and builds the REST
 * body directly. Only `contents` is the same artifact on both sides, so the
 * google scenarios compare that and NOTHING ELSE. Everything outside it
 * (generationConfig / tools / systemInstruction) is out of the differential's
 * reach — see README "Known limits".
 */
function project(payload, comparePaths) {
	if (!comparePaths || comparePaths.length === 0) return payload;
	const out = {};
	for (const p of comparePaths) {
		const m = /^\$\.([A-Za-z0-9_]+)$/.exec(p);
		if (!m) throw new Error(`comparePaths only supports "$.<topLevelKey>", got ${p}`);
		if (!(m[1] in payload)) throw new Error(`comparePaths ${p}: key absent from payload`);
		out[m[1]] = payload[m[1]];
	}
	return out;
}

async function main() {
	await mkdir(OUT, { recursive: true });
	const only = process.env.DIFF_ONLY;
	const files = (await readdir(SCENARIOS)).filter((f) => f.endsWith(".json")).sort();
	let failures = 0;

	for (const file of files) {
		const scenario = JSON.parse(await readFile(path.join(SCENARIOS, file), "utf8"));
		if (only && scenario.name !== only) continue;
		try {
			const payload = project(await captureScenario(scenario), scenario.comparePaths);
			// The wire bytes: exactly what pi would have POSTed.
			const wire = JSON.stringify(payload);
			const { body, order } = canonicalize(wire);
			await writeFile(path.join(OUT, `${scenario.name}.raw.json`), wire + "\n");
			await writeFile(path.join(OUT, `${scenario.name}.body.json`), body);
			await writeFile(path.join(OUT, `${scenario.name}.order.txt`), order);
			console.log(`pi  ok    ${scenario.name} (${scenario.backend})`);
		} catch (err) {
			failures++;
			console.error(`pi  ERROR ${scenario.name}: ${err?.stack ?? err}`);
		}
	}
	if (failures) process.exit(1);
}

await main();
