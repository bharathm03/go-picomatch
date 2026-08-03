# Transcription traps

Places where the obvious Go reading of upstream's source is wrong — where a
careful, idiomatic transcription produces code that compiles, reads correctly,
and disagrees with picomatch.

This is not a list of bugs in picomatch, and not a list of divergences in the
port. Every entry is somewhere the port *matches* upstream because the trap was
noticed. It is here so the next branch does not have to notice it again.

**Add to this list when a branch turns up another one.** An entry needs the
upstream site, the reading that would have been wrong, and what it costs —
without the third part a reader cannot tell whether it is worth the attention.

## How these were found, and what that implies

By reading `lib/parse.js`, not by the token gate. The gate reported `0 wrong` on
its first run precisely because these had already been handled.

That is the ordering worth keeping. `make tokens` splits its failures into
**unbuilt** (the scanner refused a construct it does not implement) and **wrong**
(a branch that exists disagreed with the recording), and `wrong` fails the run
unconditionally — see [DECISIONS.md](../DECISIONS.md) §9. But it is a backstop,
not the method: it can only catch a trap on a pattern the corpus happens to
contain, and it tells you a token differed, not which reading was too clever.
Reading first is cheaper than bisecting afterwards.

---

## 1. `!` falls through — `parse.js:1053-1065`

```js
if (value === '!') {
  if (opts.noextglob !== true && peek() === '(') { ... continue; }
  if (opts.nonegate !== true && state.index === 0) { negate(); continue; }
}
// no continue here
```

Every neighbouring branch ends by consuming its character. This one does not: a
`!` that is neither an extglob opener nor at index 0 drops out of the branch
entirely and is picked up by the plain-text case at `:1109`.

**The wrong reading.** Closing the branch the way its neighbours close — adding a
trailing `continue`, or restructuring into a `switch` where each case is
self-contained — silently discards every mid-pattern `!`.

**What it costs.** `a!b` loses a character. A `switch` on the current character
is the natural Go shape for this loop and it makes the trap structural rather
than accidental, which is why the port keeps the `if`/`continue` chain in
upstream's order instead.

## 2. The text merge is JavaScript-truthy — `parse.js:513`

```js
prev.output = (prev.output || prev.value) + tok.value;
```

`prev.output || prev.value` falls back to the value when `output` is the **empty
string**, not only when it is absent. JavaScript's `||` tests truthiness; `""` is
falsy.

**The wrong reading.** `if prev.output != nil` — the direct translation of "has
an output" — is a different test, and it differs on exactly the tokens that carry
an empty output.

**What it costs.** 1,883 of the 10,558 recorded tokens have `output` present and
empty (against 2,366 with it absent), so this is not an edge case. It is also the
reason `Token.Output` is a `*string`: a plain `string` collapses absent and empty
into one state and cannot express either side of this line.

## 3. The bare `+` arm is shaped unlike its siblings — `parse.js:1087`

```js
if ((prev && prev.value === '(') || opts.regex === false) {
  push({ type: 'plus', value, output: PLUS_LITERAL });   // :1078
}
if (... prev.type === 'bracket' | 'paren' | 'brace' ...) {
  push({ type: 'plus', value });                          // :1083
}
push({ type: 'plus', value: PLUS_LITERAL });              // :1087
```

Three arms, three shapes. The last one puts the escape in `value` and sets **no
`output` at all**, where the first sets both and the second sets neither.

**The wrong reading.** Normalising the three into one constructor — the obvious
tidy-up — gets the token type and the emitted text right and the field
*presence* wrong.

**What it costs.** Nothing visible in the output string, which is what makes it
dangerous: the regex source matches and only the token record differs. The
pointer fields on `Token` exist so the gate can see it.

**Measured, now that all three arms are built.** Giving the third arm the
shape of the first — `{ type: 'plus', value, output: PLUS_LITERAL }` instead of
`{ type: 'plus', value: PLUS_LITERAL }` — emits the identical regex and scores
**14 wrong**, on inputs as short as `+`, which records `plus:"\+"` with no
`output` at all. On the enumerated differential below it is 17,635 of 159,424.
The entry predicted exactly this before the arms existed; the numbers are the
receipt.

## 4. `}` emits a literal where `]` escapes — `parse.js:840` vs `:901`

```js
push({ type: 'text', value, output: `\\${value}` });   // ']' with no '['  :840
push({ type: 'text', value, output: value });          // '}' with no '{'  :901
```

An unmatched `]` is escaped. An unmatched `}` is not.

**The wrong reading.** The two branches are otherwise symmetrical, sit ninety
lines apart, and both handle "closing delimiter with nothing open". Factoring
them together, or assuming the second is a typo for the first, changes the
emitted regex.

**What it costs.** `\}` and `}` mean the same thing to most regex engines, so
this survives casual testing and shows up as a token-level difference rather than
a behavioural one.

## 5. `state.consumed` is not a slice of the input — `parse.js:430`, `:982`

Three separate mechanisms detach `consumed` from the input the caller passed:

- `utils.removePrefix` strips a leading `./` **before the loop starts**
  (`:430`), so `./foo` consumes `foo`;
- the leading-`!` run is consumed by `negate()` and never appended, so
  `!!!!!!!!abc` consumes `abc`;
- escaped values are accumulated in their **escaped** form, so `!$` consumes
  `\$` — two characters where the input had one.

Separately, `:982` resets `consumed` to empty mid-parse when a `./` survived
behind a negation.

**The wrong reading.** Treating `consumed` as an offset or a subslice of the
input — the representation a Go port reaches for, since it avoids a copy.

**What it costs.** 23 of the 134 simplest patterns in the corpus already differ
from their input, before any of the interesting syntax is built. It also runs the
other way: `Consumed` can be *longer* than the input it came from, which an
offset cannot express at all. The doc on `State.Consumed` records the case where
parsing `a/**/*.js` consumes `a/**//*.js` — the scanner invents a slash.

## 6. `consume` has a second parameter, and eight of nine callers omit it — `parse.js:447`

```js
const consume = (value = '', num = 0) => {
  state.consumed += value;
  state.index += num;
};
```

Two jobs, one helper. Eight call sites pass only the value; the ninth is
`consume('/**', 3)` in the globstar branch (`:1175`), which is also advancing the
index past the three characters it just accounted for.

**The wrong reading.** Writing the helper from the calls you have in front of you.
Every branch built so far routes through `append()`, which calls
`consume(token.value)` — so a Go transcription that stops at
`func (s *scanner) consume(value units)` is complete for everything currently
written, compiles, passes, and is missing half its body.

**What it costs.** Nothing yet, which is the problem: the parameter is missing
exactly until the branch that needs it. `*` is top of the measured build order at
696 blocked patterns, and the globstar path is inside it — so the next person to
open this file reaches for a helper whose signature says it does the whole job,
and the scanner silently re-reads `/**`. The port carries the parameter with all
current callers passing `0` rather than a comment saying to remember.

## 7. `slice(0, -X.length)` empties the output when `X` is empty — `parse.js:499`, `:861`, `:1189`, `:1204`, `:1232`

```js
state.output = state.output.slice(0, -prev.output.length);
```

Read as "drop the last `n` units", which is what it is for every non-degenerate
case. When `prev.output` is the empty string, `n` is `0` — and JavaScript's `-0`
is `0`, so the call is `slice(0, 0)`: the **whole output is discarded**.

**The wrong reading.** `out[:len(out)-n]`. It is the correct translation of the
sentence, it is what a reviewer would ask for, and on `n == 0` it does the
opposite thing: it leaves `out` untouched.

**What it costs.** Every one of the five sites is a truncate-then-rebuild, so the
difference is not a missing character but a whole regex source retained where
upstream threw it away.

**And, measured: nothing yet — which is the reason to keep the entry.** Stage 2
of the star branch built four of the five sites (`:499`, `:1189`, `:1204`,
`:1232`; the fifth, `:861`, arrived with the bracket branch — see below), and the
degenerate case is still not reached. Instrumenting a *copy* of `tests/original/lib/parse.js`
at all four and enumerating every pattern of up to four characters over the
seventeen characters `parse()` branches on — 88,740 patterns — the length being
sliced is **0 on none of them**. The port agrees from the other side: with
`dropLast` panicking on `n == 0`, 402,233 patterns of up to five characters parse
without reaching it, and replacing `dropLast` with the plain Go reading moves the
token gate by **0 wrong**.

An empty `output` is common — 1,883 of the 10,558 recorded tokens carry one — but
not on the token these five sites truncate by. The reachable pairing needs an
empty-output `star` (`:1157`, `:1164`) or an empty-output `slash` (`:1216`,
`:1227`) to arrive as `prev` or `prior` at a truncation, and the arms that
produce those two are exactly the arms that route the next star away from the
truncations. It is not obviously impossible — brackets, braces and extglobs all
add ways to reach `:1189` and `:1204` with a different `prior` — so the helper
stays JavaScript-faithful and this entry stays.

**Re-measured independently, and now pinned by a test.** A second instrumented
copy, at the same four sites but over three different alphabets — `*/a.!,b}` to
length 5, `*/a.` to length 8, `*/.!a@+}]|$,` to length 4, 147,448 patterns — hits
them 6,666 / 1,098 / 1,099 / 10,451 times and again records a sliced length of
**0 on none of them**. So the degenerate case is unreachable from any pattern
`Parse` accepts today, and no fixture, no gate run and no pattern-driven test can
speak for it: replacing `dropLast` with the plain Go reading still moves the token
gate by `0 wrong`, and differs from upstream on none of the 193,353 patterns the
differential below compares token for token.

That is an argument for asserting the helper directly rather than for leaving it
unasserted. `TestDropLastIsJavaScriptSlice` in `internal/parse/scanner_test.go`
does that — `slice(0, -0)`, `n` at and past the length, and a truncation landing
inside a surrogate pair — and it fails on the plain Go reading at the first row.
It asserts JavaScript's `slice`, not picomatch's answer for anything, which is
the same footing `units_test.go` stands on. All four built sites route through the
one helper, so one test covers them all.

**`:861` is now built, and it is the one site this entry cannot vouch for.** It
slices by `prev.value.length` rather than by an output's length, and `prev.value`
there is `[` plus at least the `]` appended at `:851` — so `n` is never below 2,
and the degenerate case is not merely unreached but unreachable. Both readings
score `0 wrong` on the gate and 0 of 294,118 on the bracket branch's
differential. The port routes it through `dropLast` regardless, because it is the
same operation and a second helper would have to be kept in step; but by trap
#27's standard the misreading is a no-op at that site and is not listed as one.

## 8. `:1170` peeks `input[state.index + 4]`, not `+ 3` — `parse.js:1168-1176`

```js
while (rest.slice(0, 3) === '/**') {
  const after = input[state.index + 4];
```

`rest` is `remaining()`, which is `input.slice(state.index + 1)`. So `rest[0]` is
already one ahead of the index, and the character *after* the three that
`rest.slice(0, 3)` matched sits at `index + 4`.

**The wrong reading.** Pairing the `3` in `slice(0, 3)` with a `+ 3` here — the
two numbers look like they must be the same offset, and the loop reads naturally
as "match three characters, then look at the next one".

**What it costs.** Off by one into the character *being* matched rather than the
one after it, so `/**/` stops being stripped and `/***` starts being. Measured
now that the branch exists: `+ 3` takes the token gate from `0 wrong` to
**23 wrong**, `a/**/**/b` among them — upstream consumes `a/*/***//b` there and
the misreading consumes `a/**/**//b`, because the strip loop breaks on the
character it was supposed to skip.

## 9. `:1263` tests `state.index === state.start`, not `=== 0` — `parse.js:1263`

The dot guard on a leading star is applied when the star is the first character
*of the pattern proper*, and `state.start` is not always `0`. It is advanced by
`negate()` once per `!` in the leading run (`:462`, `:471`) and reset outright at
`:981` when a `./` survived behind a negation.

**The wrong reading.** `state.index === 0`. It agrees on every pattern that does
not begin with `!`, which is most of the corpus.

**What it costs.** `!*` loses `(?!\.)(?=.)` — the guard that stops a leading star
matching a dotfile — so the negation of "everything visible" quietly becomes the
negation of "everything". `!./*` is the same, two characters further along.

## 10. The star's dot guard is appended to two places at once — `parse.js:1263-1281`

```js
state.output += nodot;
prev.output  += nodot;
```

The guard is written into the emitted output *and* back into the token that was
already emitted. It is the port's first retroactive edit to a pushed token, and
`prev` here is a `bos`, `slash` or `dot` token whose output the scanner is still
holding.

**The wrong reading.** Growing `prev.output` in place — `append(*prev.output,
g...)` — which is what push() does for the text merge and is there for a good
reason (cloning on every merge makes a 65,536-unit literal quadratic). At this
site the operand is a token that other state may share a backing array with, and
`append` writing into spare capacity lands inside whatever else points there.

**What it costs.** Nothing deterministic, which is the worst kind: the corruption
depends on what `append` decided to allocate, so the same pattern can parse
correctly and then not, depending on the order the tests ran in.
`TestTokensDoNotShareMemory` grows each field past its end and checks the others
are unchanged, and it now carries `state.output` as a field for exactly this
pair.

## 11. The star token is built before the guard and pushed after — `parse.js:1246` vs `:1283`

Thirty-seven lines separate `const token = { type: 'star', ... }` from
`push(token)`, and the guard rules at `:1263-1281` run between them.

**The wrong reading.** Pushing at the point of construction — the tidy shape,
and the one every neighbouring branch uses, since they all `push(...)` inline.

**What it costs.** `push` calls `append`, which is what puts the star's own
`[^/]*?` into `state.output`. Pushing first emits the star and *then* the guard,
so `*` compiles to `[^/]*?(?!\.)(?=.)` instead of `(?!\.)(?=.)[^/]*?` — a regex
that still compiles and still matches many of the same strings, anchored at the
wrong end.

## 12. A token's `value` can shrink — `parse.js:501`

```js
prev.type = 'star';
prev.value = '*';
```

push()'s globstar lookbehind assigns `prev.value` rather than appending to it, so
a token that had grown to `**` goes back to `*`. Every other retroactive write in
the file is `prev.value += value`.

**The wrong reading.** Treating `value` as append-only — the invariant every
other site upholds — and therefore treating `state.consumed` and the token values
as two views of the same text. Upstream records `**c` as `consumed: "**c"` with a
single `star` token of value `*`.

**What it costs.** Directly, it makes `state.consumed` unreconstructible from the
tokens, which is a second reason (beside trap #5) not to represent it as an
offset. Measured now that the branch exists: appending instead of assigning takes
the token gate from `0 wrong` to **41 wrong**, every one of them a token recorded
as `star:"*"` reported as `star:"**"` — and `state.consumed` still says `**`,
which is what makes the two look reconcilable when they are not.

Indirectly it is a planning trap: `tools/probes/build-order.js` estimates a
branch's yield from the recorded token *types*, so the 48 patterns whose `**` is
rewritten back into a plain `star` look reachable with the plain-star branch
alone. They are not — see [DECISIONS.md](../DECISIONS.md) §12.

---

# lib/scan.js

`scan()` is a second entry point with its own state machine, and it shares no
code with `parse()` — so it has its own traps. All five below were found by
reading it before writing the port; the cost figures come from applying each
misreading to a *copy* of `tests/original/lib/scan.js` and replaying every
pattern of up to four characters over the sixteen characters `scan()` branches
on, times seven option sets: 489,370 comparisons. `tests/original` is never
edited, exactly as `tools/mutate` does it.

A sixth candidate is deliberately not listed. `base` at `:303` is the *input*
string, held only for its emptiness while the slices below it come from the
reassigned `str` — but reading it as the base instead changes no result in the
same 489,370 comparisons, so it is a curiosity rather than a trap, and listing a
no-op would be the same mistake `tools/mutate` guards against.

## 13. `(code = advance())` is a truthiness test, so a NUL ends the loop — `scan.js:101`, `:187`, `:228`, `:260`

```js
while (eos() !== true && (code = advance())) {
```

Four inner loops — brace, extglob, bracket, paren — are written this way. The
assignment's value is the character code, and JavaScript's loop condition tests
it for truthiness, so the loop ends on **0** as well as on the NaN that a
past-the-end `charCodeAt` returns. Code 0 is a literal NUL in the pattern.

**The wrong reading.** `for !eos() { code = advance(); ... }`, which is the
natural Go shape and the one the surrounding `eos()` call invites. It keeps
scanning past a NUL.

**What it costs.** 7 of 489,370. Small, and entirely invisible to the recorded
corpus, which contains no NUL: `scan('{a\0,b}')` is **not** a brace upstream —
the loop dies on the NUL before reaching the comma — where the eos-only reading
reports `isBrace: true`, `isGlob: true` and hands back a different base and glob.

## 14. The `..` test consumes the character it compares — `scan.js:113`

```js
if (braceEscaped !== true && code === CHAR_DOT && (code = advance()) === CHAR_DOT) {
```

`advance()` runs inside the condition, so a dot inside a brace always eats the
character after it, and `code` keeps that character whether or not it was the
second dot. The comma test on the next line then sees the *new* character.

**The wrong reading.** `peek() === CHAR_DOT`, which is what the line means and
not what it does. It leaves the following character to be re-read by the next
iteration.

**What it costs.** 34 of 489,370, and the shortest witness is four characters:
`{.{}` is a brace upstream, because the `{` after the dot is swallowed by this
line and never increments `braces`, so the closing `}` takes the count to zero.
Under the peeking reading `braces` reaches 2, never returns to 0, and `isBrace`
stays false.

## 15. `prevIndex ? ... : start` treats index 0 as "no previous slash" — `scan.js:354`, `:373`

```js
const n = prevIndex ? prevIndex + 1 : start;
...
if (prevIndex && prevIndex + 1 < input.length) {
```

`prevIndex` holds the index of the previous slash, and both sites test it for
truthiness. A leading slash is at index **0**, which is falsy, so it reads as
"there was no previous slash" — at the first site the next segment is measured
from `start` instead, and at the second the trailing segment is not emitted at
all.

**The wrong reading.** Treating the variable as the optional it looks like —
`prevIndex !== undefined`, or a Go `(int, bool)` pair, or an `int` initialised to
-1. All three are the careful transcription and all three are wrong.

**What it costs.** 599 of 489,370, the largest of the five, and it is the
`parts` array — the reason to pass the option at all. `scan('//', {parts: true})`
gives `["/"]` upstream and `[""]` under the careful reading.

## 16. The paren loop tests `(` where its three siblings test `\` — `scan.js:261`

```js
if (code === CHAR_LEFT_PARENTHESES) {
  backslashes = token.backslashes = true;
  code = advance();
  continue;
}
```

The brace, extglob and bracket scan-to-end loops all open with the same shape
testing `CHAR_BACKWARD_SLASH`. This one tests `CHAR_LEFT_PARENTHESES` — and
still sets `backslashes`, which is what gives it away as a slip rather than an
intent.

**The wrong reading.** Making it symmetrical, on the reasonable belief that four
loops written alike were meant alike.

**What it costs.** 2 of 489,370, so this is the entry that pays for itself least
— but it is also the one a reader is most likely to "fix". `scan('(\\)/',
{parts: true})` yields `parts ["(\\)"]` and `slashes [3]` upstream, and both
empty under the symmetrical reading: the `(` arm consumes the `)` that would
otherwise have set `finished`, and `finished` is what the slash branch at `:161`
consults.

## 17. The trailing-separator strip is guarded against the base being the whole input — `scan.js:313`

```js
if (base && base !== '' && base !== str) {
  if (isPathSeparator(base.charCodeAt(base.length - 1))) {
    base = base.slice(0, -1);
  }
}
```

Four conditions, and the interesting one is the last. `base !== str` means the
trailing separator is stripped only when a glob was actually split off; when the
whole (prefix-stripped) input *is* the base, its trailing separator stays.

**The wrong reading.** Dropping it as redundant — `base` is either a slice of
`str` or `str` itself, so `base !== str` looks like it is already implied by the
branch above.

**What it costs.** 40,627 of 489,370, by far the most of any misreading here,
and it is not confined to exotic input: `scan('\\')` has base `\` upstream and
`""` without the guard. It is also the only one of the five the recorded corpus
catches on its own — dropping it takes the 586 scan cases from 100% to 90.10%.

---

# lib/parse.js, the globstar arms

Four more from `parse.js:1145-1244`, found by reading the branch before writing
it. The cost figures are the token gate's `wrong` column with the misreading
applied and nothing else changed; `0 wrong` is the correct value, so every number
below is the number of corpus patterns that misreading breaks.

**The branch has since been re-derived from the JavaScript a second time and
cross-checked against upstream directly**, because `0 wrong` on the gate is a
backstop and not a proof: it can only report a misreading on a pattern the
inherited corpus happens to contain, and 55 of the patterns this branch unblocks
compile differently under the fast path, so the scanner alone does not pin them.
The check runs `parse(pattern, {fastpaths: false})` — the exact function
`internal/parse.Parse` corresponds to, and therefore independent of the fast-path
question — over 193,353 enumerated patterns across five alphabets
(`*/a.!,b}`≤5, `*/a.`≤8, `*/.!a@+}]|$,`≤4, `*/`≤13, `*/a`≤9) and compares
`consumed`, `output`, `globstar`, `backtrack`, `negated` and every token's type,
value, `star`, `extglob` and `output` — *presence included*. Zero mismatches.

The teeth are checked the way `tools/mutate` checks its own: `+ 3` for `+ 4` at
`:1170` differs on 49, appending instead of assigning `state.output` at `:1182`
differs on 7, dropping the empty `output` from the `:1164` star differs on 15,038
and from the `:1216`/`:1227` slashes on 2,536. The corpus is enumerated rather
than committed — regenerating it needs Node and `tests/original` stays read-only —
so it is a check that can be re-run, not a fixture.

## 18. Two arms *assign* `state.output`, they do not append to it — `parse.js:1182`, `:1224`

```js
state.output = prev.output;      // :1182 and :1224
state.output += prev.output;     // :1196, :1211, :1240 — the other three
```

Five arms of the branch finish by putting the globstar body into `state.output`.
Three append. Two — the whole-pattern `**` at `:1178` and the leading `**/` at
`:1220` — assign, discarding everything the loop had accumulated. What they
discard is not nothing: the star branch at `:1263-1274` has already written the
`(?!\.)` dot guard into `state.output` *and* into `bos.output`, and only the
`state.output` copy goes.

**The wrong reading.** `+=`, on the reasonable belief that five arms of one
branch ending the same way were meant to end the same way. It is also the reading
Go pushes you towards, because `s.output = *s.prev.output` aliases two slices that
are both appended to later, so the assignment has to be written as an explicit
copy — and at that point `append` looks like the tidier line.

**What it costs.** **109 wrong**. `**` compiles to `(?!\.)(?:(?:(?!(?:^|\/)\.).)*?)`
instead of `(?:(?:(?!(?:^|\/)\.).)*?)` — a globstar that has stopped matching
dotfiles at the top level while still matching them everywhere below, which is
the one thing `**` is for. The recorded `bos` token keeps its `(?!\.)` either
way, so the tokens agree and only `state.output` differs.

## 19. The globstar arms rewrite two tokens back and must *not* set `state.backtrack` — `parse.js:1188-1243` vs `:1133`

`:1190` and `:1205` assign `prior.output`, where `prior` is `prev.prev`. That is
a retroactive edit to a token emitted two pushes ago, and it is deeper than
anything else in the file reaches. `state.backtrack` — the flag whose whole job
is "output has been invalidated, rebuild it from the tokens at `:1309`" — is
nevertheless left alone. Upstream sets it at exactly one site in `parse()`,
`:1133`, and that site does not touch `prior` at all.

**The wrong reading.** Setting it wherever an already-emitted token is rewritten.
That is what the flag appears to mean, it is what a reviewer would ask for on
seeing `prior.output` assigned, and the arms hand-maintain `state.output` in
parallel precisely so it is not needed.

**What it costs.** **116 wrong**, and none of them is a wrong regex: the arms
keep `state.output` in step, so the rebuild produces the same string. What breaks
is `state.backtrack` itself, which is a recorded field. Moving the truncation at
`:1189` to *after* the `prior.output` rewrite at `:1190` scores 116 as well — the
other way to get this pair wrong, and worth knowing about because the two lines
look independent and are not: `:1189` measures the output `:1190` replaces.

## 20. `:1214` and `:1226` consume the slash, and then `push()` consumes it again — `parse.js:1214-1216`, `:1226-1227`

```js
consume(value + advance());              // "*" plus the "/" that follows it
push({ type: 'slash', value: '/', output: '' });
```

`push` calls `append`, which calls `consume(token.value)`. So the `/` is added to
`state.consumed` twice from two different lines, and `state.consumed` ends up
holding a separator the input never had: `a/**/b` consumes `a/**//b`.

**The wrong reading.** Deduplicating it. Either half looks redundant on its own —
`advance()` inside `consume` reads as "step past the slash", and the pushed token
reads as "record the slash" — and removing either one produces a `consumed` that
finally equals the input, which looks like a bug being fixed.

**What it costs.** **173 wrong**, the largest of the four. It is also the second
half of trap #5's claim that `consumed` is not a slice of the input: that entry
shows it can be *shorter*, this one is why it can be **longer**.

## 21. `:1164`'s star carries an empty output, not no output — `parse.js:1157`, `:1164`

```js
push({ type: 'star', value, output: '' });
```

The second star of a `**` that does not start a path segment stays a plain star,
and the token that records it has `output` **present and empty** — the same
shape trap #3 flags on the bare `+` arm, which sets no `output` at all. The token
is still pushed and still consumed, even though it emits nothing.

**The wrong reading.** Two of them, and they fail differently. Omitting the field
(`{ type: 'star', value }`) is the tidy one, since an empty output and no output
emit the same text. Skipping the `push` altogether is the efficient one — the
token contributes nothing to the regex, so it looks like bookkeeping.

**What it costs.** **51 wrong** for the omission, on nothing more exotic than
`a**`, which records five tokens ending `star:"*":"" maybe_slash:"":"\/?"`.
Skipping the push is worse than it scores, because `prev` is what the next
iteration's lookbehind reads: the token is invisible in the output and
load-bearing in the branch.

---

# lib/parse.js, the extglobs and the parens under them

Five more from `parse.js:287-347`, `:523-600`, `:788-808` and the five opener
arms, found by reading the branch before writing it. As above, the cost figures
are the token gate's `wrong` column with the misreading applied and nothing else
changed, so every number is corpus patterns broken.

**Two of the five score zero there, and are the reason this section also quotes a
second number.** The extglob branch is much larger than the corpus exercises: the
ReDoS analysis at `:287-347` and the paren counter's behaviour below zero are
reached by a handful of the 1,491 patterns and by none of them respectively. So
the branch was cross-checked the same way the globstar arms were — `parse(pattern,
{fastpaths: false})` against `internal/parse.Parse` over **1,152,029** enumerated
patterns, comparing `consumed`, `output`, `globstar`, `backtrack`, `negated`,
`negatedExtglob` and every token's type, value, `star`, `extglob`, `posix`,
`comma`, `output`, `outputIndex` and `tokensIndex`, *presence included*, plus the
declined patterns' prefixes under the gate's own rule. Zero mismatches. The
second figure below each entry is that corpus's, over the 159,424-pattern subset
used for the teeth runs.

The corpus is enumerated over three alphabet families — a positional enumeration
over nine extglob-flavoured alphabets, a wider one to length 7, and a targeted
one that builds `+(…)` and `!(…)` bodies from 31 fragments — and it is
regenerated rather than committed, so it is a check that can be re-run, not a
fixture. An instrumented **copy** of `lib/parse.js` — `tests/original` stays
read-only — confirms it reaches every new site rather than merely running past
them. Over the targeted family: `extglobOpen` 851,582 times, the risky rewrite at
`:560` 200,840, `buildCharClassStar` 60,340, the recursive parse at `:588`
11,000, `hasRepeatedCharPrefixOverlap` returning true 20,960. Over the positional
families, which reach the two sites the targeted one does not: the unclosed-paren
loop at `:1294` 302,083 times, and the bare `+` arm at `:1087` 159,773.

## 22. `@(` is the one opener that is not an extglob — `parse.js:1095-1103`

```js
if (value === '@') {
  if (opts.noextglob !== true && peek() === '(' && peek(2) !== '?') {
    push({ type: 'at', extglob: true, value, output: '' });   // not extglobOpen
    continue;
  }
```

Four of the five openers call `extglobOpen`. `@` pushes a token and falls
through, so the `(` after it is picked up by the *generic* paren branch at `:788`
— nothing is pushed onto `extglobs`, and `extglobClose` never runs for it. The
`)` is then the plain arm at `:805`.

**The wrong reading.** Routing `@` through `extglobOpen` like its four siblings.
`constants.extglobChars` even defines an `'@'` entry — `{ open: '(?:', close:
')' }` — which reads as proof the omission is a slip. It is not: that entry is
dead code for `parse()`.

**What it costs.** **54 wrong**, and 19,626 of 159,424. `@(a)` compiles to
`(a)` — a *capturing* group, because the generic paren branch emits the `(` and
`)` characters themselves — where `extglobOpen` would give `(?:a)`. The two match
the same strings, so nothing behavioural fails; what changes is the group index
of every capture after it, and the token stream.

## 23. `*(` is tested differently from the other four openers — `parse.js:1140`

```js
if (opts.noextglob !== true && /^\([^?]/.test(rest)) {   // "*(" — :1140
if (opts.noextglob !== true && peek() === '(' && peek(2) !== '?') {   // "+(" — :1072
```

The regex needs **two** characters. The `peek` form does not: past the end
`peek(2)` is `undefined`, which is `!== '?'`, so it is satisfied by a `(` that is
the last character of the pattern.

**The wrong reading.** Spelling all five alike, in either direction. They agree
on every input except one — a pattern ending in the opener — and that input is
two characters long.

**What it costs.** **1 wrong**, and 1,606 of 159,424. `a+(` opens an extglob and
compiles to `a\(?:` — an unclosed group that `escapeLast` then patches at
`:1294`. `a*(` does not, and compiles to `a[^/]*?\(`. Same two trailing
characters, opposite answers.

## 24. The `)` branch decrements unguarded, and reads the counter for truthiness — `parse.js:805-806`

```js
push({ type: 'paren', value, output: state.parens ? ')' : '\\)' });
decrement('parens');
```

`decrement` runs whether or not anything opened a paren, so an unmatched `)`
leaves `state.parens` at **-1**. The output above it is chosen by JavaScript
truthiness, and `-1` is truthy — so the *first* unmatched `)` emits `\)` and
every one after it emits `)`.

**The wrong reading.** Clamping the counter at zero. A negative nesting depth
reads as a bug, `state.parens > 0` reads as the same test as `state.parens ?`,
and Go invites both.

**What it costs.** **0 wrong** — the corpus never gets there — and **29,640 of
159,424**. Two live tests read the counter: this line, and the dot branch at
`:1008` (`(state.braces + state.parens) === 0`), which decides whether a `.` is a
`dot` token or plain `text`. `)a.*` records `paren:"\)" text:"a"
dot:".":"\.(?!\.{0,1}(?:\/|$))(?=.)"` — a *dot* token, and therefore the
`NO_DOT_SLASH` guard on the star behind it, only because the counter is at -1.
Clamped, it is a `text` token and the star takes `NO_DOT` instead.
`TestUnmatchedCloseParenTakesTheCounterNegative` is what holds it.

## 25. `token.inner` is not the extglob's body — `parse.js:507-509` vs `:541`

```js
const body = input.slice(token.startIndex + 2, state.index);   // :541
...
if (extglobs.length && tok.type !== 'paren') {                 // :507
  extglobs[extglobs.length - 1].inner += tok.value;
}
```

`extglobClose` computes `body` two lines before it reads `token.inner`, and the
two look like the same text. They are not. `inner` is built from *token values*,
it skips paren tokens, and — the part that bites — it accumulates into the
**innermost** open extglob, so a nested extglob's contents never reach the outer
one.

**The wrong reading.** Using `body` for the two tests at `:574`
(`inner.includes('/')`) and `:582` (`inner.includes('*')`). It is right there, it
is the same span of input, and it is what "the body of this extglob" means in
English.

**What it costs.** **0 wrong**, and **103 of 159,424**. `!(+(*))` has a `*` in
its body and none in its `inner` — the star belongs to the nested `+(`. So
upstream does *not* splice a trailing `.d` into the close, and
`!(+(*)).d` compiles to `(?=.)(?:(?!(?:(?:[^/]*?)+))[^/]*?)\.d` where the `body`
reading gives `…)\.d)[^/]*?)\.d`. The same split changes `extglobStar` on the
`/` test: `!(+(a/b))` keeps `[^/]*?` and `!(a/b)` takes the globstar body.
`TestExtglobInnerIsNotTheBody` is what holds it.

## 26. The `/^[*?]+$/` guard only applies when there is more than one branch — `parse.js:299-307`

```js
if (branches.length > 1) {
  if (branches.some(branch => branch === '') ||
      branches.some(branch => /^[*?]+$/.test(branch)) || ...) {
    return { risky: true };
  }
}
```

A body that is *only* `*` is not risky. A body that is `*` beside anything else
is.

**The wrong reading.** Hoisting the two `some` calls out of the guard — they read
as per-branch properties, and a single branch is a list of one, so the guard
looks like a redundant fast path.

**What it costs.** **4 wrong**, and 2,684 of 159,424. `+(*)` stays a real
extglob, `(?=.)(?:[^/]*?)+`; `+(*|a)` is rewritten to the literal
`\+\(\*\|a\)`. Hoisting turns the first into the second — a pattern that matched
one-or-more of anything stops being a glob at all.

## 27. `extglobOpen` snapshots `state.output` before it emits — `parse.js:528` vs `:534-535`

```js
token.output = state.output;      // :528, before anything is pushed
...
push({ type, value, output: state.output ? '' : ONE_CHAR });   // :534
push({ type: 'paren', extglob: true, value: advance(), output });  // :535
```

The snapshot is read twice at close: `token.output ? '' : ONE_CHAR` at `:546`,
and `state.output = token.output + open.output` at `:560`, which discards
everything the extglob emitted in between.

**The wrong reading.** Taking it after the pushes, or (in Go) storing the slice
rather than a copy and letting the later appends grow it. Both make the snapshot
non-empty for an extglob at the start of a pattern.

**What it costs.** **4 wrong**, and 175 of 159,424, all on the risky path.
`+(*(a)|*(b))` compiles to `(?=.)[ab]*` and `x+(*(a)|*(b))` to `x[ab]*` — the
`(?=.)` is present exactly when the snapshot was empty. Snapshot late and the
first loses its one-character guard.

**Only one of the snapshot's two readers has teeth, and the entry says so rather
than implying both do.** The other, `state.output = token.output + open.output`
at `:560`, is an *assignment* that throws away everything the extglob emitted —
the same shape as trap #18, and the same misreading (`+=`) is available. Applying
it changes **nothing**: 0 wrong, and 0 of 159,424. The reason is two lines below
it, at `:561`: the risky path sets `state.backtrack`, so the post-loop rebuild at
`:1309` discards `state.output` and reconstructs it from the tokens regardless.
The port still writes the assignment, because it is what upstream writes and
because `state.output` is read before the loop ends — by a following
`extglobOpen`'s snapshot, by the globstar arms' truncations, and by `escapeLast`
— but a no-op is not a trap, and listing one would be the mistake
`tools/mutate` exists to prevent.

---

# lib/parse.js, the bracket branch and the character class under it

Eight more from `parse.js:707-711`, `:718-758` and `:814-875`, found by reading
the branch before writing it. Each cost figure is two numbers: the token gate's
`wrong` column with the misreading applied and nothing else changed, and the same
misreading's count over a **294,118-pattern** enumerated differential.

**The branch was cross-checked against upstream directly**, on the same footing
as the globstar arms and the extglobs, because `0 wrong` on the gate is a
backstop and not a proof. `parse(pattern, {fastpaths: false})` — the exact
function `internal/parse.Parse` corresponds to — was run against the port over
**1,178,803** enumerated patterns across nine families: seven positional
enumerations (`[]^:a-`≤6, `[]!/*.\`≤5, `[]^-a/*`≤6, `[]:aph`≤7, `[]*(|)a`≤6,
`[]?{a/`≤7, `[]^!a"`≤6), one targeted family that builds class bodies from 24
fragments and all fourteen `[:name:]` classes plus five that do not resolve, and
every proper prefix of that targeted family — 65,308 truncations, which is what
reaches a class name sitting at end-of-input. Compared: `consumed`, `output`,
`globstar`, `backtrack`, `negated`, `negatedExtglob` and every token's `type`,
`value`, `star`, `extglob`, `posix`, `comma`, `output`, `outputIndex` and
`tokensIndex`, *presence included*, plus the declined patterns' prefixes under
the gate's own rule. Zero mismatches.

**820 patterns are excluded, because upstream does not return on them.** Eight
are the backslash runs [DECISIONS.md](../DECISIONS.md) §11 already records; the
other 812 are new, and this branch is what makes them reachable — see §11 for the
site. The port reports each as a `NonTerminatingError`, and each was checked
individually against node under a timeout: of a 25-pattern sample across the 812,
25 hang and 0 return.

The corpus is enumerated rather than committed, so it is a check that can be
re-run, not a fixture.

**One candidate is deliberately not listed.** `parse.js:861` is the fifth
`slice(0, -X.length)` site, the one trap #7 declined to vouch for, and it is now
built. Both readings score **0 wrong and 0 of 294,118**: `prev.value` at that
point is at least `[` plus the `]` just appended, so the degenerate `-0` case
cannot arise there at all. Listing a no-op is the mistake `tools/mutate` exists
to prevent — see the addendum to trap #27.

## 28. `prev.posix` is set two levels out from the lookup that gives it meaning — `parse.js:722` vs `:729`

```js
if (inner.includes('[')) {
  prev.posix = true;                       // :722
  if (inner.includes(':')) {
    ...
    const posix = POSIX_REGEX_SOURCE[rest];
    if (posix) { ... }                     // :729
  }
}
```

The flag is set on the *outer* test — "there is a `[` somewhere in the body" —
not on the class name resolving. `[[:foo:]]` is marked `posix: true` and nothing
is substituted.

**The wrong reading.** Moving the assignment inside `if (posix)`, where it looks
like it belongs: the flag reads as "this token is a POSIX class", and two lines
above it the parser does not yet know whether it is.

**What it costs.** **3 wrong**, and 9,852 of 294,118. The flag is not decorative
— it is read at `:847` to suppress the `^`-negation rewrite — so a token that
sets it spuriously keeps its meaning for the rest of the parse. `[[:al:]` and
`[[:constructor:]]` are both recorded `posix: true`, and the second is also why
upstream declares `POSIX_REGEX_SOURCE` with `__proto__: null`.

## 29. The `advance()` that ends a POSIX class consumes nothing — `parse.js:732`

```js
prev.value = pre + posix;
state.backtrack = true;
advance();
```

Its return value is discarded. The unit it steps over is the `]` of `:]`, and it
is appended to neither `prev.value` nor `state.consumed` — which is why
`[[:alpha:]]` records `consumed: "[[:alpha]"`, three units shorter than the
input, with the second `:` and one `]` simply gone.

**The wrong reading.** `prev.value += advance()`, or routing it through the
`consume` helper. Every other `advance()` in the file feeds something.

**What it costs.** **56 wrong**, and 27,406 of 294,118. `[[:alnum:]]` compiles to
`(?=.)[a-zA-Z0-9]]\/?` instead of `(?=.)[a-zA-Z0-9]\/?` — a stray `]` outside the
class, so the pattern stops matching what it names. Inside an extglob the same
slip is `*([[:alpha:].])` giving `(?=.)(?:[a-zA-Z].])*`.

## 30. The `/` a negated class gains is part of the token *value* — `parse.js:847-849`

```js
if (prev.posix !== true && prevValue[0] === '^' && !prevValue.includes('/')) {
  value = `/${value}`;
}
prev.value += value;
append({ value });
```

`value` is rewritten before *both* uses, so `[^a]` becomes the five-unit
`[^a/]` in the token and in `state.consumed`, not only in the emitted regex.

**The wrong reading.** Treating the `/` as a regex-level concern — appending the
bare `]` to `prev.value` and emitting `/]` — which is what "a negated class must
not match a separator" means as a sentence about the output.

**What it costs.** **18 wrong**, and 4,238 of 294,118. `**/[^abc]*` records its
bracket token with `value: "[^abc/]"`, and `[^abc]` is what the wrong reading
produces. It is also a third mechanism behind trap #5's claim that
`state.consumed` is not a slice of the input, alongside the globstar double-count
of trap #20.

## 31. A `]` straight after `[` or `[^` is a member, not the close — `parse.js:718`, `:747`

```js
if (state.brackets > 0 && (value !== ']' || prev.value === '[' || prev.value === '[^')) {
```

The body branch claims a `]` back from the closing branch when nothing has been
accumulated yet, and `:747` then escapes it. That is what makes `[]]` a single
bracket token holding `[\]]` rather than an empty class followed by stray text.

**The wrong reading.** "Scan until `]`" — the shape a character-class loop takes
in every other parser, and the one the branch's own comment ("continue until we
reach the closing bracket") describes.

**What it costs.** **4 wrong**, and 16,352 of 294,118. `[]` consumes `[\]` and
never closes, so the unclosed-bracket loop at `:1286` escapes its `[`; under the
wrong reading it consumes `[]` and closes empty. `a[]]b` and `a[]-]b` are the
corpus witnesses.

## 32. `!bos.output` is falsy for the empty string that field always holds — `parse.js:734`

```js
if (!bos.output && tokens.indexOf(prev) === 1) {
  bos.output = ONE_CHAR;
}
```

`bos` is created at `:371` as `{ type: 'bos', value: '', output: opts.prepend || '' }`
— the field is always *present*, and empty unless `opts.prepend` was passed. So
`!bos.output` is a truthiness test that fires on the default, not a test for a
missing field.

**The wrong reading.** `bos.output == nil`, the direct translation of "bos has no
output". It is never true, so the guard never fires. This is trap #2's mechanism
at a site where it is harder to see, because there the field is sometimes absent
and here it never is.

**What it costs.** **38 wrong**, and 17,394 of 294,118. `[![:alpha:]]` loses its
leading `(?=.)`: the one-character guard that stops the pattern matching the
empty string. Every POSIX class that opens a pattern is affected.

## 33. `\` inside a character class is the file's second fallthrough — `parse.js:707-711`

```js
if (state.brackets === 0) {
  push({ type: 'text', value });
  continue;
}
// no continue, and no else
```

Trap #1 records the `!` branch dropping out of its arm. This one does the same
thing for a reason that is easier to miss: the escape branch has already built a
two-unit `value`, and when a class is open it hands that value down to the body
branch rather than pushing it. No push happens inside a character class at all.

**The wrong reading.** Closing the branch with a `continue`, the way its two
neighbours close. The escaped unit then vanishes from the class.

**What it costs.** **16 wrong**, and 1,291 of 294,118. `[\d]+` consumes `[\]+`
instead of `[\d]+` — the `d` is dropped and the `\` escapes the wrong thing.
`[\[:]ab]` loses its opening bracket the same way.

## 34. `[` opens a class if a `]` appears anywhere later, matched or not — `parse.js:815`

```js
if (opts.nobracket === true || !remaining().includes(']')) {
  value = `\\${value}`;
} else {
  increment('brackets');
}
```

A plain substring search over the whole remaining input. Escaping and nesting are
not considered, so `[bar\]` opens a class on the strength of a `]` that the body
branch will go on to escape — and the class is then closed not by a `]` but by
the `escapeLast` loop at `:1288`.

**The wrong reading.** Searching for a `]` that would actually *close* the class
— skipping escaped ones, or matching nesting. It is the reading a reviewer asks
for, because the test is plainly there to answer "is this a class or a literal".

**What it costs.** **2 wrong**, and 764 of 294,118 — the smallest of the eight,
and, like trap #16, the one a reader is most likely to "fix". `[bar\]` consumes
`[bar\]` upstream and `\[bar\]` under the careful reading: one is an unterminated
character class, the other is four literal characters.

## 35. The close reads `prev.value.slice(1)`, and the `[` it drops is itself a regex special — `parse.js:846`, `:847`, `:856`

```js
const prevValue = prev.value.slice(1);
if (prev.posix !== true && prevValue[0] === '^' && ...) { ... }
...
if (opts.literalBrackets === false || utils.hasRegexChars(prevValue)) {
  continue;
}
```

Both of the closing branch's decisions are made on the class body *without its
opening bracket*. `prevValue` is also computed before `:851` appends to
`prev.value`, so it is a snapshot in JavaScript and has to be copied in Go.

**The wrong reading.** `prev.value` for either test. "Does the body contain regex
characters" is what the second one means in English, and `prev.value` is the
value of the bracket token.

**What it costs.** Two different failures. For the `^` test, **18 wrong** and
4,238 of 294,118: `prev.value[0]` is always `[`, so the negated-class `/` is
never injected and `!(*.[^a-c])` consumes `!(*.[^a-c])` where upstream consumes
`!(*.[^a-c/])`. For `hasRegexChars`, **44 wrong** and 15,171 of 294,118, and it
is worse than it looks: `[` is itself in `REGEX_SPECIAL_CHARS`, so the test is
*unconditionally true* on `prev.value` and the match-both rewrite at `:872` never
runs for any pattern. `**/[abc]*` emits `[abc]` where upstream emits
`(?:\[abc\]|[abc])`.

---

# lib/parse.js, the question-mark branch

Six more from `parse.js:1021-1047`, found by reading the branch before writing
it. Each cost figure is two numbers: the token gate's `wrong` column with the
misreading applied and nothing else changed, and the same misreading's count
over a **618,242-pattern** enumerated differential.

**Three of the six score `0 wrong`**, which is the reason this section quotes a
second number at all. The branch's two hard arms are decided by a lookahead pair
at `:1032`, and the corpus reaches them on a handful of patterns — `(?<!c)` and
`(?<=c)` shapes from `test/extglobs.js`, and nothing else. So the branch was
cross-checked against upstream directly, on the same footing as the globstar
arms, the extglobs and the brackets: `parse(pattern, {fastpaths: false})` — the
exact function `internal/parse.Parse` corresponds to — was run against the port
over 618,242 distinct enumerated patterns across three families. Nineteen
positional enumerations (`?/.a*`≤6, `(?<!=`≤6, `(?<a>`≤6, `?(|)a`≤6, `?[]\`≤5,
`?!@+a`≤5, `?*(/.a)`≤4, `?"^$a`≤5, `?()<>`≤5, `?[]a^`≤6, `?()!a`≤6, `?*/a`≤8,
`?.\/a`≤6, `?@+(a`≤6, `?|)(a`≤6, `?:[]a`≤6, `?"a/.`≤6, `?<>(a`≤6, `?/*.`≤9), one
targeted family building `(` + 38 fragments × 11 tails × 12 heads, one that
inserts `?` at every position of seventeen skeletons, and one that puts astral
and BMP non-ASCII in a `<...>` group name. Compared: `consumed`, `output`,
`globstar`, `backtrack`, `negated`, `negatedExtglob` and every token's `type`,
`value`, `star`, `extglob`, `posix`, `comma`, `output`, `outputIndex` and
`tokensIndex`, *presence included* — 611,081 accepted patterns full and 7,161
declined ones prefix-compared under the gate's own rule. Zero mismatches.

**33 patterns are excluded**, every one a run of four or more backslashes, which
is the `parse.js:689` site [DECISIONS.md](../DECISIONS.md) §11 already records.
This branch adds no new non-terminating input.

An instrumented **copy** of `lib/parse.js` — `tests/original` stays read-only —
confirms the corpus reaches the new arms rather than running past them. Over the
first family alone: the `QMARK_NO_DOT` arm 22,851 times, the plain `QMARK` arm
57,561, the paren arm at `:1028` 21,027, its first disjunct 9,873 and its second
2,480, the undefined-lookahead case below 2,197, and the unanchored case below
540.

## 36. `/[!=<:]/.test(next)` is false when there is no next character — `parse.js:1032`

```js
const next = peek();
if ((prev.value === '(' && !/[!=<:]/.test(next)) || ...) {
  output = `\\${value}`;
}
```

`peek()` past the end is `undefined`, and `RegExp.prototype.test` coerces its
argument to a string first — so the test runs against the seven characters of
`"undefined"`, none of which is in the class. The test is therefore **false**,
the negation is true, and a `?` that is the last character of the input takes the
escape.

**The wrong reading.** Requiring the character to exist: `hasNext && !isIntro(next)`,
which is what a Go `(uint16, bool)` peek invites and what "the next character is
not one of these" means as a sentence. It agrees on every input except one — a
`?` at end of input directly after a `(`.

**What it costs.** **0 wrong**, and 2,287 of 618,242. `(?` compiles to `\(\?`
upstream and `\(?` under the misreading — a literal question mark becomes a
*quantifier* making the preceding `\(` optional. The same coercion runs at
`:1055` on `peek(3)`, where the `!` branch has used it since before this helper
had a second caller; `TestLookaroundIntroOnAbsentCharacterIsFalse` is what holds
both.

## 37. `/<([!=]|\w+>)/` is a search over the whole remainder, not a test of what follows the `<` — `parse.js:1032`

```js
(next === '<' && !/<([!=]|\w+>)/.test(remaining()))
```

The guard has already established that `next` is `<`, and `remaining()` starts at
`next` — so the regexp reads as "is this a lookbehind (`<!`, `<=`) or a named
group (`<name>`)". It is not anchored, so a `<...>` **anywhere further along the
pattern** satisfies it just as well and suppresses the escape.

**The wrong reading.** Testing at position 0. Every fact in the line points that
way: the caller pinned the first character, the regexp opens with the character
that was pinned, and the alternatives describe exactly the two things that can
legally follow it.

**What it costs.** **0 wrong**, and 540 of 618,242. `(?<<!` compiles to `\(?<<!`
upstream — a bare `?` quantifier — because the `<!` at offset 1 matches; anchored
at 0 the `<` is followed by another `<`, nothing matches, and the output becomes
`\(\?<<!`. `(?<(<!` and `(?<?<!` are the same shape one character wider.

## 38. `\w` in that regexp is ASCII, because it has no `/u` flag — `parse.js:1032`

`\w` in a JavaScript regexp is `[A-Za-z0-9_]` and nothing else. It does not grow
a Unicode meaning without the `u` flag, and this regexp has none.

**The wrong reading.** `unicode.IsLetter`/`IsDigit`, or any "is this a word
character" helper that respects Unicode — the idiomatic Go spelling of `\w`, and
the one a reviewer asks for on seeing an ASCII range test.

**What it costs.** **0 wrong**, and 480 of 618,242. `(?<é>` compiles to
`\(\?<é>` upstream: `é` is not a word character, `<é>` is not a named group, so
the `?` is escaped. Under a Unicode-aware reading it is `\(?<é>` — the `?`
becomes a quantifier. `(?<e>` is unaffected, which is what makes this invisible
to any ASCII corpus. `TestAngleGroupIntroIsTheJavaScriptRegexp` pins all thirty
rows against the JavaScript regexp itself.

## 39. `QMARK_NO_DOT` is its own constant, not `NO_DOT` in front of `QMARK` — `constants.js:25`, `parse.js:1041`

```js
const QMARK = '[^/]';
const QMARK_NO_DOT = `[^.${SLASH_LITERAL}]`;
```

A `?` that opens a path segment emits a single character class excluding both the
dot and the separator. It does not emit the `NO_DOT` lookahead the *star* branch
uses (`parse.js:1263-1274`) in front of `QMARK`.

**The wrong reading.** Composing it — `noDot + qmark`, giving `(?!\.)[^/]`. The
port already spells eight of `constants.js`'s derived constants as
concatenations of their leaves, exactly as upstream builds them, so composing a
ninth is the house style rather than a slip. This one is a leaf.

**What it costs.** **73 wrong**, and 23,470 of 618,242, on inputs as short as
`/?` — recorded as `\/[^.\/]`, produced as `\/(?!\.)[^/]`. The two match the same
strings, so nothing behavioural fails; the regex source and the token both
differ, and `**/?dot` and `*/?dot` are the corpus witnesses.

## 40. The paren arm pushes a `text` token, not a `qmark` one — `parse.js:1036`

```js
push({ type: 'text', value, output });
```

Three of the branch's four arms push `qmark`. The one at `:1028`, for a `?`
directly after a paren, pushes `text` — so push()'s merge at `:512-516` folds it
into the characters that follow, and `(?:a)` records **one** token holding
`?:a` rather than a `qmark` and a `text`.

**The wrong reading.** Making it a `qmark` like its three siblings. The branch is
introduced by a comment reading "Question marks", the value being pushed is a
question mark, and the type is the one thing in the arm that is not.

**What it costs.** **11 wrong**, and 21,288 of 618,242, and none of them is a
wrong regex: the emitted text is identical either way, and only the token stream
differs. `c!(?)z` reports `token 4 (text): want "text", got "qmark"`, and
`a/**/(?:dd)/e.md` reports ten tokens against eleven — the merge that did not
happen. It is trap #3's failure mode at a different site: right output, wrong
record.

## 41. Three consecutive arms read three different things about the same token — `parse.js:1022`, `:1028`, `:1040`

```js
const isGroup = prev && prev.value === '(';   // :1022
if (prev && prev.type === 'paren') { ... }    // :1028
if (... prev.type === 'slash' || prev.type === 'bos') { ... }   // :1040
```

`isGroup` is the one that is not a type test, and the difference is not
cosmetic: a **closing** paren is also a `paren` token, with value `)`. So
`isGroup` is false after a `)`, and `)?(` opens an extglob where a type test
would have sent it to the arm below instead.

**The wrong reading.** `prev.type === 'paren'`, matching the two arms underneath
it. This is the shape trap #3 already records as not interchangeable on the bare
`+` arm at `:1077`, which spells the same test the same way for the same reason.

**What it costs.** **2 wrong**, and 553 of 618,242. `+(a)?(b)` compiles to
`(?=.)(?:a)+(?:b)?` and becomes `(?=.)(?:a)+?(b)` — the second extglob stops
being an extglob and turns into a lazy quantifier followed by a capturing group.
`?([[:alpha:].])?([[:alpha:].])?([[:alpha:].])` is the corpus's other witness.

---

# lib/parse.js, the braces

Eight more from `parse.js:22-38`, `:881-940`, `:958-969`, `:997-1006` and
`:1298-1302`, found by reading the branch before writing it. Each cost figure is
two numbers: the token gate's `wrong` column with the misreading applied and
nothing else changed, and the same misreading's count over a **1,304,643-pattern**
enumerated differential.

**Five of the eight score `0 wrong`,** which is why this section quotes a second
number at all. The corpus has 120 patterns containing a `{` and only **3**
containing a `{a..b}` range, so the arm that calls `expandRange` — the one whose
answer a *regular expression engine* decides — is exercised by three inputs, all
of which produce a valid class on the first try. So the branch was cross-checked
against upstream directly, on the same footing as the globstar arms, the
extglobs, the brackets and the qmark: `parse(pattern, {fastpaths: false})` — the
exact function `internal/parse.Parse` corresponds to — was run against the port
over **2,555,964** enumerated patterns across three kinds of family. Twenty-six
positional enumerations over brace-flavoured alphabets — `{},.a`≤7, `{},.*`≤7,
`{},a/`≤7, `{}.,a[]`≤6, `{}.,a()`≤6, `{}.,a|!`≤6, `{}.,a+?`≤6, `{}.,a"\`≤5,
`{},*(a)`≤6, `{}.,a^$`≤6, `{},.ab`≤6, `{}[]a-`≤6, `{},.a:`≤6, `{}[]:a`≤6,
`{},/**`≤6, `{}<>a-`≤5, `{},.09`≤6, `{},!./a`≤6, `{}"a,.`≤6, `{}a,.@+`≤6,
`{},.a*/`≤6, `{},[]:^`≤6, `{}()|,a`≤6, plus [DECISIONS.md](../DECISIONS.md) §14's
own three (`+*(|{)`≤7, `*(|{)?`≤7, `+(|{a)`≤6) — one targeted family that builds
brace bodies from 65 fragments in twelve skeletons, and one that inserts a brace
construct at every position of seventeen skeletons.
Compared: `consumed`, `output`, `globstar`, `backtrack`, `negated`,
`negatedExtglob` and every token's `type`, `value`, `star`, `extglob`, `posix`,
`comma`, `output`, `outputIndex` and `tokensIndex`, *presence included*. **Zero
mismatches, and zero declined patterns** — this is the branch that leaves nothing
unbuilt.

**8 patterns are excluded**, every one a run of four or more backslashes, which
is the `parse.js:689` site [DECISIONS.md](../DECISIONS.md) §11 already records.
This branch adds no new non-terminating input.

An instrumented **copy** of `lib/parse.js` — `tests/original` stays read-only —
confirms the corpus reaches the new arms rather than running past them. Over
1,915,288 patterns: the `{` arm 1,854,389 times, the unclosed-brace loop at
`:1298` 1,500,622, the no-open-brace `}` at `:900` 834,287, a real `}` close
353,767, the literal rewrite at `:925` 267,778 (taken, not merely reached), the
comma-inside-braces arm at `:963` 283,410, the `..` arm at `:1001` 55,594, and
`expandRange` 28,902 — of which **1,938 took the `catch`**.

**One candidate is deliberately not listed.** The pop loop at `:911-919` leaves
`prev` pointing at a token it has just removed from `tokens`, and resetting it to
the new last token — the obvious tidy-up — changes **nothing**: 0 wrong, and 0 of
1,304,643. The reason is structural rather than lucky. The only token pushed
after that loop is the closing `brace`, and push()'s globstar lookbehind at
`:495` exempts a `brace` whenever `state.braces > 0` — which it is, because
`decrement('braces')` runs *after* the push. So the stale `prev` is never read
for anything it could change. Listing a no-op is the mistake `tools/mutate`
exists to prevent; see the addendum to trap #27.

## 42. The `}` pop loop removes the brace token too — `parse.js:911-919`

```js
for (let i = arr.length - 1; i >= 0; i--) {
  tokens.pop();
  if (arr[i].type === 'brace') {
    break;
  }
  ...
}
```

`tokens.pop()` is unconditional and runs **before** the type test, so the
iteration that finds the opening brace has already removed it. The `break` stops
the loop from going further; it does not save the brace.

**The wrong reading.** Testing first and popping second, which is what "unwind
back to the brace" means as a sentence and what every loop that searches for a
delimiter does.

**What it costs.** **3 wrong**, and 27,960 of 1,304,643. `a/{a..c}` compiles to
`a\/([a-c]` instead of `a\/[a-c]` — the opening brace's `(` survives into the
output with nothing to close it, so the range is wrapped in an unbalanced group.
`{..}` is the two-character version: `([]` against `[]`. It is also why no
recorded token in `testdata/tokens` carries the `dots` flag or the `dots` type:
both live only on tokens this loop deletes.

## 43. The brace's delimiters are rewritten *before* the output is replayed — `parse.js:928-933`

```js
brace.value = brace.output = '\\{';
value = output = '\\}';
state.output = out;
for (const t of toks) {
  state.output += (t.output || t.value);
}
```

`toks` holds *references*, and `brace` is the first of them. So the assignment
two lines up is what the replay reads: the rebuilt output starts with `\{`, not
with the `(` the brace was emitted as.

**The wrong reading.** Replaying first and rewriting after — the order that reads
as "put the output back, then fix up the tokens", and the one a reviewer asks for
because the rewrite looks like bookkeeping for the *next* pass rather than input
to this one.

**What it costs.** **4 wrong**, and 249,853 of 1,304,643 — the largest of the
eight. `{}` compiles to `(\}` instead of `\{\}`, so a brace with no alternation
becomes a capturing group that never closes. `a {abc} b` is the corpus witness,
recorded as `a \{abc\} b`.

## 44. `outputIndex` and `tokensIndex` are taken before the push, not after — `parse.js:888-889` vs `:893`

```js
const open = {
  type: 'brace', value, output: '(',
  outputIndex: state.output.length,
  tokensIndex: state.tokens.length
};

braces.push(open);
push(open);
```

Both are snapshots of a *length*, and the push that follows changes both. So they
name the position this token is about to occupy and the output as it stood
without it — `outputIndex` points at the `(` the brace emits, not past it.

**The wrong reading.** Taking them after the push, or (in Go) computing them
inside the token literal *after* the token has been appended. Both are off by
exactly one in each field, and both read as "where is this token" rather than
"where does this token start".

**What it costs.** **97 wrong**, and 796,683 of 1,304,643. It is the only writer
of `outputIndex` in the file, 18 recorded tokens carry it, and every one of them
is `0` — a value the wrong reading cannot produce for a brace that opens a
pattern. `{` alone records `outputIndex: 0, tokensIndex: 1`. The off-by-one in
`outputIndex` also moves where `:926` truncates, so the literal rewrite keeps one
character too many.

## 45. The comma arm needs the *top of the stack*, not the brace counter — `parse.js:961-962`

```js
const brace = braces[braces.length - 1];
if (brace && stack[stack.length - 1] === 'braces') {
```

Two tests, and the second is the only read of `stack` in the whole file. A brace
must be open **and** be the innermost open construct, so a comma inside a paren
inside a brace is still a comma.

**The wrong reading.** `state.braces > 0`, which is the test every other brace
arm uses and which the first half of this line already implies. The two agree on
every pattern where nothing else is open inside the brace.

**What it costs.** **0 wrong** — the corpus has no comma nested inside a paren
inside a brace — and 13,635 of 1,304,643. `{(,` compiles to `(\(,` upstream and
`(\(|` under the wrong reading: the comma turns into an alternation bar inside a
group it does not belong to. `TestCommaInsideBracesNeedsTheStackTop` is what
holds it.

## 46. The `:932` replay is JavaScript-truthy where the `:1313` rebuild is `!= null` — `parse.js:932` vs `:1313`

```js
state.output += (t.output || t.value);                       // :932
state.output += token.output != null ? token.output : token.value;  // :1313
```

Two loops that replay the token list into `state.output`, ninety lines apart,
with **different** fallback rules. At `:932` an *empty* output falls back to the
token's value; at `:1313` it stays empty. `:932` also ignores `token.suffix`,
which `:1313` appends.

**The wrong reading.** Factoring them together, or reusing the rebuild helper for
the brace replay. They look like the same operation and the second is the more
carefully written of the two.

**What it costs.** **0 wrong**, and 1,358 of 1,304,643. `{.**}` compiles to
`\{\.(?!\.{0,1}(?:\/|$))[^/]*?*\}` — note the stray `*`, which is the second
star's *value* standing in for its empty output (trap #21's token, replayed by
the wrong loop). Under the `!= null` reading the `*` disappears and the pattern
silently starts meaning something else. It is trap #2's mechanism at a third
site.

## 47. `expandRange` decides by *compiling*, and it sorts first — `parse.js:27-35`

```js
args.sort();
const value = `[${args.join('-')}]`;
try { new RegExp(value); } catch (ex) {
  return args.map(v => utils.escapeRegex(v)).join('..');
}
return value;
```

The brace token's output is chosen by asking a regular expression engine whether
the class it just built is legal. And the sort runs first, so the obvious
out-of-order candidates never reach the `catch`: `{z..a}` sorts to `[a-z]` and
`{b..a}` to `[a-b]`.

**The wrong reading.** Two of them. Dropping the validity check, on the grounds
that a sorted two-element range cannot be out of order — it can, because the sort
is *lexicographic over strings* and the range test is over single characters, so
`{ac..b}` sorts to `["ac", "b"]` and builds `[ac-b]`, whose `c-b` is backwards.
And dropping the sort, on the grounds that the `catch` handles whatever comes
out.

**What it costs.** Always-valid: **0 wrong**, and 1,938 of 1,304,643. `{ac..b}`
compiles to the literal `ac..b` upstream and to `[ac-b]` under the misreading — a
character class the JavaScript engine would refuse to compile at all. No-sort:
**0 wrong**, and 15,607 of 1,304,643, on inputs as short as `{$..#}`, which is
`[#-\$]` upstream and `\$..#` unsorted.

The port cannot ask RE2 this question — it answers differently — so it
transcribes the acceptance predicate instead. That is a divergence in mechanism
and it has its own entry: [DECISIONS.md](../DECISIONS.md) §15.

## 48. The `..` arm tests `prev.type === 'dot'`, not `'dots'` — `parse.js:998`

```js
if (state.braces > 0 && prev.type === 'dot') {
  ...
  prev.type = 'dots';
```

The arm converts a `dot` into a `dots` and then can never fire on it again, so a
range separator is exactly two dots. A third one falls through to the tests below
and becomes a fresh `dot` token.

**The wrong reading.** Accepting `dots` as well, so a run of dots keeps
extending. It is what "`..` inside braces" suggests, and the arm's own assignment
of `prev.type = 'dots'` invites the symmetry.

**What it costs.** **0 wrong**, and 4,279 of 1,304,643. `{...` consumes `{..`
upstream — the third dot is pushed as a `dot` token and consumed — against `{.`
under the wrong reading, because the extending arm appends to `prev.value`
without going through `push`, and only `push` feeds `state.consumed`. That last
detail is one more mechanism behind trap #5, and it runs in both directions
again: `{a..c}` consumes `{a.c}`, one dot short of its input, while `{}` consumes
`{\}`, one backslash longer — the `}` arm reassigns `value` to `\}` before
pushing it.

## 49. The unclosed-brace loop escapes a `{` the emitter wrote, never the one the user did — `parse.js:1298-1302`

```js
while (state.braces > 0) {
  state.output = utils.escapeLast(state.output, '{');
  decrement('braces');
}
```

`escapeLast` searches `state.output`, and an open brace's output is `(`, not `{`.
So the `{` this loop finds is never the user's — it is whichever `{` the *port's
own emitted regex* happens to contain, and the nearest candidate is the `{0,1}`
in `NO_DOT_SLASH`.

**The wrong reading.** Two, and they fail the same way. Escaping the `(` instead,
on the reasonable belief that the loop exists to neutralise the group the brace
opened — which is what the bracket and paren loops beside it do, because for
those two the emitted character and the source character are the same. Or
dropping the loop as a no-op, since there is no `{` left in the output to find.

**What it costs.** **0 wrong**, and 9,273 of 1,304,643. `.*{` compiles to
`\.(?!\.\{0,1}(?:\/|$))(?=.)[^/]*?(` — upstream has escaped the quantifier inside
its own dot guard, turning `{0,1}` into four literal characters and changing what
the pattern matches. Dropping the loop leaves `{0,1}` a quantifier, which is the
regex a reader would call correct. Reproducing it is the point: this is
upstream's answer, and the port's job is to give the same one.

---

## 50. `fastpaths` measures the input *before* `REPLACEMENTS`, `parse()` after — `parse.js:1333` vs `:361`

The two parsers apply the same two steps in opposite orders.

```js
// parse.fastpaths, :1332-1338
const max = typeof opts.maxLength === 'number' ? Math.min(MAX_LENGTH, opts.maxLength) : MAX_LENGTH;
const len = input.length;                      // :1333  measure
if (len > max) { throw new SyntaxError(...); }
input = REPLACEMENTS[input] || input;          // :1338  then replace

// parse, :361-367
input = REPLACEMENTS[input] || input;          // :361   replace
...
let len = input.length;                        // :366   then measure
if (len > max) { throw new SyntaxError(...); }
```

`REPLACEMENTS` (`constants.js:105`) maps `'***'` to `'*'` and `'**/**'` and
`'**/**/**'` to `'**'`, so for those three inputs the two length checks are
counting different strings.

**The wrong reading.** That the two share a length guard, so the port can hoist
one `maxLength` check in front of both parsers — or, worse, apply
`REPLACEMENTS` once at the entry point where both can see it.

**What it costs.** The throw itself, on exactly the inputs where it is easiest to
believe there is no difference. Measured:

```
parse.fastpaths('***', {maxLength: 2})   SyntaxError: Input length: 3, exceeds maximum allowed length: 2
parse('***', {maxLength: 2})             returns, output (?!\.)(?=.)[^/]*?\/?
```

And because `fastpaths` is called bare at `picomatch.js:313`, that throw escapes
`makeRe` from the *fast path*, not from `parse()`. `testdata/emit` keeps
`fastpathThrow` and `scannerThrow` as separate fields so the asymmetry is
recordable — but **no recorded case exercises it**: the corpus's only
`fastpathThrow` is a 65,537-unit pattern under default options, which is over
`MAX_LENGTH` on either ordering, so the scanner throws beside it. The witness
above is a chosen input, and that is the point — this trap is invisible to every
fixture set in the repo.

## 51. `nodot` and `star` are defined differently in the two parsers — `parse.js:1353`, `:1357` vs `:399`, `:401`

Same two names, driven by the same two options, different values.

```js
// parse.fastpaths, :1353 and :1357
const nodot = opts.dot ? NO_DOTS : NO_DOT;
let star = opts.bash === true ? '.*?' : STAR;

// parse, :399 and :401
const nodot = opts.dot ? '' : NO_DOT;
let star = opts.bash === true ? globstar(opts) : STAR;
```

Under `dot` the fast path emits `NO_DOTS` where the scanner emits **nothing**;
under `bash` the fast path emits the literal `.*?` where the scanner emits a
whole `globstar(opts)` group. The `!opts.dot` arms agree, which is exactly why
this survives casual reading: under default options the two definitions are
identical.

**The wrong reading.** Factoring the two parsers' preamble into one helper —
the natural move, since much of it *is* identical, including the whole
`constants.globChars(opts.windows)` destructuring — or writing the fastpaths
pass later and copying the scanner's definitions across because they are already
there and already tested.

**What it costs.** Every `dot`- or `bash`-bearing record on the fastpath layer:
**225 of the 728** eligible pairs in `testdata/emit` carry one of the two keys
(157 `dot`, 68 `bash`), and 29 of the 79 pairs that actually take the top path
do. It costs nothing at all under default options, so no gate that runs today
would report it.

---

# lib/picomatch.js, the compile layer

## 52. `source` is not `^(?:output)$` — V8 escapes `/` on the way out — `picomatch.js:273` vs `RegExp.prototype.source`

`compileRe` builds the string `^(?:${state.output})$` and hands it to
`toRegex`. Reading the recorded `source` back off the compiled RegExp does not
return that string:

```js
const pm = require('./tests/original');
pm.makeRe('foo[/]bar', {}, true)      //=> 'foo(?:\\[/\\]|[/])bar'
pm.makeRe('foo[/]bar', {}).source     //=> '^(?:foo(?:\\[\\/\\]|[/])bar)$'
new RegExp('a/b').source              //=> 'a\\/b'
```

The `/` inside `\[/\]` came back as `\/`. This is not picomatch: ECMAScript
specifies `RegExp.prototype.source` to escape every `/` that is not already
escaped and not inside a character class, so the source can be re-read as a
`/…/` literal. The `[/]` two characters later is *inside* a class and is left
alone, which is why the two `/` in one pattern serialise differently.

**The wrong reading.** Implementing the compile layer as
`"^(?:" + output + ")$"` from the doc comment, then diffing it against the
recorded `source` and concluding the emitter is wrong. Or worse, "fixing" the
emitter to produce `\/` so the diff goes away, which corrupts `output` — the
field the scanner is actually gated on — to satisfy a serialisation artifact.

**What it costs.** 5 of the 2,028 compiled records in `testdata/emit`, all of
them patterns containing `[/]`: `foo[/]bar` (×3 option sets), `foo[/]bar[/]`,
`foo[/]bar[/]baz`. `TestEmitParity` is fatal on any `Wrong`, so these become 5
false disagreements the day the compile layer's blocker is lifted.

Re-check — prints `{ ok: 2020, fallback: 3, slashArtifact: 5 }`:

```bash
node -e "const r=require('fs').readFileSync('testdata/emit/cases.jsonl','utf8').split('\n').filter(Boolean).map(JSON.parse);let ok=0,fb=0,slash=0;for(const x of r){if(x.source===undefined)continue;const w='^(?:'+x.output+')\$';if(x.source===w||x.source==='^(?!'+w+').*\$'){ok++;continue}if(x.source==='\$^'){fb++;continue}slash++}console.log({ok,fallback:fb,slashArtifact:slash})"
```

---

## 53. An uncompilable source is not an error — it is `/$^/` — `picomatch.js:344-347`

```js
picomatch.toRegex = (source, options) => {
  try {
    const opts = options || {};
    return new RegExp(source, opts.flags || (opts.nocase ? 'i' : ''));
  } catch (err) {
    if (options && options.debug === true) throw err;
    return /$^/;
  }
};
```

When the emitter produces something the RegExp constructor rejects, picomatch
does not throw and does not report it. It returns `/$^/` — `$` then `^`, a
pattern that matches nothing — and the caller gets a matcher that answers
`false` to every input. The throw is reachable only under `opts.debug === true`,
which no corpus record sets.

**The wrong reading.** Treating an uncompilable output as an error path, so the
port returns an error where upstream returns a total function that always says
no. Every one of those inputs is a recorded `false`, not a recorded throw, so
the two are distinguishable by fixture and the error is scored as a failure.

The subtler half: because the failure is swallowed, an emitter bug that produces
invalid regex syntax is *invisible* in behaviour except as unexplained
non-matching. This is the one place in the pipeline where being wrong looks
exactly like matching nothing.

**What it costs.** 3 of the 2,028 compiled records, whose recorded `source` is
the literal `$^`: `a\\(b` under defaults, `[[:alpha:]\]` under
`{posix, regex, strictSlashes}`, and the 65,504-unit `[!(\\…` pattern. Same
re-check as trap #52 — the `fallback: 3` column.

## 54. `WINDOWS_CHARS` is four leaves and twelve derivations, and one leaf looks derivable — `constants.js:52-66`

```js
const WIN_SLASH = '\\/';
const WIN_NO_SLASH = `[^${WIN_SLASH}]`;

const QMARK_NO_DOT = `[^.${SLASH_LITERAL}]`;      // POSIX, :25

const WINDOWS_CHARS = {
  ...POSIX_CHARS,
  SLASH_LITERAL: `[${WIN_SLASH}]`,
  QMARK_NO_DOT:  `[^.${WIN_SLASH}]`,               // :61 — WIN_SLASH, not SLASH_LITERAL
  ...
};
```

`WINDOWS_CHARS` reads as twelve independent overrides. It is not: four of them
are leaves — `SLASH_LITERAL`, `QMARK`, `QMARK_NO_DOT`, `SEP` — and the other
eight fall straight out of the same expressions `constants.js:18-26` uses for the
POSIX set, with the new leaves substituted. Re-deriving them by hand is eight
chances to transcribe a string wrong for no benefit.

**The wrong reading.** Concluding from that structure that `QMARK_NO_DOT` is a
derivation too. Its POSIX spelling *is* one — `[^.${SLASH_LITERAL}]` — and it is
the only key whose two definitions read the same expression against different
variables. Substituting the Windows `SLASH_LITERAL` into the POSIX expression
gives

```
[^.[\/]]      instead of      [^.\/]
```

a character class that opens another character class. It is the wrong regex, and
in JavaScript it is not even a syntax error: `[^.[\/]]` compiles, matches one
character that is not `.`, `[`, `\` or `/`, and then requires a literal `]`. So
the mistake survives compilation and surfaces as a handful of patterns quietly
not matching.

The cause is that `SLASH_LITERAL` changes *shape* between platforms — an escaped
character on POSIX, a whole bracketed class on Windows — while `WIN_SLASH` is the
class *body*. Any POSIX expression that puts `SLASH_LITERAL` inside brackets is
therefore not the Windows expression. `QMARK_NO_DOT` is the only one that does,
which is why `QMARK` (`[^/]`, spelled with a bare `/` rather than
`${SLASH_LITERAL}`) is a leaf as well and not an oversight.

**What it costs.** Small enough to be missed, which is the point: 4 of the 2,038
recorded pairs contain `QMARK_NO_DOT` in a Windows output at all, so the emitter
gate reports the regression as `wrong=4` and a headline that moves by 0.04
points. `internal/parse.TestQmarkNoDotIsALeaf` fails on it by name instead.

**Re-check.** Save this **in the repo root** — `require` resolves relative to the
script, not the shell — and run `node <file>`. It has to be a file for the reason
trap #52 gives: Git Bash on Windows eats the backslash runs in a pasted one-liner
and every Windows leaf silently comes back as its POSIX value, which makes the
check pass against itself. It re-derives both tables from the four leaves and
prints `exact on all 16` twice.

```js
const C = require('./tests/original/lib/constants.js');
const derive = (SLASH_LITERAL, QMARK, SEP, QMARK_NO_DOT) => {
  const DOT_LITERAL = '\\.';
  const END_ANCHOR = `(?:${SLASH_LITERAL}|$)`;
  const START_ANCHOR = `(?:^|${SLASH_LITERAL})`;
  const DOTS_SLASH = `${DOT_LITERAL}{1,2}${END_ANCHOR}`;
  return {
    DOT_LITERAL, PLUS_LITERAL: '\\+', QMARK_LITERAL: '\\?', SLASH_LITERAL,
    ONE_CHAR: '(?=.)', QMARK, END_ANCHOR, DOTS_SLASH,
    NO_DOT: `(?!${DOT_LITERAL})`,
    NO_DOTS: `(?!${START_ANCHOR}${DOTS_SLASH})`,
    NO_DOT_SLASH: `(?!${DOT_LITERAL}{0,1}${END_ANCHOR})`,
    NO_DOTS_SLASH: `(?!${DOTS_SLASH})`,
    QMARK_NO_DOT, STAR: `${QMARK}*?`, START_ANCHOR, SEP
  };
};

for (const [win, got] of [
  [false, derive('\\/', '[^/]', '/', '[^.\\/]')],
  [true,  derive('[\\\\/]', '[^\\\\/]', '\\', '[^.\\\\/]')]
]) {
  const want = C.globChars(win);
  const bad = Object.keys(want).filter(k => got[k] !== want[k]);
  console.log(win ? 'win  ' : 'posix',
    bad.length ? 'MISMATCH ' + bad.join(',') : 'exact on all ' + Object.keys(want).length);
}
```

---

## 55. `!` closes the inline fast path without opening the top one — `parse.js:606` vs `picomatch.js:312`

Two guards decide which of upstream's three parsers `makeRe` reaches, and they
are written a line apart in the same call:

```js
// picomatch.js:312 — the top path
if (options.fastpaths !== false && (input[0] === '.' || input[0] === '*'))

// parse.js:606 — the inline path
if (opts.fastpaths !== false && !/(^[*!]|[/()[\]{}"])/.test(input))
```

They look like one eligibility test spelled twice. They are not, and the
character that separates them is `!`. A leading `*` shuts the inline door *and*
opens the top one; a leading `!` shuts the inline door and opens nothing. So a
negated pattern runs neither fast path, reaches the full scanner, and its
recorded `path` is `none` — every negated pattern in the corpus, not by
coincidence but by this asymmetry.

The second reading inside the same regexp is a trap on its own. The `^` binds to
the `[*!]` alternative only, not to the whole alternation. Simplifying it to a
single character-set test over the string — the obvious tidy-up — makes `a*b`
ineligible when upstream takes the inline path for it.

**The wrong reading.** Treating "not fast-path eligible" as a single predicate
and concluding `!abc` behaves like `*.js`. It costs nothing at the `path` field
directly, because both readings happen to decline `*.js`; it costs the 59
negated class-A records, which a merged predicate hands to a `parse.fastpaths`
that never ran.

**The one-way answer.** The deeper point is that neither guard settles the path
on its own. `parse.fastpaths` is *called* at `picomatch.js:313` and its result
tested at `:316`, so an eligible pattern whose output is falsy still falls
through — 382 corpus patterns are eligible for the top path and 25 take it.
Only the negative direction is decidable without running it: when neither guard
opens, nothing ran, so there is nothing to have returned. `internal/compile`'s
`PathFullScanner` answers in that direction alone, which is why the compile
layer scores 1,134 of 2,028 records rather than all of them.

Re-check — prints `{ classA: 1134, allRecordNone: true, negatedInClassA: 59 }`:

```bash
node -e "const r=require('fs').readFileSync('testdata/emit/cases.jsonl','utf8').trim().split(String.fromCharCode(10)).map(JSON.parse);const IN=/(^[*!]|[/()[\]{}\"])/;const A=r.filter(x=>x.source!==undefined&&!(x.pattern[0]==='.'||x.pattern[0]==='*')&&IN.test(x.pattern));console.log({classA:A.length,allRecordNone:A.every(x=>x.path==='none'),negatedInClassA:A.filter(x=>x.negated===true).length})"
```

---

## Related

- [DECISIONS.md](../DECISIONS.md) §8 — why the scanner indexes UTF-16 code units.
  Arguably the largest trap of all, but it is a deliberate divergence in
  representation rather than a misreading, so it is recorded as a decision.
- [DECISIONS.md](../DECISIONS.md) §9 — the unbuilt/wrong split that backstops
  this list, and why a declined parse still returns its tokens so the branches
  that did run are scored.
- [DECISIONS.md](../DECISIONS.md) §12 — the globstar arms rewrite two tokens
  back, one deeper than §9's exemption reaches, so the scanner declines `**` a
  character before upstream's branch for it rather than emitting a prefix it
  knows is wrong.
- [DECISIONS.md](../DECISIONS.md) §14 — `extglobClose`'s risky path rewrites
  arbitrarily far back and does not decide until the closing paren, which is one
  form further than §12's, so a declined parse inside a `+(` hands back nothing
  from that extglob onwards. Spent: the brace branch removed the last construct
  that could decline inside one.
- [DECISIONS.md](../DECISIONS.md) §15 — `expandRange` decides what a `{a..b}`
  compiles to by handing a character class to the RegExp constructor, so the
  answer is whatever the *host engine* accepts. Go's RE2 answers a different
  question, so the port transcribes the acceptance predicate instead of calling
  a regex engine. Trap #47 is the same site read as a misreading rather than as
  a divergence.
- [DECISIONS.md](../DECISIONS.md) §11 — upstream's `parse()` does not terminate
  on every input. The faithful transcription of `eos()` inherits the hang, which
  is the one case where matching upstream exactly is the wrong answer.
- [tools/mutate/README.md](../tools/mutate/README.md) — the complementary
  question: not "what is easy to transcribe wrongly" but "what would no fixture
  notice if you did".
