# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

> Note: Do not create new branch unless asked or confirmed

## What this repo is

A Go port of [picomatch](https://github.com/micromatch/picomatch) v4.0.5 (Port
Mortem 2026, Track F). **The matcher is not implemented** — every public entry
point that reaches it returns `ErrNotImplemented`, and behavioural parity is
3.13%. What exists is the *evidence machinery*: upstream's own Mocha suite
vendored byte-for-byte, a recorder that captures what picomatch does while that
suite runs, and several independent measurement harnesses.

`Scan` is the one exception. `internal/scan` is a complete port of `lib/scan.js`
— a second upstream entry point with its own state machine, sharing no code with
`parse()` — and all 586 recorded `lib/scan.scan` cases pass. It accounts for 586
of the 588 parity passes and touches the matcher nowhere, so none of it is credit
against the 15,060 `isMatch` cases. DECISIONS.md §13 records the two upstream
values it deliberately does not reproduce (`opts.tokens`, `basename("")`).

`internal/parse` holds the *scanner*, and under default options it is complete:
`make tokens` reads 100.00% — 1,491 of 1,491 — with `0 unbuilt` and `0 wrong`.
Every construct is built (text, slashes, dots, escapes, quotes, the negation
prologue, stars and globstars, extglobs, brackets, braces and `?`), so no input
is declined any more and `UnsupportedError` no longer names a construct.

What remains in `internal/parse` is the **option surface**, not another branch.
`internal/parse.Parse` now takes an `Options`, and it carries exactly one key:
`Windows`. That is deliberate and is the rule for the rest — a key earns a field
on the day its branch is written, never before, because an accepted-and-ignored
option returns a plausible token stream for the wrong configuration and scores a
pass wherever the two happen to agree. Roughly forty `opts.` sites are still
transcribed but marked; `grep -n "opts\." internal/parse/*.go` is the
authoritative list. Three need an answer before code: `literalBrackets` (tested
`=== false` at parse.js:856 and `=== true` at :865, so unset is a third state),
`maxExtglobRecursion` (a number or `false`), and `expandRange` (parse.js:23 — a
caller-supplied *function*, with no `Options` field yet; DECISIONS.md §15).

**That surface is sequenced, not a pile of cleanup.** `testdata/emit` ranks the
keys by the (pattern, options) pairs each unblocks, and it is very lopsided:
`windows` 570, `strictSlashes` 245, `bash` 235, `dot` 207, then a tail ending at
1 pair. Those four keys alone fully unblock **882 of the 1,018** non-default
pairs.

`windows` — 56% of them, and the *only* key on 324 — is **done**: it is a
constants-table swap (`constants.globChars(opts.windows)`) rather than a branch,
so it lives in `internal/parse/chars.go` and touches no branch in the scanner.
The 324 windows-only pairs went from blocked to **648 fields, 0 wrong**, taking
the scanner layer 50.11% → 66.04% and the headline 18.73% → 24.69%. `WINDOWS_CHARS`
is four leaves and twelve derivations, and the one leaf that looks derivable and
is not is docs/transcription-traps.md #54 — read it before touching that table.

`strictSlashes`, `bash` and `dot` are **done**, all three as branches rather
than swaps, built in that order — `bash` first because it is genuinely
independent (no corpus pair combines it with an unbuilt key, so its raw and
solo counts both read 235), then `strictSlashes` (two isolated sites), then
`dot` last because it reshapes `globstarBody` and `nodot`, bindings every
globstar arm reads. All three together take the scanner layer 66.04% → **93.48%**
and the headline 24.69% → **34.95%**, at **0 wrong**. `docs/emit-oracle.md` §4
holds the derivation and site inventory; re-derive from
`testdata/emit/summary.json` rather than trusting either file.

What's left of the option surface: `posix` (65 pairs), `regex` (52),
`noextglob` (20), then a tail ending at 1. None is a table swap the way
`windows` was; `posix` and `regex` also combine with `strictSlashes` on 51
pairs neither of these three keys realized.

The decline rule still governs everything added from here. Never fall back to
plausible output: a plausible-but-wrong token stream scores as a pass wherever the
guess coincides, which is indistinguishable from real progress. The token gate
counts *unbuilt* and *wrong* separately and fails outright on any *wrong*.

The organising principle everywhere: **fixtures are recorded, never authored.**
No file in `testdata/` states what picomatch ought to do; every expected value
came from running upstream. Keep it that way — the whole claim of the repo rests
on nobody having hand-written an answer.

## Commands

```bash
make build          # go build ./...
make test           # the port's own tests (must always be green)
make check          # fmt-check + vet (both tag sets) + test + verify-original
make help           # lists targets, from the `## ` comments in the Makefile
```

Without `make`: `go build ./...`, `go test ./...`,
`go vet ./... && go vet -tags conformance ./...`.

### Measurement targets

```bash
make conformance    # replay testdata/original + testdata/charaxis -> parity %
make tokens         # replay testdata/tokens against internal/parse -> parser %
make emit           # replay testdata/emit against the emitter -> emitter %
make mutate         # what the fixture sets CANNOT detect (needs Node)
make probes         # diagnostics: which parser ran, which rule decided it
```

`conformance`, `tokens` and `emit` report rather than gate by default. Turn any of
them into a gate with `PICOMATCH_PARITY_MIN`, `PICOMATCH_CHARAXIS_MIN`,
`PICOMATCH_TOKENS_MIN`, `PICOMATCH_EMIT_MIN` (percentages, e.g.
`PICOMATCH_PARITY_MIN=95 make conformance`). `tokens` and `emit` also fail
unconditionally on any *wrong* answer, floor or no floor.

### Running a single test

The conformance and token harnesses live behind the `conformance` build tag, so
they need the tag even for `-run`:

```bash
go test -run TestFixturesLoad ./...                       # untagged test
go test -tags conformance -run TestTokenParity -v ./...    # tagged test
go test ./internal/testcase/ -run TestDecodeTaggedValues   # one package
```

A gopls "No packages found" diagnostic on `conformance_test.go` / `tokens_test.go`
is expected — they are tag-gated, not broken.

### Regenerating fixtures (needs Node ≥ 18)

```bash
make deps           # npm install for tools/extract
make verify-original # prove tests/original matches MANIFEST.json
make extract        # re-record testdata/original from the upstream suite
make charaxis       # regenerate testdata/charaxis
make tokens-fixture # regenerate testdata/tokens
make emit-fixture   # regenerate testdata/emit
```

## Architecture

### The pipeline

```
tests/original/       upstream picomatch v4.0.5, byte-for-byte, SHA-256 pinned
      │               (require() intercepted; the specs themselves are untouched)
      ▼
tools/extract/        runs the suite twice per platform (posix + windows):
      │               once clean, once instrumented — aborts if any outcome differs
      ▼
testdata/original/    20,930 recorded behaviours, language-neutral JSONL
      ▼
conformance_test.go   replays them against the Go port -> parity %
```

`internal/testcase` decodes the tagged value encoding (`$undefined`, `$regexp`,
`$function`, …) documented in `tools/extract/README.md`. `internal/tokencase` is
the equivalent loader for the token fixtures, deliberately a *separate* type from
`internal/parse.Token` so drift shows up at the conversion.

### Four oracles, and what each localises

```
tokens differ                            -> parser bug      (make tokens)
tokens match, source differs             -> emitter bug     (make emit)
tokens + source match, behaviour differs -> matcher bug     (make conformance)
which parser ran / which rule decided it -> diagnostics     (make probes)
```

The token gate exists because parity replays behaviour end-to-end and therefore
reads 0% for the entire time the parser is being written. See DECISIONS.md §6 for
why using parser state as an *internal oracle* is not a reversal of the decision
not to *expose* it.

The emitter row used to point at `tools/probes`, which reports and gates nothing.
Half that oracle already existed: `tools/tokens/generate.js:90` records
`state.output` and `tokens_test.go:217` compares it, so the **full-scanner
emitter under default options** has been gated at 1,491/1,491 all along.
`testdata/emit` adds the three layers that were unmeasured — non-default options,
`parse.fastpaths()`, and `compileRe`'s `^(?:…)$` wrap and flags. It scores
**fields, not cases**, across 2,038 (pattern, options) pairs, and reads
`2686 of 10879 = 24.69%, 0 wrong` today: the scanner layer at 66.04% and the
other three at zero. See `docs/emit-oracle.md`.

### Three fixture sets, never merged

`testdata/original` is what upstream's own suite exercises — nobody chose its
contents, which is what makes the number worth quoting. `testdata/charaxis` is
chosen input covering holes `tools/mutate` proved the upstream suite is blind to
(the alphabet axis: UTF-16 units vs runes, JS `Canonicalize`, JS `.` exclusions,
`maxLength` units, both fast paths). They have separate directories, separate
tests, separate reports and separate floors. **Do not fold charaxis into the
headline parity figure** — that would mix a measurement with a target.

`testdata/emit` is the third: upstream's own patterns, but upstream's *internal*
output — the emitted source, the fast paths, the compiled `source` and `flags`,
per (pattern, options) pair. That makes it DECISIONS.md §6 material exactly as
`testdata/tokens` is, so it gets its own directory, its own test, its own
`emit-report.json` and its own floor, and **it is never folded into the parity
figure either**. What it does not record, and why, is DECISIONS.md §16.

### Upstream has three parsers, and they disagree

| Path | Site | Entered when |
|---|---|---|
| `parse.fastpaths()` | `lib/parse.js:1330` | `makeRe` sees `input[0]` is `.` or `*` |
| inline fast path | `lib/parse.js:606` | inside `parse()`, no `` /()[]{}" ``, not starting `*` or `!` |
| full scanner | `lib/parse.js:440`–`1322` | everything else |

Eligibility is not use: 382 patterns are eligible for the top path, **25 actually
take it** (`picomatch.js:316` falls through to the scanner whenever the result is
falsy). 67 corpus patterns compile to different source depending on the path, and
the delta is *not* a flag — 18 of the 25 differ structurally (`(?:X\/)?` vs
`(?:^|\/|X\/)`). Plan of record: build full-scanner semantics as the AST, then the
fast path as a separate normalisation pass gated by `Options.NoFastpaths`.

Leniency runs both ways. "The fast paths are more permissive" is wrong for the
inline path — on 28 patterns the *scanner* appends the trailing-slash allowance
the fast path omits.

### Why there is no `MakeRe`

Go's `regexp` is RE2 and picomatch's output relies on lookaround in almost every
non-trivial pattern (six of seven representative patterns fail `regexp.Compile`).
Matching goes through `Pattern` alone; the target implementation is a memoised
backtracking AST walker. DECISIONS.md §1.

## Invariants that break things quietly if ignored

- **`tests/original/` is read-only.** It is hash-pinned by `MANIFEST.json` and CI
  re-hashes it on every push. `tools/mutate` copies it to a temp dir before
  editing. Never edit it in place, and never "fix" a fixture to make a test pass —
  that is the exact failure mode this repo exists to rule out.
- **`testdata/charaxis/` must regenerate byte-identically.** CI runs the generator
  and `git diff --exit-code`. Same for `testdata/tokens/` and `testdata/emit/`,
  which matters more than it looks: all three read the pattern corpus through
  `tools/probes/lib/corpus.js`, so one edit there has to leave three committed
  fixture sets byte-identical.
- **`go test ./...` must stay green** even at 0% parity. Parity lives behind the
  build tag so the everyday signal is never diluted.
- **`go vet` runs under both tag sets** — a compile error inside `//go:build
  conformance` will not surface otherwise.
- **`ErrNotImplemented` is scored as a failure**, never as a match for a recorded
  throw. It is a placeholder, not a behavioural answer.
- **`parse.Parse` returns a partial state alongside an `UnsupportedError`.** The
  token gate compares it against the recording's leading tokens, which is what
  makes `0 wrong` a statement about built branches rather than about the 176
  patterns that parse end to end. The last token is exempt because upstream
  rewrites `prev` after pushing it — DECISIONS.md §9. Returning `nil` there
  silently guts the gate.
- **Upstream's `parse()` does not terminate on some inputs** (`a` plus four or
  more backslashes). The scanner detects it and errors; `eos()` is `>=`, not
  upstream's `==`, so the loop cannot run away. DECISIONS.md §11. No fixture can
  cover this — the recorder hangs on the same input — so the untagged
  `internal/parse` tests are the only thing holding it.
- **Stdlib only.** No third-party imports, no `unsafe`, no cgo. `any` is confined
  to the harness and `internal/testcase`, where it decodes arbitrary JSON.
- **Options field names encode evidence, not taste.** `NoFastpaths` (upstream
  defaults it on, so a `Fastpaths bool` would invert the Go zero value);
  `LiteralBrackets *bool` and `MaxExtglobRecursion *int` because unset is a third
  state. A field is added only if upstream actually reads the key — see
  DECISIONS.md §2 before adding one.
- **`Options.Windows` is never inferred from the host.** 17% of paired fixtures
  genuinely diverge between platforms; both are recorded and both are contract.
  The other side of the same fact: `testdata/emit` has **no** platform axis,
  because `utils.isWindows` is called nowhere in `lib/` and `opts.windows` is the
  only platform input the *emitter* has. The 17% diverge in the matcher.
  DECISIONS.md §16.
- **picomatch counts UTF-16 code units**, not runes or bytes. `for i, r := range s`
  and `len(s)` are both wrong for `?`, `maxLength`, and character classes. This is
  the single most likely way an idiomatic Go implementation silently diverges, and
  no upstream fixture catches it. `internal/parse` holds its input as
  `units` (`[]uint16`) and converts only at the package boundary — DECISIONS.md §8.
  Keep new branches on `units`; a token value built as a Go string cannot hold
  half a surrogate pair, so the distinction is lost before anything compares it.
- **Argument order in fixtures:** `picomatch.isMatch(str, pattern)` — input first,
  pattern is `args[1]`. Getting it backwards does not throw; it produces a corpus
  of inputs-treated-as-patterns. `tools/probes/lib/corpus.js` is the single
  definition — use it rather than re-deriving.
- **Every mutation in `tools/mutate/mutations.js` needs witnesses**, and must edit
  the site its name claims. A no-op mutation looks exactly like a coverage hole; a
  mislabelled one measures the wrong site (this happened — see DECISIONS.md §7).
  Each `charaxis` axis's `kills` field must name a mutation that exists.
- **Makefile targets are documented with `## name: description`** on one or two
  lines; `make help` greps for them.

## Where to look before deciding something

- `docs/build-order.md` — which branch to write next, staged, with the upstream
  sites and the gate figure each stage must reach. Derived from
  `make build-order`; re-run that rather than trusting the file, which goes stale
  as branches land. The scanner's order is finished; what comes next is ranked in
  the file below, not there.
- `docs/emit-oracle.md` — the same thing for the emitter: the field census, the
  ceilings (no single axis clears half), which option key to thread next ranked
  by pairs unblocked, and the regex constructs the emitter has to produce.
  Authority is `testdata/emit/summary.json`; re-derive rather than trust.
- `DECISIONS.md` — every deliberate divergence from upstream, with a re-check
  command for each. Add an entry rather than a code comment when the port will not
  match upstream.
- `tools/probes/README.md` — measured facts about upstream's structure, including
  several corrections where an earlier claim was wrong. Re-run the probes rather
  than trusting the prose.
- `tools/mutate/README.md` — what the fixtures cannot see, and why each hole is a
  case where the idiomatic Go code is the wrong code.
- `docs/transcription-traps.md` — places where the obvious Go reading of
  `lib/parse.js` is wrong: `!` falls through rather than continuing, the text
  merge is JavaScript-truthy, `}` emits a literal where `]` escapes, `consumed` is
  not a slice of the input. **Read it before adding a scanner branch**, and add an
  entry when a branch turns up another. Each entry needs the upstream site, the
  reading that would have been wrong, and what it costs. #50 and #51 are the two
  places `parse.fastpaths` and `parse()` read the *same* option differently —
  read those before writing the fastpaths pass.

Prose learnings go in `docs/`, not in a comment block at the top of a file. The
code keeps one-line markers at the sites they describe; the reasoning lives in
`docs/` where it can be read before the file is opened.

`fuzz/` and `bench/` are scaffolding only; both land after the matcher does.
