// Molang inside a JSON string is analysed as the string's VALUE -- what the
// game reads after JSON has decoded it -- but everything reported about it
// has to point into the file, where the text is still escaped. `\"`, `\\` and
// `\n` are two characters in the file and one in the value, `é` six and
// one. This builds the map between the two once per string.

export interface DecodedJsonString {
  /** The decoded value, as a JavaScript string. */
  value: string;
  /**
   * map[i] is the offset in the raw text of the escape or character that
   * produced value unit i (UTF-16 units on both sides). map[value.length]
   * is the raw length, so an end offset maps like any other.
   */
  map: number[];
}

const simpleEscapes: Record<string, string> = {
  '"': '"',
  '\\': '\\',
  '/': '/',
  b: '\b',
  f: '\f',
  n: '\n',
  r: '\r',
  t: '\t',
};

/**
 * Decodes the text between a JSON string's quotes. It is tolerant, because
 * the file is being edited: an escape JSON does not have, or a \u with too
 * few hex digits, is taken literally rather than refused -- the JSON
 * language service already reports it, and the Molang inside still deserves
 * its own diagnostics meanwhile.
 */
export function decodeJsonString(raw: string): DecodedJsonString {
  let value = '';
  const map: number[] = [];
  let i = 0;
  while (i < raw.length) {
    const c = raw[i];
    if (c !== '\\' || i + 1 >= raw.length) {
      value += c;
      map.push(i);
      i++;
      continue;
    }
    const e = raw[i + 1];
    if (e in simpleEscapes) {
      value += simpleEscapes[e];
      map.push(i);
      i += 2;
      continue;
    }
    if (e === 'u' && /^[0-9a-fA-F]{4}$/.test(raw.slice(i + 2, i + 6))) {
      // One UTF-16 unit per \u escape: a surrogate pair is written as two
      // escapes, and each half maps to its own.
      value += String.fromCharCode(parseInt(raw.slice(i + 2, i + 6), 16));
      map.push(i);
      i += 6;
      continue;
    }
    value += c;
    map.push(i);
    i++;
  }
  map.push(raw.length);
  return { value, map };
}

/** The raw offset of value offset v. */
export function valueToRaw(d: DecodedJsonString, v: number): number {
  if (v <= 0) return 0;
  if (v >= d.map.length) return d.map[d.map.length - 1];
  return d.map[v];
}

/**
 * The value offset of raw offset r: the value unit whose escape contains r,
 * or, for a raw offset between two units, the later one. A cursor inside
 * `\n` is at the newline.
 */
export function rawToValue(d: DecodedJsonString, r: number): number {
  let lo = 0;
  let hi = d.map.length - 1;
  while (lo < hi) {
    const mid = (lo + hi + 1) >> 1;
    if (d.map[mid] <= r) lo = mid;
    else hi = mid - 1;
  }
  return lo;
}
