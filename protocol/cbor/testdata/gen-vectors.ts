// Emits CBOR encode vectors + decode accept/reject vectors from the REAL
// upstream implementation, for the Go port to assert against.
import { decodeCbor, encodeCbor } from "./cbor/index.ts";
import { encodeFrame } from "./framing.ts";

const hex = (b: Uint8Array) => Buffer.from(b).toString("hex");

// name -> value. Kept in the same shape the Go test table uses.
const encodeCases: Array<[string, unknown]> = [
	["null", null],
	["true", true],
	["false", false],
	["zero", 0],
	["one", 1],
	["twentythree", 23],
	["twentyfour", 24],
	["uint8_max", 255],
	["uint16_edge", 256],
	["uint16_max", 65535],
	["uint32_edge", 65536],
	["uint32_max", 4294967295],
	["uint64_edge", 4294967296],
	["max_safe_int", Number.MAX_SAFE_INTEGER],
	["neg_one", -1],
	["neg_twentyfour", -24],
	["neg_twentyfive", -25],
	["neg_max_safe", -Number.MAX_SAFE_INTEGER],
	["float_half", 0.5],
	["float_neg_half", -0.5],
	["float_pi", 3.141592653589793],
	["neg_zero", -0],
	["integral_float", 1.0],
	["integral_float_big", 1e15],
	["empty_string", ""],
	["ascii", "hello"],
	["unicode", "héllo ✨ 世界"],
	["emoji_surrogate_pair", "👋🏽"],
	["long_string", "x".repeat(300)],
	["empty_bytes", new Uint8Array()],
	["bytes", new Uint8Array([0, 1, 2, 250, 255])],
	["empty_array", []],
	["array_ints", [1, 2, 3]],
	["nested_array", [1, [2, [3, [4]]]]],
	["empty_map", {}],
	["map_simple", { a: 1, b: "two", c: true }],
	["map_key_order", { z: 1, a: 2, m: 3 }],
	["map_undefined_dropped", { a: 1, b: undefined, c: 3 }],
	["map_null_kept", { a: null }],
	["nested_map", { outer: { inner: { deep: [1, 2] } } }],
	["mixed", { id: "s1", n: 42, ok: true, tags: ["a", "b"], meta: { x: null } }],
	[
		"protocol_hello",
		{ type: "hello", version: 2, token: "t0ken" },
	],
	[
		"protocol_request",
		{ type: "request", id: "req-1", request: { command: "prompt", sessionId: "s1", text: "hi" } },
	],
];

const encoded = encodeCases.map(([name, value]) => {
	try {
		const bytes = encodeCbor(value);
		return { name, cbor: hex(bytes), frame: hex(encodeFrame(bytes)) };
	} catch (error) {
		return { name, error: (error as Error).message };
	}
});

// Values the encoder must REJECT.
const encodeRejects: Array<[string, unknown]> = [
	["infinity", Number.POSITIVE_INFINITY],
	["nan", Number.NaN],
	["unsafe_int", Number.MAX_SAFE_INTEGER + 2],
	["lone_surrogate", "\ud800"],
	["undefined_top", undefined],
	["function", () => 1],
	["array_hole_undefined", [1, undefined, 3]],
];
const rejects = encodeRejects.map(([name, value]) => {
	try {
		return { name, cbor: hex(encodeCbor(value)) };
	} catch (error) {
		return { name, error: (error as Error).message };
	}
});

// Raw byte payloads the decoder must accept or reject.
const decodeCases: Array<[string, string]> = [
	["indefinite_array", "9f01ff"],
	["indefinite_map", "bf6161 01ff".replace(/\s/g, "")],
	["indefinite_text", "7f6161ff"],
	["tag", "c074323031332d30332d32315432303a30343a30305a"],
	["break", "ff"],
	["half_float", "f93c00"],
	["single_float", "fa47c35000"],
	["undefined_simple", "f7"],
	["simple_16", "f0"],
	["trailing_data", "0101"],
	["truncated", "18"],
	["int_key_map", "a10101"],
	["duplicate_key", "a2616101616102"],
	["invalid_utf8", "62c328"],
	["huge_declared_length", "5affffffff"],
	["uint64_over_safe", "1b0020000000000000"],
	["float64_one", "fb3ff0000000000000"],
	["float64_half", "fb3fe0000000000000"],
	["neg_zero_float", "fb8000000000000000"],
	["canonical_one", "01"],
	["nonminimal_one", "1801"],
];
const decoded = decodeCases.map(([name, hexBytes]) => {
	try {
		const value = decodeCbor(Uint8Array.from(Buffer.from(hexBytes, "hex")));
		return { name, hex: hexBytes, ok: true, json: JSON.stringify(value ?? null) };
	} catch (error) {
		return { name, hex: hexBytes, ok: false, error: (error as Error).message };
	}
});

console.log(JSON.stringify({ encoded, rejects, decoded }, null, "\t"));
