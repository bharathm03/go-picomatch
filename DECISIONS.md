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

"Everything earlier is settled" holds for every retroactive site in `parse.js`
except two. `:1190` and `:1205` write `prior.output`, two tokens back, so a
scanner that declined `**` at upstream's own branch would hand back a prefix the
exemption does not cover. [§12](#12--is-declined-one-character-before-upstreams-branch-for-it)
records how the scanner stopped earlier instead, and what that cost, until the
globstar branch landed and the question stopped arising for `**`.

The general form has not gone away, and it is what to check when the next branch
declines something. The exemption is one token deep because that is how deep
`prev` reaches; any construct that rewrites `prior` has to be declined *before*
it emits, not at upstream's own line, or its unbuilt prefix is wrong rather than
merely short. Two sites in `parse.js` do that today and both are now built.

**The bracket branch added the deepest rewrite in the file, and it is safe for a
reason worth writing down rather than rediscovering.** `parse.js:734` assigns
`bos.output` — token **zero**, however long the token list has grown. Nothing
about the exemption covers that. What makes it harmless is that the rewrite
cannot be separated from the tokens it follows: while `state.brackets` is
nonzero, the only branches the loop can reach are the character-class body, the
closing `]`, the U+0000 skip and the escape arm that falls into the body. None
of them is unbuilt, and none of them can be, because every construct that would
decline — `?`, `{`, an unbuilt extglob — is swallowed by the body branch as a
class member. So a parse can never stop *between* a bracket token being pushed
and the POSIX class that rewrites `bos` behind it. The check for the next branch
is that same question, not the depth: can the scanner decline while the rewrite
is still pending?

The extglob branch found a third form the rule did not anticipate: a rewrite
whose depth is *unbounded* and whose firing is not decided until a later
character. `extglobClose`'s risky path rewrites the extglob's opening token and
blanks everything after it, and what decides it is the body — which the scanner
has not read when it declines. Declining earlier is not available there, because
"earlier" would mean refusing every `+(` on sight.
[§14](#14-a-declined-parse-inside-an-open--or--hands-back-nothing-from-that-extglob-onwards)
records what the scanner does instead, which is to hand back less.

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
`state.index === input.length - 1`, an equality, and **two** sites step the index
past that value. Once either has, `eos()` is never true again, `advance()`
returns `''` forever, and the loop spins.

The first is the backslash-run collapse at `parse.js:689-699`, which does
`state.index += slashes` followed by `advance()`. Measured, not inferred: `a`
followed by one, two or three backslashes returns; `a` followed by four or more
never does.

The second was found by the bracket branch, and it is a bare `advance()` rather
than an arithmetic overshoot. The POSIX-class rewrite at `parse.js:732` steps
over the `]` of `:]` without checking that one is there, so a resolvable class
name whose closing `:` is the **last unit of the input** leaves the index at
`length`. It needs an open character class, which needs a `]` somewhere ahead of
the `[` — so the `]` that satisfies `:815` has to be one the body already
consumed. `[][:alpha:` is the shortest witness; `[[:alpha:` is not, because
without the earlier `]` the class never opens. There is no throw, no output, and
no timeout in either case.

**This port.** The scanner detects both and returns a `NonTerminatingError`
naming the upstream site — `parse.js:689` or `parse.js:732`. `eos()` is also
widened from `==` to `>=`, so the loop stays bounded even if a third site turns
up.

**The second site is not rare, and finding it was luck of the enumeration.** Over
the bracket branch's differential corpus, 812 of 65,308 enumerated prefixes of
POSIX-class patterns reach it — `[[:alnum:][:alnum:` and `[^][:upper:` among them
— against 8 of 19,607 for the backslash runs. A 25-pattern sample across the 812
was run against node under a 4-second timeout: 25 hang, 0 return. The port
reports `nonterm` on exactly those and on nothing else in 1,178,803 compared
patterns, so the detection is neither missing cases nor inventing them.

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

## 12. `**` is declined one character before upstream's branch for it

**Spent.** The globstar branch landed, `startsGlobstar` is gone from
`internal/parse/scanner.go`, and nothing declines `**` any more. The entry is
kept rather than deleted because it made a prediction — "it reverts on its own
when stage 2 of the star branch lands and the lookahead is deleted" — and a
decision that expires as predicted is worth more on the record than one that
quietly disappears. Everything below described the state of the port between the
two stages of the star branch; the re-check no longer applies, and the note at
the end about `docs/build-order.md`'s stage-1 figure still does.

**Upstream.** Not applicable, in the same way as
[§9](#9-an-unbuilt-construct-is-an-error-never-a-guess): upstream has no unbuilt
constructs. This is a decision about *where* an unfinished scanner stops.

**This port.** The scanner refuses `**` at its **first** star, before any token
is pushed, rather than at the second one where upstream's branch sits
(`parse.js:1145`). `startsGlobstar` in `internal/parse/scanner.go` is the
lookahead, and it repeats upstream's own `/^\([^?]/` test at the second star so
that `**(a)` — which enters `extglobOpen` at `:1140` and never reaches `:1145` —
keeps being reported as the extglob it is.

**Why.** [§9](#9-an-unbuilt-construct-is-an-error-never-a-guess) exempts the last
token of a declined parse from scoring, on the measured grounds that every
retroactive assignment in `parse.js` is to `prev.<field>` — so the token adjacent
to the unbuilt construct is the one that construct would have edited, and
"everything earlier is settled".

The globstar arms break that. `:1190` and `:1205` assign `prior.output`, where
`prior` is `prev.prev` — **two** tokens back, one deeper than the exemption
covers. Declining at `:1145` therefore hands the gate a prefix that is not merely
short but wrong: it scored **104** corpus patterns as `wrong` against a scanner
whose star branch is correct. `a/**` records its slash as `(?:\/(?!\.)`, and the
`(?:` is written by the globstar branch — no scanner produces it before that
branch exists.

The two alternatives are both worse. Widening the exemption to two tokens throws
away real scoring across every other construct to accommodate one. Emitting the
star anyway and accepting 104 `wrong` deletes the only unconditional check the
gate has. Refusing the whole `**` construct as a unit keeps §9's invariant true
as stated: what the scanner hands back is settled, and what it will not answer it
does not answer.

**The cost, stated.** The token immediately before a `**` is now unscored where
it used to be scored wrongly — the same cost §9 already names for every other
unbuilt construct, one position earlier. It reverts on its own when stage 2 of
the star branch lands and the lookahead is deleted.

**Not a way of buying a percentage.** It changes no pattern from failing to
passing. Before and after, `go test -tags conformance -run TestTokenParity ./`
reports the same **441 passed (29.58%)**; only the failure classification moves,
from 946 unbuilt + 104 wrong to 1,050 unbuilt + 0 wrong.

**Re-check.** Delete the `startsGlobstar(rest)` disjunct at `parse.js:1145` in
`internal/parse/scanner.go` and re-run
`go test -tags conformance -run TestTokenParity -v ./`: 104 patterns move to
`wrong`, every one of them a `slash` token reported as
`want "(?:\\/(?!\\.)", got "\\/(?!\\.)"`, and the pass count does not move.

**A separate thing this measurement settled.** `docs/build-order.md` puts stage 1
at 489 patterns. That figure comes from `tools/probes/build-order.js`, which
estimates a branch's yield from the recorded token *types* — and 48 of the 489
are patterns whose `**` upstream turns into a `globstar` and then rewrites back
into a plain `star` (push()'s lookbehind at `:499-502`, and the non-start arm at
`:1161-1166`). Their recorded types are all `star`, so the probe counts them as
plain-star patterns; producing them requires the globstar branch. The probe's own
header calls its number an upper bound. The reachable stage-1 figure is 441.

**Re-check.**
`node -e "const p=require('./tests/original/lib/parse.js'); const s=p('**c',{fastpaths:false}); console.log(s.consumed, s.globstar, s.tokens.map(t=>t.type+':'+t.value).join(' '))"`
prints `**c true bos: star:* text:c` — two stars consumed, one `star` token of
value `*`, and `state.globstar` set.

---

## 13. Two values on the scan path that Go cannot hold

`internal/scan` is a complete port of `lib/scan.js` — all 586 recorded
`lib/scan.scan` cases and all 12 `lib/utils.basename` cases replay — with two
exceptions, both at the boundary where a JavaScript value has no Go counterpart.

**`opts.tokens` is not ported.** Upstream reads it at `scan.js:342` and it does
two separate things. It attaches `state.tokens`, one object per path segment
carrying `value`, `depth`, `isPrefix` and its own copies of the result's flags,
plus `state.maxDepth`. And it attaches `state.parts` and `state.slashes`
(`:350`) *without* also forcing scan-to-end, which is what `opts.parts` does at
`:53` — so the two keys do not produce the same segment list:

```
scan('**/*.js', {parts:  true})   slashes [2]  parts ["**", "*.js"]
scan('**/*.js', {tokens: true})   slashes []   parts []
```

`picomatch.ScanResult` has no field for a token list and `picomatch.Options` has
no key that would ask for one, so `internal/scan.Options` deliberately does not
add one either — a field there would be a promise the exported type cannot keep.
This is the same line [§6](#6-parser-state-and-match-objects-are-not-reproduced)
draws around `parse()`'s state, applied to a different upstream file: the
segment tokens are a JavaScript caller's view of the scanner's internals, not a
behaviour.

Nothing recorded exercises it. Across the 586 scan cases the option surface is
`unescape` (44), `parts` (42), `noparen` (10), `scanToEnd` (4), `nonegate` (2)
and `noext` (2) — `tokens` appears zero times, so no fixture leaves the
denominator over this.

**`basename("")` returns `""` where upstream returns `undefined`.**
`utils.js:63` splits the path and takes the last segment; for `""` the split is
`[""]`, that last segment is empty, so it falls to `segs[-1]` and yields
`undefined`. A Go function returning `string` has no such value, and widening
the signature to `(string, bool)` would put a second return on every call site
to describe one input.

The empty path is the only input that reaches it. Over all 7,812 combinations of
a path of up to five characters drawn from `{/ \ a b .}` and both `windows`
settings, `""` is the sole case where upstream returns `undefined`; every other
input, including paths that are nothing but separators, has a real segment to
return.

**Re-check.**
`node -e "const u=require('./tests/original/lib/utils.js'); console.log(u.basename('', {}) === undefined)"`
prints `true`, and `go test ./internal/scan/ -run Basename` pins the Go side.
For the first: `node -e "const s=require('./tests/original/lib/scan.js'); console.log(JSON.stringify(s('**/*.js',{tokens:true}).parts), JSON.stringify(s('**/*.js',{parts:true}).parts))"`
prints `[] ["**","*.js"]`. For the option surface,
`grep '"api":"scan"' testdata/original/cases.jsonl | grep -c tokens` is 0.

---

## 14. A declined parse inside an open `+(` or `*(` hands back nothing from that extglob onwards

**Spent, as predicted, and the prediction is why the entry is kept.** The brace
branch landed, `prefixTokens` is gone from `internal/parse/scanner.go`, and a
declined parse now hands back every token it produced. Everything below described
the port between the extglob stage and this one. The entry made two predictions
and both held: that the truncation would become unreachable when `{` was built,
and — the part that mattered — that **the token gate would not be the thing that
told you**, because the corpus witness had already stopped witnessing.

The re-check the entry specified is the one that was run, not `make tokens`.
Re-enumerating `+*(|{)`≤7, `*(|{)?`≤7 and `+(|{a)`≤6 — 610,645 patterns — against
`parse(p, {fastpaths: false})` with `prefixTokens` deleted: **610,645 compared,
0 declined, 0 mismatches**. The 423,109 patterns that used to be declined and
prefix-compared now parse end to end, including `+(+{|)`, the shortest witness
the entry named; there is no prefix left to be wrong about. The truncation is
unreachable because nothing in the loop returns an `UnsupportedError` for a
construct any more, which is the "no by exhaustion" the build order predicted.

**Upstream.** Not applicable, in the same way as
[§9](#9-an-unbuilt-construct-is-an-error-never-a-guess) and
[§12](#12--is-declined-one-character-before-upstreams-branch-for-it): upstream
has no unbuilt constructs. This is a decision about what an unfinished scanner
may claim.

**This port.** When `Parse` returns an `UnsupportedError` while a `+(` or `*(`
extglob is still open, the partial state stops at that extglob's opening token.
`prefixTokens` in `internal/parse/scanner.go` is the truncation; a parse that
*succeeds* keeps every token, and an extglob left open at end-of-input is not
truncated because it never reaches `extglobClose`.

**Why.** §9's exemption is one token deep, on the measured grounds that every
retroactive assignment in `parse.js` is to `prev.<field>`. §12 recorded the first
site that reaches further — the globstar arms' `prior.output`, two back — and the
extglob branch adds one that has no fixed depth at all.

`extglobClose`'s risky path (`parse.js:544-566`) rewrites
`tokens[token.tokensIndex]` into a single text token carrying the whole literal,
and then blanks the value and output of **every token after it**:

```js
open.type = 'text';
open.value = literal;
open.output = safeOutput || utils.escapeRegex(literal);

for (let i = token.tokensIndex + 1; i < tokens.length; i++) {
  tokens[i].value = ''; tokens[i].output = ''; delete tokens[i].suffix;
}
```

Whether it fires is not known until the closing `)` is reached: it depends on
`analyzeRepeatedExtglob`'s verdict on the body, which is a slice of input the
scanner has not read yet. So a scanner that declines *inside* a `+(` has already
emitted tokens it cannot stand behind, and no lookahead short of re-implementing
the paren matcher would tell it which.

Measured: without the truncation, `go test -tags conformance -run TestTokenParity
./` reports **1 wrong** — `+(*|?)`, which upstream records as
`bos text:"+(*|?)":"\+\(\*\|\?\)" paren star text qmark paren`, a single text
token where the scanner had pushed `plus`. The scanner is not wrong about the
`plus`; upstream pushed one too and then replaced it.

**The three alternatives, and why each is worse.** Widening §9's exemption to
cover it means exempting an unbounded suffix, which is most of the token list for
exactly the patterns this stage added. Emitting anyway and accepting `1 wrong`
deletes the only unconditional check the gate has, over one pattern. Declining
the whole `+(…)` construct at its opener — §12's move — would refuse 43 patterns
that parse correctly today, because the great majority of extglob bodies contain
nothing unbuilt.

**The cost, stated, and it is not free.** Tokens between a `+(` or `*(` and an
unbuilt construct inside it are no longer scored. That is **12 of the 433
declined corpus patterns**, and on eleven of the twelve the longer prefix
*agreed* with the recording — so the truncation gives up eleven real comparisons
to stop one wrong claim. The trade is taken because those eleven were not
evidence of anything: whether the rewrite fires is decided by input the scanner
never read, so agreeing was a property of those bodies rather than of the
scanner, and a gate that reports `wrong` on one of twelve arbitrary cases is
reporting the corpus, not the port. Tokens *before* the extglob are unaffected:
the risky path never touches them,
and the one edit that reaches back from inside — push()'s globstar lookbehind at
`:494-505`, rewriting a preceding `globstar` — lands on the last token of the
truncated prefix, which the gate already excludes.

**It reverts on its own,** like §12: when `[`, `?` and `{` land there is no
unbuilt construct left to decline inside an extglob, and `prefixTokens` becomes
unreachable.

**Not a way of buying a percentage.** It changes no pattern from failing to
passing. Before and after, the gate reports the same **1058 passed (70.96%)**;
only the classification moves, from 432 unbuilt + 1 wrong to 433 + 0.

**Re-check.** Replace `prefixTokens`'s body with `return s.tokens` and re-run
`go test -tags conformance -run TestTokenParity -v ./`: `+(*|?)` moves to
`wrong`, reported as `token 1 (text) … type: want "text", got "plus"`, and the
pass count does not move.

**Re-measured after the bracket and qmark stages, and the re-check above no
longer works.** `+(*|?)` was the corpus's only witness and it now parses end to
end, so the gate reports `0 wrong` with the truncation removed. `{` is the sole
remaining decline, and no pattern in `testdata/tokens` puts one inside an open
`+(` or `*(` — measured, 0 of 1,491.

That did not make the truncation a no-op, only invisible to that corpus, which
is the distinction the whole differential exists to draw. Enumerating
`+*(|{)`≤7, `*(|{)?`≤7 and `+(|{a)`≤6 — 610,646 patterns, 423,109 of them
declined and prefix-compared — the truncation is required on **1,135**. The
shortest witness is `+(+{|)`: upstream's `analyzeRepeatedExtglob` calls the body
risky, rewrites the opening `plus` into a single `text` token holding
`+(+{|)`, and the un-truncated port hands back the `plus` it had every reason to
push. Exactly the shape the entry was written for, one construct later.

So the prediction stands and its trigger has moved: `prefixTokens` becomes
unreachable when the **brace** branch lands, not when `[` and `?` did, and the
gate will not be the thing that tells you. Delete it then, and re-run the
enumeration above rather than the gate to confirm.

*(That is what happened. See the note at the top of this entry for the numbers.)*

---

## 15. `expandRange` asks a regex engine a question Go's cannot answer, so the port answers it itself

**Upstream.** `expandRange` (`lib/parse.js:22-38`) turns a `{a..b}` into a
character class, and decides whether that class is legal by **compiling it**:

```js
args.sort();
const value = `[${args.join('-')}]`;

try {
  new RegExp(value);
} catch (ex) {
  return args.map(v => utils.escapeRegex(v)).join('..');
}

return value;
```

So the brace token's `output` — a recorded field, and one the emitter will have
to reproduce — is chosen by whatever the host JavaScript engine accepts. This is
the only place in `parse()` whose answer comes from an *engine* rather than from
the grammar.

**This port.** `internal/parse/ecmaregexp.go` implements the acceptance predicate
directly: `ecmaRegExpValid(src units) bool`, a transcription of the ECMAScript
pattern grammar (ES2024 22.2.1 plus the Annex B B.1.2 extensions) for a
non-unicode pattern compiled with no flags. It parses; it does not compile,
match, or hold any state. `expandRange` in `internal/parse/brace.go` calls it
where upstream calls the constructor.

**Why not `regexp.Compile`.** It answers a different question, and it differs in
both directions. Measured on `[X]` values from the reachable domain:

| Source | V8 | RE2 |
| --- | --- | --- |
| `[]` | empty class, **ok** | `missing closing ]` |
| `[a-\d]` | Annex B, **ok** | `invalid escape sequence: \d` |
| `[\b]` | backspace, **ok** | `invalid escape sequence: \b` |
| `[\c]` | literal `\` then `c`, **ok** | `invalid escape sequence: \c` |
| `[😀-😁]` | `\uDE00-\uD83D`, **rejected** | ok — RE2 ranges over runes |

The first four are cases where the port would take the `..` fallback upstream
never takes; the last is one where it would emit a class upstream refuses.
Routing the question to RE2 puts picomatch's answer for `{a..b}` at the mercy of
a grammar picomatch has never seen — and the last row is
[§8](#8-the-scanner-indexes-utf-16-code-units-not-bytes-or-runes)'s rune-versus-code-unit
divergence arriving through a third door.

**Why not skip the check.** The `catch` is reachable, and the sort is what makes
that non-obvious: `{z..a}` sorts to `[a-z]` and `{b..a}` to `[a-b]`, so the
obvious candidates never get there. The sort is *lexicographic over strings*
while the range test is over single characters, and `{ac..b}` is the two-argument
counterexample — it sorts to `["ac", "b"]`, builds `[ac-b]`, and `c-b` is
backwards, so upstream returns the literal `ac..b`. Over an enumerated corpus of
1,304,643 patterns, `expandRange` runs 28,902 times and **1,938 of those take the
`catch`**; assuming the class is always valid differs from upstream on every one.
That is the fallback [§9](#9-an-unbuilt-construct-is-an-error-never-a-guess)
rejects, one construct along.

**What the predicate has to get right, and what it does not.** For a non-unicode
pattern with no flags the constructor throws in exactly twelve ways, and Annex B
lets everything else through — an over-large backreference is a legacy octal
escape, `\c` with nothing controllable after it is a literal backslash, a `{`
that does not parse as a quantifier is a pattern character, and `]` and `}`
outside a class are pattern characters. Three properties are not optional:

- it counts **code units**, so `[😀-😁]` is four units between the brackets and
  the range V8 checks is `\uDE00` against `\uD83D` — out of order, so the class
  is *rejected*. The same reason the scanner holds `units`
  ([§8](#8-the-scanner-indexes-utf-16-code-units-not-bytes-or-runes));
- it computes each ClassAtom's **value**, because the range check compares
  characters and an escape stands for one — `[a-\c]` is `'a'` against U+005C;
- it pre-scans for a GroupName, because `\k` is a named backreference exactly
  when one appears **anywhere** in the pattern, before it or after it.

**Measured, not argued.** Two differentials, both against `new RegExp` under
node:

- **1,222,753 distinct enumerated pattern sources**, over nineteen families
  covering class ranges and every escape form, quantifier braces, group nesting,
  named groups, duplicate names and `\k` references — 351,114 of them accepted by
  V8 and 871,639 rejected. **Zero disagreements.**
- **3,938 distinct `[X]` values** that `expandRange` actually produced over the
  brace branch's 1,304,643-pattern parse differential, 553 of which V8 rejects.
  Zero disagreements, and the parse differential itself reports zero mismatches
  on `output` and on the brace tokens.

`TestECMARegExpValidIsTheJavaScriptGrammar` pins a table of rows in the untagged
suite, on the footing `TestDropLastIsJavaScriptSlice` and `units_test.go` already
stand on: it asserts JavaScript's semantics, not picomatch's answer for any
pattern, and every row came out of running node rather than out of reasoning.

**`opts.expandRange` is a gap, not a decision.** Upstream reads it at `:23` and
it replaces the whole helper with a caller-supplied **function**, so by
[§2](#2-options-is-reconciled-against-two-sources-not-one)'s rule — a key
upstream reads gets a field — `Options` owes it one. It does not have one, and
this stage did not add it: the surrounding question is what a Go signature for
`(...args, options) => string` should be, which belongs with the API rather than
with the scanner. `internal/parse` marks the site. The corpus never passes it —
`expandRange` does not appear in `optionSurface` — so no fixture leaves the
denominator over it.

**Re-check.** `go test ./internal/parse/ -run ECMARegExp` for the table. For the
differential, dump `ecmaRegExpValid` over an enumeration and compare against
`node -e "try { new RegExp(s) } catch (e) { ... }"`; the teeth are checked the way
`tools/mutate` checks its own — replacing the predicate with `true` scores
`0 wrong` on the token gate and **1,938 of 1,304,643** on the parse differential,
and dropping `args.sort()` scores `0 wrong` and **15,607**. Both are recorded as
trap #47.

---

## Escape hatches

None. No `unsafe`, no cgo, no `any` in non-generic positions in the port itself.
`any` appears only in the conformance harness, where it decodes arbitrary
recorded JSON values, and in `internal/testcase`, for the same reason.
