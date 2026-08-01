'use strict';

const path = require('path');

/** Absolute path to `tools/extract`. */
const EXTRACT_DIR = path.resolve(__dirname, '..');

/** Absolute path to the repository root. */
const REPO_ROOT = path.resolve(EXTRACT_DIR, '..', '..');

/** Vendored, unmodified upstream picomatch tree (source + its own test suite). */
const UPSTREAM_DIR = path.join(REPO_ROOT, 'tests', 'original');

/** Upstream Mocha specs. These are the files judges hash; nothing here is ever written. */
const UPSTREAM_TEST_DIR = path.join(UPSTREAM_DIR, 'test');

/** Hash manifest covering every vendored upstream file. */
const MANIFEST_PATH = path.join(UPSTREAM_DIR, 'MANIFEST.json');

/** Where extracted fixtures land, consumed by the Go conformance harness. */
const FIXTURE_DIR = path.join(REPO_ROOT, 'testdata', 'original');

/** npm dependencies used only by the extractor (mocha, fill-range). */
const NODE_MODULES = path.join(EXTRACT_DIR, 'node_modules');

/**
 * The exact upstream revision this port targets. `vendor.js` refuses to copy
 * anything else, so the fixtures can always be traced back to one commit.
 */
const UPSTREAM = {
  repo: 'https://github.com/micromatch/picomatch',
  version: '4.0.5',
  commit: '4f41a8edade7a5ab19832f7b40ecce46b288767f'
};

/**
 * Files copied out of the upstream checkout. `test/` and `lib/` are copied
 * recursively; everything else is a single file.
 */
const VENDORED_ENTRIES = [
  'index.js',
  'posix.js',
  'package.json',
  'LICENSE',
  'lib',
  'test'
];

module.exports = {
  EXTRACT_DIR,
  FIXTURE_DIR,
  MANIFEST_PATH,
  NODE_MODULES,
  REPO_ROOT,
  UPSTREAM,
  UPSTREAM_DIR,
  UPSTREAM_TEST_DIR,
  VENDORED_ENTRIES
};
