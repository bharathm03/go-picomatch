'use strict';

/**
 * Shared corpus access for tools/probes/.
 *
 * The probes were written against a flat research corpus whose records were
 * `{pattern, input, expected}`. `testdata/original/cases.jsonl` is richer: it
 * records the actual call that upstream made, so WHERE THE PATTERN LIVES
 * DEPENDS ON THE API. Getting that wrong does not throw -- it silently yields a
 * corpus of inputs-treated-as-patterns, which every probe would then report on
 * confidently. Hence one definition here rather than four copies.
 */

const fs = require('fs');
const path = require('path');

const ROOT = path.resolve(__dirname, '..', '..', '..');
const UPSTREAM_DIR = path.join(ROOT, 'tests', 'original');
const PARSE_JS = path.join(UPSTREAM_DIR, 'lib', 'parse.js');
const CASES = path.join(ROOT, 'testdata', 'original', 'cases.jsonl');

/** The vendored upstream picomatch. `make verify-original` is the receipt that it is unmodified. */
const upstream = () => require(UPSTREAM_DIR);

/** `lib/parse` directly, for probes that need the raw parser rather than the facade. */
const parseModule = () => require(path.join(UPSTREAM_DIR, 'lib', 'parse'));

const records = () =>
  fs.readFileSync(CASES, 'utf8').split('\n').filter(Boolean).map(JSON.parse);

/**
 * The glob pattern a record exercised, or null if it did not have one.
 *
 * Three shapes, and the second is the trap:
 *
 *   construct != null     a matcher was built first -- pattern is construct[0]
 *   api === 'isMatch'     picomatch.isMatch(STR, PATTERN) takes the input
 *                         FIRST, so the pattern is args[1], not args[0]
 *   otherwise             args[0]
 *
 * `lib/utils` records (basename) take a path, not a pattern, and are excluded.
 */
const patternOf = r => {
  if (r.module === 'lib/utils') return null;
  const p = Array.isArray(r.construct) ? r.construct[0]
    : r.api === 'isMatch' ? r.args[1]
      : r.args[0];
  return typeof p === 'string' ? p : null;
};

/**
 * The boolean match verdict a record asserts, or null when it asserts something
 * else (a regex, a parse state, a scan result). Used to tell whether the corpus
 * ever proves BOTH sides of a pattern's boundary.
 */
const verdictOf = r => {
  if (r.error) return null;
  if (typeof r.result === 'boolean') return r.result;
  if (r.result && typeof r.result.isMatch === 'boolean') return r.result.isMatch;
  return null;
};

/**
 * Distinct patterns over the replayable records.
 *
 * Deduped across `platform`: posix and windows records carry the same pattern
 * strings, and every probe here runs the parser under default options, so
 * keeping both would double the work for identical results.
 */
const patterns = (recs = records().filter(r => r.portable)) =>
  [...new Set(recs.map(patternOf).filter(p => p !== null))];

/** The record subset the probes report against: replayable, and pattern-bearing. */
const portableWithPattern = () =>
  records().filter(r => r.portable && patternOf(r) !== null);

/**
 * Which of picomatch's three parsers `makeRe` actually used, under DEFAULT
 * options. One of 'top' | 'inline' | 'none'.
 *
 *   'top'    parse.fastpaths() returned output, so parse() never ran.
 *            ELIGIBILITY IS NOT USE: picomatch.js:311-317 calls fastpaths for
 *            every pattern starting '.' or '*' but falls through to the full
 *            scanner whenever the result is falsy. On this corpus 382 patterns
 *            are eligible and 25 actually take it.
 *   'inline' parse() returned from the fast path at parse.js:606, before the
 *            scanner loop. Observable because `index` is still its initial -1;
 *            that block's two `return state` sites are the only exits from
 *            parse() that skip advance().
 *   'none'   the full scanner ran.
 *
 * MEASURED, NOT TRANSCRIBED, and that is not pedantry: the condition at :606
 * tests `input` after `utils.removePrefix` rewrote it at :430, so './foo'
 * contains a '/' and looks ineligible while the scanner actually sees 'foo' and
 * takes the fast path. Five corpus patterns differ between the two readings.
 */
const fastpathOf = p => {
  const parse = parseModule();
  try { if (parse.fastpaths(p, {})) return 'top'; } catch (e) { /* fall through, as makeRe does */ }
  try { if (parse(p, {}).index === -1) return 'inline'; } catch (e) { /* unparseable: not a fast path */ }
  return 'none';
};

/**
 * Whether the path taken produced different regex source than the full scanner
 * would have.
 *
 * Compared via makeRe(p, opts, returnOutput=true), which is state.output before
 * compileRe wraps it — comparing the compiled `.source` instead double-counts,
 * because compileRe adds another ^(?:...)$ layer.
 *
 * THE INLINE SIDE MUST BE UNWRAPPED FIRST, and an earlier revision of this
 * function did not do it. The inline fast path calls `utils.wrapOutput` inside
 * parse() (lib/parse.js:653), so its state.output is ALREADY `^(?:X)$` while the
 * scanner's is a bare `X` that compileRe anchors later. Comparing them raw marks
 * every inline pattern divergent no matter what it produced: it reported 172
 * patterns, of which 105 are byte-identical once unwrapped. `.dotfile` compiled
 * to `^(?:\.dotfile)$` against `\.dotfile` and was scored a divergence.
 *
 * The corrected count is 34. Note the leniency runs BOTH ways — the top fast
 * path adds `\/?` on 5 patterns, and the scanner adds it on 28 the inline path
 * leaves strict — so "the fast paths are more lenient" is not a safe shorthand.
 */
const fastpathDiverges = p => {
  const pm = upstream();
  const unwrap = s => {
    const m = /^\^\(\?:([\s\S]*)\)\$$/.exec(s);
    return m ? m[1] : s;
  };
  try {
    const fast = pm.makeRe(p, {}, true);
    const slow = pm.makeRe(p, { fastpaths: false }, true);
    return (fastpathOf(p) === 'inline' ? unwrap(fast) : fast) !== slow;
  } catch (e) {
    return false;
  }
};

module.exports = {
  ROOT, UPSTREAM_DIR, PARSE_JS, CASES,
  upstream, parseModule,
  records, patternOf, verdictOf, patterns, portableWithPattern,
  fastpathOf, fastpathDiverges,
};
