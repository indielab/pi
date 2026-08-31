# Upstream provenance & sync ledger

Tracks exactly which upstream pi the Go port corresponds to, and the
commit-by-commit sync pipeline that keeps it current.

- **Upstream**: https://github.com/earendil-works/pi (TypeScript, by Mario Zechner)
- **This port started**: 2026-06-08 (cloned upstream `main` HEAD of the day)

## Current pin

| What | Value |
|---|---|
| TS source fully reviewed/ported | `853a80d26` — "Add [Unreleased] section for next cycle" (2026-08-29; **2 port, 2 port-but-QUEUED, 3 n/a, 0 decide**). Delta `56f3f33a9..853a80d26`, **7** first-parent changes, **no merges** — first-parent count and total commit count are both 7, so nothing rode in under a merge. **RELEASE v0.84.4 CROSSED** at `b79e4cc83`: all nine `packages/*/package.json` go 0.84.3 → 0.84.4 and the tag `v0.84.4` contains that sha, so the catalog is regenerated, the npm reference build moves to **0.84.4**, and a **port tag (`v0.84.20`) is cut with a release tweet**. Two Go commits plus one review-fix commit: `08f5bed` (catalog regen) and `e1e8cdb` + `3ad9fe5` (the agent-loop turn-preparation reorder, upstream `56700d42e`). **Catalog:** 558,804 → 563,988 B, **1312 → 1290 models**, 39 providers unchanged, 57 added / 79 removed / 227 changed, decided by EXECUTING `JSON.stringify(MODELS)` against both builds and endpoint-pinned at both ends, so the ported diff IS the upstream release diff. **No schema drift at any level** — model key set, `cost` sub-keys and the 26 `compat` keys all identical — and `compat.allowedFallbackModels` stays at its two anthropic entries with identical chains. The `port-but-CATALOG-ONLY` queue **drains 3 → 0**, every member verified an ancestor of the release sha by `merge-base --is-ancestor` rather than by log order. The **Scope queue takes two deltas** — entry 6 (Mistral) gains `6c87d9a02` and entry 3 (images) gains `5ce4afbd9`, both `port-but-QUEUED` with no Go base — and **no new row was opened**. The harness backlog is **unchanged at 11** against entry 8's own tree. Both reconciliation passes ran: the unrestricted `--name-only` **detector** printed **47** paths, all classified and all status M (no A/D/R even under `--find-renames`) — no new top-level package, no file moved out of `packages/`, no new repo-root directory; the **accounting** sweep reported **31 files, 493(+)/140(−)**. Base gate green: `gofmt -l` clean, `go build ./...` and `go vet ./...` clean, `go test -race ./... -count=1` green across all **10** test-carrying packages (13 listed, 3 with no test files). Differential harness re-pinned `56f3f33a9` → `853a80d26` and moved to the 0.84.4 dist, still **49 dist / 0 src**, at **49 PASS / 0 KNOWN / 0 FAIL**, exit 0. Nothing under `ai/providers/openai*.go` changed, so the 6-scenario request diff was not triggered. **One new deliberate divergence** — steering typed *during* compaction lands one turn later in the port than in pi, a tripwire on the compaction-placement ruling; see the cycle section. Prior pin: `56f3f33a9` (2026-08-28, 0 ports, no release). |
| npm build the byte-goldens were captured from | `@earendil-works/pi-ai` **0.84.4**, **moved this cycle** (0.84.3 → 0.84.4) — the delta crosses release `v0.84.4` at `b79e4cc83`, so the reference build under `~/.cache/pi-npm/0.84.4` is the new ground truth and the differential harness runs against its dist. Both builds were authenticated before use: package-lock integrity matches the registry `dist.integrity` for 0.84.3 (`sha512-M0YUV8…P32XQ==`) and 0.84.4 (`sha512-AClAZxf5…ypfVw==`). |
| Parity proofs at the pin | **2026-08-29 (2 ports, RELEASE v0.84.4):** the catalog proven by **endpoint pinning executed on both builds**, and independently re-derived by BOTH review gates rather than taken from the porter's intermediates — `JSON.stringify(MODELS)` imported from each build's `dist/models.generated.js`, with the OUTGOING embed at `768d336` byte-identical (sha256 `83154b13…ca5429`) to the 0.84.3 derivation and the incoming one byte-identical (`3775ebf0…260604`) to a fresh 0.84.4 dump, plus a cross-check that the two derivations genuinely differ (first at char 18837) so the match is not degenerate; both npm builds authenticated against registry `dist.integrity` first; schema drift enumerated against the consuming types rather than eyeballed (13 model keys, 26 `compat` keys, `cost` sub-keys with 22 tiered models, 9 `api` values, 7 `thinkingLevelMap` levels — all unchanged and all covered by `ai.Model`), with **no duplicate JSON keys** anywhere (checked with an `object_pairs_hook`, so no silent last-wins) and **zero null-valued fields**, so no null-vs-absent hazard; the orphan sweep run over all 40 `defaultModelPerProvider` entries AND all 32 `difftest/scenarios` model references, with the only two misses (`radius`, and `vertex/claude-haiku-9` in a cross-provider scenario) shown **pre-existing in the 0.84.3 embed too** rather than caused by the regen. The loop reorder proven faithful hunk for hunk against `853a80d26:packages/agent/src/agent-loop.ts:163-273`, with the single-type reuse established **structurally rather than assumed**: upstream's `PrepareNextTurnContext extends ShouldStopAfterTurnContext {}` adds **zero members** (`types.ts:147`), so Go's one type is exact. Two candidate defects were attacked and both cleared: the `NewMessages` slice-header copy (every statement between the snapshot and the next `PrepareNextTurn` enumerated — `ShouldStopAfterTurn`, the steering poll, the follow-up poll — none appends) and the new head-of-loop poll versus `skipInitialSteeringPoll` (the one-shot skip is consumed by the unconditional pre-loop poll, same as upstream `agent.ts:446,476`). **Mutations: 4, each red on exactly one test and green elsewhere** — reverting `PrepareNextTurn` to its pre-`56700d42e` position reds exactly the two reorder tests and **no pre-existing test**, establishing nothing else had silently encoded the old order; deleting the re-poll reds only the pick-up test; dropping the `len(pending) == 0` guard reds only the discard test; and gutting `applyTo` reds only the new snapshot test — that last one added **because** the reviewer showed the whole apply path could be deleted with the entire repo suite still green. The load-bearing no-port claim for the coding-agent half was verified structurally and independently: `TransformContext` is invoked inside `streamAssistantResponse` (`agent/loop.go:252`) on every request the loop makes, so upstream's new POSITIVE test is satisfied by construction and so is its new NEGATIVE one (a terminating batch produces no request, and the port only compacts when a request is built) — with `PrepareNextTurn` confirmed to have no setter anywhere in the port, so `_installAgentNextTurnRefresh` has no counterpart to move. The one upstream test that does NOT hold is recorded as a deliberate divergence rather than asserted away. Harness **49 PASS / 0 KNOWN / 0 FAIL**, exit 0, on the newly published 0.84.4 dist. **2026-08-28 (0 ports, no release):** nothing new to prove — no ported surface moved, established by tree identity at sha-named reads rather than by reading the diff: `packages/ai/src` is tree-identical at `ccfe79ed2` and `56f3f33a9` (`bb234391`), as are `packages/agent` (`9073a144`), `packages/protocol/src`, `packages/client/src`, `packages/server/src`, `packages/telemetry`, `packages/session-backends` and `packages/coding-agent/src/{utils,client,server}`. The only movement inside a ported package is `packages/ai/test` — one deleted catalog assertion with no Go counterpart — and `core/settings-manager.ts`, which is E1. The proofs below still stand at this pin. **2026-08-27 (4 ports, no release):** the thinking-signature change proven **by executing real pi, not by reading it** — `EventStream.prototype.push` patched in the TS at `ccfe79ed2` to deep-snapshot `partial` at push time, driven through a fake fetch, and the identical SSE run through Go: two interleavings, **event-for-event and byte-for-byte identical**, with the signature reading the bare reasoning FIELD NAME on every partial up to and including `text_delta` and the full sequence appearing only from `thinking_end` (and, for a truncated body, for the first time on the `error` event's message on both sides). Apply sites enumerated rather than assumed: exactly two in Go (`openai.go:107` in `fail`, `:522` before the thinking_end push) against pi's two (`openai-completions.ts:431` in `finishBlock`, `:694` in `catch`), with a grep establishing there is no third. The **at-most-one-thinking-block invariant the port relies on was PROVEN both sides**, not accepted: pi assigns `thinkingBlock` only inside `ensureThinkingBlock` under `if (!thinkingBlock)` and never resets it (all 5 references), Go's `thinkBuilder` likewise only under `if thinkBuilder == nil`. The absent-vs-empty question — pi's guard is `!== undefined`, Go's is `len(...) == 0` — was closed by showing the empty-but-present state is **unreachable in pi**: `streamedReasoningDetails ??= []` is immediately followed by an append with no branch between, so the length is ≥ 1 the instant the variable stops being undefined. `tool_choice` proven on the wire against the PUBLISHED build — 0.84.3's own `dist/api/openai-completions.js:594` reads `if (options?.toolChoice)`, i.e. it never contained the reverted guard — and the renamed scenario proven **load-bearing rather than passing by mutual absence**: both sides emit `"tool_choice":"none"` with no `tools` key, and reinstating the guard in a scratch worktree turns it into `FAIL … missing-key @ $.tool_choice / pi: "none" / go: <absent>`, exit 1, while the with-tools control still PASSes — so the pair localizes the difference to the tools param. All three Go `tools`-writing paths checked, the empty-array tool-history arm by hand against pi dist (both emit `"tools":[],"tool_choice":"none"`). The session repair adjudicated against pi's **three** non-test `loadEntriesFromFile` callers at the sha rather than against the porter's summary. `??` semantics for `SystemRoot` settled at the DISCRIMINATOR, not the name: Node's `undefined` comes from libuv's `uv_os_getenv` returning ENOENT and Go's `LookupEnv` ok-flag from the same `ERROR_ENVVAR_NOT_FOUND`, so `LookupEnv` matches `??` under either Windows semantic where `Getenv` would match only one — mutation-locked. **Every finding from both review gates was put through an independent adversarial verifier instructed to REFUTE it: 2 of 15 survived**, both fixed in `b789eca`; 13 were refuted with reproduction. Harness **49 PASS / 0 KNOWN / 0 FAIL**, exit 0, re-run independently by the parity reviewer and again with `PI_UPSTREAM_DIR=/nonexistent`. **2026-08-25 (6 ports, RELEASE v0.84.3):** the catalog proven by **endpoint pinning, executed on both builds** rather than read from git — `JSON.stringify(MODELS)` from each build's `dist/models.generated.js`, with the outgoing `ai/models_catalog.json` `cmp`-identical to the 0.84.2 derivation and the incoming one `cmp`-identical to a second, independently re-derived 0.84.3 dump, so the ported diff IS the upstream release diff (1267 → 1312 models, 536,642 → 558,804 B); the newly-activated `allowedFallbackModels` proven **on the wire, not in the JSON** — `TestAnthropicCatalogFallbacksAreLive` drives the REAL catalog compat of both anthropic models through a stub and asserts the projected `[{model}]` chain in catalog order, and was verified **red against the 0.84.2 catalog** for the right reason ("catalog carries allowedFallbackModels for []"), with a verifier separately confirming it also reds when the field is present but no longer decodable; the bash `command` description change confirmed against **npm 0.84.3's own dist** and the composed bash `description` proven byte-**un**changed by reading the `Execute a ${config.shellName} command…` template out of `dist/core/tools/bash.js` with `shellName: "bash"`; the DEFAULT system prompt proven unmoved **structurally** — `defaultActiveToolNames` read at `a79b37334:src/core/sdk.ts:256` is still the four-tool list, and `systemprompt_golden_test.go` passes UNEDITED and is in no commit's file list; the reasoning-delta merge proven by **four mutation probes each red on exactly one operator** (push-instead-of-merge → 7 of 10 subtests; `index` nullish→falsy → only the "index of 0 survives" case; `format` falsy→nullish and `id` nullish→falsy → the empty-string case from either side), with the merged entry's KEY ORDER pinned as a byte string because JS assignment keeps an existing key in place and appends a filled-in one; `tool_choice` suppression proven on the wire for **both** Go paths that write `tools` (converted array and the `[]map[string]any{}` tool-history case) and, in the harness, by **temporarily re-pointing the new scenario at the 0.84.3 dist to watch it FAIL** (`pi: "none"` vs `go: <absent>`) — which is what proves the scenario load-bearing rather than passing by mutual absence; the compaction call-site collapse (pi 3 → Go 1) proven by **two independent reds** on the two paths that reach the single gate (`summarize` returned partial text; `compact` installed the truncated summary and dropped 9 of 12 messages) plus a grep establishing branch summarization has no Go implementation at all; the MIME export proven by a source-level assertion (a call-site test would have red as a **compile error**, which does not count) and the "and no more" half locked against pi's own `index.ts` at two shas. **Every finding from both review gates was then put through an independent adversarial verifier**: 1 should-fix and 3 nits survived and were fixed, 5 were refuted with reproduction. Harness **49 PASS / 0 KNOWN / 0 FAIL**, exit 0, fully dist-backed. **2026-08-24, second check (0 ports):** nothing new to prove — the delta touches no ported surface at all: `packages/ai`'s **whole** tree is identical at `a470b121b` and `4af9d21d3` (`a37cc08d`), as are every other ported package's `src`, `packages/ai/scripts` and `packages/session-backends`; the proofs below still stand at this pin. **2026-08-24 (1 port):** the walk was proven by **execution, not inspection** — pi's `findNodePackageDir` extracted verbatim from `a470b121b:packages/coding-agent/src/config.ts:368-383` into a runnable harness and driven against the real Go function over one shared 12-branch tree, **40 start-paths, same cwd: 40/40 agree on every path `PackageDir()` can actually produce**. Termination proven by **instrumented probe-sequence capture** rather than by reading the loop: with a stub where only `/package.json` exists and start `/usr/local/bin`, pi probes `/usr/local/bin`, `/usr/local`, `/usr` and answers `/usr/local/bin`; the new Go probes the **same three in the same order** for the same answer; the OLD Go probed a **fourth** (`/package.json`) and answered `/` — which is what establishes the closed root-probe divergence as real rather than asserted. `basename` vs `filepath.Base` cleared by a Node-vs-Go table: they disagree on exactly three inputs (`""`, `"/"`, `"//"`) and **neither side yields `"dist"` in any of them**, so the `== "dist"` test never flips; `"/"` cannot even enter the loop body, being a fixed point of both `dirname` and `filepath.Dir`. Fallback equivalence checked at the parameter, not the name: pi returns `startDir` and Go returns `start`, both unmutated (Go's loop variable is `for`-scoped). **Golden proven not to move, and the reason matters**: `systemprompt_golden_test.go:27-29` passes `/pkg/...` through opts so `PackageDir()` is never called on the golden path, and the second golden (`:74`) is the custom-prompt branch that omits the docs block entirely — but `coding/session.go:379` leaves all three opts empty, so **live** default-prompt bytes DO move with `findNodePackageDir`, in the two cases recorded in the cycle section. Mutations: **4**, each verified red on exactly one arm and green elsewhere, and re-run independently by the parity reviewer in a scratch copy. **2026-08-22 (0 ports):** nothing new to prove — no ported surface moved (`packages/ai` and `packages/agent` are **tree-identical** at `b7bb00b93` and `c49906ec7`); the proofs below still stand at this pin. **2026-08-20 (5 ports, no release):** header precedence established by **executing the pinned vendor SDKs**, not by reasoning about them — @anthropic-ai/sdk **0.91.1** and @google/genai **1.52.0**, both taken from `~/.cache/pi-npm/0.84.2` (versions re-confirmed off their own `package.json`s) and driven against a local server with the exact header object `mergeClientHeaders` builds at the sha: an **8-case matrix** — oauth and api-key branches x four spellings and both null markers — which the port now matches **8/8**, alongside the PRE-commit `mergeClientHeaders` run through the same SDK returning the opposite answer, which is what proves the seed rather than the merge order is what changes it; the two SDK behaviours the port cannot reproduce (@google/genai comma-joining case-variant names, `net/http` dropping an empty-string user agent) were established the same way and recorded as deliberate divergences rather than asserted away. `getPiUserAgent()` executed from the 0.84.2 dist and `cmp`-compared against Go's `piUserAgent()` — **25 bytes, identical**. `jsStringify` verified against node over **1,863 generated cases plus a 47-case hand-written table, zero divergences** — random float64 bit patterns rendered several ways, exponents from `1e-330` to `1e330`, integer literals 1-29 digits long, 300 random key sets mixing index-like and ordinary keys, 100 duplicate-key objects, and 400 random Unicode strings emitted both `ensure_ascii=True` and `False`. The reasoning-details pair's net-zero halves were proven by **BLOB IDENTITY**, not by reading the diff: `git rev-parse 4ca636c5e^1:packages/server/src/protocol.ts` and `git rev-parse b7bb00b93:packages/server/src/protocol.ts` return the same object (`069828e5`), and on `packages/ai/src/types.ts` the `reasoningDetails` field occurs exactly once at `4ca636c5e` and zero times at BOTH ends, the entire cross-pair diff of that file being one **comment** line — so the session format is byte-untouched. **The differential harness earned its keep**: two reviewer probe scenarios FAILED against ground-truth pi before the fix — value at `$.messages[2].reasoning_details[0].conf` (`pi: 1` vs `go: 1.0`) and key order at `$.messages[2].reasoning_details[1]` (`pi: 0,type,data,id,custom_flag,nothing` vs `go: type,data,id,custom_flag,nothing,0`) — which is what turned byte compaction into `jsStringify`. Mutations: **8** recorded for `a20597b` and **22** for `41171de`; `1308a5a` and `dc2befa` record theirs individually rather than as counts (the `splitBOM` call site, observationally inert under the first fixture and leaving the whole `coding` suite green when removed; and the headers slice's four families — seed deleted -> 5 subtests red, seed moved back to the old force position -> 23 subtests across 10 tests red, re-assigned key promoted to the end -> the OAuth subtest red, sorted order instead of slot order -> 4 tests red). Every one verified red for a **behavioral** reason. Harness **47 PASS / 0 KNOWN / 0 FAIL**, exit 0, re-verified for this ledger entry. **2026-08-19 (3 ports, no release):** the anthropic fallback-costing extract verified against an **oracle transliterated from `4809c2abc:packages/ai/src/api/anthropic-messages.ts` lines 606-613** — modelling `?.cost`, `??` and the truthiness gate with an explicit present/absent type rather than paraphrasing them — run over a **10 compat × 7 fallback-set × 5 served-id matrix (350 cases), zero divergences**, covering unpriced targets, `"cost": null`, zero-priced targets, duplicate-first-unpriced, self-referential catalog entries, `served == ""`, malformed compat and the `"default"` arm, and additionally asserting that the shared catalog `*ai.Model` is never mutated and that the no-swap arm returns the SAME pointer (pi's `: model`); `cost` proven stripped from the `fallbacks` wire value on a real request body (pi's explicit `.map(f => ({model: f.model}))`), mutation-locked; the compaction prompt constants **executed and byte-compared pin vs HEAD** to clear `ef8dc7385`'s prompt-shaped refactor — composed UPDATE prompt **1257 chars, identical**, `SUMMARIZATION_PROMPT` and `SUMMARIZATION_SYSTEM_PROMPT` identical; the un-port proven **exactly** the revert by byte-comparing `agent/loop.go` and `agent/agent.go` against `b79c9e6` (the pre-port parent — identical) and by a `go doc -all` delta showing exactly the three identifiers upstream removed, with no remaining callers including `examples/` and `cmd/`; the two revert PAIRS proven net-zero by `git rev-parse` blob comparison rather than by reading the subjects; **19 mutations each verified red for the right reason**, with one planned mutation accepted as correctly GREEN (the `min` inside `clampThinkingBudgetToAnswerRoom` is algebraically inert — the anthropic test's value comes from a second, separate clamp in `StreamSimpleAnthropic`). Harness: **40 PASS / 0 KNOWN / 0 FAIL** (34 → 40, six new `src` scenarios), verified cold after `rm -rf pisrc/3a0b9a3ee`. **2026-08-18 (6 ports, no release):** `resolveGoogleThinkingLevel` **executed from sha-extracted TS** (`google-shared.ts` at `af2c35223`, blob `a49c6689`) against Go over 18 inputs — **18/18 byte-identical**, covering every `String(mapped)` case (absent → `undefined`, explicit null → `null`, self-referential `"xhigh"`, uppercase, empty-string entry with its trailing space, non-ASCII provider/id) and confirming Go renders the ORIGINAL mapped string, not the lower-cased one; the anthropic fallback beta driven through **all four** Go auth branches against a live server (third in `betaFeatures` every time, absent when unset, and the OAuth branch keeping pi's `claude-code-20250219,oauth-2025-04-20` prefix); `fallbacks` differential-verified on the wire for **both arms** of pi's union (order-preserving 2-entry chain; the `"default"` literal); the served-model capture verified live (`served-model` overriding a requested `claude-x`); the catalog confirmed to carry zero `allowedFallbackModels` at the sha AND in the Go embed, so the feature is dormant both sides; tool choice wire-verified for all five ported providers with google's precedence probed separately **8/8** (strict+auto → `VALIDATED`, i.e. auto does NOT short-circuit; strict+none → `NONE`); the summarization guard proven a strict cover (pi has exactly three `completeSummarization` callers at the sha, all guarded; Go has exactly two, both routed through the single site). Harness: **21 PASS / 0 KNOWN / 0 FAIL** on the 0.84.2 dist, plus **13 new scenarios** built for this cycle's surface (`toolChoice`/`refusalFallbacks` threaded through `SimpleStreamOptions`, re-pinned to `2509b5c03` with `backend:"src"`) — **13 PASS**, payloads inspected to confirm the keys are present rather than passing by mutual absence — those 13 were **promoted into the shipped harness the same day**, taking it to **34 PASS / 0 KNOWN / 0 FAIL**. **2026-08-17 (5 ports, no release):** the single-`Set` rendering of pi's delete-then-set proven by auditing every header write path in `ai/providers` (all `Set`/`Del`, no raw map writes) with exactly-once wire asserts, and the UA string **byte-compared against `getPiUserAgent()` executed from the authentic 0.84.2 dist**; the kimi cache-read chain byte-checked at `d3ab2af96` with per-arm nullish semantics locked by tests; the compaction SessionID threading checked call-site-for-call-site (agent-session passes `undefined` upstream ⇒ behavior unchanged; branch-summary shape stays fresh); `isSingleEditInput`'s domain identical over JSON values; the skills `isString` screen verified against **yaml@2.9.0** (the exact version pinned at the sha and in the build) via a 74-literal adversarial probe, **74/74** agreement; **7 mutations in a scratch worktree each red for the right reason**; catalog untouched and independently re-derived `cmp`-identical (536,642 B); harness **21/21 PASS** on the 0.84.2 dist exercising both changed builders. **2026-08-15 (4 ports, release):** the 0.84.2 catalog **independently re-derived and `cmp`-byte-identical** (536,642 B), endpoint-pinned at both ends (old embed ≡ a fresh 0.84.1 derivation ⇒ ported diff ≡ upstream release diff); schema drift enumerated against consuming types (one new key, decoded); the Google guard proven same-value/same-point against `5093641a5` with `mapGoogleStopReason` entry-for-entry ≡ upstream and mutation-verified on a scratch copy (guard removed → "got toolUse"); the APP_NAME no-op proven against the **shipped package** (`piConfig` carries no `name` ⇒ every changed string byte-identical for stock pi); the zai table compared whole (41 entries, delta exactly the two upstream lines) and mutation-verified via the new guard test; the harness flip justified by ancestry (all 13 scenario-note shas first-parent ancestors of `914cf1472`) and by the reviewer's own re-run: **21 PASS / 0 KNOWN / 0 FAIL on the 0.84.2 dist**. **2026-08-14 (1 port):** the kimi UA override proven **sha-anchored + mutation-verified** (headers are outside the bodies-only harness): all three upstream `createClient` branches confirmed to route through `mergeClientHeaders` after `optionsHeaders` with no per-request header bypass; the Go single-`Set` equivalence established by reading every write path (all canonicalize — no raw map writes); Node token fidelity checked down to the `RTL_OSVERSIONINFOW` layout and libuv's `uv_os_uname` release format; the authentic 0.84.1 dist (integrity-matched) shown to carry `KimiCLI/1.5` and no pi-user-agent module, making TS-at-`9d2ec7ffa` the reference; and both tests mutation-verified in a throwaway worktree (override removed → wire shows `[custom-client]`; made unconditional → non-kimi test fails). Harness re-run anyway for the body surface: **21/21 PASS**. **2026-08-12 (1 port):** the strict conversion was proven against **executed upstream TS at `7915cdac6`** three ways: a 28-case conversion probe (`makeStrictJsonSchema` run via node from sha-extracted source vs Go) byte-identical on 24/28 including every error string and the unsupported-key precedence order — the 4 mismatches are the recorded decode-boundary drift class, not wire-reachable through pi's own tools; a 10-case `validateToolArguments` probe vs **real TypeBox** (npm 0.84.1's) 10/10 including the nested-`$ref` compile-path constructed to break the `Check(nil)` mapping; and 3 new differential-harness scenarios asserting full request bodies on the anthropic/openai-completions/openai-responses wires (required ordering with deliberately non-alphabetical properties, anyOf-null widening, no-rewrap of already-nullable shapes, nested object closing, zero-property object, inconvertible-tool fallback carrying the ORIGINAL parameters). The harness is what caught the one real divergence (`required: []` dropped on zero-property strict objects) — fixed, then re-verified **21/21 PASS**. **2026-08-11 (3 ports):** cwd-footer byte-proven via `cmp` on both prompt branches; DeepSeek fold faithful on membership and order (15 terms) and on `strings.Contains(ToLower)` ≡ `.toLowerCase().includes` for the ASCII needle; gateway binding proven by running real upstream TS against real Go `Do()` over a shared case table (7 divergences found and pinned). |
| Reviewed via | 2026-08-29 (2 ports, RELEASE v0.84.4) — per-commit diff triage of all 7 changes, judged from the real diff rather than the subject line; hunks read in full for everything touching `packages/agent/src` and `packages/coding-agent/src/core`, diffstat-only dispatch for CHANGELOG/docs/AGENTS.md and the release's lockfile and `package.json` churn. Both reconciliation passes ran: the unrestricted `--name-only` **detector** printed **47** paths, all classified and all status M, with no new top-level package, no file moved out of `packages/`, no new repo-root directory; the **accounting** sweep over `packages` minus the four structural exclusions reported **31 files, 493(+)/140(−)**, every file mapped to a verdict. None of the four never-diffstat-dispatch files appears in the range. **Review was independent of porting**: `pi-go-review` and `pi-parity-review` each ran as its own subagent over the integrated diff, both re-deriving the catalog themselves rather than trusting the porter's dumps. `pi-parity-review` filed **no divergences** and confirmed the reorder faithful hunk for hunk, but surfaced **one residual** in the load-bearing no-port claim — upstream's third new test does not hold here — now recorded as a deliberate divergence. `pi-go-review` filed **three should-fix findings, all three accepted and fixed in `3ad9fe5`** (untested snapshot-application path; an unreachable assertion plus a misdescribing test name; missing doc comments on two exported hooks whose contract changed and which upstream filed under Breaking Changes) along with five cheap cleanups. The first two were accepted **on reproduction rather than on assertion**: the reviewer demonstrated the apply path could be replaced with a discard while the whole repo suite stayed green, and the mutation output for the guard test showed the message being *discarded* (`user texts = [start second]`) rather than double-delivered, which is what proved the old headline assert unreachable. Base gate re-run green after the fixes. Differential harness re-pinned `56f3f33a9` -> `853a80d26` and moved to the 0.84.4 dist, green at **49/49**, exit 0; nothing under `ai/providers/openai*.go` changed, so the 6-scenario request diff was not triggered. 2026-08-28 (0 ports, no release) — per-commit diff triage of all 3 changes, judged from the real diff rather than the subject line; hunks read in full for `packages/ai/test` and `core/settings-manager.ts` (in or near ported surface), diffstat-only dispatch for tui/docs/CHANGELOG and the repo-root bundler script. Both reconciliation passes ran: the unrestricted `--name-only` **detector** printed **17** paths, all classified and all status M, with the top-level tree listing and the `packages/` directory set identical at both ends — no new top-level package, no file moved out of `packages/`, no new repo-root directory; the **accounting** sweep over `packages` minus the four structural exclusions reported **9 files, 155(+)/18(−)**, every file mapped to a verdict. **No review gates ran — there is no ported diff to review.** Instead each of the three verdicts was handed to an independent subagent instructed to REFUTE it, and **all three survived**, each attacked structurally rather than by category: `1defa151e` by grepping the port for every bundler/proxy identifier it touches (zero hits for `https-proxy-agent`, `esbuild`, `jiti`, `undici`; the port’s only proxy site is `ai/providers/retry.go:113` `http.ProxyFromEnvironment`, which supplies natively the capability the bundled Node agent was failing to supply, and the patched dynamic import turns out to live in `google-auth-library/gaxios` inside `node_modules`, not in pi source at all); `4e4949299` by confirming the port mirrors none of the new setting’s siblings and has no settings reader whatsoever (`tuiMode`, `fullscreenExitOutput`, `fullscreenScrollbar`: zero hits; no `Settings` struct, no `settings.json` reader), so there is nothing for a round-trip to lose; and `56f3f33a9` by confirming zero `turbo` and zero `routers/` hits in any `*.go`, and that `coding/resolve.go:47`’s fireworks default is the `accounts/fireworks/models/kimi-k2p6` id, byte-equal to upstream’s `model-resolver.ts:46` at the new sha — that being the `defaultModelPerProvider` table this ledger records as having caused a miss TWICE, so it was diffed rather than assumed. Base gate: `gofmt -l` clean, `go build ./...` and `go vet ./...` clean, `go test -race ./... -count=1` green across all **10** test-carrying packages (13 listed, 3 with no test files). Differential harness re-pinned `ccfe79ed2` -> `56f3f33a9` and green at **49/49**, exit 0; nothing under `ai/providers/openai*.go` changed — nothing in the port changed at all this cycle — so the 6-scenario request diff was not triggered. 2026-08-27 (4 ports, no release) — per-commit diff triage of all 13 changes, judged from the real diff rather than the subject line; hunks read in full for everything touching `packages/ai/src` and `packages/coding-agent/src/{core,utils}`, diffstat-only dispatch for tui/docs/CHANGELOG. Both reconciliation passes ran: the unrestricted `--name-only` **detector** printed **44** paths, all classified, no new top-level package, no file moved out of `packages/`, no new repo-root directory; the **accounting** sweep over `packages` minus the four structural exclusions reported **28 files, 523(+)/59(−)**, every file mapped to a verdict. Ports were executed by three worktree-isolated subagents grouped by Go file so no two could touch the same file; **review was independent of porting** — `pi-go-review` and `pi-parity-review` each ran as its own subagent over the integrated diff, and all 15 findings they filed were handed to further subagents instructed to REFUTE them, with 13 refuted by reproduction. Two n/a verdicts were attacked **structurally** rather than by category: `8fa7eebd2` by grepping the port for every scoped/enabled-model and persist-default identifier it touches (zero hits; `coding/session.go:325` `SetModel` has no persist option and no settings manager), and `e86823096`/`ccfe79ed2` by confirming the port carries no `TerminalCapabilit*`, no `UIPrompt*` and no extension-runner analogue at all. One process note worth carrying: two of the three port worktrees were created on a **five-day-stale base** (`241fdde`, the 2026-08-22 cycle); their target files were verified byte-identical across the two bases before cherry-picking, and the authoritative gate was re-run on `main` after integration. The third agent detected the staleness itself and re-branched from `main`. 2026-08-25 (6 ports, release) — per-commit diff triage of all 22 changes, judged from the real diff rather than the subject line; hunks read in full for everything touching `packages/ai/src`, `packages/coding-agent/src/core` and `packages/ai/scripts`, diffstat-only dispatch for CHANGELOG/.github/docs/examples/packaging. Both reconciliation passes ran: the unrestricted `--name-only` **detector** printed **82** paths, all classified, no new top-level package, no file moved out of `packages/`, no new repo-root directory (the one new file, `packages/ai/scripts/openrouter-reasoning-options.ts`, lands in the known catalog-generator home); the **accounting** sweep over `packages` minus the four structural exclusions reported **59 files, 1348(+)/157(−)**, every file mapped to a verdict. Ports were executed by three worktree-isolated subagents grouped by Go file so no two could touch the same file; **review was independent of porting** — `pi-go-review` and `pi-parity-review` each ran as its own subagent over the integrated diff, and every finding they filed was handed to a further subagent instructed to REFUTE it, with 5 of 9 refuted by reproduction. `cacb5917f` was checked to be a genuinely empty commit (tree hash equal to its parent's) rather than assumed from its diffstat. Base gate green before and after the review fixes; `ai/providers/openai*.go` changed, so the harness request diff was triggered and re-run at **49/49**. 2026-08-24 second check (0 ports) — per-commit diff triage of the single change, judged from the real diff rather than the subject line: hunks read in full for `src/main.ts` and `src/package-manager-cli.ts` (both in or near ported surface), diffstat-only dispatch for the docs line and the release-tooling script. Both mandatory reconciliation passes run under the corrected guard from `6d7e7f1`: the unrestricted `--name-only` **detector** printed **5** paths, all classified, with no new top-level package, no file moved out of `packages/`, and no new repo-root directory (`scripts/` predates the range); the **accounting** sweep over `packages` minus the four structural exclusions reported **3 files, 395(+)/5(−)**, every file mapped to a verdict. **No review gates ran and no subagents were spawned — there is no ported diff to review**; instead the `n/a` itself was attacked structurally, by grepping the port for every self-update/package-manager identifier the change touches (zero hits) and by confirming each of the three imported-but-unmodified helpers pre-exists at the old pin. Base gate: `gofmt -l` clean, `go build ./...` and `go vet ./...` clean, `go test -race ./... -count=1` green across all **10** test-carrying packages (13 listed, 3 with no test files). Differential harness re-pinned `a470b121b` -> `4af9d21d3` and green at **47/47**, exit 0; nothing under `ai/providers/openai*.go` changed — nothing in the port changed at all — so the 6-scenario request diff was not triggered. |

## The scope boundary (rewritten 2026-08-27 — read this before triaging anything)

**The port is pi's SDK half.** Scope is decided by two **exclusion tests** —
E1 and E3. A hunk is `n/a` if and only if one of them fires; **otherwise it is
IN scope**. (E2 was an exclusion test until 2026-08-27; the owner replaced it
with a consult — see below.)

*The tests are the authority.* Every path list — in this file, in the
`pi-triage` skill, anywhere — is a **derived convenience**. When a list and a
test disagree, the test wins and the list is fixed in the same commit. This
replaces the old "Deliberately not ported" enumeration, which was a path list
pretending to be a boundary: 12 of 16 scope rulings in this ledger resolved a
path INTO scope, every reversal ran OUT→IN, and twice the cause was upstream
simply moving a file across the line.

**Reachability is evidence, not a test.** "Published, independently reachable
surface" (`src/index.ts`, the `exports` map) is what tells you something is SDK
surface *at all* — it is the deciding fact in 2026-08-07, 2026-08-11 and
2026-08-18, and it is why the default is IN. But it argues in **one direction
only**: it can never override E1–E3. pi exports its entire application from
`packages/coding-agent/src/index.ts`, `InteractiveMode` and the TUI components
included; being root-exported does not make something SDK surface.

### E1 — host surface

Fires when the file belongs to **the application pi ships around the SDK**
rather than to the SDK itself: its entry points and its modes of operation
(interactive, rpc, print — *headless or not*; `modes/rpc` is a JSON-stdio
embedding surface and is still host), its terminal UI, its operator
configuration, its packaging and installer — **or when its only consumers are
host surface.**

That last clause is load-bearing and is what the older rulings were reaching for
without naming: it covers host machinery that *populates* an already-ported SDK
seam (the seam is ported and latent; the populator is not — 2026-06-17) and
constructs that exist to serve the TUI (2026-07-30). **It fires only when EVERY
consumer is host surface. A mixed consumer set does NOT fire it — read the
hunk.** That restriction is the whole safety margin: without it the clause
would swallow files the port has already partly ported.

Derived list (today): `packages/tui`, `packages/evals`, `packages/ai/src/cli.ts`,
and under `packages/coding-agent/src` — `modes/**`, `cli/**`, `main.ts`,
`core/extensions/**`, `core/export-html/**`, `core/settings-manager.ts`,
`core/prompt-templates.ts`, `core/agent-session-runtime.ts` and the session
reload / `/new` lifecycle, `core/model-registry.ts`,
`core/resolve-config-value.ts`, `core/radius.ts`, `core/resource-loader.ts`
source-info accessors, `migrations.ts`, `bun/**`, `package-manager-cli.ts`.

**A commit whose excluded content is entirely on that list is triaged on its
non-host hunks alone, and never escalates on account of the host half.** No
hunk-reading is owed; the diffstat is enough. This is the single highest-value
rule here — roughly half of all mixed commits per 90 days are mixed *only*
because of host content, and ~65% of mixed commits end in `n/a` anyway.

#### Files that must be split by HUNK — never diffstat-dispatched

These are **partly ported**. The diffstat shortcut above does not apply to them,
and treating them as host surface would manufacture exactly the miss this
ledger has already recorded twice:

| file | ported half | host half |
|---|---|---|
| `core/model-resolver.ts` | `defaultModelPerProvider` and the fallback-model construction → `coding/resolve.go:18-21,381` | the host resolution flow. **The table has been the source of a MISS twice** (2026-07-26 `24e5cc04`, 2026-08-04 `c1019d920`); the standing rule to diff `defaultModelPerProvider` on every touch is still in force |
| `core/package-manager.ts` | project/user skill discovery → `coding/resources.go` (it is the `if (projectTrusted)` site the 2026-08-27 trust ruling cites) | npm install / self-update, no Go surface |
| `core/trust-manager.ts` | the trust decision and gate (2026-08-27) | the prompt, selector and persistent store |
| `packages/telemetry/src/index.ts` | the runtime `Span` contract (2026-08-06) | the schema half — E3 |

Three riders:
- **E1 applies to a HUNK, not only a file.** A hunk whose added lines serve only
  host surface is host, even inside a file that is not. The converse also holds
  and is the 2026-07-21 lesson: a hunk inside a host file is `port` when its Go
  home is a shared or generic function.
- The port deliberately has **no mode layer**. `cmd/pi` is a hand-rolled SDK
  CLI, not a port of pi's modes; never map a mode change onto it.
- **A gate is in scope exactly when the port has the thing it gates**
  (2026-08-27 project-trust ruling). Host code that only *asks* the user stays
  out; the decision and the gate come in the moment the port grows the consumer.

### E2 — a third-party dependency is a CONSULT, not an exclusion

**Owner ruling, 2026-08-27 (evening): if it is part of the pi SDK, the port
supports it. Full stop. If porting it would need an outside Go dependency, that
is a question to bring to the owner — not a reason to decide `n/a` on your
own.**

This replaces the earlier form of E2, which made "would this add a `require`
line?" an automatic exclusion and ruled bedrock and codex permanently out. That
was the port drawing its own boundary rather than following pi's, which is the
thing this whole rewrite exists to stop. **E2 is therefore no longer an
exclusion test** — the boundary has two: E1 and E3.

The standing preference still holds and is worth stating: the port's runtime
module today depends only on the Go standard library and `golang.org/x/*`, and
staying there is a real asset (trivial cross-compilation, no cgo, a tiny supply
chain). **As of 2026-08-31 this is enforced mechanically, not by this
paragraph** — `internal/policy/deps_test.go` fails when the build reaches a
module outside its allowlist, or when any non-stdlib package carries cgo files.
It runs under the base gate's existing `go test ./...`, which is the only thing
this repo runs every cycle (there is no CI). Adding a module means editing that
allowlist, which is the consult, made unskippable. So the rule is:

> A `packages/ai/src` adapter is **IN scope**, like everything else the SDK
> publishes. If a faithful Go port of it would require a third-party module for
> the transport, the wire encoding, or the credential chain, **open a Scope
> queue row flagged `CONSULT` and put the specific question to the owner** —
> which module, how large, what it buys, and whether hand-rolling is credible.
> Do not port it silently, and do not `n/a` it.

Riders that survive unchanged: a hunk inside such an adapter is still `port`
when its Go home is a shared or generic function (the 2026-07-21 lesson); and a
transparent JSON/SSE-over-HTTPS vendor wrapper like `@google/genai` raises no
dependency question at all, since the port already hand-rolls what the `openai`
and `@anthropic-ai/sdk` packages do.

**Consequence: `amazon-bedrock` and `openai-codex` are back IN scope**, as queue
entries with open consults. The catalog's 125 bedrock + codex models stop being
"accepted debt that fails at stream time" and become work with a question in
front of it.

#### The two consults, stated so they can be answered

- **Bedrock** — needs AWS SigV4 request signing, the `vnd.amazon.eventstream`
  binary frame format, and the AWS credential chain. **A dependency may not
  actually be required:** SigV4 is a documented HMAC-SHA256 derivation (~300 Go
  lines) and eventstream is a documented length-prefixed binary format with
  CRC32 (~200 lines); both are hand-rollable against stdlib `crypto/*` and
  `hash/crc32`. The alternative is `aws-sdk-go-v2`, which is large and pulls a
  wide tree. **Question: hand-roll, or take the SDK?** Note pi's own bedrock
  wire is authored by `aws-sdk-js`, so there is no pi-authored byte sequence to
  be faithful to — parity here means "AWS accepts it", which argues for
  hand-rolling and testing against the documented format.
- **Codex** — needs a WebSocket transport and zstd decompression. zstd is not
  credibly hand-rollable; `klauspost/compress` is the standard Go answer.
  WebSocket could be hand-rolled (RFC 6455) or taken from
  `nhooyr.io/websocket`. **Question: take `klauspost/compress` (and probably a
  WebSocket module), or leave codex queued until someone wants it?**

### E3 — no Go representation

Fires when the construct is a **TypeScript type-level artifact with no runtime
behavior**, so there is nothing to port. Today this is exactly one thing: the
schema/type-inference half of `packages/telemetry` (`defineTelemetrySchema`,
`Infer*`, `SchemaTelemetrySpan` — 2026-08-06). It is root-exported, which is
why it needs E3 rather than reachability: exported *types* are not runtime
surface. It shares `src/index.ts` with the ported runtime half, so **this one
must be split by HUNK** — and it is the standing example of an exclusion that
is not expressible as a path.

Go generics do not change the answer: pi's schema half exists to make
`Infer<typeof schema>` produce a compile-time span type, which is a TypeScript
type-system feature with no runtime residue at all.

### What this leaves genuinely out

| out | test | note |
|---|---|---|
| `packages/tui` + `coding-agent/src/modes/**` | E1 | 37,257 LOC of implementation — **69% of the 53,706 LOC this table excludes**. TUI alone is 16,966 LOC (34,144 with upstream's own test suite) across 101 commits in 90 days, yet the sole cause of only **4** mixed commits, one triage lesson and zero defects: the cheapest exclusion the port has. `modes/**` is the other 20,291 LOC and 56 sole-cause mixed commits. The pair is ~95 port-cycles, and there is no oracle — a request body is a value you can diff, an escape-byte stream interpreted by a terminal is not. |
| extensions runtime (`core/extensions/**`) | E1 | 4,121 LOC. Runs operator-authored JS in the host process. |
| `cli/**`, `main.ts`, `bun/**`, `package-manager-cli.ts`, `migrations.ts`, `packages/ai/src/cli.ts` | E1 | 4,303 LOC plus `ai/src/cli.ts`. Entry points, installer/self-update, config migrations. `ai/src/cli.ts` was carved out by the old skill table ("except … cli") and keeps its exclusion here on E1 rather than on a list entry. |
| `core/settings-manager.ts`, `core/prompt-templates.ts` | E1 | 1,645 LOC. Operator configuration. |
| `core/agent-session-runtime.ts` + the session reload / `/new` lifecycle | E1 | Drives the interactive and print session lifecycle; its consumers are modes. |
| `core/model-registry.ts`, `core/resolve-config-value.ts`, `core/radius.ts` | E1 (only-consumers clause) | 445 LOC. The host-side machinery that POPULATES ported SDK seams — `StreamOptions.Env` (2026-06-17) and the Radius host wiring (2026-07-14). The seams stay ported and latent. **`core/model-resolver.ts` (782 LOC) is deliberately NOT here** — it is partly ported and is in the split-by-HUNK table above. |
| `core/export-html/**` | E1 | 746 LOC of TypeScript plus 4,275 lines of bundled template/asset files. Renders a session to an HTML file for a human. |
| `core/resource-loader.ts` source-info accessors | E1 (only-consumers clause) | 2026-07-30. Sole upstream consumer is `modes/interactive`. |
| `packages/evals` | E1 | 1,277 LOC. Upstream's own eval harness. |
| — | — | **Not out, but not homed:** `packages/coding-agent/src/server/**` (161 LOC, `create-harness.ts`) was ruled IN on 2026-08-07 and is the harness factory; it is covered by Scope queue entry 8, not by this table. |
| — | — | **No longer out:** `amazon-bedrock` and `openai-codex` are IN scope with open dependency consults (see E2). They are queue entries, not exclusions. |
| the telemetry schema/type-inference half | E3 | Split by HUNK — shares `src/index.ts` with the ported runtime half. |
| the trust prompt, selector and store — `cli/project-trust.ts`, `modes/interactive/components/trust-selector.ts`, and `core/trust-manager.ts`'s persistence | E1 | Asking the user is host surface. The trust *decision and gate* are ported (2026-08-27), so **`core/trust-manager.ts` splits by HUNK** — see the table above. |
| docs, CHANGELOG, CI, `.github/`, `.pi/`, examples, per-package `package.json` version bumps, repo-root `scripts/` | — | Always noise; not a scope question. |

**That table is exhaustive.** If a hunk is not covered by a row above and no
test fires on it, it is **in scope** — that is the default, and it is the
direction this boundary has always moved. If you believe something belongs out
and no test reaches it, that is a `decide`: it means a test needs changing.

### Rulings (answers to `decide` escalations — triage must not re-ask)

- **2026-08-31 — the Bedrock consult is answered TAKE `aws-sdk-go-v2`, CONFINED
  TO A SUBMODULE: `github.com/sky-valley/pi/providers/bedrock`, own tag series,
  root module untouched.** This is the first ruling to admit a non-`golang.org/x`
  module into the port at all, and it is admitted *outside* the root module.

  **Why not hand-roll.** The 2026-08-27 framing ("~500 Go lines, stdlib only")
  priced the signer and nothing else. A trimmed SigV4 plus an eventstream decoder
  is plausibly 600–900 Go lines — the SDK's own are `signer/v4` 1,197 +
  `signer/internal/v4` 528 + `eventstream` 1,353 = 3,078 — and that is the cheap
  third of the job. The expensive part is credentials, which pi does not
  implement: it contains **no credential-resolution code**, delegating entirely
  to the SDK default chain. `packages/coding-agent/docs/providers.md` advertises
  `AWS_PROFILE`, ECS task roles and IRSA as supported Bedrock credential sources,
  and `AWS_ENDPOINT_URL_BEDROCK_RUNTIME` likewise appears in zero lines of pi
  source. All of it is free only because the chain is linked in. The Go
  equivalent of that surface is `config` 7,297 + `credentials` 2,970 + `imds`
  1,752 = **12,019 LOC**, and the sharpest edge is not EKS but **SSO** — the
  `~/.aws/sso/cache` token store and OIDC refresh, i.e. the enterprise laptop
  path. Lambda alone would survive a hand-roll, because it injects static env
  keys. So hand-rolling does not buy independence; it buys a documented-feature
  regression.

  **Why the root module still must not carry it.** This follows upstream's own
  engineering rather than departing from it. Upstream *took* the AWS SDK
  (`@aws-sdk/client-bedrock-runtime` + `@smithy/node-http-handler`, declared in
  `packages/ai`), then deliberately quarantined it: `bedrock-converse-stream.lazy.ts`
  loads the implementation through a **variable specifier** so bundlers cannot
  follow the import into the Node-only SDK, with a `setBedrockProviderModule`
  override for the Bun binary build. Take the dependency; do not let it reach
  everyone. Go has no bundler, so the module boundary is the mechanism that
  expresses the same intent.

  **Verified in a three-module prototype, not argued:** root `go list -m all`
  contains **0** `github.com/aws/` modules and `go.sum` stays at **4 lines**; the
  submodule builds and carries 6 aws modules; `internal/policy/deps_test.go`
  stays **green** on the root, so this shape needs no exception to the gate
  landed the same day. Marginal binary cost, measured on the real `cmd/pi` at
  linux/amd64 `-s -w`: **10,674,338 → 13,951,138 bytes, +3.12 MiB (1.31×)** with
  the full credential chain — and only for consumers who opt in. The earlier
  "5.5× blowup" figure was measured against hello-world, which links neither
  `net/http` nor `crypto/tls`; pi already pays for both.

  **Scope note:** roughly **1,021 of the ~1,459 upstream lines is business logic**
  — message conversion, thinking payloads, GovCloud display omission, the
  byte-stable error prefixes `agent-session` string-matches, diagnostics, stop
  reasons. That work is identical under every option, so the dependency question
  governs about a third of the entry and must not be allowed to hold the other
  two thirds hostage.

  **Known risk — the submodule rots silently, because there is no CI.** Only the
  root is built each cycle, so a change to `ai`'s `ApiProvider`, `StreamOptions`
  or the assistant event-stream contract leaves root green while nothing compiles
  `providers/bedrock`. Tells: `go build ./...` inside the submodule fails at a
  pin advance, `go list -m -u github.com/sky-valley/pi` there reports the root
  requirement behind the newest root tag, or a cycle tags `v0.8x.NN` with no
  matching `providers/bedrock/v0.8x.NN`. Mitigation follows the enforcement
  correction already on record — not a `CGO_ENABLED=0` build, which is
  structurally useless, but a per-module build plus a dependency-set assertion
  (zero `^github.com/aws/` in root, non-zero only in the submodule), extending
  `internal/policy/deps_test.go` when the submodule actually exists. Absent CI it
  is a release-checklist step run at each pin advance.

- **2026-08-31 — the session-backends driver consult is answered NEITHER, NOT
  YET: entry 7 stays queued and the root module takes no SQLite driver.** The
  question as posed ("`modernc.org/sqlite` or `mattn/go-sqlite3`?") rested on a
  premise that had gone stale. Verified at the pin, not from the 08-27 note:
  `git grep -l better-sqlite3 853a80d26` exits 1 — the string is absent from the
  whole upstream tree — and `packages/session-backends/sqlite-node/package.json`
  lists exactly two dependencies, `@earendil-works/pi-ai` and
  `@earendil-works/pi-agent-core`, **no sqlite dependency of any kind**. The
  backend is Node's builtin `node:sqlite` gated by `engines.node >=22.19.0`, and
  upstream split the package out so the core would not pull native sqlite in.
  Taking either Go driver would therefore add a native dependency in order to
  match an upstream that has none. The second and stronger reason: entry 7 is a
  backend *implementation* of the 8c `SessionStorage`/`SessionRepo` seam, and 8c
  is recorded here as "NOT STARTED, owner-gated … do not fund until the owner
  wants pluggable durability as a product feature". Choosing a driver now picks
  flooring for an unapproved storey — nothing in the port imports session
  storage today (`SessionRepo`/`SessionStorage` appear in no Go file; the lone
  grep hit is a substring of `TestRemoteSessionReportsSubscriberFailures`).

  **The merits are nonetheless settled, so 8c can be answered in one line.**
  Established by EXECUTING a probe against both drivers, not by reading docs:
  `modernc.org/sqlite` v1.57.0 (SQLite 3.53.3) passes every exotic construct the
  upstream schema uses — FTS5 external-content with `content_rowid`, the
  `trigram remove_diacritics 1` tokenizer, the `ai`/`ad`/`au` sync triggers,
  `VALUES('rebuild')`, `bm25()` + `MATCH`, conditional `UPSERT … RETURNING`,
  nested `SAVEPOINT`/`ROLLBACK TO`, `WITHOUT ROWID`, WAL and `BEGIN IMMEDIATE` —
  **with no build tags**. `mattn/go-sqlite3` needs cgo *and* a non-default
  `-tags sqlite_fts5`, failing at runtime with `no such module: fts5` without
  it. If 8c ever opens, the driver is `modernc.org/sqlite`, and it belongs in a
  `github.com/sky-valley/pi/backends/sqlite` submodule with its own tag series —
  never in the root module. Measured cost of putting it in the root: non-stdlib
  modules **2 → 11**, binary **13.9 → ~21.9 MB**, and the loss of the
  dragonfly, solaris, illumos and aix targets that build clean today.

  **Correction to the enforcement story, worth more than the ruling itself.**
  The no-cgo stance in E2 is prose with no mechanical check, and the obvious
  check does not work: adding `mattn/go-sqlite3` and building with
  `CGO_ENABLED=0` **succeeds**, because mattn ships a build-tagged stub — the
  binary then fails at runtime with "requires cgo to work. This is a stub". A CI
  rule of the form "`CGO_ENABLED=0 go build ./...` must pass" is therefore
  structurally incapable of catching the regression it would exist to prevent.
  The gate that works asserts the dependency set (`go list -deps ./...`, or a
  `go.mod` allowlist). **A second, subtler trap was found while implementing
  it:** the natural fallback — "assert no package has `CgoFiles`" — is blind for
  the same reason if it runs with cgo off, because the toolchain excludes cgo
  files by build constraint before `go list` sees them. Measured on
  `mattn/go-sqlite3 v1.14.50`: `cgofiles=0` under `CGO_ENABLED=0`, `cgofiles=10`
  under `CGO_ENABLED=1`. Any cgo check must therefore force `CGO_ENABLED=1`; a
  dependency-set assertion is immune either way. **Implemented 2026-08-31** as
  `internal/policy/deps_test.go` — two independent tests (allowlist, cgo),
  both proven to fail against a real violation before being landed, the cgo one
  against third-party *and* first-party `import "C"`. Package count 13 → 14,
  test-carrying 10 → 11.

  **Known cost of this answer, and the tells that reopen it.** A driver-free
  seam is not free: porting the interface with zero in-tree implementers repeats
  the "ported seam with no implementers" failure already recorded against entry
  1 (OAuth) and `server.Service`. If 8c is funded, land the in-memory backend
  from `session/memory.ts` in the same slice so the seam has one real
  implementer, and keep `modernc.org/sqlite` behind a build-tagged **test-only**
  import so conformance runs against a real database without the root module
  requiring it. Reopen this ruling on either of two upstream tells, neither
  present at the pin: a **second** backend sibling appearing under
  `packages/session-backends/` (a `bun-sqlite` or `pg`), or `packages/coding-agent`
  gaining its first real import of `SqliteSessionRepository` — at `853a80d26`
  it has none. Either means durability has become a product feature.

- **2026-08-27 — scope is decided by EXCLUSION TESTS, not by a path list; the three
  tests are in "The scope boundary" above and they are authoritative over
  every list in this repo.** This is the structural answer to a failure the
  ledger had recorded six separate times without naming: the boundary was drawn
  on paths, and upstream refactors move files across paths. Evidence, all
  already in this file — host→SDK relocations (2026-06-23, 06-25, 07-17), a
  type's home moving into a brand-new package (08-06), an exclusion becoming a
  published npm artifact (08-07), a carve-out outliving the directory it named
  (08-25), and the 2026-08-22 sweep rewrite independently concluding that "some
  real exclusions are unexpressible as paths at all." **Twelve of sixteen scope
  rulings resolved a path INTO scope**. The other four (2026-06-12 trust,
  2026-06-24 null headers, 2026-07-30 resource-loader, 2026-08-05 harness) all
  DECLINED to bring something in; **not one ruling has ever moved surface that
  was already in scope back OUT**, and two of those four were themselves
  reversed IN later — null headers after 41 days, the harness after two. So a
  list of exclusions was a lagging indicator being maintained as though it were
  a contract. Consequence for triage: when a list and a predicate
  disagree, the predicate wins and the list is fixed in the same commit — and a
  hunk no predicate excludes is IN scope by default. This ruling does not move
  any single boundary by itself; it changes what a boundary IS.

- **2026-08-27 — the host seam replaces the TUI / modes / extensions / cli /
  settings / packaging entries as a single structural exclusion, and a commit
  spanning it never escalates on account of its host half.** These were seven
  separate list entries describing one thing: pi's host application. Stated as a
  seam it is self-maintaining — a new host file is host surface on the day it
  appears, with nothing to add to a list. **This is a restatement, not a
  boundary change**: nothing moves in or out, and the TUI's exclusion is
  reaffirmed on its merits (16,966 LOC of implementation, 34,144 with
  upstream's own tests; 101 commits touching it in 90 days yet
  the sole cause of only 4 mixed commits; zero defects; and no oracle — a
  request body is a value you can diff, an escape-byte stream interpreted by a
  terminal is not). `modes/**` is a further 20,291 LOC and 56 sole-cause mixed
  commits; the pair costs ~95 port-cycles to take on. The cost it removes is
  per-commit hunk-reading that ended in `n/a` ~65% of the time.

- **2026-08-27 — `go.mod` decides adapter scope. Azure, Mistral, Vertex, Radius
  (+ `radius-config`) and the whole OAuth token-acquisition tree are IN scope
  and queued. Bedrock and Codex are OUT for as long as the go.mod policy holds.** This **supersedes the 2026-07-14 ruling** insofar as that ruling
  put the Radius provider and its host wiring out: `providers/radius-config.ts`
  is reachable through pi-ai's `"./providers/*"` exports wildcard, i.e.
  published SDK surface, which is the exact fact that ruled the Cloudflare
  binding IN on 2026-08-11 — and the 2026-08-22 tripwire had already recorded
  that "daylight in the ruling text," noting it was spared a `decide` only
  because `packages/ai` had no delta that range. It is closed now rather than
  when it fires. The OAuth tree is the highest-value item and is not really "an
  excluded provider" at all: `OAuthAuth.Refresh`/`ToAuth` and `LazyOAuth` are
  **already-ported seams with zero implementers**, so Anthropic Pro/Max,
  Copilot, OpenRouter, Kimi and xAI subscriptions are unreachable from Go
  today — and closing that needs no dependency the port does not already have.
  The host-side *env-override population* machinery (2026-06-17) and the Radius
  HOST wiring (`core/model-registry.ts`, `core/resolve-config-value.ts`,
  `core/radius.ts`) stay OUT under **E1's only-consumers clause**, not under the
  reachability default, and are named in E1's derived list and in the out-table.
  `core/model-resolver.ts` is the exception and must NOT be dispatched from a
  diffstat: it is partly ported (`defaultModelPerProvider` → `coding/resolve.go`)
  and sits in the split-by-HUNK table instead. The queue entry covers the
  `packages/ai` half only.

- **2026-08-27 — image generation is IN scope and queued.** It was a
  founding-day list entry with **no recorded reasoning**, which is precisely why
  it kept regenerating the question: a triager had nothing to apply.
  `packages/ai/src/index.ts` carries `export * from "./images-models.ts"`, so
  `ImagesModels` / `createImagesModels` / `createImagesProvider` sit on the
  **root** export of `@earendil-works/pi-ai` — strictly stronger reachability
  than the `./api/*` wildcard that decided 2026-08-11. Under predicate 2 it was
  never out. `image-models.generated.ts` joins the catalog-regen surface;
  `scripts/generate-image-models.ts` becomes `port-but-CATALOG-ONLY` like its
  sibling.

- **2026-08-27 — project trust: a gate is in scope exactly when the port has the
  thing it gates, and any new project-local read must ship WITH its gate.**
  This **supersedes the 2026-06-12 ruling in part**. That ruling rested on three
  criteria, the third being "verified not to change behavior of ported surface";
  that criterion stopped holding — not through an upstream commit but because
  **the port grew the consumer**. `coding/session.go` called `LoadSkills(cwd)`
  unconditionally, and `coding/resources.go` scanned `<cwd>/.pi/skills`, a
  directory upstream reaches only inside `if (projectTrusted)`
  (`package-manager.ts:2417`) and answers *untrusted* for a UI-less host
  (`project-trust.ts`). The port answered *trusted*: a parity inversion on a
  security default, in public MIT code, letting a hostile repository author part
  of the system prompt. Fixed in `873e35a` with the gate defaulting untrusted
  and `SessionOptions.TrustProject` as the opt-in.
  **The load-bearing half of this ruling is the corollary**, and it is a rule
  that would have caught the regression on the day it shipped: *whenever the
  port adds any new project-local read — anything under `<cwd>/.pi/**` or an
  ancestor `.agents/**` — that same change must carry the gate. A new reader
  shipping ungated is a defect, not a scope question.* The trust *prompt*,
  selector and store stay out under E1 (asking the user is host surface); the decision and the gate are in. Ancestor `<project>/.agents/skills`
  discovery remains unimplemented (2026-08-18) and `TrustProject` does not
  enable it — when it is implemented, it ships gated.

- **2026-08-27 (evening) — if it is part of the pi SDK, the port supports it; a
  third-party dependency is a CONSULT, not a reason to say no.** Owner ruling,
  overturning the `go.mod`-decides form of E2 written earlier the same day.
  That rule made "would this add a `require` line?" an automatic exclusion and
  put `amazon-bedrock` and `openai-codex` permanently out — which was **the port
  drawing its own boundary instead of following pi's**, the exact failure this
  rewrite exists to stop. A dependency question is a real question, but it is
  the owner's to answer per case, not a standing no.
  **Effects:** the boundary now has TWO exclusion tests (E1, E3), not three.
  Bedrock and codex return to scope as queue entries with open consults, each
  stated concretely enough to answer — bedrock may need no dependency at all
  (SigV4 and `vnd.amazon.eventstream` are both hand-rollable against stdlib),
  codex realistically needs `klauspost/compress` for zstd. The session-backends
  sqlite question (entry 7) becomes the same kind of consult rather than a
  precondition for the entry surviving. The stdlib-plus-`golang.org/x/*`
  preference stays as a stated preference and a thing to weigh, not a gate.

- **2026-08-27 (evening) — the agent harness is FUNDED. The owner ruled "harness
  is in"; it is no longer deferred and the pin's asterisk is retired on
  completion.** Supersedes the "decision owed" ruling below in its operative
  half: the question is closed, only the schedule remains. Scope queue entry 8
  becomes an active drain rather than a parked item, and triage carries its
  backlog as ordinary queued deltas.
  **Scoping fact established when the work started, and it changes the shape of
  the job rather than the decision:** `packages/agent/src/harness` is NOT a
  relocation of code the port already has. It is a **second, parallel agent
  implementation** — upstream's reusable "durable harness" for embedders — that
  coexists with `packages/coding-agent/src/core`, the thing the Go port actually
  ported. Both exist at `ccfe79ed2` with genuinely different implementations
  (compaction 848 vs 1012 lines; bash tool 161 vs 544). The coupling is narrow:
  the ported coding-agent consumes exactly **9** harness symbols through the
  package root — `createBashTool`, `createEditTool`, `BashToolOptions`,
  `convertToLlm`, `buildSessionContext`, `CompactionResult`, `SessionStats`,
  `COMPACTION_SUMMARY_PREFIX`, `createBranchSummaryMessage` — every one of which
  the port has already absorbed into `coding/` while porting coding-agent.
  **So the open question is no longer "port it?" but "in what SHAPE?"** — see
  the Scope queue entry, which records the three options and the default.

- **2026-08-27 — the agent harness is the one item still owed an owner
  decision, and "IN-but-deferred" is not an answer.** Recorded here so it stops
  being re-derived every cycle. Status is unchanged — IN scope since 2026-08-07,
  backlog now 12 — but the state itself is the most expensive one available: it
  generates triage work every cycle and produces no parity, and the pin has
  carried a qualifying asterisk for 12+ consecutive cycles. Sized at ~24
  port-cycles (~10.3k lines of harness + search src, ~5.7k lines of upstream
  test, plus ~1.6 upstream commits/day of new obligation). **The owner owes one
  call: fund it, or rule it out under E1.** Until then it stays at the
  bottom of the scope queue and the pin keeps its asterisk. Triage must not
  escalate it again; it must simply carry the backlog.

- **[AMENDED 2026-08-27] 2026-08-25 — the `pi-triage` skill's `port` definition is the single list of
  in-scope trees, and this ledger is authoritative over it.** Closing an item
  carried OPEN since 2026-08-22. The skill's verdict-assigning text named three
  trees while these Rulings had put five more in scope (protocol, client, server,
  telemetry, session-backends), and a second, correct map sat further down the
  same file — so the skill held both the wrong answer and the right one. It never
  bit only because those trees happened not to change. **Not a boundary change:**
  no tree was added or removed, the definition was made to match rulings already
  given. The duplicate map is gone; there is now ONE table, in the verdict rules.
  If the table and this file ever disagree again, this file wins and the table
  gets fixed. Two carve-outs from the 2026-08-01 ruling are formally retired as
  dead text: `server/src/legacy/**` (deleted upstream in `05bf9df65`) and
  `server/src/testing/**` (ported as `server/internal/servertest/`).


  > **AMENDED 2026-08-27.** The mechanism this ruling installed — one
  > authoritative LIST — is superseded by the three exclusion tests in "The
  > scope boundary". Its *substance* stands: this ledger is authoritative over
  > the skill, and there must never be two copies of anything. Read "single
  > list" as "single source of truth", which is now the tests.
- **2026-08-19 — an upstream REVERT is ported like any other change, even when
  it removes exported Go API the port shipped days earlier.** (re: `3a0b9a3ee`
  reverting `2509b5c03`, which the port had landed the previous day as
  `0f0b461` + `b548506`.) Triage must not treat "we just built this" as a
  reason to keep it, and must not escalate the API removal as a `decide`.
  **The deciding fact is that the port's public surface is downstream of pi's,
  not a commitment of its own.** `BuildProviderContext`,
  `Agent.BuildProviderContext` and `ContextPipeline` existed for exactly one
  reason — to mirror `2509b5c03` — so once upstream withdrew it, keeping them
  would have *created* a divergence rather than avoided one. The pi-sync hard
  rule "anything that would change the public Go API → escalate" aims at the
  port diverging from pi under its own steam; following pi back is the rule's
  purpose, not its exception.
  **Bounded by the release line.** This was safe to do silently because the API
  was never released: it landed after `v0.84.18` and this cycle crossed no
  release tag, so no consumer could have depended on it. **If a future revert
  targets surface that a cut tag already published, that IS a `decide`** — the
  question stops being "what does pi do" and becomes "what did we promise" —
  and the escalation should offer keeping the Go symbol as a deprecated
  no-op-forwarding shim until the next minor.
  **Verify the un-port is exactly the revert, no more.** The 2026-08-19 cycle
  proved it by byte-comparing `agent/loop.go` and `agent/agent.go` against
  `b79c9e6` (the pre-port parent) — identical — and by diffing `go doc -all`
  across the change to confirm the exported delta was exactly the three
  identifiers upstream removed, with no remaining callers anywhere including
  `examples/` and `cmd/`. Do that comparison every time; a revert is the one
  shape of port where "restore the old file" is checkable exactly.
  **Test coverage does not revert with the code.** The un-port deleted a test
  that pinned four properties; the replacement initially covered two, and the
  gap was invisible to the suite. When a revert removes a test, re-establish
  what it pinned through whatever path survives before shipping.

- **[AMENDED 2026-08-27] 2026-08-18 — `.agents/skills` discovery: the USER directory is IN scope
  (port it, as backlog); the project-ancestor directories stay `n/a` under the
  trust ruling.** (re: `5e11f6586` "load nested markdown skills", which only
  widens `collectSkillEntries` for mode `"agents"`.) Deliberately NOT escalated
  as a `decide`: the owner's **standing formula** (2026-08-11) already answers
  it, and re-asking would be re-litigating. Full pi SDK functionality as
  represented in Go; deciding fact = **published, independently reachable
  surface**.
  **The user half is in scope.** `collectAutoSkillEntries(join(homedir(),
  ".agents","skills"), "agents")` is unconditional — no gate, no runtime
  dependency, plain filesystem enumeration — and it feeds the same `loadSkills`
  whose Go counterpart (`coding/resources.go` `loadSkillsFromDir`) is already
  ported and already feeds the ported system prompt. That makes this a **parity
  gap inside a ported function**, not a boundary question.
  **The project half is not.** `collectAncestorAgentsSkillDirs(cwd)` is gated on
  `settingsManager.isProjectTrusted()`, and project trust is on the non-port
  list (2026-06-12) — the same fact that decided the resource-loader accessors
  (2026-07-30).
  **Recorded as a pre-existing gap, not this cycle's work.** Agents-mode
  discovery predates the port: it landed upstream in `39cbf47e4` (2026-02-20),
  before the port started (2026-06-08). So `5e11f6586` itself is `n/a` — a no-op
  for the `"pi"` mode the port implements — and the user-dir port is backlog.
  When it lands, implement HEAD semantics directly: in mode `"agents"` a NESTED
  `.md` counts and a ROOT-level `.md` does not (the mirror image of `"pi"` mode),
  `SKILL.md` still stops recursion, and the agents directory carries its own
  baseDir (`~/.agents`) for relative-path resolution.
  **Landed the same day in `d7f4daf`**, implementing HEAD semantics directly:
  `~/.agents/skills` is scanned last (upstream's own position among its four
  discovery calls), in agents mode, and since names dedup first-wins it can only
  ADD names no pi directory already resolved. Three mutations verified red.
  Triage from here: `package-manager.ts` skill-discovery commits are judged
  against BOTH modes now; the ancestor `<project>/.agents/skills` dirs stay `n/a`
  while project trust is unported.


  > **AMENDED 2026-08-27 — the trust legs below are DEAD; do not cite them.**
  > Every sentence in this ruling that grounds the project-ancestor `n/a` on
  > "project trust is on the non-port list" or "while project trust is
  > unported" is false since `873e35a`: the gate IS ported
  > (`SessionOptions.TrustProject`, `LoadSkillsWithTrust`).
  > **Corrected disposition:** ancestor `<project>/.agents/skills` discovery is
  > IN scope and has no Go home, which under the rewritten boundary is a
  > **Scope queue** matter, not an `n/a` — it is folded into queue entry 3's
  > neighbourhood as a one-file item and ships GATED when built, per the
  > 2026-08-27 project-trust corollary. Marking it `n/a` would re-create exactly
  > the "in scope, no home, called n/a" pattern the queue exists to end.
- **2026-08-11 — the Cloudflare AI-binding gateway transport is IN scope; port it
  latent. A transport is not out of scope for being runtime-specific.** (re:
  `230029078` "feat(ai): AI Gateway transport over the Cloudflare AI binding",
  #7901.) Triaged `decide` because it looked like a new boundary class: a
  transport whose only input is a JS-runtime object. Owner call (noam): apply the
  **standing formula**, don't re-litigate — full pi SDK functionality as
  represented in Go, close faith to the source architecture, leaning into Go's
  idioms and upsides.
  **The deciding fact is the same one as 2026-08-07: published, independently
  reachable surface.** `cloudflare-gateway-binding.ts` sits in
  `packages/ai/src/api/`, the ported layer under the 2026-07-14 pi-messages
  ruling, and pi-ai's `package.json` `exports` map carries a **`"./api/*"`
  wildcard subpath** — so `@earendil-works/pi-ai/api/cloudflare-gateway-binding`
  is publicly importable by any consumer the moment it ships. This is **not** the
  unreachable-module situation recorded against `create-harness.ts`: that
  counter-fact rested on absence from `src/index.ts` **and** from the `exports`
  map; here only the first holds, and the second is what governs reachability.
  **The latency objection was already settled, twice.** No Go host can produce an
  `env.AI` binding, because that is a Cloudflare Workers JS-runtime object and Go
  does not target that runtime. That is the same objection overruled for the
  retry classifier (2026-06-25, no consumer at all), `StreamOptions.Env`
  (2026-06-17) and `ProviderRequestOptions.TelemetryContext` (2026-08-06).
  **Recorded counter-fact, so this is not re-litigated from one side:** those
  precedents were latent pending a *host wiring something that exists*; this one
  is latent pending a *runtime the port does not target*, so unlike `Env` and
  `TelemetryContext` it may never activate in Go at all. Judged not to matter:
  scope here follows the published SDK surface, not the odds of activation.
  **Scope as ported** (`ai/providers/cloudflare_gateway_binding.go`): the
  transport behavior whole — origin+prefix guard, POST-only, provider/endpoint
  split on the URL-normalized path, query string retained on the endpoint, JSON
  body probe, header lowercasing with `content-length`/`host`/
  `cf-aig-authorization` stripped, binding response returned untouched — plus the
  `CloudflareGatewayBindingAuthSentinel` constant and a Go `AIGatewayBinding`
  interface standing in for pi's structural `env.AI` type. **Excluded**: pi's
  fetch-input reconciliation (see the deliberate divergence recorded in the
  2026-08-11 cycle section) — it exists only to reconcile a `Request` input with a
  `RequestInit` override, and an `*http.Request` has one of each.
  Triage from here: commits to
  `packages/ai/src/api/cloudflare-gateway-binding.ts` are `port`, and triage must
  **not** re-escalate a transport merely for depending on a runtime Go does not
  target — that axis is settled. A genuinely new question would be a transport
  that changes the `ai.HTTPDoer` seam itself.

- **2026-08-07 — the agent-harness exclusion is REOPENED. The harness tree and
  the session backends are IN scope, but are DEFERRED, not ported.** (re:
  `6fb2d766a` "feat(coding-agent): add configurable Harness factory", with
  riders `9ab91fb93` — which refactored ported `core/tools/*.ts` purely to feed
  it — and `5cd46ee11`, the docs commit that deleted the protective clause.)
  Both halves of the 2026-08-05 standing tripwire fired in one cycle:
  (1) `packages/coding-agent/src/server/create-harness.ts` now imports
  `AgentHarness`/`AgentHarnessOptions`/`AgentHarnessTool`/`HarnessTool` from
  `@earendil-works/pi-agent-core` **and** wires them to surface we already port
  (`core/system-prompt.ts` plus the new `*SystemPromptContribution` exports);
  (2) the blanket constraint *"No work package may modify coding-agent source,
  tests, RPC, UI, or package metadata except I0's telemetry build-order
  integration"* is **gone** from `harness-v2.md` — what remains is scoped to
  Track O alone. The 2026-08-06 entry's "watch whether the carve-out widens"
  is exactly what happened; it did not widen, it was removed.
  **The deciding fact was the npm registry, not the source tree.** Owner call
  (noam), on a conditional — hold if the harness is not an operational part of
  the release, reopen if the release surfaces it as a real thing.
  `@earendil-works/pi-session-backend-sqlite-node` has exactly two published
  versions, **0.84.0 and 0.84.1, both inside this delta**, and its predecessor
  `packages/storage` was never published under any name. v0.84.0 is therefore
  the first release in pi's history to ship a harness-v2 component as an
  independently installable artifact, and `pi-agent-core@0.84.1` publicly
  exports the harness API. Verdict: **(c) reopen fully.**
  **Recorded counter-fact, so this is not re-litigated from one side:**
  `create-harness.ts` is reachable from nothing — not re-exported from
  `src/index.ts`, absent from the package `exports` map (only `.`,
  `./rpc-entry`, `./client`), and imported by no runtime path — so the shipped
  `pi` binary still does not run on the harness, and *"Coding-agent migration"*
  is still a written Non-goal. That argued for holding; it was judged to be
  about coding-agent **migration**, which was never the trigger this ruling set.
  **Scope, and the deferral.** Newly in scope: `packages/agent/src/harness/**`,
  `packages/session-backends/**`, `packages/coding-agent/src/server/**`. That is
  ~12.4k lines of TypeScript (41 harness src files ≈ 9,919 lines; sqlite backend
  ≈ 2,284 src + 1,665 test; the factory 159) which **predates this delta and is
  not ported**. It is a separate project, not a daily-sync item, and the pin
  advanced without it — so from 2026-08-07 the pin means *"everything in scope
  is reviewed and ported **except the harness tree, tracked here**"*, not
  "everything in scope is ported". Do not read a pin advance as harness parity
  until this paragraph is deleted.
  **Standing caution**: upstream's own compatibility policy still says *"All
  other formats and APIs in `packages/agent/src/harness` and
  `packages/session-backends/sqlite-node` may break"*, and R0 remains the only
  checked box in the work-package list — `session/session.ts` was rewritten
  twice in a single earlier cycle. Port against the published npm surface, which
  is the part carrying a stability claim, rather than head-of-churn.
  Triage from here: harness/session-backend commits are `port` (deferred —
  add them to the backlog, do not silently mark them `n/a`), and the 2026-08-05
  tripwires are retired.

- **2026-08-06 — the telemetry package is IN scope; port its runtime tracing
  surface** (re: `04d6447f7` "feat: add typed telemetry contracts" +
  `6b461b75b` "feat: extract telemetry package", escalated because the only
  in-scope residue was `ProviderRequestOptions.telemetryContext`, whose type
  lives in the brand-new `@earendil-works/pi-telemetry` workspace package — a
  package this ledger had never tracked). Owner call (noam): **port the
  telemetry as well.** Scope as ported: a new top-level Go `telemetry` package
  carrying the RUNTIME contract — `Context`/`Span` (callback-scoped
  `StartSpan`; `AddEvent`/`SetAttributes`/`SetStatus`),
  `SpanOptions`/`SpanAttributes`/`SpanStatus`, and the shared noop context —
  plus `ai.ProviderRequestOptions.TelemetryContext` (latent until a host
  passes a real tracer; the `StreamOptions.Env` precedent). The
  schema-definition / type-inference half of upstream's package
  (`defineTelemetrySchema`, `TelemetryAttributeDefinition` and friends,
  `SchemaTelemetrySpan`, all `Infer*` helpers) is **excluded**: compile-time
  TS machinery whose only consumers are the unported agent harness
  (`agent/src/harness/telemetry.ts`) and its docs generator. Going forward:
  commits to `packages/telemetry`'s runtime surface are `port`; commits to its
  schema/typing half or to harness span catalogs stay `n/a` under the
  2026-08-05 harness ruling.

- **[SUPERSEDED 2026-08-07 and 2026-08-27] 2026-08-05 — the agent-harness exclusion HOLDS even though harness-v2 is now
  `packages/agent`'s promoted public API. No cutover, no date; watch a tripwire
  instead.** (re: `44289550a` "feat(agent): promote durable harness API", with
  riders `f119b01cb`, `79cc1ef00`, `591f22a61`, `7bdeeb8f9`, `1e95e16b6`,
  `651d5d6a5`, `a80008b96`.) Triaged `decide` because it looked like the
  2026-08-01 test firing — a non-ported area becoming load-bearing for the SDK:
  `agent/src/index.ts` now re-exports `harness/session/` and `harness/result.ts`
  wholesale while ~9,000 lines of the pre-v2 harness and its tests are DELETED,
  not deprecated (`session/{jsonl-repo,memory-repo,repository}.ts`,
  `array-session-index.ts`, `keyed-operation-queue.ts`, `agent-harness.test.ts`
  −1280, `repo.test.ts` −645); `04133eb01` deleted 2,503 lines of superseded
  design docs leaving `harness-v2.md` the sole plan of record; `a80008b96` +
  `79cc1ef00` productized the backend layer (`packages/storage` →
  `packages/session-backends`, with its own tests). That is a replacement, not
  an experiment.
  **It still does not obligate us, on upstream's own written terms.**
  `harness-v2.md` states as an explicit **non-goal**: *"Coding-agent migration.
  `packages/coding-agent` remains on its current runtime and is not modified by
  this implementation plan."* §21 hardens it into a per-work-package constraint
  — *"No work package may modify coding-agent source, tests, RPC, UI, or package
  metadata"* — and the plan's final package (O4) lists "`packages/coding-agent/**`
  is unchanged" as an acceptance criterion. The plan ENDS before the thing that
  would force our hand.
  **Verified, not assumed**: every `@earendil-works/pi-agent-core` import across
  `coding-agent/src` is agent-loop primitives only (`Agent`, `AgentEvent`,
  `AgentMessage`, `AgentState`, `AgentTool`, `PrepareNextTurnContext`,
  `ThinkingLevel`, `uuidv7`) — **zero harness symbols**. Confirming detail:
  `44289550a` deleted the old session-repository exports from `packages/agent`'s
  public index and nothing in coding-agent broke, because nothing imported them.
  The harness is a parallel tree that duplicates coding-agent core (the tell:
  upstream currently has TWO `skills.ts` and TWO `prompt-templates.ts`, one per
  tree — and the Go port's `coding/resources.go` ports the **coding-agent** one).
  **The compatibility policy cuts in our favour.** Verbatim: *"Old coding-agent
  v3 JSONL sessions must open and restore idle. This is the only
  backward-compatibility requirement. All other formats and APIs in
  `packages/agent/src/harness` and `packages/session-backends/sqlite-node` may
  break. We do not write migrations, schema versioning, or conversion paths for
  anything else."* Coding-agent v3 JSONL is exactly what `coding/session_store.go`
  implements, byte-golden with a fixture corpus — so our session format is not a
  casualty of harness-v2, it is the one fixed point harness-v2 has committed to
  reading. Work package J2 is a v3 decoder; when it lands it is a free
  cross-implementation parity oracle for our format. And "all other formats may
  break" is upstream's own warrant for not tracking head-of-churn: `651d5d6a5`
  is titled "**partial** harness v2/json backend", `session/session.ts` was
  rewritten twice in this one cycle, and R0 is the only checked box in §21.
  **Standing tripwire — check both every sync; the doc moves before the code
  does.** (1) `git grep -l "pi-agent-core" -- packages/coding-agent/src | xargs
  grep -l "AgentHarness\|harness/session\|LaneId"` — any hit means the harness
  has become load-bearing for surface we already port, and this ruling reopens.
  (2) Diff `harness-v2.md`'s Non-goals and the §21 constraint: if *"Coding-agent
  migration"* leaves the non-goal list, or §21 stops saying
  `packages/coding-agent/**` is untouched, that is the announcement — expect it
  weeks before the first import.
  Until one fires, `packages/agent/src/harness/**`,
  `packages/session-backends/**`, and `packages/agent/docs/harness-v2.md` remain
  `n/a` and triage must not re-escalate them.


  > **SUPERSEDED 2026-08-07** (reopened IN scope) and again **2026-08-27**
  > (queued, entries 7 and 8). Its closing instruction that the harness and
  > session-backends "remain `n/a`" is DEAD TEXT — do not act on it. Retained
  > only for the reasoning trail.
- **2026-08-04 — null-`ProviderHeaders` suppression is now IN scope; port it**
  (re: `a24fb9e96` "preserve auth header deletion markers", #7539). Owner call
  (noam): **port it.** This closes the 2026-06-24 deferral on its own terms —
  that ruling declined `Record<string,string|null>` null-suppression explicitly
  *"Revisit only if a consumer needs to suppress a default header"*, and this
  commit is that consumer arriving: upstream's host stops stripping nulls and
  passes `ProviderHeaders` through intact, with a cloudflare-compat test. The
  hunk itself is in `coding-agent/src/core/model-registry.ts` (host), so it is
  not `port` by path — it is `port` because it fires the named trigger. The
  resulting public Go API change (`StreamOptions.Headers` / `Model.Headers` can
  no longer be a plain `map[string]string`) is sanctioned under the 2026-07-17
  clause that upstream-driven public Go API breaks are allowed. **Supersedes
  item 1 of the 2026-06-24 divergence list** — Go's conditional-skip workaround
  for cloudflare-ai-gateway's `Authorization` suppression should collapse into
  the real null-marker mechanism rather than sitting beside it. Future commits
  to null-header plumbing in `packages/ai/src` are `port`.

- **2026-08-04 — upstream `DRAFT:` commits are ported like any other change
  (INTERIM)** (re: `382aa641c` "DRAFT: add openai background mode responses",
  the deferred-response capability class: `DeferredHandle`, new `StopReason`
  value `"deferred"`, `fetchDeferred`/`cancelDeferred` on
  `Provider`/`Models`/`ProviderStreams`, `SimpleStreamOptions.deferred`,
  `lazyApi` capability flags, `providers/faux.ts`). Owner call (noam): **port
  it for now**, pending an answer to the open question below. Rationale: on
  *scope* the 2026-08-01 ruling already answers this — it is `packages/ai/src`
  SDK surface, i.e. exactly the "full functionality of the pi SDK as
  represented in Go" class. The only thing the DRAFT label raised was
  *stability*, which is a different axis and is not on its own a reason to
  hold. Triage must not re-escalate a change **solely** because its subject
  says `DRAFT:` — judge it on scope like anything else.
  **OPEN QUESTION (unresolved — when answered, replace this ruling): when is a
  draft no longer a draft?** Upstream's `DRAFT:` is a free-text subject prefix
  with no lifecycle behind it: the commit is already on `main` (not a branch or
  a PR left open), carries no tag, and nothing marks the transition when it
  stabilizes — a later commit may simply edit the surface without ever removing
  the word. So there is no upstream signal to wait *for*, which is part of why
  holding was the weaker option. Candidate signals if we want a rule later:
  first release tag that ships the surface; the surface surviving N sync cycles
  unmoved; or upstream's own `server/protocol.ts` dropping its
  `"Deferred assistant messages are not supported by protocol v1"` throw.
  **Standing caution while this is interim**: the new `StopReason` value
  `"deferred"` enters the **session/message format** (a byte-golden surface),
  and upstream itself refuses to serialize it over protocol v1. Port it, but
  treat any golden movement it causes as requiring an explicit parity note
  rather than a silent regen, and expect churn — this surface is not frozen
  upstream (same class of caution as `protocol/src/schemas.ts` under the
  2026-08-01 ruling).

- **2026-08-01 — the remote-session stack (protocol + client + server core) is
  IN scope** (re: `06a1ceb8d` "coding-agent remote client controller", with
  riders `5a38a1c12` (`packages/client`), `7d5fc9499` (unix transport),
  `73b24639f` (`packages/server` core), `03eba409c` (server/protocol
  invariants)). Owner call (noam): **full functionality of the pi SDK as
  represented in Go — close faith to the source architecture while leaning into
  Go's idioms and upsides.** Judgement: `pi-coding-agent/client` is a published,
  programmatic entrypoint (new package subpath `./client`), i.e. exactly the SDK
  use case the port exists to serve — not the TUI/host machinery the boundary
  excludes. **All server core is in**, so a Go consumer can serve as well as
  connect. **Port**: `packages/protocol/src` (cbor encoder/decoder/options,
  `codec.ts`, `framing.ts`, `schemas.ts`) → new `protocol/` package;
  `packages/client/src` (`client`/`connection`/`state`/`session-handle`/
  `unix`/`promise`/`errors`/`transport`/`types`) → new `client/` package;
  `packages/server/src` **minus `legacy/`** (`server`/`protocol`/`sessions`/
  `snapshots`/`listener`/`connection`/`errors`/`types` + `transports/unix`) →
  new `server/` package; `coding-agent/src/client/{remote-session,transcript}.ts`
  → `coding/remotesession.go` + `coding/transcript.go`.
  **Not ported**: `packages/server/src/legacy/**` (`radius.ts` OAuth acquisition,
  `supervisor.ts`, `rpc-process.ts`, `cli.ts`, `serve.ts`, `config.ts`,
  `storage.ts`, the `ipc/` pair) — process/CLI/OAuth-acquisition machinery,
  excluded under the 2026-07-14 Radius ruling and the standing host-wiring
  boundary. `packages/server/src/testing/**` is upstream's own fake backend;
  port only if the Go server tests want the same shape.
  **The agent-harness exclusion is UNAFFECTED**: dependency edges were traced at
  ruling time — `server/src/{server,protocol,sessions,types}.ts` import only
  `@earendil-works/pi-protocol` + `node:crypto`, with the agent runtime behind an
  injected `Backend` interface (`server/src/types.ts`), and `remote-session.ts`
  imports only `pi-client` + `pi-protocol` + its own `transcript.ts`. Nothing in
  this stack reaches `AgentHarness`, so the "no `harness/` tree in the Go port"
  ruling stands and the session-store refactors stay `n/a`.
  **New golden class — the wire.** `protocol/` is byte-golden in a way no prior
  ported surface is: CBOR encoding and frame layout are observable to a *peer*,
  so a Go implementation must be byte-compatible with a Node one or interop
  fails. This needs its own fixture corpus (encode/decode round-trips +
  cross-implementation frame vectors), not the usual request-body diff. Note
  `protocol/src/schemas.ts` moved twice in its first cycle (`73b24639f`,
  `03eba409c`) — it is not frozen upstream; re-check it every sync.
  Future commits to `packages/{protocol,client}/src`, `packages/server/src`
  (non-`legacy/`), and `coding-agent/src/client/` are `port` under this ruling;
  commits confined to `server/src/legacy/**` are `n/a`.

- **[PARTLY SUPERSEDED 2026-08-27] 2026-07-17 — the model-runtime facade is ported SDK-scoped** (re:
  `ff28097a` "merge model runtime facade" + rider `bd9e09db` "expose dynamic
  provider refresh"). Owner call (noam): maximum fidelity to source via
  maximum Go idioms, and the port supports **only the SDK and everything it
  depends on**. Judgement: the facade's `packages/ai/src` deltas are **port**
  — Provider `refreshModels(context)`/`filterModels`, `ModelsStore` (+
  `ModelsStoreEntry{models, checkedAt}`), `Models`
  `refresh(options)→{aborted,errors}` / `checkAuth` / `getAvailable` /
  `login`/`logout` / overloaded `getAuth(+overrides)` / stream transforms,
  the auth-type overhaul (`AuthInteraction` rename, provider-scoped
  `ApiKeyAuth.resolve`, optional `check`, `CredentialStore.list`,
  `Credential.availableModelIds` typed), github-copilot `filterModels`,
  `radius`→`RADIUS_API_KEY`. Upstream-driven public Go API breaks are
  sanctioned under this ruling (pre-1.0, upstream broke the same APIs).
  **Not ported**: the OAuth reorg (`utils/oauth`→`auth/oauth` — acquisition),
  `cli.ts`, extension-oauth compat types, the host `coding-agent/src/core`
  restructuring (`model-runtime`/`model-config`/`provider-composer`/
  `models-store`/`runtime-credentials`/`remote-catalog-provider` + the
  `model-registry` collapse and `ModelRegistry`→`ModelRuntime` renames), the
  radius provider (2026-07-14 ruling stands), and `bun-oauth.ts`. The
  compat-routing and inline-cloudflare divergences (2026-06-24) and the
  no-lazyStream divergence stand — pi's new `compat.ts` routing converges
  toward Go's raw-provider+env-key path (cloudflare exception ≡ Go's inline
  `resolveCloudflareBaseURL`). Future commits to the facade surface in
  `packages/ai/src` are `port`; commits only to the host runtime files above
  are `n/a`.


  > **PARTLY SUPERSEDED 2026-08-27.** Its "Not ported" list has moved: the
  > **OAuth reorg / acquisition tree is now IN scope and queued** (entry 1),
  > and `radius`/`radius-config` with it (E2). `cli.ts` and the host
  > `coding-agent/src/core` restructuring (`model-runtime`, `model-config`,
  > `provider-composer`, `models-store`, `runtime-credentials`,
  > `remote-catalog-provider`) stay out — `cli.ts` under E1 directly, the `core`
  > cluster under E1's only-consumers clause where every consumer is host.
- **[PARTLY SUPERSEDED 2026-08-27] 2026-07-14 — port the `pi-messages` provider API; leave Radius OAuth + host
  wiring out** (re: `961fa6c1` "feat(ai): add Radius gateway support"). The
  commit adds a new first-class provider API `pi-messages` — a generic
  POST-`{model,context,options}` + SSE wire protocol — plus a `radius` provider
  that authenticates via OAuth with a dynamic (non-catalog) model list. Owner
  call (noam): stay loyal to canon + lean into Go idioms, and we currently ship
  **only the SDK**. Judgement: **port the `pi-messages` API** (`ai/providers/pi_messages.go`,
  new `Api` const `APIPiMessages`, `radius`→`PI_GATEWAY_API_KEY` env-key —
  renamed upstream to `RADIUS_API_KEY` in `ff28097a`, ported in the 07-17
  cycle — `RegisterPiMessages`) — it lives in `packages/ai/src/api/` (the ported layer,
  not an excluded provider), is a **generic protocol usable by any custom
  provider** via `"api":"pi-messages"`, and authenticates by plain API key
  (`PI_GATEWAY_API_KEY` through the already-ported env-key path) with **no OAuth
  dependency in the streaming path**. **Not ported** (stay on the existing
  boundary): `packages/ai/src/utils/oauth/radius.ts` (OAuth token acquisition),
  `coding-agent/src/core/radius.ts`, the `model-registry.ts` `oauth:"radius"`
  wiring, `model-resolver.ts` (`radius:"auto"`), `provider-display-names.ts` —
  all host-side / OAuth-acquisition. `radius` is a dynamic OAuth-only provider
  with **no static catalog entry**, so no catalog model is added; the built-in
  `radius` provider stays latent until a host wires OAuth. Future commits to
  `packages/ai/src/api/pi-messages.ts` are `port`; commits only to the Radius
  OAuth/host machinery are `n/a` under this ruling.


  > **PARTLY SUPERSEDED 2026-08-27.** The Radius *provider* + `radius-config`
  > are IN scope and queued (E2). The *host wiring* stays out under E1's
  > only-consumers clause. See the 2026-08-27 go.mod ruling.
- **2026-06-25 — adopt the relocated SDK retry classifier as a latent export**
  (re: `371adcf3` "retry explicit provider retry errors", #6019). Upstream moved
  `isRetryableAssistantError` out of host code (`coding-agent/src/core/agent-session.ts`)
  into the in-scope SDK package (`packages/ai/src/utils/retry.ts`) and added three
  new retryable patterns (`"you can retry your request"`, `"try your request
  again"`, `"please retry your request"`). Owner call (noam): **port it** — mirror
  pi's SDK structure even though the Go port currently has **no consumer** (Go's
  `MaxRetries`/`ai/providers/retry.go` is provider-HTTP-level backoff *within* one
  request; the assistant-turn auto-retry loop that calls this classifier lives in
  the unported agent-session-runtime). Consistent with the 2026-06-23 "maximum
  parity with the SDK package" adopt ruling: port the classifier + its
  non-retryable/retryable pattern sets as idiomatic Go in package `ai` (latent
  until an auto-retry loop consumes it). The `isContextOverflow` pre-check stays
  on the (unported) consumer side, as upstream keeps it in `agent-session.ts`.
  Future commits to `packages/ai/src/utils/retry.ts` (new patterns, etc.) are
  `port` under this ruling. This makes `371adcf3` a `port`, no longer a `decide`.

- **2026-06-24 — models-runtime migration completed under the "globals stay as
  compat" divergence** (re: the `129eb460` "complete models runtime migration"
  consolidation). The 06-23 adopt ruling stands (maximum parity + Go idioms);
  this records WHERE the Go port deliberately diverges in *structure* while
  staying byte-faithful through its actual consumer path. The Go coding agent
  streams via the **compat globals** (`ai.Stream` → `withEnvAPIKey` → raw
  provider), NOT the Models runtime — the on-record "globals stay as compat"
  divergence. Three pieces of `129eb460` are therefore **not transliterated**;
  the 2026-06-24 parity review confirmed each is observably byte-identical
  through the compat path (they "compensate precisely"):
  1. **`ProviderHeaders` null-suppression** (`Record<string,string|null>`) — NOT
     ported. `Headers` stays `map[string]string`. Porting it would change the
     public Go API (`StreamOptions.Headers`/`Model.Headers`) for a **latent**
     capability: zero 0.80.2 catalog models set a null header, and pi's only
     real null use (cloudflare-ai-gateway suppressing `Authorization`) is
     already handled in Go by a conditional skip, not a null marker. Revisit
     only if a consumer needs to suppress a default header.
  2. **Cloudflare base-URL placeholder resolution + `cf-aig-authorization`** —
     kept **inline** in the openai providers (`resolveCloudflareBaseURL`) rather
     than relocated to a `cloudflare-auth` layer. Verified byte-identical
     baseURL + headers vs pi's relocated version for gateway + workers-ai.
  3. **compat `shouldUseBuiltinModels` routing** — NOT ported. pi routes catalog
     models through the Models runtime (empty credential store + env-only auth +
     cloudflare-auth baseURL); Go's "raw provider + `withEnvAPIKey` + inline
     cloudflare" path resolves to the same bytes. Divergences (1)+(3) cancel.
  In scope and ported this cycle (all faithful): `ef231c49` (request-scoped auth
  overrides — the named `auth/resolve.ts` boundary edge), `49fbe683`
  (`api-key`→`api_key`, credential `metadata`→`env`), `cd95c274` (OpenAI
  Responses terminal-event requirement + compaction zero-usage guard),
  `6184307c` (anthropic compat now from catalog — byte-safe; the 0.80.2 catalog
  carries the fields the removed auto-detect synthesized), `129eb460`'s
  `getClientApiKey` "unused" sentinel + vercel routing ungate (byte-safe for the
  catalog). The catalog-data reorg (per-provider `*.models.ts` + huggingface
  registration provider) landed via the 0.80.2 regen. Future commits to the
  null-`ProviderHeaders` plumbing or compat-routing in `packages/ai/src` re-open
  this — re-judge against the compat-path equivalence above.

- **2026-06-23 — adopt the SDK-side model-registry / env-resolution overhaul**
  (re: the `732bb161` "Merge model-registry into main" merge + rider
  `2cbce395` "pass provider-resolved env to APIs"). Owner call: **maximum
  parity with the source + maximum use of Go idioms** — port the new
  `packages/ai/src/auth/` resolution layer (`context`/`credential-store`/
  `helpers`/`resolve`/`types`), give `AuthResult` its `env` (`ProviderEnv`),
  and populate `StreamOptions.Env` from `resolution.env` merged with explicit
  `options.env` inside the Go `models.ts`/`Stream` resolution path. This
  **supersedes the latency clause** of the 2026-06-17 ruling: `StreamOptions.Env`
  no longer stays latent-until-a-host-sets-it — the ported SDK resolution now
  populates it itself, as upstream moved that machinery out of host-side
  coding-agent and into `packages/ai/src`. The earlier-named "host-side
  population machinery (resolve-config-value, model-registry, settings)" is
  re-scoped accordingly: the part that now lives in `packages/ai/src` (the
  model-registry + auth resolution) is **in scope**; whatever remains in
  `coding-agent` host wiring stays out. Idiomatic Go, not transliterated TS
  (the `pi-go-review` bar applies). Consequences: `732bb161` and `2cbce395`
  are `port` (no longer `decide`); the catalog *data* reorg
  (`models.generated.ts` → per-provider `*.models.ts`, new providers) is still
  deferred to the next release regen since 0.79.10 was not re-published; new
  providers are catalog-data/registration and land with that regen unless they
  introduce genuinely new provider *behavior* (judge per provider at port
  time). `8eeaa2bc` (compat scoped-env API-key injection) remains a `port`
  under the 2026-06-17 plumbing clause and now composes with the populated
  `Env`. The `auth/resolve.ts` credential→env resolution is the new boundary
  edge: future commits to it in `packages/ai/src` are `port`.

- **[RE-DERIVED 2026-08-27] 2026-06-17 — provider-scoped env overrides ported faithfully** (re:
  `7f29e7a3`). Owner call: maximum parity. `StreamOptions.Env`
  (`map[string]string`) is consulted ahead of `os.Getenv` (helper
  `getProviderEnvValue`: non-empty scoped value wins, empty falls through —
  pi's `||`) for the two ported consumers pi touches: `PI_CACHE_RETENTION` and
  Cloudflare base-URL placeholders, across anthropic/openai-completions/
  openai-responses. pi's `getBunSandboxEnvValue` `/proc/self/environ` fallback
  is DELIBERATELY OMITTED — it works around oven-sh/bun#27802 (Bun compiled
  binaries expose an empty `process.env` in Linux sandboxes), a runtime defect
  Go does not have. The host-side population machinery stays unported (field
  latent, matching pi SDK consumers that don't populate it). Future commits to
  the env-override *plumbing in ported providers* are `port`; commits only to
  the unported host-side population are `n/a`.


  > **RE-DERIVED 2026-08-27.** Verdict unchanged; the host-side population
  > machinery stays out under E1's only-consumers clause, and those files are
  > now named explicitly in E1's derived list rather than left to prose.
- **2026-06-16 — provider-attribution ported faithfully** (port-it ruling); SDK
  sends pi's default attribution headers (http-referer/x-title/...) on the
  providers pi does.

- **[PARTLY SUPERSEDED 2026-08-27] 2026-06-12 — project trust stays excluded** (re: `718215bd`, `d8aef0fe`,
  and the wider upstream trust push). Criteria set by the owner: not an SDK
  use case (host apps control what loads), postponable (purely additive
  subsystem), and verified not to change behavior of ported surface (the only
  ported-adjacent diff was a behavior-neutral refactor inside the unported
  extension resource-loader; `skills.ts` untouched). Future trust commits are
  `n/a` under this ruling UNLESS they change behavior of surface we ported —
  that re-escalates.


  > **PARTLY SUPERSEDED 2026-08-27.** The trust DECISION and GATE are ported
  > (`873e35a`); only the prompt/selector/store stay out under E1. This
  > ruling's third criterion — "verified not to change behavior of ported
  > surface" — stopped holding when the port grew the consumer.
- **2026-07-30 — catalog data lives only in the npm build, never in upstream
  git.** The per-provider model data (`dist/providers/data/*.json`) is generated
  at publish time by `scripts/generate-models.ts`; it is **not committed**
  upstream. A `git diff <pin>..origin/main` sweep therefore can NEVER detect a
  catalog change, and `packages/ai/src/models.generated.ts` is a small
  aggregator whose own bytes rarely move. This cycle's triage initially and
  wrongly concluded "no regen needed" from exactly that git sweep, and a
  file-level `cmp` of the two builds' `dist/models.generated.js` **confirmed the
  error** — that file is byte-identical (4,373 B) across 0.82.0 and 0.83.0 while
  the real data differs by 20,653 B. **Rule: whenever a release tag is crossed,
  decide the regen by executing `JSON.stringify(MODELS)` against both npm
  builds and comparing — never by reading git, and never by `cmp`-ing
  `models.generated.js`.** A generator-only (`scripts/generate-models.ts`) hunk
  in the delta is a positive signal that the next release WILL move the catalog.

- **[RE-DERIVED 2026-08-27] 2026-07-30 — `core/resource-loader.ts` source-info additions stay `n/a`**
  (re: `bff5ab71`, extending the `66eead65` precedent). The port has no
  `ResourceLoader` object, no `SYSTEM.md`/`APPEND_SYSTEM.md` discovery, and no
  path-or-inline prompt source — `BuildSystemPromptOptions.AppendSystemPrompt`
  is a plain literal string. The new `getSystemPromptSource()` /
  `getAppendSystemPromptSources()` accessors have exactly one upstream consumer
  (`modes/interactive/interactive-mode.ts`, unported TUI), and their backing
  discovery is gated on `isProjectTrusted()`, which is itself on the non-port
  list (2026-06-12). Porting them would mean inventing an entire exported
  subsystem to serve no ported consumer. Future `resource-loader.ts` commits are
  `n/a` UNLESS the port grows a resource-loader analog.


  > **RE-DERIVED 2026-08-27.** Verdict unchanged, ground restated: it is E1's
  > only-consumers clause (sole upstream consumer is `modes/interactive`). The
  > second leg below — "gated on `isProjectTrusted()`, itself on the non-port
  > list" — is DEAD: the trust gate is ported as of `873e35a`. Do not cite it.
## Scope queue (IN scope, not yet built — opened 2026-08-27)

The 2026-08-27 rewrite moved surface into scope faster than it can be ported.
Those items live here, and this queue is why that is **not** the
"IN-but-deferred" trap the harness fell into: each entry has an order, a size
measured at `ccfe79ed2`, and a place to accumulate deltas, and the queue is
reported in every cycle's ledger entry like the catalog-only queue is.

**How triage handles a queued tree.** An upstream change touching one is
`port-but-QUEUED`: verdict `port`, no Go commit this cycle, delta appended to
the entry below. That is the `port-but-CATALOG-ONLY` pattern generalized — these
trees have **no Go base yet**, so a hunk-sized port of a file that does not
exist is not a thing that can land. Once an entry's base port ships, its queued
deltas land with it, the entry closes, and that tree triages as ordinary `port`.

**How to OPEN an entry — this is the part the harness never had.** If a change
is IN scope (no exclusion test fires) and the port has no Go home for it, do
**not** escalate and do **not** mark it `n/a`. Open a new row here: name the
tree, size it at the pin, and put it at the bottom. Opening a row is a normal
triage action requiring no owner decision, because the scope question was
already answered by the tests — only the scheduling is open. Say so in the
cycle's ledger entry so the row is visible the day it appears.

Drain order is value-first, not size-first. Sizes are upstream source lines at
`ccfe79ed2`, implementation only.

| # | entry | size | est. | why here | queued deltas |
|---|---|---|---|---|---|
| 1 | **OAuth token acquisition** (`auth/oauth/**`, `oauth.ts`) | 2,983 LOC / 12 files (2,439 excluding `openai-codex.ts`, whose adapter is OUT under E2) | ~3 | Highest value in the queue. `OAuthAuth.Refresh`/`ToAuth` and `LazyOAuth` are **ported seams with zero implementers** — Anthropic Pro/Max, Copilot, OpenRouter, Kimi and xAI subscription auth are simply unreachable from Go. Needs nothing beyond `crypto/rand`, `crypto/sha256`, `net/http`, `os/exec`. Carries `auth/oauth/radius.ts` (403), so land it with entry 2. | — |
| 2 | **Radius provider** (`providers/radius.ts`, `providers/radius-config.ts`) | **178 LOC** | ~0.5 | Smallest entry by an order of magnitude, and it closes the **2026-08-22 tripwire**, which is loaded and will fire on the next behavioral commit: `radius-config.ts` is reachable through pi-ai's `"./providers/*"` exports wildcard, i.e. published SDK surface the 2026-07-14 ruling did not name. Only ~8 first-parent commits in 90 days touch anything Radius-named, so this is bought for the tripwire, not for churn relief. | — |
| 3 | **Image generation** | 1,125 LOC in `packages/ai/src` (of which **684 is `image-models.generated.ts`** → catalog surface, not hand-ported) + 228 LOC of `openrouter-images` api/provider | ~2 | Root-export surface (2026-08-27 ruling). Auth, catalog machinery and helpers already exist port-side. `scripts/generate-image-models.ts` is `port-but-CATALOG-ONLY`. | **1** — `5ce4afbd9` (2026-08-29: `image-models.generated.ts` +75 lines, three openrouter image models — `meta/muse-image`, `recraft/recraft-v4-styles` and one sibling. Catalog surface with no port-side file: `ai/` embeds `models_catalog.json` only, so this lands when the entry's base port ships) |
| 4 | **Azure OpenAI responses** | 364 LOC | ~1 | JSON over HTTPS, header auth. | — |
| 5 | **Google Vertex** | 710 LOC | ~2 | IN under E2's transparent-wrapper rider (`@google/genai`). ADC is the risk — scope it to the credential paths Go reaches with stdlib. | — |
| 6 | **Mistral conversations** | 963 LOC | ~1.5 | JSON over HTTPS, header auth. | **1** — `6c87d9a02` (2026-08-29: merge indexed Mistral tool-call chunks — `consumeChatStream`'s `toolBlocksByKey` is rekeyed from the composite `` `${callId}:${index}` `` string to `toolCall.index ?? callId`, so chunks of one call that arrive with differing ids still merge. No Go base: `ai/providers/` has no mistral adapter, only registry-level references in `ai/envkeys.go`, `ai/types.go` and `coding/resolve.go`) |
| 7 | **session-backends** (`packages/session-backends/**`) | 2,389 LOC src | ~4 | IN scope since 2026-08-07, never given a home until now — the skill has been telling triage to append deltas to an entry that did not exist. **CONSULT ANSWERED (2026-08-31) — NEITHER DRIVER, NOT YET.** The 2026-08-27 premise was stale: at `853a80d26` `better-sqlite3` appears **nowhere** in upstream (`git grep -l better-sqlite3 853a80d26` exits 1) and `packages/session-backends/sqlite-node/package.json` declares **zero** sqlite dependencies — the backend is Node's *builtin* `node:sqlite` behind `engines.node >=22.19.0`. The consult was therefore asking which native dependency to take in order to match an upstream that deliberately took none. Entry 7 is a backend for the **8c** `SessionStorage`/`SessionRepo` seam, which is owner-gated and unfunded, so the entry stays queued and the root module takes no driver. Merits settled for whenever 8c opens: `modernc.org/sqlite`. See the 2026-08-31 ruling. | **4** — `e7fb8eb2a`, plus the sqlite halves of `7bdb16c28`, `a4453b79b`, `b75be04d9` (reassigned from entry 8, 2026-08-27) |
| 9 | **Bedrock adapter** | 1,459 LOC | ~3 + consult | **CONSULT ANSWERED (2026-08-31) — TAKE `aws-sdk-go-v2` (with `config`), CONFINED TO A `github.com/sky-valley/pi/providers/bedrock` SUBMODULE** with its own tag series; the root module's graph stays at its two `golang.org/x/*` requires. Back in scope 2026-08-27. The 2026-08-27 note that parity "favours hand-rolling" is **withdrawn**: it is true that there is no pi-authored byte sequence to match, but it priced only the signer. Signing is roughly a third of the work and the credential chain is the rest — and pi carries **no credential-resolution code at all**, so `AWS_PROFILE`, ECS task roles and IRSA (all advertised in `packages/coding-agent/docs/providers.md`) work only because the SDK's default chain is linked in. Hand-rolling ships a documented-feature regression. See the 2026-08-31 ruling. | — |
| 10 | **Codex adapter** | 2,228 LOC | ~3 + consult | **CONSULT: take `klauspost/compress` for zstd (and likely a WebSocket module), or leave queued?** Back in scope 2026-08-27. zstd is not credibly hand-rollable. | — |
| 8 | **Agent harness + search** | 10,273 LOC src (+5,733 LOC upstream test) | see below | **FUNDED 2026-08-27** — the owner ruled it in. Active drain, not a parked item. **Shape not yet fixed:** the harness is a parallel implementation of surface `coding/` already has, so the estimate depends on the shape chosen — see "Harness shape" below. Backlog: **11** against its own tree (12 minus `e7fb8eb2a`, reassigned to entry 7) — of which 3 are already satisfied in `coding/` and 3 are upstream dead code, leaving **4** load-bearing. See "Harness delta" below. **Slice 8b SHIPPED 2026-08-28** (`b677517`, `ce76e94`, `cd0e3b9`, `1f49233`), and the re-measure split it into **8b-i** (`ExecutionEnv`, harness source, 7 symbols closed) and **8b-ii** (the seven `*Operations` seams, coding-agent source, invisible to this entry's counter) — 8b-ii ships with a named remainder. The entry stays open on 8c and 8d, and the backlog count is unaffected because 8b was a base-port slice rather than one of the deferred commits. | 11 |

Entries 1–6 total **~10 port-cycles**. Entry 7's E2 answer landed 2026-08-31 —
**no driver in the root module**, and the entry is now gated on slice 8c rather
than on a dependency question. Entry 8 is funded but its cost depends on the
shape chosen below.

### Submodule shape for heavy optional dependencies (settled 2026-08-31)

The Bedrock ruling establishes a reusable layout, and future consults of the
same kind should follow it or say why they depart from it.

```
github.com/sky-valley/pi                     root — stdlib + golang.org/x/* only
github.com/sky-valley/pi/providers/bedrock   own go.mod, own tag series, requires root
```

Rules that make it work, all verified in prototype rather than assumed:

- The submodule **requires a tagged root**, never a `replace` directive —
  `replace` is ignored for downstream consumers, so a `replace`-based submodule
  is unimportable and proves nothing. This is what makes `difftest/` (module
  `pidiff`, `replace … => ..`) precedent for confining something nobody imports,
  and *not* precedent for this.
- It gets its **own tag series** (`providers/bedrock/v0.8x.NN`), which means a
  two-step release: tag root, then tag the submodule against it.
- The seam it plugs into already exists and is public: `ai/registry.go` exposes
  `RegisterApiProvider` over an open map, so a provider registers at runtime and
  no root code changes to admit one.
- Root stays clean by construction, and `internal/policy/deps_test.go` proves it
  each cycle — the submodule's dependencies never enter the root graph because
  `go list ./...` in the root does not descend into a nested module.

The cost is the risk recorded in the ruling: with no CI, only the root is built,
so the submodule can rot silently against root API changes. That cost is
accepted deliberately. It is the price of keeping the root module's dependency
budget, which is the port's most-stated invariant.

### Harness shape — the open question, with a default

`packages/agent/src/harness` duplicates surface `coding/` already implements.
Three shapes, materially different in cost:

| shape | what it means | cost | cost of being wrong |
|---|---|---|---|
| **(a) mirror upstream** | a new Go package mirroring `packages/agent/src/harness`, alongside `coding/` | ~24 cycles | two implementations of compaction, tools, session storage to keep in parity forever — the port's maintenance burden roughly doubles on its busiest surface |
| **(b) delta only** *(default)* | treat `coding/` as the port's harness; port only what the harness has that `coding/` lacks, exposing it through existing packages | ~4-6 cycles, pending the delta measurement | the port's package layout keeps diverging from upstream's, so future harness commits need a mapping step rather than a path match |
| **(c) extract** | refactor `coding/` to split a reusable harness core out, matching upstream's architecture | ~12 cycles + a public Go API break | a large refactor of shipped, working code for an architectural match no Go consumer has asked for |

**Default is (b)**, on the port's own standing formula: full pi SDK
functionality represented in Go, close faith to the source, leaning into Go's
idioms — not a transliteration of upstream's package boundaries. (a) buys
upstream's file layout at the price of duplicating the port's most
parity-sensitive code; (c) rewrites working code for the same reason. Proceeding
on (b) unless the owner says otherwise; the delta measurement is the next step
and will put a real number on it.

**Queue discipline.** An entry untouched for more than ~10 cycles is evidence
the ruling that created it was wrong; re-open the ruling rather than letting the
row rot. That is the lesson entry 8 has been teaching for 16 cycles.

### Harness delta, measured 2026-08-27 — and why most of entry 8 should wait

The funding decision stands. The measurement changes what it buys, and the
finding is large enough to state first:

**Upstream's `AgentHarness` is not implemented at `ccfe79ed2`.**
`packages/agent/src/harness/agent-harness.ts` is 508 lines of which ~450 are
type declarations, and the class body has **27 sites** that throw
`HarnessNotImplemented(...)` or return `this.unavailable(...)` — `prompt`,
`skill`, `compact`, `navigateTree`, `resume`, `abort`, `steer`, `followUp`,
`nextRun`, `cancelQueued`, `recordUsage`, `waitForIdle`, `peekAction`,
`executeAction`, `runToCompletion`, `watch`, `lane`, `createLane`, `lanes`,
`watchSession`, all of them. Its own test file is named
`agent-harness-scaffold.test.ts`. Corroborating, verified by grep at the sha:
`reducer.ts` (667 LOC) and `events.ts` (102 LOC) are **not exported from
`packages/agent/src/index.ts`** and have exactly one importer each — their own
tests; `startAiSpan`/`startHarnessSpan` are exported and called from nowhere in
the monorepo. That tree took **79 first-parent commits in 90 days**.

So a large part of entry 8 is upstream's own unfinished rewrite. Porting it now
would mean porting a spec mid-flight, on the port's most parity-sensitive
surface, to deliver a recovery kernel with nothing to recover, a durable
protocol with no writer, and a session format with no data in it.

**Correction to the funding ruling's scoping note.** It said the ported
coding-agent consumes 9 harness symbols. Measured properly: those 9 names are
coding-agent's own **local definitions** of the same names, not imports. The
real coupling is **10 symbols through one file**,
`packages/coding-agent/src/server/create-harness.ts` — `AgentHarness`,
`AgentHarnessOptions`, `AgentHarnessTool`, `HarnessTool`, `ExecutionEnv`,
`ExecutionToolContext`, `createBashTool`, `createEditTool`, `createReadTool`,
`createWriteTool`. This strengthens shape (b) rather than weakening it: the two
trees are independent implementations, not a shared core.

**Symbol delta:** 333 export sites / 329 unique names → **93 HAVE, 171
MISSING-REAL, 69 MISSING-MOOT**. `messages.ts` is 13/13 HAVE; `utils/truncate.ts`
9/9; `compaction/utils.ts` 6/6; tools 26/36; compaction 22/34. The port's
existing behavior is coding-agent's — the *richer* of the two implementations
(544-line bash tool vs the harness's 161).

#### Revised sequencing — rewritten 2026-08-28, after 8b shipped

The 2026-08-27 table was written before slice 8b landed. Re-measuring it against
the shipped code found three of its numbers wrong **as originally written** —
`packages/agent` is tree-identical across `ccfe79ed2..56f3f33a9`, so nothing
upstream moved and none of these is staleness. They are corrected below and the
old values are named so nobody reconciles to them again.

**Correction 1 — `FileSystem` is a `cwd` field + 17 methods, not 13.** The old
row's "13 methods (…)" listed twelve and counted the `cwd` field as the
thirteenth — and that list describes what the **port** shipped, not upstream's
interface. Upstream adds `createDir`, `remove`, `createTempDir`, `createTempFile`
and a `cleanup`. So the shape claim was measuring the wrong object.

**Correction 2 — the conformance suite is 30 cases in 5 groups, not 13**
(entries and lanes 8, records and log 8, repository and forks 6, queries and
facts 4, validation and immutability 4). Count it with
`git show <sha>:packages/agent/src/harness/session/testing/conformance.ts | grep -c '^\t\tcreateCase('`
— a single-line regex under-reports, because 14 of the 30 call sites wrap their
arguments.

**Correction 3 — the symbol universe is 334 export sites / 330 unique names**,
not 333/329. Every per-file denominator the old row quoted reproduces exactly
(`messages.ts` 13, `utils/truncate.ts` 9, `compaction/utils.ts` 6, tools/ 36,
compaction/ 34), so the extraction rule is the same rule, off by one site and one
name. **The dropped site was never located** — do not repeat the guess that it
was `harness/env/nodejs.ts`; that was proposed and left untested.

**Symbol delta, stated honestly: 100 HAVE / 165 MISSING-REAL / 69 MISSING-MOOT**
— which is the prior 93/171/69 **plus 7 movers, not an independent recount**. The
buckets were not re-derived this cycle, and the split is contingent on the
unlocated 334th site being MISSING-REAL. Per-file scores that were re-verified
and are unchanged: `messages.ts` 13/13, `utils/truncate.ts` 9/9,
`compaction/utils.ts` 6/6, tools/ **26/36**, compaction/ 22/34.

**The structural finding that broke the old model, and the reason this table is
now split.** Slice 8b as shipped straddles two upstream trees. `ExecutionEnv` /
`FileSystem` / `Shell` are **harness** source (`harness/types.ts:231-315`), inside
entry 8. All seven `*Operations` interfaces are **coding-agent** source —
`packages/coding-agent/src/core/tools/{read:49,edit:96,write:31,bash:63,grep:56,find:55,ls:37}.ts`,
and a grep for them across `packages/agent` returns empty. `tooloperations.go` is
522 of the 955 shipped Go lines, and **entry 8's counter cannot see any of it**.
A single row holding both will keep mis-predicting, because half the row is
invisible to the measure that scores it. Hence 8b-i and 8b-ii.

| # | slice | est. | state | what it bought, and what remains |
|---|---|---|---|---|
| ~~8a~~ | ~~Telemetry `pi.ai.request` emission~~ | — | **STRUCK 2026-08-27** | Struck the same day it was proposed, on a check that should have come first: **pi does not emit this span either.** `git grep -ln "startAiSpan\|pi.ai.request" <sha> -- packages/ai` is EMPTY; the only callers of `startAiSpan`/`AI_TELEMETRY_SCHEMA` in the monorepo are the doc generator and their own test. The port's telemetry seam is latent because pi's is. Emitting would be the port INVENTING behavior pi lacks. Re-open only when upstream's own request paths start a span. |
| 8b-i | **`ExecutionEnv` + `LocalEnv`** — harness source, inside entry 8 | 2-3 | **SHIPPED 2026-08-28** (`b677517`) | `coding/execenv.go`, **433 LOC** + 294 test. Closes **7 export sites** MISSING-REAL → HAVE: `ExecutionEnv`, `FileSystem`, `Shell`, `FileInfo`, `FileKind`, `ShellExecOptions`, `NodeExecutionEnv`→`LocalEnv`. Two of the seven (`ShellExecOptions`, `FileInfo`) sit on a judgement boundary — the pre-8b bash tool already carried cwd/env/timeout inline and ls used `os.Stat` metadata, which the same shape-collapsed rule scores HAVE elsewhere. **Coverage is 13 of 18 members** (counting `cleanup` on the `Shell` side): `cwd`→`Cwd()` plus 12 methods, `fileInfo`→`Stat` (a Go method may not share its return type's name). **Unported and uncommented: `createDir`, `remove`, `createTempDir`, `createTempFile`.** `Shell` is 2/2. Side effect worth carrying: the port now has a Go stand-in for a `harness/` runtime class for the first time, so "no Go home, therefore zero owed" is no longer available for `harness/env/nodejs.ts` — it remains available for `harness/session/**`, `harness/tools/**`, `events.ts` and `agent-harness.ts`. |
| 8b-ii | **The seven per-tool `*Operations` seams** — coding-agent source, **NOT entry 8** | not estimated | **SHIPPED 2026-08-28, with a remainder** (`ce76e94`, `cd0e3b9`, `1f49233`) | `coding/tooloperations.go`, **522 LOC** + 371 test, plus 207 changed lines in `coding/tools.go`. Seven seams / 16 members, matching pi member-for-member, as **structs of funcs** rather than interfaces (pi's members are individually overridable and one is optional). Seven `Env*Operations` bridges turn an `ExecutionEnv` into each seam — a port-only construct with no single upstream file behind it. **REMAINDER, and it is why this is not "done":** (a) three members are declared, defaulted, bridged and **never consulted** — `EditOperations.Access`, `LsOperations.Exists`, `FindOperations.Glob` — while pi consults all three (`edit.ts:349`, `ls.ts:133`, `find.ts:169-178`); `Glob` is the substantive one, since upstream a supplied glob replaces the matcher wholesale, so an injected find seam cannot yet change what find finds. (b) **No host can reach any of this** — every `*ToolOps` constructor is unexported and `CreateTool`/`CreateCodingTools`/`CreateAllTools` take only `cwd`, so injection works from inside package `coding` and its tests only. The slice's stated purpose, pointing the agent at a sandbox, has not shipped. (c) `EnvBashOperations` drops `BashExecOptions.Env` (the `PI_*` session metadata never reaches the child) and never yields `ErrShellAborted`/`*ShellTimeoutError`, so an `ExecutionEnv`-backed shell tool renders neither "Command aborted" nor "Command timed out". (d) find and grep still enumerate the local disk directly (`tools.go:1617`, `:1820`), faithful to pi's fd/ripgrep architecture but meaning an injected remote filesystem would enumerate nothing. |
| 8c | **`SessionStorage`/`SessionRepo` seam + in-memory backend + conformance suite** | re-derive — the old 3.5-4 was set against wrong numbers | **NOT STARTED, owner-gated** | Re-sized at `56f3f33a9`: `SessionStorage` is **17 methods** (`session/types.ts:290-326`), `SessionRepo` **5** (create, open, list, delete, fork); in-memory backend `session/memory.ts` 192 LOC; conformance `session/testing/conformance.ts` **1,016 LOC / 30 cases**. Three honest tiers, because the seam alone does not compile: **narrow 1,280 LOC**, **buildable 1,623** (the interfaces are typed against the `Entry`/`RecordBase`/`EntryQuery`/`SessionError` substrate in the same file), **whole non-JSONL session subtree 2,379**. The gate is unchanged and still correct: the port's v3 recorder cannot conform — no records, no lanes, no facts — so this is a design decision, not a mechanical port. Do not fund until the owner wants pluggable durability as a product feature. |
| 8d | **`search/`** (`b75be04d9`) | 0.5 | **NOT STARTED, rides with 8c** | **208 LOC src** (`search/index.ts` 32 + `search/scanning.ts` 176), 12 exported symbols, +125 LOC upstream test. All 12 MISSING-REAL; the port has zero analogue and 8b changed that not at all. Published SDK surface (`export * from "./search/index.ts"`). Confirmed still worthless before 8c: `scanning.ts` types its entire substrate against the `SessionStorage` query surface — `ScanningReadable` is a `Pick<>` of it. |
| — | **`reducer.ts`, JSONL v4, `agent-harness.ts`, `events.ts`** | ~6-8 | **DEFERRED, tripwired** | **2,134 LOC src + 2,836 LOC test** — 21% of entry 8's source but **49% of its test mass**, which is itself an argument for deferring. All four are dead or stub code upstream: `events.ts`'s only importer at the pin is its own test, and `agent-harness.ts` is a scaffold. Re-open on the first upstream commit that makes them real. |

**The tripwire, so this is checkable rather than remembered:**
`git show <sha>:packages/agent/src/harness/agent-harness.ts | grep -cE "HarnessNotImplemented|this\.unavailable\("`
is **27** at `ccfe79ed2` and **27** at `56f3f33a9` — not merely equal but the same
blob (`3802900d`, 508 lines), split 5 `HarnessNotImplemented` + 22
`this.unavailable(`. When that number starts falling, upstream is landing the
writer side and the recovery kernel becomes portable against a real consumer.
Triage checks it whenever a cycle touches the harness tree.

**Spent and remaining.** 8b shipped in 4 commits over 2 days: **1,737(+)/90(−)
across 5 files**, 955 LOC of new implementation and 665 of new test, 24 new test
functions. Read the estimate as "about right for the scoped part, and the slice
then grew to roughly twice it" — not as pessimism, since 8b-ii was never scoped
here at all. What remains of the callable delta is **8b-ii's remainder** (the
three dead members and, more importantly, an exported injection point), then
8c + 8d, both still owner-gated. Entry 8 does **not** close.

**Estimating rule this rewrite establishes:** score a slice by what it closes in
the entry-8 symbol universe, not by lines shipped. 8b moved **7 sites** for 1,737
inserted lines, and the gap is not waste — it is 8b-ii, which is real
coding-agent parity that entry 8's counter structurally cannot see. A slice that
cannot be scored by the counter needs its own row.

#### The 12-item backlog, enumerated — and recounted

The counter is honest as a count of deferred commits (11 against
`harness`/`search` + `e7fb8eb2a` against `session-backends` = 12, cross-checked
against upstream; none reverted). It is misleading as work owed:

| bucket | items | owed under shape (b) |
|---|---|---|
| **Already satisfied in `coding/`** | `ca21c1686` (single-edit guard → `coding/tools.go:725`, reached via `PrepareArguments: prepareEditArguments` at `:624`; the pre-8b citation `:664` was invalidated by 8b's rewire), `8c2529dae` (root-md skills → `coding/resources.go`), `7af2d27dc` (taskkill → `coding/proc_taskkill.go`) | **zero** |
| **Belongs to entry 7, not 8** | `e7fb8eb2a` (touches no harness/search source at all), plus the sqlite halves of `7bdb16c28`, `a4453b79b`, `b75be04d9` | move to entry 7 |
| **Real code, no upstream consumer** | `14ad9801b`, `d1a305613`, `1dd235405` — the whole of `events.ts` | discretionary; deferred above |
| **Genuine load-bearing delta** | `b75be04d9` (search, → 8d), and `7aca0d7b3` / `4a0e2f115` / `7bdb16c28`-harness (all edits to the harness's **own JSONL v4 store**, → 8c) | 4 items |
| **Subsumed** | `a4453b79b`-harness — the file it retyped was deleted three commits later | zero |

**Drain against the tree state at the pin, never by replaying the commits.**
Two items prove it: `4a0e2f115`'s try/catch became a `Result` check, and
`a4453b79b` retyped a field on a file `b75be04d9` then deleted — **22** upstream commits later, though the very next backlog commit (both landed 2026-08-11); the "three commits later" written here previously is wrong under either reading. A commit-replay
drain would port two things that no longer exist in that form.

**Ledger corrections applied:** entry 7 now carries the four sqlite deltas;
entry 8's backlog is 11 commits against its own tree, of which 3 are already
satisfied and 3 are upstream dead code.

## Drift at last sync check (2026-08-29) — pin advanced to 853a80d26

Delta `56f3f33a9..853a80d26`, **7** first-parent changes, **no merges**: the
first-parent count and the total commit count are both 7, so nothing rode in
under a merge. **Release v0.84.4 CROSSED** at `b79e4cc83` — all nine
`packages/*/package.json` go 0.84.3 → 0.84.4 and the tag `v0.84.4` contains that
sha — so the catalog is regenerated, the npm reference build moves, and a **port
tag (`v0.84.20`) is cut with a release tweet**. Verdicts: **2 port → 2 Go
commits; 2 port-but-QUEUED; 3 n/a; 0 decide**.

Both reconciliation passes ran. The **detector** (`--name-only`, no pathspec)
printed **47** paths, every one classified, all of them status M — no A/D/R even
under `--find-renames` — so there is no new top-level package, no file moved out
of `packages/`, and no new repo-root directory. The **accounting** sweep over
`packages` minus the four structural exclusions reported **31 files,
493(+)/140(−)**, each mapped to a verdict below. Of the four
never-diffstat-dispatch files, none appears in the range.

### Port worklist (2 → 2 Go commits)

| upstream | subject | Go | notes |
|---|---|---|---|
| `b79e4cc83` | Release v0.84.4 | `08f5bed` | **Catalog surface.** Decided the regen by EXECUTING `JSON.stringify(MODELS)` against both builds' `dist/models.generated.js`, never from git (2026-07-30 ruling): 558,804 → 563,988 B, **1312 → 1290 models**, 39 providers unchanged, **57 added / 79 removed / 227 changed**. **Endpoint-pinned at both ends** — the outgoing `ai/models_catalog.json` is byte-identical to the same derivation from 0.84.3, so the ported diff IS the upstream release diff. **No schema drift at any level**: the model-level key set is identical (13 keys), `cost` sub-keys unchanged (22 tiered models both sides), `compat` unchanged at 26 keys. `compat.allowedFallbackModels` **stays at its two anthropic entries with identical chains**, so the refusal-fallback path keeps working against unchanged data and `TestAnthropicCatalogFallbacksAreLive` passes unedited. Drains the **entire 3-item catalog-only queue**, every member verified an ancestor of the release sha by `merge-base --is-ancestor` rather than by log order. Orphan sweep: all 39 non-`radius` `defaultModelPerProvider` entries still resolve, and the table itself is entry-for-entry identical to upstream `model-resolver.ts` at `853a80d26` — that being the table this ledger records as having caused a miss TWICE, so it was diffed rather than assumed |
| `56700d42e` | fix(coding-agent): compact before post-tool model requests (#8782) | `e1e8cdb` | **Turn-ordering surface; no byte-golden moves.** pi moves `prepareNextTurn` out of the post-`turn_end` block and to the head of the next inner-loop iteration, so preparation runs immediately before the request it prepares for — post-tool requests included. Three behavior changes ported into `agent/loop.go`: preparation now runs **only when another turn will actually run** (it no longer fires after the final turn); `ShouldStopAfterTurn` now runs **first**, on the completed-turn context; and steering queued while a long preparation ran is picked up before the turn starts, but **only when the earlier poll came back empty**, or one-at-a-time mode would deliver two messages in one turn. Go renders pi's `firstTurn` flag and its `lastCompletedTurn` snapshot as the single nilable `*ShouldStopAfterTurnContext` they became upstream — exact rather than approximate, since upstream's `PrepareNextTurnContext extends ShouldStopAfterTurnContext {}` adds **zero members** (`853a80d26:packages/agent/src/types.ts:147`). **The coding-agent half needs no port**, and this is the load-bearing judgement of the cycle: Go drives auto-compaction through `Session.EnableCompaction`'s per-request `TransformContext` hook (`coding/compaction.go:325`), invoked inside `streamAssistantResponse` (`agent/loop.go:252`) on **every** request the loop makes, so upstream's new positive test (compact after a tool result, before the next assistant request) is satisfied **by construction** — and so is its new negative one (do not compact after a *terminating* tool result), because a terminating batch produces no further request and the port only compacts when a request is built. `PrepareNextTurn` has no setter anywhere in the port, so `_installAgentNextTurnRefresh` has no Go counterpart to move. See the deliberate divergence below for the third upstream test that does NOT hold here |

### Port-but-QUEUED — two Scope queue entries take a delta

| upstream | subject | entry | what it will add |
|---|---|---|---|
| `6c87d9a02` | fix(ai): merge indexed Mistral tool call chunks | **6** (Mistral conversations) | `consumeChatStream`'s `toolBlocksByKey` is rekeyed from the composite `` `${callId}:${index}` `` string to `toolCall.index ?? callId` (map type widened to `string \| number`), so chunks of one call that arrive with differing ids still merge instead of splitting into separate tool blocks. No Go base: `ai/providers/` has no mistral adapter — the only mistral references in the port are registry-level, in `ai/envkeys.go`, `ai/types.go` and `coding/resolve.go` |
| `5ce4afbd9` | fix(ai): refresh generated image model catalog | **3** (image generation) | `image-models.generated.ts` +75 lines: three openrouter image models. Catalog surface with no port-side file — `ai/` embeds `models_catalog.json` only — so it lands when entry 3's base port ships, not at this regen |

### n/a (3)

| upstream | subject | why |
|---|---|---|
| `e54d45d11` | docs(coding-agent): remove issue-specific regression test placement rule | Prose only — `AGENTS.md` and `packages/coding-agent/test/suite/README.md`. Zero `src` content |
| `bba6be972` | docs: audit unreleased changelogs | CHANGELOG prose only, in the always-noise set |
| `853a80d26` | Add [Unreleased] section for next cycle | CHANGELOG prose only, in the always-noise set. Two lines per package across nine packages |

### Deliberate divergence added this cycle — steering typed *during* compaction lands one turn later

Upstream's third new test for `56700d42e`, `includes steering queued during
compaction in the resumed assistant request` (asserting `callCount === 4`), does
**not** hold in the port, and the reorder ported here is what makes that
visible. The cause is the architectural difference this ledger has recorded
since compaction was ported: pi compacts in `prepareNextTurn`, the port compacts
in a per-request `TransformContext`.

- **pi post-fix:** tools done → prepare (compaction runs here, slowly) → re-poll
  steering → inject → **one** request carrying both the summary and the steering
  message.
- **The port:** prepare (nil) → re-poll (instant — nothing is queued yet) →
  inject → `streamAssistantResponse` → `TransformContext` → compaction runs
  **here, after injection**. Steering typed while it ran misses this request, is
  picked up by the end-of-turn poll at `agent/loop.go:219`, and lands one turn
  later — one extra provider round-trip versus pi.

Verified structurally rather than asserted: injection is `agent/loop.go:160-168`
and the transform is `agent/loop.go:252`, so the ordering is not a timing
accident. The final transcript content converges; the turn boundary and request
count do not.

This is recorded as a **tripwire on the compaction-placement ruling**, not as a
port defect: it re-escalates the moment the port gains a `PrepareNextTurn`-driven
or persisted compaction path, at which point the steering re-poll ported in this
very commit becomes the mechanism that closes it.

## Drift at last sync check (2026-08-28) — pin advanced to 56f3f33a9

Delta `ccfe79ed2..56f3f33a9`, **3** first-parent changes, **no merges**: the
first-parent count and the total commit count are both 3, so nothing rode in
under a merge. **No release crossed** — `git tag --contains` is empty at both
ends of the range (v0.84.3 @ `4e58f324f` is an *ancestor* of the old pin, so the
pin already post-dated the last release), no `packages/*/package.json` is
touched at all, and `packages/ai/src/models.generated.ts` is blob-identical
(`12952045`) — so no catalog regen, no npm reference-build move, and **no port
tag and no release tweet** this cycle. Verdicts: **0 port; 0
port-but-CATALOG-ONLY; 3 n/a; 0 decide**.

This is a triage-only pin advance with **no ported Go commits and no review
gates** — but, unlike the two prior zero-port cycles, it is not a cycle in which
nothing happened to the repo: four Scope-queue **entry 8** commits landed
out of cycle and are recorded below for the first time.

`packages/ai/scripts` is byte-identical across the range (tree `a2ab05bc`), so
the catalog-only queue **stays at 3**. `packages/agent` is tree-identical
(`9073a144`), so the harness backlog is unchanged at **11** against entry 8's own
tree; `packages/session-backends` is tree-identical (`37d7c1b0`), so entry 7's
four queued deltas are unchanged. No new Scope queue row was opened.

Both reconciliation passes ran. The **detector** (`--name-only`, no pathspec)
printed **17** paths, every one classified, all of them status M — no A/D/R even
under `--find-renames` — with the repo's top-level tree listing and its
`packages/` directory set identical at both ends, so there is no new top-level
package, no file moved out of `packages/`, and no new repo-root directory. Only
one of the 17 lies outside `packages/` (`scripts/build-coding-agent-bundle.mjs`).
The **accounting** sweep over `packages` minus the four structural exclusions
reported **9 files, 155(+)/18(−)**, each mapped to a verdict below. None of the
four never-diffstat-dispatch files (`core/model-resolver.ts`,
`core/package-manager.ts`, `core/trust-manager.ts`,
`packages/telemetry/src/index.ts`) appears anywhere in the range.

### n/a (3)

| upstream | subject | why |
|---|---|---|
| `1defa151e` | fix(coding-agent): expose https-proxy-agent named export (#8723) | **E1** — the whole change is an esbuild plugin in repo-root `scripts/build-coding-agent-bundle.mjs` (packaging the Node artifact, and appended right beside the `lazyJitiPlugin` whose own commit `c1279a65b` is already recorded `n/a`) plus one CHANGELOG line. **Zero `packages/*/src` content.** The interesting part is what the plugin patches: its `onResolve` filter returns undefined unless `args.kind === "dynamic-import"`, and pi's own `https-proxy-agent` import is *static* (`bedrock-converse-stream.ts:27`), so the failing dynamic import is `google-auth-library/gaxios` inside `node_modules` — which is why the reported symptom is Vertex-specific. **Structural:** zero hits in the port for `https-proxy-agent`, `HttpsProxyAgent`, `esbuild`, `jiti` or `undici`; the port's single proxy site is `ai/providers/retry.go:113` `Proxy: http.ProxyFromEnvironment`, i.e. Go's stdlib supplies natively the capability the bundled Node agent was failing to supply, so the user-visible defect has no Go analogue to fix |
| `4e4949299` | feat(tui): allow disable copy on fullscreen, ctrl + x copies selection (#8731) | **E1** on every hunk, read in full rather than dispatched: `packages/tui/src/tui-alt-screen.ts` (mouse-selection and OSC 52 clipboard machinery), `modes/interactive/{interactive-mode.ts,components/settings-selector.ts}`, and `core/settings-manager.ts`, which the scope boundary names explicitly — that hunk is one optional `fullscreenCopyOnSelect?: boolean` plus its getter/setter, i.e. operator configuration. The remaining files are README/docs and host tests. The closest thing to an escape is that the commit edits the method wrapping upstream's `getLastAssistantText()`, which the port *does* have as `Session.LastAssistantText()` — but the edit only prepends a host-side guard reachable through a `TuiAltScreen` instance, and `core/agent-session.ts` is not in the diffstat. **Structural:** the port mirrors none of the setting's siblings (`tuiMode`, `fullscreenExitOutput`, `fullscreenScrollbar`: zero hits) and has no settings-file reader or writer at all, so there is no struct for the field to be missing from |
| `56f3f33a9` | fix(ai): remove retired Fireworks turbo router test | Test-only, **empty Go delta** — and the reason is worth stating precisely, because **no exclusion test fires on it**. E1 does not reach `packages/ai/test`; E2 is no longer an exclusion; E3 is the telemetry schema half alone. By the boundary's own default the hunk is therefore *in scope*, and the honest verdict is "in scope, nothing to delete" rather than an exclusion — it is tallied under `n/a` only because the ledger's taxonomy has no cell for a structurally empty delta. It removes one assertion that a `accounts/fireworks/routers/*-turbo` model registers, that model having been retired upstream. The port has no counterpart to `fireworks-models.test.ts` — not to the deleted case and not to the surviving GLM-5.2-Fast one — because the port pins catalog rows by regenerating `ai/models_catalog.json` from the npm build rather than by asserting individual models. **Structural:** zero `turbo` and zero `routers/` hits in any `*.go`; and `coding/resolve.go:47`'s fireworks default was diffed rather than assumed — it is `accounts/fireworks/models/kimi-k2p6`, byte-equal to upstream's `model-resolver.ts:46` at the new sha, so the retirement orphans nothing. That table is the one this ledger records as having caused a miss TWICE, which is why it was checked explicitly |

### Watch item (not a queue row) — the retired Fireworks turbo router

`56f3f33a9` is the visible edge of a **catalog data** change, and catalog data is
generated at publish time rather than committed: `git grep "kimi-k2p6-turbo"` is
empty at *both* ends of the range, and upstream's committed
`models.generated.ts` is a 6.5 KB stub containing no `routers/` ids at all. The
port's embedded catalog, pinned to `pi-ai` 0.84.3, still carries
`accounts/fireworks/routers/kimi-k2p6-turbo` with exactly the api/baseUrl/input
triple the deleted test asserted.

It is **not** a `port-but-CATALOG-ONLY` row: that verdict is defined as a
`packages/ai/scripts` generator delta, and `packages/ai/scripts` is byte-identical
here. It is a pre-existing catalog fact that resolves itself mechanically at the
next release regen, where the fireworks bucket is expected to go **23 → 22**.
Recorded because a *shrinking* catalog is otherwise the shape of a failed dump —
whoever reviews the next regen should read that one deletion as expected.

### Entry 8 — slice 8b shipped out of cycle, and the queue did not know

Four Go commits landed after the 2026-08-27 pin row was written and appear
nowhere in this ledger until now. They are Scope queue **entry 8** work, drained
between sync cycles rather than by one:

| commit | date | what |
|---|---|---|
| `b677517` | 08-27 | `ExecutionEnv` (pi's `FileSystem` + `Shell`, `harness/types.ts:231-315`) plus `LocalEnv`, with the read tool rewired as its first consumer and an in-memory `fakeEnv` for tests → new `coding/execenv.go`. Standing renderings recorded in-source: `Result<T, FileError>` → `(T, error)`, trailing `abortSignal` → leading `context.Context`, `fileInfo` → `Stat` |
| `ce76e94` | 08-28 | Corrects the above — that seam is the *harness's*, not the coding-agent's. Ports all seven coding-agent `*Operations` seams (Read/Edit/Write/Bash/Grep/Find/Ls) as structs-of-funcs → new `coding/tooloperations.go`, with two fidelity fixes (`ops.access` called before reading, per `read.ts:248`; EISDIR emulation moved into `DefaultReadOperations`) |
| `cd0e3b9` | 08-28 | Corrects the resolution semantics: pi resolves `options?.operations ?? defaultOperations` as **whole-object replacement**, not spread-over-defaults, so the seams take a pointer and nil matches pi's absent `operations?`. Wires ls and find |
| `1f49233` | 08-28 | Completes it — shell and grep wired, all seven tools pluggable, nothing deferred. `runBashCommand` takes an `io.Writer`; pi's thrown `aborted`/`timeout:<n>` render as `ErrShellAborted` and `*ShellTimeoutError`. Records a deliberate divergence on `GrepOperations.readFile` (Go scans primarily; pi reads ripgrep context lines) and a test-quality fix — `classifyShellRun` was extracted after mutation testing found an abort-vs-timeout test that still passed with the precedence inverted |

**Consequence for the queue, applied below rather than left implicit:** slice
**8b is shipped**, and the seven `*Operations` seams it grew into were not in the
8a-8d model of the work at all. The slice row and the cycle total said "2-3
cycles" for work already in `main`. Entry 8 **does not close** — 8c
(`SessionStorage` seam + conformance suite) and 8d (`search/`) are untouched, and
both remain owner-gated per the 2026-08-27 measurement. The backlog count is
unaffected: 8b was a base-port slice, not one of the 11 deferred commits.

The harness **tripwire was re-measured at the new sha**, per this ledger's own
rule that triage checks it whenever a cycle touches the harness tree:
`grep -cE "HarnessNotImplemented|this\.unavailable\("` on
`packages/agent/src/harness/agent-harness.ts` is **27 at `56f3f33a9`**, exactly
as at `ccfe79ed2`. Upstream has not started landing the writer side, so the
deferred slices stay deferred.

### Harness — re-pinned to 56f3f33a9, 49 PASS / 0 KNOWN / 0 FAIL

`PI_UPSTREAM_SHA` moves `ccfe79ed2` → `56f3f33a9` in `difftest/config.env`.
`PI_NPM_VERSION` stays **0.84.3** — no release crossed. The re-pin is a
**provable no-op** this cycle: the suite is 49 `dist` / 0 `src`, and `run.sh`
only extracts `pisrc/$PI_UPSTREAM_SHA` when a `src` scenario asks for it, so the
new sha is never read. Re-run after the move anyway, per house convention:
**49 PASS / 0 KNOWN / 0 FAIL**, exit 0.

### Base gate

`gofmt -l` clean; `go build ./...` and `go vet ./...` clean; `go test -race ./...
-count=1` green across all **10** test-carrying packages (13 listed, 3 with no
test files), **1,277** test functions. Nothing in the port changed this cycle, so
the gate is a base check on the tip rather than a verification of new work — but
it is **not** skipped as the two prior zero-port cycles skipped it, because the
four entry-8 commits above landed since the last recorded gate run and had never
been gated in a ledger entry. `ai/providers/openai*.go` is untouched, so the
6-scenario request diff was not triggered.

No new boundary questions, and no `decide` items. The 2026-08-22 "OPEN — carried
from 2026-08-22" item is **closed**, not carried: it asked for `pi-triage`'s
narrow `port` definition to be aligned with the rulings, and the 2026-08-27
rewrite replaced that definition outright with the three exclusion tests.

## Drift at last sync check (2026-08-27) — pin advanced to ccfe79ed2

Delta `a79b37334..ccfe79ed2`, **13** first-parent changes, **no merges**: the
first-parent count and the total commit count are both 13, so nothing rode in
under a merge. **No release crossed** — no tag contains the pin and no
`packages/*/package.json` moved — so `pi-ai` stays 0.84.3, the catalog is
untouched, the npm reference build does not move, **no port tag is cut and there
is no release tweet**. Verdicts: **4 port → 4 Go commits + a harness re-pin + a
review-fix commit; 0 port-but-CATALOG-ONLY; 9 n/a; 0 decide**.

`packages/ai/scripts` is tree-identical across the range, so the catalog-only
queue **stays at 3**. The `packages/agent` harness tree moved once, so the
**harness backlog goes 11 → 12**. Every other deferred tree
(`packages/session-backends`) is untouched.

Both reconciliation passes ran. The **detector** (`--name-only`, no pathspec)
printed **44** paths, every one classified, with no new top-level package, no
file moved out of `packages/`, and no new repo-root directory — every path lands
under `packages/`. The **accounting** sweep over `packages` minus the four
structural exclusions reported **28 files, 523(+)/59(−)**, each mapped to a
verdict below.

### Port worklist (4 → 4 Go commits + review fixes)

| upstream | subject | Go | notes |
|---|---|---|---|
| `6b36eb592` | fix(coding-agent): remove explicit tool choice from compaction calls | `351c0f4` | **Request-body golden surface, and an exact REVERT of `fe37e9f9b`** — which this port had shipped two days earlier as `833af61` and released in tag `v0.84.19`. Blob indices confirm the revert (`04326ca2f`→`0529db628`, then back). Ported without escalation per the **2026-08-19 ruling**: an upstream revert is ported like any other change, and the port's public surface is downstream of pi's. Nothing public is removed — `ai.ToolChoice`, `ToolChoiceNone` and `SimpleStreamOptions.ToolChoice` all stay; what goes is the `len(tools) > 0` half of the guard at `ai/providers/openai.go` and the `opts.ToolChoice = ai.ToolChoiceNone` call site in `coding/compaction.go`. Both doc comments in `ai/types.go` stopped asserting an `"auto"` default, mirroring upstream's JSDoc edit. **Three previous-cycle tests were INVERTED rather than deleted**, so the coverage that pinned suppression now pins emission. pi's own new test (`honors caller-supplied routing session and tool choice`) has no Go analogue: Go's `completeSummarization` builds its `SimpleStreamOptions` internally and takes no caller options struct, so there is no caller-supplied `toolChoice` to honor — recorded so it does not read as missing coverage |
| `7aab6c26e` | fix(ai): serialize thinking signature once (#8671) | `c728a26` | **Session-format golden surface** — and the replayed request body is byte-**un**changed, which is what makes the differential harness silent here (it compares requests only; response parsing is explicitly out of its scope). The port had already converged on HALF of this change: it held the sequence in a local accumulator rather than re-parsing the signature per delta. What moved is the other half — the assignment itself, which fired on every delta and now fires once at block end plus once on the abort path, matching pi's `applyStreamedReasoningDetails` at `finishBlock` and in `catch`. To let the Go failure path do the catch block's work, `thinkBuilder`, `thinkingDetails`, `order` and `materialize` were hoisted above `fail` — which mirrors upstream hoisting `streamedReasoningDetails` above its own `try` for exactly that reason. Upstream ALSO dropped seeding from an existing signature; Go seeds nil and never reads the slot, test-locked |
| `0b5ee5d8b` | fix(coding-agent): repair unterminated session files | `46116a4` | Upstream appends a newline to a session file whose last line lacks one, **after** the header validates, so the next appended entry cannot fuse with the tail (upstream #8345). The bug is real in the port and was reproduced before it was fixed: Go's readers split on `"\n"` and tolerate an unterminated tail, but `ResumeSession` opens `O_WRONLY|O_APPEND` and `writeLine` appends directly, so the next entry fuses and **two** entries are lost to the reader. **pi's one shared loader maps to three Go readers and one writer**, so the placement was a judgement call: the repair sits in `ResumeSession` alone, reusing the append fd already being opened, because that is the port's only append path. See the deliberate divergence below for the half not ported |
| `7af2d27dc` | fix(coding-agent): prevent Windows taskkill spawn crashes | `d5d7b4a` | Upstream fixes the same function in two files; only one is in scope. **In scope:** `utils/shell.ts`, whose `killProcessTree` is consumed by the ported bash tool (`core/tools/bash.ts` imports it at `ccfe79ed2`, lines 114 and 122) → `coding/proc_windows.go`. **Out of scope:** `packages/agent/src/harness/env/nodejs.ts`, the deferred harness tree — backlogged, not ported. Of the two halves of the fix, only the absolute-path resolution is portable; `child.once("error", …)` exists solely because Node reports a failed spawn asynchronously as an unhandled event, where `exec.Cmd.Run` returns it synchronously and the port's pre-existing fallback to `cmd.Process.Kill()` already handles it. Recorded as a deliberate no-op half rather than reinvented. New untagged `coding/proc_taskkill.go` holds the path arithmetic so it is testable on darwin, leaving `proc_windows.go` a thin caller — note a file named `proc_windows_test.go` would have carried Go's implicit `GOOS=windows` filename constraint and could never have run here |

### n/a (9)

| upstream | subject | why |
|---|---|---|
| `8fa7eebd2` | fix: persist default to scoped if non-empty | settings-manager + scoped/enabled-models surface, both excluded; second hunk is `modes/interactive`. **Structural check, not precedent:** zero hits in the port for `scopedModels`/`EnabledModels`/`DefaultModelAndProvider`, and `coding/session.go:325` `SetModel` has no persist option and no settings manager |
| `b37ebb7f2` | fix(tui): autocomplete orders nested results (#8669) | `packages/tui` only |
| `6c4f36026` | fix(tui): chunk large main-screen renders | `packages/tui` only |
| `b07e17faa` | fix(coding-agent): preserve partial Bash output when toggling thinking | `modes/interactive/` only |
| `1ac6128e6` | fix(tui): make alt screen not segment on - and / (#8676) | `packages/tui` only |
| `938109e72` | chore: changelog entries | CHANGELOG only |
| `42f7f29ad` | docs(settings): default model and thinking | docs/README only |
| `e86823096` | feat(tui): add terminal capability overrides | tui + settings-manager + `src/cli` + `main.ts` + modes, all excluded. **Structural:** no `TerminalCapabilit*`, `trueColor` or `hyperlinks` anywhere in the port |
| `ccfe79ed2` | feat(extensions): ui prompt events (#8355) | extensions runtime, excluded. **Structural:** no `UIPrompt*` and no extension-runner analogue in the port; the `src/index.ts` hunk is a pure type re-export of the same excluded types |

### SUPERSEDED — the `reasoning_details` re-validation divergence has changed shape, not closed

The 2026-08-25 entry below ("`reasoning_details` is never re-validated") recorded
pi re-deriving the sequence from `block.thinkingSignature` on every arriving
detail, re-running `isOpenAIReasoningDetail` over every stored entry and
discarding the whole sequence if one had turned invalid — reachable only via an
`index` magnitude `JSON.parse` saturates to `Infinity`. **`7aab6c26e` deleted
that re-parse**, so pi no longer re-validates and no longer discards, and that
divergence is gone.

It does not simply close, because the same root cause — the port renders each
detail to bytes on arrival where pi holds live JS objects — now surfaces
differently, and **the new shape is narrower**. Established by execution, not by
reading the diff: pi's own `fillMissingCommonReasoningDetailFields` and
`appendOpenAIReasoningDetail`, taken verbatim from `ccfe79ed2` and run in node
over `{"index":1e400}` followed by an adjacent same-type delta, yield
`[{"type":"reasoning.text","text":"ab","index":null}]`; the port yields
`[{"type":"reasoning.text","text":"ab"}]`. The mechanism is the nullish fill:
`Infinity` is not nullish, so pi's `index ??=` leaves it and the final
`JSON.stringify` renders it `null`, while rendering on arrival has already turned
it into a JSON `null` that the same fill reads as absent and drops. **Unmerged,
the two agree** — both render `"index":null` — so the divergence needs the
saturated value AND an adjacent merge.

**Recorded, NOT fixed**, on the same reasoning as before: reaching it needs a
provider to send `index: 1e400`, and closing it would mean giving up the eager
byte rendering on a hot path for a case no provider produces. The comment on
`mergeOpenAIReasoningDetail` now states this shape rather than the retired one.

### Deliberate divergence added this cycle — the session repair is narrower than pi's

pi has ONE shared `loadEntriesFromFile`, so upstream's repair fires on read-only
loads too: at `ccfe79ed2` it has three non-test callers (`session-manager.ts`
`_setSessionFile` :899, `open` :1543, and :1589), which means merely enumerating
sessions rewrites every valid session file whose tail is unterminated. The port
repairs **only in `ResumeSession`**, its single append path.

The user-visible outcome is identical — no entry can ever fuse, because appends
only happen after `ResumeSession`/`StartSession` — and nothing model-visible
differs. What differs is the on-disk side effect of *listing*: pi terminates the
tail, the port leaves it. That was judged the correct Go answer, because
`ListSessions` walks every `.jsonl` in the directory through `readSessionInfo`
and having a read mutate every file it touches is a surprising side effect for a
function that reads. The narrower surface is deliberate, not an oversight.

Upstream's ordering constraint IS preserved exactly: the header validates first,
and both of pi's early exits (no entries, bad header) leave the file untouched,
as does the port's `not a pi session file` error return. Mutation-probed — hoist
the repair above the header check and three subtests report a non-session file
was modified.

### Review gates

`pi-go-review` and `pi-parity-review` each ran as an independent subagent over
the integrated diff — neither had ported anything — and **all 15 findings they
filed were then handed to separate verifiers instructed to REFUTE them**. Two
survived; 13 were refuted with reproduction, including four that turned out to
be mid-cycle process artifacts rather than defects (the ledger pin and this very
divergence record are filled at stage 5, after the review gates at stage 3, so a
reviewer reading the diff correctly sees them stale and incorrectly reads that
as a fault in the diff).

The two survivors, both fixed in `b789eca`:

1. **`ai/providers/openai_reasoning_details.go`** — `mergeOpenAIReasoningDetail`'s
   doc comment still justified the byte-level merge with pi's per-arrival
   re-parse, which is exactly what this cycle's own port deleted. The porter had
   correctly removed the sibling claim in `openai.go` and missed this one. Not a
   pre-existing gap: the comment was TRUE before the cycle and was made FALSE by
   it, which is why the standing formula does not cover it. Rewriting it is what
   surfaced the divergence reshaping recorded above.
2. **`difftest/run.sh`** — section 2 extracted upstream TS unconditionally and
   exited 1 when the clone was missing, though `351c0f4` took the suite to zero
   `"src"` scenarios and nothing reads the extracted tree. Also created by this
   cycle, not inherited. Now gated on a scenario actually asking for it; both
   arms verified, and a full run with `PI_UPSTREAM_DIR=/nonexistent` is 49 PASS,
   exit 0.

The parity gate's verdict on all four ports was **faithful**, and it earned that
by execution rather than inspection — see the "Parity proofs at the pin" row.

### Harness — re-pinned to ccfe79ed2, 49 PASS / 0 KNOWN / 0 FAIL, fully dist-backed

`PI_UPSTREAM_SHA` moves `a79b37334` → `ccfe79ed2`. Scenario count is unchanged at
**49**, but the backend mix reaches **49 `dist` / 0 `src`** — the first cycle in
which the harness needs no upstream clone at all. The tool-choice pair, added
last cycle as the only two `src` scenarios precisely because `fe37e9f9b`
post-dated 0.84.3, flips back to `dist` for the symmetric reason: upstream
reverted the guard, so the published build is once again the correct reference.

`tool-choice-without-tools-absent` is renamed (`git mv`, so the rename is
recorded) to `tool-choice-without-tools-present` and its assertion inverted. It
is **load-bearing, not passing by mutual absence**: both sides emit
`"tool_choice":"none"` with no `tools` key, and reinstating the guard in a
scratch worktree turns it into `FAIL … missing-key @ $.tool_choice / pi: "none" /
go: <absent>` with exit 1, while the with-tools control still PASSes.

`difftest/README.md`'s scenario table and its `47 dist + 2 src` count were stale
on both counts and are corrected.

### Base gate

`gofmt -l` clean; `go build ./...` and `go vet ./...` clean; **`GOOS=windows go
build ./...` and `GOOS=windows go vet ./...` clean too**, new this cycle and
load-bearing — `coding/proc_windows.go` does not compile on the darwin host, so
without the cross-target vet the taskkill change would ship unchecked.
`go test -race ./... -count=1` green across all **10** test-carrying packages
(13 listed, 3 with no test files), with **8 new test functions**, 3 of them
inversions of assertions the previous cycle pinned the other way.

`ai/providers/openai.go` changed, so the harness request diff was triggered and
re-run: **49/49**.

### Process note — two port worktrees started on a stale base

Two of the three worktree-isolated port agents were created at `241fdde`, the
2026-08-22 cycle tip, five days and 17 commits behind `main`. The third detected
it and re-branched from `main` itself. This was caught before integration: both
affected agents' target files (`coding/session_store.go`, `coding/proc_windows.go`)
were verified **byte-identical** across `241fdde..846a2af` before their commits
were cherry-picked, and the authoritative gate was re-run on `main` afterwards,
so nothing shipped on the strength of a gate run against stale siblings. Worth
knowing the isolation layer can hand out a stale base without saying so; verify
it rather than assuming it.


## Drift at last sync check (2026-08-25) — pin advanced to a79b37334

Delta `4af9d21d3..a79b37334`, **22** first-parent changes, **no merges**: the
first-parent count and the total commit count are both 22, so nothing rode in
under a merge. **Release v0.84.3 CROSSED** at `4e58f324f` — all nine
`packages/*/package.json` go 0.84.2 → 0.84.3, the tag `v0.84.3` exists and
contains that sha — so the catalog is regenerated, the npm reference build moves
for the first time in five cycles, and a **port tag (`v0.84.19`) is cut with a
release tweet**. Verdicts: **6 port → 6 Go commits + 1 review-fix commit; 3
port-but-CATALOG-ONLY; 13 n/a; 0 decide**.

Every non-`ai`/`coding-agent` ported tree is **tree-identical** across the range —
`packages/{agent,protocol,client,server}/src`, `packages/telemetry/src` and
`packages/session-backends/sqlite-node/src` — so the **harness backlog stays 11**.
`packages/telemetry` and `packages/session-backends` show as moved only through
their CHANGELOG and `package.json` version bumps. The trees that move are
`packages/ai/src`, `packages/ai/scripts` and `packages/coding-agent/src`.

Both reconciliation passes ran. The **detector** (`--name-only`, no pathspec)
printed **82** paths, every one classified, with no new top-level package, no
file moved out of `packages/`, and no new repo-root directory; the one new file,
`packages/ai/scripts/openrouter-reasoning-options.ts`, lands in the known
catalog-generator home. The **accounting** sweep over `packages` minus the four
structural exclusions reported **59 files, 1348(+)/157(−)**, each mapped to a
verdict below.

### Port worklist (6 → 6 Go commits + review fixes)

| upstream | subject | Go | notes |
|---|---|---|---|
| `4e58f324f` | Release v0.84.3 | `3016ea2` | **Catalog + whole-harness surface.** Decided the regen by EXECUTING `JSON.stringify(MODELS)` against both builds' `dist/models.generated.js`, never from git (2026-07-30 ruling): 536,642 → 558,804 B, **1267 → 1312 models**, 39 providers unchanged, **81 added / 36 removed / 88 changed**. **Endpoint-pinned at both ends** — the outgoing `ai/models_catalog.json` is byte-identical (`cmp`) to the same derivation from 0.84.2, so the ported diff IS the upstream release diff. Drains the **entire 8-item catalog-only queue**, every member verified an ancestor of the release sha. **No schema drift at the model level** (no new or removed top-level keys), but ONE new key in the *data*: `compat.allowedFallbackModels`, **0 → 2 entries** (`anthropic/claude-fable-5` chaining `claude-opus-4-8` then `claude-opus-5`; `anthropic/claude-opus-5` chaining `claude-opus-4-8`). That **activates the refusal-fallback path ported in `a20597b`**, dormant in practice until now — every existing test built its own compat. `TestAnthropicCatalogFallbacksAreLive` drives the REAL catalog compat onto the wire rather than re-asserting the JSON, and is red against the 0.84.2 catalog for the right reason. Orphan sweep: all 39 non-`radius` `defaultModelPerProvider` entries still resolve (`radius` has no static catalog entry by the 2026-07-14 ruling) |
| `80e62761f` | feat(coding-agent): add optional PowerShell tool (#8512) | `dcfce62` | **Request-body golden surface**, and the largest port of the cycle. Upstream factors bash into a generic shell-tool factory and builds a second built-in tool on it. Go: `coding/tools.go` grows `shellToolConfig` + `shellTool(cwd, config, sessionEnv)` with `bashTool`/`powershellTool` as thin wrappers (mirroring upstream's own retained `createBashToolDefinition` wrapper), `ToolNames` gains `"powershell"` **after `bash`**, matching upstream's `allToolNames` insertion point; `coding/systemprompt.go`'s file-exploration guideline becomes the 3-way branch. **The golden that moves** is the bash `command` schema property description, `"Bash command to execute"` → `"Shell command to execute"` — on the wire in every request carrying the bash tool. **Two goldens proven NOT to move, structurally rather than by assumption:** the composed bash tool `description` is byte-identical because `bashToolConfig.shellName` is `"bash"` (checked against npm 0.84.3's `dist/core/tools/bash.js` template); and the DEFAULT system prompt is untouched because upstream's `defaultActiveToolNames` is still `["read","bash","edit","write"]` at `a79b37334:src/core/sdk.ts:256` — so Go's `defaultActiveToolNames` does NOT gain powershell, only the all-tools list does, and `coding/systemprompt_golden_test.go` passes UNEDITED |
| `97fa14e39` | fix(coding-agent): reject truncated compaction summaries (#7048) | `175aca9` | A `length` stop reason is now a summarization failure, so a truncated summary can never become a compaction checkpoint. Go: `summarizationFailed(reason)` in `coding/compaction.go`, gating the port's single choke point `Session.completeSummarization` where the old test was the bare `msg.StopReason == ai.StopError`. **Call-site mapping (pi 3 → Go 1), mapped by behavior not by name**: pi's `generateSummaryWithUsage` and `generateTurnPrefixSummary` both delegate to that one gate in Go, proven by two independent reds (the `summarize` path returned partial text; the `compact` path installed the truncated summary and dropped 9 of 12 messages); pi's `generateBranchSummary` has **no Go analogue** — branch summarization is not ported. Abort semantics unchanged. See the deliberate divergence below for the half deliberately not ported |
| `c5ad7c1b0` | fix(ai): concatenate openai completions reasoning deltas (#8605) | `03775b0` | **Session-format AND request-body golden surface.** OpenRouter streams `reasoning_details` as deltas; consecutive same-type text/summary deltas now fold into the entry they extend. The crux is that pi uses **two deliberately different operators in the same function**: `id ??=` and `index ??=` are nullish fills (an id of `""` and an **index of `0`** are present and survive) while `format ||=` and `signature ||=` are falsy fills (an empty string IS overwritten). Go renders the entry as ordered members (`reasoningDetailMembers` in the new `ai/providers/openai_reasoning_details.go`) because **JS property assignment keeps an existing key in place and appends a newly-created one, which is observable in the serialized bytes**; serialization reuses the `stringifyReasoningDetail`/`jsStringify` path from `41171de` rather than hand-rolling it. Merging is adjacent-only, so an encrypted entry breaks a text run. Four mutation probes each red on exactly one operator |
| `de82e5367` | feat(coding-agent): export image MIME detector (#8600) | `b644578` | Additive public Go API. The function already existed unexported at `coding/tools.go:324`; upstream promoted it to its package index, so the port exports `DetectSupportedImageMimeTypeFromFile` and — verified against `index.ts` at both `de82e5367` and `a79b37334` — **only** that one: the buffer variant `detectSupportedImageMimeType` stays unexported because pi does not publish it, and that negative is itself test-locked |
| `fe37e9f9b` | fix(ai): omit tool_choice without tools (#8607) | `833af61` | **Request-body golden surface.** `tool_choice` is written only when `params["tools"]` is a non-empty slice, matching pi's `options?.toolChoice && params.tools?.length`. Both Go paths that write `tools` were checked: the converted non-empty array, and the `[]map[string]any{}` sent when the conversation carries tool history — **the empty-array path now suppresses `tool_choice` too**, which a nil-only check would have missed |

### Port-but-CATALOG-ONLY — queue drains 3 → 0 at the 0.84.4 regen (2026-08-29)

All three deltas queued on 2026-08-25 are **ancestors of `b79e4cc83`** (v0.84.4),
verified with `merge-base --is-ancestor` rather than by log order, so they land
in this regen and the queue empties: `e8c632ef6` (cloudflare gateway
`workers-ai/` mirror — visible in the new catalog as the
`cloudflare-ai-gateway/workers-ai/@cf/...` ids), `650e7a612` (OpenRouter
reasoning options) and `587be985a` (`deepseek-v4-flash-vision-exp`).
**No new generator deltas** were queued this cycle: `packages/ai/scripts` is
untouched across `56f3f33a9..853a80d26`, so the queue stays at **0**.

### Port-but-CATALOG-ONLY — queue drains 8 → 0, then reopens 0 → 3

The eight deltas queued since 2026-08-18 are all **ancestors of `4e58f324f`** and
therefore land in the 0.84.3 regen: `3de00332f`, `4809c2abc` (as amended by
`ed867e909`), `eb1f87fa9`, `0e4d49541`, `87205484b`, `6db110e6f`, the `70e878d4c`
xai half, and `86d001d36`. The `allowedFallbackModels` entries appearing in the
catalog are the visible output of the `4809c2abc`/`ed867e909` pair, in exactly the
`[{provider, model, cost}]` shape the ledger predicted.

Three new generator deltas land **after** the release sha (verified by ancestry,
not by log order) and so await the next regen:

| sha | subject | what it will add |
|---|---|---|
| `e8c632ef6` | fix(ai): cloudflare gateway type, include workers | Mirrors the Workers AI catalog under the documented `workers-ai/` prefix into `cloudflare-ai-gateway`, tool-call models only, skipping ids the gateway already lists. Its `src/providers/cloudflare-ai-gateway.ts` half is `n/a` — extracting a `CloudflareAIGatewayApi` type alias and passing it explicitly to `createProvider` is a TypeScript inference change with no behavior and no Go analogue |
| `650e7a612` | fix(ai): derive OpenRouter reasoning controls (#8614) | New `packages/ai/scripts/openrouter-reasoning-options.ts` feeding generator-side reasoning metadata |
| `587be985a` | feat(ai): add deepseek-v4-flash-vision-exp | One model |

### n/a (13)

| sha | subject | reason |
|---|---|---|
| `7623e8a0f` | docs: audit unreleased changelogs | CHANGELOG prose only |
| `31d4ed586` | Add [Unreleased] section for next cycle | CHANGELOG prose only |
| `bfb004d44` | fix: extract Windows release ZIPs in CI | `.github/workflows/` |
| `b7170b86a`, `c5de2cc67`, `45207864f`, `c1729a0f7` | chore: approve contributors | `.github/APPROVED_CONTRIBUTORS`, two lines each |
| `1bb61986c` | docs(coding-agent): update provider links to api | `docs/custom-provider.md` prose |
| `dcd461925` | feat: show llama presets if autoload enabled (#8558) | `src/extensions/llama/{client,provider}.ts` — extensions runtime, on the non-port list. The port has no llama extension; its only `llama` references are the openai-compat comments in `ai/types.go`, `ai/providers/openai.go` and `openai_compat.go` |
| `5cd6a2a50` | fix(ai-test): update glm 5.3 price | Upstream **test** only (`test/zai-coding-plan-models.test.ts`), asserting glm-5.3 now carries API-equivalent reference costs instead of zero. **Confirmed not to bite this cycle by executing the build**: glm-5.3's cost in the 0.84.3 catalog is still `{0,0,0,0}` on both `zai` and `zai-coding-cn`, because this commit lands after the release sha. See the watch item below |
| `cacb5917f` | fix: export ToolExecution*Event types from public API (#6847) | **A genuinely empty commit**: its tree hash equals its parent's (`86f45665a`), so it changes nothing. The `ToolExecutionEndEvent`/`StartEvent`/`UpdateEvent` exports it claims to add already exist at `a79b37334:src/index.ts:138-141`. Recorded rather than skipped, because "subject promises an export" is exactly the shape that would otherwise look like a missed port |
| `240eb29c4` | fix(coding-agent): append run-time custom messages after the turn's tool results | `src/core/agent-session.ts` — agent-session-runtime, on the non-port list. **Recorded absence, checked rather than assumed**: the port has no custom-message surface at all — no `CustomMessage`, no `_pendingCustomMessages`, no `_flushPendingBashMessages` anywhere in non-test Go — and `coding.Session`'s API (`Run`/`RunMessages`/`Steer`/`FollowUp`/`Continue`/`Abort`) has no `sendCustomMessage` analogue for the ordering fix to apply to |
| `a79b37334` | feat(coding-agent): expose RPC queue clearing | `src/modes/rpc/{rpc-client,rpc-mode,rpc-types}.ts` — host mode surface. The port has **no RPC surface whatsoever** (`grep -rl rpc` over `*.go` returns nothing), so there is no Go home for a `clearQueue` request type |

### Deliberate divergence added this cycle — pi's summarization failure LABELS are not ported

`97fa14e39` has two halves: a `length` stop becomes a failure, and the failure
messages become uniformly `"<label> failed: <reason>"` across three labels
(`"Summarization"`, `"Turn prefix summarization"`, `"Branch summarization"`).
**The behavioral half is ported in full; the strings are not**, and the reason is
structural rather than a shortcut. The port's `completeSummarization` returns
`(string, bool)`; failure propagates to `Session.compact`, which is installed as
an `agent.TransformContext` (`func(ctx, []AgentMessage) []AgentMessage`) with no
error return, and the `coding` package has no logger, no telemetry sink and no
error callback. In pi the strings surface because compaction throws out of
`session.compact()` to the UI; here compaction is silent by design and simply
keeps the current view, so a label introduced now would be read by nobody.

This is **not a new gap**: the same function already carries a recorded ruling of
exactly this shape for the tool-call guard ("pi puts this guard in each of
`completeSummarization`'s callers, differing only in the message it throws; the
port has one place to put it and no channel for those strings"), and that comment
was extended to cover the stop-reason failure citing `97fa14e39` and #7048.
Consequence for coverage, stated plainly: the length-rejection is test-locked at
both the `summarize` and the `compact`/checkpoint level; the three labels and
pi's `||` empty-`errorMessage` → `"Unknown error"` fallback are NOT, because
neither is observable in the port. The `||` in any case only shapes a string, never
a branch. **Tripwire**: upstream's sibling `session_compact_failed` extension
event (#8175, a separate commit not in this delta) is the thing that would give
these strings a real destination — if that is ever ported, the labels come with it.

### Review gates

Both gates ran on `192e0cb..833af61` as independent subagents (never the porter),
and — new this cycle — **every filed finding was then put through its own
adversarial verifier instructed to refute it**. **pi-parity-review: clean**, no
blockers and no should-fixes. **pi-go-review: 1 should-fix + 3 nits survived; 5
findings refuted.** Fixes folded into `5418dde`.

**The should-fix was a real bug the port's own tests did not catch.** `dcfce62`
added `"powershell"` to `ToolNames` and to `createTool`'s switch but left the
exported `CreateAllTools` returning seven tools with a doc comment still reading
"all seven built-in tools" — while upstream `80e62761f` adds powershell to **both**
`createAllTools` and `createAllToolDefinitions` alongside `allToolNames`, and npm
0.84.3 ships it that way (`dist/core/tools/index.js:117-128`). An SDK consumer
asking for the full built-in set silently got 7 of 8. The verifier confirmed by
execution and disposed of the obvious defence: powershell is safe to construct off
Windows because, exactly as in pi, the shell is resolved inside `Execute` — only
running it reports the platform error. `TestCreateAllToolsCoversToolNames` now pins
the two literals against each other and is red against the pre-fix code.

Nits fixed: `shellToolConfig.guidelines` was a knob with one value across both (and
only) instantiations — the same "no Go consumer" category the port already dropped
`label`/`prompt`/`promptSnippet` for — which additionally aliased a single backing
array across every tool built from the package-level configs; inlined at its sole
consumer. `TestPublishedMimeSurface` kept a positive branch that could never fire,
since the sibling test's call from `package coding_test` makes removing the export
a compile error first; dropped, keeping the negative "and no more" half and stating
the `GOOS`-filtering limit of its source scan.

**Refuted findings, recorded so the next cycle does not re-derive them:** (1) the
`tool_choice` guard re-reading `params["tools"]` instead of the typed local — the
claimed silent-regression risk was disproven by execution, and the payload-keyed
form matches the adjacent `anthropic.go` precedent; (2) the catalog-activation test
selecting models by substring-matching raw compat JSON — the verifier reproduced
all three regressions the doc comment claims, including the "present but no longer
decodable" case, which the substring selector deliberately survives so the wire
drive can catch it; (3) `resolveShell`'s four-value return with a bare bool —
`ShellConfig` is a TypeScript language constraint (TS has no multiple returns), not
a design choice, and `runtime.Caller` is the stdlib precedent; (4) the shared shell
machinery keeping bash-prefixed names — **upstream deliberately did the same in
this exact commit** (`bashSchema`, `BashOperations`, `BASH_UPDATE_THROTTLE_MS` and
the file `bash.ts` itself all stay bash-named while serving both shells), so
renaming would increase the distance to the stem; (5) lone-surrogate handling in
`jsStringify` — real JS/Go divergence but pre-existing, already documented at
`ai/providers/json.go:100-107`, in a file this cycle does not touch, and reproduction
showed the merge adds no new exposure because U+FFFD substitution happens per detail
*before* any concatenation.

### Pre-existing divergence surfaced this cycle (recorded, NOT fixed) — `reasoning_details` is never re-validated

> **SUPERSEDED on 2026-08-27 — do not read this as live.** Upstream `7aab6c26e`
> deleted the per-arrival re-parse this entry is about, so pi no longer
> re-validates and no longer discards the sequence. The underlying
> render-on-arrival mismatch survives in a narrower form (a saturated `index`
> merged with an adjacent same-type delta). See "SUPERSEDED — the
> `reasoning_details` re-validation divergence has changed shape, not closed"
> in the 2026-08-27 cycle section above.

pi re-derives the preserved sequence from `block.thinkingSignature` on **every**
arriving detail, which re-runs `isOpenAIReasoningDetail` over every stored entry
and discards the whole sequence if one now fails. The port holds the sequence in
its accumulator and only appends. The single member a merge can render into a
pi-invalid shape is `index`: a magnitude `JSON.parse` saturates to `Infinity` is
still `typeof "number"` on arrival, but `JSON.stringify` emits it as `null`, and
pi's next re-parse then rejects it. **Predates `c5ad7c1b0`** — the accumulator and
the old push-only code held the sequence the same way — and reaching it needs a
provider to send `index: 1e400`. The accumulator comment now says so rather than
claiming an unconditional equivalence.

### Watch item (not a decide) — glm-5.3 pricing arrives at the NEXT regen

`5cd6a2a50` is `n/a` as a test-only commit, but it encodes a models.dev data
change that the **next** release will pull into the catalog: glm-5.3 gains
API-equivalent reference costs (input 1.4, output 4.4, cacheRead 0.26,
cacheWrite 0) in place of zeros. Verified by execution that 0.84.3 still carries
zeros, so nothing moves now. The port resolves glm-5.3 as the default for both
`zai` and `zai-coding-cn` (`coding/resolve.go:39-40`) with guard tests at
`coding/resolve_test.go:265-290` written against the zero-cost derivation —
**expect those to need re-baselining at the regen that ships this.**

### FIXED — the `client` disposal race, open since 2026-08-10

`TestConcurrentCloseWaitsForTeardown` had been flaky since 2026-08-10 and was
carried again as a follow-up on 2026-08-20 (see that cycle's "Follow-ups
recorded, NOT fixed", which had the cause right and proposed a candidate fix).
Root-caused and fixed here. **Both earlier items are closed by this.**

**Verdict: the implementation was wrong; the test was right.** Three independent
investigations — Go-concurrency, pi-parity and a lens tasked specifically with
arguing that the *expectation* was wrong — converged, each by execution rather
than by reading.

**Cause.** `Client.notifying` was a **client-global bool**, but the fact it had
to express — "am I the goroutine currently running teardown?" — is
**goroutine-scoped**. That type mismatch is the entire bug. The flag was raised
around the whole of `conn.Disconnect`, so ANY unrelated goroutine calling
`Close` inside that window read `notifying == true`, concluded it must be the
re-entrant callback, and returned without waiting on `<-c.closed` — observing a
client that was not yet disposed. Instrumentation put the escape hatch firing on
roughly 22% of concurrent non-winning `Close` calls, wrong about 3% of the times
it fired. The code comment stating the cost ("costs it the wait but never
correctness") was simply false, and is why this survived review for four cycles:
**the defect was written down as a design note.**

**pi parity settles the contract, and it was established by running real pi**,
not by reading it — `packages/client` extracted at `a79b37334` and driven under
node with an in-memory transport. pi's `dispose()` memoises an *already-resolved*
promise **before** its teardown body, and that body is entirely synchronous, so
run-to-completion hands every caller — the re-entrant one included — a view of a
fully torn-down client. So "Close returning means fully disposed" is a faithful
port of pi's guarantee, not a Go embellishment, and the `<-c.closed` wait is the
minimum needed to reproduce in Go what JavaScript gets from its event loop.
`notifying` corresponded to **nothing in pi** — pi's whole idempotence guard is
`if (this.#disposePromise) return this.#disposePromise`.

**Fix: delete the flag rather than make it smarter.** Disposal now publishes
completion BEFORE it notifies. `Connection` grows `failDeferred`/
`DisconnectDeferred`, which tear the connection down and hand the state-change
callback back instead of delivering it; `Close` then does the client-side
teardown itself, closes `c.closed`, and only then notifies. A nested `Close`
from a listener finds an already-closed channel and returns immediately — no
deadlock — and an unrelated caller waits and sees full disposal. Nothing has to
tell the two apart, which is the point: **no goroutine identity is needed, so no
`runtime.Stack` goid hack.** `notifying` and `setNotifying` are gone.

**Rejected alternatives, and why — the near-misses matter more than the fix.**
- *Narrow the `notifying` window to the listener dispatch only.* Drives the
  observed failure to zero and is a one-line change. **Rejected**: the dispatch
  is arbitrary user code and unbounded, so an unrelated goroutine can still land
  in it. It converts a reproducible bug into a rarer one — the symptom, not the
  cause.
- *Move `close(c.closed)` ahead of `conn.Disconnect`.* **Validated as wrong.**
  It makes the test pass at `-count=500` while silently breaking the
  connection-state invariant (the reversed-order probe reported 83 violations in
  4000). It looks green, which is exactly what makes it dangerous.
- *Goroutine-id keying.* Semantically exact and it works — it is what the probes
  used to isolate the variable — but parsing `runtime.Stack` for identity is not
  something a port that prizes idiomatic Go should ship.
- *Weaken the test.* Both weakenings considered ("after `wg.Wait()` the client is
  disposed"; "at least one caller observes full disposal") were **coded and run
  against the broken implementation and passed 9000 iterations**. They assert
  nothing. Had the contract genuinely been unwanted, the honest form would have
  been to delete `c.closed`, the wait, and the promise in `Close`'s doc — not to
  keep a test that reads like a guard and is not one.

**The tests are now stronger and no longer flaky.**
`TestConcurrentCloseWaitsForTeardown` gains two assertions and short-circuits on
none of them. `Snapshot()` is the load-bearing addition: `Attached()` and
`ConnectionState()` are both settled before listeners run, so a window-narrowing
fix satisfies those two while still exposing an undisposed `State` — only
`Snapshot()` separates "fully disposed" from "far enough along to look disposed".
Measured against the pre-fix code over 200 runs: the old assertion fired **7**
times, the new `Snapshot()` one **148**. A new test,
`TestCloseFromAListenerDoesNotStrandAnotherCloser`, pins both contracts at once —
the listener's nested `Close` must return while an unrelated closer must still be
held — and starts the unrelated closer only once the listener is running, so it
cannot be the goroutine performing teardown. It is **deterministically red**
against the old code (0.00s, every run), where the old test needed ~100 runs to
catch anything.

**Deliberate divergence added, recorded in `Close`'s doc comment.** pi runs
`#state.dispose()` AFTER `#connection.disconnect`, so a pi connection-state
listener still sees live session subscribers; the port now disposes `State`
before notifying, so such a listener sees them cleared. The observable window is
one statement wide even in pi — a subscription registered from that listener is
destroyed by the very next line of pi's `dispose()` — and it is what buys the
guarantee. It is the same class of divergence `State.Dispose` itself already
records: pi's single-threaded assumptions do not survive the port.

Verified: full `-race` suite green, and `go test ./client/ -count=300 -race`
green (39s) where the flake previously appeared a few times per 200.

### Harness — 47 → 49 scenarios, FULLY DIST-BACKED, 49 PASS / 0 KNOWN / 0 FAIL

Re-pinned `PI_NPM_VERSION` 0.84.2 → **0.84.3** and `PI_UPSTREAM_SHA` `4af9d21d3`
→ `a79b37334`. **All 27 `src` scenarios flipped to `dist`** under the README rule:
every sha they cover (`b7bb00b93`, `4ca636c5e`, `ed867e909`, `e5dde9a76`,
`90305d90a`, `b23741269`) is an ancestor of the release commit, checked by
`merge-base --is-ancestor` rather than by reading dates. Backends go **27 `src` /
20 `dist` → 0 / 47**, the first fully published-build suite since the 0.84.2
milestone, and all 47 pass against 0.84.3 — an independent confirmation of both
the ports and the regen.

**Two new `src` scenarios** for this cycle's own request-body change, since
`fe37e9f9b` post-dates the release and the published build cannot be its
reference: `tool-choice-without-tools-absent` and `tool-choice-with-tools-control`.
The pair was checked to **discriminate rather than pass by mutual absence** — the
control shows `tool_choice: "none"` present with two tools on both sides — and the
absent arm was **temporarily re-pointed at the 0.84.3 dist to watch it FAIL**
(`pi: "none"` vs `go: <absent>`), which proves it is load-bearing and that the
behavior genuinely changed post-release. Final backends: **47 `dist` / 2 `src`**.

`ai/providers/openai*.go` changed, so the request diff was triggered and re-run
after the review fixes: **49 PASS / 0 KNOWN / 0 FAIL**, exit 0. The harness
README's illustrative scenario table had drifted (five rows still said `src` for
scenarios flipped in an earlier cycle); corrected, the two new rows added, and a
note added that the column is a snapshot.

### The differential harness is now IN the repo (`difftest/`)

Promoted from `~/.cache/pi-diff`, where it was durable but unversioned: 49
scenarios, a runner, a canonicalizer written twice as deliberate twins, and a
known-divergence baseline, all on one disk with no history and no backup —
while every cycle of this ledger cites its results as evidence. It now lives at
`difftest/`, versioned in lockstep with the code it verifies, which is the only
arrangement that holds: each sync re-pins it and flips scenarios based on the
port's own state, and the two drifted whenever they were kept apart (this
cycle's README table had five stale rows for exactly that reason).

**The recorded objection was resolved, not overruled.** The harness README
carried an emphatic note that it must *not* live in the repo because
`github.com/sky-valley/pi` is published, public and MIT. `difftest/` therefore
carries **its own `go.mod`**, and a subdirectory containing a `go.mod` is
excluded from the parent module. Verified rather than asserted: a module zip
built with `golang.org/x/mod/zip` — the same library the module proxy uses —
over the repo root contains **297 files and zero `difftest/` entries**, so
nobody running `go get github.com/sky-valley/pi` downloads a byte of it. The
note has been rewritten in place to record both the move and the reasoning.

Made clone-portable in the process: the `replace` is now `..` instead of an
absolute path to one machine's home directory, and `PI_GO_REPO` defaults to the
repo the harness sits in, so a fresh clone needs no configuration. The only
remaining absolute paths are the two shared caches (`~/.cache/pi-npm`,
`~/.cache/pi-upstream`), both overridable by env and both shared with the sync
job. `out/`, `pisrc/` and the compiled binary are gitignored — all three are
reproducible on demand. Verified green from the new location at **49 PASS /
0 KNOWN / 0 FAIL**.

### Base gate

`gofmt -l` clean, `go build ./...` and `go vet ./...` clean, `go test -race
./... -count=1` green across all **10** test-carrying packages (13 listed, 3 with
no test files), before the cycle and after the review fixes. **20 new test
functions** across 7 files; every behavior change observed red for a behavioral
reason before being made green, including the two cases where a naive red would
have been a compile error (the MIME export, asserted over package source; and the
`CreateAllTools` invariant).

### RESOLVED (2026-08-25) — the `pi-triage` scope definition now matches the rulings

Carried as OPEN since 2026-08-22 and closed this cycle on the owner's
instruction. The problem was that `pi-triage`'s `port` **definition** — the text
a triager actually reads when assigning a verdict — named only three trees
(`packages/ai/src`, `packages/agent/src`,
`packages/coding-agent/src/core|main|sdk`) while the recorded rulings had put
five more in scope (protocol, client, server, telemetry, session-backends). The
skill also carried a SECOND, correct map further down in its rulings section, so
it contained both the wrong answer and the right one, several hundred lines
apart. It never bit only because those five trees happened to be tree-identical
in every cycle since — luck, not coverage.

Not treated as a boundary change, and that is the point: writing already-decided
rulings into the definition does not move the boundary, it makes the document
match the boundary. No tree was added to or removed from scope.

What changed in `.claude/skills/pi-triage/SKILL.md`:
- The `port` definition is now a **single table** of every in-scope tree with its
  Go home, living in the verdict rules where a triager reads it. The duplicate
  map in the rulings section is replaced by a pointer to it, with an explicit
  "do not reintroduce a second copy" and a note that this exact duplication is
  what drifted.
- The dead `src/sdk` element is gone (`sdk.ts` lives at `src/core/sdk.ts`,
  already covered by `core`).
- `modes/**` is named as excluded and enumerated as it exists at the pin
  (`interactive/`, `rpc/`, `print-mode.ts`, `json-event.ts`), with the note that
  the TUI is `packages/tui` and not a mode, and that `cmd/pi` is a hand-rolled
  SDK CLI rather than a port of pi's mode layer. This is the gap that had to be
  resolved from first principles for `a79b37334` this cycle.
- `port-but-CATALOG-ONLY` is now a named verdict rather than folk knowledge, with
  the ancestry rule (`merge-base --is-ancestor`, never log order) and the
  execute-don't-read-git rule for deciding a regen.
- The `decide` entry gains the standing formula and the four axes that are
  SETTLED and must not be re-escalated (runtime Go does not target; `DRAFT:`
  prefix; an upstream revert; a pre-existing parity gap in a ported function).

**Two stale carve-outs were found and removed while verifying the table**, both
inherited from the 2026-08-01 ruling and both already flagged as dead text
elsewhere in the same skill: `server/src/legacy/**` no longer exists (upstream
deleted it in `05bf9df65`, 2026-08-04), and `server/src/testing/**` is now ported
as `server/internal/servertest/`. Every remaining row was checked against the pin
and the repo — all 13 upstream trees present at `a79b37334`, all 11 Go homes
present — rather than copied forward on trust.


## Drift at last sync check (2026-08-24, second check) — pin advanced to 4af9d21d3

Delta `a470b121b..4af9d21d3`, **1** first-parent change, **no merges**: the
first-parent count and the total commit count are both 1, so nothing rode in
under a merge. **No release crossed**: all nine `packages/*/package.json` are
0.84.2 at both ends, **no tag contains either sha**, and `models.generated.ts` is
**blob-identical** at both ends (`12952045922123bc1e7a696e75bdae386e68d552`), so
no catalog regen, no npm reference-build move, and **no port tag** and no release
tweet this cycle. Verdicts: **0 port; 1 n/a; 0 decide** — a triage-only pin
advance with no Go commits and no review gates.

The second sync check of 2026-08-24; the first advanced the pin to `a470b121b`
earlier the same day. Every ported tree is **tree-identical** across the range —
`packages/{ai,agent,protocol,client,server,telemetry}/src`, `packages/ai/scripts`
and `packages/session-backends` — and this time `packages/ai`'s **whole** tree is
identical too (`a37cc08d`), not just its `src`, which is what makes the harness
re-pin a byte-level no-op rather than the one-line devDependency delta of the
previous cycle. So the harness backlog stays **11** and the catalog-only queue
stays **8**. The only tree that moves is `packages/coding-agent/src`
(`bfb72e5a` → `500b94d6`), by exactly the two files in the row below.

Both reconciliation passes ran under the corrected guard from `6d7e7f1`. The
**detector** (`--name-only`, no pathspec) printed **5** paths, every one
classified, with no new top-level package, no file moved out of `packages/`, and
no new repo-root directory — `scripts/` predates the range, and is the same
release-tooling directory `39d869f02` touched last cycle. The **accounting**
sweep over `packages` minus the four structural exclusions reported **3 files,
395(+)/5(−)** — `src/main.ts`, `src/package-manager-cli.ts` and their test — each
mapped to the verdict below.

### n/a (1)

| sha | subject | reason |
|---|---|---|
| `4af9d21d3` | feat(coding-agent): update managed installations in place | Installer/self-update packaging, on the non-port list. The bulk (`src/package-manager-cli.ts`, +217) is new managed-install machinery — `getActiveManagedInstallRoot`, `runManagedSelfUpdate` (fetch `package.json` + `package-lock.json` from `https://pi.dev/api/installer/releases`, `npm ci --ignore-scripts` into a staged dir, `--version` smoke test, atomic `current-version` rename), `proper-lockfile` mutual exclusion, `cleanupManagedInstall`, and a `--force`-rejection message — the same surface ruled `n/a` for `bc0db643` ("install checked pi update version", *same file*), `aae62dfa` and `4a9c962b`. The `src/main.ts` hunk is one import plus one `cleanupManagedInstall()` call in the CLI startup sequence, next to the already-`n/a` `cleanupWindowsSelfUpdateQuarantine`; judged per hunk rather than by path, since `main.ts` IS swept. Confirmed structurally, not by precedent alone: the port has **zero** self-update/package-manager surface — `selfupdate\|detectInstallMethod\|installMethod\|PackageManager\|quarantine\|lockfile` returns no hits across all non-test Go — and `cmd/pi/main.go` is a 365-line hand-rolled SDK CLI ("Go port of the pi coding agent CLI"), not a mirror of pi's startup sequence. The new code *imports* three helpers with Go homes — `canonicalizePath` and `getCwdRelativePath` (`utils/paths.ts`) and `getPiUserAgent` (`utils/pi-user-agent.ts` → `ai/providers/pi_user_agent.go`) — but **modifies none of them**: all four pre-exist at `a470b121b` (`paths.ts:28,108`, `child-process.ts:28,49`) and none appears in the diffstat, so this adds a consumer, not a port. `src/config.ts` is untouched, leaving last cycle's `findNodePackageDir` port unaffected. Remainder: `test/package-command-paths.test.ts` (+180) tests the above; `scripts/publish-release-announcement.mjs` uploads installer artifacts to R2 (release tooling, precedent `39d869f02`); `docs/packages.md` is one prose line |

### Watch item (not a decide) — the managed layout is a second consumer of `getPackageDir`

Recorded because last cycle's *only* port was this exact function. A managed
install puts pi at `<PI_MANAGED_INSTALL_ROOT>/releases/<version>/node_modules/…`,
and `getActiveManagedInstallRoot()` classifies it by calling the ported
`getPackageDir()` and testing `getCwdRelativePath(canonicalizePath(getPackageDir()),
releasesDir)`. Nothing in that path changes the ported function — it is a caller,
and `src/config.ts` is not in the diff — so there is no port work and no boundary
question. But `getPackageDir` now has a consumer whose correctness depends on the
walk terminating inside a release directory, which raises the odds that a future
commit adjusts the walk itself. **If one does, that hunk is `port`**, and it
should be read against the two divergences already recorded for `PackageDir()`
(the `PI_PACKAGE_DIR` `normalizePath` bypass, and the Bun-vs-Node structural
watch item) rather than in isolation.

### Harness

Re-pinned `a470b121b` → `4af9d21d3` and re-run green at **47 PASS / 0 KNOWN /
0 FAIL**, exit 0. Unlike the previous cycle's re-pin this one is **byte-identical**
and was proven so rather than assumed: `run.sh` archives the whole `packages/ai`
directory, and that tree hash is unchanged across the range
(`a37cc08d10979e5bbc3a1b55b95b376fbf89d487` at both shas), so every `src`-backend
scenario re-extracts the same bytes. `PI_NPM_VERSION` stays 0.84.2, so the 20
`dist` scenarios cannot move either; backends remain **27 `src` / 20 `dist`**.
Nothing under `ai/providers/openai*.go` changed — nothing in the port changed at
all — so the 6-scenario request diff was not triggered.

### OPEN — carried from 2026-08-22, still needs an owner ruling

Unchanged and still unaddressed, now for a third cycle: `pi-triage`'s `port`
**definition** reads "`packages/ai/src`, `packages/agent/src`,
`packages/coding-agent/src/core|main|sdk`", which is narrower than the recorded
rulings — it names none of protocol, client, server, telemetry or
session-backends, and repeats the dead `sdk` element (`sdk.ts` lives at
`src/core/sdk.ts`). A triager reading only the skill would mark a
`packages/protocol` change `n/a`. It did not bite this cycle either, and for the
same reason as last: all five of those trees are tree-identical across the range.
That is still luck, not coverage. Aligning the definition with the rulings is a
non-port-boundary edit and belongs to the owner under the skill's own "escalate,
don't ship" rule.

## Drift at last sync check (2026-08-24) — pin advanced to a470b121b

Delta `c49906ec7..a470b121b`, **17** first-parent changes, **no merges**: the
first-parent count and the total commit count are both 17, so nothing rode in
under a merge. **No release crossed**: all eight `packages/*/package.json` are
0.84.2 at both ends, no tag contains `c49906ec7`, and `models.generated.ts` is
**blob-identical** at both ends (`12952045922123bc1e7a696e75bdae386e68d552`), so
no catalog regen, no npm reference-build move, and **no port tag** and no release
tweet this cycle. Verdicts: **1 port; 16 n/a; 0 decide**.

An unusually packaging-heavy week upstream — bundling a Node runtime, pruning the
dependency tree, deferring syntax grammars and jiti, publishing installer
artifacts. `packages/{ai,agent,protocol,client,server,telemetry}/src`,
`packages/ai/scripts` and `packages/session-backends` are all **tree-identical**
at both ends, so the harness backlog stays **11** and the catalog-only queue
stays **8**. `packages/ai`'s whole tree differs by exactly one line — a
types-only `@types/node` devDependency — which matters only because the
differential harness archives that directory; see the harness note below.

Both reconciliation passes ran under the corrected guard landed in `6d7e7f1`
earlier the same day. The **detector** (`--name-only`, no pathspec) printed **51**
paths, every one classified, with no new top-level package, no file moved out of
`packages/`, and no new repo-root directory. The **accounting** sweep reported
**33 files, 1052(+)/413(−)**. The detector earned its keep immediately: the
2026-08-22 `coding-agent/src/*.ts` blind spot **fired usefully**, surfacing
`src/config.ts` — which is this cycle's only port, and which the retired sub-path
pattern would have printed nowhere.

### port (1)

| sha | subject | Go files | golden surface? | commit |
|---|---|---|---|---|
| `7d4c0e05d` | feat(coding-agent): bundle Node runtime (#8474) | `coding/resources.go` (`PackageDir`, new unexported `findNodePackageDir`) + `coding/resources_test.go` | golden **insulated**, live prompt bytes **can** move — see below | `701822b` |

Only the `packages/coding-agent/src/config.ts` hunk is in scope; the rest of
`7d4c0e05d` is tui, `core/extensions/loader.ts`, `scripts/` and lockfiles, all
n/a on their own terms. Upstream extracted `getPackageDir`'s Node walk into
`findNodePackageDir` and added one case: a directory named `dist` whose parent
ALSO has a `package.json` yields the parent, because `build:binary` copies Bun's
metadata — `package.json` included — into `dist/`, and Node still needs the
package root or its dist-relative asset paths become `dist/dist/`.
`coding.PackageDir()` ran pi's OLD algorithm while its doc comment claimed to
mirror `getPackageDir`, so the mirror had gone stale — a parity gap inside an
already-ported, exported, unconditional function, which the standing formula
(2026-08-11) resolves as a `port` rather than a `decide`.

Kept **unexported**. Upstream's `export` is a TypeScript module artifact — its
test file is a separate module and needs the symbol — whereas
`coding/resources_test.go` is `package coding` and reaches it for free. So the
port's public surface stays exactly `PackageDir()`: no public Go API change, and
therefore nothing to escalate under the hard rule.

**Golden reach, stated precisely.** `PackageDir()` feeds
`ReadmePath()/DocsPath()/ExamplesPath()`, interpolated into the system prompt at
`coding/systemprompt.go:150`. The captured goldens do NOT move —
`systemprompt_golden_test.go:27-29` passes `/pkg/...` through opts so
`PackageDir()` is never called on that path, and the second golden (`:74`) is the
custom-prompt branch, which omits the docs block entirely. But
`coding/session.go:379` leaves all three opts empty, so **live** default-prompt
bytes do move with `findNodePackageDir` in exactly the two layouts the new
behavior targets.

### n/a (16)

| sha | subject | reason |
|---|---|---|
| `39d869f02` | fix: publish installer artifacts | `.github/workflows/build-binaries.yml` + `scripts/publish-release-announcement.mjs`. CI/packaging |
| `cec3a91c0` | feat(coding-agent): defer uncommon syntax grammars | `utils/syntax-highlight.ts` + two `.d.ts` + `modes/interactive`. Judged **by consumer**, per the `utils/` rule: the only importers at `a470b121b` are `modes/interactive/interactive-mode.ts` and `modes/interactive/theme/theme.ts`, both TUI. No Go home — `syntax-highlight`/`SyntaxHighlight` appears nowhere in the port |
| `74786a748` | fix(coding-agent): support -- end-of-options, closes #7269 | `src/cli/args.ts` `parseArgs` gains a `--` branch pushing the remainder to `messages`/`fileArgs`. `src/cli` is outside `core\|main\|sdk` and absent from the path map; the port's CLI is its own hand-rolled loop in `cmd/pi/main.go` ("Go port of the pi coding agent CLI"), not a mirror of `parseArgs`. The one constant Go DOES mirror from this file — `VALID_THINKING_LEVELS`, cited at `coding/resolve.go:64` — is untouched |
| `62bcbf6be` | docs(coding-agent): document -- end-of-options delimiter | README + `docs/usage.md` + help text in `cli/args.ts`. Rider on `74786a748` |
| `faecac2ca` | feat(coding-agent): reduce bundled startup work | `utils/syntax-highlight.ts` + `scripts/build-coding-agent-bundle.mjs`. Same consumer analysis as `cec3a91c0` |
| `77c540704` | meta(changelog): Add missing changelog entry | CHANGELOG only |
| `a1f955e9f` | fix(coding-agent): remove redundant development dependencies | `package.json` + lockfiles only |
| `f8f03460a` | fix: reduce workspace dependency tree | `core/package-manager.ts` swaps the `glob` npm dependency for `node:fs` `globSync` behind a new `expandPackageGlob`, adding dot-path filtering and an explicit sort. No Go counterpart exists: `PackageManager`, `collectFilesFromPaths` and `expandPackageGlob` all return zero hits across the port — npm package installation is not ported. The `ai`/`agent`/`telemetry`/`evals` `package.json` edits are a `@types/node` **devDependency** downgrade (24.12.4 → 22.19.19), types-only and not runtime |
| `309b524f4` | fix(coding-agent): avoid duplicate clipboard binaries | `.github/workflows/build-binaries.yml` + `scripts/build-binaries.sh`. CI/packaging |
| `c1279a65b` | feat(coding-agent): defer jiti until extension loading | CHANGELOG + `scripts/build-coding-agent-bundle.mjs` |
| `bcad846f9` | fix(coding-agent): update end-of-options CLI test | `test/experimental-cli-command.test.ts` only |
| `a69bef789` | fix(coding-agent): discard failed extension factory state (#8424) | `core/extensions/loader.ts`, extensions runtime, on the non-port list since 2026-06-12. All 8 hunks land inside `createExtensionAPI`/`createExtension`/`loadExtension*`; verified by reading the hunk headers rather than trusting the path |
| `460191cfc` | feat(coding-agent): include context in Radius session shares | Radius is on the non-port list (2026-07-14 ruling). The `core/` half is a pure **extraction**: `AgentSession.exportToJsonl` moves verbatim into a new `core/session-export.ts`, gaining an optional `createTrailingEntries` callback whose only caller is the new `modes/interactive/session-share.ts`. Behavior on the no-callback path is unchanged — the header `timestamp` is now computed once and shared, and `prevId` is renamed `parentId`. Moot for the port regardless: `exportToJsonl`/`ExportToJsonl` has no Go counterpart at all, and Go's only JSONL writer is the session recorder (`coding/session_store.go:121`) |
| `27b7a626d` | fix(coding-agent): use Windows-friendly keybinding defaults | `core/keybindings.ts` adds a `useWindowsKeybindings()` WSL/win32 probe and re-defaults nine TUI bindings; `packages/tui/src/keybindings.ts` on the other half. The port has no keybindings — the only `keybinding` hit in Go is the word inside the system prompt's docs list (`coding/systemprompt.go:155`) |
| `81152d88b` | docs(coding-agent): clarify custom footer usage APIs (#8482) | Two **doc-comment** lines in `core/extensions/types.ts` and `core/footer-data-provider.ts` (3 insertions, 3 deletions). No code |
| `a470b121b` | fix(coding-agent): expose finish reason compatibility override (#8487) | Adds `supportsFinishReason` to the coding-agent's TypeBox `OpenAICompletionsCompatSchema` — a **host config validation allowlist**, not behavior. `packages/ai` already had the field, and **the port already has it end-to-end**: `ai/providers/openai_compat.go:44,154,188,225` and `ai/providers/openai.go:543,565`, with `openai_test.go:470` already setting `{"supportsFinishReason":false}` through raw compat JSON. Go decodes `compat` with permissive `encoding/json` and has no allowlist to widen, so there is nothing to unblock |

### Divergences surfaced by the gates (recorded, NOT fixed)

Both are **pre-existing** — present at the pin, outside this cycle's delta — and
both were deliberately kept out of `701822b` rather than smuggled into a port
commit. They are recorded here with the **measured** drift, not an assumed one:
the parity reviewer corrected its own tasking on this point, establishing that
`normalizePath` does **not** realpath (that is `canonicalizePath`, a different
function) and that trim / unicode-space folding / `@`-strip are **opt-in flags
`getPackageDir` does not pass**.

**1. `PI_PACKAGE_DIR` bypasses `normalizePath` entirely.** pi returns
`normalizePath(envDir)` (`config.ts:389`); `coding/resources.go:27-29` returns
the raw environment value. With the options `getPackageDir` actually passes
(`{}`, darwin) the measured drift is: `~` → `/Users/noam`, `~/pi-pkg` →
`/Users/noam/pi-pkg`, `file:///opt/pi` → `/opt/pi`; on win32 add
`normalizeWindowsShellPath` (`/c/pi` → `C:\pi`, and the MSYS/Cygwin/WSL forms).
Reachable: a quoted, systemd, `.env` or Docker `PI_PACKAGE_DIR="~/pkg"` leaves a
literal `~` in the system prompt's doc paths where pi resolves it. Narrow, but
user-visible — rated **above cosmetic** by the reviewer, and this row is the
promise that commit message made.

**2. And it is NOT a one-line fix, which is the more useful finding.** The port
already has a `normalizePath` (`coding/tools.go:39`), but it is **not a drop-in
substitute**: it applies unicode-space folding and `@`-stripping
**unconditionally**, where `a470b121b:packages/coding-agent/src/utils/paths.ts:74-99`
makes both **opt-in** and `getPackageDir` passes neither. Reusing it at this call
site would *introduce* a divergence rather than close one. It also **omits
`normalizeWindowsShellPath` altogether**, which pi applies unconditionally on
win32 — so that gap is wider than `PI_PACKAGE_DIR`: it affects every already-ported
tool call site that resolves a path on Windows. Closing either properly means
giving the Go `normalizePath` pi's options shape and updating its existing
callers — its own slice of work, with its own triage and gates, not a rider on a
pin advance. **Backlog item, owner's call on priority.**

**3. `existsSync` vs `fileExists`, now double-sited (LOW).** `existsSync` is true
for a **directory** named `package.json`; `coding/resources.go:543` is
`err == nil && !info.IsDir()`. The class is pre-existing (primary probe), but
this commit adds a second site at the parent probe: given a package root whose
`package.json` is a directory plus a real one in `root/dist/`, pi returns `root`
and Go returns `root/dist`. Confirmed live by the reviewer. Effectively
unreachable in practice; recorded because the new site is ours.

### Watch item — the Go binary is structurally pi's *Bun* case, not its *Node* case

Not a divergence today, and deliberately not acted on. pi's `getPackageDir`
branches: a Bun binary returns `dirname(process.execPath)` with **no walk and no
dist rule**; everything else takes `findNodePackageDir(__dirname)`. A compiled Go
binary is the analogue of the *Bun* arm, yet `coding.PackageDir()` follows the
*Node* arm — so the newly-ported dist rule now fires on the binary's own
directory. For a `package-root/dist/{package.json, pi}` layout, pi's Bun binary
answers `dist/` while Go now answers the parent; before this commit Go answered
`dist/`, matching **by accident**. Unreachable while the port ships no
`package.json` and no packaging — a `go install` binary hits neither branch — but
if the port ever ships a dist layout, this is the row to re-read.

Related, and also latent: now that the walk is a named function it invites reuse,
and Go's `filepath.Dir` is `Clean`-based where Node's `dirname` is lexical. Six
divergent rows exist (sharpest: start `root/dist/` with a trailing slash → pi
`root`, Go `root/dist`). All are unreachable from `PackageDir()`, which always
passes the `Clean` result of `filepath.Dir(exe)` — hence the precondition now
documented on the helper rather than pinned by tests.

### Harness

Re-pinned `c49906ec7` → `a470b121b` and re-run green at **47 PASS / 0 KNOWN /
0 FAIL**. Unlike the 2026-08-22 re-pin this is **not** a byte-identical
extraction, and the difference was checked rather than assumed: `run.sh` archives
the whole `packages/ai` directory, whose tree hash does change across the range
(`be2a1c50` → `a37cc08d`). The delta is exactly one line — `@types/node` 24.12.4
→ 22.19.19 in `package.json` — a types-only devDependency the harness never
loads, since bare imports resolve through the `node_modules` symlink to the
installed 0.84.2 build. Verified by extracting both shas and diffing: the `src`
trees are byte-identical and `package.json` is the only differing file in the
entire extraction. Nothing under `ai/providers/openai*.go` changed, so the
6-scenario request diff was not triggered.

### OPEN — carried from 2026-08-22, still needs an owner ruling

Unchanged and still unaddressed: `pi-triage`'s `port` **definition** reads
"`packages/ai/src`, `packages/agent/src`, `packages/coding-agent/src/core|main|sdk`",
which is narrower than the recorded rulings — it names none of protocol, client,
server, telemetry or session-backends, and repeats the dead `sdk` element
(`sdk.ts` lives at `src/core/sdk.ts`). A triager reading only the skill would mark
a `packages/protocol` change `n/a`. It did not bite this cycle, because all five
of those trees are tree-identical across the range — but that is luck, not
coverage. Aligning the definition with the rulings is a non-port-boundary edit and
belongs to the owner under the skill's own "escalate, don't ship" rule.

## Drift at last sync check (2026-08-22) — pin advanced to c49906ec7

Delta `b7bb00b93..c49906ec7`, **12** first-parent changes, **no merges**: the
first-parent count and the total commit count are both 12, so nothing rode in
under a merge. **No release crossed**: no `packages/*/package.json` version bump
(`pi-ai`, `pi-coding-agent` and `tui` are all 0.84.2 at both ends), no tag
contains `b7bb00b93`, and `models.generated.ts` is **blob-identical** at both
ends (`12952045922123bc1e7a696e75bdae386e68d552`), so no catalog regen, no npm
reference-build move, and **no port tag** and no release tweet this cycle.
Verdicts: **0 port; 12 n/a; 0 decide** — a triage-only pin advance with no Go
commits and no review gates.

Whole-range reconciliation (the merge-smuggling guard) over
`packages/{ai,agent}/src` + `coding-agent/src/{core,main.ts}` +
`packages/{protocol,client,server}/src` found **3** files, 12(+)/17(−), every
hunk accounted for by the rows below. `packages/ai` and `packages/agent` are
**tree-identical** at both ends (`be2a1c5024dd7dd03681210f36c91a7a406c4844` and
`f9debfed01e972580e37da1839d29e4713eca1b1`), as are `packages/server`,
`packages/client` and `packages/protocol` — so the harness backlog stays **11**
and the catalog-only queue stays **8**.

All three in-scope files sit in `coding-agent/src/core`, which is why this cycle
got a second look rather than a diffstat dispatch. After the per-commit triage
the "0 port" verdict was **adversarially re-attacked by four independent
reviewers** — a whole-range sweep auditor, a `core/agent-session.ts` behavior
auditor, a file-permission/credential-store auditor, and a boundary auditor —
each tasked with REFUTING it rather than confirming it. All four returned **not
refuted, at high confidence**. The two tripwires and the one pre-existing
divergence recorded below are their by-products, not the triage's.

### n/a (12)

| sha | subject | reason |
|---|---|---|
| `5133c9284` | chore(settings-selector): get rid of --default and global model | 4 of the 6 files are selector/TUI/tests; two hunks land in `core/`. `core/slash-commands.ts` drops `[--default]` from the `model` and `thinking` `argumentHint`s — Go has no slash-command table (precedent `312bc713` 2026-07-08, `496185f6e` 2026-08-20). `core/agent-session.ts` is three comment-only edits plus one behavioral change in the **private** `_getThinkingLevelForModelSwitch` (`:1781-1784`): `if (!supportsThinking()) return settingsDefault ?? DEFAULT; return this.thinkingLevel` becomes `settingsDefault ?? this.thinkingLevel ?? DEFAULT`. Its three callers — `setModel` (`:1604`), `_cycleScopedModel` (`:1654`), `_cycleAvailableModel` (`:1690`) — make it reachable **only on a model switch**, and every non-explicit branch it takes is settings-manager-fed. **With no settings manager the expression collapses to `this.thinkingLevel ?? DEFAULT`, which IS the Go behavior**; upstream's own new test ("falls back to current session thinking level when no per-model or global default is configured") asserts exactly the port's world, while the changed test that now expects `"medium"` is gated on `settings: { defaultThinkingLevel: "medium" }` and is unportable. Direct follow-up to `2ff8ba622`, ruled n/a on 2026-08-20 — and the ruling **covers** this commit rather than being stretched, since `5133c9284` removes the very `--default` mechanism `2ff8ba622` added and rests on the same fact: `Session.SetModel`/`SetThinkingLevel` (`coding/session.go:325,336`) never write a default anywhere |
| `5b3caaf4c` | chore(settings-selector): get rid of default thinking in settings, ctrl + S is enough | `components/settings-selector.ts` + `interactive-mode.ts`, deletions only. TUI |
| `1d3503fb9` | feat(settings-selector): show default, make default searchable for model and thinking (#8399) | `components/{model-selector,thinking-selector}.ts` + `interactive-mode.ts`. TUI |
| `768184923` | fix(settings-selector): nit spacing | `components/model-selector.ts` + `packages/tui/src/components/settings-list.ts`. TUI on both halves |
| `cffe4d776` | fix(settings-selector): nit ordering in default t.l. per model | `components/{model-selector,settings-selector}.ts` + `interactive-mode.ts`. TUI |
| `8f2ae3fad` | fix(tui): prevent wrapped table link color leaks (#8363) | `packages/tui/src/components/markdown.ts` + its test + CHANGELOG. There is no `tui/` tree in the Go port |
| `5cd93f688` | feat(coding-agent): add development pi wrapper | `scripts/auto-pi.sh` only — a 75-line dev symlink wrapper that execs `packages/coding-agent/dist/cli.js` with `PI_EXPERIMENTAL` defaulted to 1. No ported surface |
| `686f3487f` | feat(interactive-mode): share via radius artifacts under experimental (#8443) | `modes/interactive/interactive-mode.ts` (TUI) plus a **comment-only** `src/config.ts` hunk. No new SDK surface: no new config key, no new env var (`PI_SHARE_VIEWER_URL` and `PI_EXPERIMENTAL` both pre-existed), no protocol message (`packages/{protocol,client,server}/src` have zero delta across the range), and no new `core/` entry point — `exportToJsonl` already existed at the pin (`b7bb00b93:core/agent-session.ts:3379`). It only CONSUMES already-ported surface: `modelRuntime.getAuth("radius", {minOAuthValidityMs})` is `AuthResolutionOverrides.MinOAuthValidityMs` (`ai/auth_resolve.go:62-68,117,180-184`). Excluded under the 2026-07-14 Radius ruling and the standing host-wiring boundary; the port has no session-sharing path of any kind (`gist`/`share`/`artifact`/`exportToHtml` return nothing across the Go repo). **But this is the commit that surfaced the radius-config daylight — see the first tripwire below** |
| `a2f369d63` | fix(slash-commands): order tree above thinking | a one-line reorder of `BUILTIN_SLASH_COMMANDS` (`tree` moved above `thinking`). Same recorded precedent as `5133c9284`'s slash-commands half — Go has no slash-command table, and upstream's only two consumers of the table are its own definition and `interactive-mode.ts` |
| `77f2d1235` | chore(interactive-mode): get rid of theme, only share via radius if logged in | `interactive-mode.ts` only. Recorded for the **directional signal** behind the tripwire rather than for the diff: it *deletes* the `useRadiusShare = process.env.PI_EXPERIMENTAL === "1"` gate, so Radius artifact upload becomes the DEFAULT `/share` path whenever a `radius` provider exists and a token resolves, with the gh-gist HTML upload demoted to fallback. Still zero SDK impact — the upload is a raw `fetch` to `${DEFAULT_RADIUS_GATEWAY}/v1/artifacts` issued from the TUI class, and the token comes from `getAuthCredential` in `src/cli/auth-command.ts` (cli, non-ported) |
| `f4585b8be` | fix(coding-agent): simplify session sharing links | `interactive-mode.ts` + CHANGELOG + a comment-only `src/config.ts` hunk; it moves the `gh auth status` probe up so the gist branch is only attempted after Radius declines. Across the WHOLE range `src/config.ts` nets to **one JSDoc period** on `getShareViewerUrl` — verified by inspecting both intermediate states (`686f3487f`: "gist ID" → "gist ID or Radius artifact reference."; `f4585b8be`: → "gist ID.") rather than only the range diff, because introduce-then-modify is exactly the case a range diff hides. The function body is byte-identical at all three states |
| `c49906ec7` | fix(coding-agent): preserve managed state file permissions | `core/auth-storage.ts` drops three `chmodSync(authPath, 0o600)` calls so the mode applies **only at creation** (via `writeFileSync`'s `mode` option), leaving administrator-managed modes and ACLs intact; both new tests exercise `FileAuthStorageBackend`, which `core/models-store.ts` merely imports — that file's blob is identical at both ends. **Recorded absence, re-verified exhaustively this cycle** rather than by category: the port has no file-backed auth storage and no `FileModelsStore`. The sole `CredentialStore` is `ai/credential_store.go`'s `InMemoryCredentialStore` (imports only `context`/`sort`/`sync`); the sole `ModelsStore` is `ai/models_store.go`'s `InMemoryModelsStore` (zero `os.`/`ioutil`/`filepath` references); `models_catalog.json` is a read-only `go:embed`; and across the whole repo there are exactly **5** non-test file-writing sites, all in `coding/` — the write/edit tools (`tools.go:551,617`), one temp file (`tools.go:1268`), and the session-transcript JSONL appends (`session_store.go:166,221`) — none of them a credential or managed-state store. Ruling already on record at `d2be68dbe`/`f8bec25f`, re-applied for `1355cd36e` last cycle. **No tripwire warranted, and this is the interesting part**: unlike the `defaultTools` watch-item there is no nameable upstream trigger that would flip this to `port`, because the fix is a *resulting state* (mode-at-creation-only), not a delta needing replay — and Go's stdlib already gives the port that state for free, since `os.WriteFile`/`os.OpenFile` apply `perm` only when they actually create. A future Go file-backed store would have to add an explicit `os.Chmod` to reintroduce the bug upstream just removed |

### Tripwire (not a decide) — `providers/radius-config.ts` is publicly importable and the Radius ruling does not name it

`686f3487f` imports `DEFAULT_RADIUS_GATEWAY` from
`@earendil-works/pi-ai/providers/radius-config`. That import resolves:
`c49906ec7:packages/ai/package.json` carries a **`"./providers/*"` wildcard** in
its exports map, alongside `"./api/*"` — so `packages/ai/src/providers/radius-config.ts`
(`DEFAULT_RADIUS_GATEWAY`, `RadiusGatewayConfig`, `RadiusOAuthCredential`,
`isRadiusGatewayModel`) is **published SDK surface**, not host wiring. Public
reachability through the exports map is the exact deciding fact the 2026-08-11
Cloudflare AI-binding ruling turned on.

The 2026-07-14 Radius ruling names `utils/oauth/radius.ts`, `core/radius.ts`,
`model-registry`'s `oauth:"radius"`, `model-resolver` and provider display
names. It does **not** name `providers/radius-config.ts`, whose only recorded
treatment anywhere in this ledger is a passing "unported host/tooling" aside.
That is daylight in the ruling text.

It is **not** a `decide` this cycle, and deliberately so: `packages/ai` has zero
delta across the range (tree-identical at both ends), so nothing can be lost by
advancing the pin, and minting a `decide` over a file that did not change would
violate the standing formula. `77f2d1235` is why it is worth writing down anyway
— Radius is moving from a latent OAuth-only provider toward a first-class,
always-on host feature.

**Tripwire: a behavioral commit to `packages/ai/src/providers/radius-config.ts`
is `port` under the standing formula** (published `packages/ai` surface, reached
through the `"./providers/*"` export), **not `n/a` under the 2026-07-14 Radius
ruling** — the ruling excludes Radius *OAuth acquisition and host wiring*, not
everything with "radius" in the path. The natural Go home is `ai/` alongside the
other provider-config helpers; today the only Radius mentions in the port are
`ai/envkeys.go:43`, a `pi_messages.go` comment, and `coding/resolve.go:28`.

### Tripwire (not a decide) — the reconciliation sweep pattern has a blind spot at `coding-agent/src/*.ts`

The merge-smuggling guard as written in `pi-triage` sweeps
`packages/ai/src packages/agent/src packages/coding-agent/src/core`. Two defects
surfaced this cycle, both benign *here* and both capable of losing a change later:

1. **Top-level `coding-agent/src/*.ts` siblings are invisible to it.**
   `src/config.ts` changed **twice** in this range (`686f3487f`, `f4585b8be`) and
   the restricted sweep never reported it; only the unrestricted `--name-only`
   classification of all 18 changed paths surfaced it. `src/migrations.ts` has
   the same exposure. Both are on the non-port list today, but "not swept" and
   "swept and ruled n/a" are different states, and only the second one is safe.
2. **`packages/coding-agent/src/sdk` matches no path at all.**
   `git ls-tree c49906ec7 packages/coding-agent/src/` has no `sdk` entry — the
   SDK lives at `src/core/sdk.ts` and is covered only incidentally by the `core`
   element. Any sweep written as `src/{core,main.ts,sdk}` has one dead element,
   which reads as broader coverage than it gives.

**Tripwire: sweep `coding-agent/src` and classify every path, rather than
enumerating subdirectories** — the unrestricted `--name-only` pass is what caught
this, and it is the pass that must not be skipped.

> **AMENDED (same day) — fixed in the skill, and the hole was far bigger than
> this row said.** Rewriting the guard turned up **nine more** blind spots
> beyond the two above, three of them **live ported code the sweep never
> printed**: `coding-agent/src/client/{remote-session,transcript}.ts` →
> `coding/remotesession.go` + `coding/transcript.go`; `coding-agent/src/utils/*`
> → `coding/{text,tools,resources,imageresize}.go`; and `packages/ai/scripts/`,
> which is the sole home of **every** catalog-only queue delta
> (`generate-models.ts` — *not* `packages/ai/src`, which is all the guard swept).
> Missed entirely as whole packages: `packages/{protocol,client,server}` (ported
> to `protocol/`, `client/`, `server/` by the 2026-08-01 ruling — whose "minus
> `server/src/legacy/`" carve-out is itself dead text, upstream having deleted
> that directory in `05bf9df65`, while `server/src/testing/**` quietly became
> ported as `server/internal/servertest/`), `packages/telemetry` (2026-08-06
> ruling), and `packages/session-backends` + `coding-agent/src/server/` (in scope
> but DEFERRED under 2026-08-07, so a hit there is backlog, never a silent
> `n/a`). There is also a cross-package leak no path-to-package mapping would
> predict: `coding-agent/src/utils/pi-user-agent.ts` and
> `core/provider-attribution.ts` land in `ai/providers/`, not `coding/`.
>
> The guard now runs **two mandatory passes** — an unrestricted `--name-only`
> detector plus a `packages`-wide accounting sweep minus four
> structurally-justified exclusions (`tui`, `evals`, `coding-agent/{docs,examples}`)
> — and **forbids sub-path carve-outs**, on the evidence that a carve-out
> outlives the tree it describes and that some real exclusions are unexpressible
> as paths at all (telemetry's excluded schema half shares `src/index.ts` with
> its ported runtime half). Verified on this very range: the corrected sweep
> reports **13** files where the old one reported 3, `src/config.ts` among them,
> and the detector catches the remaining 5 — the four excluded `packages/tui`
> files plus `scripts/auto-pi.sh`, which lives outside `packages/` entirely.
>
> **OPEN — needs an owner ruling, deliberately not decided here.** The fix was
> scoped to the *guard*. But `pi-triage`'s `port` **definition** still reads
> "`packages/ai/src`, `packages/agent/src`, `packages/coding-agent/src/core|main|sdk`",
> which is narrower than the recorded rulings — it names none of protocol,
> client, server, telemetry or session-backends, and it repeats the dead `sdk`
> element. A triager reading only the skill would mark a `packages/protocol`
> change `n/a`. Aligning that text with the rulings is a non-port-boundary edit
> and belongs to the owner under the skill's own "escalate, don't ship" rule.

### Pre-existing divergence surfaced (recorded, NOT fixed) — no thinking clamp or transcript entry on model switch

Surfaced by the `agent-session` reviewer while attacking `5133c9284`. It
**predates the `b7bb00b93` pin and is untouched by this delta** — `5133c9284`
only reorders settings-fed priorities — so it belongs to the standing formula
and the recorded `2ff8ba622` ruling, not to this cycle's verdicts and not to a
`decide`. It is not fixed here: it does not belong in a sync commit.

pi's `setThinkingLevel` (`c49906ec7:core/agent-session.ts:1716-1719`, unchanged
by this commit) clamps the requested level to the new model's capabilities via
`_clampThinkingLevel` and appends a `thinking_level_change` transcript entry when
it actually moves; all three model-switch callers route through it. The Go port
does neither: `coding/session.go:325-333` (`SetModel`) assigns `s.Model`, calls
`Agent.SetModel` and records only `RecordModelChange` — no re-derivation, no
clamp, no `RecordThinkingLevel` — and `coding/session.go:336-341`
(`SetThinkingLevel`) does not clamp either.

Concretely: in Go, `/think high` followed by `/model <non-reasoning>` leaves
`Agent.State().ThinkingLevel == "high"` and writes no `thinking_level_change`,
where pi lands on `"off"` and records it. Mostly display- and transcript-level,
because three of the four providers re-clamp at request time
(`ai/providers/openai.go:42`, `google.go:70`, `openai_responses.go:274`) — **but
not anthropic**: `ai/providers/anthropic.go:330-334` gates only on
`reasoning == ""` and never consults `model.reasoning`, so an anthropic
non-reasoning model would get thinking turned **on**. That last leg is the part
worth a future fix.

### Differential harness — re-pinned to c49906ec7, provable no-op, 47 PASS / 0 KNOWN / 0 FAIL

`config.env` `PI_UPSTREAM_SHA` advanced `b7bb00b93` → `c49906ec7`. Unlike the
2026-08-13 zero-port cycle, which left it alone, this one advances it: there is
1 `src`-backed scenario in that cycle's world and **27** in this one, and the
2026-08-20 review's reproducibility finding asked that the harness's declared pin
track the ledger's.

**The re-pin is a provable no-op, and the proof is what is recorded — the re-run
only confirms it.** `run.sh:94` archives exactly one path, `packages/ai`, and
that path is byte-identical at the two shas: tree
`be2a1c5024dd7dd03681210f36c91a7a406c4844` at both, and the fresh extraction
`run.sh` performed into `pisrc/c49906ec7/` (333 files) `diff -r`s clean against
the existing `pisrc/b7bb00b93/`. `PI_NPM_VERSION` stays 0.84.2 — no release in
the delta — so the 20 `dist` scenarios cannot move either. Backends unchanged at
**27 `src` / 20 `dist`**; `known-divergences.json` still empty, so KNOWN is
unreachable and any difference at all is a FAIL.

**Re-run for this ledger update: `47 PASS, 0 KNOWN (tracked debt), 0 FAIL
[47 scenario(s)]`, exit 0.** Verified **not DARK** two ways rather than by exit
code, per the 2026-08-20 tripwire: `~/.cache/pi-diff/go` builds and vets clean
against the port at HEAD before the run (`go list -m` confirms the `replace`
resolves to the working tree, not a cached module), and the run's own per-scenario
`go ok` lines show all 47 Go captures were produced. The tripwire's trigger — a
port slice deleting or renaming exported `ai` surface — cannot fire in a cycle
with zero port slices.

Two harness-side notes, neither blocking: `PI_GO_REPO` in `config.env` is **dead
config** — its only occurrence anywhere is its own definition, and the Go driver
is bound to the port by a hardcoded `replace` in `go/go.mod`, so setting the env
var does nothing; and `go/pidiff` is a stale prebuilt binary that `run.sh`
ignores (`go run .`), inert but a trap for anyone who assumes it is the thing
under test.

## Drift at last sync check (2026-08-20) — pin advanced to b7bb00b93

Delta `3a0b9a3ee..b7bb00b93`, **20** first-parent changes, **no merges** — the
first-parent count and the total commit count are both 20, so nothing rode in
under a merge. **No release crossed** — `packages/ai` and
`packages/coding-agent` are on 0.84.2 at BOTH ends (no `package.json` appears in
the range diff at all, and no tag in the upstream repo contains `3a0b9a3ee`), so
the npm reference build stays **0.84.2**, no port tag is cut, and no release
tweet is drafted. Verdicts: **6 port — 5 needing Go code, landing as 4 commits,
plus 1 catalog-only (queued); 14 n/a; 0 decide**.

**Review findings were folded into each port commit this cycle rather than into
a separate review commit.** The two previous cycles landed them as one trailing
commit (`527c11f` on 2026-08-19, `b548506` on 2026-08-18); here each slice was
gated and fixed before the next slice started, so `a20597b`, `1308a5a`,
`dc2befa` and `41171de` each already carry their own reviews' findings and there
is no review-fix commit to look for. Recorded because a reader diffing the
cycle's commits will otherwise go looking for one.

Whole-range reconciliation (the merge-smuggling guard) found in-scope deltas in
exactly **44** files, all attributed: `ai/scripts/generate-models.ts`
(`ed867e909` metadata rename + `3de00332f` Z.AI, both catalog-only);
`ai/src/api/anthropic-messages.ts` (`ed867e909` + `87af49dec`);
`ai/src/api/openai-completions.ts` (`87af49dec` + `4ca636c5e` + `b7bb00b93`);
`ai/src/api/{openai-responses,google-generative-ai}.ts` (`87af49dec`);
`ai/src/api/{azure-openai-responses,google-vertex,mistral-conversations}.ts`
(`87af49dec`, all unported providers); `ai/src/api/bedrock-converse-stream.ts`
(`d57e531f5`, unported provider); `ai/src/utils/pi-user-agent.ts`
(`87af49dec`); `ai/src/types.ts` (`ed867e909` + `4ca636c5e` + `b7bb00b93`, the
last two netting to a **comment-only** delta — see below);
`core/compaction/compaction.ts` (`ed867e909`); the seventeen `1355cd36e` BOM
files — `cli/file-processor.ts`, `config.ts`, `migrations.ts`,
`core/{auth-storage,keybindings,model-config,models-store,package-manager,pi-manifest,resource-loader,settings-manager,trust-manager}.ts`,
`core/tools/{edit,edit-diff}.ts`, `modes/interactive/external-editor.ts`,
`modes/interactive/theme/theme.ts`, `utils/frontmatter.ts` — plus the new
`utils/text.ts` they all import; `core/agent-session.ts` (`2ff8ba622` +
`4495469a5`); `core/{defaults,model-resolver,sdk}.ts` (`2ff8ba622`);
`core/settings-manager.ts` (`2ff8ba622` + `1355cd36e` + `1e1a6e27b`);
`core/settings-diagnostics.ts` (`913bcf339`); `core/session-manager.ts`
(`d711bd5f0`); `core/slash-commands.ts` (`496185f6e` + `2ff8ba622`);
`src/main.ts` (`678f0af30` + `913bcf339`); `modes/json-event.ts` (`830a0a59e`);
and the five `modes/interactive` files —
`components/{model-selector,settings-selector,settings-submenu,thinking-selector}.ts`
and `interactive-mode.ts` (`2ff8ba622`, `98767a25d`, `ee29aa118`, `a669db3c3`,
`f0a2880f2`, `9c8070fbe`, `496185f6e`, `678f0af30`).

Two files outside that set are worth naming rather than leaving silent.
`packages/server/src/protocol.ts` is touched TWICE inside the range and is
**unchanged across it**: `4ca636c5e` adds a `reasoningDetails` accounting entry
and `b7bb00b93` removes it again, and the range diff carries no delta for the
file at all — blob `069828e5` at `4ca636c5e^1` and blob `069828e5` at
`b7bb00b93`. `packages/tui/src/components/settings-list.ts` (`2ff8ba622`) is
TUI, out of scope.

The deferred harness backlog is unchanged at **11** — `git diff --name-only
3a0b9a3ee..b7bb00b93 -- packages/agent` is EMPTY, so this delta does not touch
`agent/src` at all, harness tree included. The catalog-only queue goes
**7 → 8**. The `defaultTools` tripwire (2026-08-13) was not hit: neither
`defaultTools` nor `createAgentSessionOptions` appears anywhere in the range
diff.

### Port worklist (5 → 4 Go commits)

| upstream | subject | Go | notes |
|---|---|---|---|
| `ed867e909` | fix(ai): fallback cost not via stream options (#8352) | `a20597b` | **Provenance lives in the 2026-08-19 sections, as AMENDED/SUPERSEDED blockquotes**, because this commit rewrites rows that already existed there rather than adding new surface: it withdraws the caller-facing refusal-fallback option one commit after the port shipped it. `AnthropicRefusalFallback`, `AnthropicRefusalFallbackTarget`, `SimpleStreamOptions.RefusalFallbacks` and `AnthropicOptions.RefusalFallbacks` are all removed; the request's `fallbacks` field, the `server-side-fallback-2026-07-01` beta and the pricing a fallback-served response is billed at now derive from `compat.allowedFallbackModels` alone. Catalog entries gain `provider`, and the cost lookup requires it to equal the model's provider while the wire list stays provider-blind — pi's asymmetry, mirrored exactly. `cost` stays a pointer so a missing or null cost reads as "no swap" the way pi's `?.cost` does, and first-match-wins survives an unpriced first hit. `coding/compaction.go` drops the summarization-fallback wiring; the provider derives it. Removing exported surface was sanctioned by the **2026-08-19 ruling** — `v0.84.18` does not contain it, so no cut tag published it. See: the `4809c2abc` and `eb1f87fa9` **SUPERSEDED** notes and the rewritten regen tripwire in the 2026-08-19 queue section; the **RETIRED** blockquotes under 2026-08-19's and 2026-08-18's divergence sections; and the 2026-08-19 harness amendment. |
| `1355cd36e` | fix(coding-agent): normalize UTF-8 BOMs in text inputs (closes #8337) | `1308a5a` | Upstream extracts `splitBom`/`stripBom` into a new `utils/text.ts` and routes **26 call sites across 17 source files** through it (verified: 17 consumer files carry one import line each plus 26 remaining occurrences; `utils/text.ts` holds the 3 definition/export occurrences). Two of those readers have Go counterparts, and both are ported: **`loadContextFileFromDir`** (`coding/resources.go`) — a BOM-prefixed `AGENTS.md`/`CLAUDE.md` otherwise injects a stray U+FEFF into the first bytes of the system prompt, which emits context-file content verbatim; and **`parseFrontmatter`** (`coding/resources.go`) — the strip lands before the `"---"` test, so a BOM-prefixed `SKILL.md` keeps its frontmatter instead of having name and description silently swallowed into the body. The helper itself lands as `coding/text.go` (`splitBOM`/`stripBOM`), moved out of `coding/editmatch.go` to mirror upstream's own relocation out of `core/tools/edit-diff.ts`. The edit-tool hunk (`coding/tools.go`) is a **pure rename** — `stripBOM` → `splitBOM` at the one call site — matching upstream's `edit.ts`/`edit-diff.ts` rename; no behavior moves. Exactly one BOM is removed, matching pi's `content.slice(1)` over a single UTF-16 code unit, so a doubled BOM keeps the second (pinned). |
| `87af49dec` | Add pi user-agent to most api adapters (closes #8305) | `dc2befa` | Upstream seeds seven adapters' header merge with `{"User-Agent": getPiUserAgent()}` — the commit body names all seven — and **deletes `forcePiUserAgent`** (2 occurrences in `openai-completions.ts`, 2 in `openai-responses.ts` and its definition in `utils/pi-user-agent.ts` at the parent; 0 at the sha). Four of the seven have Go counterparts and all four are ported: `anthropic-messages`, `openai-completions`, `openai-responses`, `google-generative-ai`. **This is a REVERSAL, not an addition.** The same string used to be re-set AFTER the whole merge for two providers — kimi-coding on anthropic-messages (`9d2ec7ffa`) and xai on both openai adapters (`70e878d4c`) — deleting every case variant first, so it outranked the catalog's `KimiCLI/1.5` and any consumer header. Spread first, it is a plain default that **every** later source beats, and a deletion marker in any of them suppresses. Four adapters that previously sent no user agent at all now send one. `pi-messages` is untouched upstream and correctly gains no seed. |
| `4ca636c5e` | feat(ai): opeani completions reasoning details (#8246) | `41171de` | **Did not introduce `reasoning_details`** — corrected after review. At `4ca636c5e^1` (= `87af49dec`) `openai-completions.ts` already carries the streaming read of `choice.delta.reasoning_details` (555-562), `isEncryptedReasoningDetail` (131), the pending-by-id map (283/374-381) and the legacy path that hangs one encrypted detail off the matching tool call's `thoughtSignature` (562) plus its replay out of the tool calls (1231-1242) — `reasoning_details`/`reasoningDetails` appear on six lines (nine occurrences) at the parent, which is also why the 0.84.2 dist carries them (see the npm row at the top of this file). What this commit actually adds: the generalized **`isOpenAIReasoningDetail`** validator over the three shapes (`reasoning.summary`, `reasoning.encrypted`, `reasoning.text`, over a shared `id`/`format`/`index` check), the **`AssistantMessage.reasoningDetails`** array — accumulated from the stream in order and replayed verbatim when `provider`+`api`+`model` all match — and the **`OPENAI_COMPLETIONS_REASONING_FIELDS` / `isOpenAICompletionsReasoningField` allowlist**, which narrows the signature-as-field-name path from `signature && signature.length > 0`. The legacy attachment path is deliberately **kept** (its new comment: "Keep the legacy encrypted tool-call attachment path"), demoted behind the new array and with its replay parse tightened through the same validator. |
| `b7bb00b93` | fix(ai): retain reasoning details in thinking signature | `41171de` | Replaced most of it the same day. `reasoningDetails` is **deleted from the message type**; the sequence now lives in the THINKING block's `thinkingSignature`, serialized as a JSON array. `pendingReasoningDetailsByToolCallId` and `applyPendingReasoningDetail` are gone (4 occurrences at `4ca636c5e`, 0 at `b7bb00b93`), `id` is dropped from the encrypted shape's required fields, and on replay a stored sequence **suppresses** the raw `reasoning`/`reasoning_content`/`reasoning_text` field instead of accompanying it. The tool-call `thoughtSignature` slot survives only as a read-only legacy source, narrowed to `reasoning.encrypted` with non-empty `id` AND `data`. |

The three slices that needed prose beyond a table cell follow. `ed867e909`'s
detail is not repeated here — it is in the 2026-08-19 sections, where the rows
it rewrites already live.

#### `1355cd36e` — the BOM slice

**n/a — the other 14 changed files, each confirmed against the port's own
reader inventory rather than by category:** `core/settings-manager.ts`,
`migrations.ts` (settings manager and config migrations are both on the
non-port list); `core/auth-storage.ts` (no file-backed auth storage in the port
— `ai/auth_types.go` models the credential union only); `core/models-store.ts`
(the port ships the `ModelsStore` *interface* plus `InMemoryModelsStore`; there
is no `FileModelsStore` and nothing reads a models JSON off disk);
`core/model-config.ts` (no `ModelConfig` analog — no percentile-cutoff config
reader exists in the port); `core/package-manager.ts` and `core/pi-manifest.ts`
(extensions runtime / packaging, non-port list); `core/trust-manager.ts`
(project-trust gating, non-port list 2026-06-12); `core/keybindings.ts`,
`modes/interactive/theme/theme.ts`, `modes/interactive/external-editor.ts`
(TUI, non-port list); `cli/file-processor.ts` (bun/CLI packaging, non-port
list); `config.ts` (reads pi's own `package.json` for version metadata — a Go
binary has no counterpart); `CHANGELOG.md`. The `resource-loader.ts` file is
ported only for `loadContextFileFromDir`: its other changed site,
**`resolvePromptInput`**, stays `n/a` under the standing resource-loader ruling
above. Its two call sites at `1355cd36e:resource-loader.ts:527,538` are both
inside `DefaultResourceLoader.reload()` (line 388), and what they feed is
`this.systemPrompt` / `this.appendSystemPrompt`, surfaced by `getSystemPrompt()`
(324) and `getAppendSystemPrompt()` (332) — **not** the `getSystemPromptSource()`
/ `getAppendSystemPromptSources()` accessors (328/336) that the 2026-07-30
ruling names, which return the stored *paths* and call nothing. The `n/a`
therefore rests on the broader half of that ruling, which is what actually
covers this: the port has no `ResourceLoader` object, no `SYSTEM.md` /
`APPEND_SYSTEM.md` discovery and no path-or-inline prompt source at all —
`BuildSystemPromptOptions.AppendSystemPrompt` is a plain literal string, so
there is nothing for a resolver to resolve.

**Half of upstream's regression test is unportable.**
`test/suite/regressions/8337-utf8-bom-parsing.test.ts` asserts three things: the
`splitBom` shape, `parseFrontmatter` over a BOM'd document, and a
`SettingsManager` round-trip that reads BOM'd global + project `settings.json`
and re-writes them without a BOM. The first two are ported byte-for-byte as
`TestSplitAndStripBOM` and `TestParseFrontmatterStripsLeadingBOM` (`{name:
"demo", description: "Test"}`, body `"Body"` — upstream's exact expectation).
The third has no Go counterpart at all: the port has no settings manager. Not a
gap to schedule — a consequence of the non-port list.

**Review note — the `splitBOM` call site is now mutation-locked.** The first
version of `TestEditToolPreservesLeadingBOM` used the fixture
`"﻿hello world\n"`, which is the one shape where the split is
observationally inert: the BOM shares line 1 with the match, the exact-match
path is taken, and the unsplit BOM rides through at offset 0 either way.
Mutating the call site to `bom, raw := "", string(data)` left the whole `coding`
suite green. The test now also carries the fuzzy case — file `"﻿\nA\n"`,
`oldText "\nA "` (the trailing space defeats the exact match), `newText "\nB"` —
where `normalizeForFuzzyMatch` trims U+FEFF as JS whitespace, a BOM-only line
normalizes to empty, and the touched line is rewritten from that fuzzy view, so
only the BOM held aside by `splitBOM` survives. Want value `"﻿\nB\n"` taken
from pi, not from Go: `utils/text.ts` + `core/tools/edit-diff.ts` extracted at
`1355cd36e` and executed in node through `edit.ts`'s exact sequence. Verified
red-for-the-right-reason under the mutation (mutant writes `"\nB\n"`), green
without it.

**Also recorded: the BOM-before-newline-normalization ordering is a parity note,
not an invariant.** Source and test both used to claim the ordering was
load-bearing. It is not: newline normalization can neither touch a leading
U+FEFF nor produce one, so the two transforms commute for every input.
Rewriting `parseFrontmatter` as `stripBOM(normalizeNewlines(content))` leaves
the entire `coding` suite green, the BOM+CRLF case included. Comments in both
places now say what is actually pinned; the nesting still mirrors upstream.

#### `87af49dec` — the user-agent seed

**n/a — the three unported adapters, confirmed by inventory rather than by
category.** Upstream files touching `getPiUserAgent` at the sha are
`anthropic-messages`, `azure-openai-responses`, `google-generative-ai`,
`google-vertex`, `mistral-conversations`, `openai-codex-responses`,
`openai-completions`, `openai-responses` (eight consumers at the sha; the commit
itself seeds seven, `openai-codex-responses` having carried it already).
`ai/providers` contains no azure/vertex/mistral/bedrock/codex Go file, so those
hunks have no site to land on.

**The port's header merge is now an ORDERED object.** The seed is what forced
this. pi builds **one** header object per request and hands it to the SDK; a JS
object keeps its string keys in insertion order and assigning to a key it
already holds updates that key **in place** rather than moving it to the end.
Two names differing only by case are two keys there and one header on the wire,
so the surviving value is the one whose key sits in the **later slot** — and
`net/http`'s `Header` canonicalizes on write, so the port's sequential
`applyProviderHeaders(r.Header, source)` calls made the later WRITE win
regardless of spelling. Before this commit nothing seeded a `User-Agent`, so no
cross-source collision existed and the two agreed. The seed creates one.

`ai/providers/headers.go` now models the object itself (`headerObject`: an
insertion-ordered name list plus values, `applyAsDefaultHeaders` for the SDK
path where a marker deletes, `applyAsRecord` for the self-built-request path
where a marker is dropped), and `anthropic.go`, `openai.go`,
`openai_responses.go`, `google.go` and `pi_messages.go` all fold their sources
into it and apply once at the end. `applyProviderHeaders`,
`mergeProviderHeaders`, `providerHeadersToRecord` and `stringHeaders` are gone,
folded into methods.

Consequences worth having in writing:

- **The anthropic OAuth branch keeps `claude-cli/<v>` against a `User-Agent`-spelled
  caller header.** pi's object holds the seed at slot 0 and the Claude Code
  identity under the lowercase name at a later slot, so `options.headers`
  spelled `"User-Agent"` writes back into slot 0 and loses; spelled
  `"user-agent"`, `"USER-AGENT"` or `"User-agent"` it takes a new slot and wins.
  Executed against **@anthropic-ai/sdk 0.91.1** from `~/.cache/pi-npm/0.84.2`
  (version re-confirmed this cycle), driving the real client with the exact
  object `mergeClientHeaders` builds at the sha: `{"User-Agent":
  "custom-agent"}` → wire `user-agent: claude-cli/2.1.75`; `{"user-agent":
  "custom-agent"}` → `custom-agent`; `{"User-Agent": null}` →
  `claude-cli/2.1.75` (the marker empties slot 0 only). The pre-commit
  `mergeClientHeaders` through the same SDK sends `custom-agent`, confirming the
  seed is what changes the answer. An **8-case matrix** was run on both sides —
  oauth and api-key branches × four spellings and both markers — and the port
  now matches pi on 8/8, the one residual difference being the base UA below.
- **Cross-source precedence no longer depends on spelling anywhere.** With
  `model.Headers{"user-agent": "model-agent"}` + `opts.Headers{"User-Agent":
  "opts-agent"}`, pi keeps the model's (slot 1 beats slot 0) and so does the
  port; the mirror case gives `opts-agent` on both. Not reachable from the
  shipped catalog — all 32 `user-agent` entries in `ai/models_catalog.json` are
  spelled exactly `User-Agent` — but reachable for any user-configured model and
  through `ai.StreamOptions.Headers`.
- **The sorted-name tie-break survives only INSIDE one source.** The 2026-08-04
  ruling (sorted order, chosen over markers-first, for a single map holding two
  spellings) still applies to one `ai.ProviderHeaders` literal, where a Go map
  genuinely has no order to reproduce. It no longer decides anything between
  sources. `TestHeaderObjectCaseCollisionIsDeterministic` and
  `TestHeaderObjectRecordCaseCollisionIsDeterministic` pin both halves.
- **`pi_messages.go` lost its map-order `range` + `Set` loop** in the same pass.
  It predates this commit and was queued rather than fixed; the ordered object
  made it a one-liner, and the wire value for `{"User-Agent": "a", "user-agent":
  "b"}` no longer flips between runs in one process.

**Proof set for the rest of the slice — verified CLEAN.**

- **UA string byte-identical.** `piUserAgent()` dumped from Go and
  `getPiUserAgent()` executed from
  `~/.cache/pi-npm/0.84.2/…/pi-ai/dist/utils/pi-user-agent.js` are `cmp`-identical:
  25 bytes, `pi (darwin 25.5.0; arm64)`.
- **All FOUR Go anthropic auth branches carry the default**, the seed being the
  first statement of `applyAnthropicHeaders`, ahead of the switch. Upstream's
  three `createClient` branches at the sha all route through
  `mergeClientHeaders`; Go's extra `ANTHROPIC_AUTH_TOKEN` branch (`24e5cc04`)
  inherits it. Mutation-verified: deleting the seed turns **five** subtests red
  — the api-key, auth-token and github-copilot defaults, plus the two OAuth slot
  subtests, which need the seed sitting in slot 0 to have anything to lose from.
- **The reversal is genuinely pinned**: moving the seed back to the old force
  position (after `opts.Headers`) on all four adapters turns **23 subtests
  across 10 tests** red — kimi-coding, both xai adapters, the live
  `github-copilot/claude-haiku-4.5` catalog model, the marker-suppression cases,
  the cloudflare attribution agent and the empty-string pin among them.
- **Slot order is pinned in both directions.** Making a re-assigned key move to
  the end turns the OAuth subtest red (`[custom-client]` for
  `claude-cli/2.1.75`); applying in sorted order instead of slot order turns four
  tests red, including the three pre-existing cloudflare-ai-gateway
  auth-suppression tests that were never part of this commit.
- **Exactly one user-agent value on the wire** in every positive case
  (`Values()` length-1 asserts), and pi's null-suppression semantics matched
  arm-for-arm against the real SDKs, including google's split behaviour: a
  colliding `{"User-Agent": null}` drops the seed, a non-colliding
  `{"user-agent": null}` leaves pi's default on the wire.
- **Catalog reachability bound**: every `user-agent` entry in
  `ai/models_catalog.json` is spelled exactly `User-Agent` — 32 of 32,
  re-counted this cycle — so the spelling-sensitive cases above are reachable
  only through user-configured models and `ai.StreamOptions.Headers`, never from
  the shipped catalog.

Not new, restated because the seed now touches it: once a marker deletes the
user agent, Go's transport substitutes `Go-http-client/1.1` where undici
substitutes `node` and @google/genai substitutes `google-genai-sdk/1.52.0 …`.
That is the pre-existing base-UA class the 2026-08-14 review already scoped out
(and the reason the marker tests assert "not pi's agent" rather than a literal).
The port also sends no `x-goog-api-client` at all, same class.

#### `4ca636c5e` + `b7bb00b93` — openai-completions reasoning details

Ported as the **net state**, not as two commits: `4ca636c5e` is about seven
hours old at `b7bb00b93` — same day, `2026-08-19 16:44:17 +0200` to `23:35:43
+0200`, author and committer dates agreeing at both ends — and nothing in
between depends on the shape it introduced, so
porting the intermediate `AssistantMessage.ReasoningDetails` field and then
deleting it would have added a field to the session format for the length of one
commit. `ai/types.go` therefore never grows the field — its only change in
`41171de` is the doc comment on `ThinkingSignature`, mirroring upstream's own —
and the new `ai/providers/openai_reasoning_details.go` holds the codec both
commits share.

**Two of `4ca636c5e`'s changes are net-zero across the pair, and were confirmed
as such rather than read off the diff.** `packages/server/src/protocol.ts` is
**blob-identical** at `4ca636c5e^1` and `b7bb00b93` (`git rev-parse` → `069828e5`
at both ends). `packages/ai/src/types.ts` is not blob-identical, but the
`reasoningDetails` field is: it appears exactly once at `4ca636c5e` and zero
times at `4ca636c5e^1` and at `b7bb00b93`, and the whole `4ca636c5e^1..b7bb00b93`
diff of that file is a one-line **comment** change on `thinkingSignature`
("e.g., for OpenAI responses, the reasoning item ID" → "Provider-specific opaque
or serialized reasoning replay data"). So the session format is byte-untouched
on both sides.

**Serialization: a detail is replayed as pi's OBJECT, not as the provider's
bytes.** The first draft compacted the provider's original JSON (`json.Compact`)
on the theory that "OpenRouter requires the sequence back unmodified" meant the
bytes. It does not — and pi could not honour that reading even if it wanted to.
pi reads the detail out of a `JSON.parse`'d SSE line, pushes the resulting
**object** onto the array, and `JSON.stringify`s it; the SDK stringifies it again
on the way out. Everything `JSON.parse` → `JSON.stringify` normalizes is
therefore part of pi's wire format, and compaction diverged from it on four
counts. The differential harness caught two of them against ground-truth pi:

```
FAIL  value     @ $.messages[2].reasoning_details[0].conf   pi: 1        go: 1.0
FAIL  key-order @ $.messages[2].reasoning_details[1]        pi: 0,type,data,id,custom_flag,nothing
                                                            go: type,data,id,custom_flag,nothing,0
```

Details now go through `jsStringify` (`ai/providers/json.go`), which already
existed for provider error bodies, extended here to stop approximating the two
object rules it had documented as out of reach:

- **A repeated key is one property** — created at its first occurrence, holding
  its last value. `{"text":"a","text":"b"}` → `{"text":"b"}`, where compaction
  emitted a request body with a duplicated object key.
- **Own-property order is not insertion order.** `OrdinaryOwnPropertyKeys` lists
  array-index keys first, ascending, then the rest in creation order, so
  `{"type":"x","0":"y"}` re-serializes as `{"0":"y","type":"x"}`.

and two rules it already had, which now reach this slice:

- **Numbers are renormalized**: `1.0` → `1`, `1e2` → `100`, `-0` → `0`,
  `12345678901234567890` → `12345678901234567000`.
- **Escapes are minimized**: `\u00e9` → `é`, `\/` → `/`. This is the reachable
  half of the class — Python-stack providers reached through OpenRouter (Qwen,
  GLM, DeepSeek, Kimi) serialize with `ensure_ascii=True`, so their
  `reasoning.text` details arrive fully escaped.

**`jsStringify` verified against node over 1,863 generated cases, zero
divergences** — random float64 bit patterns rendered several ways, exponents
from `1e-330` to `1e330`, integer literals 1–29 digits long, 300 random key sets
mixing index-like and ordinary keys, 100 duplicate-key objects, and 400 random
Unicode strings emitted both `ensure_ascii=True` and `False` — plus a 47-case
hand-written table. (Both are the cycle's out-of-tree differential probe against
node, the same shape as previous cycles' oracles; the in-repo residue is the
`TestJSStringify*` tables in `ai/providers/errors_anthropic_test.go`.)
`jsNumber` gained the range-error arm the corpus demanded: `ParseFloat` reports
±Inf or a signed zero *alongside* `ErrRange`, and `JSON.stringify(Infinity)` is
`null`, so `1e400` → `null` and `1e-400` → `0` where the old code passed the
literal through. Only a genuinely malformed literal still passes through.

`isJSONNumber` had to be widened to match: pi's guard is `typeof index ===
"number"` and `JSON.parse` saturates `1e400` to `Infinity`, which passes it.
Decoding into a `float64` returned "number out of range" and **dropped the whole
detail** — and, in the stored-signature path, the whole sequence. It decodes
into a `json.Number` now, which keeps the literal; the leading-byte screen is
what still rejects non-numbers, because `encoding/json` would otherwise accept a
*quoted* number (`"1"`) there.

**A non-array `reasoning_details` no longer truncates the turn.** `openAIChunk`
typed the field `[]json.RawMessage`, so a provider sending anything else failed
the **whole chunk unmarshal** — and `iterateOpenAISSE`'s deliberate junk-line
leniency then skipped the entire `data:` line. Demonstrated end to end before the
fix: the delta

```
{"choices":[{"delta":{"content":"hello","tool_calls":[…],"reasoning_details":{}}}]}
```

produced a final assistant message with **0 content blocks**, stop reason
`stop`, no error — text and tool call both gone. pi's `Array.isArray` ignores
exactly that one field. The chunk field is `json.RawMessage` now and the array
is decoded at the use site, which contains the failure where pi contains it.
Pre-existing (`4ca636c5e` shipped the typed field), but this slice is what put a
provider-controlled, loosely-specified field on OpenRouter's hot path, so the
blast radius is new. Locked by
`TestOpenAIStreamReasoningDetailsNonArrayIgnored` over six non-array values.

**The accumulate loop was quadratic with a very large constant.** Each arriving
detail re-parsed the entire accumulated signature, re-validated every earlier
entry and re-serialized all of them. Measured on the real stream path: n=100 →
18 ms, n=400 → 254 ms, n=1600 → 3.72 s, n=3200 → 14.68 s — 4× per doubling,
burned on the goroutine reading the SSE body, and reached whenever a provider
streams `reasoning.text` fragments one detail per chunk (which is what the
`index` field exists for). pi is structurally quadratic too, but its
per-arrival validation is O(1) property reads on already-parsed objects.

The sequence is now held next to `thinkBuilder` in the closure and only the
arriving entry is validated and rendered. One stream opens at most one thinking
block, so the parse pi does could only ever return what the last delta wrote.
The "a non-sequence signature does not survive" property is unchanged — the
slice starts empty and overwrites the field-name signature wholesale.

**`materialize()` is now pinned.** Deleting it left the whole suite green: the
final message is unaffected because finalization materializes again, and every
`stream.Push` is already preceded by a `materialize()`. The one thing that is
not is `fail(err)`, which pushes the live `output`.
`TestOpenAIStreamReasoningDetailsSurviveATruncatedStream` hijacks the connection
and hangs up mid-body right after a details-only delta; without the call the
aborted message reports `reasoning` — the field-name signature — instead of the
accumulated sequence.

### Port-but-CATALOG-ONLY — queue 7 → 8 (parked for the next release regen)

| sha | generator delta | lands as |
|---|---|---|
| `3de00332f` | **NEW this cycle.** `fix(ai): derive Z.AI reasoning effort metadata` (#8336) replaces the hand-written `ZAI_GLM52_THINKING_LEVEL_MAP` with `getEffortThinkingLevelMap(m.reasoning_options ?? [])`, derived from models.dev, and hoists the whole zai/zhipuai loop out of `loadModelsDevData` into a new `processZaiModels`. `supportsReasoningEffort` is now set for every zai model that HAS an effort map, not just GLM-5.2. **`off: "none"` is NEW here, not "kept"** — corrected after review: the deleted `ZAI_GLM52_THINKING_LEVEL_MAP` at `3de00332f^` is `{minimal:null, low:"high", medium:"high", high:"high", max:"max"}` with no `off` key at all, and the pre-change test asserted exactly that with `toEqual`; no other generator post-processing branch reaches zai (the parent's `off:"none"` sites are `OPENAI_RESPONSES_NONE_REASONING_MODELS`, fireworks `glm-5p2`, and baseten's provider-local `glm52ThinkingLevelMap`). What widens is the **`isGlm52` gate**, from `modelId === "glm-5.2"` to `"glm-5.2" \|\| "glm-5.2-highspeed"`, and the new code then sets `thinkingLevelMap.off = "none"` on the derived map | next regen. Upstream's own test pins the target shapes for both `zai` and `zai-coding-cn`: `glm-5.2` and `glm-5.2-highspeed` → `{off:"none", minimal:null, low:null, medium:null, high:"high", xhigh:null, max:"max"}`; `glm-5.3` → `{off:null, minimal:null, low:"low", medium:null, high:"high", xhigh:null, max:"max"}`. Note `low`/`medium` on GLM-5.2 flip from `"high"` to `null` — the derived map is not a superset of the old one. **Catalog-only, verified rather than assumed:** the change touches exactly two files, `packages/ai/scripts/generate-models.ts` and `packages/ai/test/openai-completions-tool-choice.test.ts`; no request-body path, no `src/api` reach |

Carried: `4809c2abc` **as amended by `ed867e909`**
(`applyAnthropicAllowedFallbackModelMetadata` emitting `[{provider, model,
cost}]` — see the 2026-08-19 queue row and its rewritten regen tripwire, which
`ed867e909` made load-bearing this cycle by removing the option that used to
override it); `eb1f87fa9` (the `ANTHROPIC_ALLOWED_FALLBACK_MODELS` table
itself), `0e4d49541`, `87205484b`, `6db110e6f` from 2026-08-18; `70e878d4c` and
`86d001d36` from 2026-08-17. All eight await the next release regen.

The catalog itself is **UNCHANGED this cycle**, and the range confirms it:
`packages/ai/src/models.generated.ts` does not appear in the range diff at all.
`ai/models_catalog.json` is still the 0.84.2 derivation at 536,642 B, last
touched by `69bcdf1`, and still carries **zero** `allowedFallbackModels`, so the
refusal-fallback feature stays dormant on both sides until the regen. No goldens
were recaptured — nothing in the delta touches a captured golden.

### n/a (14)

| sha | subject | reason |
|---|---|---|
| `d711bd5f0` | fix(coding-agent): preserve branch summary source leaf | the whole in-scope hunk is `SessionManager.addBranchSummary` hoisting `fromId` off `this.leafId` *before* the navigation instead of taking the destination's `branchFromId`. **The port has no branch_summary WRITER at all**: `coding/session_tree.go` READS the entry (`FromID`, `Summary`) when replaying a transcript, and `SessionRecorder` writes only `session`, `model_change`, `thinking_level_change` and `message` entries (`coding/session_store.go`, four `"type":` literals). Named explicitly rather than left implicit: the absence is already on record from 2026-08-18, where `90305d90a`'s `branch-summarization.ts` guard was logged **NO Go home** because "the port consumes stored branch summaries and never generates one". So this is `n/a` against a recorded absence, not a silent one — and the day the port grows a branch-summarization writer, this fix has to ride with it |
| `830a0a59e` | fix(coding-agent): expose tool metadata at stream start (#7953) | `src/modes/json-event.ts` (plus `docs/` and a regression test) — coding-agent `--mode json` stdout shaping. There is no `modes/` tree in the Go port; its wire surface is the pi-messages protocol. Precedent `c93ea6ccf` (2026-08-13) |
| `d57e531f5` | fix(ai): round-trip Bedrock redacted reasoning (#8314) | `bedrock-converse-stream.ts` + its test only. Bedrock is an unported provider (non-port list) |
| `1e1a6e27b` | feat(coding-agent): include paths in settings errors | `core/settings-manager.ts` + its test. The settings manager is on the non-port list; the port has no settings-file reader to attach paths to |
| `913bcf339` | fix(coding-agent): report settings diagnostic paths | `core/settings-diagnostics.ts` + `src/main.ts` + a test — the CLI reporting half of the same feature. Same non-ported surface |
| `678f0af30` | fix(coding-agent): show startup diagnostics in TUI | `src/main.ts` + `modes/interactive/interactive-mode.ts` — the TUI half of the same feature. TUI, non-port list |
| `4495469a5` | fix(coding-agent): compact without provider usage | agent-session's auto-compaction guard. Upstream deletes the early `if (estimate.lastUsageIndex === null) return false` so a context with NO provider usage falls through to the pure message-size estimate, and narrows the stale-pre-compaction check to usage-backed estimates only. **Go already behaves this way**: `estimateContextTokensUsageAware` (`coding/compaction.go:220`) does `if lastIdx == -1 { return EstimateContextTokens(messages) }`. pi is converging on the Go behavior, so no Go change is warranted and none was made |
| `2ff8ba622` | fix(coding-agent): keep model and thinking level changes session scoped (#8356) | settings persistence + TUI selectors (18 files, 12 of them selector/TUI/tests). The core intent — a `/model` or thinking-level change stays in the session unless `--default` is passed — is **already the Go behavior**: `Session.SetModel` and `Session.SetThinkingLevel` (`coding/session.go:325,336`) update the agent and call `RecordModelChange`/`RecordThinkingLevel` on the recorder, and never write a default anywhere. pi converging on Go again. `THINKING_LEVEL_OPTIONS`, the one new `core/defaults.ts` export, has exactly two consumers at the sha — `agent-session.ts:1762` (`if (!this.model) return [...THINKING_LEVEL_OPTIONS]`) and `interactive-mode.ts:4568` (TUI) — and the port handles the no-model case by clamping to `agent.ThinkOff` (`coding/session.go:400`), which is what pi's own `sdk.ts` does |
| `98767a25d` | fix(settings-selector): remove token estimates | settings-selector TUI. Also the first half of a **revert pair** — see `f0a2880f2` |
| `ee29aa118` | feat(settings-selector): make default model and thinking level searchable | settings-selector + settings-submenu TUI |
| `a669db3c3` | fix(settings-selector): show modelid [provider] like /model | settings-selector + settings-submenu TUI |
| `f0a2880f2` | fix(settings-selector): revert token estimate removal | the revert half of the `98767a25d` pair. **Net zero, verified rather than assumed** — the two patches are exact inverses on the same `THINKING_DESCRIPTIONS` map (`minimal/low/medium/high` gain and lose their `(~Nk tokens)` suffixes; `xhigh` goes `"Extra-high"` → `"Extra-deep"` → `"Extra-high"`). Blob identity does NOT hold here, because `ee29aa118` and `a669db3c3` landed on the same file in between — the inversion is the proof, not the blob |
| `9c8070fbe` | feat(settings-selector): ctrl + s persists /model | model-selector + settings-selector + interactive-mode TUI |
| `496185f6e` | feat(coding-agent): /thinking command | the only core-path hunk is **one row** appended to `BUILTIN_SLASH_COMMANDS` in `core/slash-commands.ts` (`{name:"thinking", description:"Set thinking level", argumentHint:"[--default] <level>"}`); the rest is `modes/interactive/components/thinking-selector.ts` + `interactive-mode.ts`. Go has no slash-command table — precedent `312bc713` (2026-07-08), whose only core hunk was likewise a `BUILTIN_SLASH_COMMANDS` edit |

### Review gates

Both gates ran independently per slice — **8 independent reviews** (pi-go-review
+ pi-parity-review on each of the four slices), never the porter. Totals across
the cycle: **1 HIGH + 12 MED + 24 LOW**. Per slice, as recorded in each port
commit's own message:

- `ed867e909` → `a20597b`: **4 MED + 6 LOW**, all valid and applied. Eight
  mutations verified behaviorally red.
- `1355cd36e` → `1308a5a`: **1 MED + 5 LOW**, all valid. The MED is the one
  above — the edit-tool test did not exercise `splitBOM` at all, and the suite
  stayed green with the call removed. Its replacement's expected values were
  produced by executing upstream's TS at the sha in node, not read off Go.
- `87af49dec` → `dc2befa`: **1 HIGH + 3 MED + 6 LOW**. **The HIGH is the OAuth
  precedence case**: the port would have let a caller's `User-Agent` beat pi's
  `claude-cli/<v>` identity. It was caught because the porter's test asserted the
  PORT's value instead of pi's — the reviewer went to the real SDK instead of the
  assertion — and it was fixed at the root cause (the ordered header object),
  not in the assertion. This is the house rule working: *a parity divergence
  "fixed" by editing the assertion to match our output does not ship.*
- `4ca636c5e` + `b7bb00b93` → `41171de`: **4 MED + 7 LOW**, one MED and one LOW
  filed twice from different angles. 22 mutations verified red. **Applied (7):**
  the compaction → `jsStringify` change (MED — the only finding the harness could
  see, and it saw it as a port bug); the non-array chunk field (MED, filed
  twice); the quadratic accumulate loop (MED); unknown-key survival now
  *asserted* rather than merely permitted (MED — an order-preserving "drop every
  key outside the eight known ones" mutation left `go test ./ai/...` entirely
  green against the pre-review `rdSummary`; with the constant carrying a
  `provider_specific` member the same mutation turns **9** reasoning-details
  tests red); the `1e400` accept divergence (LOW); `materialize()` pinned (LOW);
  the missing `orderSensitivePaths` (LOW). **Recorded, not fixed (2 LOW):** the
  repo-wide `json.Marshal` HTML-escaping row and the unpaired-surrogate row, in
  the two divergence sections below. **Superseded (1 LOW):** a finding proposing
  that compaction be kept and its three divergences merely documented — the fix
  removes them instead. **Redirected (1 LOW):** the harness-pin
  reproducibility gap — the harness result was recorded without naming the
  `PI_UPSTREAM_SHA` that produced it, while `~/.cache/pi-diff/config.env` moved
  twice inside the cycle (`3a0b9a3ee` → `ed867e909` → `b7bb00b93`) and every
  `src` scenario's ground truth is that pin, so the PASS count could not be
  re-derived from the ledger alone. It was written into the working harness
  notes rather than into a pin row that could not yet advance; the full chain is
  now carried by the "Reviewed via" row at the top of this file and by the
  harness section below. The same finding's second half, `3de00332f` as an
  unrecorded catalog-queue item, is confirmed and taken **7 → 8** above.

Two reviewer suggestions on the reasoning-details slice were **not** taken:

- **Recording the number/key-order divergence as a permanent
  `known-divergences.json` entry.** pi is ground truth, the harness classified it
  as a port bug, and the machinery to match pi already existed one file away.
- **The suggested test for `materialize()`** — "assert the Partial of the event
  following a details-only delta already carries the signature" — would have
  stayed green under the mutation. Every `stream.Push` in this stream is already
  preceded by a `materialize()`, so every Partial is fresh by construction; the
  only consumer of a *stale* `output` is `fail()`, which is why the test aborts
  the stream instead.

Mutation accounting: the two slices that record a total record **8**
(`a20597b`) and **22** (`41171de`). `1308a5a` and `dc2befa` record theirs
individually rather than as a count — the `splitBOM` call-site mutation above,
and for the headers slice the four families named in that section (seed deleted
→ 5 subtests red; seed moved back to the force position → 23 subtests across 10
tests red; re-assigned key promoted to the end → the OAuth subtest red; sorted
order instead of slot order → 4 tests red). Every fix was verified red for a
**behavioral** reason before shipping; a compile-fail red was not accepted as a
red — where neutralizing a mutation would only have broken the build, the
production change was kept compiling so the test could be observed failing for
the behavior it asserts. (The rule is stated here rather than cross-referenced:
the `2026-08-XX red/green note` pointer that stood in this sentence, and still
stands in the 2026-08-19 section, resolves to nothing — there is no dated
red/green note anywhere in this ledger.)

Final gate: `gofmt -l` clean, `go build ./...` and `go vet ./...` clean, and
`go test -race ./... -count=1` green across all **10** test-carrying packages
(13 listed, 3 with no test files).

### Pre-existing divergences surfaced this cycle (recorded, NOT fixed)

Two items **older than this delta** were surfaced this cycle by the reviewers'
differential execution against pi. Neither is a regression from these ports, so
neither was folded into a sync commit; each is recorded here with its executed
evidence and with the fix to schedule. A third finding of this cycle — the
`(*Client).Close` re-entrancy race — was first written into this table and has
been **moved out** in review: it is a Go-side concurrency defect, not a
pi-vs-Go divergence, and it belongs in **"Follow-ups recorded, NOT fixed this
cycle"** below, which is where the house pattern (2026-08-10, 2026-08-08) puts
an item that needs an owner and a next step.

| site | divergence | executed evidence | reachable? |
|---|---|---|---|
| `coding/resources.go` `parseFrontmatter` body trim | uses `strings.TrimSpace`, where pi uses JS `.trim()`. Go's `unicode.IsSpace` and ECMAScript's trim set are mirror images on exactly two characters: JS strips **U+FEFF** and Go does not; Go strips **U+0085 (NEL)** and JS does not | `utils/frontmatter.ts` extracted at `1355cd36e` and executed in node against Go over a 15-case table: **6 divergences, all on those two characters.** `---\nname: demo\n---\n﻿Body` → pi `"Body"`, Go `"﻿Body"`; `…\nBody﻿` → pi `"Body"`, Go `"Body﻿"`; `…\n﻿ Body ﻿` → pi `"Body"`, Go `"﻿ Body ﻿"`; `…\n﻿` → pi `""`, Go `"﻿"`; and the reverse on NEL — `…\nBody` and `…\nBody` → pi keeps the NEL, Go strips it. U+00A0, U+2028, U+1680 and ordinary whitespace all agree | **latent.** The sole production caller (`coding/resources.go:441`, `fm, _ := parseFrontmatter(...)`) discards the body, so no value reaches a model today. It goes live the moment any consumer takes the second return value |
| every `json.Marshal(body)` request-body site (`ai/providers/openai.go:129` and its four peers) | **Go HTML-escapes `<`, `>`, `&`, U+2028 and U+2029 on the wire; `JSON.stringify` emits them literally.** `encoding/json` marshals with `escapeHTML=true` and re-compacts embedded `json.RawMessage` through the same path, so the escaping applies to replayed reasoning details exactly as it does to every other string in every body | driven through the real `StreamOpenAICompletions` against a local server: a detail whose text is `if (a < b) && c > d then é ok` arrives as `"if (a \u003c b) \u0026\u0026 c \u003e d then é ok"` (re-executed for this ledger entry: `json.Marshal` of that exact string, and of a `json.RawMessage` carrying it, both produce those bytes). `é` survives — that half is this cycle's fix — and `<`/`>`/`&` do not | **reachable and realistic.** Reasoning text about code is where `<` and `&` live. Value-preserving on both sides (identical after parse), which is why nothing has caught it |

The first predates this delta — `TrimSpace` has been there since the function
was written, and the *class* of divergence is unchanged by the port. What the
port does change is **reach**: before `1355cd36e`, a document whose BOM preceded
the `---` failed the prefix test on **both** sides and returned untrimmed, so the
trim was never entered for BOM'd input at all. It is entered now. Not folded into
this cycle's commits per the standing practice — the fix is one token (the
package already ships `trimJS` over the `jsWhitespace` cutset at
`coding/remotesession.go:1218`, ECMAScript WhiteSpace ∪ LineTerminator, exactly
right) and should be scheduled deliberately, alongside an audit of the port's
other JS-`.trim()`-modelling `TrimSpace` calls, rather than smuggled in behind a
BOM commit.

The second is repo-wide and older than the reasoning-details slice; **invisible
to the differential harness**, whose Go side captures bodies through its own
`marshalWire` with `SetEscapeHTML(false)` (`~/.cache/pi-diff/go/canon.go:227`)
rather than through the real client. Recorded because the reasoning-details
slice's premise is "replay what pi replays", and because the first draft's
rationale for choosing compaction over decode+re-encode claimed compaction
preserved these characters — it does not, the outer marshal escapes them either
way. The fix is one shared `json.Encoder` + `SetEscapeHTML(false)` request-body
helper across the five adapters; it needs its own golden pass and should be
scheduled deliberately rather than folded in behind a reasoning-details commit.
No golden or test in the repo asserts the escaped spelling today — `grep -r
'\\u003c'` over the whole repo finds nothing outside this ledger's own sentence
(re-checked this cycle) — so no golden is holding it in place.

### Deliberate divergences added this cycle

Three, all accepted rather than fixed. **Two are pinned by a test; the third is
recorded UNPINNED** — corrected after review, which found the blanket
"all pinned" claim false for the surrogate row. Every row below now either names
its pinning test or says explicitly that it has none.

| site | divergence | executed evidence | reachable? |
|---|---|---|---|
| `ai/providers/google.go` header record | **@google/genai comma-JOINS case-variant names; the port sends the last slot's value alone.** `getHeadersInternal` fills a `Headers` with `append`, not `set`, so two spellings of one name both survive | **@google/genai 1.52.0** from `~/.cache/pi-npm/0.84.2` (version re-confirmed this cycle) driven against a local server with the exact record the ported code produces: `{"User-Agent": "pi (darwin 25.5.0; arm64)", "user-agent": "custom-agent"}` → wire `user-agent: pi (darwin 25.5.0; arm64), custom-agent`. Same join for `"USER-AGENT"`, and `{"User-Agent": "opts-agent", "user-agent": "model-agent"}` → `opts-agent, model-agent` | **live, and the seed is what made it reachable from ONE spelling.** Before `87af49dec` the record carried no user agent, so a join needed two colliding names from the consumer or catalog. Reproducing it would need `Header.Add` plus a model of `patchHttpOptions`' slot behaviour (the SDK's own `{"User-Agent": "google-genai-sdk/…", "x-goog-api-client", "Content-Type"}` defaults hold slot 0 for the exact spelling `User-Agent`) — SDK emulation the port does not do. The port picks the value pi puts **last** in the join. **Pinned** on the port's half by `TestGoogleCaseCollidingModelUserAgentWins` (`ai/providers/pi_user_agent_wire_test.go:469`), which asserts across three spellings that exactly one value — the winner — reaches the wire; pi's joined value is recorded here rather than asserted there, and that test's own comment points back at this ledger for it |
| `ai/providers/headers.go` → `net/http` | **an empty-string user agent vanishes; pi sends the header present-and-empty.** `net/http.Request.write` special-cases exactly this one header: an empty value means "omit", not "send blank" | Go side: `model.Headers{"User-Agent": ""}` reaches the server with no `User-Agent` key at all on anthropic and google, while `X-Empty: ""` arrives present-and-empty. pi side: the same `{"User-Agent": ""}` through @anthropic-ai/sdk 0.91.1 delivers `user-agent:` with an empty value | **live on four adapters that never carried the header before `87af49dec`.** Not fixable through `http.Header` — the skip is unconditional in the transport. Pinned by `TestEmptyUserAgentIsDroppedEntirely` (both arms plus the `X-Empty` control) so it cannot drift silently |
| `ai/providers/json.go` `jsStringify` ← reasoning details | **an unpaired UTF-16 surrogate in a detail's string does not survive the round trip.** `encoding/json` replaces `\ud800` with U+FFFD while decoding, before `jsEscape` — which does understand WTF-8 — can see it, so Go re-emits `�` where JS keeps the code unit and re-emits `\ud800` | node vs Go on the same inputs: `{"text":"\ud800"}` → pi `{"text":"\ud800"}`, Go `{"text":"�"}`; same for `"\udfff"` and `"a\ud83d b"`. A well-formed pair (`😀`) agrees | **adversarial only.** A lone surrogate is not producible by a conformant JSON serializer; the previous `json.Compact` passed those bytes through unchanged, so this is the one case where compaction was closer. **UNPINNED — nothing in the repo asserts it.** The three `TestJSStringify*` tables (`ai/providers/errors_anthropic_test.go:85,102,126`) carry key-order, own-property-order and malformed-rejection cases and no surrogate case; `TestSanitizeSurrogatesDeletesUnpaired` (`ai/providers/json_test.go:35`) exercises a different function (`sanitizeSurrogates`, the streaming-error path); and the WTF-8 rows in the `jsQuote` table (`ai/providers/constrained_sampling_test.go:425`) sit **upstream** of the `encoding/json` decode that causes the loss, so they cannot catch this drift. The candidate pin is recorded as a follow-up below |

The surrogate loss was accepted rather than fixed because recovering it means
replacing the `json.Decoder` token stream with a raw scanner that decodes strings
into WTF-8 itself. It is the one `jsEscape`'s own doc comment already calls
unrecoverable at this layer; this cycle only widens who reaches it.

### Follow-ups recorded, NOT fixed this cycle

- **`client/client.go:592` `(*Client).Close` lets an unrelated goroutine return
  before teardown finishes** — **CLOSED 2026-08-25**; root-caused and fixed in
  that cycle (see "FIXED — the `client` disposal race"). The cause recorded below
  was correct; the candidate fix below was NOT the one taken — scoping the early
  return to the tearing-down goroutine needs goroutine identity, which Go does
  not support without a `runtime.Stack` hack, so disposal was restructured to
  publish completion before notifying and the flag was deleted outright. The
  2026-08-10 follow-up this bullet points at is closed by the same change.
  Original record follows. (Go-side race, **pre-existing**, not introduced by
  this delta — nothing in `3a0b9a3ee..b7bb00b93` touches the client or the
  protocol layer). `reentrant := c.notifying` is true for *any* goroutine that
  arrives while teardown is inside one of its own callbacks, not just for the
  callback that is itself part of the teardown, so an unrelated second caller
  skips the `<-c.closed` wait and sees a client that is not yet disposed. The
  code comment states the cost deliberately ("An unrelated goroutine that
  happens to land in the same window returns early too… never correctness"),
  which is the part review disputes: for a caller that is not part of the
  teardown it *is* a correctness gap, because `Close` returning is documented to
  mean fully disposed.
  - **Where it is filed: nowhere but here.** "Filed separately" in the first
    draft named no destination and there is no issue id — this bullet is the
    record.
  - **Reproduction recipe** (re-run this before assuming it is gone): observed
    **once** under full-suite load this cycle, then not reproduced in 20
    isolated runs, 10 package runs, 5 `-race` runs or 3 full-suite runs, and the
    full `-race` suite for this ledger update was green. The failing assertion
    is `client/disposal_test.go:215`, "Close returned while a child handle was
    still attached", from the 8-goroutine concurrent-`Close` test — i.e. the
    same symptom the **2026-08-10 follow-up** already recorded as load-flaky and
    spun off to its own session. Treat the two as one item; what this cycle adds
    is the cause, not a new bug.
  - **Candidate fix:** scope the early return to the goroutine actually
    performing teardown (record that goroutine, or pass re-entrancy through the
    notification call path) instead of to any goroutine in the `c.notifying`
    window, so an unrelated caller still waits on `<-c.closed`.

- **The `jsStringify` unpaired-surrogate divergence has no pinning test.**
  Recorded UNPINNED in the deliberate-divergence table above. Candidate pin:
  feed a detail whose string carries a lone surrogate (WTF-8 `\xed\xa0\x80`,
  i.e. pi's `{"text":"\ud800"}`) through `jsStringify` and assert Go emits
  U+FFFD where node emits `\ud800` — a characterization test, green from the
  first run, so it cannot be red/green-verified and must be labelled as such
  (see the red/green rule under Review gates). Cheap, but it needs the
  node-executed want value captured alongside it, so it is scheduled rather than
  folded in behind a reasoning-details commit.

### Differential harness — DARK (40 uncompared) → 47 PASS / 0 KNOWN / 0 FAIL

**The harness started this cycle dark, and that is the finding worth recording.**
`ed867e909`'s port deleted `ai.AnthropicRefusalFallback` and
`SimpleStreamOptions.RefusalFallbacks`, which broke the Go driver's **compile**.
`run.sh` runs the whole Go side in one shot — `(cd go && go run .)` at
`run.sh:114-117` — before it reaches the scenario list (131) and `classify.py`
(143), so the compile error aborted the run up front and ALL 40 shipped
scenarios went uncompared: every provider, not just the fallback ones. The abort
is loud (a red "go side failed"), but it exits **1** — the same status
`classify.py` returns for "any FAIL" — and prints no scenario tally, so a caller
reading only the exit code cannot tell DARK from FAIL.

**Tripwire, new this cycle** (same register as the `defaultTools` tripwire of
2026-08-13 and the regen tripwire in the 2026-08-19 queue section): **a port
slice that deletes or renames exported `ai` surface must build
`~/.cache/pi-diff/go` before any harness result from that slice is trusted, and
a run that stops at "go side failed" is DARK — not FAIL, and never PASS. Zero
compared scenarios is not a result.** The trigger is structural and will recur:
the driver calls the port's exported API directly, so every future un-port
breaks it the same way. The durable guard, whenever the harness is next edited,
is for `run.sh` to compile the driver as its own step and exit with a status
distinct from `1`.

Repaired inside slice 1 (see the 2026-08-19 harness amendment blockquote, which
records the repair against the row it amends): `~/.cache/pi-diff/go/main.go`
dropped the `refusalFallbacks` field and its assignment, and the four
`refusal-fallbacks-anthropic-*` scenarios were re-pointed at
`model.compat.allowedFallbackModels` in the `{provider, model, cost}` shape,
which is now the only way to drive the feature. That took it **40 → 42**
(`-default` RETIRED, `-empty-list` and `-no-compat` added, plus a
`-cross-provider` pinning `ed867e909`'s asymmetry).

The reasoning-details slice took it **42 → 45 → 47** — three scenarios shipped
with the port, two added by the review:

- `reasoning-details-replay-sequence` — the three detail shapes replayed in
  order out of the thinking signature, winning over a legacy encrypted detail
  still sitting on the tool call. Declares `orderSensitivePaths` for each
  detail: without it the harness sorts keys before diffing, so the `order.txt`
  lines were written and compared against nothing. Verified to assert something
  — rendering objects in alphabetical order instead turns it red on all three
  paths.
- `reasoning-details-replay-suppresses-raw-reasoning` — the stored sequence is
  the structured **alternative** to a raw reasoning field, not a companion. Turn
  1 carries visible thinking plus a sequence in the thinking signature, so
  `reasoning_details` is emitted and no `reasoning` / `reasoning_content` /
  `reasoning_text` key is written at all; turn 2 carries the same visible
  thinking with an ordinary `reasoning_text` field-name signature and no
  sequence, so the raw field IS written there. Both sides must differ between
  the two turns in exactly that way.
- `reasoning-details-replay-legacy-encrypted` — with no thinking block carrying
  a sequence, the tool calls' `thoughtSignature` slots are the source, narrowed
  to well-formed `reasoning.encrypted` details with non-empty `id` AND `data`,
  replayed in tool-call order. The three other signatures in the fixture — a
  `reasoning.text` detail, an encrypted one with `id: ""`, and the bare JSON
  number `123` — all parsed before `b7bb00b93` and were replayed as entries;
  they must now be dropped. Together with the one above it, this pair was
  shipped as the porter wrote it and the review changed nothing in either —
  which is all the first draft's "unchanged" meant; both are new this cycle, so
  there was nothing for them to be unchanged *from*.
- `reasoning-details-replay-unknown-keys` **(new)** — members pi has no name for
  (`Record<string, JsonValue>`), with nested object and array values, `id:null`,
  and an integer-like `"0"` key that JS hoists; order-sensitive at each detail.
- `reasoning-details-replay-number-forms` **(new)** — `1.0`, `1e0`, `1e2`, `-0`
  and `1e400` in a stored sequence. Both sides emit
  `"conf":1 … "scale":100,"neg":0 … "index":null`, byte-identical.

All five are `backend: "src"` and require `PI_UPSTREAM_SHA >= b7bb00b93`: at an
older pin pi still replays the pre-`b7bb00b93` shape and they FAIL.

`~/.cache/pi-diff/config.env` `PI_UPSTREAM_SHA` is now **`b7bb00b93`** (via
`ed867e909` mid-cycle), and `reasoning.json` **moved from `dist` to `src`**.
That flip is not bookkeeping: released 0.84.2 really does turn an opaque
thinking signature into a literal request key —
`assistantMsg[signature] = …` under `if (signature && signature.length > 0)` in
`dist/api/openai-completions.js:918-923` — and `b7bb00b93` gates the same
assignment on `isOpenAICompletionsReasoningField(signature)`. The scenario's
`sig-abc` signature is captured against the sha for exactly that reason; it
flips back to `dist` at the release that ships the narrowing (README rule).

Reach audit of the 19-commit span between the harness's mid-cycle pin
(`ed867e909`) and `b7bb00b93`, so the advance could not silently move an
unrelated scenario's ground truth: exactly **four** commits touch
`packages/ai/src`, and only the reasoning-details pair touches a captured body.
`87af49dec` is `createClient` headers (already ported, outside the bodies-only
harness) and `d57e531f5` is `bedrock-converse-stream.ts` (no Go file). The other
15 are coding-agent, settings-selector, TUI and generator changes.

**Re-run for this ledger update: `47 PASS, 0 KNOWN (tracked debt), 0 FAIL [47
scenario(s)]`, exit 0.** Scenario backends are 27 `src` / 20 `dist`.
`known-divergences.json` still holds an empty `divergences` array — the port
carries no accepted-wrong baseline entry.

Known drift left alone: `~/.cache/pi-diff/README.md`'s `## Scenarios` table
still lists **11** rows against 47 actual scenarios, and its `reasoning` row
still says `backend: dist` (stale as of this cycle's flip). Its "order-sensitive
paths" list is **not** drift and was corrected in review: it already carries
three entries, `$.chat_template_args` (Baseten — genuinely declared by
`baseten-thinking`, `-thinking-off`, `baseten-catalog-kimi` and
`thinking-budget-baseten-args-var`), `functionCall.args` on the google
scenarios, and the `$.messages[i].reasoning_details[j]` entry added this cycle.

## Drift at last sync check (2026-08-19) — pin advanced to 3a0b9a3ee

Delta `2509b5c03..3a0b9a3ee`, **12** first-parent changes, no merges. **No
release crossed** — `packages/ai` and `packages/coding-agent` are on 0.84.2 at
both ends, so the npm reference build stays **0.84.2** and no tag is cut this
cycle. Verdicts: **3 port → 3 Go commits + review fixes; 1 catalog-only
(queued); 9 n/a; 0 decide**.

**This delta is unusually revert-heavy — four of the twelve changes are reverts
or the commits they undo, and one of them reverts the sha the previous pin sat
on.** Two pairs net to zero and were verified byte-restored rather than assumed:

- `cff1cf52c` (cache-friendly compaction primitives) + `8dab70281` (its revert)
  — `ai/src/index.ts`, `coding-agent/src/index.ts` and `core/agent-session.ts`
  all confirmed identical to their pre-`cff1cf52c` blobs by `git rev-parse`
  object comparison. `compaction.ts` differs only because `ef8dc7385` and
  `4809c2abc` also touched it.
- `a6c6f8018` (anthropic fallback usage, first attempt) + `59a71b235` (its
  revert) — `anthropic-messages.ts` restored to the exact pre-`a6c6f8018` blob.
  Superseded by the re-land `4809c2abc`, which is therefore the only net change
  and the only thing triaged. The first attempt looked the served model up in
  `ANTHROPIC_MODELS` directly; the re-land carries pricing on the fallback
  target instead. **The port must not follow `a6c6f8018`'s approach**, and the
  distinction is load-bearing beyond style — see the harness note below.
- `3a0b9a3ee` reverts `2509b5c03`, the previous pin, and is ported as an
  un-port. See the 2026-08-19 ruling.

Whole-range reconciliation (the merge-smuggling guard) found in-scope deltas in
exactly 14 files, all attributed: `agent/src/{agent-loop,agent}.ts`
(`3a0b9a3ee`), `ai/scripts/generate-models.ts` (`b23741269` type-only +
`4809c2abc` cost metadata), `ai/src/api/anthropic-messages.ts` (`4809c2abc`),
`ai/src/api/openai-completions.ts` + `ai/src/api/simple-options.ts`
(`b23741269`), `ai/src/types.ts` (`b23741269` + `4809c2abc`),
`ai/src/auth/oauth/{github-copilot,kimi-coding}.ts` + `ai/src/utils/sleep.ts`
(`55b0db4d3`, all OAuth), `core/agent-session.ts` +
`core/compaction/compaction.ts` (`ef8dc7385` refactor + `4809c2abc` type),
`core/extensions/loader.ts` (`c06132898`), and `core/settings-manager.ts`
(`836aee6d3`, a comment). The deferred harness backlog is unchanged at **11** —
nothing in this delta touches `agent/src/harness`. The catalog-only queue goes
**6 → 7**. The `defaultTools` tripwire (2026-08-13) was not hit.

### Port worklist (3 → 3 Go commits + review fixes)

| upstream | subject | Go | notes |
|---|---|---|---|
| `b23741269` | feat(ai): generalize openai-completions thinking token budget fields | `f0311e3` | The vLLM-only `thinking_token_budget` becomes a compat-selected field name: new `ThinkingTokenBudgetField` (`thinking_token_budget` vLLM / `thinking_budget` Qwen-DashScope-SGLang / `thinking_budget_tokens` llama.cpp), with the old `supportsThinkingTokenBudget` boolean retained as a documented alias resolving to the vLLM name. The clamped budget is computed once and now also reaches chat-template values through a new `{"$var":"thinking.budget"}`, on BOTH the `chat-template` and `baseten` call sites. pi's `simple-options.ts` extraction (`thinkingBudgetForLevel`, `clampThinkingBudgetToAnswerRoom`) lands beside the existing budget helpers in `ai/providers/anthropic.go`. Ordering preserved: the ceiling still reads whichever max-tokens field `buildParams` set (pi assigns at openai-completions.ts:725-728, resolves at :758, so the value is identical to the old late block). The generator hunk is type-only and upstream documents the field as "not set on the generated catalog" — **no catalog change and no queue item**. |
| `4809c2abc` | fix(ai): anthropic fallback usage | `bc7964e` | A response served by a refusal fallback is now COSTED against the model that served it. `AnthropicRefusalFallback`'s union collapse grows from `Models []string` to targets carrying an optional `cost`; the wire projection strips `cost` deliberately (pi's explicit `.map(f => ({model: f.model}))`) rather than by construction, and that stripping is test-locked on a real request body. `message_start` resolves pricing through pi's `??` chain — request option first, then `compat.allowedFallbackModels` — under pi's truthiness gate, and re-prices on every `message_start`, so a second one naming the requested model bills at the original rates again. **Reshaping the type broke no released API: it landed in `13c801e`, after `v0.84.18`, and this cycle cuts no tag.** Generator half → catalog queue. **SUPERSEDED by `ed867e909`: upstream withdrew the caller-facing option one commit later, so `AnthropicRefusalFallback`, its targets type and both `RefusalFallbacks` fields are removed and the `??` chain collapses to a single catalog lookup that also matches on `provider`. The costing behavior described here survives; the surface it was reached through does not — do not treat this row as live API.** |
| `3a0b9a3ee` | Revert "feat(agent): expose provider context construction" | `0cfa63f` | **Un-port.** Upstream withdrew `2509b5c03` the day after the port shipped it, so `BuildProviderContext` (agent/loop.go), `Agent.BuildProviderContext` (agent/agent.go) and the `ContextPipeline` type `b548506` had introduced to express pi's `Pick<AgentLoopConfig, …>` are all removed, folding the transform→convert pipeline back inline. Verified to be exactly the revert and no more: `agent/loop.go` and `agent/agent.go` are byte-identical to `b79c9e6` (the pre-port parent), the `go doc -all` delta is exactly the three identifiers upstream removed, and no caller remains anywhere including `examples/` and `cmd/`. See the 2026-08-19 ruling. |

Review fixes for all three landed in `527c11f`.

### Port-but-CATALOG-ONLY — queue 6 → 7 (parked for the next release regen)

| sha | generator delta | lands as |
|---|---|---|
| `4809c2abc` → **amended by `ed867e909`** | `applyAnthropicAllowedFallbackModelMetadata` (renamed from `applyAnthropicFallbackCostMetadata`) builds the whole list in one pass and merges it through `mergeAnthropicMessagesCompat`; `getAnthropicMessagesCompat` no longer seeds it. Each entry is `{provider: fallbackModel.provider, model: fallbackModel.id, cost: fallbackModel.cost}` | next regen. **The emitted value is `[{provider, model, cost}]` — not `[{model, cost}]` (`4809c2abc`) and not `[string]` (`eb1f87fa9`). Regenerate to THIS shape.** A regen that drops `provider` decodes in Go as `Provider: ""`, which never equals the model's provider, so `anthropicFallbackModelCost` matches nothing and every fallback-served response is billed at the REQUESTED model's rates — while `fallbacks` still goes out on the wire and the beta header is still sent, because both gate on length alone. Silent in every direction; see the regen tripwire. |

Carried: `eb1f87fa9` (the `ANTHROPIC_ALLOWED_FALLBACK_MODELS` table itself —
`claude-fable-5` → `[claude-opus-4-8, claude-opus-5]`, `claude-opus-5` →
`[claude-opus-4-8]`), `0e4d49541`, `87205484b`, `6db110e6f` from 2026-08-18;
`70e878d4c` and `86d001d36` from 2026-08-17.

**Regen tripwire — read before the next catalog regen.** *(Rewritten for
`ed867e909`, which deleted one of the two consumers this paragraph used to name
and made `provider` load-bearing.)* There is now exactly ONE Go consumer:
`getAnthropicCompat` in `ai/providers/anthropic.go` decodes
`allowedFallbackModels` off the raw compat blob, and everything else — the
request's `fallbacks` field, the `server-side-fallback-2026-07-01` beta, and the
pricing a fallback-served response is billed at — reads that one slice.
`coding/compaction.go`'s `anthropicSummarizationFallback` is **gone**: pi
withdrew `getAnthropicSummarizationFallback` at `ed867e909`, so compaction no
longer picks a fallback per call site.

Two regen shapes fail silently, in different ways:

- **The old `[string]` shape** fails `json.Unmarshal` into the fallback struct,
  leaves the slice empty, and sends no `fallbacks` key and no beta.
- **A shape missing `provider`** (i.e. `4809c2abc`'s `[{model, cost}]`) decodes
  fine — so `fallbacks` and the beta still go out — but every entry carries
  `Provider: ""`, `anthropicFallbackModelCost` requires
  `f.Provider == model.Provider`, and no entry ever matches. The feature looks
  live on the wire while every fallback-served response is billed at the
  requested model's rates.

Neither produces an error or a failing test on its own, because the catalog
carries zero `allowedFallbackModels` today (verified: 0 occurrences in
`ai/models_catalog.json`, and 0 in `ed867e909:packages/ai/src/models.generated.ts`).
The in-repo guard is `TestAnthropicCompatSurvivesMalformedFallbackTargets`
("generated shape decodes whole", `ai/providers/anthropic_test.go`), which
decodes a hand-written `{provider, model, cost}` blob — it proves the DECODER,
not the regen, so the shape above still has to be checked by eye against the
generator at the sync sha.

### n/a (9)

| sha | subject | reason |
|---|---|---|
| `cff1cf52c` | feat(coding-agent): add cache-friendly compaction primitives | reverted by `8dab70281` inside this same delta; net zero, blobs verified restored |
| `8dab70281` | Revert "add cache-friendly compaction primitives" | the revert half of the pair above |
| `a6c6f8018` | fix(ai): anthropic fallback usage (#8308) | reverted by `59a71b235`; superseded by the `4809c2abc` re-land |
| `59a71b235` | Revert "anthropic fallback usage (#8308)" | the revert half of the pair above |
| `ef8dc7385` | feat(coding-agent): centralize compaction summary requests | **pure refactor** — `_runDefaultCompaction` and `buildSummarizationContext` extractions with value-identical call sites. The extraction of `UPDATE_SUMMARIZATION_INSTRUCTIONS` out of `UPDATE_SUMMARIZATION_PROMPT` looked like prompt surface, so all three constants were executed and byte-compared pin vs HEAD: the composed UPDATE prompt is **1257 chars, identical**, and `SUMMARIZATION_PROMPT` / `SUMMARIZATION_SYSTEM_PROMPT` are identical too. No Go change warranted |
| `55b0db4d3` | fix(ai): prevent copilot policy login rate limits | entirely `ai/src/auth/oauth/` — OAuth token acquisition, non-port list since 2026-06-12. The new `ai/src/utils/sleep.ts` has exactly two consumers at HEAD, both OAuth files; the `sleep` import in `agent-session.ts` is the unrelated pre-existing `coding-agent/src/utils/sleep.ts` |
| `836aee6d3` | feat(coding-agent): show compaction usage notices | the only in-scope hunk is a **one-line comment** on `Settings.showCacheMissNotices`; the behavior lives in `modes/interactive`. The port does not carry that setting at all |
| `c06132898` | fix(coding-agent): load extensions in Node SEA hosts | extensions runtime — unported |
| `f0c5d86d2` | fix(tui): fit text padding to narrow widths | TUI — unported |

### Review gates

Both gates ran independently per commit (6 reviews: go-review + parity-review
on each of the three ports), then an independent fourth pass re-verified the
result. **3 MED + 12 LOW**, all triaged; the MEDs and the non-pre-existing LOWs
are applied in `527c11f`.

- **MED (go-review, `bc7964e`)** — the fallback-pricing decision sat as 27 lines
  of triple-nested `if` inside the `message_start` case of the SSE callback,
  despite being a pure function of (model, servedID, refusalFallbacks).
  Extracted as `anthropicUsageModel`.
- **MED (go-review, `0cfa63f`)** — the un-port's replacement test passed a
  transform that ignored both parameters, so two properties the deleted test
  pinned were uncovered **tree-wide**: mutating the loop to hand the transform
  `nil` messages, or a `context.Background()`, left the entire suite green.
- **MED (independent verification, `0cfa63f`)** — found while probing past the
  review's scope: the repaired context assertion checked only that a context
  *value* survived, so a `context.WithoutCancel` substitution still escaped the
  whole suite. The test now pins cancellation, which is the half that matters —
  `coding/compaction.go` installs a provider-backed transform, so a severed
  context would decouple `Abort` from summarization.
- **MED (parity, `bc7964e`)** — the catalog-only queue still recorded the old
  string shape and had no row for `4809c2abc`'s generator half. Fixed in this
  ledger; see the regen tripwire above.

Parity verification worth recording: the fallback-costing extract was checked
against an **oracle transliterated from `4809c2abc:packages/ai/src/api/anthropic-messages.ts`
lines 606-613**, modelling `?.cost`, `??` and the truthiness gate with an
explicit present/absent type rather than paraphrasing them, run over a
**10 compat × 7 fallback-set × 5 served-id matrix (350 cases) — zero
divergences**. The matrix covered unpriced targets, `"cost": null`, zero-priced
targets, duplicate-first-unpriced, self-referential catalog entries, `served ==
""`, malformed compat, and the `"default"` arm. It also asserts the shared
catalog `*ai.Model` is never mutated and that the no-swap arm returns the *same
pointer* (pi's `: model`).

**Caveat to record rather than assume away: the npm reference build cannot
cross-check this cycle's anthropic slice at all.** `grep -rc fallback` over
`~/.cache/pi-npm/0.84.2/**/dist/*.js` is **0 in every file** — the
refusal-fallback feature postdates 0.84.2 entirely, so TS-at-the-sha is the only
reference available for `bc7964e`. Nothing here is "npm-verified".

Every ported behavior was mutation-verified red for a behavioral reason before
shipping (**19 mutations** across the three changes and the review fixes; a
compile-fail red was not accepted as a red — see the 2026-08-XX red/green note).
One planned mutation deliberately came back **green and was accepted as
correct**: dropping the `min` inside `clampThinkingBudgetToAnswerRoom` does not
red `TestAnthropicStreamSimpleClampsMaxTokensAndBudget`, because that test's
value comes from a *second*, separate clamp in `StreamSimpleAnthropic` applied
after the context re-clamp. Inside `adjustMaxTokensForThinking` the clamp only
runs under `maxTokens <= thinkingBudget`, where `max(0, maxTokens-1024)` is
always the smaller operand — so the `min` is genuinely inert there, and the
green is the empirical confirmation of the refactor's algebraic no-op.

### Pre-existing divergences surfaced this cycle (recorded, NOT fixed)

The parity reviewers built differential scenarios that caught three real
divergences **older than this delta**. None is a regression from these ports, so
none was folded into a sync commit; each is recorded here with its executed
evidence so the fix can be scheduled deliberately.

| site | divergence | executed evidence | reachable? |
|---|---|---|---|
| `ai/providers/openai.go` max-token write | gates on `*opts.MaxTokens > 0`; pi gates on JS truthiness `if (options?.maxTokens)`, and a negative value is truthy | vLLM zai model, `maxTokens = -5`, reasoning `high`: pi emits `max_completion_tokens: -5` and NO budget; Go omits the field and emits `thinking_token_budget: 15360` | yes, from any SDK caller. `f0311e3` **widens the blast radius** — the budget ceiling is read back out of `params`, so the divergence now also flips the budget on both the top-level field and the new `$var` path |
| `ai/providers/openai.go` `params[compat.MaxTokensField]` | writes whatever string the compat carries; pi's ternary funnels anything outside the union onto `max_completion_tokens` | compat `maxTokensField: "weird_tokens"`, maxTokens 4096: pi → `max_completion_tokens: 4096`, `thinking_token_budget: 3072`; Go → `weird_tokens: 4096`, budget `15360` (ceiling lost) | only with an out-of-union compat value |
| `ai/providers/anthropic.go` `resolveThinkingBudgets` | a nil `*int` means "not overridden", but pi's `{...DEFAULT, ...custom}` spread copies an explicit `null` over the default and `Math.min(null, room)` coerces to 0, which pi then drops as non-positive | `thinkingBudgets = {"medium": null}`, reasoning `medium`: pi omits the key, Go sends `8192` | **latent** — pi reaches null via `settingsManager.getThinkingBudgets()`, but Go exposes `*ai.ThinkingBudgets` only as an embedder-set struct field with no settings decode, so no Go caller can currently express null-vs-absent |

Also recorded as accepted debt, now test-locked for the first time: the Go
loop's `if config.ConvertToLlm != nil { … } else { defaultConvertToLlm(…) }`
fallback is Go-only — pi's `AgentLoopConfig.convertToLlm` is a required field
called unconditionally (`3a0b9a3ee:packages/agent/src/types.ts:178`,
`agent-loop.ts:295`), so a direct `AgentLoop(…)` caller that leaves it unset
gets a converted context in Go and a `TypeError` in pi. Unchanged by `0cfa63f`;
pinned by `TestAgentLoopDefaultConvertToLlm`.

And carried forward, re-confirmed on this cycle's golden surface: pi's
empty-array arm (`refusalFallbacks: []`) serializes to `"fallbacks": []` where
the Go collapsed union serializes it to `"fallbacks": "default"`. Deliberate per
`eb1f87fa9`'s collapse, unreachable from any pi code path (pi's only producer is
guarded on `length > 0`), and **not widened** by `4809c2abc` — the cost half
falls through to the catalog on both sides.

> **RETIRED by `ed867e909` — do not carry this entry forward again.** That
> commit withdrew the caller-facing option, the union and the `"default"`
> literal outright: both sides now derive `fallbacks` from
> `model.compat.allowedFallbackModels` alone and both omit the key when the list
> is empty (`length > 0` in pi, `len(...) > 0` in Go). There is no value on
> either side that can produce `"fallbacks": []` or `"fallbacks": "default"`, so
> the divergence has no reachable input left. A recorded divergence that can no
> longer fire is the doc analogue of the harness's FIXED state — retired here
> rather than re-confirmed.

### Differential harness — 34 → 40 scenarios, 40 PASS / 0 KNOWN / 0 FAIL

Six new `backend:"src"` scenarios for this cycle's openai surface
(`thinking-budget-field-qwen`, `-field-llamacpp`, `-field-overrides-alias`,
`-chat-template-var`, `-chat-template-var-off`, `-baseten-args-var`), taking the
shipped set **34 → 40**.

`~/.cache/pi-diff/config.env` `PI_UPSTREAM_SHA` advanced `2509b5c03` →
`3a0b9a3ee`. **The pin bump was mandatory, not cosmetic**: with the new
scenarios in place at the old pin the stock `./run.sh` was `35 PASS / 5 FAIL`,
exit 1 — a runner that can never exit 0 trains reviewers to read FAIL as normal.
All five failures were stale-ground-truth artifacts (pi at `2509b5c03` emits
`"high"` or omits the field; pi at `3a0b9a3ee` emits `15360`, matching Go
exactly), so no Go change was warranted and none was made.

A predicted blocker turned out to be already moot, and the reason is worth
keeping: at `b23741269` the four `simple-tool-choice-anthropic-*` src scenarios
break because `a6c6f8018` made `anthropic-messages.ts` import the generated
`providers/data/anthropic.json`, which is gitignored and so can never ride in a
`git archive` extraction — reproduced as
`ERR_MODULE_NOT_FOUND … /pisrc/b23741269/src/providers/data/anthropic.json`. The
`59a71b235` revert plus the `4809c2abc` re-land dropped that import
(`git show 3a0b9a3ee:…/anthropic-messages.ts | grep -c ANTHROPIC_MODELS` → 0),
so at the new pin the extraction has no `data/` directory and all five pass.
**One-line pin bump, no harness change.** Verified cold: `rm -rf pisrc/3a0b9a3ee
&& ./run.sh` re-extracts from scratch and still lands `40 PASS / 0 KNOWN / 0
FAIL`, exit 0. All 13 promoted toolChoice/refusalFallbacks scenarios pass at the
new pin, and `thinking-budget-chat-template-var-off` — which passed at both pins
— is now a meaningful control (it proves the budget is suppressed) rather than
the vacuous agreement it was at the old pin, where the lagging field never
appeared at all.

Known drift left alone: `~/.cache/pi-diff/README.md`'s `## Scenarios` table
still lists 11 rows against 40 actual scenarios, and its "order-sensitive paths"
list may need a third entry for `$.chat_template_args`.

> **AMENDED before the next pin — harness repaired for `ed867e909`, now 42 PASS
> / 0 KNOWN / 0 FAIL.** Deleting `ai.AnthropicRefusalFallback` and
> `SimpleStreamOptions.RefusalFallbacks` broke the Go driver's **compile**, so
> `run.sh` aborted at "go side failed" for ALL scenarios — the harness was dark
> for every provider, not just the fallback ones. Repaired:
> `~/.cache/pi-diff/go/main.go` drops the `refusalFallbacks` field and its
> assignment (the driver's `DisallowUnknownFields` now makes a scenario still
> carrying the retired option a hard error rather than a silent no-op);
> `config.env` `PI_UPSTREAM_SHA` advanced `3a0b9a3ee` → `ed867e909` (exactly one
> first-parent commit, so no other `src` scenario's ground truth moves); and the
> four `refusal-fallbacks-anthropic-*` scenarios were re-pointed at
> `model.compat.allowedFallbackModels` in the `{provider, model, cost}` shape,
> which is now the only way to drive the feature. **40 → 42:**
> `-default` is RETIRED (upstream deleted the `"default"` arm) and is replaced by
> `-empty-list` and `-no-compat` (both must OMIT the key), plus a new
> `-cross-provider` pinning the asymmetry `ed867e909` introduced — the provider
> match guards the cost lookup only, so an entry for another provider must still
> reach the wire. Non-vacuity checked by reading the captures rather than
> trusting the PASS: the `fallbacks` arrays are present and byte-identical on
> both sides (including the `vertex` entry on an `anthropic` model), and
> genuinely absent on both sides for the empty-list and no-compat controls.

## Drift at last sync check (2026-08-18) — pin advanced to 2509b5c03

Delta `d3e3bbc01..2509b5c03`, **21** first-parent changes, no merges. **No
release crossed** — `packages/ai` and `packages/coding-agent` are on 0.84.2 at
both ends, so the npm reference build stays **0.84.2** and no tag is cut this
cycle. Verdicts: **6 port → 7 Go commits; 3 catalog-only (queued); 12 n/a;
0 decide**.

Whole-range reconciliation (the merge-smuggling guard) found in-scope deltas in
exactly 26 files, all attributed: `ai/src/api/anthropic-messages.ts`
(`eb1f87fa9` + `e5dde9a76`), `ai/src/api/google-{generative-ai,shared,vertex}.ts`
(`af2c35223` + `e5dde9a76`), `ai/src/api/openai-{completions,responses}.ts` +
`ai/src/api/pi-messages.ts` (`e5dde9a76`), `ai/src/api/{azure-openai-responses,
bedrock-converse-stream,mistral-conversations,openai-codex-responses}.ts`
(`9117326b4`/`10acee604` + `e5dde9a76`, all unported providers),
`ai/src/index.ts` (`af2c35223` type-export rename), `ai/src/types.ts`
(`eb1f87fa9` + `e5dde9a76`), `agent/src/{agent-loop,agent}.ts` (`2509b5c03`),
`core/agent-session.ts` + `core/extensions/{index,types}.ts` (`a6b1dbceb`,
extensions runtime), `core/compaction/compaction.ts` (`eb1f87fa9` +
`90305d90a`), `core/compaction/branch-summarization.ts` (`90305d90a`, no Go
home), `core/model-resolver.ts` (`1c28f3032`), `core/package-manager.ts`
(`080932e53` + `5e11f6586`), `core/remote-catalog-provider.ts` +
`utils/management-http.ts` (`df018b602`, unported host), `src/main.ts` +
`src/cli/args.ts` (`b82a374c7`, behavior-neutral test plumbing), and
`ai/scripts/generate-models.ts` (the three catalog-only changes plus
`eb1f87fa9`'s generator half). The deferred harness backlog is unchanged at
**11** — nothing in this delta touches `agent/src/harness`. The catalog-only
queue goes **2 → 6**. The `defaultTools` tripwire (2026-08-13) was not hit.

### Port worklist (6 → 7 Go commits)

| upstream | subject | Go | notes |
|---|---|---|---|
| `af2c35223` | fix(ai): honor Google thinking level maps | `156749d` + test re-pin `86caf32` | New `resolveGoogleThinkingLevel` (ai/providers/google.go) resolves the clamped level through `model.ThinkingLevelMap` before either the Gemini-3 `thinkingLevel` table or the 2.5-family budget table, and the custom-`thinkingBudgets` lookup keys off the RESOLVED level. A level landing outside Google's four standard levels is now an **error**, retiring the F4c divergence (xhigh used to fall through both tables into `thinkingConfig:{includeThoughts:true}` with neither key) — that test now asserts the rejection and that no request is sent. pi throws synchronously out of `streamSimple`; the Go seam returns a stream, so the failure is a terminal error event via a new unexported `terminalErrorStream` (ai/providers/errors.go), the same shape the in-flight `fail()` paths produce. Error text byte-exact against pi's template including JS `String(mapped)` rendering (absent key → `undefined`, explicit null → `null`). NO Go home: the `google-vertex.ts` half and the `GoogleThinkingLevel` → `GoogleApiThinkingLevel` type-export rename. |
| `1c28f3032` | fix(ai): update cloudflare gateway sonnet test id | `416917c` | Subject lies: the only in-scope hunk flips `defaultModelPerProvider.cerebras` from `zai-glm-4.7` to `gpt-oss-120b` (coding/resolve.go). The 0.84.2 catalog carries `cerebras/gpt-oss-120b`, so the new default is not dangling. |
| `eb1f87fa9` | fix(coding-agent/ai): anthropic refusal error and fallbacks | `13c801e` | `SimpleStreamOptions.RefusalFallbacks` (ai/types.go) carries pi's `"default" \| readonly {model}[]` union; Go keeps BOTH arms rather than collapsing them (they are different values on the wire) as `AnthropicRefusalFallback{Default bool, Models []string}` with a `MarshalJSON` emitting whichever is set — nil pointer is pi's absent option. Threaded through all three `StreamSimpleAnthropic` paths; adds `server-side-fallback-2026-07-01` **third** in pi's beta order, on every auth branch; lands as the request's last key. `message_start` now overwrites `output.Model` with the served model (pi assigns unconditionally, so the port does too) — that is how a fallback becomes visible on the message. Compaction asks for a fallback when the summarization model is a first-party Anthropic model whose catalog compat lists permitted targets, using the FIRST only; Go reads `allowedFallbackModels` off the raw compat blob inside that one helper, which is where pi keeps the equivalent typed read. Generator half → catalog queue. **SUPERSEDED by `ed867e909`: `SimpleStreamOptions.RefusalFallbacks`, the union and its `"default"` arm are gone, and `getAnthropicSummarizationFallback` was deleted upstream — compaction no longer picks a fallback per call site. The beta ordering and the `output.Model` overwrite both survive. Not live API.** |
| `e5dde9a76` | feat(ai): add simple tool choice option | `ee68701` | `ai.ToolChoice` (`"auto"`/`"none"`) on `SimpleStreamOptions`, mapped onto each ported provider's native shape: anthropic wraps the bare string as `{type}`, openai-completions/openai-responses pass it through, google upper-cases into a `functionCallingConfig` mode, pi-messages puts it in `requestOptions`. Empty stays pi's absent option — no provider invents a selection, and google keeps deriving AUTO/VALIDATED from the tools alone. Also closes pi's own asymmetry: pi-messages read `toolChoice` off the provider-extra object, so it was unreachable through the unified entry point (the Go doc comment had claimed the forwarding that only now exists). NO Go home: the bedrock/azure/mistral/codex/vertex halves. |
| `90305d90a` | fix(coding-agent): disable tools during summarization | `b79c9e6` | Summarization requests carry `toolChoice: "none"`, and a response containing a `toolCall` fails the summarization instead of checkpointing whatever text rode with it. pi puts the guard in each of `completeSummarization`'s three callers, differing only in the thrown message; the port has two of those callers and no channel for those strings (a failed summarization is "keep the current view" either way), so the guard sits in `completeSummarization` itself, covering exactly the same call sites. NO Go home: the `branch-summarization.ts` guard — the port consumes stored branch summaries and never generates one. |
| `2509b5c03` | feat(agent): expose provider context construction | `0f0b461` | The transform-then-convert pipeline `streamAssistantResponse` ran inline is now exported `BuildProviderContext` (agent/loop.go), with `Agent.BuildProviderContext` (agent/agent.go) delegating using the agent's own hooks. pi's `Pick<AgentLoopConfig, "convertToLlm" \| "transformContext">` is expressed against the whole config; pi's optional `AbortSignal` is the Go hook's existing `context.Context`. Pure refactor plus new public surface — no wire change. **SUPERSEDED 2026-08-19: upstream reverted `2509b5c03` in `3a0b9a3ee` the next day, and the port un-ported it in `0cfa63f`. `BuildProviderContext`, `Agent.BuildProviderContext` and `ContextPipeline` no longer exist — do not treat this row as live surface.** |

### Port-but-CATALOG-ONLY — queue 2 → 6 (parked for the next release regen)

| sha | generator delta | lands as |
|---|---|---|
| `0e4d49541` | remove deprecated Xiaomi models | next `models.generated.ts` regen |
| `87205484b` | Chinese ZAI Coding Plan catalog (77 generator lines) | next regen |
| `6db110e6f` | Qwen Token Plan Individual DeepSeek V4 Pro 0813 | next regen |
| `eb1f87fa9` | `ANTHROPIC_ALLOWED_FALLBACK_MODELS` + `applyAnthropicMessagesCompatMetadata`: `claude-fable-5` → `[claude-opus-4-8, claude-opus-5]`, `claude-opus-5` → `[claude-opus-4-8]`, anthropic provider only | next regen — until then the ported consumer finds no `allowedFallbackModels` and asks for no fallback, which is the 0.84.2 dist's behavior. **SHAPE AMENDED TWICE — by `4809c2abc` (2026-08-19) and again by `ed867e909`. These are no longer bare strings, and no longer `{model, cost}` either: the generator emits `{provider, model, cost}`. See the 2026-08-19 queue table and its regen tripwire; regenerating to the shape written in THIS row leaves the slice empty, so no `fallbacks` field and no beta go out at all.** |

Carried from 2026-08-17: `70e878d4c` (xai routing/thinking-map) and `86d001d36`
(DS4-Flash low on opencode/opencode-go).

### n/a (12)

| sha | subject | reason |
|---|---|---|
| `080932e53` | fix(package-manager): use semver.gt for version comparison | package-manager's npm install/update half — unported packaging (recorded n/a since 2026-06-14) |
| `df018b602` | fix(coding-agent): retry hung model catalog requests | `remote-catalog-provider` + `utils/management-http` — unported host surface |
| `a6b1dbceb` | fix(extensions): emit compaction failed for extensions | new `session_compact_failed` extension event; the `agent-session.ts` hunks exist only to feed it (the `fromExtension` hoist and the extracted `errorMessage` leave `compaction_end`'s values unchanged). Extensions runtime is unported |
| `1d08508ef` | fix(extension-examples): use agent_settled instead of end | examples |
| `10acee604` | fix(ai): bedrock response to include smithy headers | bedrock (unported provider) |
| `5e11f6586` | fix(coding-agent): load nested markdown skills | widens `collectSkillEntries` for mode `"agents"` only; a no-op for the `"pi"` mode the port implements. Surfaced a pre-existing gap — see the 2026-08-18 ruling |
| `54d22b74b` | fix(coding-agent): reduce redundant git update tests | tests |
| `b82a374c7` | fix(coding-agent): reduce redundant slow tests | tests; the two `src` hunks are a behavior-preserving `normalizeSessionName` extract and an export widened for tests |
| `209bc7b9a` | fix(ai): remove unused opentelemetry dependency | dependency removal, no `src` delta |
| `9117326b4` | fix(ai): forward Azure Responses tool choice | azure (unported provider) |
| `ad58801ce` | fix(ai): update Baseten GLM input modalities | test-only; catalog data drift, which arrives via the regen |
| `8af7690c4` | fix(coding-agent): skip trusted subagent prompts | subagent example + project-trust gating (excluded 2026-06-12) |

### Review gates

Independent **pi-parity-review**: **6/6 FAITHFUL**, with the evidence recorded
in the pin table above. Two nits it raised, neither behavioral: the phrase
"lands as the request's last key" describes **pi's source order**, not the Go
wire — the Go body is a `map[string]any`, so top-level keys serialize
alphabetically and `fallbacks` lands first (JSON object order is not
semantically meaningful, and the harness only compares order at declared
order-sensitive paths); and `anthropic.go`'s native `ToolChoice` guard is
`!= nil` where pi is truthy, so a NATIVE caller passing `""` would emit
`tool_choice:{"type":""}` — unreachable from the ported simple path, which
guards on `!= ""`, and pre-existing.

Blast radius the reviewer traced and cleared: the served model from
`message_start` now flows into every `am.Model == model.ID` gate
(`ai/providers/transform.go:122` thinking-block retention, `google.go`,
`openai_responses.go`). pi has the identical assignment and the identical
consumers, so a fallback turn flips those gates on both sides; cost stays
computed against the REQUESTED model on both sides.

Independent **pi-go-review**: **fix-first — 2 MED + 7 LOW**, all applied in
`b548506` (see below). It endorsed three of the porter's judgement calls: the
inline compat decode in `anthropicSummarizationFallback` (every other consumer
decodes `Model.Compat` locally — house pattern), the single guard site in
`completeSummarization` (verified a strict cover of pi's three), and — after
the fix — the seam for pi's synchronous throw.

### Deliberate divergence added this cycle

`ai.AnthropicRefusalFallback` **cannot express pi's empty-array arm.** pi's type
is `"default" | readonly {model}[]`, which admits `[]`; the Go type collapses the
union onto the chain (empty chain IS `"default"`), the way `DeferredRequest`
collapses its own. Rationale: no pi code path produces `[]`, and a model whose
compat lists no permitted targets must OMIT `fallbacks` rather than send it
empty, which Anthropic rejects. The first shape kept both arms as a
`Default bool` beside `Models`, which go-review rejected — it made
`{Default, Models}` both-set representable and made the zero value marshal to
`[]`, i.e. exactly the shape the doc warned against.

> **RETIRED by `ed867e909`.** `ai.AnthropicRefusalFallback`,
> `ai.AnthropicRefusalFallbackTarget`, `SimpleStreamOptions.RefusalFallbacks`
> and `AnthropicOptions.RefusalFallbacks` are all gone: upstream deleted the
> union and the option, and both sides now derive `fallbacks` from
> `model.compat.allowedFallbackModels`. There is no Go type left to be unable to
> express `[]`, and pi has no `[]` producer either. Kept for the record of why
> the collapse was shaped the way it was; **not a live divergence.**

### Review fixes (`b548506`)

MED: the union reshape above (plus the missing `UnmarshalJSON`, with errors that
name what a valid value looks like); and `ai.errorStream` **exported as
`ai.ErrorStream`** so the google provider calls it instead of the verbatim copy
this cycle had added to `ai/providers/errors.go` — the two copies had already
started to drift from the in-flight `fail()` paths on `StopAborted`.
LOW: don't alias the decoded compat slice; one pass over `msg.Content` instead of
two; pi's `Pick<AgentLoopConfig, …>` expressed as a named `agent.ContextPipeline`
rather than passing a 30-field config to read two, with `AgentContext` taken by
value in the exported entry point (`Agent.Context()` returns one by value)
— **both removed 2026-08-19 with the un-port (`0cfa63f`); `ContextPipeline`
existed only to serve `BuildProviderContext`, so its removal is entailed**;
per-level subtests where a randomized map loop hid failures; per-provider
expectations moved into the tool-choice table; `textOf` deduplicated.

### Harness follow-ups — DONE (same day)

Both items the parity reviewer flagged are closed, and the shipped harness now
runs **34 PASS / 0 KNOWN / 0 FAIL** (was 21).

- `~/.cache/pi-diff/config.env` `PI_UPSTREAM_SHA` advanced `086c32e74` →
  `2509b5c03` (it was two pins stale). This stopped being cosmetic the moment
  the scenarios below landed: they are the first shipped `backend:"src"`
  scenarios since the 0.84.2 caveats cleared, so a stale pin would now silently
  extract the wrong upstream pi.
- **13 scenarios promoted** into `~/.cache/pi-diff/scenarios` covering the
  simple-path `toolChoice` (anthropic none/auto/unset/adaptive/reasoning;
  openai-completions and openai-responses none/unset) and `refusalFallbacks`
  (single-model chain, two-model chain with order asserted, the `"default"`
  literal arm, and one combined with a tool choice). The driver
  (`~/.cache/pi-diff/go/main.go`) threads both through `SimpleStreamOptions`;
  `refusalFallbacks` decodes straight into `ai.AnthropicRefusalFallback`, so the
  scenarios exercise the new `UnmarshalJSON` as well as the wire shape.
  Non-vacuity checked by reading the captures rather than trusting the PASS:
  `tool_choice:{"type":"none"}` and the two-entry `fallbacks` array are present
  and identical on both sides, and genuinely absent on both sides when unset.
  **`refusalFallbacks` half SUPERSEDED by `ed867e909`** — the option, the union
  and its `UnmarshalJSON` are gone, and the four scenarios now drive
  `model.compat.allowedFallbackModels`; see the 2026-08-19 harness amendment
  above. The `toolChoice` half is unaffected.

## Drift at last sync check (2026-08-17) — pin advanced to d3e3bbc01

Delta `086c32e74..d3e3bbc01`, **13** first-parent changes, no merges. **No
release crossed** — the npm reference build stays **0.84.2** and no tag is cut
this cycle. Verdicts: **5 port → 6 Go commits; 1 catalog-only (queued); 7 n/a;
0 decide**.

Whole-range reconciliation (the merge-smuggling guard) found in-scope deltas in
exactly 13 files, all attributed: `ai/src/api/openai-completions.ts`
(`70e878d4c` UA + `d3ab2af96` usage), `ai/src/api/openai-responses.ts` +
`ai/src/providers/xai.ts` + `ai/src/utils/pi-user-agent.ts` +
`core/model-resolver.ts` (`70e878d4c`), `core/agent-session.ts` (`47bf47f11`
comments + `58302d34e` call sites + `c7c763f5c`), `core/compaction/compaction.ts`
(`58302d34e`), `core/tools/edit.ts` (`ca21c1686`), `core/skills.ts`
(`8c2529dae`), `core/extensions/{loader,types}.ts` (`f47faf459`, extensions
runtime), `agent/src/harness/tools/edit.ts` + `agent/src/harness/skills.ts`
(the two deferred halves). The deferred harness backlog goes **9 → 11**
(`ca21c1686` harness edit half, `8c2529dae` harness skills half). The
catalog-only queue REOPENS **0 → 2** (below). The `defaultTools` tripwire
(2026-08-13) was not hit.

### Port worklist (5 → 6 Go commits + review fixes)

| upstream | subject | Go | notes |
|---|---|---|---|
| `70e878d4c` | feat(ai): route xAI models through Responses and default to Grok 4.6 | `b047852` + test re-pin `82cda36` | Ported now: `forcePiUserAgent` (utils/pi-user-agent.ts) as a single canonical `Header.Set` in `ai/providers/pi_user_agent.go` — every merge path writes through Set/Del, so one Set replaces all case variants (the kimi-cycle equivalence) — wired after the options merge in BOTH the completions and responses builders under `model.Provider == "xai"`; and `coding/resolve.go` xai default `grok-4.5` → `grok-4.6` (grok-4.6 exists in the 0.84.2 catalog, so not dangling; the a01baaae-era fallback-template test re-pinned to 4.6). NO Go home: the `providers/xai.ts` api-map narrowing to `Provider<"openai-responses">` — Go dispatches on the catalog's per-model `api` (`ai/stream.go` `resolveProvider(model.Api)`), so the routing flip rides the next catalog regen (queued below). Wire effect until then matches the 0.84.2 dist: grok-4.5 via responses (store:false + encrypted-content include pre-exist at `openai_responses.go:996`), grok-4.3/4.6/build-0.1 via completions. Headers are outside the bodies-only harness; UA locked by 3 wire-capture tests (consumer header loses on xai over both APIs; non-xai keeps the merge result), red-observed. |
| `d3ab2af96` | fix: track kimi cached tokens | `44365e3` | `parseChunkUsage` cache-read chain gains the third arm: `prompt_tokens_details.cached_tokens ?? prompt_cache_hit_tokens ?? cached_tokens ?? 0` (Kimi documents top-level `usage.cached_tokens` on the final usage chunk). `PromptCacheHitTokens` pointer-ized so an explicit 0 (DeepSeek's no-hit shape) stops the chain — pi's `??` is nullish, not falsy; locked by two new tests (top-level fallback red-observed; explicit-zero middle arm). Response parsing only — no golden surface. |
| `58302d34e` | feat(coding-agent): support compaction routing sessions | `56a14c3` | `CompactionSettings` gains optional `SessionID`; threaded `compact` → `generateSummary`/`generateTurnPrefixSummary` → `completeSummarization` (the upstream param chain), which now does `?? uuidv7()` — Go form: `if sessionID == "" { sessionID = uuidv7() }`. Cache retention stays `"none"` (a routing ID is forwarded WITHOUT enabling prompt caching). `summarize()` (the branch-summary shape) passes "" — fresh per-request, like upstream's undefined. Upstream's own agent-session call sites pass `undefined`, so behavior is unchanged for existing callers: latent SDK plumbing (the `StreamOptions.Env` / `TelemetryContext` precedent). Red-observed via a faux-provider capture (fresh uuidv7 where the routing id belonged). |
| `ca21c1686` | fix: single edit input | `888ae01` | `prepareEditArguments` wraps a bare `{oldText,newText}` object — raw or JSON-stringified — into a one-element `edits` array (`isSingleEditInput`: non-array object, both fields strings). Non-edit shapes pass through untouched and keep failing schema validation, exactly upstream's fallthrough. Harness half (`harness/tools/edit.ts`) → deferred backlog. |
| `8c2529dae` | fix: dont load root mds as skills in settings | `92c0bc4` | `loadSkillFromFile` (coding/resources.go): markdown files not declared as skills (basename ≠ `SKILL.md`) with no usable description are silently skipped — no skill, NO diagnostics (a stray README.md in a skills root no longer warns); `SKILL.md` keeps the full warnings including "description is required". Description/name count only when **string-typed**: new `fmValue.isString()` screens YAML-1.2-core non-string plain scalars (null/bool/int/float literals, matching the `yaml` package's core-schema resolution upstream's `typeof` guards sit on); quoted/block scalars always strings; non-string name falls back to the directory name. NO Go home: upstream's parse-error-warns-only-for-SKILL.md branch — the port's forgiving line parser never fails. Harness half (`harness/skills.ts`) → deferred backlog. |

### Port-but-CATALOG-ONLY — queue reopens (0 → 2, parked for the next release regen)

| sha | generator delta | lands as |
|---|---|---|
| `70e878d4c` | all xai models → `api: "openai-responses"` + `XAI_RESPONSES_COMPAT`; `{off: null, minimal: null}` only for xai models WITHOUT a models.dev thinkingLevelMap (grok-build-0.1 keeps reasoning always-on, never sends none/minimal); copilot `needsResponsesApi` widens `grok-4.5` → `grok-*` | next `models.generated.ts` regen (grok-4.6 xhigh arrives via models.dev reasoning_options at the same regen) |
| `86d001d36` | `DEEPSEEK_V4_FLASH_THINKING_LEVEL_MAP` (low effort) applied to `opencode`/`opencode-go` providers via `id.includes("deepseek-v4-flash")`, not just provider `deepseek` | next regen |

### n/a (7)

| sha | subject | reason |
|---|---|---|
| `47bf47f11` | docs(coding-agent): clarify compaction paths | comment-only; documents the manual-//compact + hook + `compaction_end` paths, which are recorded-absent agent-session-runtime (Go ports only automatic compaction as a TransformContext hook) |
| `c7c763f5c` | fix(coding-agent): clarify truncated recovery failure | the errorMessage split (truncated vs overflow) lives in the unported overflow-recovery branch + `compaction_end` emission — both recorded absent |
| `f47faf459` | fix: register flag type mismatch | extensions runtime only (`core/extensions/{loader,types}.ts`) |
| `955a543b3` | fix: expose sleeping llama.cpp models | llama.cpp extension (`src/extensions/llama/`) |
| `374e56e55` | fix(tui): avoid duplicate VS Code right-click paste | TUI |
| `a1bc0ec79` | fix: llama.cpp guidance as no default | modes/interactive (TUI mode) |
| `d3e3bbc01` | fix: llama.cpp allow network for model discovery | llama.cpp extension |

### Review gates

Independent **pi-parity-review**: **5/5 FAITHFUL** (+ the go-review refactor
confirmed a no-op). The single-`Set` rendering of pi's
delete-every-case-variant-then-set proven by auditing every header write path
in `ai/providers` (all `Set`/`Del`, no raw map writes) with exactly-once wire
asserts; the UA string **byte-compared against `getPiUserAgent()` executed
from the authentic 0.84.2 dist** (`pi (darwin 25.5.0; arm64)` both sides). The
no-Go-home claim for the `xai.ts` narrowing verified mechanically: upstream's
generated models data is **not checked in** (built from models.dev at
release), so the flip genuinely lands at the next regen — interim state
recorded: Go serves grok-4.6 over completions exactly like the 0.84.2 build,
upstream-at-HEAD over Responses; **converges at the regen**. The kimi chain
byte-checked at the sha (the pointer-ized middle arm is REQUIRED for
correctness once a third arm exists). The skills `isString` screen verified
against the real reference — **yaml@2.9.0** (exact version pinned at the sha
and in the build), a 74-literal adversarial probe agreeing **74/74** on
classification and value. **7 mutations in a scratch worktree, each red for
the right reason** (xai force removed → "custom-agent" on the wire; kimi third
arm removed → cacheRead=0; middle arm made falsy → 0 falls through; routing id
ignored → fresh uuid; edit wrap removed; silent-skip removed → README warns;
isString→true → boolean descriptions load). Goldens: **none moved** — catalog
untouched and independently re-derived `cmp`-identical (536,642 B).
Differential harness: **21 PASS / 0 KNOWN / 0 FAIL** on the 0.84.2 dist,
exercising both changed builders.

Independent **pi-go-review**: **SHIP** (1 LOW, applied in `64f88ab`): the
kimi-coding site now reuses `forcePiUserAgent` instead of hand-rolling the
identical operation. Explicitly cleared, not flagged: the single-`Set`
equivalence (every write traced), the `*int`-as-absent nullish pattern,
`CompactionSettings.SessionID` as the natural Go home for upstream's trailing
optional param (zero-value-useful, so existing constructors unaffected),
`isSingleEditInput` tighter-but-equivalent over JSON values, and the
`isString` regexes probed against YAML 1.2 core edges (1.1 forms —
`yes`/`on`/`0b1010`/`1_000` — correctly stay strings). `gofmt`/`go vet`/full
`-race` clean.

Pre-existing residual recorded by parity (predates the range, unchanged by
it): inputs where real YAML throws (`description: -`) or yields collections
(`description: [a, b]`) still load as skills in Go — the frontmatter parser's
documented NOT-supported scope.

**No new public Go API beyond the additive `CompactionSettings.SessionID`
field (standing-formula latent plumbing). +10 test functions (3 xai UA wire, 2
usage-chain, 1 compaction routing, 1 single-edit, 2 skills, 1 xai default
guard) plus 1 re-pinned stale expectation. No goldens moved (headers are
outside the bodies-only harness; no body/system-prompt/tool-string/
session-format changes).**

## Drift at last sync check (2026-08-15) — pin advanced to 086c32e74

Delta `f3c406a9b..086c32e74`, **14** first-parent changes, no merges. **Release
v0.84.2 crossed** (`914cf1472`; pi-ai + pi-coding-agent 0.84.1 → 0.84.2) — the
npm reference build moved to the integrity-verified 0.84.2 install, the catalog
was regenerated, and **port tag `v0.84.18` was cut this cycle**. Verdicts:
**4 port → 3 Go commits + 1 verified no-op + 1 review-fix commit; 10 n/a; 0
decide**.

Whole-range reconciliation (the merge-smuggling guard) found in-scope deltas in
exactly 9 files, all attributed: `ai/src/api/google-generative-ai.ts` +
`google-vertex.ts` (`5093641a5`; vertex half unported),
`ai/src/auth/oauth/device-code.ts` + `github-copilot.ts` (`d5278eaac` +
`086c32e74`, OAuth token acquisition — non-ported), `ai/src/image-models.generated.ts`
(`914cf1472`, images unported), `core/model-resolver.ts` (`e429d90b8`),
`core/project-trust.ts` + `core/session-manager.ts` + `main.ts` (`ab0dc51fc`).
`packages/agent/src` untouched — the deferred harness backlog stays **9**
(through `b75be04d9`). The catalog-only queue goes **4 → 0**: every queued
generator delta shipped in the 0.84.2 build and landed with the regen. The
`defaultTools` tripwire (2026-08-13) was not hit.

### Port worklist (4 → 3 Go commits + 1 no-op + review fixes)

| upstream | subject | Go | notes |
|---|---|---|---|
| `5093641a5` | fix(ai): preserve Google length stops with tool calls | `ffcd83a` | The toolCall→toolUse stop override in the google stream gains upstream's guard: apply only when the freshly-mapped reason is `stop`, so MAX_TOKENS keeps `length` (and error-reasons keep `error`) even when the truncated turn carries a tool call. Go form: `if reason == ai.StopStop` around the builders loop (`ai/providers/google.go`) — same value, same point in flow; `builders` ≡ `output.Content` in lockstep (parity-verified). Response-side only: NOT harness-visible (bodies-only). Tests mirror upstream's two new cases (MAX_TOKENS+toolCall → length with the call surviving in content; STOP+toolCall → toolUse), red-observed before the fix ("got toolUse"). Vertex half n/a — `APIGoogleVertex` is a types constant only; no vertex stream exists in Go. |
| `ab0dc51fc` | fix(coding-agent): use APP_NAME in user-facing messages | *no-op (verified)* | Upstream swaps hardcoded "pi" for `APP_NAME = pkg.piConfig?.name \|\| "pi"` across 6 files; the shipped 0.84.2 package carries `piConfig: {configDir: ".pi"}` with **no `name`**, so every changed string is byte-identical pre/post for stock pi — the constant folds to `"pi"`. Parity review enumerated all 9 hunks: the only one in ported surface is session-manager's invalid-session error, whose Go home (`coding/session_store.go:219`) is the already-recorded wording divergence (parity-sweep-2 I13); auth-command/package-manager-cli (CLI packaging), project-trust (gating), interactive-mode (TUI), and the main.ts `-ne` hint (extensions runtime) have no Go homes. Nothing to port; no branding constant introduced (packaging-derived white-labeling is unported surface). |
| `e429d90b8` | fix(coding-agent): update Z.AI Coding Plan defaults | `4abced4` | `coding/resolve.go`: `zai` + `zai-coding-cn` defaults `glm-5.1` → `glm-5.3`, and ONLY those (vercel-ai-gateway stays `"zai/glm-5.1"`, per upstream; full 41-entry table verified entry-for-entry at the sha). Also ports upstream's new guard test: every catalog provider's default must resolve to a real catalog model (`TestDefaultModelsExistInCatalog`; iterates catalog providers, so table-only entries like `radius` stay out of scope, matching upstream). Red-observed: the guard caught exactly the two dangling zai defaults — glm-5.1 had left the zai catalogs at **0.84.1**, so the defaults dangled for a whole release; 0.84.2 adds glm-5.3 and upstream re-points them. Mutation-verified by parity review (revert → the guard fires). |
| `914cf1472` | Release v0.84.2 | `69bcdf1` | Catalog regen from the **0.84.2** build (`~/.cache/pi-npm/0.84.2`; package-lock integrity == registry `dist.integrity`). Parity reviewer re-derived `JSON.stringify(MODELS)` from `dist/models.generated.js` independently: **byte-identical** (536,642 B single line), endpoint-pinned at both ends (old embed ≡ 0.84.1 derivation). 1220 → 1267 models (+71/−24), 39 providers. **Flushes all four queued generator deltas**: deepseek `compat.maxTokensField:"max_tokens"` (`c185d4123`), `supportsStrictMode` 0→34 cloudflare-ai-gateway openai-responses models (`75c7fd662`+), `deepseek-v4-flash` `thinkingLevelMap.low` null→"low" (`2f8b4b42f`), kimi-coding static `KimiCLI/1.5` headers removed on all 4 models (`9d2ec7ffa` — coherent with the runtime UA override, which now merely re-asserts what the catalog already says). Schema drift: exactly one new key, `compat.supportsAdditionalTools`, already decoded (`ai/providers/openai_responses.go:80`; absent→false ≡ dist `?? false`). Orphan sweep of the 24 removed ids: clean (remaining `gemini-3-pro-preview`/`kimi-k2.5` test hits are synthetic fixtures or different-provider catalog entries). `image-models.generated.ts` half excluded (images unported). Generator artifact, no action: `opencode/grok-build-0.1` flipped api to openai-responses but kept completions-only `compat.supportsReasoningEffort:false` — the responses runtime never reads it in either implementation (inert both sides). |

### n/a (10)

| sha | subject | reason |
|---|---|---|
| `c5b7cd2a3` | approve contributors #8063 | meta |
| `48bd3f424` | approve contributors #8092 | meta |
| `b1efcf7d7` | approve contributors #8124 | meta |
| `4caa3c440` | route selection copy through the host clipboard (#8110) | TUI only (`tui/tui-alt-screen.ts` + interactive-mode) |
| `d10c974b9` | docs: audit changelogs since v0.84.1 | changelogs only |
| `9b4adc823` | docs: audit changelogs since v0.84.1 | changelogs only |
| `0e0021fbb` | Add [Unreleased] section for next cycle | changelogs only |
| `c36552212` | agents: Adjust rules for changelog entries | `.pi/` + AGENTS.md (upstream repo agent config) |
| `d5278eaac` | enable Copilot model policies sequentially during login | entirely `ai/src/auth/oauth/**` — OAuth token acquisition, deliberately not ported; login-time only, no boundary change |
| `086c32e74` | retry Copilot GET /models once on 429 during login | same — `auth/oauth/{device-code,github-copilot}.ts` + test only |

### Review gates

Independent **pi-parity-review**: **4/4 FAITHFUL, no divergences.** Google
guard proven same-value/same-point against the sha (multi-finishReason-chunk
edge included); `mapGoogleStopReason` entry-for-entry ≡ upstream
`mapStopReason`; mutation-verified in a scratch copy (guard removed → the new
test fails with "got toolUse"). APP_NAME no-op proven against the **build**
(shipped `piConfig` has no `name`). Zai table compared whole (41 entries).
Catalog re-derived independently and `cmp`-identical; schema drift enumerated
against consuming types (`Compat` is a raw per-api passthrough; both live keys
decode). Harness flip verified: all 13 shas cited across scenario notes are
first-parent ancestors of `914cf1472`, and the reviewer's own re-run printed
**21 PASS / 0 KNOWN / 0 FAIL, exit 0**.

Independent **pi-go-review**: **SHIP** (4 LOW + 1 informational, LOWs applied
in `631a010`): glm-5.1 provenance comment corrected (it left the catalogs at
0.84.1, not 0.84.2), shared `googleStreamSSE` test helper extracted,
`slices.ContainsFunc` for the tool-call scan, zai cohort cases wrapped in
`t.Run` subtests. The informational catalog note is recorded in the release
row above. Catalog embed independently confirmed byte-identical; no orphaned
ids; `gofmt`/`go vet`/`-race` all clean.

### Differential harness — fully dist-backed for the first time

With 0.84.2 shipping every wire-relevant behavior the port carries, all **15**
remaining `backend: "src"` scenarios flipped to `"dist"` (README rule), and
`config.env` moved to `PI_NPM_VERSION=0.84.2`, `PI_UPSTREAM_SHA=086c32e74`.
**All 21 scenarios now compare against the published build — the strongest
ground truth — with `known-divergences.json` EMPTY: 21/21 PASS, 0 KNOWN, 0
FAIL.** This closes the 2026-08-12 pin note ("flip deepseek back to dist at
the next release") and clears all four recorded 0.84.1 dist caveats
(max_completion_tokens, case-sensitive deepseek.com, no strict conversion,
static KimiCLI/1.5): each now ships, so the dist is the reference for every
surface again. No goldens moved this cycle (nothing in the delta touches
session format, system prompt, tool output strings, or image decisions).

**No new public Go API. No new deliberate divergence.**

## Drift at last sync check (2026-08-14) — pin advanced to f3c406a9b

Delta `46bb9a2c3..f3c406a9b`, **10** first-parent changes, no merges. **No
release crossed**: no `packages/*/package.json` version bump (pi-ai still
0.84.1) and `models.generated.ts` untouched at both ends, so no catalog regen,
no npm reference-build move, and **no port tag** this cycle. Verdicts: **1 port
→ 1 Go commit + 1 review-fix commit; 9 n/a; 0 decide**.

Whole-range reconciliation (the merge-smuggling guard) found in-scope deltas in
exactly 5 files — `ai/src/api/anthropic-messages.ts`, `ai/src/utils/pi-user-agent.ts`
(both `9d2ec7ffa`, ported), `ai/src/api/openai-codex-responses.ts` (`9d2ec7ffa`,
excluded codex provider), and `core/tools/{find,grep}.ts` (`6f707eb36`, see its
row) — all accounted for. `packages/agent/src` untouched (harness backlog stays
9); the catalog-only queue grows **3 → 4** (see the port row).

### Port worklist (1 → 1 Go commit + review fixes)

| upstream | subject | Go | notes |
|---|---|---|---|
| `9d2ec7ffa` | fix(ai): use pi user agent for Kimi Coding requests | `68cdb4e` + `89511fa` | **Request-header golden surface** on the anthropic-messages wire — a surface the differential harness does NOT capture (bodies only, per its README); parity was proven by sha-anchored review + mutation-verified wire tests instead. pi's `mergeClientHeaders` (anthropic-messages.ts): after the FULL defaultHeaders merge, in all three `createClient` branches (copilot / OAuth / API-key, each after `optionsHeaders`), iff `model.provider === "kimi-coding"`, every case variant of `user-agent` is deleted and `User-Agent` is set to `getPiUserAgent()` = `` `pi (${platform()} ${release()}; ${arch()})` `` (new `utils/pi-user-agent.ts`; `"pi (browser)"` without node:os) — so the pi UA outranks the catalog's static header, the OAuth claude-cli identity, and consumer options. Go: one canonicalizing `Header.Set` at the tail of `applyAnthropicHeaders` (`ai/providers/anthropic.go`), equivalent because every write in the Go pipeline goes through `Set`/`Del` (parity-verified, no raw map writes); new unexported `piUserAgent()` (`ai/providers/pi_user_agent.go` + per-GOOS `osRelease`: linux `uname(2)` w/ int8-vs-uint8 generic, darwin+BSD `sysctl kern.osrelease`, windows `RtlGetVersion` "major.minor.build" ≡ libuv `uv_os_uname` ≡ Node, guarded `Find()`) with Node token maps (`windows→win32`; `amd64→x64`, `386→ia32`, `mipsle→mipsel`, `ppc64le→ppc64`), `sync.OnceValue`-cached. **Why it matters at 0.84.1:** the npm build predates this commit and ships `"User-Agent": "KimiCLI/1.5"` (in `dist/providers/data/kimi-coding.json` — per-provider dist layout), and our catalog carries it on all **4** kimi-coding models; the runtime override defeats the stale header until the next regen (test-locked, red-observed: wire showed `custom-client` before the fix). Tests mirror upstream's auth-token additions at the sha: catalog `KimiCLI/1.5` + consumer `user-agent` both replaced by exactly one pi UA (`headers.Values` length-1 assertion), non-kimi consumer passthrough, plus a token-map shape test (fallback GOOSes assert `"pi (browser)"`). Generator half (`generate-models.ts` drops `KIMI_STATIC_HEADERS`) is generator-only → **queued as the 4th catalog-regen delta**. Codex half + codex stream test: excluded provider (UA construction there unchanged, just moved to the shared util). |

### n/a (9)

| sha | subject | reason |
|---|---|---|
| `6f707eb36` | show managed-tool startup status in TUI | substance is `utils/tools-manager.ts` (recorded non-ported host-utils surface: `ensureTool`'s `silent` flag becomes an `onStatus` callback feeding new interactive-mode startup status) + `modes/interactive`; the `core/tools/{find,grep}.ts` hunks are call-site signature adaptation only (`ensureTool(x, true)` → `ensureTool(x)` — silent before, silent after). Go's find/grep are native implementations with no rg/fd provisioning, so neither half has a Go home |
| `9d2ec7ffa` | *(port — see worklist)* | — |
| `5f7195c51` | update Cloudflare compat test model | upstream test file only (`kimi-k2.5` → `kimi-k2.6` in coding-agent's cloudflare compat test) |
| `d268454e9` | accept array-form `tools` in the subagent example (#7598) | examples only |
| `721e2768e` | approve contributors #7973 | meta |
| `663361835` | update vulnerable nanoid dependency | npm lockfile bump; no Go dependency analogue |
| `83aed2ba5` | handle generic SGR mouse releases | TUI only |
| `e14afc648` | collapse fallback tool output (#7979) | `modes/interactive/components/tool-execution.ts` rendering component only |
| `0589435e7` | approve contributors #8007 | meta |
| `f3c406a9b` | approve contributors #8018 | meta |

### Review gates

Independent **pi-parity-review**: **CLEAN**, differential harness **21/21
PASS** (run although the surface is header-only, since `anthropic.go` request
building was touched — no body regression). Verified sha-anchored: all three
`createClient` branches route through `mergeClientHeaders` with no per-request
header bypass; the Go single-`Set` equivalence argument (every pipeline write
canonicalizes); Node token fidelity incl. the `RTL_OSVERSIONINFOW` layout;
npm-0.84.1-authentic dist carries `KimiCLI/1.5` (per-provider
`dist/providers/data/kimi-coding.json`) with no pi-user-agent module — TS at
`9d2ec7ffa` is the reference (unreleased). Mutation-verified in a throwaway
worktree: override removed → kimi test fails with `[custom-client]`; override
made unconditional → non-kimi test fails. Two INFO notes, no action: (1)
unshipped-GOOS fallback (solaris/aix would say `pi (browser)` where Node says
`pi (sunos …)` — conditions coincide on every platform the port targets,
comment records it); (2) the non-kimi test asserts consumer *passthrough*
rather than upstream's *absence*, because Go's transport injects
`Go-http-client/1.1` when unset — pre-existing base-UA territory the harness
README scopes out.

Independent **pi-go-review**: **CLEAN** (1 should-fix + 2 nits, all applied in
`89511fa`): fallback-GOOS test degradation, `Find()` guard keeping windows
`osRelease`'s soft-fail contract total, lowercase `user-agent` key for
intra-function consistency. Judged sound: the 5-file build-tag split, the
load-bearing `utsField` generic (stdlib `Utsname` is `[65]int8` vs `[65]uint8`
per GOARCH), first-in-repo `sync.OnceValue`, zero new exported identifiers
(tighter than upstream's `utils/` placement), stdlib `syscall` over an x/sys
dependency. Full `-race` suite green; cross-vetted on 21 GOOS/GOARCH combos.

**No new public Go API.** No new deliberate divergence beyond the two INFO
notes above (recorded here, not in the divergence ledger — both are
outside-target-platform or pre-existing-transport territory).

## Drift at last sync check (2026-08-13) — pin advanced to 46bb9a2c3

Delta `2e4d23959..46bb9a2c3`, **9** first-parent changes, no merges. **No
release crossed**: no `packages/*/package.json` version bump (pi-ai still
0.84.1) and `models.generated.ts` untouched at both ends, so no catalog regen,
no npm reference-build move, and **no port tag** this cycle. Verdicts: **0
port; 9 n/a; 0 decide** — a triage-only pin advance with no Go commits and no
review gates.

Whole-range reconciliation (the merge-smuggling guard) over
`packages/{ai,agent}/src` + `coding-agent/src/{core,main,sdk}` found 4 files,
25(+)/9(−), every hunk accounted for by the rows below; `packages/ai/src` and
`packages/agent/src` untouched entirely (harness backlog stays 9, catalog-only
queue stays 3).

### n/a (9)

| sha | subject | reason |
|---|---|---|
| `6d520c58d` | fix ascii alignment in compaction docs | docs only |
| `47b5119d0` | trigger turn false should not start turn (#8022) | 1-line fix inside `sendCustomMessage` — recorded unported (agent-session-runtime custom-message delivery, see the `8c0ccd14` note); Go's agent-level `Steer`/`FollowUp` queues carry no `triggerTurn` option |
| `4d9aa837c` | feat: configurable default tools | `defaultTools` **setting** consumed by `createAgentSession` via settings-manager (deliberate non-port); no new SDK option added. Same class as the recorded `findInitialModel` treatment — settings-driven initial selection stays host-side; Go hosts pass `ToolNames` explicitly |
| `541045ae0` | fix: preserve extension tools with defaults | follow-up narrowing the above: the setting seeds only the initial *built-in* selection and the allowlist coupling is reverted. Same non-ported surface |
| `9795d6023` | feat: per-run theme selection (#7722) | `--use-theme` CLI/TUI plumbing; the `core/agent-session.ts` hunk is `exportToHtml` theme options — HTML export & themes unported (precedent `d0b46764` "theme is TUI"); the `main.ts` hunk is interactive-mode theme override plumbing |
| `c93ea6ccf` | fix: preserve usage in streaming events (#7982) | `modes/json-event.ts` only — coding-agent `--mode json` stdout shaping; Go has no JSON mode (its wire surface is the pi-messages CBOR protocol, untouched here) |
| `581d75a89` | document model catalog refresh | docs only |
| `7d8c11d37` | fix: share concurrent model catalog refreshes | all hunks in `modes/interactive/` (TUI model-selector runtime refresh, incl. the new `modes/interactive/model-catalog-refresh.ts`) |
| `46bb9a2c3` | clarify Windows paths in settings | docs only |

### Watch-item (not a decide) — `defaultTools` tripwire

The default-tools pair (`4d9aa837c` + `541045ae0`) gives pi a capability Go's
`SessionOptions` cannot express: an initial active *built-in* selection
decoupled from the allowlist (extension/custom tools stay enabled and
unconstrained). It is reachable only through the settings file — settled
non-ported surface — so it is `n/a` today, not a boundary change. **Tripwire:
if upstream later surfaces `defaultTools` as a `createAgentSessionOptions`
field (published SDK surface), that commit is `port`** under the standing
formula, and the natural Go home is a new `SessionOptions` field seeding
`resolveTools`' initial built-in set without populating the allowlist.

## Drift at last sync check (2026-08-12) — pin advanced to 2e4d23959

Delta `536eb7179..2e4d23959`, **30** first-parent changes, no merges. **No
release crossed**: no `packages/*/package.json` version bump (pi-ai still
0.84.1) and `models.generated.ts` untouched at both ends, so no catalog regen,
no npm reference-build move, and **no port tag** this cycle. Verdicts: **1 port
→ 1 Go commit + 1 review-fix commit; 1 port-but-DEFERRED (harness); 28 n/a; 0
decide**.

Whole-range reconciliation (the merge-smuggling guard) found in-scope deltas
only in the files of `7915cdac6`, `b75be04d9`, and `b3edf0170`, all accounted
for by the rows below.

### Port worklist (1 → 1 Go commit + review fixes)

| upstream | subject | Go | notes |
|---|---|---|---|
| `7915cdac6` | feat(ai): add strict tool schema conversion | `d3978d3` + `446162e` | **Request-body golden surface** on every strict-capable wire. Two independent behavior changes: (1) `makeStrictJsonSchema` — deep-clone + recursive walk that rejects 16 unsupported keywords (byte-exact error strings, first-present-key precedence), forbids object/array unions and tuples, widens every non-required non-nullable property to `anyOf:[schema,{type:"null"}]`, and sets `required` = ALL property names in serialization order + `additionalProperties:false`; `resolveJSONSchemaStrictSampling` now probes convertibility (prefer → fall back to unconstrained, require → error naming the tool). (2) `normalizeOptionalNulls` — **unconditional** in `ValidateToolArguments` (not experimental-gated): explicit `null` for an optional property whose schema rejects null is deleted instead of failing validation (consumer `agent/loop.go:621`). Go homes: `ai/providers/constrained_sampling.go`, `ai/validation.go`, `ai/schema.go` (new `Clone`), call-sites in `anthropic.go`/`google.go`/`openai.go`/`openai_responses.go` (bedrock/vertex/mistral call-sites have no Go homes; the `create-harness.ts` hunk rides the deferred harness tree), `coding/experimental.go` + `coding/tools.go` (`PI_EXPERIMENTAL=1` sets `{type:"json_schema",strict:"prefer"}` on read/bash/edit/write). Property-order invariant: `required` is derived from the same rule `Schema.MarshalJSON` serializes `properties` with (PropertyOrder, sorted fallback), hoisted to `ai.Schema.OrderedProperties` by review so the two sites cannot drift. Upstream's `defaultStrict` half of `convertResponsesTools` is vacuous in Go (only codex — unported — ever passes one; noted inline). Tests mirror upstream's `constrained-sampling.test.ts` / `validation.test.ts` / `experimental-tool-strict-mode.test.ts`; every lock observed red first. |

### Port-but-DEFERRED — 1 more onto the harness backlog (now 9)

`b75be04d9` "refactor: search (#7797)": extracts session search from
`harness/session/search.ts` into a new standalone `packages/agent/src/search/`
module (`SessionSearch` interface + scanning implementation over
`SessionStorage`), rewires the jsonl repo and the sqlite-node backend onto it,
and swaps the `agent/src/index.ts` export. The new module's types import
`harness/session/types.ts` — same project, same deferral. This is the code half
of the harness.md redesign's "search becomes a standalone service" (see the
design-docs note below).

### Notable n/a (28)

- **Harness design docs (19)**: `43b4e057a`, `9c75cd2f3`, `f13284565`,
  `f98629b39`, `638ac1123`, `bcd056a8c`, `01701fd37`, `b8349db85`, `e1b444ba6`,
  `2862eb4ed`, `a348560b1`, `e33e74450`, `254558539`, `47b021a65`, `8d8c60da7`,
  `b6557f43e`, `51b45bc5b`, `40a3d8556` — all `packages/agent/docs/harness-v3.md`
  editing passes — and `85a206081`, which **deletes `harness-v2.md` (−4,612),
  `agent-harness-spec.md` (−2,524) and the v2 test matrix, renaming
  `harness-v3.md` → `harness.md`** as the sole plan of record. No boundary
  change (harness already in-scope-deferred), but the deferred backlog's spec
  base has been replaced wholesale: one sqlite file per session, search external
  (see `b75be04d9`), parallel storage/runtime build tracks. Another data point
  for the 2026-08-07 standing caution to port against the published npm surface
  rather than head-of-churn.
- **TUI (5)**: `452923b54` + `534bcbffb` (LaTeX argument/control-space parsing),
  `2a95ef70d` (`PI_TUI_ESC_TIMEOUT` scoped to lone ESC; also brushes
  `coding-agent/docs/environment-variables.md` — docs), `2e4d23959` (fullscreen
  overlay wheel/viewport keys), `9c53c47f8` ("fix indent" — tui test file only).
- `b3edf0170` "fix(ai): bound Copilot policy update concurrency" — confined to
  `ai/src/auth/oauth/github-copilot.ts` (+test): `enableAllGitHubCopilotModels`
  inside the device-code login flow now enables models in batches of 4. OAuth
  token acquisition is excluded; checked that Go's `ai/providers/copilot.go`
  carries no policy-update logic, so nothing leaks into ported surface.
- `2a9b4ebc6` "docs: document terminal-specific fullscreen mouse behavior" —
  coding-agent docs only.
- **Meta (2)**: `00eb2d151` (contributor approvals), `f8c71c6a0` (SECURITY.md
  typos).

### Review gates, and one recorded drift class

Independent **pi-parity-review**: **FIX-FIRST**. Its differential-harness run
measured one real, model-visible divergence introduced by the port — a
strict-converted **zero-property object dropped `required: []`** (pi assigns
`required = Object.keys(properties)` unconditionally; Go's marshalling gated the
key on `len > 0`) — on all three strict wires, nested nodes included. Fixed in
`446162e` by presence-based emission (`Required != nil`), red-observed first.
The same fix round corrected a **pre-existing** inverse: marshalling invented
`properties: {}` for object nodes that never carried the key (now emitted only
when the map is non-nil; `ai.Object()` always initializes it, and a sweep found
no production schema built with `Type: "object"` and a nil map, so no other wire
moves). The reviewer's 28-case conversion probe (executed upstream TS at
`7915cdac6` vs Go) and 10-case validation probe vs real TypeBox came back
byte-identical apart from the drift class below; error strings and
unsupported-key precedence all match.

**Recorded drift (accepted, not fixed) — decode-boundary flips.**
`ai.Schema.UnmarshalJSON` silently drops malformed/exotic keywords
(pre-existing lenient-decode convention), so for tool schemas **decoded from
JSON** the strict probe can resolve `strict=true` where pi errors or falls
back: tuple `items` / boolean `items` / boolean `anyOf` variants are dropped
rather than rejected (pi: "tuple schemas are unsupported" / "boolean schemas
are unsupported"), non-array `anyOf`/`required` and `type:["object"]` convert
where pi errors, and a partially-string `required` errors with a different
string ("required contains an unknown property" vs pi's "object required must
be a string array"). Unreachable through pi's own built-in tools (json_schema
sampling is set only on Go-constructed definitions); reachable by a library
caller decoding tool schemas from JSON. Tied to the decode convention — fixing
it means strictifying `UnmarshalJSON`, a separate decision from this port.

Independent **pi-go-review**: **CLEAN** (nits only). Applied (all
behavior-identical, in `446162e`): the `OrderedProperties` hoist above; generic
`clonePtr` + `slices.Clone` replacing four hand-rolled clone helpers; a
reflection-based Clone-aliasing test that fails if a `Schema` field is added
without teaching `Clone` about it. Declined and recorded: making
`normalizeOptionalNulls` a method (cosmetic churn); collapsing the up-to-3×
strict conversion per request (mirrors upstream's exact resolve/convert
decomposition — the reviewer itself recommended keeping it).

**New public Go API (additive):** `ai.Schema.Clone` (structuredClone analogue
the conversion needs) and `ai.Schema.OrderedProperties` (review-driven, guards
the required-matches-properties wire invariant).

Differential harness: 18 → **21 scenarios, 21/21 PASS** — new
`strict-tools-{anthropic,openai,responses}` (full request-body assertions:
required ordering, anyOf-null widening, no-rewrap of already-nullable shapes,
nested object closing, zero-property object, inconvertible-tool fallback with
original parameters), all `backend: "src"` at the new pin; the harness's
`PI_UPSTREAM_SHA` advanced `536eb7179` → `2e4d23959`.

## Drift at last sync check (2026-08-11) — pin advanced to 536eb7179

Delta `31b513e31..536eb7179`, **38** first-parent changes, no merges. **No
release crossed**: no `packages/*/package.json` version bump (pi-ai still
0.84.1) and `models.generated.ts` untouched at both ends, so no catalog regen,
no npm reference-build move, and **no port tag** this cycle. Verdicts: **3 port
→ 3 Go commits; 2 port-but-CATALOG-ONLY (parked for the next regen); 1
port-but-DEFERRED (harness); 32 n/a; 1 decide → RULED (2026-08-11, ported this
cycle)**.

Whole-range reconciliation (the merge-smuggling guard) found exactly 7 in-scope
src files plus `ai/scripts/generate-models.ts` and the session-backends tree,
all accounted for by the rows below.

### Port worklist (3 → 3 Go commits)

| upstream | subject | Go | notes |
|---|---|---|---|
| `3dd4623ee` | Fix prompt formatting for current working directory (#7887) | `7e787c2` | **Byte-golden surface (system prompt).** Upstream added a trailing `\n` to the `Current working directory:` footer in `buildSystemPrompt`'s **customPrompt branch only** (its line 69), leaving the identical line in the default-prompt path (line 159) untouched. The Go port has the same two sites — `coding/systemprompt.go:75` (custom) and `:157` (default) — so the trap was moving both, or the wrong one. Only `:75` moved. Golden `TestCustomSystemPromptAssemblyGolden` was moved FIRST and observed red with `got` missing the newline; `TestSystemPromptShape` (default path) and the `Contains`-based assembly test were unaffected by construction. |
| `b647d1879` | fix(ai): detect DeepSeek base URLs case-insensitively (#7933) | `f499e8a` | **Request-body / compat-detect surface.** Direct follow-on to last cycle's `c185d4123`. Upstream folds case on the `deepseek.com` baseUrl probe and hoists `isDeepSeek` above the `isNonStandard` disjunction so both it and `useMaxTokens` read from one variable; both moves mirrored, with the disjunction's membership and order intact. **The trap: Go's `has()` closure is shared by every provider probe** — lowercasing `has()` itself would have silently made zai/moonshot/chutes/together/nvidia/x.ai detection case-insensitive too, which upstream did NOT do. Only the DeepSeek probe folds case, and a regression test pins the other five as case-sensitive. Red observed on all five fields an uppercase baseUrl was getting wrong: `max_completion_tokens`, `thinkingFormat: openai`, `requiresReasoningContentOnAssistantMessages: false`, and `supportsStore`/`supportsDeveloperRole` true (the last two because `isDeepSeek` now feeds `isNonStandard`). The `generate-models.ts` half has no Go home — catalog-regen only, same as last cycle. |
| `230029078` | feat(ai): AI Gateway transport over the Cloudflare AI binding (#7901) | `1674990` | **New public Go API; latent.** See the 2026-08-11 ruling. `createGatewayBindingFetch` → `providers.NewGatewayBindingDoer`, an `ai.HTTPDoer` translating gateway-prefixed HTTPS requests into `env.AI.gateway(id).run()` universal-endpoint calls. Behavior ported whole: origin+prefix guard, POST-only, provider/endpoint split on the URL-normalized path, query string retained on the endpoint, JSON body probe, lowercased headers with `content-length`/`host`/`cf-aig-authorization` stripped, binding response returned untouched. **Deliberately not ported: pi's fetch-input reconciliation** — most of the TS module's length exists to reconcile a `Request` input against a `RequestInit` override (six body types, four header shapes, fetch-spec `body: null` / `signal: null` rules); an `*http.Request` has exactly one of each, so that code has no subject in Go. Cancellation moves from the AbortSignal options bag to the request context, per the port's standing convention. New exported surface: `AIGatewayBinding`, `AIGatewayBindingGateway`, `AIGatewayUniversalRequest`, `GatewayBindingDoerOptions`, `NewGatewayBindingDoer`, `CloudflareGatewayBindingAuthSentinel`. |

### Port-but-CATALOG-ONLY — 2 parked for the next release regen

Both are `packages/ai/scripts/generate-models.ts`-only. Go reads these as catalog
data (`ai/providers/openai_responses.go` takes `supportsStrictMode` straight from
the model's `compat` blob; `thinkingLevelMap` is catalog data), so neither has a
Go source home and neither can land before upstream regenerates
`models.generated.ts` at its next release. Nothing to do now — recorded so the
next regen is not a surprise:

- `75c7fd662` "fix(ai): declare Cloudflare Responses strict tools (#7934)" — will
  add `supportsStrictMode: true` to all **19** `cloudflare-ai-gateway`
  `openai-responses` catalog models (`compat: null` today).
- `2f8b4b42f` "fix(ai): expose low reasoning effort for native DeepSeek V4 Flash
  (#7807)" — `deepseek/deepseek-v4-flash` `thinkingLevelMap.low` goes `null` →
  `"low"`.

With last cycle's deepseek `maxTokensField` item that makes **3 deltas queued for
the next catalog regen**.

### Port-but-DEFERRED — 1 more onto the harness backlog (now 8)

`a4453b79b` "fix: sqlite time to number": `agent/src/harness/session/search.ts`
plus the sqlite-node backend (`001_initial.sql`, `repo.ts`, `search-backend.ts`,
`storage/{branch-entries,entries,records,sessions}.ts`). A stored-time
representation change in the harness's own SQLite schema. No golden impact on us
— coding-agent v3 JSONL is our format and is untouched — but it is schema churn
to honor when the harness port happens, and another data point for the standing
caution to port against the published npm surface rather than head-of-churn.

### Notable n/a (32)

- **Contributor approvals (10)**: `6a214fab9`, `caf8fa6b5`, `0db3d3879`,
  `c1a4e86c0`, `85b1efa14`, `f858ae3e8`, `52771a07b`, `c2b06f093`, `c0468b922`,
  `536eb7179` — `.github/APPROVED_CONTRIBUTORS` only.
- **Harness design docs (13)**: `3059b8131` + `3709bef75` (new 2,541-line
  `agent-harness-spec.md`, then a terminology pass), `24c3b5e4c` +
  `2f9e9298a` (storage-redesign proposal, added then deleted along with
  `harness-v2-state-machine.md` — −1,599), `7a6a1c2db`/`87142a8d5`/`dea1b248f`/
  `28657a2ff`/`ec317e4ed`/`cd6852a12`/`8431bfbea`/`cf5f35216`/`24047f5df`
  (`harness-v3.md` written in parts 0–9 plus audit findings). No boundary change
  — the harness is already in-scope-deferred — but harness-v3 supersedes v2 in
  one cycle, which is exactly the churn the 2026-08-07 standing caution names.
- **TUI (4)**: `00121ed99` (fullscreen transcript search), `1279952de`
  (single-line scrolling actions), `06ed87167` (split Alt+Enter), `4a879dd75`
  (idle repaint on focus loss).
- `98145a6c0` "fix(ai): sanitize empty Bedrock tool argument keys (#7882)" —
  confined to `bedrock-converse-stream.ts` + its test; `sanitizeBedrockDocument`
  is a file-local helper with no shared-code hunk, and the port ships no bedrock
  provider. Checked explicitly against the 2026-07-21 multi-site lesson: clean.
- `b987ead35` "feat(agent): expose `expandPromptTemplates` in `sendUserMessage`
  (#7857)" — `core/agent-session.ts` + `core/extensions/types.ts` only, both
  unported (extensions excluded by boundary; agent-session runtime on the
  not-ported list). Verified the ported `coding/remotesession.go` exposes no
  `SendUserMessage`, so there is no Go home for the new option.
- `9cb7f493e` "docs(coding-agent): document AI_AGENT process marker" — checked
  for a doc-ahead-of-code miss and there is none: `AI_AGENT=pi` is set only in
  `cli.ts` and `rpc-entry.ts`, and the port sets neither it nor
  `PI_CODING_AGENT`, consistent with the CLI/packaging exclusion.
- `afae6a1a9` "fix(ai): update stale Gemini test model" — `packages/ai/test` only,
  `skipIf`-gated live-API tests; `gemini-2.0-flash` appears in the Go tree only
  as a catalog entry, so there is no test to follow.
- `e3798ca91` "fix(coding-agent): inherit subagent session config (#7897)" —
  `examples/extensions/subagent/` only.

### Review gates — parity FIX-FIRST, go-review FIX-FIRST (all applied in `663e8f5`)

Both gates cleared changes 1 and 2 and both landed on the **same defect** in
change 3, independently and from different directions: `url.Parse` +
`path.Clean` is not a substitute for WHATWG `new URL()`. Parity quantified it by
running verbatim upstream TS at `536eb7179` against real Go `Do()` over a shared
case table — **7 divergences**, two of which changed the dispatch decision:

1. `<base>//anthropic/v1/messages` — `path.Clean` collapses the empty segment, so
   the port **dispatched** `anthropic/v1/messages` where pi rejects with
   *missing provider/endpoint path*. (MED-HIGH: an empty segment silently
   rewrote the endpoint sent to the binding — the exact opposite of the guard's
   stated purpose.)
2. `<base>/anthropic/..` — WHATWG leaves a trailing slash, `path.Clean` drops it,
   so the prefix test failed and the port raised *outside the prefix* where pi
   raises *missing provider/endpoint path*. Wrong error branch.
3. `<base>/anthropic/v1/..` and `<base>/anthropic/.` — pi **dispatches**
   `provider=anthropic, endpoint=""`; the port rejected.
4. Uppercase host (`GATEWAY.AI.CLOUDFLARE.COM`) — pi dispatches (WHATWG
   lowercases the host); the port rejected, because `url.Parse` lowercases the
   scheme but not the host.
5. Explicit `:443` — pi dispatches (WHATWG drops a default port); the port
   rejected.
6. `<base>/anthropic%2Fv1/messages` — the port split on `url.URL.Path`, which is
   percent-**decoded**, so `%2F` acted as a separator; pi splits on
   `URL.pathname`, which is encoded. `EscapedPath()` is both correct and the
   faithful equivalent.
7. An empty body built the idiomatic way (`strings.NewReader("")` ⇒
   `http.NoBody`) reported *missing body*; pi reaches `JSON.parse("")` and throws
   *non-JSON body*. The port's own comment already described the correct
   behavior — and the test could not catch it because the helper mapped `""` to a
   **nil** reader, conflating absent with empty.

Go-review reached the same URL layer plus five findings of its own: the 35-line
file narrative sat above `package providers`, making it a **second package
comment** that led `go doc` output ahead of the real one (`pi_messages.go` puts
that narrative below the package clause for exactly this reason); header
"collapse" was **nondeterministic** (measured 259/1741 over 2000 runs — map
iteration picked the winner, and the test's four-way tolerance was conceding it
rather than pinning behavior); `req.Body` was **never closed on any path**,
breaking the `*http.Client` contract this type substitutes for; `Query any`
**reordered keys and lost integer precision**; and the two failure classes had no
sentinel, so the only way to branch was string matching.

**Three go-review findings were declined, on parity grounds — recorded because
the two lenses genuinely conflict here.** (a) Errors from `Do` keep the
*constructor* name as their prefix; Go convention says name the failing
operation, but pi throws `createGatewayBindingFetch: …` from inside the returned
fetch, so upstream wins. (b) `strings.ToUpper(req.Method)` accepts `"post"` where
`http.Client` would send it verbatim and the gateway would reject it — pi
uppercases identically. (c) `AIGatewayBindingGateway` is a mouthful and
`CloudflareGatewayBindingAuthSentinel` is the only `Cloudflare`-prefixed new
name; both mirror upstream's `AiGatewayBindingGateway` and
`CLOUDFLARE_GATEWAY_BINDING_AUTH_SENTINEL` exactly, and mirroring upstream public
names is the convention.

### New deliberate divergence — the binding entry carries raw JSON, not a decoded value

`AIGatewayUniversalRequest.Query` is `json.RawMessage`, where pi's
`AiGatewayUniversalRequestLike.query` is `unknown` holding whatever `JSON.parse`
returned. pi can afford the decode because JS objects preserve insertion order,
so re-serializing inside `run()` reproduces the provider's key order; a Go
`map[string]any` cannot — `encoding/json` would emit keys alphabetically, and
every number would round-trip through `float64`, corrupting integer ids past
2^53. The port therefore validates with `json.Valid` (which keeps the
`non-JSON body` gate byte-identical: `json.Valid("")` is false, `json.Valid("null")`
is true) and forwards the bytes untouched. Test-locked by
`TestGatewayBindingPassesBodyThroughByteForByte` with a non-alphabetical key order
and a 2^53+1 id.

### Differential harness — 17 → 18 scenarios, 18/18 PASS

New permanent sibling `deepseek-fix-upper` pins the **uppercase** baseUrl route
into `isDeepSeek` body-level, mirroring upstream's `customUppercaseModel` case.
Three independent observables ride on it: `max_tokens` (not
`max_completion_tokens`) = `useMaxTokens`; the `thinking` key =
`thinkingFormat: "deepseek"`; and the **absence** of `store:false` =
`isNonStandard`, which is precisely what the hoist changed. Parity caught the
scenario over-claiming a fourth — `supportsDeveloperRole` is unobservable without
a system message — so the scenario gained `"systemPrompt": "Reason step by step."`
to pin `system` vs `developer` for real. `config.env` `PI_UPSTREAM_SHA` advanced
`31b513e31` → `536eb7179` with the pin; parity independently verified the
reference side is genuinely post-fix rather than a stale cache.

### Triage lesson — the upstream clone's WORKING TREE was stale; always read via sha (FIXED)

`~/.cache/pi-upstream`'s checked-out `main` was at `130ae577a` (2026-06-07) — an
ancestor of `origin/main` by roughly two months, because the sync only ever
fetched `origin/main` and never moved the branch. A `grep` of the clone's working
tree for this cycle's cwd-footer change therefore showed **no** trailing `\n` and
read as if upstream had reverted it. It had not: `git show
536eb7179:…/system-prompt.ts` line 69 is
`` prompt += `\nCurrent working directory: ${promptCwd}\n` ``, verified
byte-level. The parity reviewer hit exactly this and nearly filed a phantom
finding against a correct port.

**Fixed in this cycle, at both layers.** (1) `/pi-sync` §0, `/pi-triage` Setup
and the scheduled report job now fast-forward the checkout after fetching
(`git -C "$dir" checkout -B main origin/main`). That is safe unconditionally: the
clone is a read-only mirror with nothing committed to it, and the differential
harness extracts from it by `git archive <sha>` (`run.sh:94`), never from the
checkout — verified by re-running the harness after the move, 18/18 PASS.
(2) All three skills plus `/pi-parity-review` now carry the standing rule that
**no upstream read may come from the working tree** — `git show <sha>:<path>`,
`git diff <sha>^1..<sha>`, `git grep <pattern> <sha> -- <path>` — because a
fast-forwarded tree sits at `origin/main`, which mid-cycle is *ahead* of the pin
and so is still not the thing under triage or review. `/pi-sync` is told to pass
that rule to every subagent it spawns.

## Drift at last sync check (2026-08-10) — pin advanced to 31b513e31

Delta `368e013de..31b513e31`, **9** first-parent changes, no merges. **No
release crossed**: no `packages/*/package.json` version bump and
`models.generated.ts` untouched at both ends, so no catalog regen, no npm
reference-build move, and **no port tag** this cycle. Verdicts: **1 port → 1 Go
commit + 1 review-fix; 1 port-but-DEFERRED (harness); 7 n/a; 0 decide**.

### Port worklist (1 → 1 Go commit + 1 review-fix)

| upstream | subject | Go | notes |
|---|---|---|---|
| `c185d4123` | send max_tokens to DeepSeek APIs | `b71388d` | **Request-body surface, live fix.** `isDeepSeek` hoisted above `useMaxTokens` and added at pi's own position (after `chutes.ai`), so built-in AND custom DeepSeek API models send `max_tokens` instead of `max_completion_tokens`. Live, not latent: the 0.84.1 catalog carries no `maxTokensField` for deepseek (compat block byte-identical to the shipped build, parity-verified), so runtime detect governs built-ins today. The `generate-models.ts` half has no Go home — upstream's per-model data JSONs are build-time generated, not in-tree — and surfaces as a `maxTokensField: "max_tokens"` catalog diff at the NEXT release regen (same value the detect now produces, so no behavior flip; expected, not a surprise). Tests mirror pi's native+custom coverage: a compat table over all four routes (provider, baseUrl, both catalog v4 models — upstream's catalog-level `model.compat` assertion is unreachable against the pinned 0.84.1 catalog and was adapted to the resolved compat, which is what governs the wire) plus a body pin (`max_tokens: 123`, never `max_completion_tokens`). Both observed red with the fix reverted. |

### Review gates — parity FAITHFUL, go-review SHIP WITH NITS (applied in `6b933b2`)

Parity confirmed disjunction membership AND order, the detection strings, the
case-sensitivity match (`strings.Contains` ≡ `.includes` on un-lowercased
inputs), the single shared `params[compat.MaxTokensField]` application site, and
the `*string`-gated catalog override layering — including that upstream's
generator delta rule bakes the same value at the next regen. It also
byte-matched the REMOVED Go disjunction line against the authentic 0.84.1 dist,
proving the pre-fix Go was faithful to what ships. Go-review's one LOW: the
compat table asserted via a flat `t.Fatalf` loop, masking later rows and
breaking style with the zai sibling — now `t.Run` subtests. It separately
verified that `isNonStandard` keeping `has("deepseek.com")` inline rather than
reusing `isDeepSeek` is REQUIRED, not an oversight: `isNonStandard` deliberately
lacks the `provider == "deepseek"` route.

### Differential harness — 16 → 17 scenarios, 17/17 PASS

The dist-backend `deepseek` scenario went red on exactly this change's surface —
pi-0.84.1 sends `max_completion_tokens` where the port now sends `max_tokens` —
the *expected* ahead-of-release divergence. Per the harness README's own rule it
flipped to `backend: "src"` (the published build cannot referee a surface it
predates; flip back to dist when a release ships the fix; a known-divergences
entry would have been wrong — that ledger is only for go-wrong debt). Parity
proved the red was not a port bug by capturing pi from TS source at the fix sha:
the clone passed byte-identically, and a second, permanent scenario
`deepseek-fix-custom` pins the `deepseek.com` baseUrl-substring route body-level
(`max_tokens: 123`, never `max_completion_tokens`) — the route the Go unit tests
cannot body-test because `captureOpenAIBody` rewrites `BaseURL` to the stub
server. `config.env` `PI_UPSTREAM_SHA` advanced `368e013de` → `31b513e31` with
the pin.

### Follow-up recorded, NOT fixed this cycle

- **`client/disposal_test.go:215` is load-flaky under `-race`** ("Close returned
  while a child handle was still attached" — twice in one full-suite run, then
  green standalone ×10 and green on two full re-runs). Unrelated to this delta:
  the port diff is a pure boolean-disjunction reorder in `ai/providers`. Spun
  off to its own session rather than widened into the sync.

### Notable n/a (7 — all docs)

- **Harness design docs (5)**: `025957c25` + `4181f66e6` (`harness-v2.md`
  reconcile/tighten, ±2,930 lines), `45fac5bd5` + `157aa19c8` + `936aff009` (new
  `harness-v2-state-machine.md`, ~1,205 lines — an explicit-state harness
  redesign). No boundary change — the harness is already in-scope-deferred — but
  it signals the session internals are heading for another rewrite; the standing
  caution to port the harness against the published npm surface, not
  head-of-churn, gets stronger.
- **Compaction docs (2)**: `476102170`, `31b513e31` — one phrasing line each.

### Port-but-DEFERRED — 1 more onto the harness backlog (now 7)

`7bdb16c28` "support clearing session names": `setName(string)` →
`setName(string \| undefined)` across `session/types.ts` / `session.ts` /
`state.ts` / `memory.ts` / `jsonl/codec.ts` + `storage.ts` / the conformance
suite AND the sqlite-node backend; the jsonl codec now accepts an absent `name`
on `fact:name` records where it previously required a string — a session-format
wire change to honor when the harness port happens.

## Drift at last sync check (2026-08-08) — pin advanced to 368e013de

Delta `e0900a6ea..368e013de`, **24** first-parent changes, no merges. **No
release crossed**: `packages/ai/package.json` stayed at **0.84.1** and
`models.generated.ts` was untouched at both ends, so no catalog regen, no npm
reference-build move, and **no port tag** this cycle. Verdicts: **4 port → 4 Go
commits + 1 review-fix; 6 port-but-DEFERRED (harness); 14 n/a; 0 decide**.

### Port worklist (4 → 4 Go commits + 1 review-fix)

| upstream | subject | Go | notes |
|---|---|---|---|
| `fe10558eb` | retry upstream request buffer failures | `6f9c7db` | One alternative appended to `retryableProviderErrorPattern` at pi's own list position, byte-identical, test message byte-identical to pi's vitest constant. Verified no existing pattern already matched it (and that the non-retryable limit pattern does not). |
| `c3e7bc60a` | preserve Codex end_turn for debugging | `e047ab1` | **Types half only.** `AssistantMessage.EndTurn *bool` — pi assigns only on `typeof === "boolean"`, so absent/true/false are three observable states and a plain `bool` would collapse two of them. The three `openai-codex-responses.ts` hunks have no Go home (provider deliberately unported), so nothing in this port ever writes the field; it is carried because session files and the server wire are shared with pi proper, the `ToolResultMessage.Usage` precedent. |
| `02bd2d1c6` | preserve Responses tool-call namespaces | `2b661cb` | **Two golden surfaces.** Request bodies (namespace echoed on `function_call` / `custom_tool_call` replay) and the session wire — `ToolCall` has a hand-written marshaller with an explicit field list, so `namespace` had to be added in all three places or it would drop silently; the round-trip test exists for exactly that. Parsed at both `output_item.added` and `output_item.done`, each guarded so an absent value cannot clobber the other. Replay is gated on `isSameModel \|\| deferred`, and pi's `isDifferentModel` gained an explicit `isSameModel` sibling (pure boolean re-association — proven unchanged on the wire). `agent/src/proxy.ts` half n/a: no Go analog. |
| `e47b8e37a` | use additional_tools for deferred tools | `6d67d5d` | **Request-body surface, currently dormant.** The deferral gate becomes a three-way mode (additional-tools > tool-search > none); capable models get a message-anchored `{type:"additional_tools", role:"developer"}` item at the introducing tool result, with **no** synthetic search call and **no** `defer_loading` on the tools. `supportsAdditionalTools` is set by pi's catalog generator and upstream did **not** regenerate — zero occurrences in `models.generated.ts` at both ends and in `ai/models_catalog.json` — so the branch is dark on BOTH sides until a release brings one. Codex + TypeBox `model-config.ts` hunks n/a (no codex provider; Go's `Compat` is a `json.RawMessage` with no validator that could reject the key). |

### Review gates — parity FAITHFUL, go-review SHIP WITH NITS (all applied in `13bfda2`)

Both lenses landed on the same weak spot from different directions, which is the
argument for running both: **the entire streaming half of `02bd2d1c6` was
unpinned at the port commit.** Deleting either `output_item.done` capture, or
the `output_item.added` capture, left the whole suite green — the single test set
the namespace on *both* items so neither was load-bearing, and the
`custom_tool_call` path had no namespace test at all. Now a six-case table
(function/custom x added-only/done-only/both) pins each site independently.

Go-review also caught the mode being re-derived per arm inside a per-message
loop (now hoisted and switched), and a **test helper that had drifted from the
production path it mirrors** — `mustResponsesInput` still gated deferral on
`SupportsToolSearch`, so a future `additional_tools` test routed through it would
have silently asserted the wrong shape. Parity independently built three
openai-responses scenarios and found the standing 13-scenario harness **vacuous
for this delta** — none of them touched openai-responses at all.

### Differential harness — 13 → 16 scenarios, 16/16 PASS

`~/.cache/pi-diff` gained its first openai-responses coverage, kept permanently:
`responses-deferred-additional-tools`, `responses-deferred-tool-search`,
`responses-deferred-off`. One transcript each, covering the full matrix in
place — additional-tools vs tool-search vs off, and same-model / different-model
/ deferred-tool namespace replay together. All three byte-identical to real pi
TS after canonicalization. `config.env` `PI_UPSTREAM_SHA` advanced
`e0900a6ea` → `368e013de` with the pin.

### New deliberate divergence — empty-string `namespace` is dropped

`ToolCall.Namespace` is a plain `string`, so an **empty** namespace does not
replay; pi guards on `namespace !== undefined` and would emit `"namespace": ""`.
Parity demonstrated it rather than argued it: a scenario carrying `namespace: ""`
produces `missing-key @ $.input[N].namespace  pi: ""  go: <absent>`. Same class
in three places — the wire, the stream (an explicit `""` on the done item can
neither be stored nor clear an earlier value), and session persistence via
`omitempty`. **Accepted**, matching the existing `ThoughtSignature` /
`ErrorMessage` precedents and requiring a provider or stored session to carry an
empty namespace, which OpenAI does not do. Now pinned by
`TestResponsesEmptyNamespaceDroppedDivergence` so it cannot drift silently into
something unintended.

### Follow-ups recorded, NOT fixed this cycle

- **`pi_messages.go` `toolcall_end` replaces where pi merges** (parity LOW,
  **pre-existing**, not introduced by this delta): `ai/providers/pi_messages.go`
  does `c.partial.Content[idx] = *ev.ToolCall` where `pi-messages.ts:252` does
  `Object.assign(existing, event.toolCall)`, so a field present on the partial
  but absent from the event survives in pi and is lost here. `namespace` itself
  is safe (it rides on the event). Flagged because `02bd2d1c6`'s sibling
  `proxy.ts` fix was precisely "metadata must survive toolcall_end".
- **Shared `blockBuilder` lives in `anthropic.go`** (go-review LOW): used by four
  providers, and now carries two fields anthropic never touches (`grammar`,
  `toolNamespace`). Not a regression — the precedent predates this cycle — but
  the cheap fix is a neutral `block_builder.go`. Deliberately not taken here to
  keep the port diff scoped.

### Notable n/a (14)

- **Harness docs (6)**: `709aa0319`, `958c13f25`, `4bf1bba20`, `541ed488d`,
  `5cfa30cc2`, `6809fabbb` — all `packages/agent/docs/harness-v2.md` only.
- **Other docs/meta (3)**: `4fbdc63ca` (AGENTS.md), `368e013de` (compaction.md
  ascii alignment), `f8746767b` (npm 12 `pack --json` in the release script).
- **Excluded providers (1)**: `9dd90a497` "replace Mistral SDK with native
  transport" — a 785-line rewrite of `mistral-conversations.ts`. Mistral is on
  the deliberate non-port list and there is no `ai/providers/mistral*.go`.
- **TUI / non-port list (2)**: `18dee5f0a` (alt-screen painter), `ac4ac9eaf`
  ("configure fullscreen exit output" — its only core hunk is
  `settings-manager.ts`, on the deliberate non-port list, and the setting is
  inert outside fullscreen TUI).
- **Tests/lockfiles (2)**: `f0feecd75` (OpenCode completions fixture string),
  `5ac913365` (nanoid 3.3.17 in the root lockfile).

### Port-but-DEFERRED — 6 more onto the harness backlog

Under the 2026-08-07 ruling the harness tree is in scope but deferred, so these
are recorded as owed, not as `n/a`: `14ad9801b`, `d1a305613`, `1dd235405`
(harness event subscription / direct listeners / buffered watches, all
`agent/src/harness/events.ts`, +102 lines), `7aca0d7b3` (explicit JSONL decode
errors), `4a0e2f115` (JSONL crash + corruption handling), `e7fb8eb2a` (no CTEs
in sqlite, delete indexes — `packages/session-backends/sqlite-node`, incl. a
migration). They extend the ~12.4k-line backlog; the pin advanced without them.

## Drift at last sync check (2026-08-07) — pin advanced to e0900a6ea

Delta `9859eaa26..e0900a6ea`, **51** first-parent changes. **TWO releases
crossed** — tags `v0.84.0` (`a5f43bf8a`) and `v0.84.1` (`53fa77ccd`), every
`packages/*/package.json` **0.83.0 → 0.84.1**; catalog regenerated, npm
reference build advanced, tag `v0.84.17` cut, tweet drafted. Verdicts:
**9 port units (10 upstream shas) → 7 Go port commits + 1 review-fix commit;
41 n/a; 1 decide** (the harness boundary, escalated and RESOLVED **(c) reopen**
by owner ruling the same day — see Rulings).

**Harness-ruling tripwires (2026-08-05): BOTH FIRED — the ruling is retired.**
(1) `coding-agent/src/server/create-harness.ts` imports `AgentHarness` &co from
`pi-agent-core` and wires them to ported surface. (2) The blanket "No work
package may modify coding-agent source…" clause was deleted from
`harness-v2.md` by `5cd46ee11`; only the Track-O scoping survives. The
2026-08-06 "watch whether the carve-out widens" note resolved: it was removed,
not widened. The harness tree is now **in scope but deferred** — see the
2026-08-07 ruling for the ~12.4k-line backlog and what the pin now means.

### Port worklist (9 units → 7 Go commits + 1 review-fix)

| upstream | subject | Go | notes |
|---|---|---|---|
| `a5f43bf8a` + `53fa77ccd` | Release v0.84.0 / v0.84.1 | `2dcc126` | Catalog regen 0.83.0→0.84.1, **511,913 B**, 37→39 providers, 1153→1220 models (+91/−24). Derived by **executing** `dist/models.generated.js` (never `cmp`-ing the aggregator), endpoint-pinned both ends, integrity matched to the registry. Schema drift = exactly two new compat keys, **both already consumed** by the earlier Baseten port — the 2026-08-04 prediction landing. Clears the `2f7f75a20` / `720f0e8ee` / `71f6c25c3` regen signals. |
| `b9497c8c1` | Fireworks GLM prompt caching | (in `2dcc126`) | Generator-only upstream (`ai/scripts/generate-models.ts`), so it is realized **by** the regen rather than by Go code: `accounts/fireworks/models/kimi-k3` and `.../routers/kimi-k3-fast` move `anthropic-messages` → `openai-completions`, baseUrl gains `/v1`, `thinkingFormat:"openai"`, `deferredToolsMode:"kimi"`. The generator's `glm-5p2` branch matches no current models.dev model and is inert at this regen. |
| `1532c9994` | reject reset during active runs | `e0f5eb4` | `Agent.Reset()` gains an error return and the `a.active != nil` guard, error string byte-identical to pi. **Public API break** (`Agent.Reset`, `coding.Session.Reset`), sanctioned by the 2026-07-17 clause. Test mirrors pi's real point — a refused reset must leave transcript and `IsStreaming` untouched. Note Go sets `IsStreaming` in `executeClaimedRun` **before** the executor commits the user message, so the test synchronises on a signal from inside the stream fn (pi's `streamStarted`), not on `IsStreaming`. |
| `1eb988cfe` | blocked tool calls can terminate | `8c4387c` | `BeforeToolCallResult.Terminate` as a plain `bool`: pi guards on `terminate === true`, so absent and false are indistinguishable and no tri-state is observable (unlike `AfterToolCallResult.Terminate`, which is `*bool` for pi's `!== undefined`). The AND rule in `shouldTerminateBatch` is unchanged and does the work. The mixed-batch test is the load-bearing one. `extensions/types.ts` half n/a. |
| `9ab91fb93` | expose tool prompt contributions | — | **n/a (no-code), but not ignorable**: it touches seven ported `core/tools/*.ts` files while being a byte-identical refactor — the same literals hoisted into exported consts so the harness factory can reuse them. Independently corroborated by the system-prompt golden being unchanged by it. Listed here because it is the coupling that fired the harness tripwire. |
| `4e64de695` | soften PI environment guideline | `7b28c67` | "Inspect PI_*…" → "You can inspect PI_*…". **System-prompt byte-golden moved**; all three Go sites move together. String byte-compared against the shipped `dist/core/tools/bash.js`. Golden verified non-vacuous by reverting only `tools.go`. |
| `c03d78bdc` | Qwen Token Plan Individual provider | `e3dedb6` | Catalog data arrived with the regen; this wires `ai/envkeys.go` (deliberately **reusing** `QWEN_TOKEN_PLAN_API_KEY`, not minting a new var) and `coding/resolve.go` (`qwen3.8-max`). Whole-table reconciliation against pi's `model-resolver.ts`: **40/40**, no missing keys, no extras, no value mismatches — closing the 2026-07-30 MEDIUM. |
| `35f5c265d` | telemetry reference adapter | `7654d96` | Runtime half only per the 2026-08-06 ruling: `InMemoryContext` + `RecordedSpan`/`RecordedEvent`. Excluded: `createTypedSpanStarter`/`TypedSpanStarter` and their duplicate-span-name type machinery (compile-time TS inference; consumers are the harness and its docs generator). `noop.ts` is a file move with no behaviour delta. Two documented divergences: a mutex (pi is single-threaded) never held across a callback, and panic-settles-then-repanics as Go's analog of a synchronous throw. |
| `6189e53b3` | session summaries → durable metadata | `0827825` | **Breaking wire change and the largest port of the cycle.** `SessionSummary` → `SessionMetadata` (`{id, createdAt, updatedAt?, parentSessionId?, sessionName?, cwd?}`); all live fields dropped, `name`→`sessionName`, `cwd`/`updatedAt` now optional. Consequences ported, not just types: listing takes no connection, one snapshot is built and broadcast to all peers, `ClientState` stops rebuilding attachments (it cannot — the snapshot no longer speaks to them), and `overlayLiveMetadata` reproduces pi's spread where the live side wins **including when absent** while `parentSessionId` survives from storage. **Wire golden re-derived and proved non-circular**; exactly three vectors moved. |

### Review gates — parity FAITHFUL, go-review SHIP WITH NITS (all findings applied in `50fa403`)

The two lenses caught disjoint classes, which is the case for running both. Parity
found a **coverage** MEDIUM (`SessionSnapshot.Metadata()` unpinned — five
mutations survived the whole suite, root-caused to a self-consistent test that
fed the projection into the message it encoded) and four LOWs. Go-review found
two **real bugs** parity structurally could not see, because neither is a
faithfulness question: the exported `InMemoryContext` zero value silently
misrecorded id-0 spans with lost parent links and inverted settle order, and the
settled-parent check released the lock before appending so a concurrently
settling parent could still take a child. It also flagged a dead `state`
parameter that misstated the point of the change, and explicitly endorsed six
porter judgment calls rather than staying silent.

Recorded non-finding: `listMetadata` still routes through `normalizedSnapshot`,
fetching each live session's full snapshot **including transcript** to project
five scalars `Metadata()` immediately discards. Newly wasteful as of this
change; left because it is what pi does.

## Drift at last sync check (2026-08-06) — pin advanced to 9859eaa26

Delta `651d5d6a5..9859eaa26`, **63** first-parent changes. **No release
crossed** — no tag, all `packages/*/package.json` still **0.83.0**; no regen,
no tag, no tweet. Verdicts: **5 port units (6 upstream shas) → 5 Go port
commits + 1 review-fix commit; 57 n/a; 2 decide** (the telemetry pair,
escalated in the morning report and RESOLVED **port** by owner ruling the same
day — see Rulings). All ports unreleased upstream; parity from TS at each sha.

**Harness-ruling tripwires (2026-08-05) both checked, both CLEAR**: (1) zero
harness symbols (`AgentHarness|harness/session|LaneId`) in any
`coding-agent/src` file importing `pi-agent-core`; (2) "Coding-agent
migration" still a non-goal and `packages/coding-agent/**`-unchanged still an
O4 acceptance criterion. First softening observed: the non-goal now admits
"only telemetry dependency build ordering and generated locks may change" and
§21 adds "except I0's telemetry build-order integration". Lockfiles/build
order only, no behavior — does not fire the tripwire; watch whether the
carve-out widens.

### Port worklist (5 units → 5 Go commits + 1 review-fix)

| upstream | subject | Go | notes |
|---|---|---|---|
| `8ecf8a988` | AGENTS.override.md | `f03b9fa` | `contextFileCandidates` gains the override first, byte-order-identical to upstream; the worktree-shadow check needed no code change (keys on the selected basename). The `read.ts` `COMPACT_RESOURCE_FILE_NAMES` half is **n/a — render-layer** (see triage lesson). If a compact-read classification is ever ported, its name list must include AGENTS.override.md. |
| `d4eaf052b` | path globs on Windows | `3cee525` | **Test-only: Go behavior ALREADY correct.** Upstream rewrites win32 patterns `/`→`[/\\]` for fd's native-separator matching; Go's `matchFdGlob` already ToSlash-normalizes both candidates — accept-set-equivalent because Windows path components cannot contain `/` or `\`. Locked by a darwin-runnable slash-image table plus a `GOOS==windows`-gated native-separator block (`GOOS=windows go vet` type-checks it; it executes on any Windows runner). Unverifiable residue recorded: whether fd-on-Windows post-fix accepts a superset (`*` crossing `\`); the port implements the fix's per-component intent. |
| `04d6447f7` + `6b461b75b` | telemetry contracts + package extraction | `9ff6f21` | Net-result port under the 2026-08-06 ruling: new top-level `telemetry` package (runtime surface; `Context`/`Span`/`NoopContext` package-qualified names, error-returning callback replaces the TS generic promise threading; noop = one shared fieldless inert span) + latent `ai.ProviderRequestOptions.TelemetryContext`. Propagation is structural (every hop copies `ProviderRequestOptions` whole) and locked by an 8-path dispatch test mirroring upstream's telemetry-options test (images half excluded — unported). |
| `d07889da0` | thinking_token_budget on openai-completions | `2392e66` | New compat flag `SupportsThinkingTokenBudget` (detect-default false; no catalog model sets it yet — the wire is unchanged until a release regen or explicit model compat opts in). Budget block position/gate/clamps value-identical to upstream; `MIN_ANSWER_TOKENS` hoisted as shared `minAnswerTokens` next to `adjustMaxTokensForThinking`. Parity gate ran 10 temporary differential scenarios against upstream TS executed at the sha — all PASS with full-body identity — then restored the stock harness (11/11). |
| `b0bd0ff9d` | isSubscription on OAuthAuth | `23a3b29` | ai half only: `OAuthAuth.IsSubscription` + `LazyOAuth` reshaped to `LazyOAuthOptions{Name, IsSubscription, LoginLabel, Load}` (sanctioned API break mirroring upstream's input object; zero non-test call sites existed). **Pre-existing miss closed**: upstream `lazyOAuth` has passed `loginLabel` through since a01baaae; Go's `LazyOAuth` structurally dropped it until now. Provider descriptors and every coding-agent hunk (isUsingSubscription/footer/provider-composer) n/a — no Go counterpart. |

### Review gates — parity FAITHFUL ×5, go-review SHIP (1 MED fixed)

- go-review MED (fixed, `5651320`): `adjustMaxTokensForThinking` clamped only
  `xhigh` to high; pi's `clampReasoning` (simple-options.ts) collapses `max`
  too. An Anthropic-path caller passing Reasoning `max` read a missing budget
  row (0) and fell to the 1024 floor where pi sends high's 16384 — confirmed
  by payload capture. Pre-existing (predates this delta) but surfaced by the
  `2392e66` refactor of exactly this function. Fixed by extracting shared
  `clampReasoning` used by both the Anthropic helper and the
  openai-completions budget block; red observed (1024 vs 16384) before the
  fix. The porter had spawned a follow-up task chip for this gap — now stale.
- LOW fixed in this ledger commit: the port's own README still described
  context files as AGENTS.md/CLAUDE.md only.
- LOWs recorded, deliberately not acted on: (a) no per-adapter
  TelemetryContext conversion lock — upstream tests only `buildBaseOptions`,
  Go conversions copy `StreamOptions` whole, and the 8-path dispatch test
  guards the frame; revisit if an adapter conversion is ever rewritten
  field-by-field (the `89a7604` bug class); (b) pre-existing: pi prints a
  chalk.yellow stderr warning for an exists-but-unreadable context-file
  candidate, Go skips silently (selection outcome identical); (c) degenerate
  `/`-inside-character-class glob patterns: upstream's win32 rewrite corrupts
  them into never-matching classes, Go rejects via `ErrBadPattern` — both
  reject on Windows; the off-Windows gap is pre-existing and outside
  d4eaf052b's blast radius.

### Triage lesson — a core/tools/ path can still be render-layer

`8ecf8a988` edits `core/tools/read.ts` — ported surface by path — but the
touched constant (`COMPACT_RESOURCE_FILE_NAMES`) feeds `formatCompactReadCall`,
the TUI's compact-read display classification. The 2026-08-04 utils rule
("judged by the consumer, not the path") applies inside `core/tools/` too:
trace the hunk's consumer before calling `port`.

### Differential harness housekeeping

`~/.cache/pi-diff/config.env` `PI_UPSTREAM_SHA` advanced `651d5d6a5` →
`9859eaa26` (src-backend scenarios now capture post-delta TS), and a permanent
`thinking-token-budget` scenario landed (vLLM zai-style model,
`supportsThinkingTokenBudget: true`, reasoning high, no caller maxTokens →
`thinking_token_budget: 15360`). Full suite green after: 12 PASS, 0 KNOWN,
0 FAIL.

### Notable n/a (57)

- **Harness v2 / session-backends src+tests (17)** — JSONL backend split and
  two rewrites (`e77e2b751` `162b1883b` `ac27947f7` `a79b22622` `b3ec7c006`
  `48b8b547c` `17f720489` `6282221b6` `c38319ea1`), reducer (`2bb7ba496`
  `a5953d2e1`), scaffold/registry (`14c89ad99` `ed303b9bd`), sqlite-node
  (`1022a9e82` `2f550d827`), codec/storage coverage (`c7fc77940`
  `acb2fd984`). All under the 2026-08-05 harness ruling; triage must not
  re-escalate.
- **Docs (17)** — harness-v2.md reservations/completions (incl. `459eb5eba`
  telemetry design finalize, `2c819a41f` align, `22db569a7` test matrix,
  `9859eaa26` the new pin itself), `db48124e9` changelog audit.
- **TUI (6)** — `05e89b418` 1,225-line LaTeX renderer, `16ad96ae8`
  prompt-history keybindings, `29d9f087c` input throttling, `b780d20aa` +
  `229afb825` OSC 8, `4254e0d93` Windows native rebuild sources.
- **.github/meta (6)** — five contributor approvals + `7e45ddc42`
  lgtm-comma workflow fix.
- **Host-only coding-agent (7)** — `2c79ce453` + `4d68d9355`
  auth-storage/models-store lock convoys and retry delays (no Go host
  credential-file store — `ai/credential_store.go` is an in-process queue over
  a caller-supplied store), `ce5d98cc0` test-only, `9524d3a58`
  `normalizeWindowsShellPath` (every consumer unported — judged by consumer),
  `3852cb2b8` compaction queue idle-state (manual-compact abort controller and
  `compaction_end` are unported agent-session-runtime; Go ports only automatic
  compaction as a TransformContext hook), `6ca423447` event-bus leak
  (extensions runtime + `reload()`), `5446cd754` UI→TUI mode rename
  (cli/settings/interactive; the `index.ts`/`main.ts` hunks are the renamed
  type crossing an unported boundary).
- **ai-side n/a (4)** — `14cc26e86` Copilot models from account policy (real
  behavior, but inside the OAuth acquisition flow's `/models` fetch; Go
  consumes `Credential.AvailableModelIDs`, never fetches — revisit only if the
  OAuth exclusion reopens), `639a4664a` + `c0947e644` test-only (abort-test
  model swap; adaptive-thinking expectation drops
  `vercel-ai-gateway/anthropic/claude-opus-5-fast`), `2f7f75a20`
  generate-models.ts qwen3.8 movement — **regen signal for the next release**,
  joining `720f0e8ee` and `71f6c25c3` under the 2026-07-30 ruling.

## Drift at last sync check (2026-08-05) — pin advanced to 651d5d6a5

Delta `b784c8096..651d5d6a5`, **37** first-parent changes. Triage: **5 port, 31
n/a, 1 decide** — the decide (`44289550a` harness promotion) was resolved
**no-action**; see the 2026-08-05 ruling. **No release crossed** — no tag in
range, every `packages/*/package.json` still **0.83.0**, `models.generated.ts`
untouched, no catalog regen, no npm bump, no tag, no tweet. Two
`ai/scripts/generate-models.ts` edits queue catalog movement for the next
release (2026-07-30 ruling): `720f0e8ee` (Copilot `grok-4.5` → `/responses`) and
`71f6c25c3` (Groq thinking override `qwen/qwen3-32b` → `qwen/qwen3.6-27b`).

### Port worklist (5 → 4 Go commits)

| sha | subject | Go files | golden? | status |
|---|---|---|---|---|
| `686f193e5` | separate deferred request options | `ai/types.go`, `ai/models_runtime.go`, `ai/registry.go`, `ai/stream.go`, `ai/providers/faux.go`, `agent/loop.go`, `coding/compaction.go` | no — **breaking public Go API** (sanctioned, 2026-07-17) | `24600a3` |
| `42a06f947` + `d3da2e968` | composite OAuth refresh cancellation | `ai/auth_test.go`, `ai/models_runtime_test.go` (tests only) | no | `acd9696` |
| `bb6a1cddc` | rename backend contract to service | `server/{types,server,sessions,snapshots,errors}.go`, `server/unix/preset.go`, `server/internal/servertest/service.go` | **yes — one wire string** | `d7860e6` |
| `dab70b2cd` | sanitize service failures | `protocol/schemas.go`, `server/errors.go`, `server/server.go`, `protocol/testdata/upstream_messages.json` | **yes — the wire** | `aa586d1` |

### Review gates — parity FAITHFUL, go-review FIX-FIRST (2 MED + 4 LOW, all fixed)

Fixes in `db2ade7` (server), `89a7604` (ai), `e4ae08d` (cosmetics).

Both MEDs were invisible to the suite — the tests were green before and after
the defect:

1. **`InternalError.Error()` swallowed its own cause.** The type exists to keep a
   cause for local reporting while never serializing it, but `Error()` returned
   the sanitized constant, so the three reporter paths that log the *wrapper*
   rather than `.Cause` (`snapshots.go:145` via `ListModels`, `sessions.go:413`
   goSafely job errors, `sessions.go:553`) logged `"Internal server error"` and
   lost the cause entirely. In Go an error's message says what happened;
   redaction belongs to the wire formatter, which has its own constant. Fixed
   and verified wire-neutral by sweeping every site in `server/` that puts error
   text into a protocol field (exactly one — `server.go:696`, and it is
   `.Error()` on the extracted `*protocol.ValidationError`, unreachable from the
   `InternalError` branch which matches first).
2. **The load-bearing branch order in `toProtocolError` was not test-locked.**
   `InternalError` must match before `*Error` because it may wrap one. The
   comment was at the site, but `NewInternalError` had a single call site in the
   repo and it wrapped a plain `fmt.Errorf` — so swapping the blocks left
   `./server/...` fully green, and a future reorder would have leaked a wrapped
   `*Error`'s own code and message to the peer with no red. Now pinned:
   reordering yields `code = "not_found", want internal_error`.

Also fixed: `FetchDeferred` rebuilt its option struct instead of the
copy-then-overwrite pattern used by `Stream`/`StreamSimple` — no field dropped
today (`Wait` is the only non-base field and was hand-copied), but the next
upstream field on `DeferredFetchOptions` would have been dropped silently at
that one asymmetric site; a dead `opts` parameter on the faux `redeem`; a stale
`b` receiver on ~20 `*Service` methods; three conformance assertions comparing
against `server.InternalErrorMessage` (self-referential — they could not fail if
the constant moved) now hard-coding `"Internal server error"` to match their
`not_implemented` siblings; and one vacuous assertion at
`ai/models_runtime_test.go:1328` (`refreshCtx.Err() == nil` after `defer cancel()`
has already run).

### Triage lesson — a "pure rename" can still move the wire

`bb6a1cddc` reads as a mechanical `Backend`→`Service` rename and its diffstat
says the same. But one renamed string in `server/sessions.go` is built into a
`ProtocolError` with code `invalid_request`, which `crossesProtocolBoundary()`
lets through **verbatim** to the peer. Upstream changed it; nothing in the Go
repo asserted it, so both keeping and changing it were green. Rule for future
triage: when a rename touches a package that serializes errors, grep the renamed
identifier inside **string literals**, not just as an identifier, and check
whether any hit crosses the protocol boundary. A wire string is golden whoever
spells it.

### Two tests deliberately STRICTER than upstream

Recorded so a future reader does not read them as straight ports. (1)
`server/unix/sessions_test.go` pins the full wire text of the wrong-ID error by
exact equality; upstream's `sessions.test.ts` asserts only
`{ok:false, error:{code:"invalid_request"}}`. (2) `assertWireIsClean` re-encodes
the response through `protocol.EncodeServerMessage` and greps the **frame
bytes**, where upstream greps `JSON.stringify(response)`. Both are stronger and
both were mutation-verified non-vacuous. Consequence: a future upstream reword
of either message fails our suite loudly instead of passing silently — which is
the intent, given the interop argument.

### Follow-up recorded, NOT fixed this cycle

`ai/auth_resolve.go:211` returns `ctx.Err()` rather than `context.Cause(ctx)`, so
a **custom** caller cause does not survive to `GetProviderAuth`'s return value —
upstream rejects with `signal.reason`. Pre-existing, ~12 sites, a package-wide
convention, not introduced by this cycle and not asserted by upstream's tests.
Deliberately left out of a sync commit rather than smuggled in; worth its own
pass if reason identity ever matters to a consumer.

### Notable n/a (31)

- **TUI / interactive mode (7)**: `f24ab6e14`, `b0d382e25`, `73dd066ee`,
  `2c233a5c0`, `783097523`, `464804e15`, `fc4a3d99b`.
- **Agent harness tree (8)**: `f119b01cb`, `79cc1ef00`, `6a140371d`, `7bdeeb8f9`,
  `591f22a61`, `1e95e16b6`, `651d5d6a5`, `a80008b96` — all under the 2026-08-05
  ruling. Note `6a140371d` Windows-hardens `agent/src/harness/{skills,
  prompt-templates}.ts`, but those are the **harness copies**; the files the Go
  port actually ports from are `coding-agent/src/core/{skills,prompt-templates}.ts`,
  which already use node `path`. `coding/resources.go` inherits no latent
  Windows bug from this.
- **Agent docs (7)**: `b68decede`, `4756e7d12`, `04133eb01`, `4b85cd978`,
  `b08e114f7`, `0df5a69e5`, `f909da2bf` — all `harness-v2.md`.
- **Server legacy (1)**: `05bf9df65` deletes `packages/server/src/legacy/**`,
  which the 2026-08-01 ruling explicitly excluded — the exclusion catching up.
- **settings-manager (2)**: `97f0ccdd9` (recursive nested merge, #7572),
  `66534fbdc` (Mermaid rendering mode) — `settings-manager` is on the deliberate
  non-port list; the rest of the Mermaid work is `modes/interactive`.
- **Host / extension wiring (1)**: `e741cb05c` adds `baseUrl` to
  `ResolvedRequestAuth` in `core/model-registry.ts` for extension-supplied auth.
- **Host utils with no Go counterpart (3)**: `46b53b995` + rider `845bb970f`
  (new `utils/management-http.ts` `fetchWithRetry`, wired into version-check,
  tools-manager, remote-catalog-provider) and `7cf90c1d1` (auth-storage /
  models-store lock contention) — all under the 2026-07-17 "host
  `coding-agent/src/core` restructuring" non-port list.
- **Catalog-script signals (2)**: `720f0e8ee`, `71f6c25c3` — data folds into the
  next regen.

Whole-range sweep reconciled: `git diff b784c8096..origin/main --stat` over
`packages/{ai,agent}/src`, `coding-agent/src/{core,main,sdk}`, and
`packages/{protocol,client,server}/src` accounts for every in-scope file delta
against the verdicts above. No merge smuggled an unattributed hunk;
`packages/client/src` is untouched this cycle.

## Drift at last sync check (2026-08-04) — pin advanced to b784c8096

Delta `c6eb6281a..b784c8096`, **45** first-parent changes. Triage: **14 port, 30
n/a, 1 decide** — the decide was then ruled `port` by the owner mid-cycle, making **15**. **No release crossed** — no tag in range, every
`packages/*/package.json` still **0.83.0**; `models.generated.ts` moved only to
register Baseten (import + two map entries), so no catalog regen, no npm bump,
no tag, no tweet this cycle. Note `ai/scripts/generate-models.ts` moved (+105):
under the 2026-07-30 ruling that is a positive signal the NEXT release will move
the catalog data.

**Triage lesson repeated (2026-07-17 merge-smuggling class).** `fed6009cc`'s
subject says `fix(coding-agent):` and its diffstat tail is all host files, but it
carries a 22-file / +708−330 `packages/ai/src` rewrite. A truncated diffstat hid
it; only the mandated whole-range reconciliation sweep
(`git diff <pin>..origin/main --stat -- packages/ai/src …`) surfaced it. **Never
read a large diffstat through `tail`/`head`.**

### Port worklist (15 — 14 triaged + `a24fb9e96` added by owner ruling mid-cycle)

| sha | subject | Go files | golden? | status |
|---|---|---|---|---|
| `b9d360a2c` | retry transient provider errors in Google adapters | `ai/providers/google.go`, `ai/providers/retry.go` | no | pending |
| `cbaca6038` | preserve Gemini 3 tool call IDs | `ai/providers/google.go` `requiresToolCallID` | **yes — request body** | pending |
| `2e95584da` | `validateToolArguments()` coerces nullable union | `ai/validation.go` / `ai/schema.go` `Coerce` | tool args | pending |
| `0f5286d8a` | expose `shouldStopAfterTurn` on Agent | `agent/agent.go` | no — public API addition | pending |
| `b0e05b442` | resize images returned by tools | `coding/imageresize.go`, `coding/session.go` tool hook | **yes — tool result content + hints** | pending |
| `c1019d920` | add Baseten provider | `ai/providers/{openai,openai_chat_template,openai_compat,builtins}.go`, `ai/envkeys.go`, `ai/types.go` | **yes — request body** | pending |
| `305c014dc` | make session authentication transport-specific | `protocol/`, `client/`, `server/` | **yes — the wire** | pending |
| `32850ef7c` | resume after context-limited length stops | `ai/providers/openai_responses.go` | **yes — `rawStopReason` → `status.reason`** | pending |
| `fed6009cc` | model refresh cancellation caller-owned | `ai/models_runtime.go`, `ai/models_store.go`, `ai/credential_store.go`, `ai/auth_{types,helpers,resolve}.go`, `ai/builtins_models.go`, `ai/providers/{anthropic,cloudflare}.go` | no — **breaking public Go API** | pending |
| `25a2c8dcf` | generic sampling parameters | `ai/types.go`, `ai/simple_options.go`, `ai/providers/{openai,openai_responses}.go`, `server/protocol.go` | **yes — request body** | pending |
| `523b5a491` | normalize find root results | `coding/tools.go` (both find call sites) | **yes — tool output paths** | pending |
| `18d65de62` | preserve Copilot summary endpoint | `coding/session.go`, `coding/compaction.go` | **yes — compaction request baseURL** | pending |
| `382aa641c` | DRAFT: openai background mode responses | `ai/types.go`, `ai/models_runtime.go`, `ai/providers/faux.go`, lazy-api layer | **yes — new `StopReason "deferred"` in session format** | pending |
| `acbdc0d25` | bound OAuth refresh duration (15s) | `ai/auth_resolve.go` | no | pending |

**Port-order constraints.** Chronological order is load-bearing this cycle:
`fed6009cc` rewrites the `resolveStoredOAuth` block that `acbdc0d25` then edits;
`c1019d920` and `25a2c8dcf` both edit openai `buildParams`; `382aa641c` edits
`models.ts`/`types.ts` after `fed6009cc` rewrites them.

**Partial port — `32850ef7c`.** The `openai-responses-shared` half maps cleanly.
Its new `isRecoverableLength` in `ai/utils/overflow.ts` has **no Go home**: the
port has no overflow-detection analog at all (`isContextOverflow` was never
ported; its consumer is the unported agent-session runtime). Port the responses
half; the overflow half is recorded absent, not missed.

### Review gates — go-review SHIP, parity-review FIX-FIRST (4 divergences, all fixed)

**The suite was green through all four.** Every divergence below survived its
porter's own verification, including behavioral-red mutation testing, and was
caught only by an independent gate. The recurring failure mode this cycle was
**not** bad facts — porters' factual claims were repeatedly confirmed correct —
but bad inference from them, and specifically the shape: *"pi lacks X, therefore
Go should lack X"*, when pi's lack is an artifact of a foreign runtime or SDK.

- **D1 HIGH `403c4b8`** — find dropped the directory marker. See the pin entry.
  Also corrected `docs/parity-sweep-2.md:83`, which claimed "pi strips trailing
  slash" — it never has.
- **D2 MED `27eb2d7`** — `18d65de62` shipped zero behavior behind a "no Go
  analog" claim, with a **vacuous** test asserting its own double. See the pin.
- **D3 MED `1dd45be`** — `Agent.ShouldStopAfterTurn` never saw the run's signal.
- **D4 MED `1a22aa7`** — `CheckAuth`/`GetAvailable`/`Login` hang on a
  non-cooperative provider where pi rejects immediately (+ `GetAvailable` now
  fans out concurrently, matching pi's `Promise.all`; Go had stopped at the
  first error where pi invokes all).
- **LOW `8310959`** — `base64.StdEncoding` is strict where Node's
  `Buffer.from(x,"base64")` is lenient, so base64url / spaced / unpadded tool
  images passed through **unresized**, partially defeating `c740b38`.
- **Polish `3fb2a28`** — direct tests for `ai/keyed_lock.go` (the new
  concurrency primitive under both the credential store and the publication
  queue, previously covered only transitively); error-resolution hints on the
  deferred refusals; `errors.New` over verb-less `fmt.Errorf`.

**Two review findings were REJECTED on verification** — recorded so they are not
re-raised:
1. *"Bound `oauth.Refresh` in `resolveRefreshCredential` too."* Upstream at HEAD
   leaves that second site **unbounded**; only `auth/resolve.ts`'s
   `resolveStoredOAuth` got the 15s bound. The asymmetry is faithful; adding the
   bound would invent one pi lacks.
2. *"`publishProviderModels` should return `false`, not an error, on cancel."*
   Upstream returns `raceWithAbortSignal(queued, signal)`, so the promise the
   provider awaits **rejects** on abort; only the inner `queued` resolves false.
   Go already matches.

**Go-review validated several porter judgment calls explicitly**, and corrected
one rationale: the refresh **generation counter is not unreachable** defensive
code. Concrete interleaving — `beginProviderRefresh` bumps the generation, THEN
creates the context, THEN registers the cancel func; a `ClearProviders` landing
in that gap cancels nothing, so the generation check is the only thing dropping
the stale publication. The two-phase sweep in `ClearProviders` is likewise
load-bearing. Both rationales are now documented in-code.

### Triage lesson — a host-only file can still contain ONE ported table

`c1019d920`'s `model-resolver.ts` hunk was missed because that file is host code
and is routinely (and correctly) triaged `n/a` — but `defaultModelPerProvider`
inside it **is** ported, to `coding/resolve.go`. This is the second time that
table has been the source of a miss (cf. the 2026-07-30 cycle, where three
entries were absent and the qwen-token-plan ones were load-bearing). **Rule:
whenever an upstream change touches `model-resolver.ts`, diff
`defaultModelPerProvider` specifically before dispatching the file as `n/a`.**
Verified this cycle that `b04faa2da`'s `model-resolver.ts` hunk touches only
`resolveCliModel`, so its `n/a` stands.

### Tooling gap — the §3 differential harness is gone

`/tmp/pi-diff` and `/tmp/diffreq2` no longer exist, so the pi-parity-review
skill's §3 live pi-vs-Go request-body procedure **could not be run as written**
this cycle. Coverage fell back to the in-repo `differential_pi_test.go` (40
pi-derived tests, all passing) plus reviewer-built scratch harnesses (one ran
465 differential request-body scenarios against transcribed upstream TS). Both
are weaker than the live harness. **Regenerate it from the npm build before the
next request-building change.**

### New deliberate divergences

- **Google retry keeps real response headers and transport retries** (re:
  `b9d360a2c`, Go `8b78853`). pi's Google adapter reaches `retryProviderRequest`
  with `@google/genai`'s `ApiError`, which carries `status` but no `headers`
  (verified against the shipped build, `@google/genai@1.52.0`
  `dist/node/index.mjs:7412`, `:13465`); `retryGoogleRequest` sets
  `headers: undefined` only to satisfy the provider-error guard. So in pi,
  Google alone ignores `retry-after`/`retry-after-ms`/`x-should-retry` and does
  not retry transport failures — while Anthropic/OpenAI do all four, because
  their `APIConnectionError` happens to declare both fields as own properties.
  **That is an SDK artifact, not a policy**: upstream's own docstring states the
  intent is the shared policy "honoring retry-after", mirroring the Anthropic and
  OpenAI adapters. Go's provider speaks raw `net/http` and genuinely has the
  headers, so it **keeps** honoring them and keeps retrying transport failures.
  Same class as the 2026-06-17 Bun `/proc/self/environ` ruling, inverted: there
  we declined to *copy* a workaround for a foreign runtime defect; here we
  decline to *manufacture* a foreign SDK's information loss. Bounded: retries are
  opt-in via `MaxRetries`; no error-surface divergence (Google has no
  `providerError` renderer, so an oversized server delay still falls back to
  backoff rather than failing fast, exactly as in pi). Future upstream commits to
  `retryGoogleRequest` are `port`, but re-check this clause each time. **A future
  parity sweep comparing the Google adapters must not re-flag this.**
  Process note: the first port of `b9d360a2c` reproduced pi's shortfall as a
  deliberate `headerlessErrors` policy — inventing a three-branch mechanism whose
  only referent in pi is an *absence*. It passed its own author's verification and
  was caught only by the independent parity gate. Faithful ports mirror code, not
  holes.

### Open `decide` (1) — RESOLVED and ported

- **`a24fb9e96` "preserve auth header deletion markers" (#7539)** — the
  2026-06-24 ruling declined null-`ProviderHeaders` suppression on the stated
  condition *"Revisit only if a consumer needs to suppress a default header."*
  This commit is that consumer appearing upstream: the host stops stripping
  nulls and passes `ProviderHeaders` through, with a new cloudflare-compat test.
  The hunk itself is in `coding-agent/src/core/model-registry.ts` (host, so not
  automatically `port`), but it is the named trigger. **Owner ruled 2026-08-04:
  port it** (Rulings, top). **Ported**: `ai.ProviderHeaders` =
  `map[string]*string` now types `Model.Headers`, `StreamOptions.Headers`,
  `ModelAuth.Headers`, `Provider.Headers()`, `CreateProviderOptions.Headers`,
  `ModelsStreamTransforms.TransformHeaders` (renamed to
  `ModelsRequestTransforms` on 2026-08-05 by `686f193e5`), and the `agent`/`coding` host
  header options — a sanctioned public API break (2026-07-17 clause). Upstream's
  own hunk deletes two null-strippers in host code with no Go home; what landed
  is the capability they preserve, end to end: `mergeHeaders` carries markers,
  and each adapter applies them the way pi does — `applyProviderHeaders`
  (delete, mirroring an SDK `defaultHeaders` null) for
  openai-completions/openai-responses/anthropic-messages, and
  `providerHeadersToRecord` (drop, pi `utils/headers.ts`) for
  google-generative-ai and pi-messages, which build their own requests and so
  cannot unset an adapter-owned header. **Item 1 of the 2026-06-24 divergence
  list is retired**: the cloudflare-ai-gateway conditional skips collapsed into
  `cloudflareAIGatewayAuthHeaders`, one marker bundle mirroring pi's
  `cloudflare-auth.ts` resolver. Cloudflare auth stays adapter-inline (2026-06-24
  items 2–3 still stand — the compat globals never reach the Models runtime), but
  it is now produced as `{cf-aig-authorization, Authorization: null, x-api-key:
  null}` instead of per-adapter `if provider != cloudflare` branches. That
  collapse also **closed a latent divergence**: anthropic-messages now takes pi's
  plain api-key branch for the gateway, so a cloudflare model whose compat asks
  for `sendSessionAffinityHeaders` sends `x-session-affinity` as pi does. Not
  ported: pi's `assertRequestAuth` header fallback in anthropic-messages (Go
  still requires an api key or `ANTHROPIC_AUTH_TOKEN` there) — a pre-existing gap
  unrelated to markers, left as-is rather than widened.
  **Both gates passed** on `bcd4712`: go-review **SHIP** (no must-fix; it went
  looking for a reason to reject `map[string]*string` and endorsed it — JSON
  round-tripping is decisive, since a `struct{Value string; Delete bool}` would
  need hand-written marshallers reintroducing the same tri-state behind a bigger
  surface, and a parallel `DeleteHeaders []string` cannot express the
  left-to-right fold where a marker must cancel in BOTH directions) and
  parity-review **FAITHFUL** (correspondence unbroken; the strongest evidence is
  non-circular — `TestDiffCloudflareAiGatewayAuthHeader` and
  `TestResponsesCloudflareAIGateway` are **not in the commit at all**, untouched
  files with unchanged asserted bytes, and both go red under a nil→skip
  mutation). Polish applied in `8cec36a`: the pointer-**aliasing contract** is
  now documented (merges copy pointers, so a consumer `TransformHeaders` hook
  writing `*headers["X"] = …` instead of replacing the pointer would silently
  mutate the shared catalog model for every other request — a data race `-race`
  can never catch here, because it lives in consumer code); `ai.HeaderValue`
  added so callers need no temp var per header; and `applyProviderHeaders` now
  iterates in **sorted name order**, since a single map holding both
  `"Authorization": nil` and `"authorization": "x"` canonicalizes to one
  `http.Header` key and map order decided whether the `Del` or the `Set` landed.
  Sorted was chosen over markers-first because it is the tie-break `mergeHeaders`
  already uses for the same unavoidable divergence (pi has object insertion
  order; Go maps have none), so the raw and merged paths agree — markers-first
  would have invented a delete-beats-set precedence pi has nowhere.

### Harness rebuilt — and it immediately found a model-visible divergence

The pi-vs-Go differential request-body harness had been lost twice because it
lived in `/tmp`. Rebuilt at **`~/.cache/pi-diff/`** (durable — `pi-npm` and
`pi-upstream` have both survived there), one self-healing runner
(`~/.cache/pi-diff/run.sh`), exits non-zero on mismatch;
`.claude/skills/pi-parity-review/SKILL.md` §3 now points there. It has **two
backends** because 0.83.0 contains none of this cycle's changes: `dist` for
shipped behavior, `src` (upstream TS at the synced sha, `git archive`d
read-only) for unreleased — a build-only harness would have reported false
mismatches. Scenarios: the historical six plus three added for this cycle.
Google scenarios compare only `$.contents`, because pi's `onPayload` sees
`@google/genai` SDK params where Go sees the REST body — stated explicitly
rather than letting a projection imply full coverage.

It **confirmed this cycle's three request-body ports faithful** (sampling-params
per-key merge landing last, Baseten `chat_template_args` + `reasoning_effort`,
Gemini 3 tool call IDs present for 3.x and absent for 2.5).

**NEW FINDING — tool-call argument key order is lost (PRE-EXISTING, escalated,
NOT fixed).** Go destroys the model's original argument key order:

```
pi: "arguments": "{\"path\":\"/tmp\",\"depth\":1}"
go: "arguments": "{\"depth\":1,\"path\":\"/tmp\"}"
```

`ai.ToolCall.Arguments` is `map[string]any` (`ai/types.go`) and
`parseStreamingJSON` returns `map[string]any` (`ai/providers/json.go`), so order
dies at parse and again on every session load; `encoding/json` then sorts on
replay. pi's `parseStreamingJson` returns a JS object preserving source order end
to end. On openai-completions `arguments` is a **string** in the body, so a model
replaying its own prior tool call is conditioned on literally different text, and
the transcript prefix shifts — which matters for prompt-cache hit stability. Same
sites in `openai_responses.go`, `anthropic.go`, `faux.go`. Left **FAILING** in
the harness (3 of 11 scenarios) rather than loosening the canonicalizer. Fixing
it needs an order-preserving `ToolCall.Arguments`, a public API change with reach
across the SDK — **escalated under the "public Go API → escalate, don't ship"
rule**, not fixed silently. Predates this cycle; blocks nothing.

**FIXED (2026-08-04, same cycle) — `fix(ai): preserve model tool-call argument
key order`.** The owner's API call came back: additive is acceptable, so
`ai.ToolCall` keeps its `Arguments map[string]any` and gains
`ArgumentsOrder ai.OrderedObject` (`json:"-"`, never persisted) plus
`OrderedArguments()`. `parseStreamingJSON` now returns the order it parsed, and
`ToolCall.UnmarshalJSON` recovers it from the stored bytes — the session file
already carries the order as the key order of its own `arguments` object, so
**the session format is unchanged and no golden moved**. All four replay sites
(`openai.go`, `openai_responses.go`, `anthropic.go`, `google.go`, plus
`faux.go`) and the compaction summarizer's `Object.entries` rendering now use
it, at every depth. Baseline entry `go-tool-call-argument-key-order` **retired**
from `~/.cache/pi-diff/known-divergences.json`; the harness reports **11 PASS,
0 KNOWN, 0 FAIL, exit 0**.

**ALSO FIXED (2026-08-04, same cycle) — the same order on the protocol wire.**
The request-body fix stopped at `server/protocol.go`, which pushed
`call.Arguments` through `ToProtocolJSONValue` and so still handed the encoder a
Go map. **The wire is a byte-golden class of its own** (2026-08-01 ruling) and
CBOR map key order is observable to a peer: pi's `toProtocolJsonValue` rebuilds
the object with `Object.entries` and its CBOR encoder walks `Object.keys`, both
insertion-ordered, so a Node pi emits a tool call's `input` in the model's order
and Go did not. `protocol/cbor` gains `OrderedObject`/`OrderedField` (the CBOR
twin of `ai.OrderedObject`) which the encoder recognises **before** the kind
switch — a naive pass would have emitted it as a CBOR *array*, since it is a
slice — plus a duplicate-key guard, and `requireJSONValue` accepts it as the
JsonValue it is. The bridge copies `ai.OrderedObject` → `cbor.OrderedObject` at
every depth and both tool-call sites now read `OrderedArguments()`.
`protocol/testdata/gen-messages.ts` gained an `orderedToolCalls` corpus derived
by running **upstream's own** `toProtocolAssistantMessage` /
`toProtocolToolResultMessage` (`git archive b784c8096 packages/server/src`,
`@earendil-works/pi-ai@0.83.0`, integrity-checked) into upstream's `codec.ts`;
**non-circularity proved** — the pre-change generator reproduces the committed
pre-change fixture byte-for-byte, and all four existing corpora are unchanged by
the regeneration.

**Still open (receive side, no production consumer today).** Go's CBOR decoder
resolves a map to `map[string]any`, so a decoded frame loses the wire order that
pi's decoder keeps in a JS object. Emission is what a peer observes and is now
correct; a Go peer that *re-emits* something it decoded would still reorder, so
the new vectors are deliberately kept out of `serverMessages` (whose round-trip
test asserts re-encode is a fixed point). Closing it means changing what a
decoded `JsonValue` *is* in Go — a much wider public break than the emission fix
needed, so it is recorded rather than taken.

**Receive side — ruled NOT to fix, and now guarded.** Three sites drop the order
on the way in: `protocol/cbor/decoder.go` (a decoded CBOR map becomes
`map[string]any`), `coding/transcript.go` `parsePartialToolInput`, and
`coding/transcript.go` `cloneJSONValue`. Upstream pi has **no mechanism for this
at all** — its decoder (`packages/protocol/src/cbor/decoder.ts` case 5) builds a
plain JS object key by key and JS objects preserve insertion order for free — so
there is nothing to port, and preserving it in Go would be invented machinery
with no upstream counterpart, the exact failure mode this cycle's reviews caught
repeatedly. It is safe **today** because a decoded transcript never reaches a
model: it stays in `protocol` types end to end (`coding/remotesession.go` →
`TranscriptState.Snapshot()` / `.Transcript()`) as a display surface for a Go
client observing a remote session, and is never converted back into
`ai.ToolCall`/`ai.Message`. That "today" is now enforced:
`TestDecodedTranscriptIsNeverConvertedToAIMessages`
(`coding/decodedtranscript_guard_test.go`) scans every non-test file in the
module and fails if any function consumes a `protocol` transcript/content type
and produces an `ai` message/tool-call type — the emit direction
(`server/protocol.go`, ai → protocol) is unaffected. Wire a decoded transcript
back into a local session and the suite goes red with the reasoning and the two
legitimate exits (preserve order at all three sites first, or don't route
decoded transcripts to a model).

### Notable n/a (30)

`1d0c97471` + `cd20a8d2e` harness-v2 in-memory storage (~1,900 lines) — standing
no-`harness/`-tree exclusion. `da66636cc` symlinked session discovery — no Go
home: `coding/session_store.go` `ListSessions` reads one per-cwd dir and has no
cross-project directory walk. `ab5f8d88e` type-level only (reverts
`18d65de62`'s generic signature, no behavior). `ebf33c0c2`'s one `core/` line is
a `UiMode`→`TuiMode` type alias. `d93e7e88f` (prompt-during-compaction throw) and
`e56893f4c` (compaction disconnect/reconnect removal) are host
agent-session-runtime event plumbing. `fed6009cc`'s host halves
(`model-runtime.ts`, `auth-storage.ts`, `utils/abort.ts`, interactive) stay `n/a`
under the 2026-07-17 ruling. `b04faa2da` model-resolver (host). `b06dc76fd`
package-manager (extensions). Remainder: tui (`a8ee03b81`, `0e633790c`,
`b103937d3`, `fa07e7bd9`, `e8a17822d`), modes (`a4475344f`, `3d264e85b`),
cli/args (`4f4762f06`, `c72728bc1`), docs (`786c76cb7`, `a0014c1a8`, `f7ea2ef38`,
`f27aaf66c`), packaging/deps (`816237c10`, `221a842c1`), `.github`
(`a96fb984d`, `bd3440e5b`), tests-only (`e563301dd`, `95249a727`, `0524d6897`,
`b784c8096` — the last a rider removing the test `acbdc0d25` added).

## Drift at last sync check (2026-08-03, second run) — pin advanced to c6eb6281a

Delta `01eeafd14..c6eb6281a`: **3** first-parent changes — **0 ports, 3 n/a, 0
decide**. Nothing landed in ported surface, so this is a **pin advance only**:
no Go commit other than this ledger update, and the parity/review state carries
over from `01eeafd14` unchanged.

**No release crossed.** No tag in range, no `packages/*/package.json` moved (all
still `0.83.0`), and `models.generated.ts` / `image-models.generated.ts` /
`generate-models.ts` are untouched. No regen, no tag, no release tweet.

### Triage rows (01eeafd14..c6eb6281a — 3 changes, all n/a)

- **`f0deb8dd8`** (`Revert docs changes`, `packages/agent/docs/harness.md` +
  `agent-harness.md`) — **n/a**, documentation only, zero code files. Reverts
  most of the harness-v2 doc expansion that arrived with `88ed89dba`/`01eeafd14`
  last cycle. Does not change the forward signal recorded there: the harness
  rewrite is still coming and `packages/agent/src/harness/**` still has no Go
  tree.
- **`a077fff0b`** (`fix(coding-agent): identify failed model catalogs`,
  `modes/interactive/components/model-selector.ts` + its test + CHANGELOG) —
  **n/a**, `modes/` interactive TUI is on the standing non-port list. A
  one-line change to how a failed provider catalog is labelled in the selector
  list; no `core|main|sdk` file touched.
- **`c6eb6281a`** (`fix(coding-agent): bound post-login catalog refresh`,
  closes upstream #7027, `coding-agent/src/core/model-runtime.ts` + CHANGELOG) —
  **n/a** under the **2026-07-17 model-runtime ruling** (host
  `coding-agent/src/core` restructuring — `model-runtime`/`model-config`/
  `provider-composer`/`models-store`/… — is explicitly not ported; only the
  facade's `packages/ai/src` deltas are). Content for the record: hoists the
  inline `15_000` into `DEFAULT_MODEL_REFRESH_TIMEOUT_MS`, and bounds
  `ModelRuntime.login`'s post-login `refresh()` with
  `AbortSignal.timeout(DEFAULT_MODEL_REFRESH_TIMEOUT_MS)`, joined via
  `AbortSignal.any` with `interaction.signal` when present. This is host
  lifecycle around an already-`n/a` facade — `packages/ai/src` has no hunk in
  the commit, and Go has no `ModelRuntime` type. No new boundary question, so
  no `decide`.

**Merge-smuggling sweep** (2026-07-17 rule): `git diff 01eeafd14..origin/main
--stat` over `packages/ai/src`, `packages/agent/src` and
`coding-agent/src/core` returns exactly one file —
`core/model-runtime.ts` (+8/−2) — which is `c6eb6281a` itself, already
accounted for above. Nothing smuggled via merges.

## Drift at last sync check (2026-08-03) — pin advanced to 01eeafd14

**The 2026-08-02 hold is released.** That entry deliberately kept the pin at
`ab366ebe9` because the remote-session worklist was open: the pin means "TS
source fully reviewed *and* ported", and `server/` did not exist in the Go tree
yet. All five ports have now landed, been reviewed by both gates, and had every
finding fixed — so the pin advances past both ranges at once.

Combined delta `ab366ebe9..01eeafd14`: **44** first-parent changes — **5 ports,
39 n/a, 0 decide**. The 40 changes of `ab366ebe9..73414d08b` were triaged on
2026-08-02 (see that entry for their rows); the 4 below are new.

**No release crossed.** No tag in range — `git tag --contains 73414d08b` is
empty, and upstream has no `v0.83.*` tag at all since npm 0.83.0 was published
untagged, so tag absence was not relied on alone. No `packages/*/package.json`
moved; ai / coding-agent / protocol / client / server / agent / tui are all
still `0.83.0`. `models.generated.ts`, `image-models.generated.ts` and
`generate-models.ts` are untouched. No regen, no tag, no release tweet.

### New triage rows (73414d08b..01eeafd14 — 4 changes, all n/a)

- **`4c01c7093`** (`fix(tui): skip clipped rows during layout painting`,
  `packages/tui/src/layout.ts`) — **n/a**, TUI is on the standing non-port list.
  A paint-loop bounds hoist inside `paintBox`: clamps `firstRow`/`lastRow`
  instead of `continue`-ing per row.
- **`35fe55573`** (**merge PR #7459, compose experimental CLI commands**,
  `coding-agent/src/cli/experimental/{cli,command-options,command}.ts` +
  `commands/{client,pi,server}.ts`) — **n/a** under the 2026-08-02 `aa0ec808b`
  ruling (`cli` is outside `core|main|sdk`) plus the standing bun/CLI-packaging
  exclusion. **This is the case that ruling told us to re-check, and the answer
  is negative**: `transport-address.ts` was not modified by the merge and is
  unchanged at `origin/main` (last touched by `15b6617fa`, pre-pin); its only
  importers repo-wide are inside `src/cli/experimental/`; and the dependency
  direction is CLI → transport, never the reverse — `parseTransportAddress`
  turns a user-supplied `unix:///abs/path` option string into `{transport,path}`,
  while our `client/unix.go` and `server/unix/` accept an already-resolved plain
  path and never parsed a URL. Real socket-path derivation (`getSocketPath()`)
  still lives in the unported `server/src/legacy/`. `experimentalCli` still has
  zero non-test consumers. **Carry-forward**: the CLI now has concrete
  `server`/`client` subcommand shapes (`--listen` repeatable, `--connect`
  single, `--auth-token`/`--auth-token-file`); when a host implementation lands
  it will call the ported client/server APIs, but the parsing layer stays
  host/CLI. Go's `cmd/pi` is a small demo CLI with no such subcommands, so there
  is nothing to converge on.
- **`88ed89dba`** + **`01eeafd14`** (`docs(agent)`, harness v2 split into
  effects/generator variants, then durable queue-item cancellation) — **n/a**,
  documentation only, for `packages/agent/src/harness/**`, which has no Go tree.
  Zero code files. **Forward signal**: both variants declare that everything in
  `packages/agent/src/harness` and `packages/storage/sqlite-node` may break,
  preserving only "old coding-agent v3 JSONL sessions must open and restore
  idle". A large harness rewrite is coming; the exclusion means no port
  obligation. Re-escalate only if it lands surface in `packages/agent/src`
  *outside* `harness/`.

**Merge-smuggling sweep** (2026-07-17 rule): `git diff 73414d08b..origin/main
--stat` over `packages/ai/src`, `packages/agent/src`,
`coding-agent/src/{core,main.ts,sdk,client}` and
`packages/{protocol,client,server}/src` returns **empty**; the full-range stat
(13 files) equals the sum of the four per-commit diffstats exactly.

### Review gates — 7 HIGH, ~23 MED, all fixed before the pin moved

Nine independent gates ran (parity + go-review over each of the four areas,
plus triage). Every area came back **diverges**/**fix-first**. Fixes landed in
`b9b70fb`, `a1bbcfc` and `e5f820d`. The HIGHs, because each says something about
where this port is structurally exposed:

1. **Union markers took value receivers** (protocol). `T` and `*T` both
   satisfied each union while only `*T` implemented `Validator`, so a hand-built
   value member skipped validation entirely — `EncodeServerMessage` produced a
   189-byte frame for an item with `id: ""` and `role: "not-assistant"`, i.e. we
   emitted what our own decoder rejects. pi validates every message, so this was
   a parity gap as much as an API-shape one. Markers are pointer-receiver now
   and the six structurally-open unions carry an unexported marker.
   `coding`'s `asPointerItem` shim existed to paper over the same hole and is
   gone — the compiler now proves the value form unreachable.
2. **`acquire` read the cleanup gate before the wait** (client) — found
   independently by BOTH gates. pi reads `sessionCleanupRequired` *after*
   awaiting the in-flight detachment; we read it in the same lock section that
   looked the detachment up, and `doRelease` settled the shared op before
   `release` ran `markCleanupRequired`. An acquirer parking behind a *failing*
   close-detach skipped reconciliation: commands were `[attach detach]` where pi
   gives `[attach detach detach attach]`, the lease came back on a stale
   `attached` bit, and the owed detach later fired as a spurious detach against
   a session a live lease still held.
3. **`handleData`'s loop guard asked "disconnected?" not "still my attempt?"**
   (client). A callback that reconnects mid-chunk left the rest of the chunk to
   be applied to the *new* connection, which then killed the freshly established
   link over a response it never asked for.
4. **The detach command arm never failed** (server). pi opens it with
   `requireAttached`; we returned `ok` for a detach of a session the connection
   does not hold. A test pinned the wrong behavior.
5. **Accepted sockets were never closed on the normal read-end path** (server).
   Only the `netFD` finalizer reclaimed them — with GC off, 300 disconnected
   peers left 300 open fds. fd exhaustion creates no GC pressure, so a peer
   churning connections outruns the finalizer; combined with an accept loop that
   gave up permanently on any error including a transient `EMFILE`, that was a
   complete DoS chain.
6. **Two shutdown/attachment races** (server). `Accept` checked `isClosing` and
   registered the connection in separate critical sections, so a connection
   accepted around `Close` survived shutdown forever (race probe hit it at round
   1113); and `attach` wrote `conn.sessionIDs` and `live.connections` under
   different locks, so a `disconnect` landing between them left a dead
   connection nothing removed — `maybeDispose` never fired and the backend
   session stayed locked against reattachment.
7. **`sessionBinding` was `struct{}`** (coding). Go allocates zero-size values
   at `runtime.zerobase`, so every `&sessionBinding{}` compared equal and the
   guard protecting subscriptions registered outside the lock degenerated to a
   nil check. The port invented that token precisely because pi's `#bind` is
   synchronous and needs no such guard. It carries an id now. Alongside it,
   `Close` could return while a refused operation was still about to acquire an
   attachment, stranding an exclusive lease under the canonical
   `defer session.Close(ctx)`.

**`GOARCH=386` was broken and is fixed.** `maxUint32` was an untyped int
constant, which does not fit in an int on a 32-bit build; `protocol` would not
compile and the breakage cascaded to `client`, `server` and `coding`. Wire
lengths are `uint64` now and range-checked before narrowing, which also closed a
latent panic: a declared frame length >= 2^31 went negative as an int, slipped
past the limit check, and would have hit a negative slice bound.

### One pre-existing miss closed

`ai.ToolResultMessage` had no `Usage` field, though upstream added `usage?:
Usage` in **`e3d066daa` (2026-05-04)** — a month before this port began. It was
invisible until the new server bridge started mapping tool results onto the
wire. Nothing upstream populates it either (it is an SDK affordance), so the fix
is additive and `omitempty`: no session golden moved and there is nothing to
backfill. Recorded here because it is a miss from the initial port, not from
this delta.

### New deliberate divergences

- **`client.TransportFactory` takes a `context.Context`** and `dialUnix` uses
  `DialContext`. `Connect` previously ignored its deadline for the entire dial
  (measured: 401 ms against a 50 ms budget). pi has no cancellation at all, so
  this is port-invented surface rather than a parity fix. Documented at both
  sites; approved by the owner as an API change.
- **Snapshot broadcasts are coalesced** (server). pi chains every `broadcast()`
  into its own pass; we keep one pending flag plus one in-flight pass. Clients
  see fewer `server_snapshot` events, but the revision still moves only forward
  and every snapshot is whole.
- **The CBOR encoder rejects untagged embedded structs** rather than flattening
  them like `encoding/json`. pi has no embedding and no protocol message embeds,
  so porting promotion/shadowing rules buys nothing; a `cbor:"name"` tag makes
  an embedded field an ordinary named one.
- **`coding`'s notification fan-out takes `notifyMu` before `mu`**, not while
  holding it. The reverse order deadlocks: a delivering goroutine holding
  `notifyMu` runs a listener that reads `ID()`/`Phase()`/`State()` and wants
  `mu`. The cost is that a listener must not call `Subscribe` or a mutation,
  which is documented on `Subscribe` and `OnListenerError`.

### Known gaps and follow-ups (not blocking the pin)

- **One fix ships without an isolating test**: the settle-ordering half of
  finding 2. The read-before-wait half is test-locked with `testing/synctest`,
  but the window where `doRelease` settles before `markCleanupRequired` could
  not be forced open (0/300 iterations with the parking window widened; freezing
  the closing goroutine on `sessionLease.mu` is incompatible with synctest,
  since mutexes are not durably blocking). It ships on the parity argument that
  pi's microtask ordering makes the sequence deterministic there. Flagged rather
  than papered over.
- `GOARCH=386 go vet ./...` fails on three **test** files (`client/connection_test.go`
  const overflow, `server/server_test.go`, `coding/imageresize_parity_test.go`).
  Confirmed identical at baseline `a1bbcfc`, so it is pre-existing and not a
  regression of this cycle; the `go build` gate is unaffected.
- Deferred as new public API the owner has not approved: a `MaxConnections` /
  in-flight-request cap on the server, and unexporting `client.State`.
- Deferred refactors: collapsing `pendingRequest`/`sharedOp`/`handshake` into a
  generic `future[T]` (~40 lines); the `protocol.ProtocolVersion` /
  `ProtocolError` / `ProtocolErrorCode` stutter; doc comments on ~50 exported
  protocol methods.
- Two narrower parity questions surfaced late and are unfixed: `protocol`'s
  parse errors embed a **peer-controlled property name unbounded** (`has
  unexpected property %q`), bounded only by `maxFrameLength` at 16 MiB — upstream's
  parse errors are constant strings, so there is no parity anchor either way;
  and `requireJSONValue` still rejects a Go struct in `Input`/`Details` even
  though the encoder writes it as the plain object pi accepts.

## Drift at last sync check (2026-08-02) — pin HELD at ab366ebe9

> **RESOLVED 2026-08-03.** The worklist below is empty: all five ports landed
> (Go `1a3b46b`, `3289329`, `86a91f3`, `d9256a9`, `78b08a9`, `a402e1d`,
> `9afbdf2`, `920f37b`), both review gates ran over every area, and the pin
> advanced past this range to `01eeafd14`. See the 2026-08-03 entry. The
> "not started" / "partial" statuses below are preserved as the record of what
> was true when this triage was written.

**Pin deliberately NOT advanced.** Delta `ab366ebe9 → 73414d08b`: **40**
first-parent changes — **5 ports, 35 n/a, 0 decide**. The pin field means "TS
source fully reviewed *and* ported"; all five ports are the remote-session stack
and only two have partially landed, so advancing the pin would tell a future
triage that `packages/{protocol,client,server}` and
`coding-agent/src/client/` are done when `server/` does not exist in the Go tree
yet. Triage for the range is complete and recorded here; the porting is not.
Advance the pin to `73414d08b` only once the worklist below is empty.

**No release crossed** — no tag in range, npm `pi-ai`/`pi-coding-agent` stay
**0.83.0**, no catalog regen, no byte-golden moved.

### Port worklist (all under the 2026-08-01 remote-session ruling — no new decides)

- **`5a38a1c12`** (runtime-neutral client, `packages/client/src`) — **partial**.
  Landed: `client/errors.go` + `types.go` (Go `86a91f3`), `client/state.go` (Go
  `d9256a9`). Remaining: `client.ts` → `client/client.go`, `connection.ts` →
  `client/connection.go`. `promise.ts` has no Go analog (channels). The commit's
  `agent/src/harness/agent-harness.ts` hunk (`shutdown()` split into
  `requestShutdown()` + `waitForShutdown()`) is harness-internal → **n/a**.
- **`7d5fc9499`** (unix client transport) — **not started**. `client/src/unix.ts`
  → `client/unix.go`. Golden: socket path resolution + frame layout.
- **`73b24639f`** (server core + `legacy/` split) — **not started**.
  `server/src/{server,protocol,sessions,snapshots,listener,connection,errors,types}.ts`
  + `transports/unix/` → new `server/` package. `server/src/legacy/**` stays
  **n/a** per the ruling; `server/src/testing/**` is upstream's own fake backend,
  port only if the Go server tests want that shape.
- **`03eba409c`** (server/protocol invariants) — **partial**. The
  `protocol/src/schemas.ts` half landed in Go `3289329`; the
  `server/src/protocol.ts` half (+244) remains. **Wire-golden**: `"tool_call"` →
  `"toolCall"` (content type AND `assistant_delta.kind`), stop reason
  `"tool_use"` → `"toolUse"`, assistant/tool transcript items split into
  status-discriminated unions (`complete` requires a non-error `stopReason`;
  `error`/`aborted` pin theirs; tool `isError` is now a literal per status), and
  `item_finished` narrowed to the terminal variants only. Confirms the ruling's
  warning that `schemas.ts` is not frozen — it moved again this cycle.
- **`06a1ceb8d`** (coding-agent remote client controller) — **not started**.
  `client/src/session-handle.ts` → `client/`;
  `coding-agent/src/client/{remote-session,transcript}.ts` →
  `coding/remotesession.go` + `coding/transcript.go`.

The **wire is a new golden class** (per the ruling): CBOR encoding and frame
layout are observable to a *peer*, so this needs its own encode/decode round-trip
+ cross-implementation frame-vector corpus, not the usual request-body diff.

### Notable n/a (35)

- **`d2be68dbe`** (**avoid auth read lock contention**, `core/auth-storage.ts`
  +87 — process-shared read cache keyed on a `dev:ino:size:mtimeNs:ctimeNs`
  revision, plus a coalesced in-flight reload). **No Go analog** — the port takes
  an injected `CredentialStore` and never ports the host-side disk store
  (`f8bec25f` precedent). Test-only rider **`e6fb3ec68`** likewise.
- **`14551e769`** (**increase connection attempt timeout**,
  `core/http-dispatcher.ts`) — undici `connect.autoSelectFamilyAttemptTimeout`
  raised from Node's 250 ms default to 2 s for high-latency routes. Node-runtime
  tuning with no `net/http` equivalent; same class as `2117b61c` and the omitted
  bun `/proc/self/environ` fallback.
- **`784653468`** (**codex account websocket**) — `openai-codex-responses.ts`
  re-keys the session websocket cache `Map<sessionId, Map<accountId, conn>>` so
  two accounts on one session no longer share a socket. Codex is an excluded
  provider and has no Go counterpart in `ai/providers/`.
- **`8f9e76974`** (**recover stalled availability refreshes**,
  `core/model-runtime.ts`) — host runtime, `n/a` under the 2026-07-17 ruling.
- **`aa0ec808b`** (**experimental CLI parser**, `src/cli/experimental/{command,
  auth,transport-address}.ts`) — `cli` is outside `core|main|sdk`, and the new
  files have **zero non-test consumers** upstream today. Forward signal only: a
  `pi connect`-style CLI over the client transport is coming; it stays host/CLI
  when it lands, but re-check whether `transport-address.ts` becomes a dependency
  of the ported `client/unix.ts` surface.
- **`f074efd92`** (**ui mode setting**) — the lone `src/main.ts` hunk is one line
  (`alt: parsed.alt` → `uiMode: parsed.uiMode`) feeding interactive mode;
  `settings-manager.ts` and the TUI are on the non-port list.
- **Agent-harness session-store churn (11 changes)** — `12a4b2429` (store/repo
  file rename), `977ec833b` (remove session search index), `b77786582`
  (per-session keyed operation queues), `a0bb4a489` + `4488ad55c` + `a11652343` +
  `b5c7e5549` (sqlite connection cleanup / linear-time reads / branch-tip cache +
  migration `002_branch_tips.sql`), `0d43c5804` (session reader), `6e48c10f5`
  (branch queries v2), `4279da1b7` (session storage API — another repo/store
  rename round-trip). `packages/agent/src/harness/` has no Go tree; the
  2026-08-01 ruling re-confirmed the exclusion holds after tracing the
  server→`Backend` dependency edge.
- **TUI (10 changes)** — `ea781d68f`, `8ac92f831`, `696a828a4`, `3c717842e`,
  `bf4a90d81`, `6129a353b`, `b3ed27b3f`, `af187eee4`, `583f153d5` (source
  filename normalization: `TuiAltScreen.ts` → `tui-alt-screen.ts`), `73414d08b`.
- **Reverted, net zero** — `1fdf21621` (switchable terminal renderers) was
  reverted same-day by `b70c0f5b4` (#7473). Nothing to port either way.
- **Test-only** — `fbab971da` (`packages/ai/test`), `a1403af8e` + `374c5b6dd`
  (agent timeout determinism), `a6f7317df` (models.json hot reload).
- **`7724472ea`** — `utils/clipboard.ts`, consumed only by the TUI.

### Two regen signals for the next release

Neither is portable now; both predict catalog movement. Per the 2026-07-30
ruling, decide the regen by executing `JSON.stringify(MODELS)` against both npm
builds — never from git, and never by `cmp`-ing `models.generated.js`.

1. **`a688e257c`** — `ai/scripts/generate-models.ts` routes **Fireworks Kimi K3**
   through OpenAI compatibility. A generator-DATA-only hunk is exactly the
   positive signal that ruling names.
2. **`fbab971da`** — the `packages/ai/test` fixes swap `zai/glm-5.1` → `glm-5.2`
   and delete the `glm-4.5-air` assertions outright, i.e. both models are gone
   from the data those tests resolve against. Expect z.ai catalog churn at the
   next publish.

## Drift at last sync check (2026-07-31) — pin advanced to ab366ebe9

**Caught up to `ab366ebe9`.** Delta `c13ffe18 → ab366ebe9`: **26** first-parent
changes — **3 ports → 4 Go commits, 23 n/a, 0 decide**. **No release crossed** —
no tag in range, npm `pi-ai`/`pi-coding-agent` stay **0.83.0**,
`models.generated.ts` untouched, so the catalog and every derived byte-golden
are untouched and no regen ran. All three ports are **unreleased upstream**: the
0.83.0 build predates them, so parity was derived from TS source at each porting
sha rather than from `dist/`.

**Ports.**

- **Support streams without finish reasons** `2c3041242` (Go `3ed35dc`) — some
  OpenAI-compatible providers never emit `finish_reason`, and pi threw
  unconditionally. A new compat flag `supportsFinishReason` (default **true**)
  lets such a provider infer the stop reason instead:
  `content.some(b => b.type === "toolCall") ? "toolUse" : "stop"`. Go carries
  default-true in the `detectOpenAICompat` defaults literal (as with the sibling
  `SupportsUsageInStreaming`) so the resolved struct's zero value is never
  observed, and `*bool` in the raw-override struct gives pi's `??` semantics —
  an explicit `false` lowers it, an absent key does not. The guard ordering is
  pi's, but only in shape: in Go a `StopAborted`/`StopError` stop reason implies
  `hasFinishReason` (only `mapOpenAIFinishReason` sets them), so both are
  mutually exclusive with the `!hasFinishReason` inference gate and **only the
  `ctx.Err()` check can actually preempt inference**. The
  `generate-models.ts` hunk is the DATA half and folds into the next regen;
  confirmed harmless — `supportsFinishReason` appears **zero** times in upstream
  `models.generated.ts` at `origin/main` and never as `false` anywhere upstream,
  so the resolved default is `true` on every path today.
- **Keep signed empty text/thinking blocks in google history** `6138f5a07` (Go
  `18b324e`) — Gemini can attach a `thoughtSignature` to a part whose visible
  text is empty and requires it echoed back; dropping it breaks the reasoning
  chain and the model intermittently ends mid-task turns with a thought-only
  STOP. Signature resolution moves above the skip in the text and same-model
  thinking branches, and each skip now also requires an absent signature. pi's
  `(!block.text || block.text.trim() === "")` is deliberately **not**
  transliterated: `Text` is a non-pointer string, so there is no `undefined`
  case and `strings.TrimSpace(x) == ""` is exactly equivalent to the whole JS
  left-hand side. **Request-body surface, and it genuinely moved** — see the
  scenario census below.
- **Preserve Anthropic initial stream block content** `59ad3dead` (Go
  `3fc128f`) — `content_block_start` can already carry `text`, or `thinking` +
  `signature`; hardcoding `""` dropped the first chunk of a block.
  `anthropicStreamEvent.ContentBlock` gains `Thinking` and `Signature` beside
  the existing `Text`, and the builders are seeded from them. Non-pointer
  strings are correct: JS `?? ""` on an absent field yields `""`, which is Go's
  zero value after unmarshalling a missing key, and pi treats `null` and `""`
  identically. Seeding precedes `materialize()`, so the emitted
  `text_start`/`thinking_start` partial already carries the initial content —
  locked by a test that goes **beyond upstream's**, which does not cover that
  ordering.

**The google change is the one that moved behavior.** The porter reported "no
scenario moved"; the parity review **refuted that as stated** and the correction
is the strongest available non-vacuity evidence. Deriving pi's `convertMessages`
from the TS at `6138f5a07` under node, across all 29 `google-generative-ai`
catalog models × 7 purpose-built scenarios (203 cases): **4 of 7 scenarios moved
on 29/29 models** — signed-empty thinking (same model), signed-empty text,
invalid-base64-sig + whitespace-text-with-valid-sig, and signed whitespace-only
thinking as the sole block. In every one, new Go matches pi where old Go did
not. The two negative scenarios (unsigned empties, cross-model signed empties)
correctly did **not** move. No *in-repo* golden moved, and none should have — no
existing fixture feeds a signed-but-empty block. The distinction is worth
keeping: "no golden moved" and "no behavior moved" are different claims, and
only the first was true here.

**The cross-model thinking branch is dead code — in Go and in pi.** Both reviews
established this independently. `googleContents` iterates the output of
`transformMessages` (`transform.go:122`), which rewrites every non-same-model
`ThinkingContent` to `TextContent` and drops the empty ones before the converter
runs; google's `isSame` (provider+model) is strictly **weaker** than transform's
`isSameModel` (provider+api+model), so `isSame == false` implies no
`ThinkingContent` survives to the branch. Verified by mutation — replacing the
whole `else` body with a panic leaves `ai/providers` green. pi has the identical
dead branch behind the identical ordering (`google-shared.ts:99`), so porting it
is faithful dead-code mirroring, not a divergence. The code now says so; the
accompanying test was renamed `TestGoogleCrossModelSignaturesNeverReachRequest`
and rewritten to name the real mechanism, since it is a regression lock on the
loosened skips rather than coverage of a branch nothing reaches.

**Reviews.** Independent go-review — **ship after 2 MED + 2 LOW**, all applied
in `9d96ca2`, none behavioral: both MEDs were places where a reader would
believe unreachable code was live (the cross-model comment stating its rule as
if it ran, and the invariant test named as if it covered that branch), and the
LOWs corrected the openai ordering comment (it credited the wrong guard) and
turned the four-scenario openai test into a `t.Run` table matching the file's
other 23 uses, so one failing case no longer hides the rest. One LOW **not
taken**: hoisting a single `getOpenAICompat(model)` through
`StreamOpenAICompletions` — a real cleanup (`buildOpenAIParams` already does it)
but touching three call sites unrelated to this port. Independent adversarial
parity review — **FAITHFUL**, no fix-first divergences, with every porter claim
independently checked (the dead-branch claim confirmed on both sides, the
`TrimSpace` collapse refuted as an *exact* equivalence per F1 below, the
anthropic append-and-seed semantics confirmed, the openai ordering confirmed
1:1 and shown moot in both implementations, and the "nothing moved" claim
corrected upward as above). Both reviewers re-ran the new tests against
`7899c6e` and confirmed four are **behaviorally red** and the two negative tests
correctly stay green.

**Two pre-existing divergences surfaced, neither introduced by this cycle,
neither fixed here.**

- **F1 (LOW) — `strings.TrimSpace` is not JS `.trim()`** (`google.go:735,748,759`
  and every other `TrimSpace` on a content path). JS trims U+FEFF but not
  U+0085; Go's `unicode.IsSpace` trims U+0085 but not U+FEFF. An assistant turn
  whose text or thinking is *only* U+FEFF or *only* U+0085 diverges on the
  request body across all 29 google models. Old Go behaved identically, so this
  diff neither introduced nor worsened it. Negligible frequency; recorded rather
  than fixed, since converging the two whitespace vocabularies is a repo-wide
  question, not a google one.
- **F2 (LOW) — anthropic `tool_use` blocks are not seeded from `input`**
  (`anthropic.go:471`). pi seeds `arguments: event.content_block.input ?? {}`
  (`anthropic-messages.ts:620`); Go hardcodes `map[string]any{}` and never reads
  `ev.ContentBlock.Input` (the field is decoded and unused). A
  `content_block_start` for `tool_use` carrying non-empty `input` with no
  following `input_json_delta` loses the arguments. Upstream `59ad3dead`
  deliberately left the `tool_use` arm alone, so this port is faithful **to the
  commit** — but it is an older gap in the very switch the fix touches, and it
  is the natural follow-up if the whole "preserve initial block content" family
  is to be closed. **Recommended next cycle.**

**No new rulings.** Two n/a calls in this delta lean on existing ones and were
re-confirmed by the parity review's out-of-scope flag: `4523528b2` ("treat only
plain objects as provider error bodies") changes exactly the SDK-error-probing
layer recorded N/A on **2026-06-30** — Go issues raw HTTP requests and reads
`resp.Body`, so the AWS-SDK `$response.body` wrapper it defends against cannot
occur — and `70bbe47a9` (Bedrock structured error metadata) is an unported
provider. `ab366ebe9` itself (`ModelRegistry.complete` delegating to
`ModelRuntime`) is host restructuring, `n/a` under the **2026-07-17** ruling.
The new `packages/protocol` package (`c066d01a4` — CBOR/framing/schemas) is
consumed only by the excluded agent harness and by neither `packages/ai/src` nor
coding-agent core; it stays `n/a`, but **if a future commit makes `ai` or
coding-agent core depend on it, that is a `decide`**.

**Hunk accounting.** All three ported commits are merges, so every file list was
re-derived with `git diff <sha>^1..<sha> --stat` rather than the combined
`git show --stat` view — identical, **no smuggled hunks** (2026-07-17 lesson
clean). 2026-07-21 multi-site lesson: `google-shared.ts` serves both google APIs
and correctly collapses into Go's single `googleContents` (no vertex duplicate);
`content_block_start` has exactly one Go site; `openai_compat.go:132` is the
only `openAICompletionsCompat` literal. Justified absences: four upstream
test-compat-literal hunks (no Go test enumerates the compat struct) and two
`packages/coding-agent/docs/` hunks (the port does not mirror pi's docs).

## Drift at last sync check (2026-07-30) — pin advanced to c13ffe18

**Caught up to `c13ffe18`.** Delta `cced6a21 → c13ffe18`: **17** first-parent
changes — **3 ports → 5 Go commits, 13 n/a, 0 decide**. **Release crossed —
npm `pi-ai`/`pi-coding-agent` 0.82.1 → 0.83.0** (`845d6ff1`; no tag object was
present in the fetched refs, the release is identified by the commit), and
unlike the last four cycles the **catalog DID move**: 456,576 → 477,229 B,
1116 → 1153 models (+64/−27/91 changed), regenerated in `1190695`.

**Ports.**

- **Qwen thinking controls** `4c1a0b92` (Go `dccdc48`) — the qwen branch of the
  openai-completions params builder now emits `reasoning_effort` when
  `reasoningEffort` is set and `compat.supportsReasoningEffort` holds, mapped
  through `model.thinkingLevelMap`. The load-bearing detail is that pi uses
  **`??`** here, not the zai branch's `=== undefined`: a *present-null* map entry
  is nullish, so it falls back to the RAW level and IS emitted. Go therefore
  routes this through `effortValue`, not `mappedEffortOrRaw`. pi's follow-on
  `typeof effort === "string"` guard cannot fail after the `??` and has no Go
  counterpart. This is a **live request-body change** for 27 catalog models
  across 3 providers — `supportsReasoningEffort` is resolved as
  `model.compat.supportsReasoningEffort ?? detected.supportsReasoningEffort`,
  and qwen-token-plan/-cn/opencode-go trip none of detection's seven negating
  flags.
- **Preserve original stop reason** `d7b02636` (Go `a1cae2a`) —
  `AssistantMessage.RawStopReason` (`rawStopReason,omitempty`, positioned as in
  pi's `types.ts`) set by anthropic, openai-completions, openai-responses and
  google; anthropic's `sensitive` case gains `Provider stopped with: sensitive`,
  and google's end-of-stream error becomes `Provider stopped with: <raw>` when a
  raw reason was captured. The bedrock / google-vertex / mistral hunks are
  correctly unported — those three have no Go stream implementation and, checked
  against the 2026-07-21 missed-half lesson, **no shared Go resolver** serves
  them (`mapGoogleStopReason` is reached only from `StreamGoogle`). The
  2026-07-21 lesson applied in the *other* direction too: pi's
  `google-generative-ai.ts` and `google-vertex.ts` carry identical edits and Go
  has a single `google.go`, which correctly collapses both.
- **Preserve function arguments with empty custom payloads** `34239180` (Go
  `162c6fa`) — a streamed tool call carrying BOTH `custom` and `function` is an
  ordinary function call; some providers attach an empty `custom: {}`, and
  treating that as a grammar call discarded the streamed arguments. Modeled by
  pointer nil-ness (`{}` → non-nil ≡ JS truthy, `null` → nil ≡ falsy) and
  extracted to `openAIToolCallDelta.isGrammarCall()`.
- **Catalog regen** `845d6ff1` (Go `1190695`) — carries the **data half** of
  `4c1a0b92`, whose generator hunk first ships in the 0.83.0 build: the 9
  excluded qwen ids × 2 regions gain `supportsReasoningEffort:false`, 9
  openai-completions models gain a `thinkingLevelMap` (glm-5/5.1/5.2 and
  qwen3.8-max-preview × 2 regions, plus `opencode|ling-3.0-flash-free`), and
  `deepseek-v4-flash`/`-pro` × 2 regions flip `thinkingFormat` deepseek → qwen.
  **No schema drift** — a full key+type enumeration across both catalogs diffs
  empty, so `ai/types.go` is unchanged. All 27 removed ids confirmed
  unreferenced (the sole `gemini-3-pro-preview` hit is a synthetic
  `ai.Model{...}` literal in a test, and only the `vercel-ai-gateway` alias was
  dropped), and all 35 `defaultModelPerProvider` entries still resolve.

**Why the regen was mandatory, not cosmetic.** Shipping `4c1a0b92`'s code on the
0.82.0 data produces a state **no published pi ever had**: the parity review
crossed 0.83.0 code against 0.82.0 data and confirmed it emits
`reasoning_effort` to `deepseek-v3.2`, `kimi-k2.5/2.6/2.7-code`, `qwen3.6-*`,
`qwen3.7-*` and `MiniMax-M2.5` — *precisely* the models the upstream fix was
written to exclude. Real 0.83.0 omits the key.

**One pre-existing divergence fixed** (Go `b293a70`, found by the parity
re-gate, not introduced by this delta): `coding/resolve.go` carried 35 of pi's
38 `defaultModelPerProvider` entries. The two missing qwen-token-plan entries
were load-bearing — a custom model id under those providers fell through to
`providerModels[0]` (MiniMax-M2.5 after the sort) and cloned its
196608/32768 limits instead of qwen3.7-max's 1000000/131072, changing the
emitted `max_tokens` and the context clamp. `radius → auto` is inert (absent
from the catalog) but carried for faithfulness.

**`bff5ab71` was triaged `port` and downgraded to `n/a` during porting** — see
the 2026-07-30 ruling above.

**Reviews.** Independent go-review (**ship**; 2 MED + 4 LOW, all applied in
`6ade7a1`): the `response.failed` clear-then-set was live but unpinned
(deleting the line left the suite green while a failed response would retain a
stale `"completed"`), and the grammar-call predicate was duplicated across two
call sites. It also validated two porter judgment calls — omitting pi's
`typeof` guard is right because Go's type model eliminates it rather than
hiding a gap, and the unreachable google fallback should stay but was
mis-commented. Independent adversarial parity review across two passes
(**FAITHFUL**): first pass 6/6 on the §3 request diff but raised the catalog
**HIGH** above; the re-gate after the regen and the review fixes ran the
request diff at **8310/8310** (554 openai-completions models × 15 scenarios,
0 mismatches, 87 case-bodies genuinely moved by the regen and all 87
reproduced), endpoint-pinned the catalog at **both** ends (0.82.0 MODELS ≡ the
catalog at `162c6fa`; 0.83.0 MODELS ≡ HEAD), and verified the `name()` fix
17/17 against the real dist. It surfaced the `defaultModelPerProvider` MEDIUM
and one LOW: a comment in `ai/providers/openai.go` misdescribed pi's Cloudflare
`{VAR}` handling (pi resolves placeholders in its `cloudflareStreams` wrapper,
not `createClient`, and **never throws** on an unset var — our fail-fast is the
documented 2026-06-24 divergence). Both fixed in `b293a70`.

**One new latent behavior fix rode in with the review pass**:
`openAIToolCallDelta.name()` now uses `*string` so an explicit empty
`function.name` no longer falls through to `custom.name` — pi's `??` does not
fall through on `""`. Pre-existing, but `34239180` makes both-payloads-present a
first-class case, so it was newly reachable.

gofmt/build/vet/`-race` green; in-repo differential request-body scenarios
38/38; live pi-vs-Go request diff 8310/8310.

## Drift at last sync check (2026-07-29) — pin advanced to cced6a21

**Caught up to `cced6a21`.** Delta `47ca25fc → cced6a21`: **9** first-parent
main-line changes — **2 ports → 3 Go commits, 7 n/a, 0 decides**. **No release
crossed** — no tag in range, npm `pi-ai`/`pi-coding-agent` stay **0.82.1**,
`models.generated.ts` and `src/data` are unchanged, so the catalog and every
byte-golden are **untouched** (no regen). Reviewed via independent go-review
(**fix-first → ship**; 1 HIGH + 1 MED + LOWs applied) + independent adversarial
parity review (**fix-first → FAITHFUL**; the same HIGH found independently, plus
two MEDs and three test-coverage gaps). gofmt clean; build/vet/`-race` green;
in-repo differential request-body scenarios **38/38**. Whole-range
`packages/ai/src` + `packages/agent/src` + `packages/coding-agent/src/core`
sweep (14 files) reconciles exactly against the per-commit verdicts — **no
merge-smuggled hunks**.

**No request-building code changed this cycle.** `ai/providers/openai*.go` have
a zero-byte diff; the ports change only *which client* performs a request, add a
pre-request guard, and touch context-file discovery. The differential harness's
external scratch dirs (`/tmp/pi-diff`, `/tmp/diffreq2`) are gone, but
regenerating them would arbitrate nothing here — the in-repo differential
scenarios cover the request bodies and are unperturbed at 38/38.

Ports:
- **per-request fetch injection** (`027a5847`, Go `6f89b12` + review-fix
  `6af8469`): pi adds `StreamOptions.fetch` so a host can supply its own HTTP
  layer per request. The Go analog is `ai.HTTPDoer` (a one-method `Do` interface
  that `*http.Client` satisfies) on `StreamOptions`, threaded through
  `sendWithRetry` — which covers anthropic-messages, openai-completions and
  openai-responses, all of which issue their requests there — and through
  pi-messages, which calls out directly and so keeps `http.DefaultClient`, not
  the retry loop's `sharedClient`, as its default (that is exactly what the line
  did before the change). google-generative-ai **rejects** an override with pi's
  byte-exact `Custom fetch is not supported by the Google Generative AI adapter`,
  because `@google/genai` cannot take a custom fetch; the guard sits before the
  api-key check as it does upstream, so a request carrying both faults reports
  the fetch error. `http.DefaultClient` is the Go stand-in for the
  `globalThis.fetch` that pi's `options.fetch !== globalThis.fetch` guard blesses
  as equivalent to unset, and after review that meaning is enforced in ONE place
  (`customHTTPClient`) for all three call sites — the first cut had the google
  guard treating it as "not an override" while `retryFromOptions` copied it
  verbatim and silently displaced `sharedClient`. Not ported: the
  azure-openai-responses, google-vertex, mistral-conversations,
  openai-codex-responses and openrouter-images hunks (non-ported providers /
  no images surface), and `ImagesOptions.fetch` for the same reason.
  `simple-options.ts`'s passthrough needs no counterpart — Go's
  `SimpleStreamOptions` embeds `StreamOptions` and every consumer copies it
  wholesale. **Request bodies unchanged**; no golden surface.
- **stop loading AGENTS.md twice in nested git worktrees** (`cced6a21`, #7221,
  Go `eae51bd`): a linked worktree nested inside its main repo
  (`main/worktrees/feat`) inherits the main repo's AGENTS.md through the
  ancestor walk while carrying its own copy of the same tracked file, so the
  content was loaded twice. `LoadProjectContextFiles` now skips the main repo's
  copy when the worktree root has a context file of the **same name**. pi
  exports `findGitPaths` from `footer-data-provider.ts` for this; that consumer
  is a TUI footer with no Go counterpart, so the walker is written in
  `coding/resources.go` — `glob.go`'s `findRepoRoot` is **not** reusable, since
  it only `Lstat`s `.git` and never resolves the `gitdir:`/`commondir` chain.
  Only `repoDir` and `commonGitDir` are carried; pi's `headPath` serves the
  footer's HEAD watcher alone. All nine upstream scenarios are ported, including
  the negatives that keep the check narrow — bare layouts (`proj/.bare` +
  `proj/main`), sibling worktrees, submodules, ordinary repos, and a dangling
  `gitdir:` target. **System-prompt-adjacent but no golden moves**: the
  `<project_instructions>` blocks derive from these files, and no golden fixture
  uses a worktree layout.

**Deliberate divergence recorded** (new this cycle): an injected HTTP client
bypasses `sharedClient`'s `ResponseHeaderTimeout`, so `StreamOptions.TimeoutMs`
does not apply to it. pi keeps its timeout under an injected fetch because its
SDKs apply the timeout *outside* fetch (`{ timeout: options.timeoutMs }` on the
client), whereas Go expresses it as a transport setting on the client being
replaced. Reproducing it would mean building bespoke header-timeout machinery pi
does not have, on a path where the caller already owns the transport; the
divergence is documented on the exported `StreamOptions.HTTPClient` field
instead. Unset and `http.DefaultClient` are unaffected — they keep
`sharedClient` and its cap.

**Review findings, all fixed in `6af8469`:** both reviewers independently found
the same **HIGH** — the injected client never reached the providers from the
agent SDK. Upstream this is free: `AgentLoopConfig extends SimpleStreamOptions`
and `agent-loop.ts` spreads the whole config into the stream call. The Go loop
forwards `StreamOptions` field-by-field, under a comment stating that every
field must be listed, and this one was not — so the ported feature was reachable
only by calling `providers.Stream*` directly. Threaded through
`AgentOptions`/`Agent`/`AgentLoopConfig` and test-locked; `coding/session.go` is
deliberately untouched, since pi's coding-agent session options do not extend
`SimpleStreamOptions`. The **MED**s were the split definition of "no override"
(above) and a wasted full read of a context file to take its basename
(`contextFileNameInDir`). A parity **LOW** corrected `findGitPaths` to keep
climbing on any stat error, matching pi's `existsSync` guard rather than
aborting the walk. Three test gaps closed: openai-responses had no
injected-client coverage (the provider tests are now table-driven over all
four), the shared-client fallback assertion never exercised `sendWithRetry`, and
**no worktree case pinned canonicalization** — every scenario built its
`gitdir:` target from the same uncanonicalized temp path, so removing all four
`canonicalizePath` calls left them green (the same hole exists in pi's tests).
A symlinked-cwd case now covers it, mutation-verified to be the only test that
catches it.

The re-review of the fix commit found **two more LOWs, both closed**. The
`contextFileNameInDir` polish (avoiding a full read to take a basename) used
`fileExists`, which accepts a mode-000 candidate that `loadContextFileFromDir`
— and pi's `readFileSync`-and-catch loop — skip in favour of the next name. A
worktree holding an unreadable `AGENTS.md` beside a readable `CLAUDE.md` would
have shadowed `main/AGENTS.md` and dropped its content entirely, where pi
shadows `main/CLAUDE.md`. The polish was reverted to keep ONE
candidate-selection rule, and the case is now pinned. Separately, the
`http.DefaultClient` normalization was correct but unpinned — reverting
`retryFromOptions` to the pre-fix line left the entire `ai/providers` suite
green — so it now has a direct assertion. Both mutation-verified
behavioral-red, bringing this cycle to **twelve** verified mutations.

**n/a (7):** `fdbedcad` harness design-doc refs (`packages/agent/docs/harness.md`
— docs, and the harness tree stays on the non-port list; the 2026-07-28 boundary
note still stands) · `0c32e83a` streaming usage for the llama.cpp provider
(#7258 — `coding-agent/src/extensions/llama/`, extensions runtime) ·
`e9e86e1c` + `a5db0e4f` + `7796481e` contributor approvals (`.github/`) ·
`fb4ecd63` shorten image fallback paths and clamp width (#7262 — `packages/tui/`)
· `f9476a61` TypeBox nullable array validation (#7243).

**`f9476a61` checked, not dispatched from the diffstat.** It is a `typebox`
1.1.38 → 1.3.7 bump plus one test: TypeBox's *generated* validator rejected
`null` for `{type:["array","null"], items:{…}}`. Go's validator is independent
and the bug is structurally absent — `matchesJSONType` returns true for `"null"`
on a nil value, and the items/minItems/maxItems checks live under the
`case []any:` arm of a type switch that nil never enters. No Go change, no test
obligation.

## Drift at last sync check (2026-07-28) — pin advanced to 47ca25fc

**Caught up to `47ca25fc`.** Delta `2efa728d → 47ca25fc`: **17** first-parent
main-line changes — **2 ports → 3 Go commits, 15 n/a, 0 decides**. **No release
crossed** — no tag in range, npm `pi-ai`/`pi-coding-agent` stay **0.82.1**,
`models.generated.ts` and `src/data` are unchanged, so the catalog and every
byte-golden are **untouched** (no regen). Reviewed via independent go-review
(**ship**; 1 MED + LOWs applied) + independent adversarial parity review
(**FAITHFUL**, no divergences; one coverage gap closed). gofmt clean;
build/vet/`-race` green; in-repo differential request-body scenarios **38/38**.
Whole-range `packages/ai/src` + `packages/agent/src` +
`packages/coding-agent/src/core|main.ts|sdk` sweep (11 files) reconciles exactly
against the per-commit verdicts — **no merge-smuggled hunks**; both merges in the
range are clean (`a597371b` is `packages/evals` only, `c820aa26` adds only
`packages/agent/docs/harness.md`).

**Both ports are unreleased upstream** — the shipped 0.82.1 build predates them,
so as in the 2026-07-23 cycle the live pi-vs-go request-diff harness cannot
arbitrate this cycle's request-body change. Both were instead derived from the TS
source at the porting shas, with pi's `detectCompat` extracted and **executed
under node** by the parity review across 17 provider/URL routes and compared
field-by-field against Go's `detectOpenAICompat` (17 cases, 0 mismatches).

Ports:
- **minimum OAuth token validity on resolve** (`99e34013`, #7168, Go `30c80c9`
  + review-fix `e073881`): `AuthResolutionOverrides` gains
  `MinOAuthValidityMs *int64` and resolution gains a default five-minute
  validity window — a stored OAuth credential expiring *within* the window now
  refreshes, where previously only a fully expired one did, on both the
  optimistic pre-lock check and the under-lock re-check. The effective window is
  `max(default, override)`, so an override can only widen it. The asymmetry is
  the subtle part and is ported exactly: the default window triggers a refresh
  but imposes **no** post-refresh contract, whereas a *set* override (pi's
  `!== undefined`, so an explicit **zero** counts) additionally fails with pi's
  byte-exact `OAuth refresh returned a token that expires too soon for
  <provider>` when the refreshed token still does not clear the window. Only
  `packages/ai/src/auth/resolve.ts` is in scope, under the 2026-06-23 ruling
  naming it the boundary edge; the commit's CLI half (`cli/args.ts`, the new
  `cli/credential-print.ts`, `main.ts`) and `core/model-runtime.ts` are
  host-side and stay unported. `ModelRuntimeAuthOverrides` was checked against
  the 2026-07-21 lesson and carries **no Go obligation** — there is no
  `ModelRuntime` type in the port, and the field it forwards to is the ported
  `ai.AuthResolutionOverrides`, already reachable from the exported
  `Models.GetAuth`/`GetProviderAuth`. No golden surface. Latent in practice (no
  Go provider has OAuth wired) but exported-API-reachable.
- **send max_tokens for Z.AI providers** (`2fe21b40`, #7174, Go `5e0b483`):
  `isZai` joins the openai-completions `useMaxTokens` disjunction, so Z.AI models
  emit `max_tokens` instead of `max_completion_tokens`. **Request-body surface.**
  Go's `isZai` was confirmed byte-equivalent to pi's (same four disjuncts, same
  order). The existing differential scenario `TestDiffDetectZai` (glm-4.6 /
  provider `zai` / `api.z.ai`) pinned the OLD field and had to move; per the
  repo's hard rule the new expectation was **re-derived from pi**, not bent to
  our output — pi's `detectCompat` at `2fe21b40` was run under node on that exact
  scenario and yields `maxTokensField:"max_tokens"`. A side-effect sweep across
  17 routes confirms **only** `maxTokensField` changes, and only on the four zai
  routes. The identical `scripts/generate-models.ts` hunk is **generator-only**
  and folds into the next catalog regen; no regen this cycle, and it is currently
  a no-op — catalog zai entries carry no `maxTokensField`, so `getOpenAICompat`
  falls back to detection and today's behavior already equals post-regen
  behavior. Ledger note for that regen: zai entries will start carrying an
  explicit `maxTokensField:"max_tokens"` — same value, no behavior change.

**Upstream inconsistency recorded** (pi-side, not ours): `2fe21b40`'s own new
test asserts `model.compat?.maxTokensField === "max_tokens"` for catalog models
`zai/glm-5.1` and `glm-5.2`, but the commit did not regenerate
`models.generated.ts` — `providers/zai.models.ts` at that sha has zero
`maxTokensField` entries, so that assertion cannot pass at the porting sha. The
port correctly declined to mirror it and pins the *effective* compat plus the
request body instead.

**n/a (15):** `a597371b` coding-agent evals merge (#7117 — `packages/evals`
only) · `c820aa26` durable-agent-harness design doc (`packages/agent/docs/harness.md`,
2132 lines, docs-only — see the boundary note below) · `2903063d` contributor
approvals (`.github/`) · `04b15259` expose `ctx.scopedModels` to extensions
(#7191 — extensions runtime; the one `agent-session.ts` line is the extension-
context getter) · `b6fb91e5` scoped models in TUI extension context (#7215 —
`modes/interactive/`) · `b63403a5` prefer configured Bedrock profile over ambient
AWS keys (#7176 — `api/bedrock-converse-stream.ts` only; Bedrock is a deliberate
non-ported provider, and unlike the 2026-07-21 case this touches **no** shared
helper) · `063fb963` isolate autoload-disabled package test from real home
(#7167 — pi-internal test) · `66eead65` preserve resource metadata after
extension resource reloads (#7218 — `core/resource-loader.ts`; **no Go analog**,
`coding/resources.go` has no extension/source-info surface) · `5d548ae9` rpc bash
no longer bypasses user_bash (#7214 — `modes/rpc/`) · `f1ea6c0d` model-selector
filter resets selection (#7211 — TUI) · `0563a7c0` clean up failed git installs
(#7210 — `core/package-manager.ts`, extension packaging) · `2fe21b40`'s generator
hunk (folded above) · `cefa40ed` guard tree navigation during responses (#7022,
WIP PoC — the `agent-session-runtime.ts` `abort()` hunk is on the explicit
non-port list, and the `agent-session.ts` `isStreaming` throw guards a host
tree-navigation flow with **no Go counterpart**: `coding/session_tree.go` exposes
only read-only `Branch`/`BuildContext`) · `f1451955` + `47ca25fc` a one-line test
fix and its revert (net zero) · `0d008b74` show tool expansion status (TUI).

**Boundary note (not a `decide`, yet):** `c820aa26` merges a 2132-line
`packages/agent/docs/harness.md` design doc for a **durable agent harness**. The
harness tree (`packages/agent/src/harness/`) is on the deliberate non-port list
and this cycle adds only documentation, so nothing changes today. Flagged because
it signals the boundary may need re-visiting when code lands behind it —
re-judge at that point rather than now.

## Drift at last sync check (2026-07-27) — pin advanced to 2efa728d

**Caught up to `2efa728d`.** Delta `5bc1c2c0 → 2efa728d`: **9** first-parent
main-line changes — **1 port → 1 Go commit, 8 n/a, 0 decides**. **No release
crossed** — npm `pi-ai`/`pi-coding-agent` stay **0.82.1**, `models.generated.ts`
is unchanged, so the catalog and every byte-golden are **untouched** (no regen).
Reviewed via independent go-review (**ship**, 2 LOW applied) + independent
adversarial parity review (**FAITHFUL**, no divergences; four vacuous/absent
coverage gaps closed with mutation-verified tests). gofmt clean; build/vet/`-race`
green; in-repo differential request-body scenarios 14/14. All 9 changes are
single-parent squashes (no merges); whole-range `packages/ai/src` +
`packages/agent/src` + `packages/coding-agent/src/core` sweep (16 files)
reconciles against the verdicts.

Port:
- **expose pending stop reason while streaming** (`f9a49869`, #7151, Go
  `3643c91`): new `ai.StopPending StopReason = "pending"`. Every ported provider
  (anthropic, openai-completions, openai-responses+shared, google, pi-messages,
  faux) initializes its streaming partial to pending instead of stop, so
  consumers watching partial events see the terminal reason only once it is
  resolved; a stream that ends still-pending now **fails** with pi's exact
  per-provider error string ("Anthropic stream ended without a stop reason",
  "OpenAI Responses stream ended without a stop reason", "Google stream ended
  without a **finish** reason", openai-completions folds `pending` into its
  `!hasFinishReason` throw, "Faux response ended without a stop reason").
  openai-responses additionally resolves pending→stop via
  `applyMessagePhaseStopReason` when a message item reaches phase `final_answer`,
  called at both the createSlot (output_item.added) and output_item.done sites; a
  terminal response event still overrides that provisional stop with its real
  reason. `"pending"` is streaming-only and never persists to session JSONL, so
  no golden moves (parity-confirmed). The upstream `packages/agent/src/proxy.ts`
  streamProxy partial-init hunk is **subsumed** — Go has no streamProxy analog;
  the agent loop consumes provider `event.Partial` values (the only non-provider
  `AssistantMessage` construction, `agent/agent.go:413`, is a synthetic failure
  message correctly built with StopError/StopAborted, independently verified).
  The same `stop→pending` init also lands in five upstream providers with **no
  Go home** (azure-openai-responses, bedrock-converse-stream, google-vertex,
  mistral-conversations, openai-codex-responses) — correctly out of scope.

**n/a (8):** `cee5ff75` README openclaw ref (docs) · `3cd39163`/`5f9d025e`
contributor approvals (`.github/`) · `f08f58f5` run coding-agent tests offline
(#7031 — pi-internal test infra: `coding-agent/test/*` + `vitest.config.ts`, no
ported surface) · `60f6a803` GitHub Copilot Claude Opus 5 support (#7158 —
**generator-only** `scripts/generate-models.ts`: adds `claude-opus-5` to the
Copilot extended-context set + `{minimal:"low"}` thinking override; no
`models.generated.ts` delta, folds into the next catalog regen) · `61da9e2f`
OpenRouter OAuth manual-redirect fallback (#7114 — `auth/oauth/openrouter.ts`,
OAuth token acquisition, deliberate non-port) · `c2275d67` duplicate messages on
startup session switch (#7110 — `modes/interactive/`, TUI/interactive, excluded)
· `2efa728d` concurrent user bash cancellation (#7103 — `core/agent-session.ts`
turns the single `_bashAbortController` into a `Set` for `abortBash()`/
`isBashRunning`; agent-session-runtime interactive-cancellation state, excluded,
**confirmed no Go analog**).

## Drift at last sync check (2026-07-26) — pin advanced to 5bc1c2c0

**Caught up to `5bc1c2c0`.** Delta `7df73a00 → 5bc1c2c0`: **19** first-parent
main-line changes — **3 ports → 4 Go commits, 15 n/a, 0 decides**. **Release
crossed — npm 0.82.0 → 0.82.1, one tag** v0.82.1 `b4f29368`, but
`models.generated.ts` is unchanged across the whole range, so the catalog and
every byte-golden are **untouched** (no regen). Reviewed via independent
go-review (**ship + follow-up**) + independent adversarial parity review
(**fix-first → fixed**: one confirmed MED precedence divergence). gofmt clean;
build/vet/`-race` green. Whole-range `packages/ai/src` + `packages/agent/src` +
`packages/coding-agent/src/core` sweep (12 files) reconciles against the
verdicts; all 19 changes are single-parent squashes (no merge-smuggled hunks).

Ports (chronological):
- **ANTHROPIC_AUTH_TOKEN bearer auth** (`24e5cc04`, #6148, Go `8489218` +
  review-fix `7ebae86`): `ANTHROPIC_AUTH_TOKEN` joins env discovery/status ahead
  of `ANTHROPIC_OAUTH_TOKEN`/`ANTHROPIC_API_KEY`, but is a bearer credential sent
  as `Authorization: Bearer` (never `x-api-key`). `24e5cc04` replaces anthropic's
  generic `envApiKeyAuth` with a custom `anthropicApiKeyAuth` whose resolve order
  is **credential.key → ANTHROPIC_AUTH_TOKEN (header) → oauth/api-key env**. The
  first pass missed the custom-resolver swap (only touched the env list + a
  provider re-read), inverting precedence — the env token overrode an explicit
  key and the facade `GetAuth` surfaced it as an api key. Both reviews caught it;
  `7ebae86` ports the custom resolver (`ModelAuth.Headers` on the facade),
  leaves `APIKey` empty on the compat path when the token is active, and gates
  the provider re-read on `apiKey == ""`. No golden touched (the path only fires
  when the env var is set; CI env is clean).
- **`ModelsStoreEntry.etag`** (`b1c444d9`, Go `853e88c`): additive-latent field;
  the ETag-revalidation consumer is the unported host remote-catalog provider.
- **ModelsError keeps the underlying cause** (`4cf0a729`, Go `18fc5e2`):
  **already-faithful** — Go's `ModelsError.Error()` already composes
  code+message+cause. Test-only lock, no logic change.

**n/a (15):** `a9f5b1c1` Radius OAuth gateway routing (Radius OAuth out —
2026-07-14 ruling) · `7304d8f8`/`8eef62ed` test-only model updates · `921c3543`
opus-5 `generate-models.ts` (generator; folds into a future regen, no
`models.generated` delta) · `a3ee1d28` scoped-models diagnostic codes (host
`model-resolver` + interactive TUI; no Go analog) · `440689df` contributor
approval · `eafe11fb`/`6173017a` vitest evals package · `af3b934f` Claude Opus 5
on **Bedrock** (bedrock provider excluded; its `error-body.ts` hunk guards the
SDK `$response.body` stream object — **no Go analog**, Go reads the raw HTTP
body, per the 2026-06-30 ruling) · `2a2b0a39` llama.cpp catalog cache
(extensions) · `518855dd` output padding for custom renderers (extensions/TUI) ·
`b711e266` issue-analysis CI workflow · `58c0bc2f` resource-loader
exclude-directories (**already-faithful** — Go's `os.ReadFile` errors on a
directory, so the candidate is already skipped) · `62a70741` changelog audit ·
`5bc1c2c0` CHANGELOG unreleased section.

## Drift at last sync check (2026-07-24) — pin advanced to 7df73a00

**Caught up to `7df73a00`.** Delta `34f3719a → 7df73a00`: **21** first-parent
main-line changes — **4 ports → 5 Go commits, 17 n/a, 0 decides**. **Release
crossed — npm 0.81.1 → 0.82.0, one tag** v0.82.0 `083e6162`; catalog re-derived
byte-identical **456,576 B**, `cmp`-clean and endpoint-pinned at both ends.
Reviewed via independent go-review (**ship**, one MED fixed in-cycle) +
independent adversarial parity review (**fix-first → fixed**: three real
divergences, including a **missed hunk**). gofmt clean; build/vet/`-race` green;
differential parity **38/38**; whole-range `packages/ai/src` +
`packages/agent/src` + `coding-agent/src/core` sweep reconciles file-by-file
(26 files) and commit-by-commit (21 commits) against the verdicts, with the one
merge read in full.

Ports (chronological):
- **make provider retries abortable** (`7af8533c`, #6980, Go `352416c`): pi added
  `utils/provider-retry.ts` because the OpenAI/Anthropic JS SDK retry timers
  ignore `AbortSignal`. Go writes its own HTTP and `sendWithRetry` was already
  abortable, so the loop was deliberately NOT restructured; only the genuine
  behavioral deltas were ported — oversized server delay now **fails fast**
  (was: silently ignored), the honoring gate matches pi (no `> 0` floor, `==`
  the limit honored), `maxRetryDelayMs` no longer caps the exponential backoff,
  an unparseable `Retry-After` retries immediately, and `parseFloatPrefix`
  reproduces JS `parseFloat` prefix semantics.
- **constrained sampling** (`24bace27`, #6341, Go `ddd2661` + `f9364ef` +
  `1758b71`): new SDK surface `ai.Tool.ConstrainedSampling` (json_schema
  strict prefer/require, or grammar with openai_lark/openai_regex variants) plus
  `ai/providers/constrained_sampling.go`. **Request-body golden on all four
  ported APIs.**
- **explicit prompt-cache mode** (`241431c6`, #6618, Go `ddd2661`): compat
  `supportsExplicitPromptCacheMode` gates
  `prompt_cache_options:{mode:"explicit"}` when cacheRetention is none.
  **Request-body golden.**
- **catalog regen at npm 0.82.0** (`083e6162`, Go `c3d3fc4`).

**Missed hunk caught by the parity review + reconciliation sweep (fixed in
`1758b71`)**: `24bace27` also threads `constrainedSampling` through
`core/tools/tool-definition-wrapper.ts`. In TypeScript that reaches `AgentTool`
for free via `AgentTool<T> extends Tool<T>`, so the upstream diff shows only two
wrapper lines. Go has no inheritance: `agent.AgentTool` had no such field and
`asAITool()` — the only path from a tool to the provider request — dropped it.
Constrained sampling was therefore fully implemented in the `ai` layer and
**unreachable from `agent/` and `coding/`**, where every built-in tool lives.
Latent (no upstream tool sets it yet) but the catalog side is live. This is the
same shape as the 2026-07-21 `1942b260` lesson: one upstream fix, a
differently-structured Go file, and the hunk evaporates.

**Two silent request-body changes** rode in with `24bace27`, both confirmed
against the shipped build and test-locked:
1. openai-responses compat `supportsStrictMode` now defaults **false**, so
   `strict` disappears for the **53 of 99** catalog responses models lacking the
   flag (github-copilot, cloudflare-ai-gateway, opencode, azure). The **46**
   under the `openai` provider set it via the generator's
   `applyStrictToolCompatMetadata`, so their bytes are unchanged. One existing
   expectation moved (`TestResponsesToolsStrictFalse` →
   `TestResponsesToolsStrictOnlyWhenSupported`); the parity review independently
   reproduced BOTH halves from the real build and confirmed the new expectation
   is pi-derived, not bent toward our output.
2. function-call replay drops item ids not starting with `fc_`.

**Other parity-review catches (fixed in `1758b71`)**: `jsNumber` used 1e-7 as
JS's fixed-vs-exponential threshold when it is **1e-6**, so every value in
`[1e-7, 1e-6)` rendered wrong (pi `9.9e-7`, Go `0.00000099`) — the `1e-8` test
case passing masked it; and `parseFloatPrefix` **rejected the `Infinity`
literal**, which `parseFloat` accepts (case-sensitively, as a prefix), inverting
the outcome for `Retry-After: Infinity` — pi fails fast, Go retried immediately.
An existing test expectation asserted the wrong behavior there and was
corrected; it pinned an assumption, never pi.

**Go-review catch (MED, fixed in `f9364ef`)**: `ConstrainedSamplingConfig.Type`
and `.Strict` were untyped strings, so a typo'd discriminant was accepted
silently and `MarshalJSON`'s default branch **rewrote it to `false`** — lossily
dropping the caller's request in both directions. Now typed, with an
unrecognized value an error on marshal and unmarshal. The same commit removed a
third `json.Unmarshal` per SSE event (`responsesItem.Input` is now a `*string`,
making absent-vs-empty a type fact) — that change briefly introduced a
`*string`-in-`any`-map bug that the whole suite stayed green through, because
nothing covered a `custom_tool_call` seeded with a non-empty input; two tests
with pi-captured expectations now close that gap.

Deliberate divergences added this cycle:
1. **Fail-fast is scoped per provider** (`retryConfig.providerError`). pi wraps
   only anthropic-messages / openai-completions / openai-responses with
   `retryProviderRequest`; its Google provider streams via `@google/genai` and
   was untouched by `7af8533c` (the parity review confirmed pi's Google provider
   has no retry code at all). Go's `sendWithRetry` is shared by all four, so
   Google gets a nil renderer and keeps the pre-change backoff fallback rather
   than inheriting a fail-fast behavior pi does not have.
2. **`anthropicSDKErrorMessage`** — the two SDKs share a byte-identical
   `makeMessage` but differ in `APIError.generate`: openai unwraps
   `errorResponse["error"]`, anthropic passes the WHOLE body. The anthropic
   shape needs order-preserving, JS-escaping-compatible stringify (`jsStringify`),
   since `encoding/json` sorts keys and escapes `<>&` differently. Two accepted
   approximations, both unreachable for conformant bodies: duplicate object keys
   (JS keeps the last, Go keeps every occurrence) and lone surrogates already
   destroyed by `encoding/json` before `jsEscape` runs.
3. **Request-body key ORDER is not reproduced** — newly *documented*, not new.
   Go builds bodies as `map[string]any` and `encoding/json` sorts keys, where pi
   uses insertion order. Pre-existing and repo-wide; unpinned by any test because
   the differential suite compares parsed maps, and semantically irrelevant to
   providers.

**Commit-structure note**: this cycle does NOT hold to one-Go-commit-per-upstream
change. The two porters ran in parallel, which entangled `24bace27` and
`241431c6` inside the same structures (the responses compat struct gains fields
from both adjacently), so no hunk boundary separates them and a three-way split
would have required re-porting. The retry port was isolated. Porting
sequentially, as the skill intends, would have avoided this.

**Correction to an earlier claim in this ledger's working notes**: a catalog type
mismatch does NOT silently abort the load — `ai/catalog.go:20-24` panics with
`embedded models_catalog.json is corrupt`.

n/a (17): `e66e3e58` test.sh · `fc85bdd8` bash_execution_update events
(coding-agent `AgentSession.executeBash`, the user-typed `!command` path; Go has
no `ExecuteBash` and no `AgentSessionEvent` channel — grep-verified) ·
`ab55c15f` **merge** #7016 (expands to exactly one commit `bc3d3647`, schema
version 2→3 + `.manifest.json` `generatedAt`, replacing a filesystem-mtime
freshness gate; Go embeds one flat catalog via `//go:embed` and has no analog on
either side — no smuggled in-scope hunks) · `65ff8e7f` oauth lint (deletes one
trailing space) · `37bedb7f` TUI · `24900814`/`edbf941d` repo meta ·
`6abb4b06` llama extension · `ec1a87e8` protobufjs pin · `2d3f5554` clipboard ·
`ee0dca45`/`8472c486`/`e1617206`/`7df73a00` changelogs · `fc8048d2` build
scripts · `f8746813` model-picker reload (host `ModelRegistry`/`ModelRuntime`;
Go has no model picker and no `models.json` loader) · `e8d97fee` image models
(Go carries no generated image-models artifact at all).

## Drift at last sync check (2026-07-23) — pin advanced to 34f3719a

**Caught up to `34f3719a`.** Delta `a5afc3f1 → 34f3719a`: **15** first-parent
main-line changes — **4 ports → 6 Go commits, 11 n/a, 0 decides**. **No release
tag crossed** — zero `package.json` version bumps in the range, so `pi-ai` and
`pi-coding-agent` both stay **0.81.1**, the catalog byte-golden is untouched,
and no reference-build refresh was needed. Reviewed via independent go-review
(**fix-first → ship**: caught a HIGH data race the ports introduced, see below)
+ independent adversarial parity review (**faithful**; all four ports match
their real upstream diffs, no missed hunk with a Go home). gofmt clean;
build/vet/`-race` green; differential parity **38/38** (37 + the new
tool-result cache scenario); whole-range `packages/ai/src` + `packages/agent/src`
+ `coding-agent/src/core` sweep reconciles against the per-commit verdicts, both
merges read in full.

Ports (chronological):
- **retry DNS transport failures** (`33e40c3e`, #6946/#6904, Go `d265557`):
  `getaddrinfo`, `ENOTFOUND`, `EAI_AGAIN` appended to the retryable
  provider-error patterns in `ai/retry_classify.go`, at pi's source position
  (after `fetch failed`, before `upstream.?connect`). Ported under the
  2026-06-25 ruling. Four cases, mutation-verified.
- **cache OpenRouter tool results** (`bc41f612`, #6940, Go `1c08770`): role
  `tool` joins user/assistant in the Anthropic-style cache_control conversation
  scan, so a turn ending on a tool result caches through the tool message
  instead of falling back to the preceding user text. **Request-body golden.**
  pi's two role-check hunks collapse into Go's single loop in
  `applyAnthropicCacheControl` — the parity review confirmed pi's
  `addCacheControlToMessage` has exactly one call site, inside the loop the
  first hunk already gates, so the checks are redundant and nothing was missed.
- **isolate summarization requests** (`9b3a2059`, Go `5c6f641`):
  `coding/compaction.go` `completeSummarization` now sends
  `CacheRetention: none` + a fresh `uuidv7()` session id. pi spreads existing
  options and overrides; Go builds fresh ones, and the parity review confirmed
  pi's `createSummarizationOptions` carried neither field before, so the
  before→after delta is identical. Explicit `CacheNone` beats the
  `PI_CACHE_RETENTION` default path (`resolveCacheRetention` returns early).
- **expose session metadata to bash tools** (`bb3d7d39`, #6967, Go `acb4077` +
  `be304ae` + `9f7cac5`): bash exports `PI_SESSION_ID`, `PI_SESSION_FILE`,
  `PI_PROVIDER`, `PI_MODEL`, `PI_REASONING_LEVEL`; the five keys are stripped
  from the inherited environment unconditionally (pi's `delete`), so a parent's
  stale values never reach a child. Plus a new bash `promptGuidelines` entry and
  `environment variables (docs/environment-variables.md)` on the system prompt's
  pi-docs line — **system-prompt golden twice over**. This change is
  **unreleased upstream**, so the golden could NOT be regenerated from the
  0.81.1 build; the guideline's placement was derived from pi's TS assembly
  (`_rebuildSystemPrompt` collects guidelines in active-tool order
  read/bash/edit/write ⇒ between read's and edit's) and independently re-derived
  by the parity review, which also byte-compared both strings. The golden
  changed in exactly the two lines upstream changed.

**Go-review catch (HIGH, fixed in `be304ae` + `9f7cac5`)**: the bb3d7d39 port
made session metadata *live* — read per bash execution — but `bashSessionEnv`
read `s.Model` and `s.Recorder` as plain fields from the tool-execution
goroutine while `SetModel`/`Record` write them from the main loop. Reproduced
under `-race` by the reviewer. `cmd/pi` never triggers it (it handles `/model`
between runs, never during one), but this is a library. Fixed by reading the
model through the agent's mutex-guarded `State()` — where it is also the
authoritative value, `Session.Model` being a shadow copy on this path — and the
recorder through a new unexported `recMu`/`recorder()` accessor used by every
read. Regression test runs bash concurrently with `SetModel`/`Record`/
`SetThinkingLevel`, mutation-verified to report DATA RACE against the pre-fix
code.

New deliberate divergences (both structural, both unobservable through
`cmd/pi`, both surfaced by the parity review):
1. **`PI_SESSION_ID` is absent without a `SessionRecorder`.** pi always has a
   session-manager id; Go's closest analog is the recorder, so an SDK embedder
   that never calls `Record` gets no id. `SessionOptions.SessionID` is a
   *different* value (provider routing/affinity), so it is not a substitute.
2. **Custom/`Tools`-override bash tools get no PI_\* metadata.** pi's
   `pi.registerTool(createBashTool(...))` does expose it (that is what the
   `tool-definition-wrapper.ts` ctx-passthrough hunk enables); Go's
   `CustomTools` are opaque `AgentTool`s built before the session exists. The
   mirror case is correct: pi's `baseToolsOverride` path also drops ctx,
   matching Go's `SessionOptions.Tools`.
   Also **not ported**: pi's `exposeSessionEnvironment` opt-out — Go has no bash
   options struct to hang it on (no `spawnHook`/`commandPrefix`/`shellPath`) and
   pi defaults it true; same call as the 2026-06-24 null-`ProviderHeaders`
   divergence. Pre-existing and untouched: pi's `getShellEnv()` prepends pi's
   bin dir to `PATH`; Go uses `os.Environ()` verbatim.

**Live request-diff note**: both request-body changes this cycle (`bc41f612`,
`9b3a2059`) are **unreleased upstream**, so the shipped 0.81.1 build predates
them and the live pi-vs-go harness cannot validate them — it would flag the
intended change as a mismatch. The in-repo differential suite (38/38) covers
regression; the two new behaviors are pinned by their own mutation-verified
tests instead.

n/a (11):
- **cache control for OpenRouter aliases** (`6f95a338`, #6940) — generator regex
  (`/^~?anthropic\//`) + catalog **data** (`~anthropic/*` aliases gain
  `compat.cacheControlFormat`, new poolside models) + a types.ts doc comment.
  No release → folds into the next regen. Pairs with the `bc41f612` port.
- **derive generated model types from JSON** (`5dc40fee`) — TS typing refactor:
  new `src/model-catalog.ts` (`flattenModelCatalog`), per-provider `*.models.ts`
  collapse to a two-line derivation, internal data JSON goes api-grouped (schema
  v1→v2). Public catalog output stays flat; runtime `MODELS` shape unchanged.
- **agent-harness tools** (`346c8547`) — 2,400 lines adding bash/read/write/edit/
  image tools + `file-mutation-queue` + shell-output/truncate rework under
  `packages/agent/src/harness/`. n/a by the established "no `harness/` tree in
  Go" treatment; the changed `utils/{shell-output,truncate}.ts` have **no
  consumers outside the harness**, so nothing leaks into ported surface. See the
  boundary note below.
- **OpenRouter OAuth** (`7b52cef2`, #6927) — OAuth **acquisition**
  (`auth/oauth/openrouter.ts`, `load.ts`, `bun-oauth.ts`) + an `oauth: lazyOAuth`
  line on the provider descriptor. Same shape as the xai (07-17) and
  kimi-coding (07-22) precedents; Go auto-registers openrouter via
  `ai/envkeys.go` → `OPENROUTER_API_KEY`. No new boundary question.
- **bracketed scoped model ids** (`da8dd872`, #6210) —
  `resolveModelScopeWithDiagnostics` in `core/model-resolver.ts`. Go has
  `findExactModelReferenceMatch`/`tryMatchModel` in `coding/resolve.go` but **no
  scope/glob resolver**, so there is no site for the hunk.
- **openai `previous_response_not_found`** (`906b40a7`, #6955) —
  `openai-codex-responses.ts`; codex provider deliberately unported.
- **tui debug/crash log dir** (`fe42ba5b`), **external editor launch**
  (`75e6123a`, plus a `getExternalEditorCommand` return-type tightening in the
  unported settings-manager), **sibling extension paths** (`c55ae2fa`),
  **version-check manual-update bypass** (`ecb9410c`, `utils/version-check.ts`),
  **contributor approval** (`34f3719a`) — TUI / interactive / packaging / meta.

**Boundary note (not yet a `decide`)**: `346c8547` puts a full first-class tool
suite (bash, read, write, edit, image) into `packages/agent/src/harness/` — a
package the triage skill's scope rule nominally lists as `port` surface. Every
prior harness commit was ruled n/a under "no `harness/` tree in Go", and these
tools duplicate what `coding/tools.go` already implements byte-faithfully, so it
was triaged n/a. But this is the first harness commit carrying *tool
implementations* rather than session/storage plumbing. If upstream keeps
migrating coding-agent tools down into the harness, this needs an explicit
recorded ruling.

**Heads-up for the next catalog regen**: `6f95a338` adds
`compat.cacheControlFormat:"anthropic"` to OpenRouter `~anthropic/*` aliases
plus new poolside models; `5dc40fee` assembles `MODELS` by flattening
api-grouped JSON, so **model key order within a provider may change** (api-group
order rather than the previous flat order) — expect ordering churn in
`ai/models_catalog.json` at the next regen, and re-derive with a fresh
`JSON.stringify(MODELS)` as usual.

## Drift at last sync check (2026-07-22) — pin advanced to a5afc3f1

**Caught up to `a5afc3f1`.** Delta `dd6bea41 → a5afc3f1` fully triaged: 3
first-parent main-line changes — **0 ports, 3 n/a, 0 decides**. **No release tag
crossed** — zero `package.json` version bumps in the range, so `pi-ai` and
`pi-coding-agent` both stay **0.81.1** and every npm byte-golden (catalog,
session tree, image decisions, differential) is untouched; no reference-build
refresh needed. Report-only advance — no Go change, no review cycle.

n/a (3):
- **add Kimi Code subscription OAuth login** (`a5afc3f1`, #6935) — a device
  authorization grant flow (RFC 8628) for the kimi-coding provider:
  `packages/ai/src/auth/oauth/kimi-coding.ts` (new, +302), a `kimiCoding` loader
  in `auth/oauth/load.ts`, a `bun-oauth.ts` bundled-loader entry, and a 12-line
  `providers/kimi-coding.ts` change adding `oauth: lazyOAuth({ name: "Kimi Code
  (subscription)", loginLabel: "Sign in with Kimi Code", load: loadKimiCodingOAuth
  })` alongside the existing `KIMI_API_KEY` apiKey auth. **OAuth token
  acquisition** is on the deliberate non-port list (and the 2026-07-17 facade
  ruling: the `auth/oauth` reorg = acquisition, not ported). The provider-descriptor
  `oauth` wiring is not mirrored in Go because Go hand-writes no kimi-coding
  descriptor — it auto-registers via `ai/envkeys.go` (`kimi-coding`→`KIMI_API_KEY`,
  `EnvAPIKeyAuth`) and does no acquisition. The OAuth **type** surface
  (`OAuthAuth`, `LoginLabel`, `LazyOAuth`, `ProviderAuth.OAuth`) already exists in
  Go as latent SDK surface (ported in the 07-17 cycle) and is wired to no provider
  — exactly the xai OAuth-login precedent. Same shape (a subscription OAuth login
  bolted onto a ported provider), already-settled boundary → **no new decide**.
- **generate-models: use reasoning options from models.dev** (`1ae06409`, #6928)
  — touches `packages/ai/scripts/generate-models.ts` (the **generator**, not
  ported source) + new `models-dev-reasoning-options.ts` generator helper +
  `scripts/diff-model-catalog.mjs` / `generate-thinking-capabilities.mjs` tooling
  + pi-internal vitest fixups. **Zero** `packages/ai/src` / `packages/agent/src` /
  `coding-agent/src/core` touch. No release → folds into the next catalog regen.
  **Heads-up:** reasoning/thinking capabilities are now derived from models.dev
  (`getModelsDevReasoningOptions`) — re-verify the `supportsXhigh` / together-model
  thinking entries absorb cleanly at the regen.
- **approve contributor mteam88** (`db647e42`) — `.github/APPROVED_CONTRIBUTORS`.
  Meta.

No new boundary questions.

## Drift at last sync check (2026-07-21) — pin advanced to dd6bea41

**Caught up to `dd6bea41`.** Delta `8b937370 → dd6bea41`: **42** first-parent
main-line changes — **6 ports → 8 Go commits, 33 n/a, 0 decides**. **Release
crossed — npm `pi-ai`/`pi-coding-agent` 0.80.10 → 0.81.1, two tags** (v0.81.0
`9c480b6a`, v0.81.1 `20be4b18`). Catalog byte-golden regenerated (**431,732 B**,
up from 420,411), `cmp`-clean vs an independent fresh-process re-derivation of
`JSON.stringify(MODELS)` from the 0.81.1 build. Reviewed via independent
go-review (**ship**; 3 LOW polish items applied — nil-safe RetryCallbacks
dispatchers + doc fixes) + independent adversarial parity review
(**fix-first → fixed**: caught the missed `amazon-bedrock.ts` half of 1942b260
— see below — then confirmed everything else faithful). gofmt clean; build/vet/
`-race` green; differential 37/37; openai request-diff scenarios pass.

Ports (chronological):
- **tool_call_id item-level uniqueness** (`d9f7f814`, Go `0e035b2` + golden
  `5f3eff3`): `ai/providers/openai.go` `normalizeOpenAIToolCallID` — pipe ids
  (`{call_id}|{item_id}`) now replay into Chat Completions as
  `{call_id}_{item_id}` (was call_id only), with a `{call_id}_{hash8}` fallback
  over 40 chars, so tool calls sharing a call_id in a turn stay distinct.
  **Request-body golden**; the pre-existing normalization test was updated to
  pi's new output (independently re-derived), plus a new item-uniqueness test.
- **credential env passthrough** (`1942b260`, Go `d30903c` + `912607a`): the
  stored-credential branch was dropping `credential.env`. pi fixed it in two
  spots — `auth/helpers.ts` (Go `ai/auth_helpers.go`, ported first) **and**
  `amazon-bedrock.ts` (Go's generic ambient-provider resolver in
  `ai/builtins_models.go`, **caught by the parity review** — first pass missed
  it). `AuthResult.Env` is consumed (models runtime → resolved credential env →
  request options), so a stored bedrock/vertex key's `AWS_PROFILE`/region
  section now survives. Both branches test-locked.
- **ModelsStoreEntry.lastModified** (`54fad505` SDK half, Go `e261520`): Unix-ms
  Last-Modified stamp on the store entry, round-tripped through the in-memory
  store; latent SDK surface. Host model-runtime/remote-catalog halves n/a.
- **RetryAssistantCall retry loop** (`65dd2e0e`, Go `965a0ed` + `24331ce`):
  `RetryPolicy` + `RetryCallbacks` + `RetryAssistantCall` in `ai/retry_loop.go`
  (bounded exponential backoff `BaseDelayMs*2^(attempt-1)`, fail-fast on
  non-retryable/quota, abort-during-backoff normalized to an aborted message),
  latent SDK surface **per the 2026-06-25 ruling** (retry.ts additions are
  ported to mirror pi's structure). The summary/branch-summary consumers that
  wire it (AgentHarness.retry + retry events) live in the unported harness/host
  layer. Mutation-verified non-vacuous (callback ordering, budget, abort).
- **compaction retainedTail** (`9e7582aa` JSONL/session-tree half, Go `3e68f4a`):
  `coding/session_tree.go` now parses a compaction entry's inlined `retainedTail`
  (with optional `firstKeptEntryId`) and reconstructs context as
  summary + retainedTail, skipping the firstKeptEntryId walk — so pi-0.81.x
  sessions rebuild identically. **Session-tree golden** (additive; existing
  fixtures don't use retainedTail so 8/8 hold). The `sqlite-node` package, the
  `getPathToRootOrCompaction` storage short-circuit, `getSessionStats`, and entry
  cursors are host/Node surface — n/a.
- **catalog regen 0.81.1 + Qwen Token Plan** (`bbb91fa8` + releases `9c480b6a`/
  `20be4b18`, Go `b4bb393`): `ai/models_catalog.json` re-derived byte-identical
  from the 0.81.1 build; Qwen Token Plan built-ins (`qwen-token-plan`,
  `qwen-token-plan-cn`) wired via `ai/envkeys.go` (models + per-region baseURL +
  `openai-completions` api ride in from the catalog and auto-register with
  `EnvAPIKeyAuth`). Rolls up the 07-20/07-21 generate-models data churn
  (gpt-5.6 context window, new Gemini/Kimi models, moonshot/kimi3 compat).
  Schema-drift enumerated: no new catalog keys that abort the Go struct load.

n/a with rationale (the notable ones):
- **usage on branch-summary/compaction/tool-result entries** (`2fd38684`) — **no
  ported consumer**. Go's compaction is an in-memory `TransformContext` (persists
  no entries with usage), its session tree is read-only reconstruction (usage not
  surfaced), and every real consumer — usage-totals, the interactive footer,
  extension usage accounting, persisted-session usage — is unported. Adding the
  `usage` fields would be dead code. Parity review concurred. Re-escalates only
  if Go gains a persisted-compaction/usage-totals consumer.
- **decouple/restore agent stream-fn from compat** (`1235c0ec` + `b9e5c5d9`) —
  TS-module decoupling to sever `pi-agent-core`'s dependency on the compat layer
  via a settable `setDefaultStreamFn`. Go has no such coupling: `agent/loop.go`
  already defaults `streamFn` to `ai.StreamSimple` inline (the on-record
  "globals stay as compat" divergence). No `packages/ai/src` change. Structural-
  mirror n/a.
- **sqlite session storage** (`9e7582aa` remainder), **defer catalog refresh**
  (`c889eb88`, `b1425041`), **extension usage accounting** (`f8b74a45`),
  **decouple agent streams** host halves, **orchestrator→server rename**
  (`8495f9d0`), **llama extension** (`864b35c4`), **rpc get_available_thinking_levels**
  (`c1793952`) — host/TUI/extensions/packaging, all on the deliberate non-port
  list. The 8 generate-models data/generator commits fold into the 0.81.1 regen.

No new boundary questions.

## Drift at last sync check (2026-07-20) — pin advanced to 8b937370

**Caught up to `8b937370`.** Delta `3da591ab → 8b937370` fully triaged: 10
first-parent main-line changes — **0 ports, 10 n/a, 0 decides**. All ten are
single-parent (no merges), so nothing could be merge-smuggled; the whole-range
`packages/ai/src` + `packages/agent/src` + `coding-agent/src/core` sweep matches
the per-commit sum exactly. **No release tag crossed** — zero `package.json`
version bumps in the range, so `pi-ai` and `pi-coding-agent` both stay **0.80.10**
and every npm byte-golden (catalog, session tree, image decisions, differential)
is untouched; no reference-build refresh needed. Report-only advance — no Go
change, no review cycle.

Four changes touch ported surface but all resolve to no Go change:
- **replace generic record checks** (`95607469`) — refactors
  `packages/ai/src/api/pi-messages.ts`'s `parsePiMessagesErrorBody`, inlining the
  `isRecord` type-guard. **Behavior-identical** (still requires a nested `error`
  that is a non-array object; an array `parsed` still fails via `parsed.error ===
  undefined`). Go's `ai/providers/pi_messages.go:83` already ports this faithfully
  via typed `map[string]any` unmarshal (rejects arrays/primitives). Already-faithful
  class — no Go change. (Also touches `radius-config.ts` + `publish-model-catalog.mjs`
  — unported host/tooling.)
- **share UUIDv7, use for Codex** (`d2f8dafb`, #6834) — relocates `uuidv7` from
  `packages/agent/src/harness/session/uuid.ts` into `packages/ai/src/utils/uuid.ts`
  (re-exported from the ai index) and adopts it in `openai-codex-responses.ts`
  (**unported** codex provider). Go already has its own `uuidv7()` at
  `coding/session_store.go:47`; the agent-harness/session consumers only change
  import path (no behavior). TS package-org move — n/a.
- **add shared contentText utility** (`94373d81`, #6840) — new
  `packages/ai/src/utils/text.ts` (`contentText(content, sep="\n")`) with all
  consumers (agent-session, compaction × both packages) refactored to it. **Pure
  refactor, behavior-identical** — empty-sep at the two former `.join("")` sites,
  default `\n` elsewhere; the `serializeConversation` assistant-text guard becomes
  `content.some(type==="text")`, equivalent. No golden/behavior change; Go extracts
  text inline. Under the 2026-06-25 "mirror SDK structure" precedent a strict read
  could make this a latent `port`, but it's a trivial join helper with zero behavior
  delta and Go idiom differs — recorded as **not ported** (structural-mirror
  judgment, no ruling needed).
- **support responses API for OpenCode Go** (`75cb0b87`) — adds `openai-responses`
  to the opencode-go provider's TS api-map. Go routes by the catalog `api` field
  through the compat path (globals-stay-as-compat divergence), not the Models-runtime
  api-map, and **no opencode-go catalog model routes to `/responses`** yet — folds
  into the next regen if one adopts it.

n/a (the other six):
- **avoid duplicate session reads** (`f1c587dd`) —
  `coding-agent/src/core/session-manager.ts` buffered session-**header-scan** perf
  optimization (`SESSION_HEADER_READ_BUFFER_SIZE`, best-effort discovery,
  `_setSessionFile` preloading). Host session-discovery/reload machinery (on the
  non-port list); **no session format or golden change** — Go's `readSessionInfo`/
  `LoadSessionMessages` read the same info.
- **clear inverted cursor on exit** (`5f2f7d06`, #6790) — `packages/tui/` only. TUI.
- **normalize bin path** (`916de90d`, #6812) — `packages/ai/package.json` bin field.
  Packaging.
- **approve contributor rsaryev / QuintinShaw** (`87845fc4`, `85f89db9`) —
  `.github/APPROVED_CONTRIBUTORS`. Meta.
- **kimi: add low,high to k3 and remove k2p7 references** (`8b937370`) — generator
  + `kimi-coding.models.ts` catalog **data** + pi-internal test churn. No release →
  deferred to next regen.

**Heads-up for the next catalog regen** (data drift accumulating, unpublished):
- `8b937370`: `k3` gains **low/high** effort variants; **`k2p7` removed**;
  **`kimi-for-coding` re-added** to the kimi-coding type map — this **supersedes**
  the 07-18 heads-up that `5124c61b` would drop `kimi-for-coding` at the next regen
  (it's back), so `coding/resolve.go:50`'s kimi-coding default stays valid.
  Still-pending `5124c61b` removals: `kimi-k2-thinking`,
  `nvidia/llama-3.3-nemotron-super-49b-v1.5` (no Go references).
- `75cb0b87`: opencode-go `openai-responses` capability — re-check whether any
  opencode model's `api` flips at the regen.
- Prior: `ce48d9b4` copilot long-context `cost.tiers`.

No new boundary questions.

## Drift at last sync check (2026-07-18) — pin advanced to 3da591ab

**Caught up to `3da591ab`.** Delta `a9f6a315 → 3da591ab` fully triaged: 5
first-parent main-line changes — **0 ports, 5 n/a, 0 decides**. **No release
tag crossed** — zero `package.json` version bumps in the range (the only root
`package.json` change is a `diff:model-catalog` script alias), so `pi-ai` and
`pi-coding-agent` both stay **0.80.10** and every npm byte-golden (catalog,
session tree, image decisions, differential) is untouched; no reference-build
refresh needed. Report-only advance — no Go change, no review cycle. The
whole-range `packages/ai/src` + `packages/agent/src` +
`coding-agent/src/core` sweep accounts for every in-scope byte (only the two
catalog-data `.models.ts` deletions + the extensions/prompt-templates files,
all n/a); no merge-smuggled hunks.

n/a (5):
- **preserve GitHub Copilot long-context pricing tiers** (`ce48d9b4`, #6668) —
  touches `packages/ai/scripts/generate-models.ts` (the **generator**, not
  ported source; we embed its derivative `models.generated.ts` →
  `ai/models_catalog.json`, regenerated only at a release) + new
  `scripts/diff-model-catalog.mjs` tooling + CHANGELOG. No release → folds into
  the next regen. **Heads-up:** the generator now emits `cost.tiers`
  context-pricing (`getModelsDevCost` flatMap over models.dev `cost.tiers`) for
  copilot long-context models; the Go catalog structs already parse `cost.tiers`
  (ported in the 0.80.6 cycle), so it should absorb cleanly — re-verify the
  copilot entries at the regen.
- **support all-argument prompt defaults** (`64f83c85`, #6695) —
  `coding-agent/src/core/prompt-templates.ts` (adds `${@:-default}` /
  `${ARGUMENTS:-default}`). **prompt-templates** is on the deliberate non-port
  list.
- **buildfix: fix tests referencing old models** (`5124c61b`) — removes 3 stale
  model type-entries from the catalog-data sources
  (`ai/src/providers/kimi-coding.models.ts`: `kimi-for-coding`,
  `kimi-k2-thinking`; `openrouter.models.ts`:
  `nvidia/llama-3.3-nemotron-super-49b-v1.5`) + pi-internal vitest fixups.
  Catalog **data**, no release → deferred to next regen. **Heads-up:** all 3 are
  in the *current* `ai/models_catalog.json` and will drop at the next regen.
  `kimi-k2-thinking` + the nvidia id have **no Go references**; `kimi-for-coding`
  is the kimi-coding **default** at `coding/resolve.go:50` — but pi upstream
  still points there too (`model-resolver.ts:44` = `"kimi-coding":
  "kimi-for-coding"`), so the Go port stays **faithful**; if the id vanishes from
  the published MODELS at the next regen it's a mirror-pi-not-diverge situation,
  not a Go fix.
- **add llama.cpp router integration** (`f1a466b1`) — new host **extension**
  `coding-agent/src/extensions/llama/*` (client/provider/index/ui) + host wiring
  (`main.ts`, `modes/interactive/interactive-mode.ts`,
  `core/resource-loader.ts`, `core/extensions/types.ts`). Extensions runtime is
  deliberately not ported; nothing lands in `packages/ai/src`. Not a new
  boundary — the provider registers via the host extension mechanism, not the
  SDK.
- **add Hugging Face llama search** (`3da591ab`) — extends the same
  `extensions/llama/*` extension (HF model search UI). Extensions — n/a.

No new boundary questions.

## Drift at last sync check (2026-07-17) — pin advanced to a9f6a315

**Caught up to `a9f6a315`.** Delta `dcfe36c7 → a9f6a315` fully processed: 38
first-parent main-line changes — **8 ports (→ 8 Go commits, 3 of them from
merge-carried hunks), 29 n/a, 0 decides** (the one boundary question — facade
scope — was ruled by the owner in-session, recorded as the **2026-07-17
ruling**: SDK-scoped). **Release crossed — npm 0.80.7 → 0.80.10, three tags**
(v0.80.8's release commit `fae7176c` rode in via merge and never appears on
the first-parent line; v0.80.9 `2d16f929`; v0.80.10 `8dc78834`). Catalog
byte-golden regenerated (**420,411 B**), endpoint-pinned byte-identical both
ends. Reviewed via independent go-review (**ship**; 3 LOWs applied) +
independent adversarial parity review (**fix-first → fixed**: caught two
merge-carried hunks first-parent triage missed — see below — then confirmed
everything faithful). gofmt clean; build/vet/`-race` green; differential
37/37; session 8/8; image decisions 8/8.

**Triage lesson recorded:** two in-scope hunks rode into the range inside
merge commits (`3524cd4c`/`5e336cfa` carried `5220aba6` and the v0.80.8
release; the facade-family merges carried `97f9978f`) and were missed by
first-parent diffstat triage — both caught by the adversarial parity review
and a follow-up full-range `git diff <pin>..origin/main -- packages/ai/src`
reconciliation sweep. Future syncs should run that whole-range sweep during
triage, not only per-first-parent-commit diffs.

- **model-runtime facade, SDK-scoped** (`ff28097a`, Go `08e0cde`): per the
  2026-07-17 ruling. New `ai/models_store.go`; Provider gains
  `DynamicModels`/`FilterModels` + context-taking `RefreshModels` (store
  restore → allowNetwork gate → fetch → persist, shared in-flight);
  `CreateProviderOptions.FetchModels/FilterModels`; baseline+dynamic overlay
  merge in `GetModels`. Models gains `Refresh(ctx, options) →
  {Aborted, Errors}` (concurrent sweep, per-provider effective-credential
  resolution with OAuth refresh under the store lock, skip-unconfigured,
  best-effort cache restore on failure), `CheckAuth`, `GetAvailable`,
  `Login`/`Logout`, `GetProviderAuth` + `GetAuth(model, overrides)` with
  case-insensitive model-header merge, `ModelsStreamOptions` transforms
  (stripped before provider dispatch), and applyAuth now errors on an
  unconfigured provider (pi `Provider is not configured:`). Auth substrate:
  `AuthLoginCallbacks`→`AuthInteraction`, provider-scoped
  `ApiKeyAuth.Resolve` (model param removed), optional `ApiKeyAuth.Check`,
  `OAuthAuth.Refresh(ctx, …)`, `CredentialStore.List`+`CredentialInfo`,
  `AuthEvent` `info`+`AuthInfoLink`, typed `Credential.AvailableModelIDs`
  (pi's OAuthCredentials index signature), github-copilot `FilterModels`,
  `radius`→`RADIUS_API_KEY`. Latent SDK surface — no golden touched; tests
  mirror pi's new models-runtime/providers vitest cases by name.
- **ModelsStore entries carry checkedAt** (`bd9e09db` SDK half, Go `945696c`):
  `ModelsStoreEntry{Models, CheckedAt}` (Unix ms), stamped on a completed
  remote check. Radius/host halves out per rulings.
- **kimi deferred tools in openai-completions** (`f16b4e0c`, Go `2b07203`):
  `deferredToolsMode:"kimi"` compat — deferred tools leave the top-level
  `tools` param and are re-declared in a `role:system`+`tools`-only message
  after their tool-result run. **Request-body golden surface, live at this
  regen** (moonshotai/-cn kimi-k3 carry the flag) but byte-no-op for all 37
  differential scenarios (none uses a kimi model or `AddedToolNames`).
  Filter + emission mutation-verified behavioral-red.
- **xai default grok-4.5 + OAuthAuth.LoginLabel** (`a01baaae`, Go `ac9abd0`):
  `defaultModelPerProvider["xai"]` re-pointed (old id pruned at the regen),
  pinned via the fallback template; LoginLabel typed + latent.
- **retry Responses early EOF** (`b0c2a90e`, Go `6b4794c`): classifier
  pattern byte-identical; pairs with Go's own responses terminal-event error.
- **catalog regen → npm 0.80.10** (releases `2d16f929`+`8dc78834`+merge-carried
  `fae7176c`; data: `70c57632` kimi-k3 deferred, `c2c32feb` gateway limits,
  `aba32450` moonshot pricing, `78ff2494` kimi max thinking, `b8575f60`
  adaptive thinking (compat flags Go already reads), `8881e176` subscription
  costs, `c1b7856e` xai regen guard, merge churn; Go `f1e4f31`): 420,411 B,
  endpoint-pinned; schema drift = `compat.deferredToolsMode` +
  `compat.allowEmptySignature` only; +12/−5 model ids, all removals
  unreferenced; smoke pin intact.
- **xai encrypted-reasoning include** (merge-carried `5220aba6`, Go `80ebacc`,
  **parity-review catch**): responses builder sends
  `include:["reasoning.encrypted_content"]` for every reasoning-capable xai
  model regardless of effort branch — live surface (grok-4.5 routes through
  responses and is the xai default). Device-OAuth half out. Mutation-verified.
- **force refresh flag** (merge-carried `97f9978f`, Go `21a18cb`,
  **parity-review catch**): `Force` on `RefreshModelsContext` +
  `ModelsRefreshOptions`, threaded to the main provider call only (cache
  restore stays force-free, matching pi). Latent for built-ins.

n/a (29): **host model-runtime restructuring (2026-07-17 ruling)** —
`fab309e9` (picker catalog refresh), `019e4ad6` (native extension providers),
`c6d83715` (set-title windows), plus the host halves of the facade merges
(`model-runtime`/`provider-composer`/`model-config`/`models-store`/
`runtime-credentials`/`remote-catalog-provider`, `ModelRegistry`→`ModelRuntime`
renames in sdk.ts/model-resolver.ts, `bun-oauth.ts`). **merge carriers** —
`3524cd4c`, `5e336cfa` (in-scope hunks extracted above; remainder is package
bumps, xai-oauth tests, docs, publish-model-catalog scripts). **TUI /
interactive** — `45203abf` (coalesce thinking blocks), `1c799cec` (normalize
tabs), `00b03267` (CRLF/CR line endings), `a2c5ee33` (don't highlight read
errors — display formatting in `tools/read.ts`, not tool-result content),
`35a0d5d6` (compaction queue in interactive-mode). **agent-session-runtime**
— `b97ed202` (clone-failure message). **xai OAuth (unported)** — the
device-flow halves of `5220aba6`/`a01baaae`. **build tooling / CI** —
`056c8cbb` (explicit model generation in release.mjs), `16afccd3` +
`c8560b8d` (catalog-publish workflow). **docs / changelogs / meta** —
`aa508b7f`, `c29eda6f`, `f7e06037`, `216e672e`, `58575888`, `5d9fedf7`,
`36db3fa3`, `dc24a73c`, `e5e87268`. **post-release build refactor** —
`a9f6a315` (**separate generated model data**, #6765 — moves generated model
data out of `*.models.ts` into JSON; MODELS output unchanged, lands after
v0.80.10 so nothing to regen; **heads-up for the next regen**: confirm the
published build's MODELS stays byte-stable through this refactor). No new
boundary questions.

## Drift at last sync check (2026-07-15) — pin advanced to dcfe36c7

**Caught up to `dcfe36c7`.** Delta `adfac437 → dcfe36c7` fully processed: 7
main-line changes — **2 ports (→ 2 Go commits), 1 data-only folded into the
regen, 4 n/a, 0 decides**. **Release crossed — v0.80.7**: both `pi-ai` and
`pi-coding-agent` bump **0.80.6 → 0.80.7** (registry integrity re-verified).
The catalog byte-golden was regenerated (**416,889 B**), endpoint-pinned
byte-identical both ends. Reviewed via independent go-review (**ship, no
findings**) + independent adversarial parity review (**both faithful**;
system-prompt removal byte-confirmed against the shipped build; catalog
endpoint-pinned; schema-drift clean; orphaned-id + resolve re-point verified).
gofmt clean; build/vet/`-race` green; differential 37/37;
`ai/models_catalog.json` regenerated.

- **remove current date from system prompt** (`f4e9ca74`, fixes #6621, Go
  `f9b6a16`): upstream drops the `Current date: YYYY-MM-DD` line (and its date
  computation) from **both** branches of `system-prompt.ts`, keeping only
  `Current working directory:`. `coding/systemprompt.go` removes the `date`
  computation, both `prompt += "\nCurrent date: " + date` lines, the now-dead
  `Now time.Time` option field, and the `time` import. **Model-visible surface**
  (the system prompt appears in request bodies) but a **byte-level no-op for
  every existing golden** — no session/differential golden embeds the assembled
  coding-agent prompt, and the dedicated `systemprompt_golden_test.go` /
  `systemprompt_assembly_test.go` goldens were updated (both `Now:` fixtures +
  `Current date: 2026-06-08` lines + the assembly ordering entry removed).
  Parity-confirmed against the shipped 0.80.7 build's `dist/core/system-prompt.js`
  (no `Current date` string; `Current working directory` retained both branches).
- **catalog regen → npm 0.80.7** (release `818d6745` + data commit `1f9e846c`;
  Go `b325abe`): `ai/models_catalog.json` re-derived byte-identical from the
  integrity-verified 0.80.7 build's MODELS (`sha512-8RLKLwe5…HsV9SQ==`),
  **endpoint-pinned** — the 0.80.6 derivation reproduces the prior committed
  catalog and the new file equals the 0.80.7 derivation, both `cmp`-clean, so
  the ported diff equals the upstream regen exactly. Drains the deferred catalog
  queue: fable-5 xhigh/max (`bc469b03`), copilot 1M (`9eedaf8c`), mai-code
  `/responses` routing (`f7b78e2a`), openrouter/vercel churn (`72d77b53`), and
  **opencode `sessionAffinityFormat:"openai-nosession"`** (`1f9e846c` — added to
  opencode's openai-responses models so they don't send the session-id header;
  the provider code was already ported in `8e258ee` on 07-13, so this cycle is
  catalog-data-only). Adds gpt-5.6 luna/sol/terra, gpt-realtime-2.1, kwaipilot
  kat-coder; prunes 11 openrouter/vercel ids (incl. `anthropic/claude-3.5-haiku`,
  `mistral/devstral-small`, `mistral/pixtral-large`, xiaomi mimo-v2). **No schema
  drift** — no new model/cost/tier keys or `api` values; the only additions
  (`compat.sessionAffinityFormat`, `compat.supportsToolSearch`) ride
  `ai.Model.Compat` `json.RawMessage` and cannot abort the load. Smoke pin
  `claude-haiku-4-5-20251001` (maxTokens 64000) intact. Orphaned-id fix:
  `coding/resolve_test.go`'s full-id-fallback fixture re-pointed from the removed
  `anthropic/claude-3.5-haiku` (was sole-hosted by vercel-ai-gateway) to
  `anthropic/claude-opus-4.8-fast` (now sole-hosted by openrouter) — same
  provider-prefix → full-id fallback path, mutation-traced through
  `findExactModelReferenceMatch`.

n/a (4): **openai-codex (unported)** — `dcfe36c7` (**clamp session-id to 64
chars for openai-codex**, #6653 — `packages/ai/src/api/openai-codex-responses.ts`
only; codex provider is on the deliberate non-port list). **test-only** —
`92ffae52` (**type Anthropic probes by catalog providers** — tightens
`KnownProvider`→`BuiltinProvider` annotations in two `packages/ai/test/*.e2e.test.ts`
live-API probe files; no behavior, `BuiltinProvider` already existed, no Go
consumer). **CHANGELOG/meta** — `53a087fe` (**audit unreleased changelogs**),
`9d09075c` (**add [Unreleased] section for next cycle**) — CHANGELOG.md only.
No new boundary questions.

## Drift at last sync check (2026-07-14) — pin advanced to adfac437

**Caught up to `adfac437`.** Delta `7303cbac → adfac437` fully processed: 6
main-line changes — **2 ports (→ 2 Go commits), 1 already-faithful/no-code, 3
n/a, 1 decide (ruled + ported)**. **No release tag crossed** — zero
`package.json` bumps in the range; `pi-ai` and `pi-coding-agent` both stay
**0.80.6**, so every npm byte-golden (catalog, session tree, image decisions,
differential diff) is untouched and no reference-build refresh was needed.
Reviewed via independent go-review (**fix-first → ship**; one HIGH panic +
one MED CRLF-seam, both fixed) + independent adversarial parity review (**both
faithful**; two low-frequency pi-messages divergences caught and fixed).
gofmt clean; build/vet/`-race` green; differential 37/37;
`ai/models_catalog.json` unchanged.

- **openai-responses reasoning `encrypted_content` backfill** (`1f0dbc00`,
  #6409, Go `592561f`): Azure OpenAI can omit `reasoning.encrypted_content` from
  `response.output_item.done` and supply it only in
  `response.completed.response.output`. `ai/providers/openai_responses.go` now
  indexes reasoning blocks by item id (`reasoningBlocksByID`) and, on the
  terminal response, backfills the missing `encrypted_content` onto the persisted
  reasoning signature so `store:false` multi-turn replay stays stateless. New
  `responsesItem.EncryptedContent` + `responsesPayload.Output` fields (purely
  additive to unmarshalling). **Request-body golden surface** but **latent** for
  the ported openai-responses path (its `output_item.done` already carries
  `encrypted_content`); no differential scenario drives a late signature →
  37/37 unchanged. Parity-verified that the stored-sig **key order is
  unobservable** — on replay the request builder re-parses the signature into a
  map and the outer `json.Marshal` re-serializes it, so only the *presence* of
  `encrypted_content` survives, not byte order. Test mutation-verified
  non-vacuous (disable the backfill → sig lacks `encrypted_content`).
- **pi-messages provider API** (`961fa6c1`, Go `e46c9c1`, **decide → ruled**):
  new first-class provider API — a single POST of `{model,context,options}` to
  `<baseUrl>/messages` returning an SSE stream of serialized assistant-message
  events + terminal `done`/`error`. Ported per the **2026-07-14 ruling**
  (SDK-only: port the generic `pi-messages` API; leave the Radius OAuth machinery
  + host wiring out). New `ai/providers/pi_messages.go` (`StreamPiMessages` +
  `StreamSimplePiMessages` + `RegisterPiMessages`, `PiMessagesOptions`), `Api`
  const `APIPiMessages`, and `radius`→`PI_GATEWAY_API_KEY` in `ai/envkeys.go`.
  Byte-faithful request (URL + `?debug=1`, three fixed headers + merged provider
  headers, `{model,context,options:{temperature,maxTokens,reasoning,cacheRetention,sessionId,toolChoice}}`
  with undefined-field omission, `OnPayload`/`OnResponse` hooks), SSE framing,
  1:1 event converter, `PiMessagesResponseError` formatting + diagnostics
  (`pi_messages_rewrite` / `pi_messages_response_failure`), terminal-event
  requirement, and `resolveCacheRetention` (`PI_CACHE_RETENTION`). **No catalog
  model added** (radius is dynamic OAuth-only), so no golden touched. Hardened
  after review: (1) a `recover` in the streaming goroutine turns a non-conformant
  backend (`*_delta` for an unstarted `contentIndex`) into a terminal error
  event — mirroring pi's throw→catch — instead of panicking the host; (2)
  whole-buffer CRLF normalization so a `\r\n` split across reads still frames;
  (3) a non-nil `OnResponse` error now fails the stream, symmetric with
  `OnPayload`. All three fixes mutation-verified non-vacuous.

**already-faithful / no Go change (1):** `0e6909f0` (**anthropic-messages: skip
usage fields if empty**, #6567) — pi wraps the `message_delta` usage-field
merges in `if (event.usage)` so a proxy that omits `usage` from `message_delta`
doesn't clobber the `message_start` values. Go is already faithful:
`ai/providers/anthropic.go`'s `message_delta` case already guards the entire
usage merge with `if ev.Usage != nil` (line 523), and the streamEvent's `Usage`
is a nil-able `*anthropicUsage`, so a missing `usage` unmarshals to nil and is
skipped — exactly pi's new behavior. No test added (already-faithful class,
same as `8c0ccd14` / `a6f720e6`).

n/a (3): **Bedrock (unported)** — `f8f75544` (**pass Bedrock unhandled stop
reasons to error message**, #6485 — `packages/ai/src/api/bedrock-converse-stream.ts`
only). **host/CLI package management** — `b084d2fb` (**add legacy-peer-deps flag
on `pi uninstall` when using npm**, #6486 — `coding-agent/src/core/package-manager.ts`
npm install/uninstall flags, no Go consumer; same class as `c8ada4e7`).
**TUI / interactive** — `adfac437` (**clarify login options** —
`modes/interactive/interactive-mode.ts` copy only). No new boundary questions
(the one boundary question, Radius, is now recorded as the 2026-07-14 ruling).

## Drift at last sync check (2026-07-13) — pin advanced to 7303cbac

**Caught up to `7303cbac`.** Delta `8479bd84 → 7303cbac` fully processed: 5
main-line changes — **2 ports (→ 2 Go commits), 3 n/a, 0 decides**. **No release
tag crossed** — zero `package.json` bumps in the range; `pi-ai` and
`pi-coding-agent` both stay **0.80.6**, so every npm byte-golden (catalog,
session tree, image decisions, differential diff) is untouched and no
reference-build refresh was needed. Reviewed via independent go-review (**ship**;
two optional LOW nits — a named `openai-nosession` const + switch-vs-nested-if,
both declined to stay parity-faithful to pi's exclusion-based branch) +
independent adversarial parity review (**both faithful**; no divergences).
gofmt clean; build/vet/`-race` green; differential 37/37; `ai/models_catalog.json`
unchanged.

- **forward Responses `tool_choice` option** (`eacaa130` openai-responses.ts
  half, #6588, Go `7a37fc8`): adds a latent `ToolChoice any` option to the
  openai-responses provider (`ai/providers/openai_responses.go`); when set it is
  emitted verbatim as the request-body `tool_choice` param (after the `tools`
  block, before reasoning — byte/position-identical to pi's
  `if (options?.toolChoice !== undefined) { params.tool_choice = options.toolChoice }`),
  nil leaves it off (API default `"auto"`). Same shape as the existing latent
  `OpenAIOptions.ToolChoice` on the completions provider. **Request-body golden
  surface** but a **byte-level no-op** — no consumer/scenario sets it (37/37
  unchanged). The `openai-codex-responses.ts` half is **n/a** (codex provider
  unported).
- **OpenRouter session-affinity format** (`298665cf`, #6496, Go `8e258ee`):
  replaces the boolean session-affinity toggles with a `sessionAffinityFormat`
  selector on both OpenAI-compatible providers.
  `ai/providers/openai_compat.go` adds `SessionAffinityFormat` (+ shared
  `sessionAffinityFormatFor(isOpenRouter)` default helper +
  `sessionAffinity{OpenAI,OpenRouter}` consts); `ai/providers/openai.go`
  branches the completions header block (openrouter → `x-session-id`; openai →
  `session_id`+`x-client-request-id`+`x-session-affinity`; openai-nosession
  drops `session_id`); `ai/providers/openai_responses.go` replaces
  `sendSessionIdHeader` with `sessionAffinityFormat`
  (`detectResponsesSessionAffinityFormat` = openrouter when
  `provider=="openrouter" || baseURL⊇openrouter.ai`) and branches the same way
  (minus `x-session-affinity`, which the Responses path never sent).
  **Header golden surface**, but the default non-openrouter path is
  byte-unchanged — new behavior only fires for openrouter models with session
  affinity, absent from all 37 differential scenarios. The
  `model-registry.ts` compat-schema half is **host-side/unported** (only the
  provider-side compat parse is in scope). The responses session-header test was
  migrated `sendSessionIdHeader:false` → `sessionAffinityFormat:"openai-nosession"`
  (mirroring pi's own test migration — pi removed `sendSessionIdHeader` as a
  documented breaking change) with added openrouter coverage on both providers.

n/a (3): **catalog churn deferred** — `f7b78e2a` (**route GitHub Copilot
MAI-Code models through /responses**, #6544 — `generate-models.ts` generator +
`github-copilot.models.ts` re-routes `mai-code-1-flash-picker` to the
`/responses` API; no bump → folds into the next release regen; **heads-up:** the
routed model's `api` field changes, re-check `ai/catalog_load_test.go`'s pinned
id + this entry at that regen, on top of the still-deferred `bc469b03`
fable-5 xhigh/max, copilot 1M, and openrouter churn). **agent-session-runtime /
branch-summary generation (unported)** — `7303cbac` (**fix branch summary with
ambient auth**, #6595 — allows a null `apiKey` so branch summary uses the
ambient-auth flow like compaction; the fix lives entirely in unported host code:
`agent-session.ts`'s `_getCompactionRequestAuth`→`_getSummarizationRequestAuth`
request-auth threading + `branch-summarization.ts`'s `generateBranchSummary`
entrypoint, which has no Go consumer — Go ports compaction summarization in
`coding/compaction.go` but not branch-summary *generation*, so the bug cannot
manifest; same ruling class as prior agent-session-runtime items). **meta** —
`16a3d420` (**approve contributor vibeinging** — `.github/APPROVED_CONTRIBUTORS`).
No new boundary questions.

## Drift at last sync check (2026-07-11) — pin advanced to 8479bd84

**Caught up to `8479bd84`.** Delta `81de5702 → 8479bd84` fully processed: 12
main-line changes — **2 ports (→ 2 Go commits), 1 already-faithful/no-code, 9
n/a, 0 decides**. **No release tag crossed** — zero `package.json` bumps in the
range; `pi-ai` and `pi-coding-agent` both stay **0.80.6**, so every npm
byte-golden (catalog, session tree, image decisions, differential diff) is
untouched and no reference-build refresh was needed. Reviewed via independent
go-review (ship; LOW nits applied) + adversarial parity review (both faithful).
gofmt clean; build/vet/`-race` green; differential 37/37; `ai/models_catalog.json`
unchanged.

- **message-anchored / deferred tool loading** (`3d8f7435`, #6474, Go `bf103a3`):
  cache-friendly dynamic tool loading. A tool result records tools it introduces
  in a new `AddedToolNames` field (`ai.ToolResultMessage` + agent
  `AgentToolResult`); supported models load those defs at the transcript point
  they appear instead of the cached prefix. New `ai/deferred_tools.go`
  (`SplitDeferredTools`); anthropic emits `tool_reference` blocks +
  `defer_loading:true` with ordinary tool-result content displaced to sibling
  blocks (Anthropic rejects the mix); openai-responses emits
  `tool_search_call`/`tool_search_output` items with a `pi_tool_load_<shortHash>`
  call id + `defer_loading` tool defs; `ai/estimate.go` adds deferred-tool tokens
  after the usage anchor; `defaultSupportsToolReferences` gates first-party Claude
  ≥4.5 non-Haiku (a date-suffixed second version group → minor 0). **Request-body
  golden surface (anthropic + openai-responses)** but a **byte-level no-op for all
  37 differential scenarios** — the paths are double-gated on model support +
  `AddedToolNames`, which no scenario carries, and the no-defer path rebuilds the
  identical `tool_result` block. `shortHash` mutation-verified byte-identical to
  pi's JS (incl. astral surrogate pairs). **Out of scope within the commit
  (n/a):** the openai-codex provider (unported — no Go file), the
  `supportsToolSearch:true` catalog *data* (lands at the next release regen — the
  openai-responses deferred path stays dormant until then; the anthropic path is
  live for Claude ≥4.5 via the id-computed default), plus extensions
  runner/wrapper, model-registry, generate-models, docs.
- **filter ambient auth marker in compat dispatch** (`850c210b` compat.ts half,
  Go `4553fc1`): `withEnvAPIKey`/`withEnvAPIKeySimple` no longer inject the
  `<authenticated>` ambient-auth marker (returned by `GetEnvApiKey` for
  ambient-credential providers) as a real API key — they treat it like an empty
  key and leave options untouched so the provider authenticates ambiently. Marker
  hoisted to a shared `ambientAuthMarker` const. **Latent** for ported providers
  (the marker only arises for google-vertex / amazon-bedrock, both unported) but
  faithful to pi's compat-path guard under the 2026-06-24 compat-routing ruling.
  The `bedrock-converse-stream.ts` half (and `19fe0e01`) is n/a (Bedrock
  unported). Test locks that the amazon-bedrock ambient path is not injected as a
  key.

**already-faithful / no Go change (1):** `bdd5c53b` (**fall back to ambient
Cloudflare account id for key-only credentials**, #6292) — changes the
Models-runtime `cloudflare-auth.ts resolveValue` to a per-field credential→env
merge so a credential carrying only the API key still picks up the account /
gateway id from ambient env. Go deliberately does not port the `cloudflare-auth.ts`
Models-runtime layer (2026-06-24 ruling items 2–3): it resolves CF placeholders
**inline** in `resolveCloudflareBaseURL` (`ai/providers/cloudflare.go`), reading
account/gateway id from env unconditionally via `getProviderEnvValue`. pi is
converging toward the env fallback Go's inline path always had — bytes unchanged
through Go's consumer path. Reinforces (does not threaten) the inline-resolution
divergence.

n/a (9): **catalog churn deferred** — `bc469b03` (**add xhigh/max to all fable-5
providers**, #6490 — generator `applyThinkingLevelMetadata` broadening +
`thinkingLevelMap` metadata on github-copilot/openrouter models + an openrouter
glm cost/context edit; no bump → `[Unreleased]`, folds into the next release
regen — re-check `ai/catalog_load_test.go`'s pinned id for orphaning then).
**Bedrock (unported)** — `3ea064ea` (support Bedrock API key login — bedrock
provider + interactive login), `19fe0e01` (preserve ambient AWS auth for Bedrock
— `bedrock-converse-stream.ts`, sets up the marker `850c210b` filters).
**coding-agent interactive / TUI / clipboard** — `3b686ac2` (message copy
shortcut — tree-selector/keybindings/interactive), `d7a48d30` (fall back to text
clipboard paste — clipboard + interactive, same class as `62f45bad`), `8479bd84`
(parse legacy alt-prefixed symbols — `packages/tui/` only). **CI / docs / meta**
— `91585d9a` (bump bun to 1.3.14 — `.github/workflows`), `4c186103` (audit
unreleased changelogs — CHANGELOG only), `5416b183` (approve contributor petrroll
— `.github/APPROVED_CONTRIBUTORS`). No new boundary questions.

## Drift at last sync check (2026-07-10) — pin advanced to 81de5702

**Caught up to `81de5702`.** Delta `4285712b → 81de5702` fully processed: 32
main-line changes — **5 ports (→ 5 Go commits), 1 already-faithful/no-code, 18
n/a, 0 decides**. **Release crossed — v0.80.4 / v0.80.5 / v0.80.6**: both
`pi-ai` and `pi-coding-agent` bump **0.80.3 → 0.80.6**. The catalog byte-golden
was regenerated (**411,270 B**) and two new schema shapes (`cost.tiers`,
thinking level `max`) landed on the Go structs before the regen so no catalog
key is dropped. gofmt clean; build/vet/`-race` green; differential 37/37.

- **retry gRPC ResourceExhausted** (`57d96d72`, #6449, Go `2bc26c1`): adds
  `"ResourceExhausted"` to `retryableProviderErrorPattern` in
  `ai/retry_classify.go` (gRPC providers like NVIDIA NIM). **Latent** under the
  2026-06-25 ruling; same class as the 524 / Bun-socket ports. Test uses pi's
  exact NIM message, mutation-verified non-vacuous.
- **ignore stale usage after compaction** (`8973ae28`, Go `afcfb0e`):
  `ai/estimate.go getLastAssistantUsageInfo` now forward-walks tracking the
  latest prefix timestamp and only accepts an assistant's usage when it is at
  least as new as every earlier message (a compaction summary inserted after a
  response invalidates that response's usage). New `messageTimestamp` helper.
  Two tests mirror pi's new vitest cases; a pre-existing anchor test gained a
  realistic assistant timestamp (assertions unchanged).
- **max thinking level + input-based pricing tiers** (`fbdd4638` + `a9ecf301`,
  Go `6acea45`, both touch `ai/types.go`+`ai/models.go`): (1) `ThinkingMax`
  `"max"` added above `xhigh`, opt-in via `thinkingLevelMap`, gated in
  `GetSupportedThinkingLevels` exactly like `xhigh`. (2) `ModelCost` gains
  optional `Tiers` (`ModelCostTier`); `CalculateCost` selects the highest tier
  whose `inputTokensAbove` the total input strictly exceeds, else base rates;
  `ai/providers/openai_responses.go` parses `cache_write_tokens`, subtracts both
  cached and cache-write tokens from input (clamped ≥0), surfaces cache-write as
  `cacheWrite`. Cost is **not** a byte-golden (Usage isn't serialized), but
  `cost.tiers` ships in the catalog, so the struct had to land first. All tests
  mutation-verified.
- **anthropic empty-text signed thinking block** (`6731a0ba`, #6457, Go
  `5015a8e`): `ai/providers/anthropic.go convertAnthropicMessages` keeps a
  thinking block that carries a real signature even when its text is empty;
  drops only when both are empty. **Request-body golden surface**, but no
  differential scenario drives an empty-text signed block → 37/37 unchanged.
  Mutation-verified against the old drop-on-empty behavior.
- **catalog regen → 0.80.6** (releases `912d0953`/`cc62baa4`/`2b3fda99` + data
  commits `7df2a94e` GPT-5.6, `6c735db0` exclude GPT-5.6 alias, `3664806f`
  GPT-5.4/5.5 long-context pricing, `5b4bda30` refresh, `46145bef` openrouter
  context-length; Go `d321e2b`): `ai/models_catalog.json` re-derived
  byte-identical from the 0.80.6 build's MODELS, `cmp`-clean vs a fresh
  independent re-derivation. All legacy claude-3.x / claude-4-0 anthropic models
  were pruned upstream; the catalog-load smoke test repoints to the dated
  `claude-haiku-4-5-20251001` (maxTokens 64000). No removed id orphaned in Go
  source or defaults (`coding/resolve.go` default `claude-sonnet-4-5` still
  present).

**already-faithful / no Go change (1):** `a6f720e6` (**count custom messages in
compaction budget** — refactors pi's *entry-based* `findCutPoint`/
`findValidCutPoints`/`findTurnStartIndex` in
`coding-agent/src/core/compaction/compaction.ts` to route each `SessionEntry`
through `sessionEntryToContextMessages`, so custom_message/branch_summary
entries now count toward the token budget and act as cut-points/turn-starts).
Go's `coding/compaction.go` already operates on the flattened
`[]agent.AgentMessage`, and `compactionSummaryMessage`/`branchSummaryMessage`
project to `ai.UserMessage` (RoleUser) — so custom/branch/compaction messages
already count in the budget and are already valid cut-points and turn-starts.
pi is converging its entry-based path to the projected-message behavior Go was
built on. Same ruling class as `ba10b60b`/`dd1c690f` (session-context
projection, no `harness/` tree in Go).

n/a (18): **agent-session-runtime / RPC / extensions** — `e9fa5a68` (settled
agent lifecycle event — `core/agent-session.ts` + extensions + rpc +
interactive), `3f9aa5d1` (prompt cache-miss tracking — agent-session +
`cache-stats.ts` + interactive stats). **host-side model-registry / settings /
CLI** — `c6251a86` (modelOverrides to extension providers — `core/model-
registry.ts`, not ported), `1a2542b1` (expand `~` in shellPath —
`settings-manager.ts`), `c4281a7d` (warn when session-id creates a session —
`main.ts`). **unported resource-loader** — `2170363a` (Windows context
file-walk hang). **TUI / interactive** — `1ffca0f2` (align reload descriptions —
docs + interactive + slash-commands + 1 cosmetic agent-session line),
`a98778e2` (fix interactive mode fixture). **test-only / CI / docs / meta** —
`33874659` (isolate service-tier pricing test — `ai/test` only), `53213442`
(gate releases on full tests — scripts), `bf75b8aa` + `1775fe4c` (audit
changelogs), `ef793a98` + `e3513193` + `34582ef3` ([Unreleased] sections),
`050b8176` + `8432c6f2` + `81de5702` (approve contributors). No new boundary
questions.

## Drift at last sync check (2026-07-09) — pin advanced to 4285712b

**Caught up to `4285712b`.** Delta `312bc713 → 4285712b` fully processed: 7
main-line changes — **1 port (→ 1 Go commit), 6 n/a, 0 decides**. **No release
tag crossed** — zero `package.json` bumps in the range; `pi-ai` and
`pi-coding-agent` both stay **0.80.3**, so every npm byte-golden (catalog,
session tree, image decisions, differential request diff) is untouched.
Reviewed via independent go-review (ship, no findings) + adversarial parity
review (faithful). gofmt clean; build/vet/`-race` green; differential 37/37
(the port touches only the retry classifier, not the request builder).

- **retry Bun socket drops** (`4285712b`, PR #6431, Go `369aca7`): adds
  `"socket connection was closed"` to `retryableProviderErrorPattern` in
  `ai/retry_classify.go`, mirroring pi's addition to
  `RETRYABLE_PROVIDER_ERROR_PATTERN` in `packages/ai/src/utils/retry.ts`
  (inserted after `"socket hang up"`, before `"timed? out"` — same source
  position). Catches Bun's `fetch()` socket-drop wording (`"The socket
  connection was closed unexpectedly. For more information, pass \`verbose:
  true\` in the second argument to fetch()"`). **Latent** under the 2026-06-25
  ruling (future `retry.ts` pattern additions are `port`; no Go consumer yet —
  the assistant-turn auto-retry loop lives in the unported
  agent-session-runtime), same class as the 07-06 `d53b5676` 524 port. Not a
  golden surface (retry classifier, in-repo test only). Test
  `TestIsRetryableAssistantError/bun_fetch_socket_drop_is_retryable` uses pi's
  exact vitest message (byte-identical) — **mutation-verified non-vacuous**:
  removing the pattern fails only this case, and no pre-existing
  `connection/socket/closed`-family pattern (`other side closed`,
  `connection.?lost`, `websocket.?closed`, …) matches the Bun message.

n/a (6): **agent-harness (unported)** — `dd1c690f` (**session context entry
projection** — refactors `buildSessionContext` in
`packages/agent/src/harness/session/session.ts` into
`defaultContextEntryTransform` + `buildContextEntries` +
`sessionEntryToContextMessages` with optional `entryTransforms`/`entryProjectors`;
**behavior-preserving** — the default LLM message-list projection (compaction
slicing, firstKeptEntryId, message/custom_message/branch_summary projections) is
unchanged, custom entries still omitted from model context by default; the new
capability is projector/extension surface with no Go consumer, and the Go port
has no `harness/` tree — same class as `ba10b60b`/`7198e78f`; Go's
`session_tree.go` stays faithful); `cb222bf9` (**export
InMemorySessionStorage/JsonlSessionStorage**, #6435 — SDK re-export of the
unported harness storage classes from `packages/agent/src/index.ts`). **TUI /
interactive** — `86afffe0` (**fix fork menu double-select**, #6430/#6321 —
`modes/interactive/interactive-mode.ts` only, closes the fork menu before
teardown). **Catalog churn deferred** — `9eedaf8c` (**GitHub Copilot extended
context windows → 1M**, #6439 — `github-copilot.models.ts` metadata:
contextWindow 1000000 for Opus 4.7/4.8, GPT-5.3 Codex/5.4/5.5 — plus
generator-only `generate-models.ts`; no new provider, lands in `[Unreleased]`,
no bump → folds into next release regen); `72d77b53` (**update model
catalogues** — per-provider `*.models.ts` churn across
opencode/openrouter/vercel-ai-gateway/xai, no new providers → next release
regen). **Meta** — `5cb50679` (**approve contributor DeviosLang** —
`.github/APPROVED_CONTRIBUTORS`). **Regen heads-up:** `9eedaf8c` (copilot 1M) +
`72d77b53` (openrouter +54 lines is real data movement) queue on top of the
still-deferred `cc2db980` — at the next release regen re-check
`ai/catalog_load_test.go`'s pinned model id for orphaning. No new boundary
questions.

## Drift at last sync check (2026-07-08) — pin advanced to 312bc713

**Caught up to `312bc713`.** Delta `2b00dade → 312bc713` fully processed: 8
main-line changes — **1 port (→ 1 Go commit), 7 n/a, 0 decides**. **No release
tag crossed** — zero `package.json` bumps in the range; `pi-ai` and
`pi-coding-agent` both stay **0.80.3**, so every npm byte-golden (catalog,
session tree, image decisions, differential request diff) is untouched.
Reviewed via independent go-review (ship; one MED test-coverage finding applied)
+ adversarial parity review (faithful). gofmt clean; build/vet/`-race` green;
differential 37/37.

- **fail tool calls from length-truncated assistant messages** (`351efc82`, PR
  #6285 / fixes #6284, Go `b5a73ea`): a `"length"` stop means the assistant
  output was cut off by the output token limit. Streamed tool-call arguments are
  finalized with a best-effort JSON salvage parser
  (`parseStreamingJSON`/`completePartialJSON`, `ai/providers/json.go`, called on
  the *final* block by anthropic/openai/openai-responses), so a truncated message
  can yield tool calls whose arguments parse and validate but are silently
  incomplete. `agent/loop.go runLoop` now dispatches a `ai.StopLength` turn to a
  new `failToolCallsFromTruncatedMessage`, which emits the usual per-call
  `tool_execution_start` → `tool_execution_end` → `message_start`/`message_end`
  sequence with an error tool result and returns `terminate: false`, so the loop
  continues and the model can re-issue the calls. **Model-visible string surface**
  (tool-result content), byte-exact vs pi's template literal — `%s`, not `%q`,
  since pi interpolates the tool name unescaped. **No byte-golden touched**: the
  diff is confined to `agent/`, no request builder or session-format code; no
  differential scenario drives a `StopLength` turn with tool calls, so 37/37
  unchanged, and no pre-existing test pinned the old behavior. **Live, not
  latent** — all four ported providers map a `"length"` finish reason to
  `ai.StopLength` (`anthropic.go:949`, `openai.go:1125`,
  `openai_responses.go:1000`, `google.go:858`), unlike the latent
  retry-classifier ports. Test `TestAgentFailsToolCallsFromLengthTruncatedMessage`
  mirrors pi's new vitest case and asserts strictly more: **two** tool calls (the
  contract is that *every* call fails), in order, with the byte-exact string and
  the start→end-per-call emission sequence. Mutation-verified non-vacuous on four
  axes (revert the guard → the tool executes with truncated `"hel"`; collapse the
  loop to one result → 1 end event; batch the starts → sequence mismatch; perturb
  the string → text mismatch).
  *Note (accepted, matches pi):* `terminate: false` means a model that
  deterministically hits `StopLength` while emitting a tool call re-prompts
  without a loop cap — the only brake is a caller-supplied `ShouldStopAfterTurn`.
  Upstream behaves identically, so this is faithful, not a divergence.

n/a (7): **catalog churn deferred** — `cc2db980` (**refresh Xiaomi token plan
model catalogs** — per-provider `*.models.ts` churn across
bedrock/anthropic/fireworks/huggingface/mistral/opencode/openrouter/xiaomi×3, plus
a **generator-only** `generate-models.ts` change: the three `xiaomi-token-plan-*`
providers now source from their own models.dev entries instead of cloning
`data.xiaomi`, dropping the `mimo-v2-flash` skip hack. No new providers
registered. Lands in `[Unreleased]` with no version bump → folds into the next
release regen, same class as `ee24a9ec` / `3d6acb37` / `9cd2c81a`. **Heads-up for
that regen:** `anthropic.models.ts` is −176 lines and `openrouter.models.ts`
+140 — expect real catalog data movement, and re-check `ai/catalog_load_test.go`'s
pinned model id for orphaning, as at the 0.80.3 regen). **Unported agent-harness**
— `7198e78f` (**custom metadata in jsonl session headers**, #6417 — purely
additive optional `metadata?: Record<string, unknown>` through
`packages/agent/src/harness/session/{jsonl-repo,jsonl-storage}.ts` +
`harness/types.ts`; there is no `harness/` tree in the Go port, whose session
storage mirrors coding-agent `session-manager.ts` — same ruling class as
`1dac0990`). **Host/CLI + TUI + OAuth** — `312bc713` (**provider arguments for
login** — the only core-path hunk is `core/slash-commands.ts` adding an optional
`argumentHint?: string` and two hint values for `/model` + `/login`; Go has no
slash-command table, and the remaining ~230 lines are `modes/interactive/*`
oauth-selector + login args); `62f45bad` (**fix native clipboard in bun release**,
#6418 — `coding-agent/src/utils/clipboard-image.ts` + `scripts/build-binaries.sh`;
`utils/` is in scope only if a ported core file consumes it, and nothing in Go
touches clipboard — the rest is bun packaging); `8a2ce5a5` (**decrement paste
counter on paste marker delete and terminal clear**, #6397 —
`packages/tui/src/components/editor.ts` only). **Meta** — `d1da5836` + `4ea062f9`
(**approve contributors ArcadiaLin / anilgulecha** —
`.github/APPROVED_CONTRIBUTORS`). No new boundary questions.

## Drift at last sync check (2026-07-07) — pin advanced to 2b00dade

**Caught up to `2b00dade`.** Delta `647c5554 → 2b00dade` fully processed: 9
main-line changes — **1 port (→ 1 Go commit), 1 already-faithful/no-code, 7
n/a, 0 decides**. **No release tag crossed** — zero `package.json` bumps in the
range; `pi-ai` and `pi-coding-agent` both stay **0.80.3**, so every npm
byte-golden (catalog, session tree, image decisions, differential request diff)
is untouched. Reviewed via independent go-review (ship, no findings) +
adversarial parity review (faithful; both new tests mutation-verified
non-vacuous). gofmt clean; build/vet/`-race` green; differential 37/37.

- **`(no tool output)` placeholder for empty tool results without images**
  (`279f53b0`, PR #6290, Go `2d523db`): `ai/providers/openai.go` and
  `ai/providers/openai_responses.go` emitted `"(see attached image)"` for **any**
  empty tool result, even with no image content, making the model hallucinate
  attachments for commands that produce no output (e.g. `curl -s` with SSL
  errors, `grep` with no matches, `true`). Both now consume the already-computed
  `hasImages` flag: empty + images → `"(see attached image)"`, empty + no images
  → `"(no tool output)"` — converging to the Google provider's existing
  three-way pattern (`ai/providers/google.go`). **Request-body golden surface**
  (tool-result content on openai-completions + openai-responses), but a no-op for
  every differential scenario: the one placeholder scenario
  (`TestDiffToolResultImagesEmitUserMessage`) is image-bearing and still expects
  `"(see attached image)"`; no scenario sends an empty no-image result, so 37/37
  unchanged. The **azure-openai-responses** half is unported (azure out of
  scope). Tests `TestOpenAIEmptyToolResultNoImagePlaceholder` +
  `TestResponsesEmptyToolResultNoImagePlaceholder` mirror pi's two new vitest
  cases — mutation-verified non-vacuous.
- **null-content ingestion normalization** (`8c0ccd14`, #6343/#6259/#6276) —
  **already-faithful, no Go change.** pi normalizes null/missing message
  `content` → `[]` at ingestion choke points (`transform-messages.ts`,
  agent-loop `createToolResultMessage`, session-entry loading) to guard the JS
  `content is not iterable` crash from untyped extension tools / hand-edited
  session files. Go is structurally immune: `ai.ContentList.UnmarshalJSON` maps
  null/missing content → nil (no error), a nil `ContentList` ranges as empty
  everywhere (transform.go, providers, session_tree.go) so the crash cannot
  occur, and `ContentList.MarshalJSON` re-emits nil as `[]` — byte-identical to
  pi's normalized output on round-trip. The `agent-session.ts` pieces (extension
  `message_end` replacement, `sendCustomMessage`, custom-message ingestion) are
  unported (extensions + agent-session-runtime).

n/a (7): **coding-agent host/interactive** — `6efc09b7` (**clear label timestamp
cache on new sessions**, #6354 — 1-line `session-manager.ts`
`labelTimestampsById.clear()`, interactive-label-cache housekeeping, no Go
consumer); `c8ada4e7` (**improve project-local pi config**, #6309 —
`package-manager.ts` / `cli/args` / interactive `config-selector` /
`settings-manager.ts` (2-line, deliberately-not-ported), host/CLI config
surface); `b3dff19a` (**InlineExtension type**, #6267 — extensions runtime + SDK
exports, no Go extension runner). **Extensions runtime** — `244f1dea`
(**`before_provider_headers` extension hook**, #6350 — additive extension
capability in the unported `extensions/runner.ts` + `sdk.ts`; header-*sending*
is ported but the mutation *hook* is unported extensions-runtime, same class as
prior `before_provider_request` rulings — no new provider/tool, not a decide);
`2b00dade` (**Revert "abort stuck context hooks"** — reverts `67575615`, itself
judged n/a as extensions runner on 2026-07-01; revert stays n/a). **CI/meta** —
`4087346d` (**persist issue analysis auth refresh** —
`.github/workflows/issue-analysis.yml` only); `cfaa52e1` (**approve contributor
affanali2k3** — `.github/APPROVED_CONTRIBUTORS`). No new boundary questions.

## Drift at last sync check (2026-07-06) — pin advanced to 647c5554

**Caught up to `647c5554`.** Delta `114bacf3 → 647c5554` fully processed: 24
main-line changes — **3 ports (→ 3 Go commits), 21 n/a, 0 decides**. **No
release tag crossed** — zero `package.json` bumps in the range; `pi-ai` and
`pi-coding-agent` both stay **0.80.3**, so every npm byte-golden (catalog,
session tree, image decisions, differential request diff) is untouched.
Reviewed via independent go-review (ship; one LOW stale-provenance-comment nit
— applied) + adversarial parity review (all 3 faithful; caught + fixed a
vacuous 524 test). gofmt clean; build/vet/`-race` green; differential 37/37.

- **retry Cloudflare 524 timeouts** (`d53b5676`, Go `4290803`): adds `"524"`
  (Cloudflare origin-timeout status, #6239) to `retryableProviderErrorPattern`
  in `ai/retry_classify.go`, mirroring pi's addition to
  `RETRYABLE_PROVIDER_ERROR_PATTERN` in `packages/ai/src/utils/retry.ts`.
  **Latent** under the 2026-06-25 ruling (future `retry.ts` pattern additions
  are `port`; no Go consumer yet — the assistant-turn auto-retry loop lives in
  the unported agent-session-runtime). Test `"524 status code (no body)"`
  (matching pi's own test string) — **mutation-verified non-vacuous**: the first
  attempt used `"error 524: origin timed out"`, which the parity review flagged
  as vacuous (the pre-existing `"timed out"` entry already matched it), so it
  was tightened to a string only the 524 entry catches.
- **clamp OpenAI Responses max output token floor** (`2e4ad6a0`, Go `2aa1b08`):
  `ai/providers/openai_responses.go` now floors `max_output_tokens` at **16**
  (const `openaiResponsesMinOutputTokens`) inside the existing `!= 0`
  truthiness guard, mirroring pi's `Math.max(options.maxTokens, 16)` in
  `openai-responses.ts buildParams` (#6265 — the Responses API rejects lower).
  **Request-body golden surface**, but a no-op for every differential scenario
  (none sends `maxTokens` < 16; 37/37 unchanged). The **azure-openai-responses**
  half is unported (azure provider deliberately out of scope). Test
  `TestResponsesMaxTokensFloor` (8→16, at-floor 16 passes through) —
  mutation-verified non-vacuous.
- **remove Vercel AI Gateway attribution** (`83cbfc65`, Go `50ccbdd`): drops the
  `vercel-ai-gateway` branch (the `http-referer: https://pi.dev` / `x-title: pi`
  headers) + its host const + `isVercelGatewayAttributionModel` from
  `ai/providers/attribution.go`, matching pi's removal in
  `core/provider-attribution.ts` (port-it under the 2026-06-16 attribution
  ruling). A vercel model now falls through to no attribution (== pi's
  `undefined`); remaining branches (openrouter/nvidia/cloudflare) keep upstream
  ordering + byte-exact values. **Header golden surface** (in-repo attribution
  tests only — no npm byte-golden pins vercel). The four tests that pinned the
  Vercel headers (`TestAttributionVercelGatewayNone`, `…ResponsesVercelNone`,
  `…GoogleVercelNone`, host-detection row) now assert **absence** —
  mutation-verified non-vacuous. Provenance comments in both files bumped to
  `83cbfc65`.

n/a (21): **catalog churn deferred** — `ee24a9ec` (**refresh generated model
catalogs** — per-provider `*.models.ts` cost/metadata churn across
bedrock/cerebras/copilot/nvidia/opencode/openrouter/together/vercel; lands in
`[Unreleased]` with no version bump, folds into the next release regen, same
class as prior deferred catalog commits). **Already-faithful (golden-adjacent,
no Go change)** — `a1b336d7` (**allow extra edit replacement fields** —
`core/tools/edit.ts` drops `additionalProperties: false`; Go's `ai.Object`
already omits `additionalProperties` on the edit schema, so the Go tool
definition already matches pi's new permissive shape — pi converging to Go).
**Unported detection/runtime layers** — `21cb3807` (**DS4 context-overflow
pattern** in `utils/overflow.ts` — overflow-detection module isn't ported; its
`isContextOverflow` consumer lives in the unported agent-session-runtime);
`1dac0990` (**short session-entry ids from the uuidv7 random tail** — fixes
`uuidv7().slice(0,8)→slice(-8)` in the unported `packages/agent/src/harness/
session/{jsonl,memory}-storage.ts`; Go's ported entry-id path mirrors
coding-agent `session-manager.ts` `randomUUID().slice(0,8)` (v4, fully-random
first 8 chars), structurally immune to the timestamp-prefix collision this
fixes); `75ac0cb0` (auto-compaction threshold test, unported runtime).
**Behavior-preserving refactor** — `035ea9c8` (**remove redundant record
guards** — the `ai/src/utils/validation.ts` half is a pure `isRecord`
type-narrowing cleanup, no behavior change; Go's `ai/validation.go` already
equivalent; the jsonl-storage half is in the unported agent-harness).
**Unported providers/OAuth** — `23d14626` (Codex websocket rotation),
`8133c94d` (device-code `slow_down` polling, OAuth). **Host/CLI + CI + meta** —
`4a9c962b` (pnpm self-update hint, `package-manager-cli.ts`); the 9-commit
issue-analysis batch (`abe9c9d9`, `d1e72d05`, `3df11fd8`, `010e519c`,
`4728706e`, `190b6459`, `7a92545b`, `fda6451a`, `647c5554` — all
`.github/workflows/issue-analysis.yml` + `.pi/extensions/import-repro.ts`, CI +
repo-own agent config, `.pi/` always n/a); `c9715af3` (APPROVED_CONTRIBUTORS);
`604ac652` (examples/sdk CI); `47830134` (vitest configs + tui). No new boundary
questions.

## Drift at last sync check (2026-07-02) — pin advanced to 114bacf3

**Caught up to `114bacf3`.** Delta `8c943640 → 114bacf3` fully processed: 11
main-line changes — **0 port, 11 n/a, 0 decides**. **No release tag crossed** —
no `package.json` bump in the range; `pi-ai` and `pi-coding-agent` both stay
**0.80.3**, so every byte-golden (catalog, session tree, image decisions,
differential request diff) is untouched. Report-only triage; no Go code changed
(pin advance only). Four changes touched core-adjacent files and were judged
from the real diff:

- **`ba10b60b`** (**add entry renderers for session entries**) — a 174-line
  `core/session-manager.ts` change, but a **behavior-preserving refactor**: it
  extracts `buildContextEntries` + `sessionEntryToContextMessages`, and
  `buildSessionContext`'s LLM **message-list output is unchanged** (compaction
  ordering, firstKeptEntryId slicing, and the message/custom_message/
  branch_summary/compaction→summary/custom→none projections all match the old
  `appendMessage` path). The new capability — preserving non-message entries in
  the selected range + `pi.registerEntryRenderer(customType, renderer)`
  custom-entry rendering — is entirely **interactive-mode/extensions surface**
  with no Go consumer (Go has no TUI/extension runner). Go's `session_tree.go`
  mirror of `buildSessionContext`+`convertToLlm` stays faithful.
- **`f58c1156`** (**serialize split-turn compaction summaries**, #5536) —
  converts pi's two split-turn summaries from `Promise.all` (parallel) to
  sequential (history first, early-return on failure, then turn-prefix). The Go
  port **already does exactly this** (`coding/compaction.go:389-405`); pi is
  converging to the Go behavior. Already-faithful. (Minor: the note at
  `coding/compaction.go:389-390` — "pi runs the two split-turn summaries in
  parallel; we run them sequentially" — is now stale since pi is sequential too;
  cosmetic doc update, port is already correct.)
- **`f8bec25f`** (**surface auth storage save failures**, #6223) —
  `core/auth-storage.ts` host-side credential store (disk write + lock). **No Go
  analog**: Go ports the `packages/ai/src/auth/` resolution layer, not the
  host-side disk store.
- **`ca09b2b1`** (**skip unauthenticated default model**, #6231) — gates
  `findInitialModel`'s saved-default path on `modelRegistry.hasConfiguredAuth`.
  Go **deliberately doesn't port `findInitialModel`** (no settings manager;
  `DefaultModelSpec` fixed-default divergence, `coding/resolve.go:12-16`).

n/a (rest, diffstat-dispatched): `e285e90f` (**remove Copilot Sonnet 5 fallback
in generate-models** — generator-only `scripts/generate-models.ts`; effect lands
in regenerated data, folds into the **next release regen** — expect copilot
catalog data to change then, not a surprise); `e2ccdc85` (**delay Copilot
device-code token polling**, #6187 — `utils/oauth/*`, OAuth not ported);
`114bacf3` (**enable Bedrock prompt caching for Claude 5**, #6235 —
`api/bedrock-converse-stream.ts`, Bedrock provider not ported); `67575615`
(**abort stuck context hooks**, #6234 — `core/extensions/runner.ts`, extensions
runtime not ported); `ec857fec` (**set executionMode: sequential on question
example tool** — `examples/extensions/question.ts`); `45c0fe78` + `9f91da21`
(**approve contributors cyzlmh / xz-dev** — `.github/APPROVED_CONTRIBUTORS`). No
new boundary questions.

## Drift at last sync check (2026-07-01) — pin advanced to 8c943640

**Caught up to `8c943640`.** Delta `9be55bc7 → 8c943640` fully processed: 16
main-line changes — **9 port-class → 2 Go commits, 7 n/a, 0 decides**. **Release
tag crossed — `v0.80.3`**: both `@earendil-works/pi-ai` and `pi-coding-agent`
bump **0.80.2/0.78.1 → 0.80.3**. Reviewed via independent go-review (ship; one
LOW computed-var nit, not applied) + adversarial parity review (both faithful).
gofmt clean; build/vet/`-race` green.

- **Catalog regen → npm 0.80.3** (release `a23abe4a` + catalog commits
  `5c1a2977`, `42063764`, `844d175e`, `1da1cdb2`, `1d061b3f`, `8c943640`, Go
  `23ef141`): `ai/models_catalog.json` re-derived byte-identical
  (**397,575 B**) from the 0.80.3 build's `MODELS` (`JSON.stringify`, insertion
  order); parity independently re-derived and `cmp`-clean (endpoint-pinned at the
  release). Folds in **all** deferred catalog churn since the 0.80.2 regen
  (06-25 `9cd2c81a`, 06-30 `3d6acb37`) plus this cycle's. Notable data:
  **`claude-sonnet-5`** added across anthropic / amazon-bedrock (6 regional ids) /
  openrouter / vercel-ai-gateway / github-copilot, all with
  `compat:{forceAdaptiveThinking:true}` — **no Go code change**: the ported
  anthropic path is catalog-driven (`getAnthropicCompat` reads `model.Compat`,
  `ai/providers/anthropic.go:182`); the bedrock `supportsAdaptiveThinking`
  hardcoded `sonnet-5` add is unported-bedrock (n/a). Fireworks GLM 5.2 Fast
  realigned to GLM 5.2; huggingface/nvidia/together/cloudflare/opencode churn;
  `generate-models.ts` "remove stale metadata fallbacks" (`1d061b3f`/`8c943640`)
  + copilot claude-4/5 routing (`42063764`) are generator-only, effect in the
  regenerated data. **Removed from anthropic:** `claude-3-5-haiku-20241022` +
  `claude-3-5-haiku-latest` — the `ai/catalog_load_test.go` smoke test was
  repointed to `claude-3-haiku-20240307` (maxTokens 4096, still present); no
  orphaned refs elsewhere (`coding/resolve.go` has no haiku default). Schema: all
  12 catalog keys map cleanly onto the Go `Model` struct — no unknown-key drop,
  no type-abort.
- **Bash tool timeout validation** (`cbcf4e04` reject oversized + `85b7c247`
  reject non-positive, Go `91d9fbf`): `coding/tools.go bashTool` now validates
  the `timeout` arg before spawning (mirroring pi's `resolveTimeoutMs` in
  `bash.ts`, which throws first inside `exec`): a non-positive timeout →
  `"Invalid timeout: must be a finite number of seconds"`, and `timeout*1000 >
  2147483647` (INT32_MAX ms) → `"Invalid timeout: maximum is 2147483.647
  seconds"` (`maxBashTimeoutSeconds` renders byte-identically to JS
  `${MAX_TIMEOUT_MS/1000}`). Both surface as the raw tool error (pi's generic
  `catch` re-throws unchanged). Validation is placed at the top of `Execute`,
  before the cwd `os.Stat`, matching pi's ordering; the old `timeout > 0` gate on
  `context.WithTimeout` relaxes to `hasTimeout` (any survivor is already `> 0`).
  pi's `!Number.isFinite` branch collapses into the `<= 0` rejection — `argFloat`
  only yields finite float64/int from JSON. Tests:
  `TestBashRejectsNonPositiveTimeout` (table over int/float 0/-1/-0.5),
  `TestBashRejectsOversizedTimeout` (message + accepted boundary
  `2147483.647`) — both mutation-verified non-vacuous.

**Release reconciliation (0.80.3 is the first build to publish three previously
deferred latent divergences — all re-checked, none bites a golden):**
- *zai `clear_thinking:false`* (`b91bdd5a`, ported 06-29): **now aligned** — the
  in-repo `TestDiffZaiGLM52ReasoningEffort` already asserts the enabled-payload
  shape that 0.80.3 ships, so publication makes the port correct, not divergent.
- *`Usage.reasoning` `omitempty`* (`d7868b09`, ported 06-25): 0.80.3 is the
  release that publishes it, so pi now emits `reasoning:0` for
  openai-completions/responses/google. **Still unpinned:** the sessparity goldens
  project only `{role,text}` and imgparity only image-decisions — neither
  serializes `Usage` — and the 6-scenario request diff covers request bodies, not
  response usage. So no golden/test forces a change to stay faithful. It remains a
  **live-but-unpinned persisted-session divergence**; the faithful fix (drop
  `omitempty` but keep anthropic emitting `reasoning` only-when-present — the
  "split anthropic-optional vs others-always" from the 06-25 note) is a
  session-format change with no golden to verify against, so it is deferred to its
  own port+parity cycle rather than bundled into this release regen.
- *Error-body 4000-char truncation micro-divergences* (`6fbeba51`, ported
  06-30): error-path only, pinned by no differential golden — unchanged status.

n/a (7): `e547bb9f` + `fd6659dd` (**prepareNextTurn context / preserve run
prompt**, #6162 — adds a `PrepareNextTurnContext` param + a parallel
`prepareNextTurnWithContext` callback in `packages/agent/src/agent.ts`, consumed
by `agent-session.ts`'s per-turn system-prompt/tools refresh; the Go port has
**no `prepareNextTurn` hook and no agent-session-runtime consumer** — same class
as the compaction-trio / extension-lifecycle rulings); `040f0a51` (**expose model
resolution helpers** — pure refactor extracting `resolveModelScopeWithDiagnostics`
+ index.ts SDK exports, no behavior change; Go has no `resolveModelScope`/
SDK-export layer, only the ported `parseModelPattern`/`ResolveModelPattern`);
`a3cc169d` (**avoid codex user-agent race**) + `0ac3cfe0` (**zstd codex SSE
transport**) — Codex provider only (`openai-codex-responses.ts`), deliberately not
ported; `f98a154d` (**docs: audit changelog**) + `dd87c02c` (**add [Unreleased]
section**) — changelog meta. No new boundary questions.

## Drift at last sync check (2026-06-30) — pin advanced to 9be55bc7

**Caught up to `9be55bc7`.** Delta `541d11f7 → 9be55bc7` fully processed: 8
main-line changes — **1 port (→ 1 Go commit), 7 n/a, 0 decides**. **No release
tag crossed** — no `package.json` bump in the range; `pi-ai` stays **0.80.2**
and `pi-coding-agent` stays **0.78.1**, so every byte-golden (catalog, session
tree, image decisions, differential request diff) is untouched (differential
request diff re-confirmed 6/6 — the port touches only error-formatting paths,
not the request builder). Reviewed via independent go-review (ship; 2 LOW
cosmetic nits, not applied) + adversarial parity review (faithful). gofmt clean;
build/vet/`-race` green.

- **surface provider HTTP error body** (`6fbeba51`, PR #5832 / #5763, Go
  `835dbac`): pi added `normalizeProviderError`/`formatProviderError`
  (`packages/ai/src/utils/error-body.ts`) to recover HTTP status+body from the
  JS provider SDKs' opaque error objects, a 4000-char body cap, and an
  openrouter `metadata.raw` double-print guard. **Architecture gap:** the Go
  port issues raw HTTP requests and already holds `resp.StatusCode` + the raw
  body (`io.ReadAll` → `formatProviderError(label, status, body)`), so the
  entire SDK-field-probing layer (`extractStatus`/`extractBody`/`pickBodyText`/
  `messageCarriesBody`) is **N/A** and the #5763 "opaque, no body" bug
  structurally cannot occur. Only the two architecture-independent behaviors
  were ported: (1) **4000-char body truncation** (`maxProviderErrorBodyChars`
  + `truncateErrorText` in `ai/providers/errors.go`, UTF-16 code-unit counting
  via `unicode/utf16` to match JS `.length`/`.slice`, suffix
  `"... [truncated N chars]"` byte-exact), applied in `formatProviderError` and
  `openaiSDKErrorMessage`; (2) the **openrouter `metadata.raw` dedup guard**
  (`ai/providers/openai.go` — append only when `!strings.Contains(err.Error(),
  raw)`). `ai/providers/google.go` is comment-only: parity confirmed the
  `@google/genai` `ApiError` carries no `.body`/`.error` field
  (`message=JSON.stringify(errorBody)`, `messageCarriesBody=true`), so pi's
  google path returns the message unchanged — a **verified no-op on the wire**.
  Tests: `TestTruncateErrorText` (incl. astral/UTF-16), `TestFormatProviderError
  Truncation`, `TestOpenAISDKErrorMessageTruncation`, `TestOpenRouterMetadataRaw
  Dedup` (all mutation-verified non-vacuous).
  **Two non-blocking divergences, neither pinned by any golden (no published
  build emits this yet — `[Unreleased]`):**
  - *Astral-boundary micro-divergence:* if the 4000th UTF-16 unit splits a
    surrogate pair, JS `.slice` emits a lone high surrogate where Go's
    `utf16.Decode` emits `�`. Pathological (body must exceed 4000 units
    AND straddle 3999–4000); pi does not surrogate-sanitize the error string
    downstream, so it would survive into a recorded error turn, but no golden
    pins it. Accepted.
  - *Pre-existing openai-completions error-shape divergence (NOT introduced by
    this commit):* for openai-completions, pi's `openai` SDK `APIError` sets
    `messageCarriesBody=false`, so pi now surfaces `<status>: <stringified
    error.error object>` (and its dedup guard suppresses the `metadata.raw`
    append because raw is already inside that object). Go instead surfaces
    `OpenAI API error <status>: <parsed .error.message>` and appends
    `\n<raw>`. `6fbeba51` did not change Go's message shape — only added the cap
    and the guard — so this is a documented pre-existing divergence; the new
    `TestOpenRouterMetadataRawDedup` correctly asserts the **Go** shape. Revisit
    only if a future published build pins a provider error string in a golden.

n/a (7): `5d499272` (**stabilize interactive status indicators**, #6026 —
`modes/interactive/*` + tests, TUI); `927e9806` (**fix compaction event
regression test** — 1-line test edit in `coding-agent/test` for the unported
agent-session compaction); `3d6acb37` (**regenerate model catalog**, #6138 —
per-provider `*.models.ts` cost/metadata churn incl. Xiaomi MiMo pricing, all
existing providers, **deferred**: lands in `[Unreleased]` with no npm publish,
folds into the next release regen — no new provider/behavior, so not a decide);
`939c39ab` (**emit session name changes to extensions**, #6175 —
`setSessionName`→`_extensionRunner.emit` + `SessionInfoChangedEvent` type
re-exports; extension-event lifecycle on agent-session-runtime, same class as
the compaction-trio / `5b9b70d2` rulings — Go has no name-write/extension-runner
consumer); `2117b61c` (**handle undici mid-stream client errors**, #6133 —
`core/http-dispatcher.ts`, a Node/undici `EventEmitter` unhandled-`error` crash
workaround; a runtime defect Go's `net/http` does not have, same class as the
omitted bun `/proc/self/environ` fallback); `6564d947` + `9be55bc7`
(**configurable assistant output padding**, #6168 — `core/settings-manager.ts`
(deliberately-not-ported list) + `modes/interactive/*`). No new boundary
questions.

## Drift at last sync check (2026-06-29) — pin advanced to 541d11f7

**Caught up to `541d11f7`.** Delta `5a073885 → 541d11f7` fully processed: 6
main-line changes — **1 port (→ 1 Go commit), 5 n/a, 0 decides**. **No release
tag crossed** — the Z.AI fix's CHANGELOG entry lands in upstream `[Unreleased]`,
no `package.json` bump; `pi-ai` stays **0.80.2** and `pi-coding-agent` stays
**0.78.1**, so every byte-golden (catalog, session tree, image decisions,
differential request diff) is untouched. Reviewed via independent go-review
(ship, no findings) + adversarial parity review (faithful). gofmt clean;
build/vet/`-race` green.

- **preserve Z.AI thinking content** (`b91bdd5a`, Go `692984a`): the zai
  `thinkingFormat` enabled payload in `applyReasoningFormat`
  (`ai/providers/openai.go:914-924`) now carries `clear_thinking:false` alongside
  `type:"enabled"` (#6083); the disabled payload stays bare `{type:"disabled"}`.
  Mirrors `openai-completions.ts buildParams`'s ternary on
  `options?.reasoningEffort` (Go's `enabled := level != ""`). **Request-body
  golden surface** (zai-format models with effort), but **no latent divergence**:
  the published 0.80.2 build still emits the bare shape, yet no zai-with-effort
  request body is pinned in any 0.80.2-derived golden or differential scenario
  (the in-repo `TestDiffZaiGLM52ReasoningEffort` is now aligned to the
  `[Unreleased]` shape). Test: `TestDiffZaiGLM52ReasoningEffort` tightened
  (`clear_thinking==false` when enabled, key absent when disabled;
  mutation-verified non-vacuous).

n/a (5): `234c2ad5` (**get_entries/get_tree RPC commands**, #6078 —
`modes/rpc/*` + docs + test, plus a one-line `index.ts` re-export of the
`SessionTreeNode` type; RPC mode is host/CLI surface, same class as the 06-27
orchestrator `rpc-entry.ts` ruling, no ported behavior); `a8c692c7` (**avoid
pre-prompt compaction continue**, #6074 — only `core/agent-session.ts`, removes a
pre-prompt `agent.continue()`/`_handlePostAgentRun` loop; agent-session-runtime,
deliberately not ported, consistent with the compaction-trio rulings);
`54113731` (**HTTP timeout for Codex SSE headers**, #4945 —
`openai-codex-responses.ts` only, Codex provider unported); `8f64353e`
(**restrict bot gate bypasses**, #6127 — `.github/workflows/*`); `541d11f7`
(**approve contributor skhoroshavin** — `.github/APPROVED_CONTRIBUTORS`,
contributor-approval meta). No new boundary questions.

## Drift at last sync check (2026-06-28) — pin advanced to 5a073885

**Caught up to `5a073885`.** Delta `622eca76 → 5a073885` fully processed: 2
main-line changes — **0 port, 2 n/a, 0 decides**. **No release tag crossed** —
`pi-ai` stays **0.80.2** and `pi-coding-agent` stays **0.78.1** (both CHANGELOG
entries land in `[Unreleased]`, no `package.json` version bump); no
`models.generated` regen, so every byte-golden (catalog, session tree, image
decisions, differential request diff) is untouched. Report-only triage; no Go
code changed (pin advance only).

n/a (2): `f2e9d753` (**preserve backslash escapes in user messages**, #6105 —
TUI-only: a markdown-rendering escape fix in `tui/src/components/markdown.ts` +
its test, plus the `modes/interactive/components/user-message.ts` caller; modes/
TUI surface, no SDK/agent/coding-agent core, no golden surface);
`5a073885` (**add external editor setting**, #6122 — host/TUI config feature:
`core/settings-manager.ts` (settings-manager — on the deliberately-not-ported
list) gains an external-editor preference consumed only by the interactive mode
(`modes/interactive/extension-editor.ts` + `interactive-mode.ts`), plus docs. No
ported behavior, no provider/tool/request-body change). No new boundary
questions.

## Drift at last sync check (2026-06-27) — pin advanced to 622eca76

**Caught up to `622eca76`.** Delta `1d486163 → 622eca76` fully processed: 2
main-line changes — **0 port, 2 n/a, 0 decides**. **No release tag crossed** —
`pi-ai` stays **0.80.2** and `pi-coding-agent` stays **0.78.1**; no
`models.generated` regen, so every byte-golden (catalog, session tree, image
decisions, differential request diff) is untouched. Report-only triage; no Go
code changed (pin advance only).

n/a (2): `87ad8243` (**pi orchestrator** — new `packages/orchestrator/`
experimental package: a host-side multi-process supervisor with IPC/RPC/
supervisor/radius/storage, entirely additive. The only `coding-agent/src` touch
is the brand-new `rpc-entry.ts`, a 12-line `--mode rpc` process entrypoint —
host/main/CLI surface, same class as the unported `main.ts`/modes wiring. The
`"version": "0.80.2"` is just the new package declaring itself; no `pi-ai` bump,
no catalog regen, no tag. Not a boundary question: it's a separate supervisor
process that doesn't make any non-ported area load-bearing for the SDK and adds
no new provider/tool on the ported `ai`/`agent`/`coding-agent` core);
`622eca76` (**installer lock generation** — packaging/CI/release tooling: new
`coding-agent/install-lock/` package, a `generate-coding-agent-install-lock.mjs`
script, a `build-binaries.yml` change, and one line in `scripts/release.mjs`
adding `install-lock:coding-agent` to the release-artifact regen. No ported
behavior, no version bump, no tag). No new boundary questions.

## Drift at last sync check (2026-06-26) — pin advanced to 1d486163

**Caught up to `1d486163`.** Delta `09f10595 → 1d486163` fully processed: 6
main-line changes — **1 port (→ 1 Go commit), 5 n/a, 0 decides**. **No release
tag crossed** — all 6 land on upstream `[Unreleased]`; `pi-ai` stays **0.80.2**
and `pi-coding-agent` stays **0.78.1**, so every byte-golden (catalog, session
tree, image decisions, differential request diff) is untouched. Reviewed via
independent go-review (ship, no findings) + adversarial parity review (faithful;
the default-model lock mutation-verified non-vacuous). gofmt clean;
build/vet/`-race` green.

- **OpenAI default model** (`77428858`, Go `c83f84f`): `defaultModelPerProvider`
  in `coding/resolve.go` advances openai `gpt-5.4 → gpt-5.5`, matching pi's
  `model-resolver.ts`. **Only openai moves** — `azure-openai-responses` and
  `github-copilot` stay `gpt-5.4`, and `openai-codex` was already `gpt-5.5`. This
  is the per-provider template id `buildFallbackModel` clones when synthesizing a
  custom-id model under a known provider; it lives in pi-coding-agent surface (not
  the pi-ai catalog), so no npm catalog regen is involved. Test:
  `TestDefaultModelPerProviderOpenAI` (locks the four openai-family ids).

n/a (5): `e454f50b` (allow session id for no-session runs — `SessionManager.inMemory`
gains an id option + `main.ts` `--no-session`/`--session-id` flag plumbing; host/CLI
surface, and Go's `Session.SessionID` field already accepts an id independent of
persistence — no `validateSessionIdFlags` consumer in the port); `543710f6` +
`0d145e89` (reject invalid session files / shorten the error — pi's `setSessionFile`
now throws on a non-empty unparseable file instead of truncating, but Go's
`ResumeSession` already rejects headerless files without modifying them, so the
fix is already-faithful; the `main.ts openSessionOrExit` console-error+exit and the
error-string wording are host/CLI); `f14b3594` (show length stop errors —
`interactive/components/assistant-message.ts`, TUI); `1d486163` (fix examples +
undici vuln bump — `examples/extensions/*` + `package-lock.json`, packaging). No
new boundary questions.

## Drift at last sync check (2026-06-25) — pin advanced to 09f10595

**Caught up to `09f10595`.** Delta `a2e3e9d8 → 09f10595` fully processed: 13
main-line changes — **5 ports (→ 5 Go commits), 7 n/a, 1 decide (ruled: port)**.
**No release tag crossed** — all 5 ports land against upstream `[Unreleased]`;
`pi-ai` stays **0.80.2**, the npm reference build is unchanged and every existing
byte-golden (catalog, session tree, image decisions, 14/14 differential) stays
valid. Reviewed via independent go-review (ship; one LOW `strings.Join` cleanup
applied) + adversarial parity review (5/5 faithful). Build/vet/`-race` green;
14/14 differential request-body diff byte-identical (the clamp is a no-op for
those scenarios).

- **retry classifier** (`371adcf3`, Go `23d15ef`): new `ai/retry_classify.go`
  exporting `IsRetryableAssistantError` (the two pattern sets byte-faithful incl.
  the 3 new #6019 strings `you can retry your request` / `try your request again`
  / `please retry your request`). **Latent SDK export** — no Go consumer yet
  (Go's `ai/providers/retry.go` is provider-HTTP backoff *within* a stream; the
  assistant-turn auto-retry loop that consumes this lives in the unported
  agent-session-runtime). Adopted per the 2026-06-25 ruling. `isContextOverflow`
  pre-check deliberately left on the consumer side, matching pi. Test:
  `TestIsRetryableAssistantError`.
- **reasoning token counts on Usage** (`d7868b09`, Go `339cb48`): `Usage.Reasoning`
  (`int`, `json:"reasoning,omitempty"`); anthropic sets it only when
  `output_tokens_details.thinking_tokens` present, openai-completions/
  openai-responses/google set it unconditionally (`|| 0`). Tests:
  `TestAnthropicReasoningTokens`, `TestOpenAICompletionsReasoningTokens`,
  `TestResponsesReasoningTokens`.
  **LATENT DIVERGENCE (must reconcile on the next pi release that publishes
  `d7868b09`):** `omitempty` drops `reasoning:0` from session JSON, whereas pi
  emits `reasoning:0` unconditionally for openai-completions/openai-responses/
  google. Acceptable *now* — the published build (≤0.80.2) has no `reasoning`
  field and the existing sessparity goldens (e.g.
  `coding/testdata/sessparity/8_agent_message_roles.json`) carry no `reasoning`
  key, so `omitempty` keeps the port byte-faithful to what real pi currently
  emits. When d7868b09 ships in an npm build: regenerate the sessparity goldens
  from that build AND drop `omitempty` for the reasoning field (or split
  anthropic-optional vs others-always-present), then re-verify the goldens.
- **responses out-of-order reasoning** (`8c9dbffa`, Go `f546acc`): rewrote the
  Responses stream parser from a single `current` pointer to a
  `map[int]*responsesOutputSlot` keyed by `output_index`, with
  `getSlot`/`createSlot`/`getOrCreateSlot`; emitted events now carry the slot's
  stable `contentIndex` (#6009). Response-parse only (request bytes unchanged).
  Faithful behavior shift confirmed against pi: a `function_call` `output_item.done`
  with no prior `added` now create-on-dones (block lands in content, `toolcall_start`
  fires, stop→toolUse) — `TestResponsesFunctionCallDoneWithoutAdded` updated to
  pi's behavior (mutation-verified non-vacuous). New regression:
  `TestResponsesOutOfOrderItemsPreserveReasoning`.
- **BMP read-tool support** (`4cc339f5`, Go `5f9c464`): `isBmp` magic+DIB
  validation (byte-exact offsets), BMP→PNG conversion (Go `x/image/bmp` +
  `png.Encode`) wired into the read tool's `processImage`; tool-description string
  → `(jpg, png, gif, webp, bmp)` (byte-golden, exact); hint/omit strings
  byte-faithful. No golden pins converted PNG bytes. Tests: `TestDetectMimeBMP`,
  `TestDetectMimeInvalidBMPRejected`, `TestReadBMPConvertsToPNG`.
- **clamp streamSimple max tokens** (`09f10595`, Go `4fed697`): new `ai/estimate.go`
  (`estimateContextTokens`; constants `charsPerToken=4`, `estimatedImageChars=4800`;
  `jsStringLength` via `utf16.Encode` = JS `.length`) + `ai/simple_options.go`
  (`ClampMaxTokensToContext`, `contextSafetyTokens=4096`, `minMaxTokens=1`) wired
  into all 4 Go streamSimple providers; anthropic thinkingBudget re-clamp
  `min(budget, max(0, maxTokens-1024))`. **Request-body golden:** streamSimple now
  always sends a clamped `max_tokens = clamp(model.maxTokens)` where it could
  previously omit it — faithful to pi, which flipped its own
  `openai-completions-empty-tools` test the same way. No-op for the existing
  differential scenarios (low-level builders untouched; 14/14 byte-identical).
  Tests: estimate/clamp units + streamSimple `3904`-clamp assertions.

**Catalog regen DEFERRED** (`9cd2c81a`): per-provider `*.models.ts` churn
(huggingface, vercel-ai-gateway, openrouter, minimax) lands in `[Unreleased]` —
no npm publish, so it folds into the next release regen (advance only when a tag
crosses). The `b940c52e` MiniMax shared-budget clamp was net-reverted by
`f78b1637` (source diff = 0); its minimax `maxTokensSharesContextWindow` churn
also folds into the deferred regen.

n/a (7): `3e551faf` (interactive-mode resource/notify ordering — TUI);
`5c76ae40` (extension-stats — startup-timing instrumentation + extensions loader,
no Go analog); `b940c52e` + `f78b1637` (MiniMax clamp add + full revert, net-zero
source); `c29bbc09` (docs/models.md); `6ca7ba7c` (`.github` contributor);
`49956a7c` (`.pi/prompts`). The lone `decide` (`371adcf3`) was ruled **port** on
2026-06-25 (see Rulings). No new boundary questions.

## Drift at last sync check (2026-06-24) — pin advanced to a2e3e9d8

**Caught up to `a2e3e9d8`.** Delta `470a4736 → a2e3e9d8` fully processed: 28
main-line changes — **9 port-tagged (→ 7 Go commits), 19 n/a, 0 decides**.
Three release tags crossed (v0.80.0/v0.80.1/v0.80.2); npm reference build
advanced 0.79.10 → **0.80.2** (each regen supersedes the prior). This cycle
**completes the models-runtime migration** (the `732bb161` follow-through);
much of it cancels intra-cycle (`detectCompat` removed in `129eb460` then
restored in `e1a2dc04` → net unchanged; anthropic compat toggled by `828493b3`
then `6184307c` → net auto-detect removed). Reviewed via independent go-review
(ship, 3 optional LOW nits) + adversarial parity review (all 7 commits
faithful; catalog re-derived byte-identical; 6/6 differential request diff;
all 3 deliberate divergences confirmed observably-faithful — see the 2026-06-24
ruling). Build/vet/`-race` green.

- **Catalog → npm 0.80.2** (`f08e968c`/`1c4a9ba7`/`0201806a`, Go `d2f937d`):
  endpoint-pinned, re-derived byte-identical (386,548 B). +24 (huggingface
  MiniMax/Qwen/GLM via the registration-only `huggingface` provider; opencode
  glm-5.2; openrouter z-ai/glm-5v-turbo), −4 openrouter (no Go refs), 357
  cost/metadata churn. `off:null` tripwires intact (110→111 in `ThinkingLevelMap`).
- **OpenAI Responses terminal events** (`cd95c274`, Go `e7c69ca`):
  `response.incomplete` finalizes like `response.completed`; stream fails with
  "OpenAI Responses stream ended before a terminal response event" if no
  terminal event. Response-parse only. Tests: `TestResponsesIncomplete…`,
  `TestResponsesNoTerminalEventFailsStream`.
- **api-key credentials → auth.json shape** (`49fbe683`, Go `fad8247`):
  `CredentialAPIKey` "api-key"→"api_key"; `Credential.Metadata`→`Env`
  (json `metadata`→`env`). On-disk breaking change (no shim, mirrors pi). Test:
  `TestCredentialAPIKeyJSON`.
- **compaction zero-usage guard** (`cd95c274`, Go `5c6c777`): usage-anchor loop
  already enforced `>0`; comment aligned + `TestUsageEstimateSkipsAllZeroUsage`.
  The agent-session.ts threshold/post-compaction halves are unported
  agent-session-runtime surface (N/A).
- **request-scoped auth overrides** (`ef231c49`, Go `b53482b`):
  `AuthResolutionOverrides{apiKey,env}` + `overlayEnvAuthContext` into
  `resolveProviderAuth`; `applyAuth` resolves through it; `GetAuth` stays
  override-free. The named `auth/resolve.ts` boundary edge. Test:
  `TestResolveProviderAuthRequestOverrides`.
- **anthropic compat → catalog** (`6184307c`, Go `64e5022`): removed
  fireworks/cloudflare auto-detect; defaults `?? true/true/false/true`, catalog
  supplies the rest. Byte-identical for catalog (0 mismatches across 14
  fireworks + 17 cf-anthropic models). `TestAnthropicSessionAffinityRetention`
  re-pinned to supply compat explicitly.
- **header-only client auth + vercel ungate** (`129eb460`, Go `54a254e`):
  `clientAPIKey` "unused" sentinel for authorization/cf-aig-authorization
  header-only auth; `vercelGatewayRouting` no longer baseUrl-gated (byte-safe —
  no catalog model sets routing). Tests: `TestClientAPIKeySentinel`,
  `TestDiffVercelGatewayRouting` (re-pinned).

**Deliberate divergences (2026-06-24 ruling):** `ProviderHeaders`
null-suppression not ported (latent + public-API change) — **retired
2026-08-04, ported with `a24fb9e96`**, cloudflare base-URL/
cf-aig auth kept inline (not relocated), compat `shouldUseBuiltinModels` routing
not ported (globals-stay-compat). All observably byte-identical through the Go
compat-globals consumer path.

n/a (19): docs/CHANGELOG (`15f92260`, `12ace0ba`, `2be6e670`, `526351d9`,
`86528dd9`, `e0007435`, `9096d5f9`, `8277bd68`); CI/packaging (`2285f879`
removed API subpath exports, `c3cfeac0`, `954ec998`, `97820276`, `ec6311be`);
`192fcccd` (extensions-load hint, main.ts); `b3776234` (type rename
`ExecutionEnvExecOptions`→`ShellExecOptions`, behavior-neutral); `828493b3`
(generator/data folds to 0.80.2; bedrock unported; anthropic intermediate);
`63386614` (TUI/benchmark timing); `a2e3e9d8` (**Azure** foundry — provider
excluded). `e1a2dc04` (restore detectCompat) is net-neutral with `129eb460`'s
removal → no Go change. No new boundary questions.

## Drift at last sync check (2026-06-23) — pin advanced to 470a4736

**Caught up to `470a4736`.** Delta `3b561346 → 470a4736` fully processed: 9
main-line changes — **3 ports (incl. the `732bb161` model-registry merge), 6
n/a, 0 decides** (the lone decide resolved to adopt, 2026-06-23 Rulings). No
release tag crossed (`pi-ai` stays 0.79.10; npm reference build unchanged, all
goldens unaffected, no catalog regen). Reviewed via independent pi-go-review
(ship) + pi-parity-review (faithful) on both the env slice and the substrate.

- **`8eeaa2bc` + `2cbce395` — scoped provider env through API-key resolution**
  (Go `1577144`). `GetEnvApiKey`/`FindEnvKeys` thread a scoped `env
  map[string]string` (canonical `ai.ProviderEnvValue`; providers' helper
  delegates), vertex-ADC + bedrock branches included; `withEnvAPIKey`/`Simple`
  pass `opts.Env`. `2cbce395` is a no-code-change passthrough (its
  `resolution.env` is latent upstream — no catalog provider's `resolve()`
  returns env — and Go's `opts.Env` already flows to providers; locked by
  `TestStreamEnvReachesProvider`). Byte-identical requests when `Env` unset.
- **`732bb161` "Merge model-registry into main" — Models runtime + auth
  substrate** (Go `bf7e7bd` + `37dcff5` + `2b164b3`), per noam's 2026-06-23
  **adopt** ruling (maximum parity for the SDK package/structure). Ported pi's
  `packages/ai/src/auth/*` as `auth_*.go` in package `ai` (CredentialStore +
  InMemoryCredentialStore, ProviderAuth/ApiKeyAuth/OAuthAuth, AuthContext,
  EnvAPIKeyAuth/LazyOAuth, resolveProviderAuth with OAuth refresh-under-lock),
  and `packages/ai/src/models.ts` as `models_runtime.go` (`Provider` interface,
  `CreateProvider`, `Models`/`CreateModels`, `GetAuth`/`applyAuth` incl. the
  `2cbce395` env merge, `HasApi`) + `builtins_models.go` (`BuiltinModels` wiring
  catalog + ProviderAuth + ApiProvider streams). Renamed the `Provider` string
  alias → `ProviderId` (pi's Provider→ProviderId), freeing `Provider` for the
  runtime interface. The pre-existing global free functions remain the **compat
  surface** (pi's `/compat`) — consumers unchanged. Deliberate divergences
  (documented): auth as files in package `ai` not a subpackage (import cycle);
  async→synchronous `(T,error)`; errors via `errorStream` not `lazyStream` (G3);
  OAuth *login acquisition* out of scope (interfaces ported, flows not); images
  excluded. Catalog-data reorg (per-provider `*.models.ts`, new providers)
  **deferred to the next release regen** (0.79.10 not re-published). Request
  bytes unaffected (no `openai*.go` request-builder changed → 6-scenario diff not
  required). **No new boundary questions.**

n/a (6): `d2677a63`/`02540acd` docs; `5a8ea0bc` Bedrock scoped AWS profile
(Bedrock provider unported); `6a4813a7` merge (only ai/src file is
`openai-codex-responses.ts` = Codex, unported; rest theme/startup-ui/
session-picker/settings-manager/main.ts = TUI/CLI/host); `7fedc332` session-name
`\r\n` sanitization (write path `appendSessionName`/`appendSessionInfo` is
host/TUI-driven — Go reads `SessionInfo` but has no name-write/rename path;
low-confidence n/a, re-confirm at substrate port time); `470a4736` threaded
session-selector sort (TUI).

## Drift at last sync check (2026-06-22, v0.79.10 cycle)

**Caught up to `3b561346`.** Ledger 2417adb4 → 3b561346 fully processed (14
main-line changes: **2 behavior ports + 1 catalog regen, 11 n/a, 0 decides**).
One release tag crossed (v0.79.10); npm reference build advanced 0.79.9 →
**0.79.10**. Reviewed via an independent idiomatic go-review (ship, three LOW
nits, no action) + adversarial parity review — which caught a real divergence:
the reasoning-details port adopted the buffering but not the same commit's
validation tightening; fixed in `62981f1` and re-verified faithful. Catalog
endpoint-pinned byte-identical both ends; build/vet/`-race` green; differential
request diff 14/14.

- **Catalog → npm 0.79.10** (`8e190066`, Go `c50acfc`): endpoint-pinned
  byte-identical (old ≡ 0.79.9 build, new ≡ 0.79.10, integrity-verified
  `sha512-9jR23…ORuew==`). **+1** (`vercel-ai-gateway/sakana/fugu-ultra`),
  **−1** (`openrouter/anthropic/claude-3.5-haiku`), 17 openrouter entries churn
  cost/maxTokens/contextWindow. `off:null` tripwires intact (fable-5 across
  anthropic/bedrock/cloudflare; moonshotai[-cn]/kimi-k2.7-code[-highspeed]).
  The dropped openrouter id was a resolve-fallback fixture; it now lives only
  under vercel-ai-gateway, so `TestResolveModelProviderPrefixFallsBackToFullID`
  updated to that provider (resolution logic unchanged — pi's registry `.find()`
  lands on the same sole remaining copy).
- **preserve early reasoning details** (`7d0497fd`, Go `4e60155`+`62981f1`):
  openai-completions buffers an encrypted `reasoning_detail` arriving before its
  tool-call block (`pendingReasoningDetails` keyed by id, drained in
  `ensureToolCallBlock` via `applyPendingReasoningDetail`), matching the tool
  call by the byID map instead of an order scan — no longer dropped (#5114).
  `62981f1` ports the same commit's `isEncryptedReasoningDetail` tightening
  (data must be a non-empty string). Response-parse only; request bytes
  unchanged. Golden surface: request body (reasoning_details round-trip,
  unexercised since no request change). Tests: early-arrival + non-string-data.
- **respect nested repo ignore boundaries in find** (`756a4e8f`, Go `46302ad`):
  the pure-Go fd reimplementation now stops outer repo-specific ignore sources
  (.git/info/exclude, ancestor + per-dir .gitignore) at a nested `.git`
  boundary, while the nested repo's own rules still apply and global
  core.excludesFile carries across (boundaryExempt); active only when the
  search root is inside a repo (preserving --no-require-git outside) (#5960).
  grep/rg path unchanged (respectNestedRepos=false). Golden surface: find-tool
  output. Test: TestFindRespectsNestedRepoBoundaries. **Known minor under-reach**
  (pre-existing, flagged by parity review, out of this commit's scope): a
  *nested* repo's own `.git/info/exclude` is not re-rooted (only the outer
  repoRoot's is read) — the nested repo's `.gitignore` IS honored; worth a
  follow-up.
- **n/a (11):** docs (`a61137a6`, `b7908b49`, `5df5a1ce`); changelog cycle
  headers (`329dceb5`); `.github` (`08457404` contributor approval, `5641d6ba`
  issue-triage workflow); `5b9b70d2` adds `reason`/`willRetry` to
  SessionBeforeCompact/SessionCompact **extension events** (agent-session-runtime
  + extensions/types.ts — unported event lifecycle, per the compaction-trio
  rulings); `717a8f95` reverts the selective pi-ai base entrypoints (packaging/
  test, reverting the n/a `0d89a333`); `4f71b2d3` ZAI "Coding Plan (Global)"
  label in provider-display-names (no Go equivalent — display/TUI) + cli/args
  help text; `71ca9b2b` OpenCode-Go GLM-5.2 xhigh effort (data-only, lands
  **post-0.79.10** so deferred to the next regen); `3b561346` tui ctrl+j newline
  default (TUI). No new boundary questions.

## Drift at last sync check (2026-06-22)

**Caught up to `2417adb4`.** Ledger 56b22768 → 2417adb4 fully processed (22
main-line changes: **4 behavior/perf ports + 1 catalog regen, 17 n/a, 0
decides**). One release tag crossed (v0.79.9); npm reference build advanced
0.79.8 → **0.79.9**. Reviewed via an independent idiomatic go-review (ship, one
cosmetic nit applied) + adversarial parity review (5/5 faithful; catalog
endpoint-pinned byte-identical on both ends; tripwire + orphaned-id checks
passed). Build/vet/`-race` green.

- **Catalog → npm 0.79.9** (`615bf2f8`, Go `5d8b72d`): endpoint-pinned
  byte-identical (old ≡ 0.79.8 build, new ≡ 0.79.9, integrity-verified). 0
  added, **2 removed** (`google/gemma-4-E2B-it`, `gemma-4-E4B-it`; no Go refs),
  20 changed. Subsumes the two data-only commits `8597ebaf` (openrouter
  z-ai/glm-5.2 `xhigh:xhigh`) and `500b568b` (fireworks glm-5p2 →
  api openai-completions, `/inference/v1`, compat, thinkingLevelMap); rest is
  cost/metadata churn. `off:null` tripwires intact (fable-5; moonshotai/-cn
  kimi-k2.7-code[-highspeed]).
- **chat-template thinking compat** (`8b97e75c`, Go `3c30dd2`+`56c73b7`): new
  openai-completions `thinkingFormat:"chat-template"` emitting configurable
  `chat_template_kwargs` ($var/omitWhenOff/scalar). **Latent** — no 0.79.9
  catalog model sets it (reachable only via custom model config); key order
  preserved for byte-exact request bodies. Golden surface: request body
  (unexercised by the 6-scenario diff until a model adopts it). Host-side
  model-registry schema + mergeCompat stay unported.
- **fuzzy edit preserves untouched lines** (`128330e3`, Go `18ef9eb`): fuzzy
  edits no longer globally normalize the file — only touched line-blocks are
  rewritten, other lines copied back verbatim. Golden surface: edit-tool file
  output.
- **legacy WSL bash via stdin** (`1287b69f`, Go `9f452a1`): System32/Sysnative
  `bash.exe` → `bash -s` + command on stdin (mishandles `-c` quoting). Windows
  legacy-WSL only; resolve-config-value half is host-side (n/a).
- **session branch traversal linear** (`a1da88ae`, Go `a88ef3b`): O(n²) prepend
  → append+reverse. Behavior-neutral.
- **n/a (17):** issue-triage automation/.github (`783571a6`, `47d1d90a`,
  `226a3168`, `416c673d`, `350ac3f3`); TUI (`3095977d`, `373cd6ae`,
  `d93b92ba`); changelog (`1aa79b9b`, `b4f31408`); examples (`542683b2`);
  catalog data folded into the 0.79.9 regen (`8597ebaf`, `500b568b`); OAuth +
  host model-registry (`6e6ce70c` Copilot account-availability filtering);
  extensions runtime (`5505316e`); packaging/self-update (`bc0db643`);
  agent-session-runtime reload + TUI (`2417adb4`). No new boundary questions.

## Drift at last sync check (2026-06-19)

**Caught up to `56b22768`.** Ledger 29c1504c → 56b22768 fully processed (32
main-line changes: **0 behavior ports, 32 n/a** for code — the only ported
surface touched is the catalog, advanced via the release regen below; 0
decides). Two release tags crossed (v0.79.7, v0.79.8); npm reference build
advanced 0.79.6 → **0.79.8** (v0.79.7 superseded — each regen supersedes the
prior). Reviewed via an independent adversarial parity review (catalog
endpoint-pinned byte-identical on both ends; authenticity, schema-drift,
tripwire, and orphaned-id checks all passed). Build/vet/`-race` green.

- **Catalog → npm 0.79.8** (`8eb9704b`, Go commit `5164314`): subsumes v0.79.7
  + the data-only generator commits `58dd2f59` (opencode-go GLM-5.2),
  `b09fbde0` (openrouter/fusion alias), and the Mistral prompt-caching data
  from `651d10d9`. Net +9/−3 ids; 44 changed entries are data churn (Mistral
  cost fields, fireworks/openrouter/vercel metadata). `off:null` gates intact
  (claude-fable-5, kimi-k2.7-code) → `TestFable5DisabledThinkingGateLive` and
  `TestDeepseekDisabledThinkingGateLive` green.
- **No behavior ports.** The substantive non-release changes all landed on
  unported surface: the compaction trio (`6b9f3f49` overflow-retry recovery,
  `7d08c81a` empty-summary guard / event reordering, `c60f6a8a`
  `estimatedTokensAfter`) edits the agent-session-runtime auto-compaction
  orchestration + `CompactionResult`/`compaction_*` event lifecycle, none of
  which the Go port has (it compacts inline via `shouldCompact`/`compact` with
  no overflow recovery or event emission); RPC unknown-command id
  (`51f75235`) → `modes/rpc` unported; Mistral prompt caching (`651d10d9`,
  provider code) → Mistral provider unported; `CONFIG_DIR_NAME` / edit-diff
  SDK exports (`008c76f9`, `2b46f388`) → no behavior change; selective pi-ai
  entrypoints (`0d89a333`) → packaging. No new boundary questions.

## Drift at last sync check (2026-06-17)

**Caught up to `29c1504c`.** Ledger f8a77f47 → 29c1504c fully processed (20
main-line changes: 3 ported, 16 n/a, 1 decide resolved). Two release tags
crossed (v0.79.5, v0.79.6); npm reference build advanced 0.79.4 → 0.79.6.
Reviewed via independent go-review (ship) + adversarial parity review
(4/4 faithful; request diff regenerated from the 0.79.6 build, 12/12 — 6
standard + the 4 new GLM-5.2 scenarios; null-content regression test
mutation-verified non-vacuous).

- Ports: `75b0d723` (Z.AI GLM-5.2 native reasoning_effort — `788c832`),
  `2d597f02` (null Responses content — `e8f7511`, no code change: Go ranges a
  nil slice safely; locked with a regression test), and `31bfb2f1` (catalog →
  npm 0.79.6 — `c2221a7`, subsumes the v0.79.5 catalog + the deepseek-v4 compat
  / cost / maxTokens data churn `2431491c`/`bd9f8773`/`7da475db`).
- **Deferred data landed:** the 0.79.6 catalog ships `off:null` for Kimi K2.7
  Code (`moonshotai`/`moonshotai-cn`, incl. `-highspeed`), activating the
  deepseek disabled-thinking gate ported on 2026-06-15 (`62fa1e3`). The
  `TestDeepseekCatalogNoOffNull` tripwire is now `TestDeepseekDisabledThinkingGateLive`,
  driving the omit end-to-end through the catalog-resolved model.
- **Decide → ruling:** provider-scoped env overrides **ported faithfully**
  (`7f29e7a3` → `872c303`; owner call 2026-06-17, recorded in Rulings).
  `StreamOptions.Env` consulted ahead of `os.Getenv`; Bun `/proc` fallback
  omitted (no Go analog); host-side population machinery stays unported.

### Prior — 93b3b7c1 → f8a77f47 (2026-06-16)

**Caught up to `f8a77f47`.** Ledger 93b3b7c1 → f8a77f47 fully processed (20
main-line changes: 6 ported, 14 n/a, 1 decide resolved). Reviewed via an
adversarial multi-agent workflow: 5/6 parity-faithful + go-review pass +
request diff 6/6 against the 0.79.4 build; the review caught a real attribution
header-precedence divergence (model.Headers must override the attribution
defaults — pi merges them at the bottom of the stack) which was fixed and
re-verified. Shipped as v0.2.2.

- Ports: `0be5bb6c` (anthropic 1h cache-write cost = 2× input), `3fa40956`
  (bash stdout drain past exit — re-armed idle grace), `0369bdb8` (deepseek
  off:null thinking gate — logic; catalog data deferred), `b0c8f65f`
  (gemini-flash-latest alias → MINIMAL thinking — logic; data deferred),
  `bba6af2c` (catalog → npm 0.79.4), and `f8a77f47` + the `provider-attribution`
  module (see ruling).
- **Decide → ruling:** provider-attribution **ported faithfully** (owner call,
  2026-06-16). The SDK now sends pi's attribution headers on the providers pi
  does, gated on `PI_TELEMETRY` (default enabled), at the bottom of the header
  precedence so `model.Headers`/`opts.Headers` override them. Closed a
  pre-existing header parity gap the body-diff never covered.

### Prior — 6f29450e → 93b3b7c1 (2026-06-14)

**Caught up to `93b3b7c1` — no-op cycle (pin advance only, no version bump).**
Ledger 6f29450e → 93b3b7c1: 12 main-line changes, **0 ported, 12 n/a, 0
escalations**. Nothing touched ported behavior code (TUI, packaging/self-update,
extension package-manager, config-migrations/settings, docs/meta/examples). No
release tag in the window, so the npm reference build stays at `pi-ai` 0.79.3
and all goldens are unaffected. **Deferred catalog note:** `21a904f4` flips
`supportsLongCacheRetention:false` for 6 opencode models in generate-models —
the *behavior* is already ported (`openai_compat.go` + `openai.go`), so it's
pure catalog data that will land with the next release regen (no npm build ships
it yet). No code change → no tag; v0.2.1 remains the latest release.

### Prior — 3f44d3e2 → 6f29450e (2026-06-13)

**Caught up to `6f29450e`.** Ledger 3f44d3e2 → 6f29450e fully processed (22
main-line changes: 4 ported, 18 n/a, 0 escalations). Reviewed via an adversarial
multi-agent workflow: 4/4 parity-faithful, idiomatic go-review pass, request
diff 6/6 against the 0.79.3 build, completeness critic mutation-tested every
ported change (each fix is load-bearing). Shipped as v0.2.1.

- Ports: `a455f62f` (anthropic refusal details), `1fc80f4f` (resolve fallback
  Reasoning flip), `daab056a` (agent late-tool-update guard), `f2585c4c`
  (catalog → npm 0.79.3).
- **Fable-5 disabled-thinking gate went LIVE.** The 0.79.3 catalog ships
  `off:null` for `claude-fable-5` (anthropic + cloudflare-ai-gateway + bedrock
  variants), activating the latent 9ccfcd7c gate ported on 2026-06-12. The
  former latency tripwire is now `TestFable5DisabledThinkingGateLive`, which
  drives the omit behavior end-to-end through the catalog-resolved model and
  fails loudly if a future regen drops `off:null`.

### Prior — 130ae577 → 3f44d3e2 (2026-06-12)

**Caught up.** Ledger 130ae577 → 3f44d3e2 fully processed (52 main-line
changes: 9 ported, 43 n/a, 0 open). Releases v0.79.0 + v0.79.1 ingested; the
9ccfcd7c disabled-thinking gate is latent pending upstream's next catalog
regen (tripwire: TestFable5DisabledThinkingGateLive).

## Sync pipeline

Runs as a daily job — the `/pi-sync` skill (`.claude/skills/pi-sync/`)
orchestrates one cycle over everything upstream added since the pin. Each
ledger row is one main-line change (a PR merge carries its full PR diff,
`git diff <sha>^1..<sha>`). Stages, each owned by a dedicated skill:

1. **Triage** (`/pi-triage`) — WHY/WHAT from the real diff, then a SCOPE
   verdict: `port` / `n/a` (with reason) / `decide` (boundary changes are
   escalated to a human, never decided silently).
2. **Port** — each in-scope change lands as an individual Go commit
   referencing the upstream sha (`port(<area>): <subject> (upstream <sha>)`).
3. **Idiomatic review** (`/pi-go-review`) — independent agent verifies the
   port is real Go, not transliterated TypeScript.
4. **Parity review** (`/pi-parity-review`) — independent adversarial agent
   verifies faithfulness against the TS source AND the published npm build
   (build wins on drift); regenerates goldens from the build, never by hand;
   re-runs the differential request diff when provider request code changed.
5. **Gate + record** — full build/vet/`-race` suite green; ledger row filled
   (status, Go commit, notes); pin advanced; pushed.

The reviewers are independent of the porter by design: every parity bug this
project has shipped was pinned in place by the author's own tests and caught
only by comparison against real pi.

Upstream reference clone: `$PI_UPSTREAM_DIR`, default `~/.cache/pi-upstream`.
When the delta crosses a release tag, the npm reference build is refreshed to
that version before parity review.

## Ledger — 470a4736 → a2e3e9d8

| Upstream | Date | Subject | Hint | Status | Go commit | Notes |
|---|---|---|---|---|---|---|
| `129eb460` | 2026-06-23 | feat(ai): complete models runtime migration | review | **ported** | `54a254e` (+catalog `d2f937d`) | The migration consolidation. Most lands via the 0.80.2 catalog regen (per-provider `*.models.ts` reorg, huggingface registration provider). Observable Go slices: `getClientApiKey` "unused" sentinel (`clientAPIKey`, both openai providers) + vercel routing ungate. `detectCompat` removal here is reverted by `e1a2dc04` (net unchanged). ProviderHeaders null-suppression / cloudflare-auth relocation / compat builtin-routing NOT ported — deliberate divergences (2026-06-24 ruling), observably byte-identical through the compat-globals path. |
| `15f92260` | 2026-06-23 | docs(ai): expand models migration guide | likely-n/a | n/a | — | ai/CHANGELOG.md |
| `12ace0ba` | 2026-06-23 | docs(ai): reference README in migration guide | likely-n/a | n/a | — | ai/CHANGELOG.md |
| `2285f879` | 2026-06-23 | fix(ai): remove legacy raw API subpaths | review | n/a | — | package.json export subpaths only (packaging) |
| `cd95c274` | 2026-06-23 | fix(ai): require OpenAI Responses terminal events | review | **ported** | `e7c69ca` (+`5c6c777`) | openai-responses-shared: response.incomplete finalizes like completed; throw on no terminal event (Go `e7c69ca`, response-parse only). Compaction zero-usage guard (Go `5c6c777`); agent-session-runtime halves N/A. |
| `2be6e670` | 2026-06-23 | docs(ai): document bundling behavior | likely-n/a | n/a | — | ai/README.md |
| `192fcccd` | 2026-06-23 | fix(coding-agent): hint when extensions fail to load | review | n/a | — | main.ts extension-load-failure hint — extensions runtime unported |
| `526351d9` | 2026-06-23 | docs: audit unreleased changelogs | likely-n/a | n/a | — | changelogs |
| `f08e968c` | 2026-06-23 | Release v0.80.0 | review | ported (superseded) | `d2f937d` | catalog regen; superseded by 0.80.2 (final build subsumes it) |
| `86528dd9` | 2026-06-23 | Add [Unreleased] section for next cycle | likely-n/a | n/a | — | changelog cycle header |
| `828493b3` | 2026-06-23 | fix(ai): unblock release provider tests | review | n/a | — | generate-models `isTogetherReasoningOnly` (data → 0.80.2 regen); bedrock scoped-profile revert (unported); anthropic compat intermediate (net via `6184307c`) |
| `1c4a9ba7` | 2026-06-23 | Release v0.80.1 | review | ported (superseded) | `d2f937d` | catalog regen; superseded by 0.80.2 |
| `e0007435` | 2026-06-23 | Add [Unreleased] section for next cycle | likely-n/a | n/a | — | changelog cycle header |
| `6184307c` | 2026-06-23 | fix(ai): require explicit anthropic compat metadata | review | **ported** | `64e5022` | getAnthropicCompat drops fireworks/cf auto-detect → OpenAI-standard defaults; catalog supplies the rest. Byte-identical for catalog (0 mismatches). Test re-pinned to explicit compat. |
| `c3cfeac0` | 2026-06-23 | fix(coding-agent): make release publication transactional | review | n/a | — | .github/workflows + scripts/publish.mjs (CI) |
| `b3776234` | 2026-06-23 | Type name change | review | n/a | — | packages/agent harness `ExecutionEnvExecOptions`→`ShellExecOptions` rename — behavior-neutral |
| `49fbe683` | 2026-06-23 | fix(ai): align api key credentials with auth json | review | **ported** | `fad8247` | Credential type "api-key"→"api_key"; `Metadata`→`Env` (json metadata→env). On-disk breaking change (no shim, mirrors pi). Test: TestCredentialAPIKeyJSON. |
| `04fce809` | 2026-06-23 | Merge remote-tracking branch 'origin/main' | review | n/a | — | new `legacy-api-aliases.ts` = deprecated TS re-export shims for the removed subpaths (Go compat globals already cover); compat.ts one-liner |
| `ef231c49` | 2026-06-23 | fix(ai): resolve request-scoped auth before provider calls | review | **ported** | `b53482b` | `AuthResolutionOverrides{apiKey,env}` + `overlayEnvAuthContext` into resolveProviderAuth; applyAuth resolves through it; GetAuth override-free. The named auth/resolve.ts boundary edge. Test: TestResolveProviderAuthRequestOverrides. |
| `e1a2dc04` | 2026-06-23 | fix(ai): restore detectCompat runtime fallback in openai-completions | review | n/a (net-neutral) | — | restores `detectCompat` removed by `129eb460` → net unchanged; Go's detectCompat stays as-is |
| `9096d5f9` | 2026-06-23 | docs: update changelog entries | likely-n/a | n/a | — | changelogs |
| `0201806a` | 2026-06-23 | Release v0.80.2 | review | **ported** | `d2f937d` | final catalog regen (reference build); endpoint-pinned, integrity-verified `sha512-5GNKfdrR…uy9RQ==` |
| `8277bd68` | 2026-06-23 | Add [Unreleased] section for next cycle | likely-n/a | n/a | — | changelog cycle header |
| `954ec998` | 2026-06-23 | fix: upload release assets from visible directory | likely-n/a | n/a | — | .github workflow (CI) |
| `97820276` | 2026-06-23 | fix: remove OpenClaw gate | likely-n/a | n/a | — | .github workflow (CI) |
| `ec6311be` | 2026-06-23 | fix: skip dirty check before npm publish | likely-n/a | n/a | — | .github workflow (CI) |
| `63386614` | 2026-06-24 | fix(coding-agent): print benchmark timings after TUI stop (#6030) | review | n/a | — | main.ts startup-benchmark timing order (TUI) |
| `a2e3e9d8` | 2026-06-24 | Merge #6004 support-azure-foundry-endpoints | review | n/a | — | azure-openai-responses.ts — Azure provider excluded |

## Ledger — 3b561346 → 470a4736

| Upstream | Date | Subject | Hint | Status | Go commit | Notes |
|---|---|---|---|---|---|---|
| `732bb161` | 2026-06-22 | Merge model-registry into main | decide→adopt | **ported** | `bf7e7bd`+`37dcff5`+`2b164b3` | pi's `packages/ai` object-model overhaul ported per the 2026-06-23 adopt ruling. auth_*.go = `auth/*` substrate (CredentialStore/InMemoryCredentialStore, ProviderAuth/ApiKeyAuth/OAuthAuth, AuthContext, EnvAPIKeyAuth/LazyOAuth, resolveProviderAuth w/ OAuth refresh-under-lock). models_runtime.go = `models.ts` (`Provider` iface, CreateProvider, Models/CreateModels, GetAuth/applyAuth incl. 2cbce395 env merge, HasApi). builtins_models.go = BuiltinModels (catalog+ProviderAuth+ApiProvider streams). Provider→ProviderId rename (alias). Globals stay as compat (pi `/compat`). Divergences (documented): auth as files-in-package not subpackage (cycle); sync `(T,error)`; errorStream not lazyStream (G3); OAuth login out of scope; images excluded. Catalog-data reorg deferred to next regen. Request bytes unaffected. pi-go-review ship + pi-parity-review faithful. Tests: auth_test, models_runtime_test, builtins_models_test |
| `d2677a63` | 2026-06-22 | docs(agent): mark sync models API complete | likely-n/a | n/a | — | packages/agent/docs/models.md |
| `02540acd` | 2026-06-22 | docs(ai): update provider README | likely-n/a | n/a | — | packages/ai/README.md |
| `5a8ea0bc` | 2026-06-23 | fix(ai): honor scoped AWS profile in Bedrock endpoint resolution | review | n/a | — | bedrock-converse-stream.ts only — Bedrock provider unported |
| `2cbce395` | 2026-06-23 | feat(ai): pass provider-resolved env to APIs | review | ported | `1577144` | No Go code change: `resolution.env` latent upstream (no catalog provider's `resolve()` returns env), Go's `opts.Env` already flows to providers (withEnvAPIKey clones preserve Env). images-models half = images (n/a). Locked by `TestStreamEnvReachesProvider`. |
| `8eeaa2bc` | 2026-06-23 | fix(ai): honor scoped env in compat API key injection | review | ported | `1577144` | `GetEnvApiKey`/`FindEnvKeys` thread scoped `env`; new canonical `ai.ProviderEnvValue` (providers' `getProviderEnvValue` delegates); vertex-ADC + bedrock branches consult scoped env; `withEnvAPIKey`/`Simple` pass `opts.Env`, host/example call sites pass nil. Golden: API-key selection — byte-identical when Env unset. Tests: `TestGetEnvApiKeyScopedEnv`, `TestWithEnvAPIKeyUsesScopedEnv`. |
| `6a4813a7` | 2026-06-23 | Merge remote-tracking branch 'origin/main' | review | n/a | — | only ai/src file is `openai-codex-responses.ts` (Codex, unported); rest theme/startup-ui/session-picker/settings-manager/main.ts (TUI/CLI/host) |
| `7fedc332` | 2026-06-23 | fix(coding-agent): normalize session names (#5999) | review | n/a | — | `\r\n`→space sanitize in `appendSessionName`/`appendSessionInfo` write path — host/TUI-driven; Go reads `SessionInfo` but has no name-write/rename path. Low-confidence n/a — re-confirm at substrate port time |
| `470a4736` | 2026-06-23 | fix(coding-agent): sort threaded sessions by latest activity in subtree (#5784) | review | n/a | — | interactive/components/session-selector.ts (TUI) |

## Ledger — 2417adb4 → 3b561346

| Upstream | Date | Subject | Hint | Status | Go commit | Notes |
|---|---|---|---|---|---|---|
| `a61137a6` | 2026-06-22 | docs(coding-agent): fix plan-mode docs links | likely-n/a | n/a | — | docs/tui.md + changelog |
| `08457404` | 2026-06-22 | chore: approve contributor any-victor | n/a | n/a | — | .github contributor meta |
| `7d0497fd` | 2026-06-22 | fix(ai): preserve early reasoning details | review | ported | `4e60155`+`62981f1` | openai.go: encrypted reasoning_details arriving before their tool-call block are buffered (pendingReasoningDetails by id) and drained in ensureToolCallBlock (applyPendingReasoningDetail); match via toolBuildersByID not order-scan; no longer dropped (#5114). `62981f1` ports the same commit's isEncryptedReasoningDetail tightening (data must be a non-empty string), replacing the old jsonValueTruthy gate. Response-parse only (request bytes unchanged). Tests: TestOpenAIReasoningDetailsEarlyArrival, TestOpenAIReasoningDetailsNonStringDataIgnored |
| `5b9b70d2` | 2026-06-22 | feat(coding-agent): add compaction reason and willRetry to extension compact events (#5962) | review | n/a | — | agent-session.ts + core/extensions/types.ts: `reason`/`willRetry` on SessionBeforeCompact/SessionCompact extension events — unported event lifecycle (compaction-trio rulings) |
| `b7908b49` | 2026-06-22 | docs(coding-agent): document slash command table | likely-n/a | n/a | — | README + docs/usage |
| `5641d6ba` | 2026-06-22 | fix: clear untriaged when no-action is added | likely-n/a | n/a | — | .github issue-triage workflow |
| `756a4e8f` | 2026-06-22 | fix(coding-agent): respect nested repo ignore boundaries in find | review | ported | `46302ad` | glob.go ignoreStack: new `boundaries` axis (respectNestedRepos) + crossesNestedBoundary/hasGitDir — outer repo-specific ignore sources stop at a nested `.git`; nested repo's own rules still apply; global excludesFile carries across (boundaryExempt); active only inside a repo. grep/rg unchanged (false). Pure-Go fd reimplementation, validated vs git oracle (#5960). Known minor under-reach: nested repo's own info/exclude not re-rooted (follow-up). Test: TestFindRespectsNestedRepoBoundaries |
| `5df5a1ce` | 2026-06-22 | docs(coding-agent): audit unreleased changelog | likely-n/a | n/a | — | changelog |
| `8e190066` | 2026-06-22 | Release v0.79.10 | review | ported | `c50acfc` | ai/models_catalog.json regenerated from npm 0.79.10 (endpoint-pinned both sides, integrity-verified). +1 (vercel-ai-gateway/sakana/fugu-ultra), −1 (openrouter/anthropic/claude-3.5-haiku), 17 openrouter cost/window churn. off:null tripwires intact. Dropped id was a resolve-fallback fixture → TestResolveModelProviderPrefixFallsBackToFullID updated to vercel-ai-gateway (sole remaining copy; logic unchanged). Independent parity review: faithful |
| `329dceb5` | 2026-06-22 | Add [Unreleased] section for next cycle | likely-n/a | n/a | — | changelog cycle header |
| `717a8f95` | 2026-06-22 | fix(ai): revert selective pi-ai base entrypoints | review | n/a | — | reverts the n/a `0d89a333` — test import paths + tsconfig/vitest/scripts (packaging only) |
| `4f71b2d3` | 2026-06-22 | fix(coding-agent): clarify ZAI Coding Plan label | review | n/a | — | provider-display-names.ts "ZAI" → "ZAI Coding Plan (Global)" (no Go equivalent — display/TUI) + cli/args help text; Go envkeys.go maps only provider→ENV_KEY (unchanged) |
| `71ca9b2b` | 2026-06-22 | fix(ai): expose OpenCode Go GLM-5.2 xhigh effort | review | n/a (data) | — | generate-models.ts + models.generated.ts (opencode-go/zai-org glm-5.2 thinkingLevelMap xhigh:max); lands **post-0.79.10** → deferred to the next catalog regen |
| `3b561346` | 2026-06-22 | fix(tui): bind ctrl+j as newline by default | likely-n/a | n/a | — | tui/keybindings.ts (TUI) |

## Ledger — 56b22768 → 2417adb4

| Upstream | Date | Subject | Hint | Status | Go commit | Notes |
|---|---|---|---|---|---|---|
| `783571a6` | 2026-06-19 | feat: track auto-closed issue triage | likely-n/a | n/a | — | .github issue-triage workflows |
| `47d1d90a` | 2026-06-19 | fix: close no-action issues as not planned | likely-n/a | n/a | — | .github issue-triage workflow |
| `373cd6ae` | 2026-06-19 | fix(coding-agent): prioritize provider matches in model selector | review | n/a | — | modes/interactive model-selector/model-search (TUI) — unported |
| `226a3168` | 2026-06-19 | fix: mark auto-closed issues not planned | likely-n/a | n/a | — | .github issue-gate workflow |
| `6e6ce70c` | 2026-06-19 | fix(ai): filter Copilot models by account availability | review | n/a | — | `ai/utils/oauth/github-copilot.ts` (OAuth, unported) + host model-registry; only model-registry *test* changed, not the registry |
| `1287b69f` | 2026-06-19 | fix(coding-agent): run legacy WSL bash commands via stdin | review | ported | `9f452a1` | tools.go getShellConfig: detect System32/Sysnative bash.exe (isLegacyWslBashPath) → `bash -s` + command on stdin; else `bash -c`. resolve-config-value half host-side (n/a). Test: TestLegacyWslBashDetection |
| `128330e3` | 2026-06-19 | fix(coding-agent): preserve untouched lines in fuzzy edit | review | ported | `18ef9eb` | editmatch.go: fuzzy edits overlay only touched line-blocks onto the LF-normalized original (splitLinesWithEndings/getLineSpans/getReplacementLineRange/applyReplacementsPreservingUnchangedLines). Golden: edit-tool output. Tests: single + multi preserve |
| `8b97e75c` | 2026-06-19 | feat(ai): add chat-template thinking compat | review | ported | `3c30dd2` | openai-completions `thinkingFormat:"chat-template"` → configurable `chat_template_kwargs` (openai_chat_template.go). Latent (no catalog model). Golden: request body. Host-side model-registry schema/mergeCompat unported. Cosmetic follow-up `56c73b7`. Tests: openai_chat_template_test.go |
| `3095977d` | 2026-06-20 | fix(tui): stabilize streaming code fence rendering (#5846) | likely-n/a | n/a | — | tui/markdown |
| `416c673d` | 2026-06-20 | fix: skip no-action for to-discuss issues | likely-n/a | n/a | — | .github issue-triage workflow |
| `8597ebaf` | 2026-06-20 | fix(ai): expose OpenRouter GLM-5.2 xhigh effort | review | n/a (data) | — | generate-models.ts + models.generated.ts; lands via 0.79.9 catalog regen (`5d8b72d`, openrouter/z-ai/glm-5.2 thinkingLevelMap xhigh:xhigh) |
| `a1da88ae` | 2026-06-20 | fix(coding-agent): make session path traversal linear | review | ported | `a88ef3b` | session_tree.go Branch: O(n²) prepend → append+reverse. Behavior-neutral (covered by session-tree parity tests) |
| `5505316e` | 2026-06-20 | fix(coding-agent): cache extension imports for session switches | review | n/a | — | core/extensions/loader.ts + resource-loader.ts — extensions runtime unported |
| `500b568b` | 2026-06-20 | fix(ai): use OpenAI endpoint for Fireworks GLM-5.2 | review | n/a (data) | — | generate-models.ts + models.generated.ts; lands via 0.79.9 regen (`5d8b72d`, fireworks glm-5p2 → api openai-completions, /inference/v1 baseUrl, compat, thinkingLevelMap) |
| `350ac3f3` | 2026-06-20 | fix: remove inprogress from auto-closed issues | likely-n/a | n/a | — | .github issue-triage workflow |
| `1aa79b9b` | 2026-06-20 | docs: update unreleased changelog audit | likely-n/a | n/a | — | changelog |
| `615bf2f8` | 2026-06-20 | Release v0.79.9 | review | ported | `5d8b72d` | ai/models_catalog.json regenerated from npm 0.79.9 (endpoint-pinned both sides, integrity-verified). 0 added, 2 removed (google gemma-4-E2B-it/E4B-it; no Go refs), 20 changed. Subsumes 8597ebaf/500b568b data + cost/metadata churn. off:null tripwires intact. Independent parity review: faithful |
| `b4f31408` | 2026-06-20 | Add [Unreleased] section for next cycle | likely-n/a | n/a | — | changelog cycle header |
| `d93b92ba` | 2026-06-20 | fix(coding-agent): show changelog URL in update notice | review | n/a | — | modes/interactive interactive-mode (TUI update notice) |
| `bc0db643` | 2026-06-21 | fix(coding-agent): install checked pi update version | review | n/a | — | config.ts (bin-dir) + package-manager-cli.ts — self-update/packaging unported |
| `542683b2` | 2026-06-21 | fix(coding-agent): fix plan-mode example | likely-n/a | n/a | — | examples/extensions/plan-mode |
| `2417adb4` | 2026-06-21 | fix(coding-agent): preserve startup extension UI | review | n/a | — | agent-session.ts `reload()` gains a beforeSessionStart hook (agent-session-runtime reload, unported) + interactive-mode (TUI) |

## Ledger — 29c1504c → 56b22768

| Upstream | Date | Subject | Hint | Status | Go commit | Notes |
|---|---|---|---|---|---|---|
| `068ab5d1` | 2026-06-17 | fix(coding-agent): horizontally pan tree selector | likely-n/a | n/a | — | TUI tree-selector + tui/index.ts |
| `ae89286d` | 2026-06-17 | docs: update changelogs for tree panning | likely-n/a | n/a | — | docs/changelog |
| `6d5ede31` | 2026-06-17 | fix(coding-agent): match provider-first model searches | review | n/a | — | modes/interactive model-selector/search (TUI) — unported |
| `58dd2f59` | 2026-06-18 | feat(ai): add GLM-5.2 to OpenCode Go model catalog | review | n/a (data) | — | models.generated data; lands via 0.79.8 catalog regen (`5164314`) |
| `008c76f9` | 2026-06-18 | feat(coding-agent): export project config dir name | review | n/a | — | `CONFIG_DIR_NAME` SDK constant export + trust-prompt/help-text string interpolation; no ported behavior (trust + interactive + SDK const only) |
| `51f75235` | 2026-06-18 | fix(coding-agent): include RPC request id on unknown commands | review | n/a | — | `modes/rpc` unported in Go (no rpc-mode) |
| `7a14325b` | 2026-06-18 | feat(tui): detect Warp terminal and enable Kitty image protocol (#5841) | likely-n/a | n/a | — | TUI terminal-image |
| `20da9bc1` | 2026-06-18 | fix attribution for 008c76f9 | likely-n/a | n/a | — | changelog attribution |
| `bc93655e` | 2026-06-18 | meta: Added report template | likely-n/a | n/a | — | .github issue template |
| `908be616` | 2026-06-18 | ref: Remove some options from package reporting | likely-n/a | n/a | — | .github issue template |
| `d0b46764` | 2026-06-18 | feat(coding-agent): add automatic theme mode (#5874) | review | n/a | — | TUI theme-controller + settings-manager (unported); theme is TUI |
| `2b46f388` | 2026-06-18 | feat(coding-agent): Expose edit-diff for extensions (#5756) | review | n/a | — | comment change + SDK export (`generateDiffString`/`generateUnifiedPatch`); no behavior change |
| `aae62dfa` | 2026-06-18 | feat(coding-agent): make bare update self-only | review | n/a | — | package-manager-cli/self-update + cli/args (unported packaging) |
| `71749422` | 2026-06-18 | docs: audit unreleased changelogs | likely-n/a | n/a | — | changelog |
| `c4ab61dc` | 2026-06-18 | Release v0.79.7 | review | ported (superseded) | `5164314` | catalog regen; superseded by 0.79.8 (no separate regen — final 0.79.8 build subsumes it) |
| `788a0444` | 2026-06-18 | Add [Unreleased] section for next cycle | likely-n/a | n/a | — | changelog cycle header |
| `6b9f3f49` | 2026-06-18 | fix(coding-agent): avoid retrying successful overflow compaction | review | n/a | — | agent-session-runtime overflow-recovery (`_runAutoCompaction` willRetry/stopReason gating) — unported; Go has no overflow-error-triggered compaction recovery |
| `7d08c81a` | 2026-06-18 | fix(coding-agent): avoid empty compaction summaries | review | n/a | — | `prepareCompaction` empty guard + `compaction_start/end` event reordering — both unported (Go has no prepareCompaction nor the event lifecycle; compacts inline via shouldCompact/findCutPoint) |
| `b09fbde0` | 2026-06-18 | feat(ai): add OpenRouter Fusion alias (#5866) | review | n/a (data) | — | generate-models.ts alias entry; lands via 0.79.8 catalog regen (`5164314`, id `openrouter/fusion`) |
| `c60f6a8a` | 2026-06-18 | feat(coding-agent): expose post-compaction token estimates | review | n/a | — | `estimatedTokensAfter` on `CompactionResult` SDK type + emitted in `compaction_end` — unported event lifecycle |
| `cab89d14` | 2026-06-18 | docs: audit unreleased changelogs | likely-n/a | n/a | — | changelog |
| `fd1ba2c7` | 2026-06-18 | test(coding-agent): seed auto-compaction queue fixture | likely-n/a | n/a | — | test-only; auto-compaction queue (unported orchestration) |
| `8025fdd0` | 2026-06-18 | meta: Update readmes slightly | likely-n/a | n/a | — | READMEs |
| `651d10d9` | 2026-06-18 | feat(ai): enable Mistral prompt caching | review | n/a | — | `ai/providers/mistral.ts` — Mistral provider unported; catalog cost-field data lands via 0.79.8 regen (44 changed mistral/* entries) |
| `9179734c` | 2026-06-18 | docs(coding-agent): audit unreleased changelog | likely-n/a | n/a | — | changelog |
| `1a418ad2` | 2026-06-19 | chore: remove inprogress label on close | likely-n/a | n/a | — | .github workflow |
| `0d89a333` | 2026-06-18 | feat(packages): Add selective pi-ai base entrypoints (#5348) | review | n/a | — | packaging/exports-map + test import paths + tsconfig/vitest/scripts; no behavior |
| `ea65a51a` | 2026-06-19 | fix: update vulnerable dependencies | likely-n/a | n/a | — | lockfiles/package.json (deps) |
| `a2f70e5f` | 2026-06-19 | fix(coding-agent): reset tool test mocks | likely-n/a | n/a | — | test-only |
| `74677bbf` | 2026-06-19 | docs: audit unreleased changelogs | likely-n/a | n/a | — | changelog |
| `8eb9704b` | 2026-06-19 | Release v0.79.8 | review | ported | `5164314` | ai/models_catalog.json regenerated from npm 0.79.8 (endpoint-pinned both sides, integrity-verified). +9/−3 ids (opencode-go glm-5.2, openrouter/fusion, fireworks glm-5p2, poolside/qwen/cohere/gemini-3-pro-image/liquid; −opencode-go glm-5, −raptor-mini, −xiaomi mimo). 44 changed = Mistral prompt-caching cost fields + fireworks/openrouter/vercel metadata. Subsumes v0.79.7 + 58dd2f59/b09fbde0/651d10d9 data. off:null tripwires intact (fable-5, kimi-k2.7-code). Independent parity review: faithful |
| `56b22768` | 2026-06-19 | Add [Unreleased] section for next cycle | likely-n/a | n/a | — | changelog cycle header |

## Ledger — f8a77f47 → 29c1504c

| Upstream | Date | Subject | Hint | Status | Go commit | Notes |
|---|---|---|---|---|---|---|
| `75b0d723` | 2026-06-16 | fix(ai): support Z.AI GLM-5.2 effort levels | review | ported | `788c832` | openai.go zai branch: when effort + `compat.supportsReasoningEffort`, emit `reasoning_effort` mapped via `thinkingLevelMap` alongside `thinking:{type}`. New `mappedEffortOrRaw` ports pi's undefined→raw / null→omit / string→mapped (distinct from `effortValue`, which returns raw on present-null). Catalog data via 0.79.6 regen. Golden: request body (zai). Tests: low/high/xhigh map, minimal:null omit, off, no-supportsReasoningEffort |
| `06d8c54d` | 2026-06-16 | fix(coding-agent): avoid Windows pi update exit assertion | review | n/a | — | main.ts self-update/CLI exit (unported) |
| `3039f3e1` | 2026-06-16 | fix(tui): restore cursorUp start-of-line jump (#5789) | likely-n/a | n/a | — | TUI editor |
| `7f29e7a3` | 2026-06-16 | feat: add provider-scoped environment overrides (#5807) | review | ported | `872c303` | `StreamOptions.Env` + `getProviderEnvValue` (scoped non-empty wins, else os.Getenv) threaded into `PI_CACHE_RETENTION` + Cloudflare base-URL across anthropic/openai-completions/openai-responses. Bun `/proc` fallback omitted (no Go analog). Host-side population unported (field latent). Owner ruling 2026-06-17. Golden: cache-retention/cloudflare request paths (byte-identical when Env unset). Tests: env precedence, cache-retention scoped env, cloudflare scoped override + empty fall-through |
| `8f0e9251` | 2026-06-16 | fix(coding-agent): do not open browser for device code login | likely-n/a | n/a | — | interactive login-dialog (TUI/oauth) |
| `0680726a` | 2026-06-16 | fix: upgrade marked to 18.0.5 | likely-n/a | n/a | — | export-html vendor min.js + tui dep |
| `91050859` | 2026-06-16 | feat(coding-agent): add settings http proxy | review | n/a | — | core/http-dispatcher: process.env HTTP(S)_PROXY + undici global-fetch host runtime config (unported; Go uses net/http) |
| `2d597f02` | 2026-06-16 | fix(ai): tolerate null Responses message content | review | ported | `e8f7511` | NO code change — Go ranges a nil slice (what `"content":null` unmarshals to) safely, rebuilding to "" exactly as pi's `?? ""`; the JS TypeError has no Go analog. Locked with `TestResponsesNullMessageContent` (mutation-verified non-vacuous) |
| `2431491c` | 2026-06-16 | fix(ai): avoid duplicate OpenCode DeepSeek reasoning controls | review | n/a | — | data-only generate-models.ts (deepseek-v4 compat); net no-op with `bd9f8773` (adds then reverts opencode-go); lands via 0.79.6 regen |
| `b6b5bed9` | 2026-06-16 | docs: update unreleased changelogs | likely-n/a | n/a | — | docs/changelog |
| `6561cb29` | 2026-06-16 | Release v0.79.5 | review | n/a | — | v0.79.5 catalog superseded by 0.79.6 (no separate regen) |
| `0b0b9eae` | 2026-06-16 | Add [Unreleased] section for next cycle | likely-n/a | n/a | — | changelog cycle header |
| `a78cd7cc` | 2026-06-16 | fix(coding-agent): stabilize self-update tests | likely-n/a | n/a | — | self-update test (unported) |
| `a93f0666` | 2026-06-16 | fix(coding-agent): preserve fetch overrides | review | n/a | — | core/http-dispatcher global-fetch/undici install guard (unported runtime config) |
| `bd9f8773` | 2026-06-16 | fix(ai): restore OpenCode Go DeepSeek thinking controls | review | n/a | — | data-only generate-models.ts (reverts the opencode-go arm of 2431491c); lands via 0.79.6 regen |
| `7da475db` | 2026-06-16 | fix(ai): regenerate model catalog | review | n/a | — | data-only catalog (cacheRead/maxTokens); lands via 0.79.6 regen |
| `34b6aea1` | 2026-06-16 | docs(coding-agent): add changelog entries for fetch override and DeepSeek V4 thinking-off | likely-n/a | n/a | — | docs/changelog |
| `31bfb2f1` | 2026-06-16 | Release v0.79.6 | review | ported | `c2221a7` | ai/models_catalog.json regenerated from npm 0.79.6 (endpoint-pinned both sides: old == 0.79.4 build, new == 0.79.6 build, integrity-verified). +11/−7 ids (GLM-5.2 across zai/openrouter/vercel/cf-workers-ai; legacy gemini-1.5/2.0 vertex pruned). Subsumes 2431491c/bd9f8773/7da475db + v0.79.5. Kimi K2.7 Code `off:null` landed → tripwire converted to `TestDeepseekDisabledThinkingGateLive`. Fable-5 off:null intact |
| `12bb8dd2` | 2026-06-16 | Add [Unreleased] section for next cycle | likely-n/a | n/a | — | changelog cycle header |
| `29c1504c` | 2026-06-17 | chore: approve contributor dodiego | n/a | n/a | — | contributor meta |

## Ledger — 93b3b7c1 → f8a77f47

| Upstream | Date | Subject | Hint | Status | Go commit | Notes |
|---|---|---|---|---|---|---|
| `b5e13bcd` | 2026-06-15 | docs(coding-agent): clarify active tools docs | likely-n/a | n/a | — | docs only |
| `ba0ec615` | 2026-06-15 | fix(coding-agent): restore terminal on SIGTERM | review | n/a | — | TUI/terminal SIGTERM handling (unported) |
| `5b6058c3` | 2026-06-15 | fix(tui): align overlays over CJK wide cells | likely-n/a | n/a | — | TUI overlay rendering |
| `24053eab` | 2026-06-15 | fix(tui): update tab overlay boundary expectation | likely-n/a | n/a | — | TUI test-only |
| `bb959aae` | 2026-06-15 | fix(coding-agent): wrap tree help on narrow terminals | likely-n/a | n/a | — | TUI tree-help rendering |
| `a8519681` | 2026-06-15 | docs(coding-agent): reorder containerization patterns | likely-n/a | n/a | — | docs only |
| `0be5bb6c` | 2026-06-15 | fix(ai): price anthropic 1h cache writes at 2x input (#5738) | review | ported | `eadac1a` | added `Usage.CacheWrite1h` (`json:"cacheWrite1h,omitempty"`); anthropic parses `cache_creation.ephemeral_1h_input_tokens` at message_start only (mirrors upstream); `CalculateCost` prices the 1h slice at 2×input and the 5m slice at `cacheWrite`, both /1e6; TestAnthropic1hCacheWriteCost (catalog claude-opus-4-8: 7.75 split / 6.25 fallback) |
| `28b3af5d` | 2026-06-15 | chore: approve contributor Mearman | n/a | n/a | — | contributor meta |
| `408ac103` | 2026-06-15 | fix(ai): update Copilot Claude thinking metadata | review | n/a | — | captured by 0.79.4 regen (github-copilot opus-4.7/4.8 +minimal:low, sonnet-4.6 +minimal:low/xhigh:max) |
| `3fa40956` | 2026-06-15 | fix: drain stdout before resolving when a child holds the pipe past exit (#5753) | review | ported | `e56f1f9` | replaced fixed `cmd.WaitDelay=100ms` drain with manual `runBashCommand`: merged stdout+stderr on one `os.Pipe`, reader goroutine feeds the updater, post-exit idle grace re-armed per chunk (100ms), releases on idle OR pipe EOF; `WaitDelay=1s` backstops cancel/kill. Tests: TestBashCapturesOutputPastExit (late TICK6 captured), TestBashReleasesPromptlyOnQuietHeldPipe (quiet sleeper releases <2s). Race-clean |
| `8a7ad60f` | 2026-06-15 | feat(coding-agent): add binary release checksums | n/a | n/a | — | CI/release |
| `b1ad469b` | 2026-06-15 | docs: audit changelog entries | likely-n/a | n/a | — | changelog only |
| `bba6af2c` | 2026-06-15 | Release v0.79.4 | review | ported | `ded439c` | catalog regenerated from npm 0.79.4 (Go catalog == npm 0.79.4). Diff 0.79.3→0.79.4: +5 (gemma-4-E2B-it, gemma-4-E4B-it, together Kimi-K2.7-Code, zai/zai-coding-cn glm-5.2), 0 removed, 11 changed (copilot thinking overrides 408ac103; opencode/* compat +supportsLongCacheRetention:false; openrouter deepseek-v4-flash & kimi-k2.7-code cost+maxTokens). claude-fable-5 thinkingLevelMap unchanged (`off:null` intact) → TestFable5DisabledThinkingGateLive safe |
| `1aa3c02d` | 2026-06-15 | Add [Unreleased] section for next cycle | likely-n/a | n/a | — | changelog cycle header |
| `0369bdb8` | 2026-06-15 | fix(ai): add Moonshot CN Kimi K2.7 metadata | review | ported (logic; data deferred) | `62fa1e3` | ported the openai-completions deepseek gate: no effort → send `thinking:{type:disabled}` only when `thinkingLevelMap.off !== null` (reuses `offEffortOrDefault` send flag); off:null omits the key. Catalog data (kimi-k2.7-code off:null) is post-0.79.4, deferred. Tests: TestDiffDeepseekThinkingOffGate (on/absent/null/string), TestDeepseekCatalogNoOffNull tripwire |
| `431d88f4` | 2026-06-15 | meta: Link to rfcs | n/a | n/a | — | repo meta |
| `bee8e9c8` | 2026-06-15 | feat(coding-agent): mark experimental sessions in footer | likely-n/a | n/a | — | TUI footer |
| `7cfd1af3` | 2026-06-16 | fix(coding-agent): keep empty session selector open | likely-n/a | n/a | — | TUI session selector |
| `b0c8f65f` | 2026-06-16 | fix(ai): update Google Vertex Gemini models | review | ported (logic; data deferred) | `62fa1e3` | ported the google.ts `isGemini3Flash` change only: also match `gemini-flash-latest` / `gemini-flash-lite-latest` (lowercased) → MINIMAL disabled-thinking config. google-vertex provider + catalog data deferred. Test: TestGoogleDisabledThinkingPerFamily +2 alias cases |
| `f8a77f47` | 2026-06-16 | feat(coding-agent): add Vercel AI Gateway attribution (#5798) | review | ported | `78f6687` | provider-attribution module ported faithfully per 2026-06-16 ruling; +vercel branch. New `ai/providers/attribution.go` (host/provider detection + per-provider header sets gated on install telemetry; PI_TELEMETRY env honored, default enabled per pi `getEnableInstallTelemetry() ?? true`). Wired into openai/openai_responses/anthropic/google at the BOTTOM of the header precedence (session-attribution then default-attribution emitted first, so model.Headers and opts.Headers both override them — matching pi's mergeProviderAttributionHeaders merge order; review caught and fixed an initial wrong-way precedence). Headers byte-exact: OpenRouter `HTTP-Referer:https://pi.dev`/`X-OpenRouter-Title:pi`/`X-OpenRouter-Categories:cli-agent`; NVIDIA `X-BILLING-INVOKE-ORIGIN:Pi`; Cloudflare `User-Agent:pi-coding-agent`; Vercel `http-referer:https://pi.dev`/`x-title:pi`; OpenCode session `x-opencode-session`/`x-opencode-client:pi`. Tests in `attribution_test.go`: all 4 APIs, vercel+openrouter+nvidia+cloudflare+opencode, telemetry gate, model.Headers+opts.Headers precedence, host detection |

## Ledger — 3f44d3e2 → 6f29450e

| Upstream | Date | Subject | Hint | Status | Go commit | Notes |
|---|---|---|---|---|---|---|
| `1c243365` | 2026-06-12 | fix(tui): keep WezTerm Kitty images visible | likely-n/a | n/a | — | TUI image rendering |
| `a455f62f` | 2026-06-12 | fix(ai): preserve Anthropic refusal details (#5666) | review | ported | `e0a362f` | parse `stop_details.explanation` in message_delta; refusal→errorMessage (or "The model refused to complete the request" fallback); throw path uses errorMessage; tests for both branches |
| `be7d5cf5` | 2026-06-12 | fix(ai): relax Codex SSE header timeout | likely-n/a | n/a | — | Codex provider (unported) |
| `1fc80f4f` | 2026-06-12 | fix(coding-agent): preserve custom fallback thinking | review | ported | `c82663e` | buildFallbackModel sets Reasoning:true when surfaced thinking level present and != "off"; fb is freshly cloned (no shared-catalog mutation); resolve_test :high(reasoning=true)/:off(stays false) on non-reasoning mistral template |
| `6102dd20` | 2026-06-12 | fix(coding-agent): handle missing export themes | likely-n/a | n/a | — | export-themes (settings) |
| `0caca6cf` | 2026-06-12 | fix(tui): support slash-separated fuzzy filter tokens | likely-n/a | n/a | — | TUI fuzzy filter |
| `1b2c32c6` | 2026-06-12 | fix(coding-agent): resolve authenticated slash model ids | review | n/a | — | no auth-aware resolution in Go |
| `adf567c1` | 2026-06-12 | fix(coding-agent): rechain fork paths without labels | review | n/a | — | fork/label runtime unported |
| `daab056a` | 2026-06-12 | fix(agent): ignore late tool progress updates | review | ported | `009dae7` | acceptingUpdates bool guarded by existing updateMu; flipped false right after Execute settles; onUpdate drops late calls under lock; ToolUpdateFunc doc updated; race-locked test |
| `17721d5e` | 2026-06-12 | fix(tui): preserve unordered user list markers (closes #5657) | likely-n/a | n/a | — | TUI markdown rendering |
| `a7cdc679` | 2026-06-12 | fix(ai): correct GPT-5 context window metadata | review | n/a | — | captured by 0.79.3 regen; nets to no change |
| `b4bff7f0` | 2026-06-12 | fix(coding-agent): avoid project trust prompt for update (#5674) | review | n/a | — | trust ruling (2026-06-12) |
| `7a3cb631` | 2026-06-13 | fix(ai): normalize generated model costs (#5634) | review | n/a | — | captured by 0.79.3 regen |
| `121f0edb` | 2026-06-13 | fix(ai): detect parenthesized context overflow errors | review | n/a | — | no overflow module in Go |
| `e320f096` | 2026-06-13 | docs: update unreleased changelogs | likely-n/a | n/a | — | docs only |
| `f21f3c4b` | 2026-06-13 | Release v0.79.2 | review | n/a | — | v0.79.2 superseded by 0.79.3 |
| `032c01c1` | 2026-06-13 | Add [Unreleased] section for next cycle | likely-n/a | n/a | — | changelog cycle header |
| `aa3a5233` | 2026-06-13 | fix(ai): restore Codex context limits | review | n/a | — | captured by 0.79.3 regen |
| `57b6bdce` | 2026-06-13 | docs(coding-agent): update Codex context limit changelog | likely-n/a | n/a | — | docs only |
| `f2585c4c` | 2026-06-13 | Release v0.79.3 | review | ported | `c12fa7d` | catalog regenerated from npm 0.79.3 (re-derived + endpoint-pinned, request diff 6/6). Adds `off:null` to claude-fable-5 thinkingLevelMap → the 9ccfcd7c disabled-thinking gate is now LIVE; tripwire converted to TestFable5DisabledThinkingGateLive (end-to-end via catalog model) |
| `b15148fe` | 2026-06-13 | Add [Unreleased] section for next cycle | likely-n/a | n/a | — | changelog cycle header |
| `6f29450e` | 2026-06-13 | fix(ai): update adaptive thinking model expectations | review | n/a | — | test-only, captured by regen |

## Ledger — 130ae577 → 3f44d3e2

Scope-hint is a mechanical pre-classification from touched paths
(`review` = touches packages/{ai,agent,coding-agent}/src outside unported
areas; `likely-n/a` = TUI/docs/unported only). The pipeline's SCOPE step is
the real decision.

| Upstream | Date | Subject | Hint | Status | Go commit | Notes |
|---|---|---|---|---|---|---|
| `38f18be4` | 2026-06-08 | fix(coding-agent): persist implicit project trust on reload | review | n/a | — | project-trust gating (non-port): trust-manager + main.ts wiring only |
| `f4f72d4e` | 2026-06-08 | docs(agent): add security advisory prompt | likely-n/a | n/a | — | upstream repo .pi/prompts only |
| `dce3e285` | 2026-06-08 | fix: show security advisories in prompt widget | likely-n/a | n/a | — | upstream repo .pi/extensions only |
| `718215bd` | 2026-06-08 | feat(coding-agent): add extension project trust decisions | review | n/a | — | trust excluded per 2026-06-12 ruling; ported-adjacent residue verified behavior-neutral (extension resource-loader refactor only) |
| `21917fed` | 2026-06-08 | Merge pull request #5499 from Roman-Galeev/fix/editor-cursor-move-refresh-autocomplete | likely-n/a | n/a | — | TUI editor autocomplete |
| `085a0858` | 2026-06-08 | fix(coding-agent): remove stale hooks export | likely-n/a | n/a | — | npm exports-map cleanup |
| `d8aef0fe` | 2026-06-08 | feat(coding-agent): allow project trust extensions to defer | review | n/a | — | rider on 718215bd — n/a under the 2026-06-12 trust ruling |
| `ce3a7244` | 2026-06-08 | docs(coding-agent): document security model | likely-n/a | n/a | — | docs only |
| `35120d7e` | 2026-06-08 | docs: audit unreleased changelogs | likely-n/a | n/a | — | changelogs only |
| `c10fb95f` | 2026-06-08 | Release v0.79.0 | review | ported | `d7c89c8` | catalog regenerated from npm 0.79.0 build (7 add/7 del/18 chg); go-review ship, parity faithful (endpoint-pinned); npm ref refreshed to 0.79.0 |
| `2edd6b43` | 2026-06-08 | Add [Unreleased] section for next cycle | likely-n/a | n/a | — | changelog cycle headers |
| `20b78eaf` | 2026-06-08 | fix(coding-agent): fix changelog links | review | n/a | — | changelog-link util consumed by TUI only + CI |
| `44e33798` | 2026-06-09 | Merge pull request #5527 from AJM10565/fix/bedrock-arn-region-parsing | likely-n/a | pending | — | |
| `4c486365` | 2026-06-09 | Merge pull request #5505 from awakenalive/patch-1 | likely-n/a | pending | — | |
| `c6bdfa19` | 2026-06-09 | chore: approve contributor davidlifschitz | likely-n/a | pending | — | |
| `2326d5cb` | 2026-06-09 | fix(ai): disable Moonshot thinking when requested | review | ported | `732cfa5` | data-only (moonshot thinkingFormat=deepseek); landed via the 0.79.1 catalog regen (`28df940f`) |
| `22e45492` | 2026-06-09 | Merge pull request #5283 from smoosex/main | likely-n/a | pending | — | |
| `def99d39` | 2026-06-09 | chore: approve contributor vdxz | likely-n/a | pending | — | |
| `8da077bc` | 2026-06-09 | fix(tui): wrap CJK text at grapheme boundaries | likely-n/a | pending | — | |
| `84cdd024` | 2026-06-09 | fix(ai): disable Azure OpenAI response storage | likely-n/a | pending | — | |
| `081a0a2b` | 2026-06-09 | chore: approve contributor dangooddd | likely-n/a | pending | — | |
| `db3f9953` | 2026-06-09 | feat(coding-agent): expose project trust to extensions | review | pending | — | |
| `e4907b3b` | 2026-06-09 | fix(tui): restore prompt draft after history browsing | likely-n/a | pending | — | |
| `19060743` | 2026-06-09 | fix(coding-agent): handle invalid models json during migration | review | pending | — | |
| `28c83e83` | 2026-06-09 | fix(coding-agent): sync queue modes on reload | review | pending | — | |
| `66335d3a` | 2026-06-09 | feat(coding-agent): add experimental feature guard (#5547) | review | ported | `16ed486` | coding/experimental.go: AreExperimentalFeaturesEnabled ⇔ PI_EXPERIMENTAL == "1" exactly |
| `64b51efb` | 2026-06-09 | fix(ai): use z.ai thinking payload | review | ported | `0b8a47c` | zai now sends thinking:{type:"enabled"\|"disabled"} instead of enable_thinking bool (openai.go applyReasoningFormat) |
| `9632bddd` | 2026-06-09 | fix(coding-agent): stabilize OAuth login prompt rows | likely-n/a | pending | — | |
| `3d02d1da` | 2026-06-09 | fix(ai): map OpenCode max tokens | review | ported | `732cfa5` | data-only (opencode/opencode-go maxTokensField=max_tokens); landed via the 0.79.1 catalog regen (`28df940f`) |
| `d041b5cc` | 2026-06-09 | Merge pull request #5549 from earendil-works/approval-settings | review | pending | — | |
| `69ea1a63` | 2026-06-09 | docs(coding-agent): clarify model name display docs | likely-n/a | pending | — | |
| `b7e721cb` | 2026-06-09 | feat(tui): support autocomplete trigger characters | likely-n/a | pending | — | |
| `ae7a885d` | 2026-06-09 | Closes #5045, /new should not persist if original session was ephemeral | review | pending | — | |
| `c5582102` | 2026-06-09 | Merge pull request #5553 from dannote/prompt-template-defaults | review | pending | — | |
| `a0c2465d` | 2026-06-09 | docs: audit unreleased changelogs | likely-n/a | pending | — | |
| `5a9d72ea` | 2026-06-09 | feat(ai): add Claude Fable 5 metadata | review | ported | `732cfa5` | data-only (claude-fable-5 entries, xhigh thinkingLevelMap); landed via the 0.79.1 catalog regen (`28df940f`) |
| `6b5923f1` | 2026-06-09 | fix(ai): correct Azure gpt-5.4/5.5 context window and gpt-5-pro maxTokens | likely-n/a | pending | — | |
| `66f432ca` | 2026-06-09 | fix(ai): regenerate models for Claude Fable 5 and Azure metadata overrides | review | ported | `732cfa5` | data-only (models.generated regen); landed via the 0.79.1 catalog regen (`28df940f`) |
| `4d9f9f45` | 2026-06-09 | fix(ai): regenerate image models for upstream Riverflow rename | review | pending | — | |
| `28df940f` | 2026-06-09 | Release v0.79.1 | likely-n/a | ported | `732cfa5` | ai/models_catalog.json regenerated from npm 0.79.1 build (11 add/0 del/51 chg; supersedes the 0.79.0 regen); captures `2326d5cb`/`3d02d1da`/`5a9d72ea`/`66f432ca` |
| `82f2b1e9` | 2026-06-09 | Add [Unreleased] section for next cycle | likely-n/a | pending | — | |
| `dacb367e` | 2026-06-09 | fix(ai): expect Claude Fable 5 in adaptive thinking model test | likely-n/a | pending | — | |
| `9ccfcd7c` | 2026-06-10 | fix(ai): omit disabled thinking for Claude Fable 5 | review | ported | `dbad9d5` | anthropic.go: skip thinking:{type:"disabled"} when thinkingLevelMap has off:null (present-nil); generate-models off:null lands with a future catalog regen |
| `a7f9fe68` | 2026-06-10 | fix: bump shell-quote to 1.8.4 in lockfile (GHSA-w7jw-789q-3m8p) | likely-n/a | pending | — | |
| `9fd75b8a` | 2026-06-10 | Merge pull request #5560 from haoqixu/fix-5552 | review | ported | `1c81b72` | coding/resolve.go: strip valid `:level` suffix before custom-id fallback, surface as ThinkingLevel, warning quotes stripped id |
| `e537dba3` | 2026-06-10 | Merge pull request #5561 from unexge/push-lpxyxwstnswr | likely-n/a | pending | — | |
| `2f5066d7` | 2026-06-10 | Merge pull request #5562 from Perlence/fix-tui-render-loose-lists | likely-n/a | pending | — | |
| `a3cd03e7` | 2026-06-10 | Merge pull request #5585 from haoqixu/fix-editor-cjk-wrap | likely-n/a | pending | — | |
| `0ab2aa86` | 2026-06-10 | feat(coding-agent): add experimental first-time setup flow (#5587) | review | pending | — | |
| `406a2214` | 2026-06-10 | fix(coding-agent): refine setup copy | likely-n/a | pending | — | |
| `1da90398` | 2026-06-11 | fix(coding-agent): skip first-time setup for forks (#5627) | review | pending | — | |
| `3f44d3e2` | 2026-06-12 | fix(ai): remove stale OpenRouter Kimi free model assertion (#5650) | likely-n/a | pending | — | |

## Ledger — 6f29450e → 93b3b7c1 (no-op cycle)

| Upstream | Date | Subject | Status | Notes |
|---|---|---|---|---|
| `f315d814` | 2026-06-13 | meta: update weekend policy in contributing | n/a | meta/docs |
| `9e9fc794` | 2026-06-13 | fix(coding-agent): treat uppercase config values as literals | n/a | config-migration / settings-manager (non-ported) |
| `21a904f4` | 2026-06-13 | fix(ai): disable OpenCode long cache retention for rejecting routes | n/a | data-only catalog flag; behavior already ported (openai_compat/openai.go); no release in window → next release regen absorbs it |
| `5be8c31f` | 2026-06-14 | meta: add extension disclaimer to bug reporting | n/a | meta |
| `2fbdff9d` | 2026-06-14 | fix(coding-agent): fix pnpm self-update bin-dir | n/a | self-update/packaging (non-ported) |
| `c48f656f` | 2026-06-14 | fix(coding-agent): handle npm package semver ranges | n/a | package-manager (non-ported) |
| `3fcfb7ab` | 2026-06-14 | docs(coding-agent): document extension resource lifecycle | n/a | docs |
| `f0989800` | 2026-06-14 | feat: detect first-run terminal theme (#5385) | n/a | TUI + interactive theme detection (non-ported) |
| `11b5403f` | 2026-06-14 | fix(coding-agent): exit after package commands | n/a | bun/CLI + package-manager (non-ported) |
| `6b40c99a` | 2026-06-14 | feat(examples): wrap question extension text instead of truncating (#5708) | n/a | examples |
| `d683a581` | 2026-06-14 | meta: update CONTRIBUTING.md for clearer language | n/a | meta/docs |
| `93b3b7c1` | 2026-06-14 | fix(tui): preserve WezTerm Kitty images on full redraw | n/a | TUI image rendering |
