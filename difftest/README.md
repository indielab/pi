# pi-vs-Go differential request-body harness

The strongest fidelity proof this project has: for identical inputs, capture the
**exact HTTP request body** that real pi builds and that the Go port builds,
canonicalize both, and diff.

Neither side needs an API key, and neither side touches the network — both
capture through a payload hook that halts before the first byte is sent:

- **pi**: `onPayload` in `@earendil-works/pi-ai` stream options; it throws.
- **Go**: `OnPayload` on `ai.StreamOptions`; it returns an error.

> **Where this lives, and why it moved.** It ran from `/tmp` (lost twice), then
> from `~/.cache/pi-diff` (durable, but unversioned and on exactly one disk —
> every ledger entry cited results from a rig with no history and no backup).
> It now lives in the repo it verifies, because that is the only place it can
> stay in lockstep: each sync re-pins it and flips scenarios based on the port's
> own state, and the two drifted whenever they were kept apart.
>
> The earlier note here said it must **not** live in the repo, because
> `github.com/sky-valley/pi` is published, public and MIT. That concern is
> resolved rather than overruled: `difftest/` carries its own `go.mod`, and a
> subdirectory with a `go.mod` is excluded from the parent module. Verified, not
> assumed — a module zip built with `golang.org/x/mod/zip`, the same library the
> module proxy uses, contains **zero** `difftest/` entries. Nobody running
> `go get github.com/sky-valley/pi` downloads any of this.
>
> Nothing here is sensitive: scenarios are synthetic, keys are the literal
> `"test-key"`, and the porting internals it exercises are already public in
> `docs/UPSTREAM.md`. Do not add a real key or a real transcript to a scenario.

## Run it

```
difftest/run.sh                    # everything; exits non-zero on any NEW mismatch
difftest/run.sh --only deepseek    # one scenario
difftest/run.sh --list             # scenario names
```

`run.sh` is self-healing: if the npm build or the extracted upstream source is
missing it rebuilds them before capturing. Nothing here is precious — deleting
`out/` or `pisrc/` costs one run.

It also re-verifies package authenticity every run: the `package-lock.json`
integrity hash must equal `npm view <pkg>@<version> dist.integrity`, or the run
aborts. (If the registry is unreachable the check is reported as SKIPPED, never
silently passed.)

## The four states, and the exit-code contract

A runner that can never exit 0 is a broken instrument. Three scenarios fail on
one known, escalated, pre-existing divergence (tool-call argument key order),
and if that permanently pinned the exit code at 1, the exit code would carry no
information and the next NEW regression would hide inside the noise — reviewers
would learn to read "3 FAIL" as normal.

So every scenario lands in exactly one of four states:

| state | meaning |
| --- | --- |
| **PASS** | pi and Go are identical. |
| **KNOWN** | Differs, and **every** difference found matches a `known-divergences.json` entry exactly — same scenario, same JSON path, same kind of difference. Accepted, tracked debt. Not clean. |
| **FAIL** | Differs in any way the baseline does not cover. A port bug until proven otherwise. |
| **FIXED** | A baseline entry that **no longer fires**. The divergence was repaired; the entry is now stale. |

Exit codes:

| exit | meaning |
| --- | --- |
| `0` | every scenario is PASS or KNOWN, and every baseline entry still fires |
| `1` | at least one **FAIL** |
| `3` | no FAIL, but at least one **FIXED** (stale baseline entry) |

The summary line always prints the KNOWN count, and never renders as clean:

```
8 PASS, 3 KNOWN (tracked debt), 0 FAIL  [11 scenario(s)]
KNOWN is not clean: it is accepted, tracked debt. See known-divergences.json.
```

**Why FIXED fails the run (exit 3), deliberately.** A stale entry is worse than
no entry: it is a live, unattended permission slip. It says "a key-order
difference at `$.messages[2].tool_calls[0].function.arguments` in
`basic-tools-cache` is expected" — so the day someone reintroduces exactly that
regression, the harness reports KNOWN and exits 0, and the bug ships. The
window between "fixed" and "entry deleted" is precisely when that hole is open,
so the runner refuses to be green during it. The cost is one loud run for
whoever lands the fix; the alternative is a silent hole nobody is looking at.
FIXED gets its own code (`3`) rather than reusing `1` so that CI, or a human
skimming, can tell "the port got worse" from "the port got better and the
paperwork is behind" — but both are non-zero, because both need a human.

## Known divergences: `known-divergences.json`

**A baseline entry is tracked debt with a fixed home in the ledger — it is not
a permanent exemption, and it is not a statement that the Go behavior is
acceptable.** pi is still ground truth at every one of these paths. An entry
buys time on a divergence that is *already escalated and already written down
somewhere durable*; it does not close the bug, and it must never be added
merely to turn a red run green.

Each entry carries `id`, `description`, `symptom`, `root_cause`, `impact`,
`why_not_fixed`, `status`, `accepted_on`, `tracked_in` (which ledger, which
section), `retire_when`, and a list of `matches`.

### The matching rule is deliberately tight

A `matches` element excuses **exactly one difference**, keyed on all three of:

- **scenario** — the scenario name;
- **path** — the exact JSON path (`[*]` is accepted as an array-index wildcard,
  but prefer exact indices: a wildcard is looser than it looks);
- **kind** — the *nature* of the difference.

Kinds the classifier produces: `missing-key`, `extra-key`, `type`, `value`,
`array-length`, `embedded-json-key-order` (two JSON **strings** that parse to
the same content with different key order — how the divergence shows up on
openai-completions, where `arguments` is a string), `key-order` and `key-set`
(from the declared order-sensitive paths in `order.txt`), and
`order-path-missing`.

This is **not** "ignore these scenarios" and **not** "ignore these paths". If
`basic-tools-cache` starts differing in a different field, or the *same* field
differs by **value** instead of key order, or the key **set** changes rather
than the order, the classifier calls it FAIL — the entry does not absorb it.
That is the whole point: the harness exists to catch those.

A scenario is KNOWN only when *every* difference it exhibits is matched. One
unmatched difference makes the whole scenario FAIL (the matched ones are still
printed, as `(also: known …)`, so the noise does not obscure the signal).

### Adding an entry

Do not add one to make a failing run green. Add one only when all of these hold:

1. pi is confirmed right and Go is confirmed wrong (an entry is never a
   canonicalizer loosening, and never a change to the pi side).
2. The divergence is **already escalated and recorded in a durable ledger** —
   normally the pi repo's `docs/UPSTREAM.md` drift note for the cycle. Fill in
   `tracked_in` with the section, not just the filename.
3. `why_not_fixed` states a real, specific blocker (e.g. "needs a public Go API
   change"), not "not got to it yet".
4. `retire_when` states the observable condition that makes the entry stale.
5. The `matches` are as narrow as you can make them — exact scenario, exact
   path, exact kind.

Then `./run.sh` and confirm the scenario reports KNOWN, not PASS and not FAIL.

### Retiring an entry

When the runner prints **FIXED**, the work is not "make the harness quiet" — it
is:

1. Delete that `matches` element from `known-divergences.json`. If the entry
   has no `matches` left, delete the whole entry.
2. Close it out where it is tracked (the `tracked_in` ledger section): the drift
   note should say it is fixed, not leave a reader believing it is still open.
3. Re-run. The scenario should now be PASS and the run should exit 0.

Retire an entry the moment it stops firing. Do not "keep it around just in
case" — that is exactly the stale permission slip described above.

## What is compared, and against which pi

Configuration lives in `config.env`. There are two pi backends, because the Go
port is currently **ahead of the last npm release**:

| backend | source | when to use |
| --- | --- | --- |
| `dist` | `~/.cache/pi-npm/<version>/` — the published `@earendil-works/pi-ai` build | anything that has shipped. Strongest ground truth: it is what real pi actually runs. |
| `src` | `~/.cache/pi-upstream` at `PI_UPSTREAM_SHA`, extracted read-only via `git archive` into `pisrc/<sha>/` and executed directly under Node's TypeScript type-stripping | changes ported **before** they were released. The published build does not contain them at all, so it cannot be the reference. |

`0.83.0` genuinely has no `samplingParams`, no Baseten provider, and the
pre-Gemini-3 `requiresToolCallId`. Scenarios covering those are `"backend":
"src"` and say so in their `note`.

**pi is ground truth.** A mismatch is a port bug until proven otherwise. Do not
"fix" a mismatch by editing the pi side, by loosening the canonicalizer, or by
adding a `known-divergences.json` entry (see "Known divergences" above — an
entry records escalated debt, it does not excuse a fresh mismatch).

## Canonicalization: what it normalizes, and what it deliberately does not

Each side writes three files per scenario into `out/<side>/`:

- `<name>.raw.json` — the wire bytes, exactly as serialized for the request.
- `<name>.body.json` — canonical form, **object keys sorted**, used for the content diff.
- `<name>.order.txt` — one line per object, `<path>\t<keys in ORIGINAL order>`.

`pi/canon.mjs` and `port/canon.go` are deliberate twins. Change one, change both.

**Normalized** (differences here are hidden, on purpose):

- **Object key order** in `body.json` — keys are sorted before diffing. A JSON
  object is unordered by specification, and Go marshals `map[string]any` with
  sorted keys unconditionally, so a raw order comparison would be pure noise on
  every scenario. This is why `order.txt` exists: **order is never discarded,
  only moved to a separate assertion** (see below).
- **String escaping** — both sides use one JSON-string writer matched to
  `JSON.stringify` (ES2019 well-formed). Go's `encoding/json` escapes `<`, `>`,
  `&`, U+2028 and U+2029 by default and JS does not; that is a serializer
  artifact, not a request difference, so the Go side disables HTML escaping and
  both sides re-encode through the same routine.
- **Indentation / whitespace.**

**Not normalized** (differences here will fail a scenario, on purpose):

- **Number formatting.** Numbers keep their original literal text on both sides
  (`json.Decoder.UseNumber` in Go, `JSON.parse` source access in JS), so `1024`
  vs `1024.0` vs `1e3` is a visible failure rather than a float round-trip.
- **Array order** — never sorted, ever.
- **`null` versus absent** — a present `null` is a key; an absent key is not.
  This is the distinction most pi porting bugs live in.
- **Key sets** — an extra or missing key always fails.
- **Key order at paths a scenario declares order-sensitive.**

### Order-sensitive paths

A scenario may declare `orderSensitivePaths`. Those paths are compared for key
order against `order.txt`, in addition to the content diff. Use it wherever
pi's insertion order is genuinely observable to the model or the provider —
the repo has an `orderedJSONObject` mechanism for exactly these places.

Currently declared:

- `$.chat_template_args` (Baseten) — fed into a chat template, where order
  changes the rendered prompt.
- `functionCall.args` on the google scenarios — the arguments a model authored.
- `$.messages[i].reasoning_details[j]` on the `reasoning-details-replay-*`
  scenarios — OpenRouter wants the sequence back unmodified, and the order is
  the parsed object's own-property order (array-index keys first, ascending,
  then creation order), not the provider's byte order.

### Known limits (things this harness does NOT prove)

- **`google-generative-ai` is only partially covered.** pi's `onPayload` sees
  the `@google/genai` SDK *call params* (`{model, contents, config}`) and the
  SDK builds the REST body internally; pi explicitly rejects a custom `fetch`,
  so pi's real REST body is not observable at all. The Go port has no such SDK
  and builds the REST body directly. The two hooks therefore sit at different
  layers. Only `$.contents` is the same artifact on both sides, so the google
  scenarios set `comparePaths: ["$.contents"]` and compare nothing else —
  `generationConfig`, `tools` and `systemInstruction` are **out of reach**.
- Request **headers**, URLs and auth are not compared here (the in-repo
  `ai/providers/differential_pi_test.go` covers some header behavior).
- Only the request is compared. Response parsing is not in scope.

## Scenarios

`scenarios/*.json` — one self-contained file each: model, context, options.
Both sides read the same file, so neither can quietly diverge on inputs.

| scenario | backend | what it pins |
| --- | --- | --- |
| `basic-tools-cache` | dist | multi-turn transcript with a tool call + tool result, tool schemas, session prompt caching |
| `reasoning` | dist | unified thinking level → `reasoning_effort`, thinking blocks replayed |
| `long-cache` | dist | `cacheRetention: long` → `prompt_cache_retention: "24h"`, 64-char session-id clamp |
| `together` | dist | `thinkingFormat: "together"`, `supportsReasoningEffort: false` |
| `deepseek` | dist | `thinkingFormat: "deepseek"`, `requiresReasoningContentOnAssistantMessages` |
| `openrouter` | dist | `thinkingFormat: "openrouter"` + `cacheControlFormat: "anthropic"` breakpoints |
| `sampling-params` | dist | model+request `samplingParams` merged per key, applied **last** so they override named fields |
| `baseten-thinking` | dist | `thinkingFormat: "baseten"` → `chat_template_args` + `reasoning_effort` via `thinkingLevelMap` |
| `baseten-thinking-off` | dist | the same, thinking OFF: `omitWhenOff`, `thinkingLevelMap.off` |
| `gemini3-tool-ids` | dist | gemini major ≥ 3 ⇒ `functionCall`/`functionResponse` carry `id` |
| `gemini25-tool-ids` | dist | control: gemini 2.x stays below the threshold, no `id` |
| `tool-choice-without-tools-absent` | src | `tool_choice` omitted when the request carries no tools (pi's `params.tools?.length` guard) |
| `tool-choice-with-tools-control` | src | control for the row above: with tools present the key IS sent |

The `backend` column above is a snapshot. Scenarios flip `src` -> `dist` as releases
ship the surface they cover, so re-read `scenarios/*.json` rather than this table when
the distinction matters. As of pi-ai 0.84.3 the suite is 47 `dist` + 2 `src`.

## Adding a scenario

1. Drop a new `scenarios/<name>.json`. Fields:
   - `name`, `note` — `note` is documentation; say what the scenario pins and
     cite the upstream sha if it is a recent port.
   - `backend` — `"dist"` (shipped) or `"src"` (unreleased; see the table above).
   - `api` — `openai-completions`, `google-generative-ai`, `anthropic-messages`,
     `openai-responses`.
   - `entry` — `"stream"` or `"streamSimple"`. Some behavior only exists on one:
     the model+request `samplingParams` merge, for instance, happens in pi's
     `simple-options.ts` and so is `streamSimple`-only.
   - `model`, `context` — pi's own wire shapes. Go decodes them through
     `ai.Model` / `ai.UnmarshalMessage`, so they must be valid pi JSON.
   - `options` — pi-shaped camelCase.
   - optional `orderSensitivePaths`, `comparePaths`.
2. `./run.sh --only <name>`.

If an option key is not yet mapped, the Go side fails loudly (it decodes with
`DisallowUnknownFields`) and tells you to add the field to `scenarioOptions` in
`port/main.go` — it will never silently ignore an input, which would make both
sides agree for the wrong reason. Adding a new `api`/`entry` combination means
adding a case to `capture()` in `port/main.go`.

## Re-pointing at a new npm version / upstream sha

Edit `config.env`:

- `PI_NPM_VERSION` — `run.sh` installs it into `~/.cache/pi-npm/<version>/` on
  the next run and verifies its integrity against the registry.
- `PI_UPSTREAM_SHA` — set to the sha the port is synced to (the "TS source fully
  reviewed/ported" row of the repo's `docs/UPSTREAM.md`). `run.sh` extracts it
  read-only with `git archive`; `~/.cache/pi-upstream` is never modified,
  never checked out.

When a release finally ships a change that a `"src"` scenario covers, flip that
scenario to `"backend": "dist"` — the published build is the better reference.

## Layout

```
config.env          versions, shas, paths
run.sh              THE runner: rebuild both sides, then classify
classify.py         structural diff + PASS/KNOWN/FAIL/FIXED + exit code
known-divergences.json   accepted-divergence baseline (tracked debt, not policy)
scenarios/*.json    inputs, shared verbatim by both sides
pi/capture.mjs      pi side (dist or src backend)
pi/canon.mjs        canonicalizer
port/main.go          Go side; go.mod has replace -> the pi repo
port/canon.go         canonicalizer, twin of canon.mjs
out/{pi,go}/        generated captures (disposable)
pisrc/<sha>/        upstream TS extracted read-only (disposable)
```
Build note: `go vet ./...` and `go build -o /dev/null ./port` are the checks to
run here. Bare `go build ./...` fails with `build output "port" already exists
and is a directory` — generic Go behaviour for a `main` package in a
subdirectory, since the binary is named after its directory. `run.sh` uses
`go run`, so nothing in the normal path hits it. The repo's own gate never does
either: `difftest/` has its own `go.mod`, so the root module's `go build ./...`
does not descend into it.

