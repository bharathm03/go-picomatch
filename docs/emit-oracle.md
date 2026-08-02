# The emitter oracle

What `testdata/emit` measures, what it cannot see, and which option to build
next. Every number here was **measured**, and every one carries the command that
re-derives it — **re-run those rather than trusting this file**, which goes stale
the moment a branch or an option lands.

```bash
make emit           # replay testdata/emit against the port -> emitter %
make emit-fixture   # re-record it from the vendored upstream (needs Node)
make tokens         # the half of the oracle that already existed
```

The authority for every census figure below is `testdata/emit/summary.json`,
which the generator writes and CI regenerates and diffs. Where this file and
that file disagree, that file is right and this one is stale.

## Where the line numbers point

Every `parse.js:N` and `picomatch.js:N` below is **`tests/original/lib/`** — the
vendored upstream, hash-pinned by `MANIFEST.json` at picomatch v4.0.5
(`4f41a8edad`). That directory is read-only. There is no other copy of picomatch
in this repo and none is needed.

---

## 1. Half of this oracle already existed

The three-oracle table said the emitter had no gate. That was wrong, and it was
wrong in a way worth stating precisely, because it changes what the new fixture
is *for*.

`tools/tokens/generate.js:90` records `output: st.output` — the full scanner's
emitted source, under default options — and `tokens_test.go:217` compares it:

```go
if got.Output != c.Output {
```

So the **full-scanner emitter under default options has been gated all along**,
and it is at `1491 / 1491 (100.00%)` with `0 wrong`, the same figure `make
tokens` prints for the parser. The emitter row in `CLAUDE.md`'s table used to
point at `tools/probes`, which is a diagnostic and gates nothing.

What that gate does **not** cover is three things, and `testdata/emit` exists for
exactly those:

| Layer | Upstream site | Gated before |
|---|---|---|
| non-default options | `opts.*` throughout `parse()` | no |
| `parse.fastpaths()` | `parse.js:1330`, called at `picomatch.js:313` | no |
| `compileRe`'s `^(?:…)$` wrap | `picomatch.js:272`, negation wrap at `:275` | no |
| flags | `picomatch.js:343` — `opts.flags \|\| (opts.nocase ? 'i' : '')` | no |

**Re-check.**

```bash
sed -n '90p' tools/tokens/generate.js          # output: st.output,
sed -n '217p' tokens_test.go                   # if got.Output != c.Output {
go test -tags conformance -run TestTokenParity -v ./     # 1491/1491, 0 wrong
```

---

## 2. The record unit, and the field census

The record is one distinct **(pattern, emit-relevant-options) pair**. Projecting
the 20,198 pattern-bearing records of `testdata/original` onto the 26 keys that
can change `state.output`, the compiled source, the flags or the throw collapses
them to **2,038 pairs** over 1,285 patterns and 36 option sets — 1,020 with
default options and 1,018 without.

Scoring is **field-level, not case-level**. A case-level score would force a
weighting judgment (is a record carrying a fastpath output worth more than one
without?); the field denominator is mechanical, and each layer contributes in
proportion to how much of it was recorded.

`summary.json`'s census, verbatim:

| Layer | Fields | What is in it |
|---|---:|---|
| path | 2,028 | which of the three parsers `makeRe` used |
| scanner | 4,067 | `scannerOutput` + `negated`, or `scannerThrow` |
| fastpath | 728 | one per *eligible* record — declining is an answer too |
| compile | 4,056 | `source` + `flags` |
| **total** | **10,879** | `cases.comparableFields` |

Field presence across the 2,038 records, which is where those come from:

```
scannerOutput 2029   negated 2029   scannerThrow 9
path 2028   output 2028   source 2028   flags 2028   throw 10
fastpathOutput 79   fastpathThrow 1   (fastpathEligible is on every record, true on 728)
```

`flags` is `""` on 2,017 of the 2,028 and `"i"` on 11.

**Two recorded fields are deliberately *not* in that denominator.** `output` and
`throw` are recorded on every applicable record but excluded from
`Case.Layers()`, because `output` is derivable from `path` plus the other two
outputs — the derivation being the claim under test — and counting it would let
one layer be scored twice. The recorder stores them anyway: a fixture that keeps
only the ingredients makes the gate compute the answer it is supposed to check.

`summary.json`'s `comparableFields` and the gate's own census agree at 10,879.
They are two independent implementations of the same rule, so if they ever
diverge, one of them has silently changed what counts.

**Re-check.**

```bash
node -e "console.log(JSON.stringify(JSON.parse(require('fs').readFileSync('testdata/emit/summary.json')).layers))"
grep -c . testdata/emit/cases.jsonl                       # 2038
go test -tags conformance -run TestEmitParity -v ./        # fields=10879
```

---

## 3. The ceilings, and why the build order is not obvious

Two axes are missing, not one, and they are worth **roughly the same**:

| If you built… | Fields won | of 10,879 |
|---|---:|---:|
| nothing more than today | 2,038 | **18.73%** |
| defaults only, all three layers | 5,414 | 49.77% |
| all options, scanner only | 4,067 | 37.38% |

**Neither axis alone clears half**, and they are within twelve points of each
other, so the build order is not obvious from the code and this measurement is
the thing that decides it.

The first row is what `make emit` prints today:

```
cases=2038 fields=10879 matched=2038 unbuilt=8841 wrong=0 emitter=18.73%
  layer    scanner            2038 of 4067 matched (50.11%)
  layer    fastpath           0 of 728 matched (0.00%)
  layer    compile            0 of 4056 matched (0.00%)
  layer    path               0 of 2028 matched (0.00%)
  options  defaultOptions     2038 of 5414 matched (37.64%)
  options  nonDefaultOptions  0 of 5465 matched (0.00%)
```

Those 2,038 fields are the default-options `scannerOutput` and `negated` pair on
1,018 pairs plus the `scannerThrow` field on the 2 default-options pairs where
the scanner throws — and the `defaultOptions` stratum's 5,414 *is* the
defaults-only ceiling in the table above, printed by the gate itself. Every point
of the 18.73% is **already proven by `make tokens`**, which is why a floor set
today buys nothing: leave `PICOMATCH_EMIT_MIN` unset until `windows` is threaded,
then set it.

Everything else is unbuilt for a stated reason, and the gate must record it as
`unbuilt` rather than `wrong`: `internal/parse.Parse` takes one argument and has
no `Options` type, so a non-default case has no callable entry point at all.

**A figure that does not reproduce.** An earlier draft put the defaults-only
ceiling at `12,906 / 19,162 = 67.35%` of *records*. Measured, a defaults-only
emitter is unblocked on **10,082 of 20,198** pattern-bearing records — **49.92%**
— and no filter (portable-only, one platform, replayable-only) produces 67.35%.
The record-level number is in any case the wrong unit: the fixture's unit is the
pair, and the gate's is the field.

**Re-check.**

```bash
node -e "const r=require('fs').readFileSync('testdata/emit/cases.jsonl','utf8').split('\n').filter(Boolean).map(JSON.parse);
const has=(x,k)=>Object.prototype.hasOwnProperty.call(x,k);let t=0,d=0,s=0;
for(const x of r){const def=Object.keys(x.options).length===0;
const sc=(has(x,'scannerOutput')?2:0)+(has(x,'scannerThrow')?1:0);
const co=(has(x,'source')?1:0)+(has(x,'flags')?1:0)+(has(x,'path')?1:0)+(x.fastpathEligible?1:0);
s+=sc;if(def){t+=sc;d+=sc+co;}}console.log({today:t,defaultsAllLayers:d,allOptionsScanner:s})"
```

---

## 4. The option build order

Ranked by the **pairs** each key unblocks. This is `optionSurface` in
`summary.json`, and it is stated in pairs rather than records because the pair is
the fixture's unit — the record counts in the right-hand column are given only so
the two are not confused.

| Key | Pairs | Records |
|---|---:|---:|
| `windows` | **570** | 6,048 |
| `strictSlashes` | 245 | 2,726 |
| `bash` | 235 | 1,488 |
| `dot` | 207 | 1,756 |
| `posix` | 65 | 462 |
| `regex` | 52 | 390 |
| `noextglob` | 20 | 150 |
| `nocase` | 8 | 30 |
| `noglobstar` | 7 | 14 |
| `flags` | 6 | 24 |
| `maxExtglobRecursion` | 6 | 16 |
| `strictBrackets` | 6 | 12 |
| `unescape` | 6 | 44 |
| `nobrace` | 5 | 34 |
| `capture` | 2 | 4 |
| `keepQuotes` | 2 | 12 |
| `nonegate` | 2 | 28 |
| `maxLength` | 1 | 2 |
| `nobracket` | 1 | 6 |
| `noext` | 1 | 8 |

The columns rank differently — `bash` is third by pairs and fourth by records,
`unescape` is eighth by records and thirteenth by pairs — which is the whole
reason to say which unit a number is in.

The head of the list is where the work is. The top four keys — `windows`,
`strictSlashes`, `bash`, `dot` — **fully** unblock 882 of the 1,018 non-default
pairs, meaning those pairs use no other key at all; 967 of the 1,018 carry at
least one of them.

**`windows` first, by a wide margin.** It alone is 570 of the 1,018 non-default
pairs, **56%**, and it is the *only* key on 324 of them. It is also a
*constants-table swap* rather than a branch:
`parse.js:377` and `:1351` pass it positionally to `constants.globChars`, which
tests `win32 === true`. The port already spells the POSIX set as constants and
the Windows set is two leaves away (`SLASH_LITERAL`, `QMARK`). Cheapest first
move available.

**The record-level ranking in an earlier draft does not reproduce** (`windows`
3240, `strictSlashes` 1840, `dot` 1668, `bash` 1132, `regex` 300, `posix` 296,
`unescape` 70 …). The measured record counts are the right-hand column above, and
they agree with `testdata/original/summary.json`'s independently recorded
`optionSurface` to within the handful of `lib/scan` and `lib/utils` records that
carry no pattern (`windows` 6,048 here against 6,056 there).

**Five allow-listed keys the corpus never exercises at all:** `contains`,
`debug`, `fastpaths`, `literalBrackets`, `prepend`. This fixture gates
twenty-one of its twenty-six keys and is **silent** on those five, so a green
`make emit` says nothing about them. `literalBrackets` is the one that matters:
`CLAUDE.md` names it as needing an answer before code — tested `=== false` at
`parse.js:856` and `=== true` at `:865`, so unset is a genuine third state — and
no recorded fixture will ever settle it. Covering them needs *chosen* inputs,
i.e. a `charaxis`-shaped companion set with its own directory and its own floor.

**Re-check.**

```bash
node -e "const s=JSON.parse(require('fs').readFileSync('testdata/emit/summary.json'));
console.log(s.optionSurface);console.log(s.optionProjection.neverExercised)"

node -e "const r=require('fs').readFileSync('testdata/emit/cases.jsonl','utf8').split('\n').filter(Boolean).map(JSON.parse);
const nd=r.filter(x=>Object.keys(x.options).length>0),four=['windows','strictSlashes','bash','dot'];
console.log({nonDefault:nd.length,
fullyCoveredByTopFour:nd.filter(x=>Object.keys(x.options).every(k=>four.includes(k))).length,
touchingOne:nd.filter(x=>Object.keys(x.options).some(k=>four.includes(k))).length,
windowsOnly:nd.filter(x=>Object.keys(x.options).every(k=>k==='windows')).length})"
```

---

## 5. The parser-path split

Upstream has three parsers and they disagree. Over the 1,491 patterns of
`testdata/tokens`, under **default options**:

| Path | Patterns | Diverge from the scanner |
|---|---:|---:|
| full scanner | 1,316 | 0 |
| `parse.fastpaths()` (top) | 25 | 23 |
| inline (`parse.js:606`) | 150 | 44 |
| **total** | **1,491** | **67** |

**Eligibility is not use.** `picomatch.js:312` calls `parse.fastpaths` for every
pattern whose first code unit is `.` or `*`. Over the 1,493 distinct corpus
patterns, **382 are eligible and 25 take it** — 356 return falsy and fall through
at `picomatch.js:316`, and 1 throws. That fall-through is the single most
consequential recorded fact about the path, and it is why `fastpathEligible` is
its own boolean in the fixture: there are three states (not eligible / eligible
and falsy / eligible and a string), and a lone optional string collapses the
first two.

With options in play the split moves, because eligibility and the fall-through
both depend on them. Over the 2,038 emit pairs: **none 1,750, inline 199, top
79** — and 728 eligible, of which 79 return output, 648 return falsy and 1
throws.

An earlier draft put the default-options split at 1,318 / 25 / 150. Measured it
is **1,316** / 25 / 150; the total is 1,491 either way, so the slip is in the
scanner column.

**Re-check.**

```bash
node -e "console.log(JSON.parse(require('fs').readFileSync('testdata/tokens/summary.json')).fastpath)"
node -e "console.log(JSON.parse(require('fs').readFileSync('testdata/emit/summary.json')).path)"
node -e "const c=require('./tools/probes/lib/corpus'),p=c.parseModule();let e=0,o=0;
for(const q of c.patterns()){if(q[0]!=='.'&&q[0]!=='*')continue;e++;try{if(p.fastpaths(q,{}))o++}catch(x){}}
console.log('eligible',e,'took it',o)"
```

---

## 6. What the emitter has to produce

The construct inventory over the **1,490 compilable outputs** — occurrences in
`makeRe(p, {}, true)`, i.e. `state.output`, over the 1,493 distinct corpus
patterns (3 throw):

| Construct | Occurrences | | Construct | Occurrences |
|---|---:|---|---|---:|
| `(?:` | 2,211 | | `(` | 267 |
| `[…]` (any class) | 1,865 | | `+` | 86 |
| `*` | 1,792 | | `{n,m}` | 51 |
| `*?` | 1,707 | | `\w` `\d` `\s` | 12 |
| `[^…]` | 1,641 | | `\b` | 10 |
| `(?!` | 1,448 | | backreference | 9 |
| `\|` | 1,254 | | lookbehind | 4 |
| `.` | 1,232 | | | |
| `(?=` | 784 | | | |
| `$` | 541 | | | |

**632 of the 1,490 — 42.42% — are free of both lookaround and backreferences**,
which is [DECISIONS.md](../DECISIONS.md) §1 read from the other side: two fifths
of this corpus has no construct RE2 rejects outright, and the other three fifths
is why there is no `MakeRe`. `(?!` and `(?=` together appear 2,232 times.

Counts are occurrences, not patterns, and the matchers are the ones in the
command below — `(?<!\\)` guards mean an escaped `\*` is not counted as a star,
and a `.` inside a character class *is* counted. Change the matchers and the
numbers move; that is why they are printed rather than described.

**Re-check.** Save this to a file and run `node <file>` rather than pasting the
heredoc into Git Bash on Windows, which strips the `\\` in each lookbehind guard
and leaves an unterminated group.

```bash
node - <<'JS'
const c = require('./tools/probes/lib/corpus');
const pm = c.upstream();
const R = {
  '(?:': /\(\?:/g, '[...]': /(?<!\\)\[/g, '*': /(?<!\\)\*/g, '*?': /(?<!\\)\*\?/g,
  '[^...]': /(?<!\\)\[\^/g, '(?!': /\(\?!/g, '|': /(?<!\\)\|/g, '.': /(?<!\\)\./g,
  '(?=': /\(\?=/g, '$': /(?<!\\)\$/g, '(': /(?<!\\)\((?!\?)/g, '+': /(?<!\\)\+/g,
  '{n,m}': /\{\d+(,\d*)?\}/g, '\\w\\d\\s': /\\[wds]/gi, '\\b': /\\[bB]/g,
  'backreference': /\\[1-9]/g, 'lookbehind': /\(\?<[=!]/g,
};
const s = [];
for (const p of c.patterns()) {
  try { pm.makeRe(p); } catch (e) { continue; }
  s.push(pm.makeRe(p, {}, true));
}
for (const [k, r] of Object.entries(R)) {
  console.log(k.padEnd(14), s.reduce((n, x) => n + (x.match(r) || []).length, 0));
}
const free = s.filter(x => !/\(\?[=!]|\(\?<[=!]/.test(x) && !/\\[1-9]/.test(x));
console.log('free of lookaround+backrefs', free.length, 'of', s.length);
JS
```

---

## 7. Four traps this fixture is shaped around

The first two have cost this repo once already; the last two were found by the
adversarial pass over this fixture and are transcribed in
[docs/transcription-traps.md](transcription-traps.md) #52 and #53. All four are
now fixture *facts* rather than footnotes, which is the point of recording
`output`, `source` and `path` as three separate fields.

### (a) Record `makeRe(p, opts, true)`, never the compiled `.source`

The third argument is `returnOutput`, and it short-circuits at
`picomatch.js:265` before `compileRe` runs. `compileRe` then adds its **own**
`^(?:…)$` layer, so diffing `.source` against anything double-counts that wrap.
The recorder never compares the two; it records both, in different fields.

### (b) The inline fast path is already wrapped

`parse()`'s inline fast path calls `utils.wrapOutput` **inside** `parse()`
(`parse.js:653`), so its `state.output` is already `^(?:X)$` where the scanner's
is a bare `X`. Comparing them raw marks every inline pattern divergent whatever
it produced.

Measured on the 1,491-pattern corpus: the raw comparison reports **172**
divergences; unwrapping the inline side leaves **67** — 44 on the inline path,
23 on the top path, 0 on the full scanner. `tools/probes/lib/corpus.js`'s doc
block said "the corrected count is 34" until this fixture landed; it now carries
the measured 67 and its own re-check. The two trailing-slash figures beside it
(the top path adds `\/?` on 5 patterns, the scanner adds it on 28 the inline
path leaves strict) always did reproduce, and the remaining 34 differ
structurally — which is where the 34 came from.

The Go gate never has to unwrap anything, because it **stratifies on the
recorded `path` field** instead of filtering. The worked example, a real line of
`testdata/emit/cases.jsonl`:

```json
{"pattern":"foo","options":{},"fastpathEligible":false,"scannerOutput":"foo","negated":false,"path":"inline","output":"^(?:foo)$","source":"^(?:^(?:foo)$)$","flags":""}
```

`foo` under default options: the scanner would have emitted `foo`, the inline
path emitted `^(?:foo)$`, and `compileRe` wrapped that again into
`^(?:^(?:foo)$)$`. All three are recorded, so no reader has to reconstruct which
layer added which anchor.

### (c) `source` is `^(?:output)$` on 2,020 of 2,028 records, not all of them

Two upstream facts break the identity, and neither is picomatch's own logic:
`RegExp.prototype.source` **escapes every unescaped `/` outside a character
class** on the way out (5 records, all containing `[/]`), and `toRegex`
**swallows a `SyntaxError` and returns `/$^/`** unless `opts.debug === true`
(3 records, whose `source` is the literal `$^`). Traps #52 and #53 carry the
full reading; the census:

```bash
node -e "const r=require('fs').readFileSync('testdata/emit/cases.jsonl','utf8').split('\n').filter(Boolean).map(JSON.parse);let ok=0,fb=0,slash=0;for(const x of r){if(x.source===undefined)continue;const w='^(?:'+x.output+')\$';if(x.source===w||x.source==='^(?!'+w+').*\$'){ok++;continue}if(x.source==='\$^'){fb++;continue}slash++}console.log({ok,fallback:fb,slashArtifact:slash})"
```

prints `{ ok: 2020, fallback: 3, slashArtifact: 5 }`. `TestEmitParity` is fatal
on any `Wrong`, so these are 8 false disagreements waiting for the day the
compile layer's blocker is lifted.

### (d) `output` was recorded and asserted by nothing

The design deliberately does not *score* `output` — it is the path plus the
layer outputs read as one answer, and scoring it would weight the same claim
twice. "Not scored" quietly became "not constrained", and a fabricated value
survived the whole suite. `TestEmitFixtureShape` now asserts it against the
layer its own `path` field names: `output == fastpathOutput` on the top path
(79/79) and `== scannerOutput` on the full scanner (1,750/1,750).

`inline` is deliberately exempt: its `output` is `wrapOutput`'s, over the bare
`parse(pattern, opts)` that no field records — `scannerOutput` is the
`{fastpaths: false}` run, which differs on 59 of the 199 inline records. That
non-derivability is precisely why `output` is a recorded field and not a
computed one, and asserting a false equality there would be worse than
asserting nothing.

**Re-check.**

```bash
grep -n '^{"pattern":"foo","options":{}' testdata/emit/cases.jsonl
node -e "const pm=require('./tests/original');
console.log(pm.makeRe('foo',{},true));console.log(pm.makeRe('foo',{}).source)"
```

The two lines print `^(?:foo)$` and `^(?:^(?:foo)$)$`. The 172-versus-67 count
is re-derived by comparing `makeRe(p,{},true)` against
`makeRe(p,{fastpaths:false},true)` over `testdata/tokens/cases.jsonl`, once raw
and once with the inline side unwrapped; `fastpathDiverges` in that fixture is
the recorded, unwrapped answer, and `testdata/tokens/summary.json` reports its
total as 67.

---

## What this oracle deliberately does not record

Two things, both with the argument written out in
[DECISIONS.md](../DECISIONS.md) §16: the **platform** (measured: `utils.isWindows`
is exported and called nowhere in `lib/`, so `opts.windows` is the only platform
input to the emitter and a platform axis would double the file to describe
nothing), and any option value the recording encodes as **`$function`**
(`expandRange` only — 32 records, 7 pairs, counted in `summary.excluded` rather
than silently dropped).

## Related

- [build-order.md](build-order.md) — the scanner's build order, finished. This
  file is its sibling for the emitter, and the two rank different things: that
  one ranked constructs, this one ranks option keys and layers.
- [DECISIONS.md](../DECISIONS.md) §6 — why parser and emitter state is an
  internal oracle and never folded into the parity percentage. `testdata/emit`
  is upstream's own patterns but upstream's internal output, so it is reported
  separately for exactly that reason.
- [DECISIONS.md](../DECISIONS.md) §1 — why there is no `MakeRe`, which §6 above
  measures from the other side.
- [transcription-traps.md](transcription-traps.md) #50 and #51 — the two places
  `parse.fastpaths` and `parse()` read the *same* option differently.
