/**
 * dev 启动器：先构建主进程/preload，再把 vite dev server 地址交给 Electron。
 *
 * 不直接用 `electron .` 是因为需要注入 VITE_DEV_SERVER_URL，并且要保证
 * dist/main/index.js 存在（否则 Electron 会以 "Cannot find module" 退出）。
 */
import { spawn } from 'node:child_process';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(fileURLToPath(new URL('..', import.meta.url)));
const devServerUrl = process.env.VITE_DEV_SERVER_URL ?? 'http://localhost:5273';

await import('./build-main.mjs');

const electronBinary = (await import('electron')).default;
const child = spawn(electronBinary, ['.'], {
  cwd: root,
  stdio: 'inherit',
  env: { ...process.env, VITE_DEV_SERVER_URL: devServerUrl, NODE_ENV: 'development' },
});

child.on('exit', (code) => process.exit(code ?? 0));
