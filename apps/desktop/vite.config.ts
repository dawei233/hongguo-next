import { defineConfig, type Plugin } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';
import { existsSync, readFileSync } from 'node:fs';
import { fileURLToPath, URL } from 'node:url';

const rendererRoot = fileURLToPath(new URL('./src/renderer', import.meta.url));

/**
 * 设计令牌解析器。
 *
 * 背景：`src/renderer/styles/` 由另一位工程师并行产出（docs/DESIGN-SYSTEM.md §2.1 把
 * `styles/` 放在 `src/renderer/` 下，而任务描述写的是 `apps/desktop/src/styles/`）。
 * 两条路径都可能，本插件按优先级解析，并在两处都没有时给出**响亮但可构建**的降级：
 *
 * 1. `src/renderer/styles/tokens.css`（DESIGN-SYSTEM.md §2.1 的布局）
 * 2. `src/styles/tokens.css`（任务描述里的目录）
 * 3. 都没有 → 虚拟空模块 + 构建告警。此时应用能构建、能跑，但没有令牌，
 *    颜色/圆角会退回 Tailwind 默认值 —— 这是「令牌未就位」的显式降级，
 *    不是「构建成功」的假象：告警会打印在每次构建输出里。
 *
 * 令牌一旦落地，本插件自动透传真实文件，不需要改任何代码。
 */
function tokensResolver(): Plugin {
  const candidates = [
    fileURLToPath(new URL('./src/renderer/styles/tokens.css', import.meta.url)),
    fileURLToPath(new URL('./src/styles/tokens.css', import.meta.url)),
  ];
  const virtualId = '\0virtual:hn-tokens';

  return {
    name: 'hn-tokens-resolver',
    enforce: 'pre',
    resolveId(source, importer) {
      if (!importer) return null;
      // 只接管「以 styles/tokens.css 结尾」的相对引用，避免误伤其它导入。
      if (!/styles\/tokens\.css$/.test(source) && source !== './styles/tokens.css') return null;
      const found = candidates.find((candidate) => existsSync(candidate));
      return found ?? virtualId;
    },
    load(id) {
      if (id !== virtualId) return null;
      const message =
        '[hn-tokens] 未找到设计令牌 tokens.css（已尝试 src/renderer/styles/ 与 src/styles/）。' +
        '本次构建使用空令牌降级：应用可运行，但颜色/圆角/字号退回 Tailwind 默认值。' +
        '令牌文件落地后本降级自动失效。';
      this.warn(message);
      console.warn(`\n\x1b[33m${message}\x1b[0m\n`);
      return '/* hn-tokens fallback: tokens.css 尚未就位 */\n';
    },
  };
}

/** 构建期把「令牌是否就位」写进产物，便于事后核对用的是哪一份。 */
function tokensProbe(): Plugin {
  return {
    name: 'hn-tokens-probe',
    buildStart() {
      const found = [
        fileURLToPath(new URL('./src/renderer/styles/tokens.css', import.meta.url)),
        fileURLToPath(new URL('./src/styles/tokens.css', import.meta.url)),
      ].find((candidate) => existsSync(candidate));
      if (found) {
        const css = readFileSync(found, 'utf8');
        // 用字节数而不是 css.length：中文注释多，字符数会比字节数少一截，容易误判。
        console.log(`[hn-tokens] 使用 ${found}（${Buffer.byteLength(css, 'utf8')} 字节）`);
      }
    },
  };
}

/**
 * 渲染进程构建。
 *
 * - `base: './'`：Electron 用 file:// 加载 dist/renderer/index.html，绝对路径会 404。
 * - 输出到 dist/renderer，主进程产物在 dist/main（见 scripts/build-main.mjs）。
 * - Tailwind v4 走 CSS-first：没有 tailwind.config.js，配置全在 CSS 里。
 */
export default defineConfig({
  root: rendererRoot,
  base: './',
  plugins: [tokensResolver(), tokensProbe(), react(), tailwindcss()],
  resolve: {
    alias: {
      '@': rendererRoot,
      '@shared': fileURLToPath(new URL('./src/shared', import.meta.url)),
    },
  },
  build: {
    outDir: fileURLToPath(new URL('./dist/renderer', import.meta.url)),
    emptyOutDir: true,
    sourcemap: true,
    target: 'chrome130',
    rollupOptions: {
      output: {
        // 播放器单独成 chunk：它比应用其余部分大一个量级，分开利于缓存与排查体积。
        manualChunks: {
          player: ['@videojs/react', '@videojs/hlsjs-video'],
          react: ['react', 'react-dom', 'react-router-dom'],
        },
      },
    },
  },
  server: {
    port: 5273,
    strictPort: true,
  },
});
