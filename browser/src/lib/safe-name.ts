const UNSAFE_RANGES: ReadonlyArray<readonly [number, number]> = [
  [0x00, 0x1f], // C0 controls
  [0x7f, 0x9f], // DEL and C1 controls
  [0x061c, 0x061c], // ALM (Arabic letter mark)
  [0x200b, 0x200f], // zero-width space/non-joiner/joiner, LRM, RLM
  [0x2060, 0x2060], // word joiner
  [0x202a, 0x202e], // LRE, RLE, PDF, LRO, RLO (embeddings and overrides)
  [0x2066, 0x2069], // LRI, RLI, FSI, PDI (isolates)
  [0xfeff, 0xfeff], // BOM / zero-width no-break space
];

export function safeDisplayName(name: string): string {
  let out = "";
  for (const ch of name) {
    const cp = ch.codePointAt(0) ?? 0;
    const unsafe = UNSAFE_RANGES.some(([lo, hi]) => cp >= lo && cp <= hi);
    if (!unsafe) {
      out += ch;
    }
  }
  return out;
}
