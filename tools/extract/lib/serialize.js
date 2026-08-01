'use strict';

/**
 * JSON encoding for arbitrary JavaScript values observed at picomatch's API
 * boundary.
 *
 * Plain JSON types round-trip as themselves. Everything JSON cannot express is
 * encoded as a single-key tagged object (`{ "$regexp": ... }`) so the Go side can
 * decode it unambiguously — a bare string is always a string, never a smuggled
 * RegExp. See `internal/testcase` for the decoder.
 */

/** Guards against pathological nesting; picomatch states nest ~6 deep in practice. */
const MAX_DEPTH = 16;

/**
 * `prev` is a back-pointer from a parse token to its parent. It creates the only
 * true cycles in picomatch's state objects and carries no portable meaning, so it
 * is dropped rather than encoded as `$circular`.
 */
const DROP_KEYS = new Set(['prev']);

/** Marks values that were cut short, so callers can flag a fixture as lossy. */
const TRUNCATED = Symbol('truncated');

const tagNumber = value => {
  if (Number.isNaN(value)) return { $number: 'NaN' };
  if (value === Infinity) return { $number: 'Infinity' };
  if (value === -Infinity) return { $number: '-Infinity' };
  return value;
};

/**
 * Functions reach us as option values (`onMatch`, `format`, `expandRange`). They
 * cannot be replayed in Go, so we record their source purely as a breadcrumb and
 * let the caller mark the fixture unportable.
 */
const tagFunction = value => ({
  $function: {
    name: value.name || '(anonymous)',
    source: Function.prototype.toString.call(value).slice(0, 400)
  }
});

/**
 * @param {*} value
 * @param {{ seen: WeakSet, depth: number, lossy: object }} ctx
 */
const encodeValue = (value, ctx) => {
  if (value === undefined) return { $undefined: true };
  if (value === null) return null;

  switch (typeof value) {
    case 'string':
    case 'boolean':
      return value;
    case 'number':
      return tagNumber(value);
    case 'bigint':
      return { $bigint: value.toString() };
    case 'symbol':
      return { $symbol: String(value) };
    case 'function':
      return tagFunction(value);
    default:
      break;
  }

  if (value instanceof RegExp) return { $regexp: { source: value.source, flags: value.flags } };
  if (value instanceof Date) return { $date: value.toISOString() };

  if (value instanceof Error) {
    return { $error: { name: value.name, message: value.message, code: value.code ?? null } };
  }

  if (ctx.seen.has(value)) {
    ctx.lossy[TRUNCATED] = true;
    return { $circular: true };
  }

  if (ctx.depth >= MAX_DEPTH) {
    ctx.lossy[TRUNCATED] = true;
    return { $truncated: true };
  }

  ctx.seen.add(value);
  const child = { seen: ctx.seen, depth: ctx.depth + 1, lossy: ctx.lossy };

  try {
    if (Array.isArray(value)) {
      const arr = value.map(v => encodeValue(v, child));
      // A RegExp match array carries `index`/`input` alongside its positional
      // captures; keeping them lets the Go harness compare capture behaviour, not
      // just the slice. Named captures live on the separate `.groups` property and
      // are not recorded — picomatch's suite never asserts on them.
      if (typeof value.index === 'number') {
        return { $match: { groups: arr, index: value.index, input: value.input ?? null } };
      }
      return arr;
    }

    if (value instanceof Set) return { $set: [...value].map(v => encodeValue(v, child)) };
    if (value instanceof Map) {
      return { $map: [...value].map(([k, v]) => [encodeValue(k, child), encodeValue(v, child)]) };
    }

    const out = {};
    for (const key of Object.keys(value)) {
      if (DROP_KEYS.has(key)) continue;
      out[key] = encodeValue(value[key], child);
    }
    return out;
  } finally {
    ctx.seen.delete(value);
  }
};

/**
 * Encodes a value for storage.
 * @returns {{ value: *, truncated: boolean }}
 */
const encode = value => {
  const lossy = {};
  const result = encodeValue(value, { seen: new WeakSet(), depth: 0, lossy });
  return { value: result, truncated: lossy[TRUNCATED] === true };
};

/**
 * True when a value contains anything the Go harness cannot reconstruct — today
 * that means callback options. Such fixtures are recorded but excluded from the
 * replayable set.
 *
 * `seen` is not optional bookkeeping: `writeCall` runs this over the same
 * arguments it hands to `encode`, and picomatch's parse tokens carry `prev`
 * back-pointers (the reason DROP_KEYS exists). Without the guard, one spec
 * passing such an object would abort extraction with a stack overflow instead of
 * recording the case. Sets and Maps are walked explicitly because `Object.values`
 * reports them as empty.
 */
const walkForFunction = (value, seen) => {
  if (typeof value === 'function') return true;
  if (value === null || typeof value !== 'object') return false;
  if (value instanceof RegExp || value instanceof Date) return false;

  if (seen.has(value)) return false;
  seen.add(value);

  if (value instanceof Set) return [...value].some(v => walkForFunction(v, seen));
  if (value instanceof Map) {
    return [...value].some(([k, v]) => walkForFunction(k, seen) || walkForFunction(v, seen));
  }
  return Object.values(value).some(v => walkForFunction(v, seen));
};

// Strictly unary. Callers pass this straight to `Array.prototype.some`, which
// supplies (element, index, array) — a `seen` default parameter would be
// clobbered by the index and every options object would throw on `seen.has`.
const hasFunction = value => walkForFunction(value, new WeakSet());

module.exports = { encode, hasFunction, MAX_DEPTH };
