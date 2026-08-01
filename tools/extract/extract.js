#!/usr/bin/env node
'use strict';

/**
 * Runs the unmodified upstream Mocha suite twice — once with POSIX path
 * semantics, once with Windows — recording every call it makes into picomatch,
 * then folds the two runs into `testdata/original/cases.jsonl`.
 *
 * Both passes matter: picomatch's entry point picks slash semantics from the host
 * OS, so a pattern like `a/*` has two legitimate answers. Pinning the platform
 * explicitly (see hook.js) turns that ambiguity into two labelled fixture sets
 * instead of a machine-dependent one.
 *
 *   node extract.js                 # both platforms
 *   node extract.js --platform posix
 */

const { spawnSync } = require('child_process');
const fs = require('fs');
const os = require('os');
const path = require('path');

const { diffHashes, hashTree } = require('./lib/hashes');
const {
  EXTRACT_DIR,
  FIXTURE_DIR,
  MANIFEST_PATH,
  REPO_ROOT,
  UPSTREAM_TEST_DIR,
  UPSTREAM_DIR,
  VENDORED_ENTRIES
} = require('./lib/paths');

const PLATFORMS = ['posix', 'windows'];

const parseArgs = argv => {
  const opts = { platforms: PLATFORMS, keepRaw: false };

  for (let i = 0; i < argv.length; i++) {
    if (argv[i] === '--platform') {
      const value = argv[++i];
      if (!PLATFORMS.includes(value)) throw new Error(`--platform must be one of ${PLATFORMS}`);
      opts.platforms = [value];
    } else if (argv[i] === '--keep-raw') {
      opts.keepRaw = true;
    } else {
      throw new Error(`unknown argument: ${argv[i]}`);
    }
  }

  return opts;
};

/** Refuses to extract from a suite that no longer matches its manifest. */
const assertUpstreamPristine = () => {
  if (!fs.existsSync(MANIFEST_PATH)) {
    throw new Error('tests/original/MANIFEST.json not found — run `npm run vendor` first');
  }

  const manifest = JSON.parse(fs.readFileSync(MANIFEST_PATH, 'utf8'));
  const { clean, changed, added, removed } = diffHashes(
    manifest.files,
    hashTree(UPSTREAM_DIR, VENDORED_ENTRIES)
  );

  if (!clean) {
    throw new Error(
      'tests/original has drifted from MANIFEST.json — run `npm run verify` for details.\n' +
      `  modified: ${changed.length}  added: ${added.length}  removed: ${removed.length}`
    );
  }

  return manifest;
};

/** Top-level specs only; `test/support/` holds helpers, not tests. */
const specFiles = () =>
  fs
    .readdirSync(UPSTREAM_TEST_DIR, { withFileTypes: true })
    .filter(e => e.isFile() && e.name.endsWith('.js'))
    .map(e => path.join(UPSTREAM_TEST_DIR, e.name))
    .sort();

/**
 * Runs mocha once under a pinned platform. Failures are expected and recorded,
 * not fatal: a failing upstream test is still a true observation of picomatch's
 * behaviour, and we want to know which fixtures came from one.
 */
const runSuite = (platform, mode, rawFile) => {
  const mocha = require.resolve('mocha/bin/mocha.js');

  const args = [
    mocha,
    '--no-config',
    '--no-package',
    '--reporter', 'json',
    '--timeout', '30000',
    '--require', path.join(EXTRACT_DIR, 'hook.js'),
    ...specFiles()
  ];

  const res = spawnSync(process.execPath, args, {
    cwd: REPO_ROOT,
    encoding: 'utf8',
    maxBuffer: 256 * 1024 * 1024,
    env: {
      ...process.env,
      PICOMATCH_EXTRACT_MODE: mode,
      PICOMATCH_EXTRACT_OUT: rawFile || '',
      PICOMATCH_EXTRACT_PLATFORM: platform
    }
  });

  if (res.error) throw res.error;

  // The json reporter prints one JSON document; anything before it is noise from
  // a crashed load, which we surface rather than swallow.
  const start = res.stdout.indexOf('{');
  if (start === -1) {
    throw new Error(`mocha produced no JSON report (${platform})\n${res.stderr.slice(0, 4000)}`);
  }

  let report;
  try {
    report = JSON.parse(res.stdout.slice(start));
  } catch (err) {
    throw new Error(`could not parse mocha report (${platform}): ${err.message}`);
  }

  // Never default these away. A report without stats would yield tests=0 for both
  // the baseline and the recorded run, and runInstrumented would then compare two
  // empty suites and declare the recorder transparent having verified nothing.
  if (!report.stats) {
    throw new Error(`mocha report for ${platform} has no stats block (${mode} mode)`);
  }

  const { tests = 0, passes = 0, failures = 0, pending = 0 } = report.stats;

  if (tests === 0) {
    throw new Error(
      `mocha ran 0 tests (${platform}, ${mode} mode) — the suite did not load.\n` +
      res.stderr.slice(0, 4000)
    );
  }

  return {
    tests,
    passes,
    failures,
    pending,
    failingTests: (report.failures || []).map(f => f.fullTitle).sort()
  };
};

/**
 * Runs the suite clean, then instrumented, and refuses to continue if the
 * recorder changed any outcome.
 *
 * This is the pipeline's core honesty guarantee. An instrumenting recorder that
 * quietly breaks three tests would produce fixtures asserting picomatch behaves
 * in a way it does not — and the Go port would then be "proved" against a lie.
 */
const runInstrumented = (platform, rawFile) => {
  console.log(`  ${platform}: baseline...`);
  const baseline = runSuite(platform, 'baseline', null);
  console.log(`    ${baseline.passes}/${baseline.tests} pass (${baseline.failures} failing)`);

  console.log(`  ${platform}: recording...`);
  const recorded = runSuite(platform, 'record', rawFile);
  console.log(`    ${recorded.passes}/${recorded.tests} pass (${recorded.failures} failing)`);

  const added = recorded.failingTests.filter(t => !baseline.failingTests.includes(t));
  const fixed = baseline.failingTests.filter(t => !recorded.failingTests.includes(t));

  // `passes` and `pending` are compared as well as the failure set. A test the
  // recorder caused to be skipped rather than failed leaves both `tests` and the
  // failing titles untouched while `passes` drops — the guard would then declare
  // transparency over fixtures that are quietly missing that test's observations.
  const counts = ['tests', 'passes', 'failures', 'pending'].filter(k => baseline[k] !== recorded[k]);

  if (added.length || fixed.length || counts.length) {
    const lines = [
      `the recorder changed upstream test outcomes on ${platform} — fixtures are not trustworthy.`,
      `  baseline: ${baseline.passes}/${baseline.tests}   recorded: ${recorded.passes}/${recorded.tests}`
    ];
    for (const k of counts) lines.push(`  ${k}: ${baseline[k]} -> ${recorded[k]}`);
    for (const t of added) lines.push(`  broken by recorder: ${t}`);
    for (const t of fixed) lines.push(`  masked by recorder: ${t}`);
    throw new Error(lines.join('\n'));
  }

  console.log('    recorder is transparent: identical outcomes');
  return { ...recorded, baselineFailures: baseline.failingTests };
};

/** Stable identity for a call: same module, api, construction and arguments. */
const caseKey = rec =>
  JSON.stringify([rec.module, rec.api, rec.construct, rec.args]);

/**
 * Folds a raw JSONL run into deduplicated cases.
 *
 * The suite calls `isMatch('a', '*')`-shaped things tens of thousands of times
 * with heavy repetition; collapsing identical calls keeps the committed fixture
 * reviewable while `occurrences` preserves how much of the suite each case backs.
 */
const foldRun = (rawFile, platform) => {
  const outcomes = new Map();
  const cases = new Map();
  const conflicts = [];

  const lines = fs.readFileSync(rawFile, 'utf8').split('\n');
  const calls = [];

  for (const line of lines) {
    if (!line) continue;
    const rec = JSON.parse(line);
    if (rec.kind === 'test') outcomes.set(rec.id, rec);
    else calls.push(rec);
  }

  for (const rec of calls) {
    const test = rec.testId !== null ? outcomes.get(rec.testId) : null;
    const key = caseKey(rec);
    const existing = cases.get(key);

    if (existing) {
      existing.occurrences++;
      // Identical inputs must give identical outputs. If they do not, the
      // extraction is not reproducible and the fixture cannot be trusted.
      const same =
        JSON.stringify(existing.result) === JSON.stringify(rec.result) &&
        JSON.stringify(existing.error) === JSON.stringify(rec.error);
      if (!same) {
        conflicts.push({ platform, module: rec.module, api: rec.api, args: rec.args });
      }
      continue;
    }

    cases.set(key, {
      platform,
      module: rec.module,
      api: rec.api,
      construct: rec.construct,
      args: rec.args,
      result: rec.result,
      error: rec.error,
      portable: rec.portable,
      truncated: rec.truncated,
      occurrences: 1,
      spec: test ? test.file : null,
      test: test ? test.title : null,
      testOutcome: test ? test.outcome : 'unknown'
    });
  }

  return { cases: [...cases.values()], conflicts };
};

/**
 * Derives the options surface from the fixtures themselves.
 *
 * Rather than transcribing picomatch's README and hoping it is complete, this
 * reports every option key the upstream suite actually exercises, how often, and
 * with which value types — which is what the Go `Options` struct is built from.
 * A key typed `function` is a callback the fixtures cannot replay.
 */
const optionSurface = cases => {
  const seen = new Map();

  const visit = value => {
    if (!value || typeof value !== 'object' || Array.isArray(value)) return;
    // Tagged scalars are values, not options objects.
    if (Object.keys(value).some(k => k.startsWith('$'))) return;

    for (const [key, raw] of Object.entries(value)) {
      if (!seen.has(key)) seen.set(key, { uses: 0, types: new Set() });
      const entry = seen.get(key);
      entry.uses++;

      if (raw === null) entry.types.add('null');
      else if (Array.isArray(raw)) entry.types.add('array');
      else if (typeof raw === 'object') {
        if (raw.$function) entry.types.add('function');
        else if (raw.$undefined) entry.types.add('undefined');
        else if (raw.$regexp) entry.types.add('regexp');
        else entry.types.add('object');
      } else entry.types.add(typeof raw);
    }
  };

  for (const c of cases) {
    for (const arg of [...(c.construct || []), ...c.args]) visit(arg);
  }

  return Object.fromEntries(
    [...seen.entries()]
      .sort((a, b) => b[1].uses - a[1].uses)
      .map(([key, v]) => [key, { uses: v.uses, types: [...v.types].sort() }])
  );
};

/** Distinct key sets each API returns — the shapes the Go types must cover. */
const resultShapes = cases => {
  const shapes = {};

  for (const c of cases) {
    const r = c.result;
    if (!r || typeof r !== 'object' || Array.isArray(r)) continue;
    if (Object.keys(r).some(k => k.startsWith('$'))) continue;

    const key = `${c.module}.${c.api}`;
    (shapes[key] = shapes[key] || new Set()).add(Object.keys(r).sort().join(','));
  }

  return Object.fromEntries(Object.entries(shapes).map(([k, v]) => [k, [...v].sort()]));
};

const tally = (items, keyFn) => {
  const out = {};
  for (const item of items) {
    const key = keyFn(item);
    out[key] = (out[key] || 0) + 1;
  }
  return Object.fromEntries(Object.entries(out).sort((a, b) => b[1] - a[1]));
};

const main = () => {
  const opts = parseArgs(process.argv.slice(2));
  const manifest = assertUpstreamPristine();

  console.log(
    `extracting from picomatch v${manifest.upstream.version} @ ` +
    `${manifest.upstream.commit.slice(0, 10)} (${manifest.specCount} spec files, unmodified)`
  );

  fs.mkdirSync(FIXTURE_DIR, { recursive: true });
  const rawDir = fs.mkdtempSync(path.join(os.tmpdir(), 'picomatch-extract-'));

  const mochaStats = {};
  const allCases = [];
  const allConflicts = [];

  try {
    for (const platform of opts.platforms) {
      const rawFile = path.join(rawDir, `${platform}.jsonl`);
      mochaStats[platform] = runInstrumented(platform, rawFile);

      const { cases, conflicts } = foldRun(rawFile, platform);
      allCases.push(...cases);
      allConflicts.push(...conflicts);

      console.log(`    recorded ${cases.length} distinct cases`);

      if (opts.keepRaw) {
        fs.copyFileSync(rawFile, path.join(FIXTURE_DIR, `raw.${platform}.jsonl`));
      }
    }

    // Deterministic ordering keeps the committed fixture's diff meaningful. The
    // key must include `construct`: thousands of matcher cases share an input
    // string and differ only in the glob they were built from, so without it they
    // all tie and their ids fall back to raw-stream insertion order — one new
    // upstream test would then renumber unrelated cases across the whole file.
    // NUL separates the components so no two distinct keys can concatenate alike.
    const sortKey = c =>
      [c.platform, c.module, c.api, JSON.stringify(c.construct), JSON.stringify(c.args)].join('\0');
    allCases.sort((a, b) => {
      const ka = sortKey(a);
      const kb = sortKey(b);
      return ka < kb ? -1 : ka > kb ? 1 : 0;
    });
    allCases.forEach((c, i) => {
      c.id = i + 1;
    });

    const casesPath = path.join(FIXTURE_DIR, 'cases.jsonl');
    fs.writeFileSync(casesPath, allCases.map(c => `${JSON.stringify(c)}\n`).join(''));

    const replayable = allCases.filter(
      c => c.portable && !c.truncated && c.testOutcome === 'passed'
    );

    const summary = {
      _comment:
        'Generated by tools/extract/extract.js. Every case is a recorded observation of ' +
        'upstream picomatch; the Go conformance harness replays them. Do not hand-edit.',
      upstream: manifest.upstream,
      generatedAt: new Date().toISOString(),
      extractor: { node: process.version, platforms: opts.platforms },
      upstreamSuite: mochaStats,
      cases: {
        total: allCases.length,
        replayable: replayable.length,
        unportable: allCases.filter(c => !c.portable).length,
        truncated: allCases.filter(c => c.truncated).length,
        fromFailingTest: allCases.filter(c => c.testOutcome !== 'passed').length,
        totalCallsObserved: allCases.reduce((n, c) => n + c.occurrences, 0),
        byPlatform: tally(allCases, c => c.platform),
        byApi: tally(allCases, c => `${c.module}.${c.api}`),
        bySpec: tally(allCases, c => c.spec || '(outside a test)')
      },
      optionSurface: optionSurface(allCases),
      resultShapes: resultShapes(allCases),
      conflicts: allConflicts
    };

    fs.writeFileSync(
      path.join(FIXTURE_DIR, 'summary.json'),
      `${JSON.stringify(summary, null, 2)}\n`
    );

    const bytes = fs.statSync(casesPath).size;
    console.log(
      `\nwrote ${allCases.length} cases (${(bytes / 1e6).toFixed(1)} MB) -> ` +
      `${path.relative(REPO_ROOT, casesPath).split(path.sep).join('/')}`
    );
    console.log(
      `  ${replayable.length} replayable  ` +
      `${summary.cases.unportable} callback-based  ` +
      `${summary.cases.fromFailingTest} from failing upstream tests`
    );
    console.log(`  ${summary.cases.totalCallsObserved} total API calls observed`);
  } finally {
    fs.rmSync(rawDir, { recursive: true, force: true });
  }

  // Outside the `finally`: process.exit terminates synchronously and would skip
  // the cleanup above, leaking the raw JSONL for both platforms into the temp dir
  // on every retry of a conflicting extraction.
  if (allConflicts.length) {
    console.error(`\nWARNING: ${allConflicts.length} non-deterministic cases — see summary.json`);
    process.exit(1);
  }
};

try {
  main();
} catch (err) {
  console.error(`extract: ${err.message}`);
  process.exit(1);
}
