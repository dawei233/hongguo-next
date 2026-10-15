import { app, BrowserWindow } from 'electron';
import type { Envelope, KernelStatus } from '../../shared/ipc.js';
import { IPC } from '../../shared/ipc.js';
import { KernelClient, type KernelLogger } from './client.js';
import { resolveCorePaths } from '../paths.js';

/**
 * 主进程侧的内核单例：把 KernelClient 的生命周期挂到 Electron app 上，
 * 并把状态变化广播给所有窗口（渲染层据此显示「内核未运行」空态）。
 */

const logger: KernelLogger = {
  info: (message, meta) => console.log(`[hncore] ${message}`, meta ?? ''),
  warn: (message, meta) => console.warn(`[hncore] ${message}`, meta ?? ''),
  error: (message, meta) => console.error(`[hncore] ${message}`, meta ?? ''),
};

let client: KernelClient | null = null;

export function getKernelClient(): KernelClient {
  if (client) return client;
  const paths = resolveCorePaths();
  client = new KernelClient({
    binaryPath: paths.binaryPath,
    dataDir: paths.dataDir,
    logger,
  });
  client.on('status', (status: KernelStatus) => broadcast(IPC.eventKernelStatus, status));
  return client;
}

export function broadcast(channel: string, payload: unknown): void {
  for (const win of BrowserWindow.getAllWindows()) {
    if (!win.isDestroyed()) win.webContents.send(channel, payload);
  }
}

/** app ready 后调用：懒启动内核，但**不阻塞**窗口显示。 */
export function startKernel(): void {
  const kernel = getKernelClient();
  const status = kernel.getStatus();
  if (!status.binaryAvailable) {
    // 二进制缺失是「已知可解释的降级」，不是崩溃：只记录，不重试。
    logger.warn('hncore-server 不存在，进入空态模式', { path: status.binaryPath });
    broadcast(IPC.eventKernelStatus, status);
    return;
  }
  void kernel.start().catch((error: unknown) => {
    logger.error('内核启动失败', { error: error instanceof Error ? error.message : String(error) });
  });
}

export async function stopKernel(): Promise<void> {
  if (!client) return;
  await client.stop();
}

export async function invokeKernel<T = unknown>(
  action: string,
  payload: Record<string, unknown> = {},
  timeoutMs?: number,
): Promise<Envelope<T>> {
  const kernel = getKernelClient();
  return kernel.invoke<T>(action, payload, timeoutMs);
}

/** 应用退出前把内核收干净；`before-quit` 里 await 用。 */
export function registerKernelLifecycle(): void {
  app.on('before-quit', (event) => {
    const kernel = client;
    if (!kernel) return;
    const status = kernel.getStatus();
    if (status.state === 'stopped' || status.state === 'failed') return;
    // 只拦一次：第二次 before-quit 直接放行，避免内核卡死时应用无法退出。
    if (quitting) return;
    event.preventDefault();
    quitting = true;
    void stopKernel().finally(() => app.quit());
  });
}

let quitting = false;
