/**
 * 构建 hncore-server 到 apps/desktop/resources/bin/。
 *
 * 与 core/cmd/hncore-server/README.md §9 的构建方式一致：CGO_ENABLED=0，
 * 目标平台由参数决定（默认宿主平台）。CI 里各平台各自跑一次即可。
 *
 * 用法：
 *   node scripts/build-core.mjs                      # 宿主平台
 *   node scripts/build-core.mjs --goos windows --goarch amd64
 */
import { spawnSync } from 'node:child_process';
import { mkdirSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(fileURLToPath(new URL('..', import.meta.url)));
const coreDir = path.resolve(root, '..', '..', 'core');

const args = process.argv.slice(2);
const flag = (name) => {
  const index = args.indexOf(`--${name}`);
  return index === -1 ? undefined : args[index + 1];
};

const goos = flag('goos') ?? process.platform === 'win32' ? 'windows' : process.platform === 'darwin' ? 'darwin' : 'linux';
const goarch = flag('goarch') ?? (process.arch === 'arm64' ? 'arm64' : 'amd64');
const outName = goos === 'windows' ? 'hncore-server.exe' : 'hncore-server';
const outDir = path.join(root, 'resources', 'bin');
const outFile = path.join(outDir, outName);

mkdirSync(outDir, { recursive: true });

const result = spawnSync(
  'go',
  ['build', '-buildvcs=false', '-trimpath', '-o', outFile, './cmd/hncore-server'],
  {
    cwd: coreDir,
    stdio: 'inherit',
    env: { ...process.env, CGO_ENABLED: '0', GOOS: goos, GOARCH: goarch },
  },
);

if (result.error) {
  console.error(`[build-core] 无法执行 go：${result.error.message}`);
  console.error('[build-core] 请先安装 Go 工具链，或把已构建的二进制放到 resources/bin/。');
  process.exit(1);
}
if (result.status !== 0) process.exit(result.status ?? 1);
console.log(`[build-core] ${goos}/${goarch} → ${path.relative(root, outFile)}`);
