# Upstream provenance & sync ledger

Tracks exactly which upstream pi the Go port corresponds to, and the
commit-by-commit sync pipeline that keeps it current.

- **Upstream**: https://github.com/earendil-works/pi (TypeScript, by Mario Zechner)
- **This port started**: 2026-06-08 (cloned upstream `main` HEAD of the day)

## Current pin

| What | Value |
|---|---|
| TS source fully reviewed/ported | `f3c406a9b` — "chore: approve contributors from issue #8018" (2026-08-14; **1 port, 9 n/a, 0 decide** — delta `46bb9a2c3..f3c406a9b`, **10** first-parent changes, no merges. The port: `9d2ec7ffa` — the **kimi-coding runtime User-Agent override** on the anthropic-messages wire (`68cdb4e` + review fixes `89511fa`; parity **CLEAN**, go-review **CLEAN**) — a request-**header** golden surface, proven outside the bodies-only harness (see the 2026-08-14 cycle section). **No release crossed** — no `packages/*/package.json` version bump (pi-ai still 0.84.1) and `models.generated.ts` untouched, so NO catalog regen, the npm reference build did not move, and no port tag was cut this cycle. Differential harness unchanged at **21 scenarios, 21/21 PASS** this cycle; the `deepseek` scenario is still on `backend: "src"` — flip back to dist at the next release. The **catalog-only queue grows 3 → 4** (`KIMI_STATIC_HEADERS` removal rides `9d2ec7ffa`'s generator half). The pin still means *"everything in scope is reviewed and ported **except the harness tree**"* — see the 2026-08-07 ruling; this delta added NO harness commits (backlog stays 9, through `b75be04d9`). The `defaultTools` tripwire (2026-08-13) was not hit. Prior pin: `46bb9a2c3` (2026-08-13, triage-only). |
| npm build the byte-goldens were captured from | `@earendil-works/pi-ai` **0.84.1**, unchanged again at the 2026-08-14 pin `f3c406a9b` (no release crossed; nothing recaptured) — with four caveats now on record: the shipped build **predates `c185d4123`** and sends `max_completion_tokens` to DeepSeek, it **predates `b647d1879`** so it matches `deepseek.com` case-sensitively, it **predates `7915cdac6`** so it carries no strict tool schema conversion at all, and it **predates `9d2ec7ffa`** so kimi-coding requests carry the static `KimiCLI/1.5` header (shipped in `dist/providers/data/kimi-coding.json` — per-provider dist layout) with no runtime pi-UA override. For all four surfaces the reference is TS source at the pin, not dist (harness `deepseek*` and `strict-tools-*` scenarios are all `backend: "src"`; the kimi UA surface is headers, outside the harness entirely). Four generator-only deltas are queued for the next catalog regen: deepseek `maxTokensField` (`c185d4123`), `supportsStrictMode` on 19 cloudflare-ai-gateway openai-responses models (`75c7fd662`), `deepseek-v4-flash` `thinkingLevelMap.low` (`2f8b4b42f`), and the `KIMI_STATIC_HEADERS` removal (`9d2ec7ffa` — inert for the wire once regenerated, since the runtime override already forces the pi UA). |
| Parity proofs at the pin | **2026-08-14 (1 port):** the kimi UA override proven **sha-anchored + mutation-verified** (headers are outside the bodies-only harness): all three upstream `createClient` branches confirmed to route through `mergeClientHeaders` after `optionsHeaders` with no per-request header bypass; the Go single-`Set` equivalence established by reading every write path (all canonicalize — no raw map writes); Node token fidelity checked down to the `RTL_OSVERSIONINFOW` layout and libuv's `uv_os_uname` release format; the authentic 0.84.1 dist (integrity-matched) shown to carry `KimiCLI/1.5` and no pi-user-agent module, making TS-at-`9d2ec7ffa` the reference; and both tests mutation-verified in a throwaway worktree (override removed → wire shows `[custom-client]`; made unconditional → non-kimi test fails). Harness re-run anyway for the body surface: **21/21 PASS**. **2026-08-12 (1 port):** the strict conversion was proven against **executed upstream TS at `7915cdac6`** three ways: a 28-case conversion probe (`makeStrictJsonSchema` run via node from sha-extracted source vs Go) byte-identical on 24/28 including every error string and the unsupported-key precedence order — the 4 mismatches are the recorded decode-boundary drift class, not wire-reachable through pi's own tools; a 10-case `validateToolArguments` probe vs **real TypeBox** (npm 0.84.1's) 10/10 including the nested-`$ref` compile-path constructed to break the `Check(nil)` mapping; and 3 new differential-harness scenarios asserting full request bodies on the anthropic/openai-completions/openai-responses wires (required ordering with deliberately non-alphabetical properties, anyOf-null widening, no-rewrap of already-nullable shapes, nested object closing, zero-property object, inconvertible-tool fallback carrying the ORIGINAL parameters). The harness is what caught the one real divergence (`required: []` dropped on zero-property strict objects) — fixed, then re-verified **21/21 PASS**. **2026-08-11 (3 ports):** cwd-footer byte-proven via `cmp` on both prompt branches; DeepSeek fold faithful on membership and order (15 terms) and on `strings.Contains(ToLower)` ≡ `.toLowerCase().includes` for the ASCII needle; gateway binding proven by running real upstream TS against real Go `Do()` over a shared case table (7 divergences found and pinned). |
| Reviewed via | 2026-08-14 cycle (1 port) — per-commit diff triage of all 10 changes plus the whole-range reconciliation sweep over `packages/{ai,agent}/src` + `coding-agent/src/{core,main,sdk}` (5 files, every hunk attributed); full gates ran: independent parity **CLEAN** (harness 21/21, mutation-verified tests) and go-review **CLEAN** (1 should-fix + 2 nits, applied in `89511fa`); full `-race` suite green. |

Deliberately not ported (out of scope for the ledger unless a commit changes
that decision): TUI, extensions runtime, OAuth token acquisition, project-trust
gating, Bedrock/Vertex/Mistral/Azure/Codex providers, image generation, bun/CLI
packaging, prompt-templates, settings-manager, config migrations,
agent-session-runtime (session reload + /new flow), the Radius OAuth provider +
its host wiring (`utils/oauth/radius.ts`, `core/radius.ts`, `model-registry`
`oauth:"radius"`, `model-resolver`) — only the generic `pi-messages` SDK API it
speaks is ported (see the 2026-07-14 ruling) — and the host-side machinery
that *populates* provider-scoped env overrides (resolve-config-value,
model-registry, settings) — the SDK `StreamOptions.Env` field is ported but
stays latent until a host sets it (see the 2026-06-17 ruling).

### Rulings (answers to `decide` escalations — triage must not re-ask)

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

- **2026-08-05 — the agent-harness exclusion HOLDS even though harness-v2 is now
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

- **2026-07-17 — the model-runtime facade is ported SDK-scoped** (re:
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

- **2026-07-14 — port the `pi-messages` provider API; leave Radius OAuth + host
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

- **2026-06-17 — provider-scoped env overrides ported faithfully** (re:
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

- **2026-06-16 — provider-attribution ported faithfully** (port-it ruling); SDK
  sends pi's default attribution headers (http-referer/x-title/...) on the
  providers pi does.

- **2026-06-12 — project trust stays excluded** (re: `718215bd`, `d8aef0fe`,
  and the wider upstream trust push). Criteria set by the owner: not an SDK
  use case (host apps control what loads), postponable (purely additive
  subsystem), and verified not to change behavior of ported surface (the only
  ported-adjacent diff was a behavior-neutral refactor inside the unported
  extension resource-loader; `skills.ts` untouched). Future trust commits are
  `n/a` under this ruling UNLESS they change behavior of surface we ported —
  that re-escalates.

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

- **2026-07-30 — `core/resource-loader.ts` source-info additions stay `n/a`**
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
