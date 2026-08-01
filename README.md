# go-picomatch

A Go port of [picomatch](https://github.com/micromatch/picomatch) v4.0.5 — the
glob matcher behind micromatch, chokidar, and a large slice of the Node
ecosystem.

**Port Mortem 2026 · Track F (JavaScript → Go)**

> **Status: scaffolding.** The matcher is not implemented yet — every entry point
> returns `ErrNotImplemented` and behavioural parity is **0.01%** (2 of 19,308
> replayable cases, both `assert.throws` fixtures the empty-pattern guard already
> satisfies). `ErrNotImplemented` is scored as a *failure*, never as a match for a
> recorded throw, so that figure can only move for real reasons. What *is* built is
> the machinery that makes the parity claim checkable: the upstream test suite,
> vendored byte-for-byte and hash-pinned, and a pipeline that records what
> picomatch actually does while that suite runs.

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

The `Options` field set is derived from the fixtures rather than transcribed from
upstream docs: every field is an option the upstream suite actually exercises. All
30 observed keys, their frequencies and their value types are in
`testdata/original/summary.json` under `optionSurface`, so the struct can be
re-checked against evidence.

## Layout

| Path                    | What it is                                                     |
| ----------------------- | -------------------------------------------------------------- |
| `picomatch.go`          | Public API (declared, not yet implemented)                       |
| `options.go`            | `Options`, derived from the observed option surface              |
| `internal/testcase/`    | Fixture loader and decoder for the recorded value encoding       |
| `conformance_test.go`   | Parity harness, behind the `conformance` build tag               |
| `fixtures_test.go`      | Guards the extraction pipeline's output                          |
| `tests/original/`       | Upstream picomatch v4.0.5, unmodified and hash-pinned            |
| `testdata/original/`    | Recorded behaviours + extraction summary                         |
| `tools/extract/`        | The recorder (Node; build-time only)                             |
| `DECISIONS.md`          | Every non-trivial divergence from the original, with rationale   |

## No source-language runtime

The Go package imports **only the standard library**. It contains no JavaScript,
links against no JS engine, and shells out to nothing. The vendored upstream tree
is used at build time for two things — recording fixtures and, later, differential
fuzzing — and never at runtime.

## Licence

MIT. This port is © 2026 Bharath Mohan; upstream picomatch is © 2017-present Jon
Schlinkert. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
