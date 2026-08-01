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
  {
    name: 'no-fastpaths',
    why: 'Never take the inline fast path. It is not just a shortcut: it changes the compiled ' +
         'output, adding trailing-slash leniency the full scanner does not.',
    edits: [['lib/picomatch.js',
      "if (options.fastpaths !== false && (input[0] === '.' || input[0] === '*')) {",
      "if (false && (input[0] === '.' || input[0] === '*')) {"]],
    witnesses: [['*.js', 'a.js/'], ['.*', '.x/']],
    expected: 'survives'
  }
];

module.exports = { MUTATIONS };
