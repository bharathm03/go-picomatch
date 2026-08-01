# tools/charaxis — the character-domain fixtures

```bash
make charaxis        # regenerate testdata/charaxis/
```

## Why a second fixture set exists

`testdata/original` is recorded from picomatch's own unmodified suite. That is
what makes the parity number worth quoting — nobody chose its contents — and it
is also its ceiling. `tools/mutate/run.js` measured that ceiling: five plausible
Go-port mistakes survive all 18,792 comparable fixtures.

The suite explores 91 distinct code points, U+0009 to U+30EB, four of them
non-ASCII. It is exhaustive about *structure* and thin about *alphabet*, which is
not a flaw — a brace-expansion test has no reason to contain an astral character.
It just means the parity number cannot speak for that axis.

## What each axis is for

Every axis names the mutation in `tools/mutate/mutations.js` it was written to
kill. The mapping is design intent, not a measured 1:1 correspondence — several
axes happen to kill several mutations, and removing one axis does not
necessarily let its mutation through. What `make mutate` checks is the property
that matters: that **no** mutation escapes every fixture set.

| Axis | Kills | The distinction |
| --- | --- | --- |
| `utf16-code-units` | `runes-not-code-units` | `?` matches one UTF-16 unit, so `"😀"` is two |
| `js-dot-exclusions` | `globstar-crosses-newline` | JS `.` excludes exactly `\n \r U+2028 U+2029` |
| `case-folding` | `unicode-case-folding` | JS non-unicode `Canonicalize` will not fold U+212A onto `k` |
| `maxlength-units` | `maxlength-in-code-points` | the cap counts UTF-16 units, not bytes or runes |
| `fastpaths` | `no-fastpaths` | the inline path adds trailing-slash leniency |
| `dot-guard-lexical` | *(none)* | the dot guard comes from the pattern's left neighbour, not the match position |

`dot-guard-lexical` kills no mutation because upstream does cover it — with
exactly one test, `a{a,b/}*.txt` vs `ab/.txt` in `test/braces.js`, recorded twice.
Two records out of 18,792 is not a margin worth relying on.

## Why this is not grading its own homework

The inputs are chosen. That is the point: upstream never chose them, which is why
the hole exists.

The **answers** are not chosen. Every expected value is produced by running
upstream picomatch, exactly as `tools/extract` does. Nothing in `generate.js`
states what picomatch ought to do — if picomatch's behaviour changed, this
directory would be regenerated and the port would have to follow.

Three things keep it that way:

- generation is deterministic, and CI regenerates the file and fails on any diff,
  so a hand-edited expectation cannot survive;
- the set lives in its own directory with its own report and its own floor
  (`PICOMATCH_CHARAXIS_MIN`), so it never inflates the headline parity figure;
- `make mutate` reports both sets in separate columns, so it stays visible which
  number came from upstream's own tests and which from these.
