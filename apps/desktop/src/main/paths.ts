import { existsSync } from 'node:fs';
import path from 'node:path';
import { app } from 'electron';

/**
 * hncore-server 子进程的定位。
 *
 * 三种情形，按优先级：
 * 1. 环境变量 `HN_CORE_BINARY`（开发/CI 显式指定，也用于把测试指向假进程）；
 * 2. 打包后：`process.resourcesPath/bin/hncore-server[.exe]`（electron-builder.yml 的
 *    extraResources 把它放到 resources/bin，asarUnpack 保证它是真实文件而非 asar 内条目）；
 * 3. 开发态：`apps/desktop/resources/bin/hncore-server[.exe]`（scripts/build-core.mjs 产出）。
 *
 * 本模块只做路径解析，不 spawn —— 缺文件时由调用方降级为空态。
 */
export interface CorePaths {
  binaryPath: string;
  binaryAvailable: boolean;
  /** 内核 `initialize` 要用的数据目录（内核会在此建引擎）。 */
  dataDir: string;
  /** 解析来源，用于诊断面板展示。 */
  origin: 'env' | 'packaged' | 'dev';
}

const BINARY_NAME = process.platform === 'win32' ? 'hncore-server.exe' : 'hncore-server';

export function resolveCorePaths(): CorePaths {
  const dataDir = path.join(app.getPath('userData'), 'core');

  const fromEnv = process.env.HN_CORE_BINARY?.trim();
  if (fromEnv) {
    return { binaryPath: fromEnv, binaryAvailable: existsSync(fromEnv), dataDir, origin: 'env' };
  }

  if (app.isPackaged) {
    const packaged = path.join(process.resourcesPath, 'bin', BINARY_NAME);
    return { binaryPath: packaged, binaryAvailable: existsSync(packaged), dataDir, origin: 'packaged' };
  }

  // __dirname 在 dist/main 下；仓库布局是 apps/desktop/{dist/main, resources/bin}。
  const dev = path.resolve(__dirname, '..', '..', 'resources', 'bin', BINARY_NAME);
  return { binaryPath: dev, binaryAvailable: existsSync(dev), dataDir, origin: 'dev' };
}

/** 渲染进程加载的入口 HTML（dev 走 vite server，prod 走打包产物）。 */
export function resolveRendererEntry(): { kind: 'url'; value: string } | { kind: 'file'; value: string } {
  const devServer = process.env.VITE_DEV_SERVER_URL?.trim() ?? process.env.HN_DEV_SERVER_URL?.trim();
  if (devServer) return { kind: 'url', value: devServer };
  return { kind: 'file', value: path.resolve(__dirname, '..', 'renderer', 'index.html') };
}
