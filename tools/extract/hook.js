'use strict';

/**
 * Mocha `--require` entry point for the extractor.
 *
 * Loaded before any spec, so the module interception is in place by the time the
 * upstream suite does its first `require('..')`. Exports mocha root hooks to tag
 * every recorded call with the test it came from.
 */

const { beginTest, endTest, install, open } = require('./lib/instrument');

/**
 * picomatch's entry point defaults `options.windows` to `utils.isWindows()`,
 * which consults `navigator.platform` before `process.platform`. Pinning
 * `navigator` makes extraction produce identical fixtures on a Windows laptop and
 * a Linux CI runner — otherwise every unpinned pattern would silently record
 * whichever slash semantics the extracting machine happened to use.
 */
const PLATFORMS = { posix: 'linux', windows: 'win32' };

const forcePlatform = mode => {
  // Reject anything unrecognised rather than falling back. extract.js labels every
  // recorded case with the mode it asked for, so a value that quietly resolved to
  // linux would produce fixtures tagged `windows` but carrying POSIX semantics —
  // precisely the mislabelling dual-platform extraction exists to prevent.
  const platform = PLATFORMS[mode];
  if (!platform) {
    throw new Error(
      `PICOMATCH_EXTRACT_PLATFORM must be one of ${Object.keys(PLATFORMS).join(', ')}, ` +
      `got ${JSON.stringify(mode)}`
    );
  }

  Object.defineProperty(globalThis, 'navigator', {
    value: { platform },
    configurable: true,
    writable: true,
    enumerable: false
  });

  if (globalThis.navigator.platform !== platform) {
    throw new Error(`failed to pin navigator.platform to ${platform}`);
  }
};

forcePlatform(process.env.PICOMATCH_EXTRACT_PLATFORM || 'posix');

/**
 * `baseline` runs the suite with the same module resolution and platform pinning
 * but no instrumentation. `extract.js` diffs the two runs; any difference means
 * the recorder perturbed the suite and the fixtures cannot be trusted.
 */
if (process.env.PICOMATCH_EXTRACT_MODE === 'baseline') {
  install({ record: false });
} else {
  const out = process.env.PICOMATCH_EXTRACT_OUT;
  if (!out) {
    throw new Error('PICOMATCH_EXTRACT_OUT is not set — run this through tools/extract/extract.js');
  }

  open(out);
  install({ record: true });

  exports.mochaHooks = {
    beforeEach() {
      beginTest(this.currentTest);
    },
    afterEach() {
      endTest(this.currentTest);
    }
  };
}
