/**
 * 令牌守卫（DESIGN-SYSTEM.md §7.3）：
 * 渲染层的样式必须只从 tokens.css 的 CSS 变量取，不允许写死颜色 / 字号 / 圆角字面量。
 *
 * 检查范围：src/renderer 下除 styles/ 以外的 .ts/.tsx（styles/ 归另一位工程师，
 * 本脚本不碰它的内容，只在末尾校验它导出的必需令牌名是否存在）。
 */
import { readdirSync, readFileSync, statSync, existsSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(fileURLToPath(new URL('..', import.meta.url)));
const rendererDir = path.join(root, 'src', 'renderer');
const stylesDir = path.join(rendererDir, 'styles');

const RULES = [
  { pattern: /#[0-9a-fA-F]{3,8}\b/, message: '十六进制颜色字面量' },
  { pattern: /\brgba?\(\s*\d/, message: 'rgb()/rgba() 颜色字面量' },
  { pattern: /\bhsla?\(\s*[\d.]/, message: 'hsl()/hsla() 颜色字面量' },
  { pattern: /text-\[\d+(\.\d+)?(px|rem)\]/, message: '任意值字号（应使用 text-xs/sm/base… 令牌档）' },
  { pattern: /rounded-\[\d+(\.\d+)?(px|rem)\]/, message: '任意值圆角（应使用 rounded-sm/md/lg… 令牌档）' },
  { pattern: /\bfont-size:\s*\d/, message: '写死 font-size' },
];

/** 允许的例外：注释行、CSS 变量引用、播放器契约里 video.js 自己的 --media-* 覆写。 */
const ALLOW = [
  /^\s*(\/\/|\*|\/\*)/,
  /var\(--/,
  /--media-/,
  /oklch\(/,
  /\/\*\s*hn-tokens-allow\s*\*\//,
];

const violations = [];

function walk(dir) {
  for (const entry of readdirSync(dir)) {
    const full = path.join(dir, entry);
    if (full.startsWith(stylesDir)) continue;
    if (statSync(full).isDirectory()) {
      walk(full);
      continue;
    }
    if (!/\.(ts|tsx)$/.test(entry)) continue;
    const lines = readFileSync(full, 'utf8').split('\n');
    lines.forEach((line, index) => {
      if (ALLOW.some((allowed) => allowed.test(line))) return;
      for (const rule of RULES) {
        if (rule.pattern.test(line)) {
          violations.push(`${path.relative(root, full)}:${index + 1} ${rule.message}\n    ${line.trim()}`);
        }
      }
    });
  }
}

if (existsSync(rendererDir)) walk(rendererDir);

// tokens.css 是否就位（它由另一位工程师产出；缺失不算本脚本失败，但要明确报告）。
/**
 * 令牌文件的两个可能位置，与 vite.config.ts 的 tokensResolver **同一份候选顺序**：
 * DESIGN-SYSTEM.md §2.1 把 styles/ 画在 src/renderer/ 下，而实际落地产出的是
 * src/styles/tokens.css。只认其中一个会让守卫静默跳过校验（假绿），所以两处都查。
 */
const TOKEN_CANDIDATES = [
  path.join(stylesDir, 'tokens.css'),
  path.join(root, 'src', 'styles', 'tokens.css'),
];
const tokensCss = TOKEN_CANDIDATES.find((candidate) => existsSync(candidate)) ?? null;
const REQUIRED_TOKENS = [
  '--background',
  '--card',
  '--surface',
  '--foreground',
  '--foreground-muted',
  '--primary',
  '--primary-subtle',
  '--border',
  '--border-subtle',
  '--radius',
  '--stage',
  '--danmaku',
];

let tokensNote = '';
if (!tokensCss) {
  tokensNote = `tokens.css 尚未就位（已查 ${TOKEN_CANDIDATES.map((c) => path.relative(root, c)).join(' / ')}），跳过令牌存在性校验。`;
} else {
  const css = readFileSync(tokensCss, 'utf8');
  const missing = REQUIRED_TOKENS.filter((token) => !css.includes(token));
  if (missing.length > 0) {
    violations.push(
      `${path.relative(root, tokensCss)} 缺少必需令牌：${missing.join(', ')}`,
    );
  } else {
    tokensNote = `tokens.css 已就位（${path.relative(root, tokensCss)}），${REQUIRED_TOKENS.length} 个必需令牌全部存在。`;
  }
}

if (violations.length > 0) {
  console.error('[guard-tokens] 失败：渲染层出现写死的设计字面量\n');
  for (const violation of violations) console.error(`  - ${violation}`);
  process.exit(1);
}
console.log(`[guard-tokens] 通过：无写死的颜色/字号/圆角字面量。${tokensNote}`);
