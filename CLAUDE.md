# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this repo is

A Go port of [picomatch](https://github.com/micromatch/picomatch) v4.0.5 (Port
Mortem 2026, Track F). **The matcher is not implemented** — every public entry
point returns `ErrNotImplemented` and behavioural parity is 0.01%. What exists is
the *evidence machinery*: upstream's own Mocha suite vendored byte-for-byte, a
recorder that captures what picomatch does while that suite runs, and several
independent measurement harnesses.

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
make mutate         # what the fixture sets CANNOT detect (needs Node)
make probes         # diagnostics: which parser ran, which rule decided it
```

`conformance` and `tokens` report rather than gate by default. Turn either into a
gate with `PICOMATCH_PARITY_MIN`, `PICOMATCH_CHARAXIS_MIN`, `PICOMATCH_TOKENS_MIN`
(percentages, e.g. `PICOMATCH_PARITY_MIN=95 make conformance`).

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

### Three oracles, and what each localises

```
tokens differ                           -> parser bug      (make tokens)
tokens match, regex differs             -> emitter bug     (tools/probes)
tokens + regex match, behaviour differs -> matcher bug     (make conformance)
```

The token gate exists because parity replays behaviour end-to-end and therefore
reads 0% for the entire time the parser is being written. See DECISIONS.md §6 for
why using parser state as an *internal oracle* is not a reversal of the decision
not to *expose* it.

### Two fixture sets, never merged

`testdata/original` is what upstream's own suite exercises — nobody chose its
contents, which is what makes the number worth quoting. `testdata/charaxis` is
chosen input covering holes `tools/mutate` proved the upstream suite is blind to
(the alphabet axis: UTF-16 units vs runes, JS `Canonicalize`, JS `.` exclusions,
`maxLength` units, both fast paths). They have separate directories, separate
tests, separate reports and separate floors. **Do not fold charaxis into the
headline parity figure** — that would mix a measurement with a target.

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
  and `git diff --exit-code`. Same for `testdata/tokens/`.
- **`go test ./...` must stay green** even at 0% parity. Parity lives behind the
  build tag so the everyday signal is never diluted.
- **`go vet` runs under both tag sets** — a compile error inside `//go:build
  conformance` will not surface otherwise.
- **`ErrNotImplemented` is scored as a failure**, never as a match for a recorded
  throw. It is a placeholder, not a behavioural answer.
- **Stdlib only.** No third-party imports, no `unsafe`, no cgo. `any` is confined
  to the harness and `internal/testcase`, where it decodes arbitrary JSON.
- **Options field names encode evidence, not taste.** `NoFastpaths` (upstream
  defaults it on, so a `Fastpaths bool` would invert the Go zero value);
  `LiteralBrackets *bool` and `MaxExtglobRecursion *int` because unset is a third
  state. A field is added only if upstream actually reads the key — see
  DECISIONS.md §2 before adding one.
- **`Options.Windows` is never inferred from the host.** 17% of paired fixtures
  genuinely diverge between platforms; both are recorded and both are contract.
- **picomatch counts UTF-16 code units**, not runes or bytes. `for i, r := range s`
  and `len(s)` are both wrong for `?`, `maxLength`, and character classes. This is
  the single most likely way an idiomatic Go implementation silently diverges, and
  no upstream fixture catches it.
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

- `DECISIONS.md` — every deliberate divergence from upstream, with a re-check
  command for each. Add an entry rather than a code comment when the port will not
  match upstream.
- `tools/probes/README.md` — measured facts about upstream's structure, including
  several corrections where an earlier claim was wrong. Re-run the probes rather
  than trusting the prose.
- `tools/mutate/README.md` — what the fixtures cannot see, and why each hole is a
  case where the idiomatic Go code is the wrong code.

`fuzz/` and `bench/` are scaffolding only; both land after the matcher does.
