/**
 * 播放器门禁（ARCHITECTURE.md §6.2）：
 * `grep -rn "videojs(" src/` 必须为空 —— 防止有人照 video.js 8 教程写出必然抛
 * `VJS8_LEGACY_*` 的代码（npm 包 `video.js` 在 v10 已是墓碑包）。
 *
 * 同时禁止直接依赖 `video.js` 包本身。
 */
import { readdirSync, readFileSync, statSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(fileURLToPath(new URL('..', import.meta.url)));
const srcDir = path.join(root, 'src');

const FORBIDDEN = [
  { pattern: /\bvideojs\s*\(/, message: '调用了 v8 的命令式 videojs()' },
  { pattern: /\bregisterPlugin\s*\(/, message: '调用了 v8 的 registerPlugin()' },
  { pattern: /\bregisterComponent\s*\(/, message: '调用了 v8 的 registerComponent()' },
  { pattern: /from\s+['"]video\.js['"]/, message: "从墓碑包 'video.js' 导入" },
];

const violations = [];

function walk(dir) {
  for (const entry of readdirSync(dir)) {
    const full = path.join(dir, entry);
    if (statSync(full).isDirectory()) {
      walk(full);
      continue;
    }
    if (!/\.(ts|tsx|js|jsx|mjs|cjs)$/.test(entry)) continue;
    const lines = readFileSync(full, 'utf8').split('\n');
    lines.forEach((line, index) => {
      for (const rule of FORBIDDEN) {
        if (rule.pattern.test(line)) {
          violations.push(`${path.relative(root, full)}:${index + 1} ${rule.message}\n    ${line.trim()}`);
        }
      }
    });
  }
}

walk(srcDir);

const pkg = JSON.parse(readFileSync(path.join(root, 'package.json'), 'utf8'));
const deps = { ...pkg.dependencies, ...pkg.devDependencies };
if (deps['video.js']) violations.push(`package.json 依赖了墓碑包 video.js@${deps['video.js']}`);

if (violations.length > 0) {
  console.error('[guard-videojs8] 失败：检测到 video.js 8 写法\n');
  for (const violation of violations) console.error(`  - ${violation}`);
  console.error('\n正确写法：@videojs/react 10.x 的 createPlayer({ features }) 组合式 API。');
  process.exit(1);
}
console.log('[guard-videojs8] 通过：无 v8 写法，无 video.js 依赖。');
