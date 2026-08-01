'use strict';

/**
 * Mutations of upstream picomatch, each one a choice a Go port would plausibly
 * make. The harness applies them to a scratch copy and replays every fixture.
 *
 * Fields:
 *   name       stable identifier; the baseline is keyed on it
 *   why        the porting mistake this stands in for
 *   edits      [file, find, replace] applied to a copy of tests/original
 *   witnesses  [pattern, input, options] triples used to PROVE the mutation
 *              changes behaviour. Without these a no-op mutation "survives"
 *              and is mistaken for a coverage hole -- which happened: making
 *              `*` greedy instead of lazy survives every fixture, but only
 *              because greedy and lazy accept the same language in an anchored
 *              pattern. It measured nothing and is not in this list.
 *   expected   'killed'   the fixtures detect it; if it ever survives, coverage
 *                         has regressed and the harness fails
 *              'survives' the fixtures cannot detect it; a documented hole.
 *                         If it starts being killed, coverage improved -- the
 *                         harness says so and the baseline should be updated.
 */

const MUTATIONS = [
  // --- controls: these must stay killed, or the harness is measuring nothing -
  {
    name: 'control-qmark-dot-guard',
    why: 'CONTROL. Drop the leading-dot guard from `?`.',
    edits: [['lib/constants.js', '`[^.${SLASH_LITERAL}]`', '`[^${SLASH_LITERAL}]`']],
    // A single dot. `?` against ".a" cannot discriminate: the lengths differ, so
    // both readings answer false and the witness proves nothing.
    witnesses: [['?', '.']],
    expected: 'killed'
  },
  {
    name: 'control-literal-equality-shortcut',
    why: 'CONTROL. Drop the input===glob fast path at lib/picomatch.js:139 and always consult the regex.',
    edits: [
      ['lib/picomatch.js', 'let match = input === glob;', 'let match = false;'],
      ['lib/picomatch.js', 'match = output === glob;', 'match = false;']
    ],
    witnesses: [['\\', '\\'], ['[', '[']],
    expected: 'killed'
  },

  // --- the character domain: five holes ------------------------------------
  {
    name: 'runes-not-code-units',
    why: 'Walk the string by code point, not UTF-16 code unit — Go `for range` and []rune.',
    edits: [['lib/picomatch.js',
      "return new RegExp(source, opts.flags || (opts.nocase ? 'i' : ''));",
      "{ const __f = opts.flags || (opts.nocase ? 'i' : '');" +
      " try { return new RegExp(source, __f + 'u'); }" +
      " catch (e) { return new RegExp(source, __f); } }"]],
    witnesses: [['?', '\u{1F600}'], ['??', '\u{1F600}']],
    expected: 'survives'
  },
  {
    name: 'unicode-case-folding',
    why: 'nocase via full Unicode folding (Go `(?i)` / unicode.ToLower) rather than JS non-unicode Canonicalize.',
    edits: [['lib/picomatch.js',
      "return new RegExp(source, opts.flags || (opts.nocase ? 'i' : ''));",
      "{ const __f = opts.flags || (opts.nocase ? 'i' : '');" +
      " if (opts.nocase) { try { return new RegExp(source, __f + 'u'); } catch (e) {} }" +
      " return new RegExp(source, __f); }"]],
    witnesses: [['k', 'K', { nocase: true }], ['s', 'ſ', { nocase: true }]],
    expected: 'survives'
  },
  {
    name: 'globstar-crosses-newline',
    why: 'Globstar body as "any character". JS `.` excludes \\n \\r U+2028 U+2029; Go has no such rule.',
    edits: [['lib/parse.js', 'DOT_LITERAL}).)*?)', 'DOT_LITERAL})[^])*?)']],
    witnesses: [['a/**/b', 'a/x\ny/b'], ['a/**/b', 'a/x y/b']],
    expected: 'survives'
  },
  {
    name: 'maxlength-in-code-points',
    why: 'Length cap counted in runes. picomatch counts UTF-16 units; Go len() counts bytes and ' +
         'len([]rune()) counts code points — three different answers for one string.',
    edits: [['lib/parse.js', 'let len = input.length;', 'let len = [...input].length;']],
    witnesses: [['\u{1F600}'.repeat(300) + '*', 'x', { maxLength: 400 }]],
    expected: 'survives'
  },
  // picomatch has TWO fast paths, at different sites, and they are separate
  // porting decisions. An earlier revision had one mutation named for the inline
  // path whose edit disabled the top-level one, so the inline path was never
  // measured at all. They are split below and each names the site it edits.
  {
    name: 'no-top-fastpaths',
    why: 'Skip parse.fastpaths() — the whitelist at lib/parse.js:1330 that makeRe consults ' +
         'when a pattern starts "." or "*" (lib/picomatch.js:312). Not a shortcut: it appends ' +
         'an optional trailing slash unless strictSlashes, and 18 of its 25 corpus patterns ' +
         'also compile structurally differently, handling the globstar prefix as (?:X\\/)? ' +
         'where the scanner emits (?:^|\\/|X\\/).',
    edits: [['lib/picomatch.js',
      "if (options.fastpaths !== false && (input[0] === '.' || input[0] === '*')) {",
      "if (false && (input[0] === '.' || input[0] === '*')) {"]],
    witnesses: [['*.js', 'a.js/'], ['**/*.md', 'm.md/']],
    expected: 'survives'
  },
  {
    name: 'no-inline-fastpath',
    why: 'Skip the inline fast path at lib/parse.js:606 — the early return inside parse() for ' +
         'patterns with no /()[]{}" that do not start * or !. It is the STRICTER of the two: ' +
         'on 28 corpus patterns the full scanner appends a trailing-slash allowance that this ' +
         'path does not, so skipping it makes a port more lenient, not less.',
    edits: [['lib/parse.js',
      'if (opts.fastpaths !== false && !/(^[*!]|[/()[\\]{}"])/.test(input)) {',
      'if (false && !/(^[*!]|[/()[\\]{}"])/.test(input)) {']],
    // Both witnesses are inline-eligible and NOT top-eligible, so they isolate
    // this site; both land on the trailing-slash difference, which is the whole
    // observable delta here. `a.js`/`a.js/` looks like the obvious witness and
    // is not one — it answers false either way.
    witnesses: [['a*', 'aa/'], ['?*', 'ab/']],
    expected: 'survives'
  }
];

module.exports = { MUTATIONS };
