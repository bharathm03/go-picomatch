# tools/probes — measuring what upstream actually does

```bash
make probes          # run the three corpus-wide probes, report only
make probes-data     # write the artifacts to testdata/probes/ (gitignored)
```

These are **diagnostics, not fixtures**. Nothing in the Go suite replays them.
They exist to answer questions the corpus cannot: *which rule decided this?*,
*do upstream's three parsers agree?*, *what does the Go `Token` struct have to
hold?*, *what pins this boundary?*

`testdata/original` tells you **what** fails. These tell you **why**, and — the
demanding part — what else moves if you fix it.

Every number below was measured against the vendored upstream at
`tests/original` (picomatch 4.0.5). Re-run the probes rather than trusting the
prose; they are cheap.

## The organising idea: three oracles, not one

picomatch exposes three independent views of the same pattern, and *which one
fails first classifies the bug* before you read any code:

```
tokens differ                           -> parser bug
tokens match, regex differs             -> emitter / semantics bug
tokens + regex match, behaviour differs -> matcher bug
```

**Do not skip past the matcher layer.** The token oracle only sees parsing, and
the highest-risk area — globstar boundary semantics, `a/**` matching `a` — is a
*matching* bug with a perfectly correct parse.

## The probes

### `fastpath-diff.js` — upstream has three parsers, and they disagree

| Path | Site | Entered when |
|---|---|---|
| `parse.fastpaths()` | `lib/parse.js:1330` | `makeRe` sees `input[0]` is `.` or `*` (`lib/picomatch.js:312`) |
| inline fastpath | `lib/parse.js:606` | inside `parse()`, no `` /()[]{}" `` and does not start `*` or `!` |
| full scanner | `lib/parse.js:440`–`1322` | everything else |

```
distinct string patterns          1,493
  parse.fastpaths() eligible        382
  inline fastpath eligible          153
  compared (either, parseable)      509

regex SOURCE differs fast vs slow   172 patterns   3,104 records
BEHAVIOUR differs (len <= 5)         37 patterns     480 records
  of those, expected=true                            238
records that set `fastpaths`                           0
```

The fast paths are **not** a pure optimisation:

```
".*"      matches ".."      fast=true   slow=false
"a*"      matches "aa/"     fast=false  slow=true
"**/*.md" matches "m.md/"   fast=true   slow=false
```

**Why this shapes the port.** No record sets `fastpaths`, so all of them assert
*default* behaviour — which for those 480 is *fastpath* behaviour. A port with
one parser implementing full-scanner semantics fails them, and the failures
present as unrelated dot-handling and trailing-slash bugs scattered across the
suite. Implement full-scanner semantics as the AST and model the fast path as
what it architecturally is: a separate normalisation pass behind a `Fastpaths`
option, default on.

**The count is a lower bound, and the bound bites.** At `MAXLEN=4` this probe
reported 28 patterns on the research corpus; at 5 it finds 37. The binding
constraint here is **length**, not alphabet — widening the alphabet at 4 changes
nothing. That is the mirror image of `fingerprint.js` below. Do not assume which
axis binds; measure it per probe.

### `token-inventory.js` — the scanner rewrites what it already emitted

This is why a conventional lexer → parser split cannot reproduce picomatch.
`parse()` maintains **two** representations at once — an incrementally-appended
regex string and the token array — and when a decision invalidates output
already appended it sets `state.backtrack` and **discards the string, rebuilding
it from the tokens** (`parse.js:1309`).

That is the tell: **when the two disagree, picomatch trusts the tokens.** The
string is a cache. The AST-first design is not merely forced by RE2 — it is what
the reference falls back to under pressure.

```
1,491 patterns parsed (2 threw), 10,558 tokens, 15 types

text 2642  slash 1646  bos 1491  star 1390  paren 1189  maybe_slash 334
globstar 316  negate 267  qmark 263  bracket 248  brace 233  dot 190
comma 182  plus 97  at 70

fields: type/value/output (string) · extglob/star (bool) · outputIndex/tokensIndex (int)

state.backtrack set    77 patterns   <- output discarded and rebuilt
  risky-extglob subset 12 patterns

patterns containing "**"   354
  globstar token emitted   272
  no globstar token         82        <- causes are mixed, not distinguished
```

Two further rewrites do **not** set the flag because they truncate the output by
hand: `push()` demotes an already-emitted globstar to a star and slices the
output back (`parse.js:493-505`), and consecutive `text` tokens are merged
(`:512-516`) — so tokens are not 1:1 with source characters.

Also note:

```
parse("a/**/*.js").consumed === "a/**//*.js"
```

The scanner **invented a slash**. That needs a deliberate home in the AST rather
than being reproduced by accident.

### `fingerprint.js` — bounded exhaustive enumeration

Rather than sampling a pattern with whatever inputs a fixture happened to
assert, enumerate every string over a per-pattern alphabet up to length 5 and
record accept/reject. Both sides of every boundary, by construction.

```
1,490 patterns in ~1.6s   (14.0M calls, ~8.7M/sec)
two-sided within bound     901  (60.5%)
match nothing in bound     572
one-sided in corpus        672
  made two-sided by enum   413  (61.5% of the gap closed)
```

The globstar rule is simply visible in the output:

```
$ node tools/probes/fingerprint.js "a/**"
accepts  a  a/  a/a  a/aa  ...        <- zero segments, plainly
```

**The alphabet must be derived per pattern, and not too narrowly.** A global
alphabet leaves most patterns matching nothing. So does taking only two
alphanumeric literals: `a/*/*.md` drops `d`, and `a+b/src/*.js` and `*.*-*` lose
`+` and `-` entirely, because `[A-Za-z0-9]` cannot see them. Those patterns then
report a clean, confident "matches nothing". Widening to four literals over
`[A-Za-z0-9_+-]` bought 13.6 points of two-sided coverage for 2× the compute;
raising `MAXLEN` 5→6 buys 3.5 for 10×.

Why it matters: a large share of patterns only ever prove **one** side. An
implementation that is systematically too permissive passes all of them.

### `coverage-diff.js` — which rule decided it?

Records which lines of `tests/original/lib/parse.js` execute for a pattern, then
diffs against a near neighbour. The residue is the rule that differs. Use it
when a conformance case fails: diff the failing pattern against the nearest
**passing** one.

```
"a/**"        \ "a/*"    -> 38 lines: 396, then 1146-1220
"a/**/c"      \ "a/**"   -> 23 lines: 495-504, 1200-1218
"[[:alpha:]]" \ "[a-z]"  -> 29 lines: 720-740, 1310-1318
"{a,b}"       \ "a/b"    -> 44 lines: 476-482, 882-968
```

`parse.js:396` is the globstar emitter — the negative-lookahead site that forced
this port to a hand-written matcher. The diff finds it with no human input.

**Two traps, both hit while building this.**

1. **V8 block coverage must be painted outer-to-inner.** V8 reports an outer
   range with `count > 0` plus *nested* `count: 0` ranges for blocks not taken.
   Collect only the non-zero ranges and you get "the whole file" for every
   pattern — 967 lines regardless of input, which looks plausible and is
   useless.
2. **Coverage accumulates across measurements in one process.** The same pattern
   measured 316 lines when it ran first and 847 when it ran later.
   `stopPreciseCoverage` does not reset it. Each pattern is therefore measured
   in a **fresh process** (~130 ms). An order-dependent diagnostic is worse than
   none.

An earlier version reported `"a/**" \ "a/*"` as a single line and concluded that
coverage cannot discriminate data differences. Both conclusions were artifacts
of trap 2. **Treat any coverage result produced without process isolation as
void.**

## Limits worth knowing before you trust a number

- **The token oracle is void for the 382 fastpath-eligible patterns.**
  `picomatch.parse` is `parse(pattern, { fastpaths: false })`
  (`lib/picomatch.js:212`), so it always runs the full scanner — but `makeRe`
  does not. For those patterns the token stream describes a code path that did
  not run. `coverage-diff.js` calls `pm.parse` and inherits the same caveat;
  diffing two patterns is still valid since both sides take the same path.
  Any `tokens.jsonl` must carry a fastpath flag per pattern or it asserts the
  wrong oracle for a quarter of the corpus.
- **Enumeration is bounded.** Length 5 over at most 7 characters, and only over
  patterns the corpus contains. Everything it reports is a lower bound.
- **The rewrite-site inventory is read, not exhaustively instrumented.** Only
  `state.backtrack` (77) and the risky-extglob subset (12) are measured; the
  `push()` truncations were read from the source.
- **The 82 `**`-without-globstar patterns conflate two mechanisms** — segment
  conditions never met, or `push()` demoted it. Separating them needs per-line
  coverage.
- **Throw counts differ between probes** (3 in `fingerprint`, 2 in
  `token-inventory`) because they call different entry points: `picomatch()`
  compiles a regex, `parse()` does not.

## Where the pattern lives in a record

`testdata/original/cases.jsonl` records the actual call upstream made, so the
pattern's position depends on the API. `lib/corpus.js` is the single definition;
the trap it exists to prevent:

```js
picomatch.isMatch(str, pattern)   // input FIRST — the pattern is args[1]
```

Getting that backwards does not throw. It yields a corpus of
inputs-treated-as-patterns that every probe then reports on confidently.
