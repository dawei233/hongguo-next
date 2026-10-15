/**
 * 主进程 + preload 打包。
 *
 * 为什么用 esbuild 而不是 tsc 直接产物：
 * preload 在 `sandbox: true` 下运行，Electron 的沙箱化 preload **只能** require
 * electron 与少数内置模块，**不能 require 本地文件**。tsc 产物会把
 * `../shared/ipc.js` 留成运行时 require，一开沙箱就崩。所以这里把两边都打成单文件，
 * 只把 `electron` 留作 external。类型检查仍由 `tsc -p tsconfig.main.json --noEmit` 负责。
 */
import { build } from 'esbuild';
import { cp, mkdir, rm } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(fileURLToPath(new URL('..', import.meta.url)));

const shared = {
  bundle: true,
  platform: 'node',
  target: 'node22',
  format: 'cjs',
  external: ['electron'],
  sourcemap: true,
  logLevel: 'info',
  define: { 'process.env.NODE_ENV': JSON.stringify(process.env.NODE_ENV ?? 'production') },
};

await rm(path.join(root, 'dist', 'main'), { recursive: true, force: true });
await rm(path.join(root, 'dist', 'preload'), { recursive: true, force: true });

await build({
  ...shared,
  entryPoints: [path.join(root, 'src', 'main', 'index.ts')],
  outfile: path.join(root, 'dist', 'main', 'index.js'),
});

await build({
  ...shared,
  entryPoints: [path.join(root, 'src', 'preload', 'index.ts')],
  outfile: path.join(root, 'dist', 'preload', 'index.js'),
});

// 静态资源（如果有）随主进程一起复制。
const staticDir = path.join(root, 'src', 'main', 'static');
await mkdir(path.join(root, 'dist', 'main'), { recursive: true });
await cp(staticDir, path.join(root, 'dist', 'main'), { recursive: true }).catch(() => undefined);

console.log('[build-main] dist/main/index.js + dist/preload/index.js');
