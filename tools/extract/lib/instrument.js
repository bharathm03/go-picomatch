'use strict';

/**
 * Records picomatch's observable behaviour while its own test suite runs.
 *
 * The upstream specs are never touched. Instead `Module._load` is patched so that
 * when a spec does `require('..')` or `require('../lib/scan')` it receives a
 * transparent wrapper around the real module. Every call the *test* makes is
 * logged with its arguments and its result; calls picomatch makes internally are
 * not (see `depth` below).
 *
 * The upshot: the fixtures describe picomatch's public contract as exercised by
 * 14k lines of upstream specs, without a single assertion being rewritten.
 */

const fs = require('fs');
const Module = require('module');
const path = require('path');

const { encode, hasFunction } = require('./serialize');
const { NODE_MODULES, UPSTREAM_DIR } = require('./paths');

/**
 * Brands a wrapper so it is never wrapped twice (index.js re-exports lib/picomatch).
 * Always attached non-enumerably: `index.js` does `Object.assign(picomatch, pico)`,
 * and an enumerable brand would be copied onto the entry point, making it look
 * pre-wrapped and silently skipping instrumentation of the public API.
 */
const WRAPPED = Symbol('picomatch.wrapped');

const brand = fn => {
  Object.defineProperty(fn, WRAPPED, { value: true, enumerable: false, configurable: true });
  return fn;
};

/**
 * Copies own properties from a real picomatch function onto its wrapper.
 *
 * `picomatch(glob, opts, true)` returns a matcher carrying `.state`, and the
 * upstream suite asserts on it. A wrapper that dropped those properties would
 * make upstream tests fail *because of the recorder* — exactly the kind of
 * harness-induced test damage this project exists to avoid.
 */
const adoptProperties = (wrapper, original) => {
  for (const key of Reflect.ownKeys(original)) {
    if (key === WRAPPED) continue;
    const desc = Object.getOwnPropertyDescriptor(original, key);
    if (!desc || !desc.configurable) continue; // skip length/name/prototype
    Object.defineProperty(wrapper, key, desc);
  }
  return wrapper;
};

/** Upstream files worth instrumenting, keyed by path relative to tests/original. */
const MODULES = {
  'index.js': { label: 'index', shape: 'picomatch' },
  'posix.js': { label: 'posix', shape: 'picomatch' },
  'lib/picomatch.js': { label: 'lib/picomatch', shape: 'picomatch' },
  'lib/scan.js': { label: 'lib/scan', shape: 'function' },
  'lib/parse.js': { label: 'lib/parse', shape: 'function' },
  'lib/utils.js': { label: 'lib/utils', shape: 'namespace' }
};

const state = {
  buffer: [],
  fd: null,
  seq: 0,
  testSeq: 0,
  /**
   * Call nesting. Only calls entered at depth 0 come from test code; anything
   * deeper is picomatch calling itself (array globs, `ignore`, scan-from-parse)
   * and would bury the public contract in implementation detail.
   */
  depth: 0,
  currentTest: null
};

const FLUSH_EVERY = 2048;

const flush = () => {
  if (!state.buffer.length || state.fd === null) return;
  fs.writeSync(state.fd, state.buffer.join(''));
  state.buffer.length = 0;
};

const emit = record => {
  state.buffer.push(`${JSON.stringify(record)}\n`);
  if (state.buffer.length >= FLUSH_EVERY) flush();
};

const open = file => {
  fs.mkdirSync(path.dirname(file), { recursive: true });
  state.fd = fs.openSync(file, 'w');
  process.on('exit', () => {
    flush();
    if (state.fd !== null) fs.closeSync(state.fd);
    state.fd = null;
  });
};

/* -------------------------------------------------------------------------- */
/* Test context                                                               */
/* -------------------------------------------------------------------------- */

const relSpec = file => {
  if (!file) return null;
  return path.relative(UPSTREAM_DIR, file).split(path.sep).join('/');
};

const beginTest = test => {
  state.currentTest = {
    id: ++state.testSeq,
    file: relSpec(test && test.file),
    title: test ? test.fullTitle() : null
  };
};

const endTest = test => {
  if (!state.currentTest) return;
  emit({
    kind: 'test',
    id: state.currentTest.id,
    file: state.currentTest.file,
    title: state.currentTest.title,
    // 'passed' | 'failed'; undefined if mocha bailed before running the body.
    outcome: (test && test.state) || 'unknown'
  });
  state.currentTest = null;
};

/* -------------------------------------------------------------------------- */
/* Wrapping                                                                   */
/* -------------------------------------------------------------------------- */

/**
 * Trims a result before encoding.
 *
 * `matcher(input, true)` returns `{ glob, state, regex, posix, input, output,
 * match, isMatch }`. `state` is picomatch's internal parse state — thousands of
 * bytes per call, structurally tied to a JavaScript tokenizer the Go port has no
 * reason to mirror, and asserted on nowhere in the suite. Dropping it shrinks the
 * fixture by roughly half without losing a single assertion's worth of signal.
 * `makeRe`/`parse` results are left untouched, which is where state *is* asserted.
 */
const project = (api, result) => {
  if (api !== 'matcher' || result === null || typeof result !== 'object') return result;
  const { state, ...rest } = result;
  return rest;
};

/**
 * Builds one JSONL record for a completed top-level call.
 *
 * `construct` is set for matcher invocations: it carries the `picomatch(glob,
 * options)` arguments the matcher was built from, so each record replays
 * standalone without the Go side having to reassemble object graphs.
 */
const writeCall = ({ label, api, args, construct, result, error }) => {
  const encodedArgs = args.map(a => encode(a));
  const encodedConstruct = construct ? construct.map(a => encode(a)) : null;

  // A factory returns a matcher function; its source is noise. The matcher's own
  // calls are recorded separately and carry the behaviour we actually replay.
  const encodedResult = error
    ? null
    : typeof result === 'function'
      ? { value: { $matcher: true }, truncated: false }
      : encode(project(api, result));

  // Construction arguments count towards truncation just as call arguments do: a
  // matcher built from an options object we cut short is replayed against an
  // expectation the fixture itself admits is incomplete, so it must not land in
  // the replayable set.
  const truncated =
    encodedArgs.some(a => a.truncated) ||
    (encodedConstruct ? encodedConstruct.some(a => a.truncated) : false) ||
    (encodedResult ? encodedResult.truncated : false);

  // Callback options (onMatch, onResult, format, expandRange) have no portable
  // representation. Keep the record for provenance, but never let it inflate the
  // parity denominator.
  const unportable = args.some(hasFunction) || (construct ? construct.some(hasFunction) : false);

  emit({
    kind: 'call',
    seq: ++state.seq,
    testId: state.currentTest ? state.currentTest.id : null,
    module: label,
    api,
    construct: encodedConstruct ? encodedConstruct.map(a => a.value) : null,
    args: encodedArgs.map(a => a.value),
    result: encodedResult ? encodedResult.value : null,
    error: error ? { name: error.name, message: error.message } : null,
    portable: !unportable,
    truncated
  });
};

/**
 * Wraps the matcher returned by `picomatch(glob, options)`, tying each match call
 * back to the arguments that produced it.
 */
const wrapMatcher = (label, matcher, constructArgs) => {
  if (matcher[WRAPPED]) return matcher;

  const wrapper = (...args) => {
    if (state.depth > 0) return matcher(...args);

    state.depth++;
    let result;
    let error = null;
    try {
      result = matcher(...args);
    } catch (err) {
      error = err;
    } finally {
      state.depth--;
    }

    writeCall({ label, api: 'matcher', args, construct: constructArgs, result, error });
    if (error) throw error;
    return result;
  };

  // `.state` and friends live on the matcher itself; the suite asserts on them.
  return adoptProperties(brand(wrapper), matcher);
};

/**
 * @param {string} label     module the function belongs to
 * @param {string} api       name recorded in the fixture
 * @param {Function} fn      the real upstream function
 * @param {boolean} matcherFactory whether the return value is a matcher to wrap
 */
const wrapFunction = (label, api, fn, matcherFactory = false) => {
  if (fn[WRAPPED]) return fn;

  const wrapper = function (...args) {
    if (state.depth > 0) return fn.apply(this, args);

    state.depth++;
    let result;
    let error = null;
    try {
      result = fn.apply(this, args);
    } catch (err) {
      error = err;
    } finally {
      state.depth--;
    }

    writeCall({ label, api, args, construct: null, result, error });
    if (error) throw error;

    if (matcherFactory && typeof result === 'function') {
      return wrapMatcher(label, result, args);
    }
    return result;
  };

  return adoptProperties(brand(wrapper), fn);
};

/** Copies own properties onto a wrapper, instrumenting the callable ones. */
const wrapProperties = (label, target, source) => {
  for (const key of Object.keys(source)) {
    const value = source[key];
    target[key] = typeof value === 'function' ? wrapFunction(label, key, value) : value;
  }
  return target;
};

const wrapModule = (label, shape, exports) => {
  if (exports[WRAPPED]) return exports;

  if (shape === 'namespace') {
    const out = {};
    for (const key of Object.keys(exports)) {
      const value = exports[key];
      out[key] = typeof value === 'function' ? wrapFunction(label, key, value) : value;
    }
    return brand(out);
  }

  if (typeof exports !== 'function') return exports;

  // `picomatch` is callable *and* carries the rest of the API as properties.
  const isFactory = shape === 'picomatch';
  const wrapper = wrapFunction(label, isFactory ? 'picomatch' : path.basename(label), exports, isFactory);
  return wrapProperties(label, wrapper, exports);
};

/* -------------------------------------------------------------------------- */
/* Module interception                                                        */
/* -------------------------------------------------------------------------- */

/**
 * Patches module resolution.
 *
 * @param {{ record: boolean }} opts When `record` is false only the `fill-range`
 * redirect is applied — that is the baseline mode, which runs the upstream suite
 * under identical resolution and platform pinning but with no instrumentation, so
 * `extract.js` can prove the recorder does not change a single test outcome.
 */
const install = ({ record = true } = {}) => {
  const wrappers = new Map(); // resolved filename -> wrapped exports
  const originalLoad = Module._load;

  Module._load = function (request, parent, isMain) {
    // The upstream specs pull `fill-range` from picomatch's own devDependencies.
    // We keep our npm tree out of tests/original, so resolve it from ours instead.
    if (request === 'fill-range') {
      return originalLoad.call(this, path.join(NODE_MODULES, 'fill-range'), parent, isMain);
    }

    const exports = originalLoad.apply(this, arguments);
    if (!record) return exports;

    let resolved;
    try {
      resolved = Module._resolveFilename(request, parent, isMain);
    } catch {
      return exports;
    }

    if (wrappers.has(resolved)) return wrappers.get(resolved);

    const rel = path.relative(UPSTREAM_DIR, resolved).split(path.sep).join('/');
    const spec = MODULES[rel];
    if (!spec) return exports;

    const wrapped = wrapModule(spec.label, spec.shape, exports);
    wrappers.set(resolved, wrapped);
    return wrapped;
  };
};

module.exports = { beginTest, endTest, flush, install, open };
