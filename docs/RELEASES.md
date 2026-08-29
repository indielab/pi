# Release tracker

Release-level view of the pi Go port: each tag mapped to the upstream pi pin it
syncs to and the `@earendil-works/pi-ai` npm catalog the byte-goldens were
captured from. The commit-by-commit triage/port ledger lives in
[`UPSTREAM.md`](UPSTREAM.md); this file is the per-release summary.

- Tags are **annotated, unsigned** (`git tag -a`). Tagger identity:
  `Noam Y. Tenne <noam@10ne.org>`.
- A release tag points at the cycle's ledger/pin-advance commit (the tip of the
  sync), so the catalog + ledger are included.
- Versioning is git-tag-only — there is no `VERSION` file or in-source version
  constant. **As of `v0.80.11` the port's `major.minor` follows the upstream
  `pi-ai` catalog `major.minor`; `patch` is the port's own monotonic counter**
  (it never resets, so it stays distinct from pi's patch — e.g. `v0.80.11` syncs
  pi 0.80.7). Earlier tags (`v0.1.0`–`v0.2.10`) used an independent `v0.2.x`
  line; they are left as-is.

## Releases

| Version | Date | Commit | Upstream pin | npm catalog | Headline |
|---|---|---|---|---|---|
| [`v0.84.20`](#v08420) | 2026-08-29 | `886ece5` | `853a80d26` | pi-ai 0.84.4 | Release v0.84.4 crossed — catalog regen 558,804→563,988 B, 1312→1290 models across an unchanged 39 providers (+57/−79, 227 changed) with no schema drift at any level, draining the whole three-item generator queue; the agent loop now prepares each turn immediately before its request rather than right after the previous one, so `ShouldStopAfterTurn` runs first on the completed turn, preparation no longer fires after a final turn, and steering typed during a long preparation reaches the turn it was typed for; two exported hooks gain the doc comments their changed contract needs; differential harness re-pinned and green at 49 PASS / 0 KNOWN / 0 FAIL on the new 0.84.4 dist |
| [`v0.84.19`](#v08419) | 2026-08-25 | `2f4e336` | `a79b37334` | pi-ai 0.84.3 | Release v0.84.3 crossed — catalog regen 536,642→558,804 B, 1267→1312 models, draining the whole eight-item generator queue and activating `allowedFallbackModels` (0→2) for the first time; PowerShell joins the built-in tools (opt-in, not default-active) on a shared shell-tool factory, moving the bash `command` schema description to "Shell command to execute"; OpenRouter `reasoning_details` deltas now concatenate into logical entries; `tool_choice` is omitted when a request carries no tools; the image MIME detector joins the public API; differential harness fully dist-backed again at 49 PASS / 0 KNOWN / 0 FAIL |
| [`v0.84.18`](#v08418) | 2026-08-15 | `3c909e6` | `086c32e74` | pi-ai 0.84.2 | Release v0.84.2 crossed — the catalog regen (536,642 B, 1220→1267 models) drains the entire four-item generator queue: DeepSeek `max_tokens` field, `supportsStrictMode` on 34 cloudflare-ai-gateway models, deepseek-v4-flash low thinking level, and the KimiCLI/1.5 static headers gone; Google MAX_TOKENS stops with tool calls keep `length` instead of being clobbered to `toolUse`; Z.AI Coding Plan defaults glm-5.1→glm-5.3 plus a new every-default-resolves-in-catalog guard test; differential harness fully dist-backed for the first time — all 21 scenarios against the published 0.84.2 build, 21/21 PASS with an empty known-divergence baseline |
| [`v0.84.17`](#v08417) | 2026-08-07 | `7a95c4b` | `e0900a6ea` | pi-ai 0.84.1 | Two releases crossed (v0.84.0, v0.84.1) — first catalog move since 0.83.0: 511,913 B, 1153→1220 models, 37→39 providers (+baseten data, +qwen-token-plan-individual), activating the already-ported `chat_template_args` / `stream_options.include_usage` paths; **breaking wire change** — session summaries replaced by durable `SessionMetadata`, so listing is no longer per-connection and one snapshot is broadcast to all peers; `Agent.Reset()` now refuses mid-run (API break); blocked tool calls can join the batch early-termination rule; telemetry gains an in-memory reference recorder; PI_* system-prompt guideline softened |
| [`v0.83.16`](#v08316) | 2026-07-30 | `13088d2` | `c13ffe18` | pi-ai 0.83.0 | First catalog move since 0.82.0 — 1116→1153 models (477,229 B), regenerated from the 0.83.0 build and endpoint-pinned both ends; Qwen token-plan `reasoning_effort` via `thinkingLevelMap` under `??` semantics (a live request-body change for 27 models); `rawStopReason` preserved across the four ported providers with pi's `Provider stopped with: …` strings; tool calls carrying both `custom` and `function` treated as function calls; three missing `defaultModelPerProvider` entries restored (qwen-token-plan fallbacks were cloning the wrong context/token limits) |
| [`v0.82.15`](#v08215) | 2026-07-26 | `b3785a1` | `5bc1c2c0` | pi-ai 0.82.1 | Release v0.82.1 crossed but catalog byte-identical to 0.82.0 (no regen); `ANTHROPIC_AUTH_TOKEN` bearer auth via a custom anthropic resolver (credential-first precedence, `Authorization: Bearer`, never `x-api-key`); `ModelsStoreEntry.etag` for remote-catalog ETag revalidation (latent); ModelsError keeps its underlying cause (already faithful in Go, locked) |
| [`v0.82.14`](#v08214) | 2026-07-24 | `b017378` | `7df73a00` | pi-ai 0.82.0 | Catalog 0.81.1→0.82.0 (456,576 B, +19/−1 models); constrained sampling — JSON-schema `strict` and Lark/regex grammar tools across anthropic/openai-completions/openai-responses/google, incl. `custom` tool calls and streaming grammar deltas; abortable provider retries with fail-fast on oversized `Retry-After`; explicit prompt-cache mode for GPT-5.6+ |
| [`v0.81.13`](#v08113) | 2026-07-21 | `b224be0` | `dd6bea41` | pi-ai 0.81.1 | Catalog 0.80.10→0.81.1 (2 upstream tags, 431,732 B) + Qwen Token Plan built-ins; tool_call_id item-level uniqueness ({call_id}_{item_id}); RetryAssistantCall bounded-backoff retry loop; compaction retainedTail reconstruction; credential env-section passthrough on env-key + ambient resolvers |
| [`v0.80.12`](#v08012) | 2026-07-17 | `7cc7772` | `a9f6a315` | pi-ai 0.80.10 | Catalog 0.80.7→0.80.10 (3 upstream tags); model-runtime facade ported SDK-scoped (ModelsStore, refresh/checkAuth/getAvailable/login/logout, header transforms, force flag); Kimi K3 deferred tools live on openai-completions; xai default grok-4.5 + encrypted-reasoning include; Responses early-EOF retry |
| [`v0.80.11`](#v08011) | 2026-07-15 | `cff5172` | `dcfe36c7` | pi-ai 0.80.7 | Catalog 0.80.6→0.80.7; system-prompt `Current date` line removed; rolls up the 07-11→07-15 cycles — deferred/message-anchored tool loading, Responses `tool_choice` + OpenRouter session-affinity formats, openai-responses `encrypted_content` backfill, new `pi-messages` provider API |
| [`v0.2.9`](#v029) | 2026-07-10 | `e77a1cc` | `81de5702` | pi-ai 0.80.6 | Catalog 0.80.3→0.80.6; input-based pricing tiers + `max` thinking level; ResourceExhausted / CF-524 / Bun-socket retry patterns; stale-usage-after-compaction guard; fail tool calls from length-truncated messages; `(no tool output)` placeholder; Vercel AI Gateway attribution removed |
| [`v0.2.8`](#v028) | 2026-07-01 | `261985e` | `8c943640` | pi-ai 0.80.3 | Catalog 0.80.2→0.80.3 (claude-sonnet-5 across providers, Fireworks GLM-5.2 Fast realign, claude-3.5-haiku pruned); bash tool timeout validation |
| [`v0.2.7`](#v027) | 2026-06-24 | `b75f5da` | `a2e3e9d8` | pi-ai 0.80.2 | Catalog 0.79.10→0.80.2; models-runtime migration complete (auth substrate + Models runtime + request-scoped auth + api_key/env credential); OpenAI Responses terminal events; anthropic compat→catalog; header-only client auth |
| [`v0.2.6`](#v026) | 2026-06-22 | `d91f8b7` | `2417adb4` | pi-ai 0.79.9 | Catalog 0.79.9; chat-template thinking compat (latent); fuzzy-edit untouched-line preservation; legacy WSL bash stdin; session-branch linearization |
| [`v0.2.5`](#v025) | 2026-06-19 | `d5f2c73` | `56b22768` | pi-ai 0.79.8 | Catalog 0.79.8 (GLM-5.2 opencode-go, openrouter/fusion, Mistral prompt-caching data); no behavior change vs v0.2.4 |
| [`v0.2.4`](#v024) | 2026-06-17 | `a9b7e5c` | `29c1504c` | pi-ai 0.79.6 | GLM-5.2 reasoning_effort; null Responses content; provider-scoped env overrides; deepseek gate live |
| [`v0.2.3`](#v023) | 2026-06-16 | `c655c5a` | `f8a77f47` | pi-ai 0.79.4 | Docs-only: disclose default provider-attribution headers (no code change vs v0.2.2) |
| [`v0.2.2`](#v022) | 2026-06-16 | `39b3879` | `f8a77f47` | pi-ai 0.79.4 | 1h cache-write 2×; bash stdout drain; deepseek/gemini thinking gates; provider-attribution headers |
| [`v0.2.1`](#v021) | 2026-06-13 | `ca0d684` | `6f29450e` | pi-ai 0.79.3 | Catalog 0.79.3; Anthropic refusal details; fallback thinking flip; late-tool-update guard; Fable-5 gate live |
| [`v0.2.0`](#v020) | 2026-06-12 | `a2f0471` | `3f44d3e2` | pi-ai 0.79.1 | First synced catalog (Fable 5, zai payload, anthropic off:null gate, PI_EXPERIMENTAL); UPSTREAM.md pipeline |
| [`v0.1.1`](#v011) | 2026-06-11 | `b09cb46` | — | — | Built-in providers register on import (init()) |
| [`v0.1.0`](#v010) | 2026-06-10 | `1210b0a` | — | — | Initial tagged baseline |

## Notes

### v0.84.20
Upstream sync `56f3f33a9 → 853a80d26` (pi 0.84.4). 7 first-parent changes, no
merges: 2 ports, 2 port-but-queued, 3 n/a, 0 decide. **Release v0.84.4
crossed**, so the catalog was regenerated from the integrity-verified build —
558,804→563,988 B, 1312→1290 models across an unchanged 39 providers (+57/−79,
227 changed) — with the decision made by executing `JSON.stringify(MODELS)`
against both builds rather than by reading git, and endpoint-pinned at both ends
so the ported diff is exactly the upstream release diff. Both review gates
re-derived it independently. The regen drains the entire three-item generator
queue that had accumulated since 0.84.3, and the queue does not reopen — no
generator delta landed this cycle. There is **no schema drift at any level**,
and `compat.allowedFallbackModels` keeps its two anthropic entries and their
chains, so the refusal-fallback path is unaffected.

Behaviour: **the agent loop prepares each turn immediately before its request**
(upstream #8782) instead of right after the previous turn ended. Three
consequences reach the public API. `ShouldStopAfterTurn` now runs first, on the
context of the turn that just completed. `PrepareNextTurn` runs only when
another turn will actually happen, so it no longer fires after a final or
terminating turn — if you were doing end-of-run work there, move it to
`agent_end` handling. And because preparation can be long-running, steering
queued while it ran is picked up before the turn starts, so a message typed
during compaction reaches the turn it was typed for rather than the one after.
Both hooks gained doc comments recording the new ordering; upstream filed the
same change under Breaking Changes.

The coding-agent half of that upstream fix needed no port: the port drives auto
compaction through a per-request `TransformContext` hook, which already fired
before every provider request including post-tool ones — the property upstream
was adding. One residual is recorded as a deliberate divergence in
`UPSTREAM.md`: because the port compacts inside the request rather than in
`prepareNextTurn`, steering typed *during* compaction lands one turn later than
in pi, costing one extra provider round-trip. It is a tripwire on the
compaction-placement ruling, not a defect.

Two upstream changes are in scope but have no Go base yet and were queued rather
than ported: a Mistral tool-call chunk-merging fix (Scope queue entry 6) and an
image-model catalog refresh (entry 3).

### v0.84.19
Upstream sync `4af9d21d3 → a79b37334` (pi 0.84.3). 22 first-parent changes,
no merges: 6 ports, 3 port-but-catalog-only, 13 n/a, 0 decide. **Release
v0.84.3 crossed**, so the catalog was regenerated from the integrity-verified
build — 536,642→558,804 B, 1267→1312 models across an unchanged 39 providers
(+81/−36, 88 changed) — with the decision made by executing
`JSON.stringify(MODELS)` against both builds rather than by reading git, and
endpoint-pinned at both ends so the ported diff is exactly the upstream
release diff. The regen drains the entire eight-item generator queue that had
accumulated since 0.84.2, and the queue reopens at three for the next one.

The regen also **activates a feature that had shipped dormant**: the catalog
now carries `compat.allowedFallbackModels` for two anthropic models (0→2),
so the server-side refusal-fallback path ported back in `a20597b` — until now
exercised only by tests that built their own compat — is live against real
catalog data for the first time.

Behaviour: **PowerShell becomes a built-in tool** (upstream #8512), built on
the same shared shell-tool factory bash now uses. It is opt-in: `ToolNames`
gains it, the default active set does not, so the default system prompt is
byte-unchanged. What does move on the wire is bash's own `command` parameter
description, now "Shell command to execute". OpenRouter streams
`reasoning_details` as deltas, and consecutive text/summary deltas now
concatenate into one logical entry instead of piling up as separate ones — a
session-format and request-body change that turns on JS's `??=` versus `||=`
distinction, where an index of 0 survives but an empty format string does not.
`tool_choice` is no longer sent on requests that carry no tools. A compaction
summary that stopped on `length` is now rejected rather than installed as a
checkpoint, so a truncated summary can no longer become permanent. And
`DetectSupportedImageMimeTypeFromFile` joins the public Go API, matching what
pi publishes — and only that, the buffer variant deliberately staying internal.

Milestone: the differential harness is **fully dist-backed again**. All 27
`src` scenarios flipped to `dist` once 0.84.3 shipped the surface they cover,
and two new `src` scenarios were added for this cycle's own `tool_choice`
change — 49 PASS / 0 KNOWN / 0 FAIL, with the new pair proven load-bearing by
watching it fail against the pre-fix published build.

### v0.84.18
Upstream sync `f3c406a9b → 086c32e74` (pi 0.84.2). 14 first-parent changes:
4 ports (one a verified no-op), 10 n/a, 0 decide. **Release v0.84.2 crossed**,
so the catalog was regenerated from the integrity-verified build — 536,642 B,
1220→1267 models (+71/−24) — and independently re-derived byte-identical by
the parity reviewer. The regen drains the whole four-item generator queue that
had accumulated since 0.84.1: DeepSeek now gets `max_tokens` from catalog data,
34 cloudflare-ai-gateway models carry `supportsStrictMode`, deepseek-v4-flash
maps the `low` thinking level, and the static `KimiCLI/1.5` headers are gone
(the runtime UA override ported last cycle now merely re-asserts the catalog).

Behaviour: upstream `5093641a5` — a Gemini turn that hits MAX_TOKENS while
carrying a tool call now keeps its `length` stop instead of being reported as
`toolUse` (the override applies only to plain STOP finishes). And `e429d90b8`
re-points the Z.AI Coding Plan defaults to glm-5.3 — glm-5.1 had left the zai
catalogs at 0.84.1, so the defaults dangled for a whole release; the newly
ported guard test (every catalog provider's default must resolve) makes that
class of orphan impossible to reintroduce silently.

Milestone: with 0.84.2 shipping every wire-relevant behaviour the port
carries, all 15 remaining `backend: "src"` harness scenarios flipped to
`"dist"` — the entire 21-scenario suite now compares against the published
build, the strongest ground truth, with an empty known-divergence baseline:
21/21 PASS.

Gates: parity **4/4 FAITHFUL**, go-review **SHIP** (4 LOW applied). Full
`-race` suite green.

### v0.84.17
Upstream sync `9859eaa26 → e0900a6ea` (pi 0.84.1). 51 first-parent changes:
9 ports, 41 n/a, 1 decide (resolved). **Two release tags crossed**, so the
catalog was regenerated for the first time since 0.83.0 — 511,913 B, derived by
executing the integrity-verified build and endpoint-pinned at both ends. Its
only schema drift is two compat keys that Go already consumed, so the regen
mostly *activated* the Baseten support ported in an earlier cycle (17 models,
`chat_template_args` + `stream_options.include_usage` now live).

The headline behaviour change is upstream `6189e53b3`: `SessionSummary` is
replaced by `SessionMetadata`, dropping every live field from server snapshots
and list results. Because `attached` was per-connection, the server now builds
one snapshot and broadcasts it to every peer, and the client can no longer
rebuild attachments from a snapshot. The wire byte-goldens moved and were
re-derived by executing upstream's own codec at the new sha — proved
non-circular against the previous pin, with exactly three vectors changing.

Also: `Agent.Reset()` and `coding.Session.Reset()` return an error and refuse
while a run is active (sanctioned API break); `beforeToolCall` can return
`terminate` alongside `block`; a new `telemetry.InMemoryContext` reference
recorder; the Qwen Token Plan Individual provider; and the PI_* environment
guideline softened, moving the system-prompt golden.

Gates: parity **FAITHFUL**, go-review **SHIP WITH NITS**, all findings applied.
Differential harness 13/13 against the 0.84.1 build, now including a permanent
catalog-backed baseten scenario. `go test -race ./...` green.

**Scope note:** the owner reopened the agent-harness boundary this cycle
(ruling (c)). `packages/agent/src/harness/**` and `packages/session-backends/**`
are in scope as of this release but are **not ported** — ~12.4k lines tracked
as an explicit deferral in UPSTREAM.md. This tag does not imply harness parity.

### v0.83.16

- **Tagged commit**: `13088d2`
- **Upstream pin**: `c13ffe18` (delta `cced6a21..c13ffe18`, 17 first-parent changes — 3 ports, 13 n/a, 0 decides)
- **npm catalog**: `@earendil-works/pi-ai` 0.83.0 / `pi-coding-agent` 0.83.0
- **Release crossed**: v0.83.0 (`845d6ff1`). Unlike the last four cycles the
  catalog **did** move: 456,576 → 477,229 B, 1116 → 1153 models (+64/−27/91
  changed), no schema drift.

Headline is the **Qwen token-plan thinking controls** (upstream `4c1a0b92`),
which is really two halves that only work together. The code half emits
`reasoning_effort` on the qwen branch when the model supports it, mapped through
`model.thinkingLevelMap`; the data half — the generator rewrite — ships only in
the 0.83.0 build. The subtle part is that pi uses **`??`** here, not the zai
branch's `=== undefined`: a *present-null* map entry is nullish, so it falls back
to the raw level and is still emitted. Go routes this through `effortValue`
rather than `mappedEffortOrRaw` for exactly that reason.

Shipping the code without the data would have sent `reasoning_effort` to
precisely the nine model ids the upstream fix was written to exclude — a state
no published pi ever had. The parity review demonstrated this by crossing 0.83.0
code against 0.82.0 data, which is why the regen landed in the same cycle.

This cycle also produced a **procedural correction worth keeping**: the model
data lives in `dist/providers/data/*.json`, generated at publish time and never
committed upstream, and `dist/models.generated.js` is a 4,373-byte aggregator
that is byte-identical between 0.82.0 and 0.83.0. A git-diff sweep and a
file-level `cmp` both said "no catalog change"; both were wrong. Whether a regen
is needed must be decided by **executing** `JSON.stringify(MODELS)` against both
builds.

Also in: `AssistantMessage.rawStopReason` preserved by anthropic,
openai-completions, openai-responses and google, with pi's byte-exact
`Provider stopped with: …` messages (upstream `d7b02636`); tool-call deltas
carrying both `custom` and `function` treated as ordinary function calls so
streamed arguments are no longer discarded (upstream `34239180`); and a
pre-existing fix found by the parity re-gate — `coding/resolve.go` was missing
three of pi's 38 `defaultModelPerProvider` entries, so a custom model id under
`qwen-token-plan(-cn)` fell through to MiniMax-M2.5 and cloned its
196608/32768 limits instead of qwen3.7-max's 1000000/131072, changing the
emitted `max_tokens` and the context clamp.

Verification: live pi-vs-Go request diff **8310/8310** (554 openai-completions
models × 15 scenarios), catalog endpoint-pinned at both ends, in-repo
differential 38/38, `-race` green.

### v0.82.15

- **Tagged commit**: `b3785a1`
- **Upstream pin**: `5bc1c2c0` (delta `7df73a00..5bc1c2c0`, 19 first-parent changes — 3 ports, 15 n/a, 0 decides)
- **npm catalog**: `@earendil-works/pi-ai` 0.82.1 / `pi-coding-agent` 0.82.1
- **Release crossed**: v0.82.1 (`b4f29368`), one tag — but `models.generated.ts`
  is unchanged across the range, so the catalog is **byte-identical to 0.82.0**
  (456,576 B) and no re-derivation ran; every byte-golden is untouched.

Headline is **`ANTHROPIC_AUTH_TOKEN` support** (upstream `24e5cc04`, #6148): the
token participates in env discovery/status but authenticates as
`Authorization: Bearer` — never `x-api-key`. Upstream swaps anthropic from the
generic env-key auth to a custom `anthropicApiKeyAuth` whose precedence is
**stored/explicit credential → `ANTHROPIC_AUTH_TOKEN` (header) →
`ANTHROPIC_OAUTH_TOKEN`/`ANTHROPIC_API_KEY` (api key)**. The Go port mirrors that
with a custom resolver on the facade (`ModelAuth.Headers`) plus compat-path
handling (`withEnvAPIKey` leaves the api key empty when the token is active; the
provider reads it only when no key resolved), so credential-first precedence
holds on both paths — an explicit request key beats the env token, and the env
token beats a plain `ANTHROPIC_API_KEY`.

The initial port inverted that precedence (the env token overrode an explicit
key, and the facade `GetAuth` surfaced the token as an api key); both independent
reviews caught it and it was fixed before shipping.

Also this cycle: **`ModelsStoreEntry.etag`** (`b1c444d9`) — a latent field
carrying the remote catalog's ETag validator for host-side revalidation; and a
regression lock that **`ModelsError` keeps its underlying cause** (`4cf0a729`),
which Go's `Error()` already did.

### v0.82.14

- **Tagged commit**: `b017378`
- **Upstream pin**: `7df73a00` (delta `34f3719a..7df73a00`, 21 first-parent changes — 4 ports, 17 n/a, 0 decides)
- **npm catalog**: `@earendil-works/pi-ai` 0.82.0 / `pi-coding-agent` 0.82.0, both integrity-verified against the registry
- **Release crossed**: v0.82.0 (`083e6162`), one tag

Headline is **constrained sampling** (upstream `24bace27`): tools can now ask the
provider to constrain sampling to their JSON schema (`strict`, prefer-or-require)
or to a Lark/regex grammar, with `type:"custom"` tool calls and a streaming
grammar-delta path on openai-completions and openai-responses, strict tool
schemas on anthropic, and `VALIDATED` function-calling mode on Gemini 3+.

Two silent request-body changes ride along and are worth knowing about:
openai-responses `supportsStrictMode` now defaults **false**, so the `strict` key
disappears for the 53 of 99 catalog responses models that lack the flag
(github-copilot, cloudflare-ai-gateway, opencode, azure) while the 46 under the
`openai` provider are unchanged; and function-call replay now drops item ids that
do not start with `fc_`.

Also: provider retries are abortable and now **fail fast** when a server requests
a delay above `maxRetryDelayMs` rather than silently ignoring it, and
`prompt_cache_options:{mode:"explicit"}` is emitted for GPT-5.6+ when cache
retention is none.

The adversarial parity review caught a **missed hunk** that would otherwise have
shipped as a feature that silently does nothing for this port's own consumers:
`24bace27` threads `constrainedSampling` through `tool-definition-wrapper.ts`,
which reaches `AgentTool` for free in TypeScript via interface inheritance but had
no Go home, leaving constrained sampling implemented in `ai` and unreachable from
`agent/` and `coding/`. Fixed before the tag.



### v0.81.13
Upstream sync `8b937370 → dd6bea41` (pi 0.81.1; two release tags crossed —
v0.81.0, v0.81.1). 42 first-parent changes → 6 ports (8 Go commits), 33 n/a,
0 decides. Catalog regenerated byte-identical from the 0.81.1 build (420,411 →
**431,732 B**, `cmp`-clean) and the **Qwen Token Plan** built-in providers
(`qwen-token-plan`, `qwen-token-plan-cn`) wired via env keys — models + baseURL +
api ride in from the catalog. Behavior ports: `tool_call_id` now preserves
item-level uniqueness (`{call_id}_{item_id}`, hash fallback >40) so tool calls
sharing a call_id when switching models stay distinct; a policy-driven
`RetryAssistantCall` retry loop (bounded exponential backoff, fail-fast on
quota, abort-normalization) landed as latent SDK surface; compaction
`retainedTail` reconstruction so pi-0.81.x sessions rebuild identically; and the
"env section ignored" credential fix on **both** the env-key and the ambient
(bedrock/vertex) auth resolvers — the ambient half was caught by the adversarial
parity review. Ruled n/a: usage-on-entries (`2fd38684`, no ported consumer) and
the stream-fn/compat decouple (`1235c0ec`/`b9e5c5d9`, Go defaults `ai.StreamSimple`
inline). go-review **ship** (3 LOW polish); parity review **fix-first → fixed**;
gofmt/build/vet/`-race` green, differential 37/37.

### v0.80.12
Upstream sync `dcfe36c7 → a9f6a315` (pi 0.80.10; three release tags crossed —
0.80.8 rode in via merge). The model-runtime facade landed **SDK-scoped** per
the 2026-07-17 ruling: ModelsStore persistence (`checkedAt` entries), context-
taking provider refresh with offline restore + `force`, `checkAuth`/
`getAvailable`/`login`/`logout`, overloaded `getAuth` with case-insensitive
model-header merge, Models-only header transforms, provider-scoped api-key
resolution (`AuthInteraction` rename), and github-copilot credential-based
model filtering — host runtime, OAuth acquisition, and the radius provider
stay out. Kimi K3 deferred tools went **live** on openai-completions with the
0.80.10 catalog (system-message tool re-declaration); xai's default is now
grok-4.5, routed through Responses with `include:["reasoning.encrypted_content"]`.
The adversarial parity review caught two merge-carried hunks first-parent
triage missed (the xai include + the force flag) — both ported and
mutation-verified; triage now sweeps the whole-range `packages/ai/src` diff.
Differential 37/37; `-race` green; catalog 420,411 B endpoint-pinned.

### v0.80.11
First release under the pi-tracking scheme (`major.minor` = pi 0.80; patch 11
continues the port's own counter from `v0.2.10`). Upstream sync
`81de5702 → dcfe36c7` — rolls up four cycles (07-11, 07-13,
07-14, 07-15). npm reference build advanced 0.80.6 → **0.80.7** (single release
crossed, this cycle). Per-cycle triage/port detail is in
[`UPSTREAM.md`](UPSTREAM.md); headline ports:

- **Catalog → 0.80.7** (`818d6745` + data commit `1f9e846c`) — re-derived
  byte-identical (**416,889 B**) from the integrity-verified build's MODELS,
  endpoint-pinned both ends. Drains the deferred queue (fable-5 xhigh/max,
  copilot 1M, mai-code `/responses` routing, opencode `sessionAffinityFormat`);
  adds gpt-5.6 luna/sol/terra, gpt-realtime-2.1, kwaipilot kat-coder; prunes 11
  openrouter/vercel ids. No schema drift.
- **System-prompt `Current date` removed** (`f4e9ca74`) — the daily date line
  (and its computation) dropped from both prompt branches, keeping only
  `Current working directory`. Byte-confirmed against the shipped build.
- **`pi-messages` provider API** (`961fa6c1`, 07-14 ruling) — new first-class
  provider API (POST `{model,context,options}` + SSE), SDK-only; Radius OAuth +
  host wiring stay out. Hardened after review (stream-goroutine recover, CRLF
  normalization, `OnResponse` error propagation).
- **openai-responses `encrypted_content` backfill** (`1f0dbc00`) — indexes
  reasoning blocks by item id and backfills a late signature from the terminal
  response so `store:false` replay stays stateless.
- **Deferred / message-anchored tool loading** (`3d8f7435`) — cache-friendly
  dynamic tool loading (`AddedToolNames`, `ai/deferred_tools.go`), gated to
  first-party Claude ≥4.5 + openai-responses.
- **Responses `tool_choice` + OpenRouter session-affinity formats**
  (`eacaa130` / `298665cf`) — latent `ToolChoice`, and a `sessionAffinityFormat`
  selector replacing the boolean toggles across both OpenAI-compatible providers.

Reviewed each cycle via independent go-review + adversarial parity review (all
faithful; catalog endpoint-pinned; differential 37/37); `-race` suite green.

### v0.2.9
Upstream sync `8c943640 → 81de5702` — five cycles (07-06 through 07-10). npm
reference build advanced 0.80.3 → **0.80.6** (v0.80.4/5/6; each regen supersedes
the prior).

- **Catalog → 0.80.6** (`d321e2b`) — re-derived byte-identical (411,270 B);
  legacy claude-3.x/4-0 pruned, smoke pin repointed to
  `claude-haiku-4-5-20251001`.
- **Input-based pricing tiers + `max` thinking level** (`fbdd4638` + `a9ecf301`)
  — `ModelCost.Tiers`; `ThinkingMax` above `xhigh`; openai-responses parses
  `cache_write_tokens`.
- **Retry-classifier expansions** — CF-524 (`d53b5676`), Bun socket drop
  (`4285712b`), gRPC ResourceExhausted (`57d96d72`).
- **Stale-usage-after-compaction guard** (`8973ae28`); **fail tool calls from
  length-truncated messages** (`351efc82`); **`(no tool output)` placeholder**
  (`279f53b0`); **anthropic empty-text signed thinking block** (`6731a0ba`);
  **remove Vercel AI Gateway attribution** (`83cbfc65`); **clamp openai-responses
  max-output floor to 16** (`2e4ad6a0`).

Reviewed via independent go-review + adversarial parity review each cycle (all
faithful; request diff 37/37); `-race` suite green.

### v0.2.8
Upstream sync `9be55bc7 → 8c943640` (upstream release v0.80.3). Catalog
regenerated from npm `@earendil-works/pi-ai` 0.80.3 (endpoint-pinned,
integrity-verified) — claude-sonnet-5 across anthropic/bedrock/openrouter/
vercel/copilot, Fireworks GLM-5.2 Fast realign, claude-3.5-haiku-* removed; plus
bash-tool timeout validation (reject non-positive / oversized). Independent
go-review (ship) + adversarial parity review (both faithful).

### v0.2.7
Rolls up **three upstream cycles** untagged since v0.2.6 (`2417adb4 → a2e3e9d8`):
the v0.79.10 catalog cycle, the 2026-06-23 model-registry adopt (`732bb161`
auth substrate + Models runtime + BuiltinModels), and the 2026-06-24 migration
completion. npm reference build advanced 0.79.9 → **0.80.2** (via 0.79.10,
0.80.0, 0.80.1, 0.80.2 — each regen supersedes the prior). The model-registry
migration is now **complete**. Per-cycle triage/port detail is in
[`UPSTREAM.md`](UPSTREAM.md); headline ports below.

- **Catalog → 0.80.2** — endpoint-pinned, re-derived byte-identical (386,548 B),
  integrity-verified `sha512-5GNKfdrR…uy9RQ==`. huggingface registration
  provider + glm-5.2/glm-5v-turbo; `off:null` tripwires intact.
- **Models-runtime migration** (`732bb161` + the 2026-06-24 follow-through) —
  auth substrate (credential store, provider auth, OAuth-under-lock), Models
  runtime (Provider iface, CreateModels/CreateProvider, BuiltinModels),
  request-scoped auth resolution (`ef231c49`), and the `api-key`→`api_key` /
  credential `metadata`→`env` alignment (`49fbe683`). Globals remain the compat
  consumer surface.
- **OpenAI Responses terminal events** (`cd95c274`) — `response.incomplete`
  finalizes like `response.completed`; the stream fails when it ends with no
  terminal event.
- **anthropic compat → catalog** (`6184307c`) — provider/baseUrl auto-detection
  removed; fireworks / cloudflare-ai-gateway-anthropic values come from the
  catalog. Byte-identical for catalog models.
- **header-only client auth + vercel routing ungate** (`129eb460`) — an
  authorization / cf-aig-authorization header satisfies auth without an api key;
  vercel gateway routing is no longer baseUrl-gated.
- **Deliberate divergences** (2026-06-24 ruling): `ProviderHeaders`
  null-suppression, cloudflare base-URL relocation, and compat
  `shouldUseBuiltinModels` routing are not transliterated — confirmed
  observably byte-identical through the Go compat-globals consumer path.

Reviewed via independent go-review (ship) + adversarial parity review (all
faithful; catalog re-derived byte-identical; 6/6 differential request diff).

### v0.2.6
Upstream sync `56b22768 → 2417adb4` — 22 main-line changes (**4 behavior/perf
ports + 1 catalog regen, 17 n/a, 0 decides**). npm reference build advanced
0.79.8 → **0.79.9**.

- **Catalog → 0.79.9** (`615bf2f8`) — endpoint-pinned byte-identical both ends
  (old ≡ 0.79.8 build, new ≡ 0.79.9, integrity-verified). 0 added, **2 removed**
  (`google/gemma-4-E2B-it`, `gemma-4-E4B-it`; no Go refs), 20 changed (cost/
  metadata churn + the folded-in data commits). `off:null` gates intact.
- **chat-template thinking compat** (`8b97e75c`) — new openai-completions
  `thinkingFormat:"chat-template"` emits configurable `chat_template_kwargs`
  ($var/omitWhenOff/scalar), with insertion-order-preserving output for
  byte-exact request bodies. **Latent**: no 0.79.9 catalog model sets it
  (reachable only via custom model config).
- **Fuzzy edit preserves untouched lines** (`128330e3`) — a fuzzy edit now
  rewrites only the touched line-blocks and copies every other line back
  verbatim, instead of globally normalizing the file.
- **Legacy WSL bash via stdin** (`1287b69f`) — the Windows-bundled
  System32/Sysnative `bash.exe` (which mishandles `-c "<cmd>"` quoting) is run
  as `bash -s` with the command on stdin.
- **Session-branch linearization** (`a1da88ae`) — O(n²) prepend replaced with
  append+reverse; behavior-neutral.

Reviewed via an independent idiomatic go-review (ship) + adversarial parity
review (5/5 faithful; catalog endpoint-pinned byte-identical, tripwire +
orphaned-id checks passed); `-race` suite green.

### v0.2.5
Upstream sync `29c1504c → 56b22768` — 32 main-line changes, **0 behavior
ports, 32 n/a, 0 decides**. npm reference build advanced 0.79.6 → **0.79.8**
(two release tags crossed; v0.79.7 superseded by v0.79.8).

- **Catalog → 0.79.8** (`8eb9704b`) — endpoint-pinned byte-identical both ends
  (old ≡ 0.79.6 build, new ≡ 0.79.8 build, integrity-verified). +9/−3 ids
  (opencode-go GLM-5.2, openrouter/fusion alias, fireworks glm-5p2,
  poolside/qwen/cohere/gemini-3-pro-image/liquid; pruned glm-5/raptor-mini/
  xiaomi-mimo); 44 changed entries are data churn (Mistral prompt-caching cost
  fields, fireworks/openrouter/vercel metadata). `off:null` gates intact.
- **No behavior change vs v0.2.4.** The substantive upstream changes landed on
  unported surface: the compaction trio (overflow-retry recovery, empty-summary
  guard, post-compaction token estimates) edits the agent-session-runtime
  auto-compaction orchestration + event lifecycle the Go port doesn't have;
  RPC unknown-command id (`modes/rpc`), Mistral prompt-caching (Mistral
  provider), and the `CONFIG_DIR_NAME`/edit-diff SDK exports are all out of
  scope.

Reviewed via an independent adversarial parity review (catalog endpoint-pinned;
schema-drift, tripwire, and orphaned-id checks passed); `-race` suite green.

### v0.2.4
Upstream sync `f8a77f47 → 29c1504c` — 20 main-line changes (3 ported, 16 n/a,
1 decide ruled). npm reference build advanced 0.79.4 → 0.79.6.

- **Z.AI GLM-5.2 native reasoning_effort** (`75b0d723`) — emits the
  `thinkingLevelMap`-mapped effort alongside `thinking:{type}`; `minimal:null`
  omits the field.
- **Null Responses message content** (`2d597f02`) — no code change; Go ranges a
  nil slice safely, matching pi's `?? ""`. Locked with a regression test.
- **Provider-scoped env overrides** (`7f29e7a3`, owner-ruled) — `StreamOptions.Env`
  consulted ahead of `os.Getenv` for `PI_CACHE_RETENTION` + Cloudflare base-URL.
  Bun `/proc` fallback omitted (no Go analog); host-side population unported.
- **Deepseek disabled-thinking gate went live** — 0.79.6 ships Kimi K2.7 Code
  `off:null`; tripwire converted to `TestDeepseekDisabledThinkingGateLive`.

Reviewed via independent go-review + adversarial parity (request diff 12/12).

### v0.2.3
Docs-only release: README disclosure that the SDK sends pi's attribution headers
by default and how to disable (`PI_TELEMETRY=0`) or override
(`model.Headers`/`opts.Headers`). No code change vs v0.2.2.

### v0.2.2
Upstream sync to `f8a77f47` (pi 0.79.4). Anthropic 1h cache-write priced at
2×input; bash stdout drained past child exit; deepseek `off:null` +
`gemini-flash-latest` thinking gates; provider-attribution headers
(OpenRouter/NVIDIA/Cloudflare/Vercel/OpenCode, `PI_TELEMETRY`-gated). Review
caught an attribution header-precedence divergence, fixed and re-verified.

### v0.2.1
Upstream sync `3f44d3e2 → 6f29450e` (pi 0.79.3). Catalog at 0.79.3; Anthropic
refusal-detail error messages; custom-fallback thinking reasoning flip; agent
ignores late tool-progress updates. Claude Fable 5 disabled-thinking gate went
live (0.79.3 ships `off:null`). Parity 4/4, request diff 6/6.

### v0.2.0
First catalog-synced release (pi 0.79.1): Claude Fable 5 + moonshot/opencode
compat, zai thinking payload, anthropic `off:null` thinking gate, `:thinking`
suffix in custom-model fallback, `PI_EXPERIMENTAL` guard. Introduced
`docs/UPSTREAM.md` and the daily sync pipeline.

### v0.1.1
Fixes the SDK trap where importing only the coding package raised
"No API provider registered" on the first live call — providers now register via
`init()` (pi's module-load side effect), wired through coding's import.

### v0.1.0
Initial tagged baseline of the Go port.
