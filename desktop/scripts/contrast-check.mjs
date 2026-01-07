import fs from "node:fs";
import path from "node:path";

function parseCssVars(blockCss) {
  const vars = new Map();
  const re = /--([a-zA-Z0-9-_]+)\s*:\s*([^;]+);/g;
  let m = re.exec(blockCss);
  while (m !== null) {
    vars.set(`--${m[1]}`, m[2].trim());
    m = re.exec(blockCss);
  }
  return vars;
}

function escapeRegExp(s) {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function extractBlock(css, selector) {
  // Match only real blocks like ".dark { ... }", not incidental occurrences (e.g. ".dark *)").
  const sel = escapeRegExp(selector);
  const re = new RegExp(`(^|\\n)\\s*${sel}\\s*\\{`, "m");
  const m = re.exec(css);
  if (!m) return null;

  const start = css.indexOf("{", m.index);
  if (start === -1) return null;

  let depth = 0;
  for (let i = start; i < css.length; i++) {
    const ch = css[i];
    if (ch === "{") depth++;
    else if (ch === "}") depth--;
    if (depth === 0) return css.slice(start + 1, i);
  }
  return null;
}

function hexToRgb(hex) {
  const h = hex.replace("#", "").trim();
  if (h.length === 3) {
    const r = parseInt(h[0] + h[0], 16);
    const g = parseInt(h[1] + h[1], 16);
    const b = parseInt(h[2] + h[2], 16);
    return { r, g, b };
  }
  if (h.length === 6) {
    const r = parseInt(h.slice(0, 2), 16);
    const g = parseInt(h.slice(2, 4), 16);
    const b = parseInt(h.slice(4, 6), 16);
    return { r, g, b };
  }
  throw new Error(`Unsupported hex color: ${hex}`);
}

function parseColor(value) {
  const v = value.trim().toLowerCase();
  if (v === "transparent") return { r: 0, g: 0, b: 0, a: 0 };
  if (v.startsWith("#")) {
    const rgb = hexToRgb(v);
    return { ...rgb, a: 1 };
  }
  const rgba = v.match(
    /^rgba\(\s*([0-9.]+)\s*,\s*([0-9.]+)\s*,\s*([0-9.]+)\s*,\s*([0-9.]+)\s*\)$/
  );
  if (rgba) {
    return {
      r: Number(rgba[1]),
      g: Number(rgba[2]),
      b: Number(rgba[3]),
      a: Number(rgba[4]),
    };
  }
  throw new Error(`Unsupported color format: ${value}`);
}

function srgbToLinear(c) {
  const cs = c / 255;
  return cs <= 0.04045 ? cs / 12.92 : Math.pow((cs + 0.055) / 1.055, 2.4);
}

function relativeLuminance({ r, g, b }) {
  const R = srgbToLinear(r);
  const G = srgbToLinear(g);
  const B = srgbToLinear(b);
  return 0.2126 * R + 0.7152 * G + 0.0722 * B;
}

function blend(fg, bg) {
  const a = fg.a ?? 1;
  if (a >= 1) return { r: fg.r, g: fg.g, b: fg.b };
  const inv = 1 - a;
  return {
    r: fg.r * a + bg.r * inv,
    g: fg.g * a + bg.g * inv,
    b: fg.b * a + bg.b * inv,
  };
}

function contrastRatio(c1, c2) {
  const L1 = relativeLuminance(c1);
  const L2 = relativeLuminance(c2);
  const lighter = Math.max(L1, L2);
  const darker = Math.min(L1, L2);
  return (lighter + 0.05) / (darker + 0.05);
}

function fmtRatio(r) {
  return r.toFixed(2);
}

function main() {
  const root = path.resolve(path.dirname(new URL(import.meta.url).pathname), "..");
  const cssPath = path.join(root, "app", "globals.css");
  const css = fs.readFileSync(cssPath, "utf8");

  const lightBlock = extractBlock(css, ":root");
  const darkBlock = extractBlock(css, ".dark");
  if (!lightBlock || !darkBlock) {
    console.error("Failed to find :root and/or .dark blocks in app/globals.css");
    process.exit(2);
  }

  const lightVars = parseCssVars(lightBlock);
  const darkVars = parseCssVars(darkBlock);

  const checks = {
    textMin: 4.5,
    surfaceMin: 1.15,
    textPairs: [
      ["--foreground", "--background", "foreground/background"],
      ["--card-foreground", "--card", "card text/card"],
      ["--muted-foreground", "--muted", "muted text/muted"],
      ["--primary-foreground", "--primary", "primary text/primary"],
      ["--secondary-foreground", "--secondary", "secondary text/secondary"],
      ["--accent-foreground", "--accent", "accent text/accent"],
      ["--destructive-foreground", "--destructive", "destructive text/destructive"],
      ["--sidebar-foreground", "--sidebar", "sidebar text/sidebar"],
    ],
    surfacePairs: [
      ["--background", "--card", "background/card"],
      ["--background", "--sidebar", "background/sidebar"],
      ["--sidebar", "--card", "sidebar/card"],
      ["--background", "--muted", "background/muted"],
    ],
  };

  function runTheme(name, vars) {
    const bg = parseColor(vars.get("--background"));
    const bgRgb = blend(bg, { r: 0, g: 0, b: 0, a: 1 });

    let failed = false;
    const rows = [];

    for (const [fgVar, bgVar, label] of checks.textPairs) {
      const fgVal = vars.get(fgVar);
      const bgVal = vars.get(bgVar);
      if (!fgVal || !bgVal) {
        failed = true;
        rows.push({ label, ratio: "MISSING", ok: false });
        continue;
      }
      const fg = parseColor(fgVal);
      const bgc = parseColor(bgVal);
      const fgRgb = blend(fg, bgRgb);
      const bgRgb2 = blend(bgc, bgRgb);
      const ratio = contrastRatio(fgRgb, bgRgb2);
      const ok = ratio >= checks.textMin;
      if (!ok) failed = true;
      rows.push({ label, ratio: fmtRatio(ratio), ok });
    }

    for (const [aVar, bVar, label] of checks.surfacePairs) {
      const aVal = vars.get(aVar);
      const bVal = vars.get(bVar);
      if (!aVal || !bVal) {
        failed = true;
        rows.push({ label: `surface:${label}`, ratio: "MISSING", ok: false });
        continue;
      }
      const a = blend(parseColor(aVal), bgRgb);
      const b = blend(parseColor(bVal), bgRgb);
      const ratio = contrastRatio(a, b);
      const ok = ratio >= checks.surfaceMin;
      if (!ok) failed = true;
      rows.push({ label: `surface:${label}`, ratio: fmtRatio(ratio), ok });
    }

    console.log(`\\n== ${name} ==`);
    for (const r of rows) {
      console.log(`${r.ok ? "OK " : "FAIL"}  ${r.label.padEnd(26)}  ${r.ratio}`);
    }
    return !failed;
  }

  const lightOk = runTheme("light", lightVars);
  const darkOk = runTheme("dark", darkVars);

  if (!lightOk || !darkOk) {
    console.error(`\\nContrast check failed (min text: ${checks.textMin}:1, min surface: ${checks.surfaceMin}:1).`);
    process.exit(1);
  }

  console.log(`\\nAll contrast checks passed (min text: ${checks.textMin}:1, min surface: ${checks.surfaceMin}:1).`);
}

main();

