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
   - `port` — touches behavior we ported. **These are the in-scope trees, all of
     them.** The list below and the Go-home map in the Rulings section are the
     same list; if they ever disagree, `docs/UPSTREAM.md` decides and you fix
     both.

     | upstream tree | Go home | notes |
     |---|---|---|
     | `packages/ai/src` | `ai/`, `ai/providers/` | except oauth acquisition, images, cli, and the bedrock/vertex/mistral/azure/codex providers |
     | `packages/ai/scripts` | `ai/models_catalog.json` **at the next release regen** | generator-only; verdict is `port-but-CATALOG-ONLY` (see below) |
     | `packages/agent/src` | `agent/` | `harness/**` and `search/**` are IN scope but DEFERRED — backlog them, never `n/a` (2026-08-07) |
     | `packages/protocol/src` | `protocol/`, `protocol/cbor/` | 2026-08-01. Byte-golden to a PEER: CBOR + frame layout |
     | `packages/client/src` | `client/` | 2026-08-01 |
     | `packages/server/src` | `server/`, `server/unix/`, `server/internal/servertest/` | 2026-08-01. Both of that ruling's carve-outs are now stale and are NOT to be re-applied: `server/src/legacy/**` no longer exists (upstream deleted it in `05bf9df65`, 2026-08-04), and `server/src/testing/**` IS ported (`server/internal/servertest/`) |
     | `packages/telemetry` | `telemetry/` | 2026-08-06, **runtime half only** — the schema/type-inference half (`defineTelemetrySchema`, `Infer*`, `SchemaTelemetrySpan`) is `n/a` and shares `src/index.ts` with the ported half, so this one must be split by HUNK |
     | `packages/session-backends` | — | 2026-08-07: IN scope but DEFERRED, same as the harness tree |
     | `packages/coding-agent/src/{core,client,utils}` | `coding/` | except extensions runtime, trust-manager, bun/packaging, `modes/**` (at the pin: `interactive/`, `rpc/`, `print-mode.ts`, `json-event.ts` — note the TUI itself is `packages/tui`, not a mode). The port has no `modes/` analogue at all; `cmd/pi` is a hand-rolled SDK CLI with a print flag of its own, not a port of pi's mode layer, so do not map a mode change onto it. Also excluded: agent-session-runtime. Two hunks land in `ai/providers/` instead: `utils/pi-user-agent.ts` → `pi_user_agent.go`, `core/provider-attribution.ts` → `attribution.go` |

     `utils/` is judged **by its consumer, not its path** — in scope only when a
     ported core file consumes it.
   - `port-but-CATALOG-ONLY` — a `packages/ai/scripts` generator delta. It is a
     `port`, but nothing lands until a release tag is crossed and the catalog is
     regenerated from the npm build. Add it to the queue in `docs/UPSTREAM.md`
     rather than porting it now.
   - `n/a` — only touches non-ported surface — give the specific reason, and
     prefer a **structural** check over precedent alone (grep the port for the
     identifiers the change touches; a recorded absence is worth more than a
     category). Non-ported: TUI, `modes/**`, extensions runtime, OAuth
     acquisition, project trust, the unported providers, image generation,
     bun/CLI packaging and installer/self-update, prompt-templates,
     settings-manager, config migrations, agent-session-runtime,
     the telemetry schema half, Radius OAuth + its host wiring, docs, CHANGELOG,
     CI, `.github/`, `.pi/`, examples.
   - `decide` — changes the *boundary* itself. Escalate to the user instead of
     deciding silently.

     **But do not reach for `decide` reflexively.** The owner's **standing
     formula** — full pi SDK functionality as represented in Go, close faith to
     the source architecture, leaning into Go's idioms — already answers most of
     what looks new, and the deciding fact is almost always **published,
     independently reachable surface** (`src/index.ts` or the package `exports`
     map). Re-asking a settled question is itself a failure mode. In particular
     these axes are SETTLED and must not be re-escalated: a transport or feature
     depending on a runtime Go does not target (2026-08-11); a `DRAFT:` subject
     prefix (2026-08-04); an upstream REVERT, even one removing Go API the port
     shipped days earlier (2026-08-19, unless a cut tag already published it); a
     pre-existing parity gap inside an already-ported function (2026-08-18).
     A genuinely new question changes a seam — e.g. a transport that changes the
     `ai.HTTPDoer` seam itself.
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
  A release tag ALSO drains the `port-but-CATALOG-ONLY` queue — but only of the
  deltas that are **ancestors of the release sha**. Check that with
  `git merge-base --is-ancestor <delta> <release>`, never by log order: generator
  deltas landing after the release commit belong to the NEXT regen. And decide
  the regen itself by EXECUTING `JSON.stringify(MODELS)` against both npm builds
  (2026-07-30 ruling) — a git sweep can never see catalog data, since it is
  generated at publish time and not committed upstream.
- `packages/coding-agent/src/utils/` is in scope only if a ported core file
  consumes it — judge by the consumer, not the path. Note this rule is
  UNEXECUTABLE if the reconciliation sweep never printed the path, which is why
  the sweep must never carve out a sub-path (see below).
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
- **A ported path does not imply a same-named Go package.** Classify what the
  sweep prints against the single table in the SCOPE section above — that table
  is the ONE list of in-scope trees and their Go homes. It deliberately lives in
  the verdict rules rather than down here, because a triager assigning a verdict
  reads the verdict rules; a second copy in this section is how the two drifted
  apart for four cycles (the copy up top named three trees while the rulings had
  ruled in five more). **Do not reintroduce a second copy.** `docs/UPSTREAM.md`
  is authoritative over both; if they disagree, re-derive and fix the table.
  Two path facts worth carrying that the table does not:
  - Catalog-only deltas live under `packages/ai/scripts`, but **not only in
    `generate-models.ts`** — `650e7a612` added a sibling
    (`scripts/openrouter-reasoning-options.ts`), so sweep the whole `scripts`
    tree rather than grepping one filename.
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
