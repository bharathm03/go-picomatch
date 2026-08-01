# Decisions

Every non-trivial divergence from upstream picomatch v4.0.5, with the evidence
behind it. A decision belongs here if a reader comparing the two implementations
side by side would otherwise think the port had simply got it wrong.

Each entry states what upstream does, what this port does, and how to re-check
the claim rather than take it on trust.

---

## 1. No compiled regular expression is exposed

**Upstream.** `picomatch.makeRe(pattern, options)` compiles a pattern to a
JavaScript `RegExp` and returns it. The matcher itself is a closure over that
`RegExp`, and `.source` is inspectable.

**This port.** There is no `MakeRe`, no `Pattern.Regexp()`, and no
`*regexp.Regexp` anywhere in the public API. Matching goes through `Pattern` and
nothing else.

**Why.** Go's `regexp` is RE2, which has no lookaround by construction — it
guarantees linear time, and lookaround is what buys backtracking engines their
exponential worst case. picomatch's output depends on lookaround in almost every
non-trivial pattern:

| Construct | Emitted by picomatch | RE2 |
| --- | --- | --- |
| leading `*` dot guard | `(?!\.)(?=.)` | rejected |
| globstar body | `(?:(?!(?:^\|\/)\.).)*?` | rejected |
| negated extglob `!(a)` | `(?:(?!(?:a)$))` | rejected |

Six of seven representative patterns fail `regexp.Compile` outright:

```
[a-z]*     ok
+(a|b)     FAIL  invalid or unsupported Perl syntax: `(?=`
*          FAIL  invalid or unsupported Perl syntax: `(?!`
a/*        FAIL  ...
!(a)       FAIL  ...
a/**/b     FAIL  ...
**/*.js    FAIL  ...
```

`[a-z]*` compiles only because its segment begins with `[` rather than `*`, so no
dot guard is emitted — an artefact of picomatch's guard being decided by the
pattern's *lexical* left neighbour, not by match position.

Returning a `*regexp.Regexp` would therefore be a type the matcher can never
populate. A port is free to reimplement; it is not free to advertise a value it
cannot produce. The matcher this port is being built towards is a memoised
backtracking AST walker, which reproduces the semantics without pretending to be
an RE2 program.

**Not a scoring dodge.** The conformance harness already scores `makeRe` (72
cases) and `parse` (32 cases) as *unsupported*, not as passes — they leave both
the numerator and the denominator. Removing the API changes no parity number.

**Reversible, at a price.** The recorded `makeRe` fixtures hold upstream's exact
ECMAScript source strings, so a `MakeRe(pattern, opts) (string, error)` emitting
that text is implementable and would make those 72 cases replayable. That is a
second full backend — an ECMAScript emitter beside the AST matcher — and it is
deliberately not in scope. It is an addition, not a change: nothing here would
have to be undone.

**Re-check.** `go doc ./... | grep -i regexp` should find nothing.

---

## 2. `Options` is reconciled against two sources, not one

**Upstream.** Options are a plain object; anything may be passed and unknown keys
are ignored.

**This port.** `Options` is a struct, so every field is a promise. The field set
is the *intersection of two independent observations*, not either one alone:

- what the upstream Mocha suite passes in — the 30 keys `tools/extract` recorded
  in `testdata/original/summary.json` under `optionSurface`;
- what upstream actually reads — every `opts.X` / `options.X` in
  `tests/original/lib` and the two entry points.

The two sets differ, in both directions, and each direction needed a rule.

**Passed but never read → no field.** `relaxSlashes` appears once, in
`test/slashes-posix.js:211`, and upstream reads it nowhere. It is inert:

```js
pm.makeRe('*').source === pm.makeRe('*', {relaxSlashes: true}).source  // true
pm.isMatch('a/', '*') === pm.isMatch('a/', '*', {relaxSlashes: true})  // true
```

A `RelaxSlashes bool` would promise behaviour picomatch does not have. The key is
still recognised by the harness — see `inertOptions` in `conformance_test.go` —
so the two fixtures using it stay in the parity denominator rather than being
dropped as unsupported over a flag that provably cannot change the answer.

**Read but never passed → field, marked.** `contains`, `fastpaths`,
`literalBrackets` and `prepend` are real options upstream reads and the suite
never exercises. They get fields, each with a doc comment saying so, because the
fixtures cannot vouch for behaviour they never trigger — the absence of a fixture
is not evidence of absence of behaviour.

**Neither → deleted.** `Options.Literal` was neither passed by the suite nor read
by upstream. `opts.literal` does not exist; `opts.literalBrackets` does, and is a
different option. The field was removed.

**Two spellings that could not survive translation:**

- `fastpaths` defaults to *on* (`opts.fastpaths !== false`). A `Fastpaths bool`
  would make the Go zero value mean "disabled" and invert the default, so the
  field is `NoFastpaths`, matching the existing `NoBrace` / `NoExtglob` idiom.
- `literalBrackets` is tested against `=== false` at `lib/parse.js:856` *and*
  `=== true` at `:865`, so unset is a third state a `bool` cannot express. It is
  `*bool`.
- `maxExtglobRecursion` accepts a number or `false` (unlimited). Kept as
  `*int` plus `UnlimitedExtglobRecursion`, so a requested cap of zero cannot
  silently become "no cap" — the cap is a denial-of-service guard
  (`test/malicious.js`).

**Re-check.** `grep -ohE "(opts|options)\.[a-zA-Z]+" tests/original/lib/*.js
tests/original/index.js tests/original/posix.js | sort -u` against
`optionSurface` in the summary.

---

## 3. Errors carry upstream's message verbatim

**Upstream.** Throws `TypeError` and `SyntaxError` with specific messages —
8 distinct ones across the 22 recorded throwing cases.

**This port.** `*picomatch.Error` carries `Name` (the JavaScript constructor) and
`Message` (byte-for-byte upstream text). `Error()` prefixes `picomatch: ` for
Go's convention that error strings are lower-case and self-identifying, while
`Message` stays capitalised because it is evidence rather than prose.

**Why not idiomatic sentinel errors alone.** The conformance harness has to
decide whether a recorded throw was reproduced. Comparing only "did an error
occur" scores `Missing closing: ")"` as equivalent to
`exceeds maximum allowed length`, and lets a port pass the throw cases by failing
for entirely unrelated reasons. The fixtures record name and message; not
comparing them would be discarding evidence already on disk.

`ErrNotImplemented` is deliberately *not* an `*Error` and never matches a
recorded throw: it is a placeholder, not a behavioural answer.

**Re-check.** Change the message in `errors.go` and re-run `make conformance`;
the two passing cases must drop to zero. (Verified: both a wrong message and a
wrong `Name` take parity from 2 to 0.)

---

## 4. `IsMatch` returns `(bool, error)`

**Upstream.** `picomatch.isMatch(str, patterns, options)` returns a bool and
throws on a bad pattern.

**This port.** Returns `(bool, error)`. Go has no exceptions, and collapsing a
compilation failure into `false` would make an invalid pattern indistinguishable
from a valid one that did not match.

An empty pattern *list* is not an error — it returns `(false, nil)`. Matching
nothing is an answer.

---

## 5. `Options.Windows` is explicit, never inferred

**Upstream.** `lib/picomatch.js` picks slash semantics from `process.platform` at
the entry point, so `a/*` has two legitimate answers depending on the host.

**This port.** `Options.Windows` is a field with no OS default. The behaviour is
part of the contract, and 1,785 of 10,465 paired fixtures (17.1%) genuinely
diverge between the two modes — mostly `[^/]` versus `[^\\/]`. Inferring it would
make the port's answer depend on the machine it runs on, and would let a
single-platform test run appear to prove both.

---

## 6. Parser state and match objects are not reproduced

`lib/picomatch.parse` returns a 17-field internal state object
(`advance`, `backtrack`, `tokens`, …) and `matcher(input, true)` returns the
JavaScript `RegExp` match array. Both are implementation details of a
regex-backed JavaScript parser.

They are excluded from parity explicitly, not silently: `parse` cases score
unsupported, and the matcher keys `match` and `regex` are listed in
`matcherFieldsNotCompared` with a stated reason. Any *other* recorded key the
harness does not compare fails the case as unsupported — see
`TestCompareFieldsRejectsUncomparedKeys`.

**Using parser state as an internal oracle is a different thing, and this is not
a quiet reversal.** `testdata/tokens/` records upstream's token stream for 1,491
patterns and `TestTokenParity` replays it against `internal/parse`. What §6
refuses is *promising* that state to callers and *counting* it as behavioural
parity; neither happens. `internal/parse` is unreachable outside this module, the
result is written to its own report under its own name, and it is never folded
into the parity percentage.

It earns its place by being a different kind of evidence. Conformance replays
behaviour, so it cannot move until the scanner, the emitter and the matcher all
work — it reads 0% for the entire time the parser is being written. The token
gate is available from the first token the scanner emits, and it localises: if
tokens differ it is a parser bug, and if tokens match while behaviour differs it
is not. The cost of being wrong about this is bounded — a fixture nobody quotes —
and the cost of *not* having it is months of a single number that says nothing.

Two properties keep it honest. It is not independent evidence: the pattern list
is inherited from upstream's suite rather than chosen, so it can only assert
structure over patterns that suite happened to use. And its records carry the
measured `fastpath` / `fastpathDiverges` flags, because for the 67 patterns
where a fast path compiled different source, matching these tokens does not
settle what `makeRe` produced. The gate stratifies on that and reports both
numbers rather than filtering the awkward ones out.

---

## 7. Parity is reported against two fixture sets, measured separately

**The problem.** `testdata/original` is recorded from picomatch's own unmodified
suite, and that is exactly what makes it worth quoting — nobody chose its
contents. But a fixture set only proves a port correct for behaviour it
exercises, and a percentage never says what it left out.

**What the measurement showed.** `tools/mutate/run.js` applies a mutation to
upstream, replays every fixture against the mutant, and counts how many detect
it. Six plausible Go-port choices survive all 18,792 comparable fixtures:

| Mutation | detected by |
| --- | ---: |
| walk by rune, not UTF-16 code unit | **0** |
| `nocase` via Unicode folding rather than JS `Canonicalize` | **0** |
| globstar body as "any character" | **0** |
| `maxLength` counted in code points | **0** |
| skip `parse.fastpaths()` — the top path at `picomatch.js:312` | **0** |
| skip the inline fast path at `parse.js:606` | **0** |

Two controls (dropping the `?` dot guard, dropping the `input === glob`
shortcut) are detected by 29 and 34, so the instrument is not dead.

The cause is not weak testing upstream. The suite is exhaustive *structurally* —
braces, extglobs, globstars, brackets, negation, slashes — and thin
*alphabetically*: 91 distinct code points across every pattern and input it uses,
U+0009 to U+30EB, four of them non-ASCII. Nothing in a structural corpus has a
reason to contain an astral character or a newline.

Every hole is a case where the idiomatic Go code is the wrong code. `for i, r :=
range s` decodes runes. `len(s)` is bytes, `len([]rune(s))` is code points, and
picomatch counts UTF-16 units — for `"😀"`, 4, 1 and 2. `(?i)` and
`unicode.ToLower` fold U+212A onto `k`; JavaScript's non-unicode `Canonicalize`
refuses to fold non-ASCII onto ASCII.

**The decision.** A second fixture set, `testdata/charaxis`, generated by
`tools/charaxis/generate.js`, covering the character domain and the one axis
upstream tests with a single test (the dot guard being lexical rather than
positional — `a{a,b/}*.txt` vs `ab/.txt` in `test/braces.js`, 2 records of
18,792).

It is kept in a separate directory and reported by a separate test
(`TestCharacterAxis`, its own `charaxis-report.json`, its own
`PICOMATCH_CHARAXIS_MIN` floor). The headline parity figure stays derived purely
from upstream's own tests. Folding the two together would mix a measurement
nobody chose with a target chosen deliberately, and the first number's whole
value is that nobody chose it.

**Why it is not grading its own homework.** The inputs are chosen — that is the
point, since upstream never chose them. The answers are not: every expected value
is recorded by running upstream picomatch, exactly as `tools/extract` does.
Nothing in the generator states what picomatch ought to do, and CI regenerates
the file and fails on any diff, so a hand-edited expectation cannot survive.

**Re-check.** `make mutate`. It fails if a mutation the suite used to detect now
survives, if a mutation is a no-op (its result would be meaningless), or if any
mutation escapes both fixture sets. The `charaxis` column killing all six holes
is verified on every run, not asserted here.

The sixth was found by splitting a mutation, not by adding one. `no-fastpaths`
was named for picomatch's inline fast path but its edit disabled the top-level
one, so the inline site had never been measured — and the top path's 6 kills made
the pair look covered. Split into `no-top-fastpaths` and `no-inline-fastpath`,
the inline site came back 0/0, and `testdata/charaxis`'s `fastpaths-inline` axis
now kills it with 8. **A mutation that does not edit what its name claims does
not measure what its result implies**, which is the same failure mode as a
witness that proves nothing — and the reason both are checked rather than
trusted.

---

## 8. The scanner indexes UTF-16 code units, not bytes or runes

**Upstream.** JavaScript strings are sequences of UTF-16 code units. `input[i]`,
`input.length`, `input.slice()` and every regex the parser runs all work in those
units, so one astral character such as `U+1F600` is two positions.

**This port.** `internal/parse` holds its input as `units` (`[]uint16`) and does
every index, slice and length comparison on it. Go strings appear only at the
package boundary, in `Parse`'s argument and in the exported `Token` values.

**Why.** The two idiomatic Go readings are both wrong, and they are wrong by
different amounts. For `"\U0001F600"`, `len(s)` is 4, `len([]rune(s))` is 1, and
picomatch counts 2. Three places in the scanner depend on the count rather than
on the characters:

| Site | What breaks under the wrong count |
| --- | --- |
| `maxLength` (`parse.js:367`) | a rune-counting guard accepts up to twice the input upstream rejects |
| `?` (`parse.js:1021`) | matches a whole astral character instead of one half |
| character-class bodies (`parse.js:755`) | accumulated one unit at a time, so they are mid-surrogate between iterations and unrepresentable in a Go string |

This is decided at the representation rather than patched at the call sites
because the third row cannot be patched: a Go string cannot hold half a surrogate
pair, so a scanner that stores values as strings has already lost the
distinction before anything compares them.

**No fixture in `testdata/original` would report the mistake.** The recorded
token corpus contains five non-ASCII patterns, all BMP (`U+30C0`–`U+30EB`), and
no astral ones, so its counts are identical under all three readings.
`tools/mutate` measures the same blindness from the other direction: the
`runes-not-code-units` mutation survives all 18,792 upstream fixtures. The
evidence that this matters lives in `testdata/charaxis`, and the arithmetic is
asserted directly in `internal/parse/units_test.go` — which is deliberately in
the *untagged* suite, since the tagged token gate cannot see any of it.

`units_test.go` asserts UTF-16, not picomatch. It is not a hand-authored fixture:
what picomatch does is still recorded, never stated.

**Re-check.** `go test ./internal/parse/ -run 'UTF16|CodeUnits'` — the maxLength
case is built from 32,768 astral characters, exactly at the 65,536-unit cap, so a
rune-counting guard passes an input twice the size upstream rejects. Then
`node tools/mutate/run.js` for the `runes-not-code-units` row: upstream 0.

---

## 9. An unbuilt construct is an error, never a guess

**Upstream.** Not applicable — upstream's parser is finished. This is a decision
about how an *unfinished* port reports itself.

**This port.** `internal/parse.Parse` returns an `UnsupportedError` naming the
construct and the upstream site (`"*" (parse.js:1128)`) when it reaches syntax
whose branch has not been written. It never falls back to treating unhandled
input as literal text.

**Why.** The fallback is the tempting one — it keeps the parser total and lets
the gate's percentage rise sooner — and it is exactly wrong here. Treating `*` as
text produces a token stream that is wrong but *plausible*, and on any pattern
where the guess happened to coincide with the recording it scores as a pass. In
the gate's percentage that pass is indistinguishable from a branch someone
actually wrote, so the number stops measuring the scanner and starts measuring
how often a guess got lucky. That is the same failure this repo exists to rule
out, one layer down from editing a fixture.

Because the errors are typed, the token gate can split its failures into two
columns that mean different things:

- **unbuilt** — the scanner refused. Expected, and shrinks on its own as branches
  land.
- **wrong** — a branch that already exists disagreed with the recording. Not
  expected at any point.

`wrong` fails the run outright rather than lowering a score, and unlike
`PICOMATCH_TOKENS_MIN` that check is not opt-in — there is no stage of the port
at which a nonzero value is acceptable. It is enforced by its own CI step: the
harness lives behind the `conformance` tag, which no other step runs, so
"unconditional" was aspirational until one existed. The percentage is the less
useful of the two numbers: it climbs as constructs are added, whereas `wrong`
only moves when something already built breaks.

The same grouping also produces the build order without anyone choosing it:
`unbuiltByConstruct` in `tokens-report.json` counts how many patterns each
missing construct blocks, so the branch worth writing next is measured rather
than argued for.

**A declined parse still returns its tokens, and they are still scored.** `Parse`
gives up at the first construct it cannot handle, so if it returned nothing on
the way out, every pattern containing one would be scored from the error alone —
and with 1,315 of 1,491 patterns in that state, "0 wrong" would be a claim about
the 176 that parse end to end. Worse, some branches could never be scored at all:
the `@` branch is only entered when the next character is `(`, which is
unsupported. So `Parse` returns the partial state alongside the
`UnsupportedError`, and `comparePrefix` in `tokens_test.go` checks it against the
recording's leading tokens.

The last of those tokens is excluded, and the reason is measured. A pushed token
is not final — every retroactive assignment in `parse.js` is to `prev.<field>`
(`:500-502`, `:722`, `:730`, `:867`, `:872`, `:999-1001`, `:1129-1132`,
`:1179-1236`), so the token adjacent to the construct that stopped the scanner is
exactly the one that construct would have edited. Comparing it reports 592 of the
1,491 corpus patterns as `wrong` against a scanner that is right: `js/*.js`
records the slash before the star with output `\/(?!\.)(?=.)`, which the star
branch writes, and no correct scanner produces that before the star branch
exists. Everything earlier is settled, because `prev` is always the last pushed
token. The cost is that the token immediately before an unbuilt construct is
still unscored — including the `@` case above.

**Re-check.** `make tokens` prints the split and the blocking constructs.
`go test -tags conformance -run TestCompareTokensChecksStateAndLength ./...`
asserts that a declined construct is classified as unbuilt, that a genuine
disagreement is not, and that a parse error never compares equal. For the prefix
scoring specifically: break a built branch that runs before an unbuilt one —
changing the slash output from `slashLiteral` to `"/"` moves `make tokens` from
`0 wrong` to 238, of which 151 are only visible because the prefix is compared.

---

## 10. The `string` boundary is lossy in two directions, and both are recorded

**Upstream.** A JavaScript string is a sequence of UTF-16 code units with no
further constraint. It can hold an unpaired surrogate, and it has no
representation for a byte that is not part of one.

**This port.** `Parse` takes and returns Go strings. `encode` substitutes U+FFFD
for each byte of invalid UTF-8 on the way in; `units.String` substitutes U+FFFD
for an unpaired surrogate on the way out. Both are documented at the functions
in `internal/parse/units.go`. Neither is worked around.

**Why, going in.** There is nothing for a stray `0xFF` byte to become — JS has no
such value — so any mapping is invented. The cost is real and worth stating
plainly rather than discovering later: `encode("a\xffb")` and `encode("a\xfeb")`
produce the same units, so `State.Consumed` does not round-trip a pattern
containing invalid UTF-8, and two such patterns are indistinguishable to this
package. Whether the public API should take `[]byte` instead is a question about
[§1](#1-no-compiled-regular-expression-is-exposed)-level signatures, not about
the scanner, and it is not settled here.

**Why, coming out.** This one is a genuine divergence from a recorded value, not
just from a hypothetical caller. The scanner does not merely pass surrogates
through — it splits them. Upstream's quoted-string branch (`parse.js:765-770`)
appends to `prev.value` alone, so a token value or output can end on a lone high
surrogate, and upstream records it as one. Parsing `@"\` followed by U+1F600
gives token 1 an output of `@\` plus a lone `U+D83D` upstream and `@\` plus
U+FFFD here. An exhaustive enumeration over patterns of up to five characters
drawn from `{"`, `\`, U+1F600, `a`, `@`, `.`, `/`} found 31 of 19,599 differing
this way, every one a U+FFFD substitution.

**Why it is not fixed.** It could be: Go strings can carry WTF-8, so
`units.String` could encode a lone surrogate as `ED A0 BD` rather than folding it
to U+FFFD. That change alone makes things worse, not better. The fixture side is
lossy in the same place and by the same amount — `encoding/json` maps `\uD83D` to
U+FFFD when `internal/tokencase` decodes a record — so today the two agree by
coincidence, and a port that stopped losing the surrogate would start failing the
gate against a fixture loader that still does. The fix is both halves at once,
and it belongs with `internal/tokencase`, not here.

**No fixture reports it.** The token corpus contains no astral pattern at all
(five non-ASCII patterns, all BMP), which is the same blindness
[§8](#8-the-scanner-indexes-utf-16-code-units-not-bytes-or-runes) records from
the counting side.

**Re-check.** `node -e` on `tests/original/lib/parse.js` against
`internal/parse` for the pattern above; and
`go test ./internal/parse/ -run 'UTF16|CodeUnits'` for the arithmetic that still
holds.

---

## 11. Input on which upstream never returns is an error, not a hang

**Upstream.** `parse()` does not terminate on some inputs. Its `eos()` is
`state.index === input.length - 1`, an equality, and the backslash-run collapse
at `parse.js:689-699` does `state.index += slashes` followed by `advance()`,
which can step the index past that value. Once it has, `eos()` is never true
again, `advance()` returns `''` forever, and the loop spins.

Measured, not inferred: `a` followed by one, two or three backslashes returns; `a`
followed by four or more never does. There is no throw, no output, and no
timeout.

**This port.** The scanner detects the overshoot and returns a
`NonTerminatingError` naming the upstream site. `eos()` is also widened from `==`
to `>=`, so the loop stays bounded even if a future branch finds another way to
step over the end.

**Why not reproduce it.** Faithfulness has nothing to be faithful *to* here.
Upstream produces no observable result for this input, so there is no recorded
behaviour to match, and no fixture can ever hold one — `tools/extract` runs
upstream to record it and would hang on the same pattern. That blindness is
structural, not an oversight in the corpus.

**Why not invent an answer either.** Terminating quietly is the other tempting
option: widen `eos()`, let the loop fall out, and return whatever state happens
to have accumulated. That is the fallback
[§9](#9-an-unbuilt-construct-is-an-error-never-a-guess) rejects, one layer over —
a plausible state upstream never produces, indistinguishable in the gate from a
real one. An error says exactly what is known: upstream does not answer this.

`NonTerminatingError` returns a nil state for the same reason.

**Re-check.** `go test ./internal/parse/ -run 'Terminat'` — a five-second
deadline around `Parse` over backslash runs of 1 to 12, and the boundary at three
versus four. Upstream's half:
`node -e "require('./tests/original/lib/parse.js')('a\\\\\\\\', {fastpaths:false})"`
does not return.

---

## Escape hatches

None. No `unsafe`, no cgo, no `any` in non-generic positions in the port itself.
`any` appears only in the conformance harness, where it decodes arbitrary
recorded JSON values, and in `internal/testcase`, for the same reason.
