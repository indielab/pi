---
name: pi-triage
description: Decide whether an upstream pi change needs porting to the Go port. Use when assessing upstream commits/PRs ("should we port X?"), or as the triage stage of /pi-sync. Outputs a WHY/WHAT/SCOPE verdict per change.
---

# pi-triage — port-or-not verdict for upstream changes

Input: one or more upstream main-line shas (or a range `A..B`). Each unit is a
first-parent change: a merged PR is ONE unit, analyzed via `git diff <sha>^1..<sha>`.

## Setup
- Upstream clone: `$PI_UPSTREAM_DIR` if set, else `~/.cache/pi-upstream`. If
  missing: `git clone https://github.com/earendil-works/pi "$dir"`. Always
  `git fetch origin main` first, then `git -C "$dir" checkout -B main
  origin/main` to keep the checkout from drifting (it is a read-only mirror;
  the harness extracts by `git archive <sha>`, so this is safe).
- **Never judge from the working tree — name a sha.** `git show <sha>:<path>`,
  `git diff <sha>^1..<sha>`, `git grep <pattern> <sha> -- <path>`. A stale
  checkout can show the exact opposite of what the pin holds (2026-08-11: the
  tree was ~2 months behind and read as though a ported change had been
  reverted), and a fresh one is at `origin/main`, which is ahead of the pin.
- The authoritative non-port list and current pin live in `docs/UPSTREAM.md`
  ("Current pin" section). Read it before judging anything.

## Per change, produce:
1. **WHY** — intent, from the commit/PR message (`git log -1 --format=%B`) and
   any linked issue number in it.
2. **WHAT** — read the actual diff (`git diff <sha>^1..<sha> --stat` then the
   hunks for non-trivial files). Note behavior, constants, model-visible
   strings, new/changed tests.
3. **SCOPE** — verdict, one of:
   - `port` — touches behavior we ported: `packages/ai/src` (except oauth,
     images, cli, bedrock/vertex/mistral/azure/codex providers),
     `packages/agent/src`, `packages/coding-agent/src/core|main|sdk` (except
     extensions, trust-manager, bun, modes/tui).
   - `n/a` — only touches non-ported surface (TUI, extensions runtime, OAuth,
     project trust, unported providers, docs, CI, examples, packaging) — give
     the specific reason.
   - `decide` — changes the *boundary* itself (e.g. a feature that makes a
     non-ported area load-bearing for the SDK, a new provider, a new tool).
     Escalate to the user instead of deciding silently.
4. For `port` verdicts: map upstream files → our Go files (e.g.
   `core/tools/grep.ts` → `coding/tools.go` + `coding/glob.go`), and flag
   whether any **byte-golden surface** is touched (system prompt, tool output
   strings, request bodies, session format, image decisions) — the parity
   reviewer needs to know.

## Output
A table: `| sha | date | subject | verdict | reason | upstream files → Go files | golden surface? |`
plus one line per `decide` item explaining the boundary question.

Rules: judge from the DIFF, not the subject line (subjects lie; refactors
hide behavior). A change that is 90% TUI but moves one constant in
`coding-agent/src/core` is `port` (for that constant).

Specific rulings (from pilot runs — keep appending):
- **Release commits are `port`**: they regenerate `packages/ai/src/models.generated.ts`,
  which IS ported surface (→ `ai/models_catalog.json`, regenerated from the
  matching npm build). The version bump/changelog parts are noise; ALSO note
  the release tag so /pi-sync refreshes the npm reference build.
  `image-models.generated.ts` is excluded (images unported).
- `packages/coding-agent/src/utils/` is in scope only if a ported core file
  consumes it — judge by the consumer, not the path.
- `.pi/` files (upstream repo's own agent config/extensions) are always `n/a`.
- Generated/data files count as ported surface when we embed their derivative
  (currently: models.generated.ts → ai/models_catalog.json — the only one).
- A follow-on commit to a pending `decide` inherits that escalation: mark it
  `decide (rider on <sha>)` and batch into ONE question to the user.
- Once the user rules on a `decide`, record the ruling in docs/UPSTREAM.md
  ("Current pin" section's non-port list or a Rulings note) so future triage
  doesn't re-ask.
- Cost control: changes whose `--stat` touches only docs/CHANGELOG/.github/
  scripts/.pi/packages/tui may be dispatched from the diffstat alone; read
  hunks only when any path is in or near ported surface.
- **Merges can smuggle in-scope hunks** (2026-07-17 lesson: `5220aba6`'s xai
  responses hunk + `97f9978f`'s force flag + the v0.80.8 release all rode in
  via merges and were missed by per-first-parent diffstats — caught only by
  the adversarial parity review). After per-commit triage, ALWAYS reconcile
  with **both** passes. They catch different things; neither replaces the other:
  1. **Detector — never skip.** `git diff <pin>..origin/main --name-only` with
     NO pathspec, and classify every path it prints. This is the pass that
     surfaces a brand-new top-level package, a file moved out of `packages/`,
     or a new repo-root directory — things no pathspec written today can
     anticipate. It is what caught the 2026-08-22 blind spot.
  2. **Accounting.** `git diff <pin>..origin/main --stat -- packages
     ':(exclude)packages/tui' ':(exclude)packages/evals'
     ':(exclude)packages/coding-agent/docs'
     ':(exclude)packages/coding-agent/examples'` — account for every file's
     delta against the verdicts.

  Merge-commit diffstats must be read in full, not truncated.
- **Sweep whole packages; never carve out a sub-path.** That exclusion list is
  the ONLY place a path may be pre-judged, and each entry earns its place
  structurally — leaf UI library, upstream's own eval harness, prose, samples —
  never by citing a current ruling. Sub-path carve-outs fail three ways, all
  observed here:
  - **They hide live code.** `coding-agent/src` holds ported code well outside
    `core/`: `src/client/{remote-session,transcript}.ts` →
    `coding/remotesession.go` + `coding/transcript.go`, and
    `src/utils/{text,mime,paths,frontmatter,image-resize-core,image-process,tool-result-images}.ts`
    → `coding/{text,tools,resources,imageresize}.go`. A guard scoped to
    `src/core` prints none of them. Note this also makes the "judge `utils/` by
    the consumer, not the path" rule above *unexecutable*: you cannot judge a
    path the sweep never printed.
  - **They outlive the tree they describe.** The 2026-08-01 ruling's "minus
    `server/src/legacy/`" is dead text — upstream deleted that directory in
    `05bf9df65`. And `server/src/testing/**`, ruled "port only if the Go server
    tests want the same shape", quietly became ported
    (`server/internal/servertest/service.go` says so in its package doc). A
    carve-out inherited from a ruling reads as coverage while giving none.
  - **Some real exclusions are unexpressible as paths.** The 2026-08-06
    telemetry ruling excludes the schema/type-inference half of
    `packages/telemetry` — but `defineTelemetrySchema` and the ported runtime
    `Span` contract share `src/index.ts`. Only a human reading the hunk can
    separate them.

  Exclusions belong in the CLASSIFICATION step, executed on a hunk, never in
  the pathspec. The pathspec's job is to put paths on your screen.
- **A ported path does not imply a same-named Go package** — use this map to
  classify what the sweep prints. Upstream trees with a Go home today:
  `packages/ai/src` → `ai/` + `ai/providers/`; `packages/ai/scripts` →
  `ai/models_catalog.json` at the next release regen (**every** catalog-only
  queue delta lives in `scripts/generate-models.ts`, never in `src/`);
  `packages/agent/src` → `agent/`, with `harness/**` and `search/**` in scope
  but DEFERRED (add to the backlog — never a silent `n/a`);
  `packages/protocol/src` → `protocol/` + `protocol/cbor/`;
  `packages/client/src` → `client/`; `packages/server/src` → `server/` +
  `server/unix/` + `server/internal/servertest/`; `packages/telemetry` →
  `telemetry/` (runtime half only); `packages/session-backends` → in scope but
  DEFERRED; `packages/coding-agent/src/{core,client,utils}` → `coding/` — except
  two hunks that land in `ai/providers/` instead
  (`utils/pi-user-agent.ts` → `ai/providers/pi_user_agent.go`,
  `core/provider-attribution.ts` → `ai/providers/attribution.go`). The Rulings
  in `docs/UPSTREAM.md` are authoritative over this map; if they disagree,
  re-derive and fix the map.
- **One upstream fix can touch multiple sites that map to ONE Go file — or to a
  differently-structured Go file** (2026-07-21 lesson: `1942b260` "env section
  ignored" fixed BOTH `auth/helpers.ts` and `amazon-bedrock.ts`; the port landed
  the helpers half but missed the bedrock half, because in Go there is no
  `amazon-bedrock.ts` — ambient providers (bedrock/vertex/… anything not in
  `apiKeyEnvVars`) route through the GENERIC resolver in `builtins_models.go`
  `builtinProviderAuth`. Caught only by the parity review). When a change edits
  a shared helper AND a specific provider, list EVERY hunk in the commit and map
  each to its Go home — a provider-specific TS edit may collapse into a generic
  Go resolver rather than a same-named file. Don't assume one TS hunk = one Go
  edit, and don't assume a provider file has a 1:1 Go counterpart.
