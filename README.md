# go-picomatch

A Go port of [picomatch](https://github.com/micromatch/picomatch) v4.0.5 — the
glob matcher behind micromatch, chokidar, and a large slice of the Node
ecosystem.

**Port Mortem 2026 · Track F (JavaScript → Go)**

> **Status: scaffolding.** The matcher is not implemented yet — every entry point
> that reaches it returns `ErrNotImplemented`, and behavioural parity is **3.13%**
> (588 passed of 18,790 comparable, from 19,308 replayable cases less 518 the port
> declares out of scope). `ErrNotImplemented` is scored as a *failure*, never as a
> match for a recorded throw, so that figure can only move for real reasons. What
> *is* built is the machinery that makes the parity claim checkable: the upstream
> test suite, vendored byte-for-byte and hash-pinned, and a pipeline that records
> what picomatch actually does while that suite runs.
>
> Of those 588, **586** are `lib/scan.js` — a second upstream entry point with its
> own state machine, ported in full, at 100% of its recorded cases. The other 2 are
> the `assert.throws` fixtures the empty-pattern guard already satisfies. Scan does
> not touch the matcher, so none of it is credit against the 15,060 `isMatch` cases.
>
> The scanner is underway behind that machinery and has its own number —
> `make tokens` reads **29.58%** with `0 wrong`, and matcher parity cannot move
> until it, the emitter and the matcher are all finished.

---

## Why picomatch, and why Go

picomatch is a pure-computation library: string in, boolean out, no I/O, no
concurrency, no platform API beyond an OS check. That makes it an unusually
honest subject for a port-equivalence experiment — there is nowhere for a
divergence to hide behind a mock or a timing difference. It is also a genuine
dependency-graph bottleneck: a Node-free implementation lets Go tooling do glob
matching with Node's exact semantics instead of `path/filepath.Match`'s much
smaller grammar.

It fits the track's constraints: 2,018 source lines across `lib/` and the two
entry points, MIT licensed, with a 1,977-test Mocha suite and no existing Go port.

## The problem this repo takes seriously

Anyone can generate a port that compiles. The hard part is proving it behaves
like the original — and the standard failure mode, as the Bun Zig→Rust merge
showed, is that the tests get edited on the way out.

picomatch's tests are JavaScript. They cannot run against Go directly, and the
usual workaround is to hand-translate assertions — which is exactly where a port
quietly starts grading its own homework.

So this repo does not translate the tests. It **runs them, unmodified, and
records what picomatch does.**

```
tests/original/          upstream picomatch v4.0.5, byte-for-byte, SHA-256 pinned
      │
      │  node's require() is intercepted; the specs are untouched
      ▼
tools/extract/           runs the suite twice (POSIX + Windows semantics),
      │                  recording every call the tests make into picomatch
      ▼
testdata/original/       20,930 recorded behaviours, language-neutral JSONL
      │
      ▼
conformance_test.go      replays them against the Go port -> parity %
```

Three properties make the resulting number trustworthy:

**The suite is provably unmodified.** Every vendored file is hashed in
`tests/original/MANIFEST.json`. `make verify-original` re-hashes the tree and
fails on any drift; CI runs it on every push. Line endings are hashed too, so a
stray CRLF conversion is caught.

**The recorder is provably transparent.** Extraction runs the suite twice per
platform — once clean, once instrumented — and aborts if a single test outcome
differs. This caught a real bug during development: the first recorder wrapped
picomatch's returned matcher without copying its `.state` property, breaking two
upstream tests. Without this check, the fixtures would have encoded behaviour
picomatch does not have, and the port would later have been "proved" against it.
Current status: **1977/1977 upstream tests pass in both modes, on both platforms.**

**The parity denominator is honest.** Of 20,930 recorded cases, 19,308 are
replayable. The rest are excluded and reported, not quietly dropped: 1,622 pass a
JavaScript callback (`onMatch`, `format`, `expandRange`) that has no fixture
representation. Cases observed inside a failing upstream test would also be
excluded — there are currently none.

### Both platforms, on purpose

picomatch's entry point picks slash semantics from the host OS, so `a/*` has two
legitimate answers. Extracting on one machine would silently bake in whichever
one that machine used. The pipeline pins the platform explicitly and records both:
**1,785 of 10,465 cases (17%) genuinely diverge** — mostly `[^/]` versus `[^\\/]`
in the compiled expression. That divergence is a first-class part of the contract
the port has to satisfy, not an accident of the build machine.

## Quick start

```bash
make build     # compile
make test      # the port's own tests
make check     # format, vet, test, and verify the upstream suite is unmodified
```

Without `make` (e.g. a stock Windows shell), the same three:

```bash
go build ./...
go test ./...
go vet ./... && go test ./... && node tools/extract/verify.js
```

Or, with nothing installed but Docker:

```bash
docker build -t go-picomatch .
```

The image build fails if the code is not gofmt-clean, does not vet, does not
build, or does not pass its tests.

### The parity report

```bash
make conformance
```

Replays every replayable fixture and prints the pass rate per API, writing
`conformance-report.json`. It reports rather than gates, so the number can be
watched as it climbs. To make it a gate:

```bash
PICOMATCH_PARITY_MIN=95 make conformance
```

### A gate that moves before parity can

Parity replays behaviour — pattern in, boolean out — so it cannot move until the
scanner, the emitter *and* the matcher all work. For the whole time the parser is
being written it reads 0% and says nothing about progress.

So the parser has its own number. `testdata/tokens/` records the token stream
upstream's parser produces for each of 1,491 patterns, and `make tokens` replays
them against `internal/parse`. It is available from the first token the scanner
emits, and it localises a failure to the layer that caused it:

```
tokens differ                     -> parser bug
tokens match, behaviour differs   -> emitter or matcher bug
```

```bash
make tokens           # report only
make tokens-fixture   # re-record from upstream (needs Node)
```

It now reads **100.00%** — 1,491 of 1,491 patterns, with `0 unbuilt` and
`0 wrong`. Under default options the scanner is complete: literal text, slashes,
dots, escapes, quotes, the leading-negation prologue, stars, globstars, extglobs,
brackets, braces and `?` are all built, and no input is declined.

The percentage is the less useful of the two numbers it prints. Every failure is
classified as either **unbuilt** — the scanner reached a construct it does not
implement and refused — or **wrong**, meaning a branch that already exists
disagreed with the recording. While branches were landing the report also named
what each failure was blocked on, which gave the build order without anyone
having to choose one:

```
of 1315 failures: 1315 unbuilt, 0 wrong
blocked on * (parse.js:1128)          696
blocked on !( extglob (parse.js:1056) 145
blocked on [ (parse.js:814)           140
```

Both columns are now zero and that table is empty. Unbuilt was expected and shrank
on its own as branches landed. Wrong was never supposed to be nonzero at any
point, so it fails the run rather than lowering a score — unlike the percentage
floor, that check is not opt-in, and CI runs the gate as its own step. It stayed
at `0` through every stage.

While anything was still declined, `0 wrong` was not a claim about the patterns
that parsed end to end. A pattern the scanner declined still returned the tokens
it had produced first, and those were compared against the recording's leading
tokens — so a bug in a branch that existed was reported as wrong even when the
same pattern tripped on something unbuilt further along. One token was exempt:
upstream rewrites tokens after pushing them, always the one immediately before, so
the token adjacent to the construct that stopped the scanner was not yet final.
DECISIONS.md §9 has the measurement, and the rule still governs any branch added
behind an option.

An unbuilt construct returns an error rather than falling back to treating the
input as literal text. The fallback would produce a token stream that is wrong
but plausible, and wherever the guess happened to coincide it would score as a
pass that looks exactly like a branch someone wrote.

Two inputs get an error for a different reason. A pattern over `maxLength` is
refused the way upstream refuses it, and a pattern on which upstream's own
`parse()` never returns — `a` followed by four or more backslashes hangs node —
is reported rather than reproduced or quietly answered. DECISIONS.md §11.

Each record carries which of picomatch's three parsers `makeRe` would really have
used, **measured rather than transcribed** — the inline fast path's condition
(`parse.js:606`) tests the input *after* `removePrefix` rewrote it, so `./foo`
reads as ineligible while the scanner actually fast-paths it. Of 1,491 patterns,
1,316 take the full scanner, 25 take `parse.fastpaths()` and 150 the inline path;
67 compile to different source. For those 67, matching the tokens does not
settle what `makeRe` produced, so the gate reports that subset separately instead
of dropping it.

This is reported separately from parity and never folded into it — the pattern
list is inherited from upstream's suite rather than chosen, and the records are
internal state that [DECISIONS.md](DECISIONS.md) §6 excludes from the headline
figure on purpose.

### What the fixtures cannot see

A parity percentage says how much recorded behaviour a port reproduces. It never
says what the recording left out. `make mutate` answers that: it applies a
mutation to upstream, replays every fixture against the mutant, and counts how
many detect it. A mutation nothing detects is behaviour a port could get wrong
while every number here still read clean.

Six plausible Go-port choices survive all 18,792 comparable fixtures — walking by
rune instead of UTF-16 code unit, `nocase` via Unicode folding, a globstar body
that crosses newlines, `maxLength` counted in code points, and skipping either of
picomatch's two fast paths. Two controls are detected by 29 and 34 fixtures, so
the instrument is not dead.

The cause is not weak testing upstream. The suite is exhaustive *structurally*
and thin *alphabetically*: **91 distinct code points** across every pattern and
input it uses, four of them non-ASCII. A structural corpus has no reason to
contain an astral character or a newline.

So there is a second fixture set, `testdata/charaxis`, covering exactly those
holes — generated the same honest way, by recording what picomatch does, and
reported separately so the headline parity number stays derived purely from
upstream's own tests. `make mutate` verifies it kills all six on every run.

The sixth hole was found by *splitting* a mutation rather than adding one. A
single entry named `no-fastpaths` described picomatch's inline fast path while
its edit disabled the top-level one, so the inline site had never been measured
and the top path's kills made the pair read as covered. Split apart, it came back
0/0.

```bash
make mutate      # measure both fixture sets
make charaxis    # regenerate the character-domain fixtures
```

See [tools/mutate/README.md](tools/mutate/README.md) and
[DECISIONS.md](DECISIONS.md) §7.

### Re-running extraction

Only needed if the fixtures are regenerated; requires Node ≥ 18.

```bash
make deps       # install mocha + fill-range for the extractor
make extract    # re-record from the unmodified upstream suite
```

`testdata/original/` is generated but committed on purpose: it is the evidence
behind the parity claim, and committing it means `go test` needs no Node.

## API

```go
import picomatch "github.com/bharathm03/go-picomatch"

p, err := picomatch.New("**/*.js", &picomatch.Options{Dot: true})
if err != nil { ... }
p.Match("src/index.js")   // bool
p.MatchDetail("src/index.js")

picomatch.IsMatch("a.js", []string{"*.js", "*.md"}, nil)
picomatch.Scan("foo/bar/*.js", nil)
```

There is no `MakeRe`. Go's `regexp` is RE2, and picomatch's output relies on
lookaround in almost every non-trivial pattern — six of seven representative
patterns fail `regexp.Compile` outright. Returning a `*regexp.Regexp` would be a
promise the matcher cannot keep, so matching goes through `Pattern` alone. See
[DECISIONS.md](DECISIONS.md).

The `Options` field set is reconciled against two independent sources rather than
transcribed from upstream docs: the 30 keys the suite actually passes (recorded in
`testdata/original/summary.json` under `optionSurface`) and the option keys
upstream actually reads. The two differ in both directions, and the difference is
the point — a key the suite passes but upstream ignores gets no field
(`relaxSlashes` is inert), and a key upstream reads but the suite never passes
gets a field marked as unexercised (`contains`, `fastpaths`, `literalBrackets`,
`prepend`). Both directions are re-checkable from the repo; the reasoning is in
[DECISIONS.md](DECISIONS.md).

## Layout

| Path                    | What it is                                                     |
| ----------------------- | -------------------------------------------------------------- |
| `picomatch.go`          | Public API (declared, not yet implemented)                       |
| `options.go`            | `Options`, derived from the observed option surface              |
| `internal/parse/`       | The scanner: pattern in, token stream out (partially built)      |
| `internal/testcase/`    | Fixture loader and decoder for the recorded value encoding       |
| `internal/tokencase/`   | Fixture loader for the recorded token streams                    |
| `conformance_test.go`   | Parity harness, behind the `conformance` build tag               |
| `tokens_test.go`        | Token gate — the parser's own pass/fail number                   |
| `fixtures_test.go`      | Guards the extraction pipeline's output                          |
| `tests/original/`       | Upstream picomatch v4.0.5, unmodified and hash-pinned            |
| `testdata/original/`    | Recorded behaviours + extraction summary                         |
| `testdata/charaxis/`    | Supplementary character-domain fixtures                          |
| `testdata/tokens/`      | Golden token streams recorded from upstream's parser             |
| `tools/extract/`        | The recorder (Node; build-time only)                             |
| `tools/mutate/`         | Measures what each fixture set can detect                        |
| `tools/charaxis/`       | Generates the character-domain fixtures                          |
| `tools/tokens/`         | Generates the golden token streams                               |
| `tools/probes/`         | Diagnostics: which parser ran, which rule decided it             |
| `docs/`                 | Porting notes — read before writing the layer they cover         |
| `DECISIONS.md`          | Every non-trivial divergence from the original, with rationale   |

## No source-language runtime

The Go package imports **only the standard library**. It contains no JavaScript,
links against no JS engine, and shells out to nothing. The vendored upstream tree
is used at build time for two things — recording fixtures and, later, differential
fuzzing — and never at runtime.

## Licence

MIT. This port is © 2026 Bharath Mohan; upstream picomatch is © 2017-present Jon
Schlinkert. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
