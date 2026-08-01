'use strict';

/**
 * Replays recorded fixtures against a picomatch tree.
 *
 * This is not a test of picomatch. It is the instrument the mutation harness
 * uses to ask what the FIXTURE SET can detect, so its comparison mirrors
 * conformance_test.go exactly and deliberately no more:
 *
 *   - only the four replayed APIs (isMatch, matcher, scan) are compared;
 *   - matcher cases compare isMatch/glob/input/output/posix and skip `match`
 *     and `regex`, exactly as matcherFieldsNotCompared declares;
 *   - makeRe / parse / utils cases are not replayed at all.
 *
 * Any divergence from the Go harness's comparison would make the measurement
 * describe a harness that does not exist.
 *
 *   node replay.js <picomatch-root> [cases.jsonl]
 */

const fs = require('fs');
const path = require('path');

const REPO = path.resolve(__dirname, '..', '..');
const DEFAULT_CASES = path.join(REPO, 'testdata', 'original', 'cases.jsonl');

/** Recorded matcher keys the Go harness does not compare. Keep in sync. */
const MATCHER_SKIP = new Set(['isMatch', 'match', 'regex']);

/** Scan keys the Go harness supplies a value for. */
const SCAN_FIELDS = [
  'base', 'glob', 'prefix', 'input', 'start', 'isGlob', 'isBrace', 'isBracket',
  'isGlobstar', 'isExtglob', 'negated', 'negatedExtglob', 'parts', 'slashes'
];

const REPLAYED = new Set([
  'lib/picomatch.isMatch', 'index.matcher', 'lib/picomatch.matcher', 'lib/scan.scan'
]);

/** Mirror of internal/testcase's decoder for the tagged-value encoding. */
const decode = v => {
  if (v === null || typeof v !== 'object') return v;
  if (Array.isArray(v)) return v.map(decode);

  const keys = Object.keys(v);
  if (keys.length === 1) {
    switch (keys[0]) {
      case '$undefined': return undefined;
      case '$number': return v.$number === 'NaN' ? NaN : Number(v.$number);
      case '$regexp': return new RegExp(v.$regexp.source, v.$regexp.flags);
      case '$error': return v.$error;
      case '$match': return v.$match.groups.map(decode);
      case '$set': return new Set(v.$set.map(decode));
      // A replayable case contains none of these by construction.
      case '$function': case '$circular': case '$truncated': return undefined;
      default: break;
    }
  }

  const out = {};
  for (const k of keys) out[k] = decode(v[k]);
  return out;
};

/**
 * picomatch reads slash semantics from navigator.platform; tools/extract/hook.js
 * pins it the same way when recording. Replaying under the host's platform
 * instead would silently mark half the fixtures as divergent.
 */
const pinPlatform = platform => {
  Object.defineProperty(globalThis, 'navigator', {
    value: { platform: platform === 'windows' ? 'win32' : 'linux' },
    configurable: true,
    writable: true,
    enumerable: false
  });
};

const loadTree = root => {
  const abs = path.resolve(root);
  for (const key of Object.keys(require.cache)) {
    if (key.startsWith(abs)) delete require.cache[key];
  }
  return {
    index: require(path.join(abs, 'index.js')),
    lib: require(path.join(abs, 'lib', 'picomatch.js')),
    scan: require(path.join(abs, 'lib', 'scan.js'))
  };
};

const equal = (want, got) => {
  if (Array.isArray(want)) {
    return Array.isArray(got) && want.length === got.length && want.every((w, i) => equal(w, got[i]));
  }
  return want === got;
};

const compareFields = (recorded, actual, skip) => {
  for (const name of Object.keys(recorded)) {
    if (!(name in actual)) {
      if (skip.has(name)) continue;
      return { status: 'unsupported', detail: `${name}: recorded but not compared` };
    }
    if (!equal(recorded[name], actual[name])) {
      return { status: 'failed', detail: `${name}: want ${recorded[name]}, got ${actual[name]}` };
    }
  }
  return { status: 'passed', detail: '' };
};

const replayOne = (trees, c) => {
  const key = `${c.module}.${c.api}`;
  const pm = trees[c.platform];
  pinPlatform(c.platform);

  const args = (c.args || []).map(decode);
  const want = c.result === undefined ? undefined : decode(c.result);

  try {
    if (key === 'lib/picomatch.isMatch') {
      const got = pm.lib.isMatch(...args);
      if (c.error) return { status: 'failed', detail: 'expected a throw' };
      return got === want
        ? { status: 'passed', detail: '' }
        : { status: 'failed', detail: `want ${want}, got ${got}` };
    }

    if (key === 'lib/scan.scan') {
      const got = pm.scan(...args);
      if (c.error) return { status: 'failed', detail: 'expected a throw' };
      const actual = {};
      for (const f of SCAN_FIELDS) if (got[f] !== undefined) actual[f] = got[f];
      return compareFields(want, actual, new Set());
    }

    const construct = (c.construct || []).map(decode);
    const matcher = (key === 'index.matcher' ? pm.index : pm.lib)(...construct);
    const got = matcher(...args);
    if (c.error) return { status: 'failed', detail: 'expected a throw' };

    if (want && typeof want === 'object') {
      if (got.isMatch !== want.isMatch) {
        return { status: 'failed', detail: `isMatch: want ${want.isMatch}, got ${got.isMatch}` };
      }
      return compareFields(want,
        { glob: got.glob, input: got.input, output: got.output, posix: got.posix },
        MATCHER_SKIP);
    }
    return got === want
      ? { status: 'passed', detail: '' }
      : { status: 'failed', detail: `want ${want}, got ${got}` };

  } catch (err) {
    // A throw is correct only where one was recorded. Comparing the message
    // would be stricter than the Go harness is on this path, and the point is
    // to measure that harness, not a better one.
    return c.error
      ? { status: 'passed', detail: '' }
      : { status: 'failed', detail: `threw: ${err.message}` };
  }
};

/** @returns {{total:number, passed:number, failed:number, unsupported:number, failures:string[]}} */
const replay = (root, casesFile = DEFAULT_CASES, { sampleFailures = 8 } = {}) => {
  const lines = fs.readFileSync(casesFile, 'utf8').split('\n').filter(Boolean);

  const trees = {};
  for (const platform of ['posix', 'windows']) {
    pinPlatform(platform);
    trees[platform] = loadTree(root);
  }

  const out = { total: 0, passed: 0, failed: 0, unsupported: 0, failures: [] };

  for (const line of lines) {
    const c = JSON.parse(line);
    if (!c.portable || c.truncated || c.testOutcome !== 'passed') continue;
    if (!REPLAYED.has(`${c.module}.${c.api}`)) continue;

    out.total++;
    const { status, detail } = replayOne(trees, c);
    out[status]++;
    if (status !== 'passed' && out.failures.length < sampleFailures) {
      out.failures.push(`${c.platform} ${c.module}.${c.api} ${JSON.stringify(c.args)} :: ${detail}`);
    }
  }

  return out;
};

module.exports = { replay, DEFAULT_CASES };

if (require.main === module) {
  const root = process.argv[2];
  if (!root) {
    console.error('usage: node replay.js <picomatch-root> [cases.jsonl]');
    process.exit(2);
  }
  const result = replay(root, process.argv[3] || DEFAULT_CASES);
  console.log(JSON.stringify({
    total: result.total, passed: result.passed,
    failed: result.failed, unsupported: result.unsupported
  }));
  if (process.env.SHOW_FAILURES) result.failures.forEach(f => console.log('  ' + f));
}
