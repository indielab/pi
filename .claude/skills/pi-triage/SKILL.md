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
- **The authoritative scope boundary is `docs/UPSTREAM.md` -> "The scope
  boundary" — three PREDICATES, rewritten 2026-08-27. Read it before judging
  anything.** It replaced an exclusion path-list; the tests beat every list,
  including the tables in this file. Also read "Scope queue" (what is IN scope
  but has no Go base yet) and the "Current pin" table.

## Per change, produce:
1. **WHY** — intent, from the commit/PR message (`git log -1 --format=%B`) and
   any linked issue number in it.
2. **WHAT** — read the actual diff (`git diff <sha>^1..<sha> --stat` then the
   hunks for non-trivial files). Note behavior, constants, model-visible
   strings, new/changed tests.
3. **SCOPE** — verdict, one of:
   - `port` — touches behavior we ported.

     **Scope is decided by the three EXCLUSION TESTS in `docs/UPSTREAM.md` ->
     "The scope boundary" (rewritten 2026-08-27). Read them; they are
     authoritative over this table.** A hunk is `n/a` iff one fires; otherwise
     it is IN. The table below is a *derived convenience* — if it and a test
     disagree, the test wins, you port accordingly, and you fix the table in the
     same commit.

     The three tests, in one line each:
     - **E1 host surface** — its job ends at presenting to / prompting /
       configuring a human at a terminal, or packaging the Node artifact, **or
       its only consumers are host surface**. Applies to a HUNK, not just a file.
     - **E2 `go.mod`** — a `packages/ai/src` adapter is OUT iff parity needs a
       third-party Go module for transport, wire encoding, or credential chain.
     - **E3 no Go representation** — a TS type-level construct with no runtime
       behavior (today: the telemetry schema half only).

     **Reachability is evidence, not a test.** Root-exported means "probably SDK
     surface" and is why the default is IN — but it argues one direction only
     and NEVER overrides E1-E3. pi root-exports its whole application, TUI
     components included.

     | upstream tree | Go home | notes |
     |---|---|---|
     | `packages/ai/src` | `ai/`, `ai/providers/` | **OUT:** `amazon-bedrock` and `openai-codex` (E2 — SigV4/eventstream; WebSocket/zstd) plus the helpers reachable only through them (`utils/node-http-proxy.ts`, `utils/abort-signals.ts` — NOT `utils/uuid.ts` or `session-resources.ts`, which are root-exported, have ported consumers, and in uuid's case are already ported), and `cli.ts` (E1). Azure, Mistral, Vertex, Radius, images and `auth/oauth/**` are IN as of 2026-08-27 — most are QUEUED, see below |
     | `packages/ai/scripts` | `ai/models_catalog.json` **at the next release regen** | generator-only; verdict is `port-but-CATALOG-ONLY`. Includes `generate-image-models.ts` as of 2026-08-27 |
     | `packages/agent/src` | `agent/` (harness deltas land per the chosen shape — default `coding/`) | `harness/**` and `search/**` are IN scope, FUNDED and draining as QUEUED (entry 8) — backlog them, never `n/a`, and **never escalate them again** (2026-08-27) |
     | `packages/protocol/src` | `protocol/`, `protocol/cbor/` | 2026-08-01. Byte-golden to a PEER: CBOR + frame layout |
     | `packages/client/src` | `client/` | 2026-08-01 |
     | `packages/server/src` | `server/`, `server/unix/`, `server/internal/servertest/` | 2026-08-01. Both of that ruling's carve-outs are stale and NOT to be re-applied: `server/src/legacy/**` no longer exists (`05bf9df65`), and `server/src/testing/**` IS ported |
     | `packages/telemetry` | `telemetry/` | 2026-08-06, **runtime half only** — the schema/type-inference half (`defineTelemetrySchema`, `Infer*`, `SchemaTelemetrySpan`) is `n/a` and shares `src/index.ts` with the ported half, so this one must be split by HUNK |
     | `packages/session-backends` | — | 2026-08-07: IN scope but QUEUED |
     | `packages/coding-agent/src/{core,client,utils,server}` | `coding/` (`src/server/` is the harness factory — queue entry 8) | **OUT** (E1, and this list must match the ledger's out-table exactly): `core/extensions/**`, `core/export-html/**`, `core/settings-manager.ts`, `core/prompt-templates.ts`, `core/agent-session-runtime.ts` + the session-reload / `/new` lifecycle, `core/model-registry.ts`, `core/resolve-config-value.ts`, `core/radius.ts`, `core/resource-loader.ts` source-info accessors, `migrations.ts`. **Partly ported, split by HUNK, never diffstat-dispatched:** `core/model-resolver.ts`, `core/package-manager.ts`, `core/trust-manager.ts`. The trust *decision and gate* are IN (2026-08-27); the trust prompt, selector and store are host. Two hunks land in `ai/providers/` instead: `utils/pi-user-agent.ts` -> `pi_user_agent.go`, `core/provider-attribution.ts` -> `attribution.go` |

     `utils/` is judged **by its consumer, not its path** — in scope only when a
     ported core file consumes it.

     **Project-local reads carry their gate.** Any change that adds a read of
     `<cwd>/.pi/**` or an ancestor `.agents/**` must ship with its project-trust
     gate (`SessionOptions.TrustProject` / `LoadSkillsWithTrust`). An ungated
     new reader is a **defect**, not a scope question (2026-08-27).

   - `port-but-QUEUED` — the tree is IN scope but has **no Go base yet**, so a
     hunk-sized port of a file that does not exist cannot land. Verdict is
     `port`; no Go commit this cycle; append the delta to its entry in
     `docs/UPSTREAM.md` → "Scope queue" instead. This is the
     `port-but-CATALOG-ONLY` pattern generalized. The current entries are the
     rows in `docs/UPSTREAM.md` -> "Scope queue" (today: OAuth acquisition,
     Radius, images, Azure, Vertex, Mistral, session-backends, agent harness) —
     read them there rather than trusting this sentence, and **open a new row
     yourself** when an in-scope change has no Go home. Once an entry's base
     port ships, the entry closes and that tree triages as ordinary `port`.

   - `port-but-CATALOG-ONLY` — a `packages/ai/scripts` generator delta. It is a
     `port`, but nothing lands until a release tag is crossed and the catalog is
     regenerated from the npm build. Add it to the queue in `docs/UPSTREAM.md`
     rather than porting it now.
   - `n/a` — excluded by a test. Give the specific reason **naming which test
     fired (E1/E2/E3)**, and prefer a **structural** check over precedent alone (grep
     the port for the identifiers the change touches; a recorded absence is
     worth more than a category).

     **Host surface (E1) — dispatch from the diffstat, no hunk-reading owed.**
     `packages/tui`, `packages/evals`, `packages/ai/src/cli.ts`, and under
     `packages/coding-agent/src`: `modes/**` (including `rpc/` — headless is
     still host), `cli/**`, `main.ts`, `core/extensions/**`,
     `core/export-html/**`, `core/settings-manager.ts`,
     `core/prompt-templates.ts`, `core/agent-session-runtime.ts` + the
     session-reload / `/new` lifecycle, `core/model-registry.ts`,
     `core/resolve-config-value.ts`, `core/radius.ts`, `core/resource-loader.ts`
     source-info accessors, `migrations.ts`, `bun/**`, `package-manager-cli.ts`.

     `core/model-registry.ts`, `core/resolve-config-value.ts`, `core/radius.ts`
     and the `core/resource-loader.ts` accessors are E1's **only-consumers**
     clause — host machinery populating a ported-and-latent SDK seam. That
     clause fires **only when EVERY consumer is host**; a mixed consumer set
     does not fire it, so read the hunk.

     **NEVER diffstat-dispatch these four — they are partly ported and the
     shortcut would manufacture a miss** (see the split-by-HUNK table in
     `docs/UPSTREAM.md`): `core/model-resolver.ts` (its
     `defaultModelPerProvider` is `coding/resolve.go`, and that table has caused
     a miss TWICE — diff it on every touch), `core/package-manager.ts` (skill
     discovery is `coding/resources.go`), `core/trust-manager.ts` (decision and
     gate are ported), `packages/telemetry/src/index.ts` (E3). **A commit spanning the seam is triaged on its
     non-host hunks alone and NEVER escalates on account of the host half.**
     This is the rule that removes most of the per-commit cost — roughly half of
     all mixed commits are mixed only because of host content.

     Also `n/a`: `amazon-bedrock` and `openai-codex` plus `utils/node-http-proxy.ts`
     and `utils/abort-signals.ts` (E2); the telemetry schema half (E3 — split by HUNK, it
     shares `src/index.ts` with the ported runtime half); the trust prompt,
     selector and store; and the always-noise set — docs, CHANGELOG, CI,
     `.github/`, `.pi/`, examples, per-package `package.json` version bumps,
     repo-root `scripts/`.

     **No longer `n/a` as of 2026-08-27** — do not carry these forward from
     memory or from an older cycle section: OAuth acquisition, Radius +
     `radius-config`, image generation, Azure, Mistral, Vertex, and the
     project-trust decision/gate. They are `port` (mostly `port-but-QUEUED`).
   - `decide` — changes the *boundary* itself. Escalate to the user instead of
     deciding silently.

     **But do not reach for `decide` reflexively.** The three exclusion tests plus
     the owner's **standing formula** — full pi SDK functionality as represented
     in Go, close faith to the source architecture, leaning into Go's idioms —
     already answer nearly everything that looks new. Re-asking a settled
     question is itself a failure mode. These axes are SETTLED and must not be
     re-escalated:
     - a transport or feature depending on a runtime Go does not target
       (2026-08-11);
     - a `DRAFT:` subject prefix (2026-08-04);
     - an upstream REVERT, even one removing Go API the port shipped days
       earlier (2026-08-19, unless a cut tag already published it);
     - a pre-existing parity gap inside an already-ported function (2026-08-18);
     - **anything E1 covers** — never escalate on a host hunk (2026-08-27);
     - **whether an adapter is in scope** — ask `go.mod`, not the user
       (2026-08-27);
     - **the agent harness** — FUNDED 2026-08-27 and actively draining as queue
       entry 8. Carry its deltas as `port-but-QUEUED`; do not re-open the
       scope question. The only harness matter still open is its SHAPE (see
       "Harness shape" in `docs/UPSTREAM.md`), which is an owner
       architecture call, not a per-commit triage question.

     **IN scope but no Go home is NOT a `decide`.** Open a Scope queue row
     instead — the tests already answered scope; only scheduling is open, and
     opening a row needs no owner decision. See "How to OPEN an entry" in
     `docs/UPSTREAM.md`. Escalating here is exactly how the harness got stuck.

     A genuinely new question changes a **test**, not a path. The two that
     exist: *"should the port take a third-party dependency?"* (which would move
     bedrock/codex and decide session-backends) and a transport that changes the
     `ai.HTTPDoer` seam itself. Both are owner policy questions with durable
     answers.
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
  `image-models.generated.ts` joins the catalog-regen surface as of 2026-08-27 (queue entry 3) — regen it alongside `models.generated.ts`.
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
  ("### Rulings", and update "The scope boundary" if a test moved) so
  future triage doesn't re-ask.
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
  sweep prints against the EXCLUSION TESTS in `docs/UPSTREAM.md` -> "The scope
  boundary", using the table in the SCOPE section above only as a lookup for Go
  homes. Do not maintain a second list anywhere: a duplicated table is how the
  skill and the rulings drifted apart for four cycles (2026-08-25), and a list
  of any kind is what the 2026-08-27 rewrite demoted — 12 of 16 scope rulings
  had moved a path INTO scope and no list survived that. If the table and a
  test disagree, the test wins, you port accordingly, and you fix the table in
  the same commit.
  Two path facts worth carrying that the table does not:
  - Catalog-only deltas live under `packages/ai/scripts`, but **not only in
    `generate-models.ts`** — `650e7a612` added a sibling
    (`scripts/openrouter-reasoning-options.ts`), so sweep the whole `scripts`
    tree rather than grepping one filename.
- **Host-seam commits are diffstat-dispatchable, and that is the point.** When
  a commit's excluded content is entirely host surface (E1), you owe no
  hunk-reading for it and no escalation — triage the non-host hunks and move on.
  Historically ~47% of mixed commits were mixed *only* because of host content,
  and reading them cost a cycle's attention to reach a foregone `n/a`.
- **Ask `go.mod`, not the user, about an adapter.** "Would porting this add a
  `require` line?" is the whole test (E2). Bedrock and codex are the
  only two that fail it today. A hunk *inside* one of them is still `port` when
  its Go home is a shared/generic function — the 2026-07-21 lesson applies
  unchanged.
- **A queued tree is `port-but-QUEUED`, never `n/a`.** OAuth acquisition,
  Radius, images, Azure, Mistral, Vertex, the agent harness and session-backends
  are IN scope with no Go base yet. Append the delta to its "Scope queue" entry
  in `docs/UPSTREAM.md`. Marking one `n/a` re-creates exactly the drift the
  2026-08-27 rewrite closed.
- **A new project-local read must carry its gate.** Any change adding a read of
  `<cwd>/.pi/**` or an ancestor `.agents/**` ships with the project-trust gate.
  Ungated is a defect, not a scope question (2026-08-27).
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
