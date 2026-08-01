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

module.exports = {
  ROOT, UPSTREAM_DIR, PARSE_JS, CASES,
  upstream, parseModule,
  records, patternOf, verdictOf, patterns, portableWithPattern,
};
