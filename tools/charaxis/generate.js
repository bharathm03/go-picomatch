#!/usr/bin/env node
'use strict';

/**
 * Records a second fixture set covering the character domain.
 *
 * WHY THIS EXISTS
 *
 * testdata/original is recorded from picomatch's own unmodified suite, and it is
 * the honest measure of parity precisely because nobody chose its contents. But
 * that also caps what it can prove: `node tools/mutate/run.js` shows five
 * plausible Go-port mistakes that no fixture in it detects. The suite explores
 * 91 distinct code points, U+0009 to U+30EB, four of them non-ASCII. It is a
 * structural corpus, not an alphabetic one.
 *
 * These cases close that gap along one axis only: which characters exist and how
 * they are counted.
 *
 * WHY IT IS NOT GRADING ITS OWN HOMEWORK
 *
 * The inputs here are chosen — that is the whole point, since the upstream suite
 * never chose them. The ANSWERS are not: every expected value is recorded by
 * running upstream picomatch, exactly as tools/extract does. Nothing in this
 * file states what picomatch ought to do. If picomatch's behaviour changes, this
 * file does not need editing; the fixtures are regenerated and the port has to
 * follow.
 *
 * The two sets are kept in separate directories and reported separately, so the
 * headline parity number stays derived purely from upstream's own tests.
 *
 *   node tools/charaxis/generate.js
 */

const fs = require('fs');
const path = require('path');

const REPO = path.resolve(__dirname, '..', '..');
const UPSTREAM = path.join(REPO, 'tests', 'original');
const OUT_DIR = path.join(REPO, 'testdata', 'charaxis');

// --- the probes -------------------------------------------------------------
// Each axis names the mutation in tools/mutate/mutations.js it is here to kill.

const SMILE = '\u{1F600}';   // U+1F600, two UTF-16 units, four UTF-8 bytes
const MATHX = '\u{1D54F}';   // U+1D54F, likewise astral

const AXES = [
  {
    axis: 'utf16-code-units',
    kills: 'runes-not-code-units',
    note: '`?` matches one UTF-16 code unit, so one astral character is two.',
    probes: [
      ['?', SMILE], ['??', SMILE], ['???', SMILE], ['*', SMILE],
      ['?', MATHX], ['??', MATHX],
      ['a?b', 'a' + SMILE + 'b'], ['a??b', 'a' + SMILE + 'b'],
      ['[a-z]', SMILE], ['?', 'é'], ['??', 'é'],
      ['?', 'é'], ['??', 'é'],
      ['?', '\uD83D'], ['*', '\uD83D'],
      ['**', SMILE], ['a/*', 'a/' + SMILE]
    ]
  },
  {
    axis: 'js-dot-exclusions',
    kills: 'globstar-crosses-newline',
    note: 'The globstar body uses JS `.`, which excludes exactly \\n \\r U+2028 U+2029.',
    probes: [
      ['a/**/b', 'a/x\ny/b'], ['a/**/b', 'a/x\ry/b'],
      ['a/**/b', 'a/x y/b'], ['a/**/b', 'a/x y/b'],
      ['a/**/b', 'a/x y/b'], ['a/**/b', 'a/x\ty/b'],
      ['**', 'a\nb'], ['**', 'a b'],
      ['*', 'a\nb'], ['*', 'a b'],
      ['**/*', 'a\nb/c'], ['a**b', 'a\nb']
    ]
  },
  {
    axis: 'case-folding',
    kills: 'unicode-case-folding',
    note: 'nocase uses JS non-unicode Canonicalize, which refuses to fold non-ASCII onto ASCII. ' +
          'Go (?i) and unicode.ToLower both do fold them.',
    probes: [
      ['k', 'K', { nocase: true }], ['k', 'K', { nocase: true }],
      ['K', 'K', { nocase: true }], ['K', 'k', { nocase: true }],
      ['s', 'ſ', { nocase: true }], ['ſ', 's', { nocase: true }],
      ['i', 'İ', { nocase: true }], ['i', 'ı', { nocase: true }],
      ['İ', 'i', { nocase: true }],
      ['ss', 'ß', { nocase: true }], ['ß', 'ss', { nocase: true }],
      ['a', 'A', { nocase: true }], ['[a-z]', 'K', { nocase: true }],
      ['[a-z]', 'K', { nocase: true }],
      ['k', 'K'], ['a', 'A']
    ]
  },
  {
    axis: 'maxlength-units',
    kills: 'maxlength-in-code-points',
    note: 'The cap counts UTF-16 units. Go len() counts bytes and len([]rune()) counts code ' +
          'points; for an astral string all three differ.',
    probes: [
      [SMILE.repeat(300) + '*', 'x', { maxLength: 400 }],
      [SMILE.repeat(300) + '*', 'x', { maxLength: 700 }],
      [SMILE.repeat(300) + '*', 'x', { maxLength: 601 }],
      [SMILE.repeat(300) + '*', 'x', { maxLength: 600 }],
      ['a'.repeat(600) + '*', 'x', { maxLength: 601 }],
      ['a'.repeat(600) + '*', 'x', { maxLength: 400 }],
      ['é'.repeat(300) + '*', 'x', { maxLength: 301 }]
    ]
  },
  {
    axis: 'fastpaths',
    kills: 'no-fastpaths',
    note: 'The inline fast path changes the compiled output, not just the route to it: ' +
          'it adds trailing-slash leniency the full scanner does not emit.',
    probes: [
      ['*.js', 'a.js/'], ['*.js', 'a.js'], ['.*', '.x/'], ['.*', '.x'],
      ['*', 'a/'], ['*', 'a'], ['*.js', 'a.js//'],
      ['*.js', 'a.js/', { strictSlashes: true }],
      ['.*', '.x/', { strictSlashes: true }],
      ['*a', 'ba/'], ['*', '.x'], ['.*', '.']
    ]
  },
  {
    axis: 'dot-guard-lexical',
    kills: '(no mutation; upstream coverage is a single test)',
    note: 'The leading-dot guard is emitted from the pattern\'s lexical left neighbour, not from ' +
          'the match position. Exactly one upstream test discriminates this.',
    probes: [
      ['a/*', 'a/.x'], ['{a/,b/}*', 'a/.x'], ['{a/,b/}*', 'b/.x'],
      ['@(a/)*', 'a/.x'], ['a{a,b/}*.txt', 'ab/.txt'],
      ['a/*', 'a/x'], ['{a/,b/}*', 'a/x'],
      ['*', '.x'], ['**/*', 'a/.x'], ['+(a/)*', 'a/.x'],
      ['{a,b}/*', 'a/.x']
    ]
  }
];

// --- recording --------------------------------------------------------------

const pinPlatform = platform => {
  Object.defineProperty(globalThis, 'navigator', {
    value: { platform: platform === 'windows' ? 'win32' : 'linux' },
    configurable: true, writable: true, enumerable: false
  });
};

const loadUpstream = () => {
  for (const key of Object.keys(require.cache)) {
    if (key.startsWith(UPSTREAM)) delete require.cache[key];
  }
  return require(path.join(UPSTREAM, 'lib', 'picomatch.js'));
};

const main = () => {
  if (!fs.existsSync(path.join(UPSTREAM, 'lib', 'picomatch.js'))) {
    console.error(`upstream not vendored at ${UPSTREAM}`);
    process.exit(2);
  }

  const cases = [];
  const byAxis = {};
  let id = 0;

  for (const platform of ['posix', 'windows']) {
    pinPlatform(platform);
    const pm = loadUpstream();

    for (const { axis, kills, note, probes } of AXES) {
      byAxis[axis] = (byAxis[axis] || 0) + probes.length;

      for (const [pattern, input, options] of probes) {
        const opts = options || {};
        const args = [input, pattern];
        if (options) args.push(options);

        let result, error = null;
        try {
          result = pm.isMatch(input, pattern, opts);
        } catch (err) {
          error = { name: err.name, message: err.message, code: err.code ?? null };
        }

        cases.push({
          id: ++id,
          platform,
          module: 'lib/picomatch',
          api: 'isMatch',
          construct: null,
          args,
          result: error ? undefined : result,
          error,
          portable: true,
          truncated: false,
          occurrences: 1,
          spec: `charaxis/${axis}.js`,
          test: `${axis}: ${JSON.stringify(pattern)} vs ${JSON.stringify(input)}` +
                (options ? ` ${JSON.stringify(options)}` : ''),
          testOutcome: 'passed',
          // Provenance. These are NOT upstream's tests; the inputs were chosen
          // here and only the answers come from picomatch.
          source: 'charaxis',
          axis,
          killsMutation: kills,
          note
        });
      }
    }
  }

  fs.mkdirSync(OUT_DIR, { recursive: true });
  fs.writeFileSync(path.join(OUT_DIR, 'cases.jsonl'),
    cases.map(c => JSON.stringify(c)).join('\n') + '\n');

  const threw = cases.filter(c => c.error).length;
  const summary = {
    _comment: 'Generated by tools/charaxis/generate.js. Inputs are chosen; every expected ' +
      'value is recorded from upstream picomatch. Not upstream\'s own tests — see ' +
      'testdata/original for those. Do not hand-edit.',
    upstream: JSON.parse(fs.readFileSync(path.join(UPSTREAM, 'package.json'), 'utf8')).version,
    generatedBy: 'tools/charaxis/generate.js',
    cases: { total: cases.length, threw, byAxis, platforms: ['posix', 'windows'] },
    axes: AXES.map(a => ({ axis: a.axis, kills: a.kills, probes: a.probes.length, note: a.note }))
  };
  fs.writeFileSync(path.join(OUT_DIR, 'summary.json'), JSON.stringify(summary, null, 2) + '\n');

  // byAxis already accumulates once per platform, so it is the total.
  console.log(`wrote ${cases.length} cases (${threw} recorded throws) to testdata/charaxis/`);
  for (const [axis, n] of Object.entries(byAxis)) console.log(`  ${axis.padEnd(22)} ${n}`);
};

main();
