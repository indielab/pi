---
name: pi-sync
description: Daily upstream-sync job for the pi Go port — fetch upstream pi, triage every change since the recorded pin, port what's in scope, verify idiomatic + parity via independent reviews, update the ledger, and push. Use for "sync with upstream", "porting job", or as the scheduled daily run.
---

# pi-sync — the daily porting job

Orchestrates one sync cycle. State lives in `docs/UPSTREAM.md` (the pin + the
ledger); this skill is restartable — a half-finished ledger resumes where it
stopped.

## 0. Preflight
- Repo: clean working tree on `main`, `git pull` first. Full gate must be
  green BEFORE starting (`go build ./... && go vet ./... && go test ./...`);
  never start a sync on a broken base.
- Upstream clone at `$PI_UPSTREAM_DIR` (default `~/.cache/pi-upstream`); clone
  if missing, `git fetch origin main`, then **fast-forward its working tree**:
  `git -C "$dir" checkout -B main origin/main`. The clone is a read-only mirror
  — nothing is ever committed to it, and the differential harness extracts from
  it with `git archive <sha>` — so this is always safe. Without it the checkout
  drifts arbitrarily far behind: at the 2026-08-11 sync it was still on a
  2026-06-07 commit, ~2 months stale, and a plain `grep` of it reported the
  OPPOSITE of what the pin contained, reading as though upstream had reverted
  the change being ported.
- **Standing rule, independent of the above: never read the clone's working
  tree.** Every upstream read names a sha — `git show <sha>:<path>`,
  `git diff <sha>^1..<sha>`, `git grep <pattern> <sha> -- <path>`. A
  fast-forwarded tree is at `origin/main`, which mid-cycle is AHEAD of the pin,
  so it is not the thing under triage or review either. Pass this rule to every
  triage/review subagent you spawn; the 2026-08-11 cycle is on record because a
  reviewer nearly filed a phantom finding from it.
- Read the pin from `docs/UPSTREAM.md`. Delta = first-parent main-line
  changes `pin..origin/main` (a merged PR = one unit). If empty: record the
  check date in UPSTREAM.md and stop.
- If the delta contains a release tag: refresh the npm reference build
  (`npm i @earendil-works/pi-ai@<ver> @earendil-works/pi-coding-agent@<ver>`
  in the scratch dir) so parity review compares against what now ships.

## 1. Triage (skill: pi-triage)
Run pi-triage over the whole delta (subagent). Append all rows to the ledger
in `docs/UPSTREAM.md` with their verdicts. `n/a` rows are done.
`port-but-QUEUED` and `port-but-CATALOG-ONLY` rows are appended to their entry
in the "Scope queue" / catalog queue instead of being ported. `decide` rows:
STOP and surface to the user — never silently expand or shrink the port's
scope. Note the 2026-08-27 rewrite made scope decidable by three EXCLUSION
TESTS, which should make `decide` rarer: before escalating, check that E1/E2/E3
in "The scope boundary" does not already answer it — and remember that "in scope
but no Go home yet" is a Scope queue row, not a `decide`.

## 2. Port (per `port`-verdict change, chronological)
- One subagent per change (or small coherent batch touching the same files).
  Input: the triage row (WHY/WHAT/file mapping), the upstream diff, and the
  standing rules: faithful to pi, npm build wins on drift, byte-exact
  model-visible strings, every behavior change test-locked, JS semantics
  ported deliberately (UTF-16 lengths, ??-semantics via pointers, Math.round).
- One Go commit per upstream change; message references the upstream sha:
  `port(<area>): <subject> (upstream <sha>)`.

## 3. Review gates (independent subagents — never the porter)
- **pi-go-review** on the ported diff → fix findings before proceeding.
- **pi-parity-review** on the ported diff vs the upstream change → fix
  divergences. If it says a golden must change, regenerate it from the npm
  build, never by hand.

## 4. Final gate
- `gofmt -l` clean, `go build ./... && go vet ./...`,
  `go test -race ./... -count=1` green.
- If the change touches any provider request building: `difftest/run.sh` must
  exit 0 (every scenario PASS or KNOWN). Exit 1 = a FAIL **or a DARK run that
  never reached the scenarios** — a run that aborts in the pi capture or the Go
  build prints no scenario tally and is not a result. Exit 3 = a stale baseline
  entry to retire. See pi-parity-review §3.
- `GOOS=windows go build ./... && GOOS=windows go vet ./...`. Unconditional, not
  gated on a `//go:build windows` file changing: the two constrained files are
  called from unconstrained code, so an ordinary `coding/` rename breaks the
  cross-target build while touching no constrained file.
- Every behaviour change is observed red for a **behavioural** reason before it
  ships. A compile-error red proves only that a symbol moved; re-engineer the
  test until it fails by asserting. Label a characterization test as such.

## 5. Record + ship
- **A cycle entry is the COMMIT MESSAGE, not a ledger section.** Anything
  durable — a ruling, a deliberate divergence, a known-debt item, a tripwire, a
  convention — goes into `docs/UPSTREAM.md`'s permanent sections (`### Rulings`,
  `## Divergences`, `## Open re-judgements`, `## Known parity debt`,
  `## Conventions`, `## Scope queue`) as a **row**. Git holds the narrative; the
  ledger holds what binds. **Do not append per-cycle sections** — that practice
  grew the ledger to 8,917 lines before it was cut back on 2026-09-03, and it
  will regrow if this rule is ignored.
- Move the **Current pin** to the new upstream sha; note the date and the new
  npm version if it changed.
- Re-pin the differential harness: set `PI_UPSTREAM_SHA` in
  `difftest/config.env` to the new pin (and `PI_NPM_VERSION` if a release was
  crossed), re-run `difftest/run.sh`, and retire any entry reporting FIXED.
- A cycle with **zero upstream drift is not a no-op cycle.** Reconcile the port's
  own `git log` since the last ledger entry against the Scope queue and the
  rulings before triaging: slices drain between cycles, and out-of-cycle commits
  answer consults, open or close rows, and fix ledger bugs.
- Every finding filed by `pi-go-review` / `pi-parity-review` is handed to
  separate agents instructed to **REFUTE** it; only survivors are fixed. (Filed
  vs survived: 9→4, 15→2, and on 2026-09-02 it caught a proposed test that would
  have pinned a real parity defect green.) Known artifact, not a finding: the pin
  and the cycle's divergence records are written at stage 5, after the gates run
  at stage 3, so a reviewer reading the mid-cycle diff correctly sees them stale.
- Commit the ledger update; push everything to
  `https://github.com/sky-valley/pi.git main` (HTTPS — SSH signing is not
  available to automation on this machine).
- **Cut the release IN THIS CYCLE iff the delta crossed a release tag** (an npm
  version bump — the same trigger that refreshes the reference build in §0).
  Releases are not cut separately; they happen here when the sync crosses one.
  - Version: **major.minor follow pi's npm catalog version; patch is the port's
    own monotonic counter.** Set `major.minor` to the crossed `pi-ai` release's
    `major.minor`, and set `patch` to the latest port tag's patch + 1 (the port
    counter never resets — so it stays distinct from pi's patch and there is no
    minor-vs-patch judgement to make). E.g. syncing pi 0.80.7 after `v0.80.10`
    → `v0.80.11`; if pi later bumps to 0.81.x, the next tag is `v0.81.<n+1>`. The
    version is git-tag-only; one tag per release-crossing cycle (cycles with no
    npm bump get no tag).
  - Tag the **ledger/pin-advance commit** (the tip of the sync) as an
    **annotated, unsigned** tag, tagger `Noam Y. Tenne <noam@10ne.org>`:
    `git -c user.name="Noam Y. Tenne" -c user.email="noam@10ne.org" tag -a
    vX.Y.Z <sha> -m "vX.Y.Z — upstream pin <sha>, npm pi-ai <ver> …"`.
  - Add the row + a Notes entry to `docs/RELEASES.md` (version, date, tagged
    commit, upstream pin, npm catalog, headline). If RELEASES.md is behind the
    tag list, backfill the missing tags from their `git tag -n99` messages first.
  - Push the tag: `git -c credential.helper='!gh auth git-credential' push
    https://github.com/sky-valley/pi.git vX.Y.Z` (HTTPS, same as the branch push).
  - **Draft the release tweet** (we tweet on every release cut). Surface it for
    the human to post — do NOT auto-post (publishing is the owner's action).
    Owner's voice/format, verbatim shape:

    ```
    Gophers of pi!

    vX.Y.Z has dropped, tracking pi <npm-ver> 🎉

    What's fresh?
    :: <high-level change 1>
    :: <high-level change 2>
    :: <high-level change 3>

    Keep it real 👉 https://github.com/sky-valley/pi
    ```

    Rules: open with `Gophers of pi!`; name the port version and the pi npm
    version it tracks; up to 3 high-level `:: `-prefixed changes (plain, no jargon
    dump); link the **repo, not the release**; keep the upbeat tone.
- Report the **Scope queue** state (entries drained, deltas appended) alongside
  the catalog-only queue. (The harness backlog is entry 8's queued-deltas column.)
- Report: N changes — X ported (with commits), Y n/a, Z escalated; the release
  tag if one was cut; any test count change; any new deliberate divergence added
  to UPSTREAM.md.

## Hard rules
- **A public Go API break that FOLLOWS upstream is not an escalation — ship it.**
  (2026-09-03 governing ruling: upstream deletes, we delete; no shims, no
  deprecation window, no keeping a symbol because a tag published it. Do not open
  a `decide` to ask whether breaking is acceptable.) Escalate only an API change
  the port would be making under its own steam.
- Anything that changes what an EXCLUSION TEST **SAYS** (E1 or E3 in
  `docs/UPSTREAM.md` -> "The scope boundary") → escalate, don't ship. Moving a
  path is not a boundary change when a test already covers it.
- A port without a test does not ship. A parity divergence "fixed" by editing
  the assertion to match our output does not ship — goldens come from pi.
- If the cycle can't finish (e.g. blocked on a decision), ship the completed
  prefix of the ledger; never leave the repo red.
