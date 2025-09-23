export type FormatAbbrevOptions = {
  // When formatting tokens/second, show decimals under 1000 instead of integer-only
  tpsMode?: boolean;
  // If true and in tpsMode, show 2 decimals when value < 100
  extraDecimalUnder100?: boolean;
};

export function formatTokensAbbrev(n?: number | null, opts?: FormatAbbrevOptions): string {
  const num = typeof n === 'number' && isFinite(n) ? n : 0;
  const sign = num < 0 ? '-' : '';
  const v = Math.abs(num);

  const fmtInt = (x: number) => new Intl.NumberFormat('en-US', { maximumFractionDigits: 0 }).format(x);

  if (v < 1000) {
    if (opts?.tpsMode) {
      const decimals = opts.extraDecimalUnder100 && v < 100 ? 2 : 1;
      const rounded = Math.round(v * 10 ** decimals) / 10 ** decimals;
      return sign + rounded.toFixed(decimals);
    }
    return sign + fmtInt(v);
  }
  const units: Array<[number, string]> = [
    [1_000_000_000_000, 'T'],
    [1_000_000_000, 'B'],
    [1_000_000, 'M'],
    [1_000, 'k'],
  ];
  for (const [div, suf] of units) {
    if (v >= div) {
      const val = v / div;
      const s = val.toFixed(1); // always show one decimal for consistency (e.g., 25.0k)
      return `${sign}${s}${suf}`;
    }
  }
  return sign + fmtInt(v);
}
