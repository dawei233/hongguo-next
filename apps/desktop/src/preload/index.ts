import { contextBridge, ipcRenderer } from 'electron';
import type {
  Envelope,
  HnBridge,
  HnEvents,
  InvokeOptions,
  KernelStatus,
  WindowState,
} from '../shared/ipc.js';
import { IPC } from '../shared/ipc.js';

/**
 * preload：**唯一**暴露给渲染进程的面。
 *
 * 安全设置（见 main/index.ts）：`nodeIntegration: false`、`contextIsolation: true`、
 * `sandbox: true`。因此这里只能用 `contextBridge` + `ipcRenderer`，且只暴露白名单方法——
 * 不暴露 `ipcRenderer` 本体，渲染层无法向任意通道发消息。
 */

const BRIDGE_VERSION = '0.1.0';

/** 允许渲染层请求的动作白名单（内核 nativeInput.action 面）。 */
const ALLOWED_ACTIONS = new Set([
  'initialize',
  'catalog',
  'cached',
  'categories',
  'recommendations',
  'cachedRecommendations',
  'suggestions',
  'rankingBoards',
  'rankings',
  'detail',
  'metadata',
  'cover',
  'prepareCover',
  'resolve',
  'fallback',
  'preload',
  'prepareHandoff',
  'localPlayback',
  'danmaku',
  'cancelRead',
  'resourceSettings',
  'saveResourceSettings',
  'sourceStatus',
  'sourceJob',
  'cancelSourceJob',
  'downloads',
  'enqueueDownloads',
  'controlDownloads',
  'downloadDirectory',
  'storage',
  'workLease',
]);

function invoke<T>(action: string, options?: InvokeOptions): Promise<Envelope<T>> {
  if (!ALLOWED_ACTIONS.has(action)) {
    return Promise.resolve({
      ok: false,
      error: `不支持的桥接动作：${action}`,
      code: 'invalid_request',
      bridge: 'kernel-not-running',
    } as Envelope<T>);
  }
  return ipcRenderer.invoke(IPC.invoke, action, options ?? {}) as Promise<Envelope<T>>;
}

function subscribe<K extends keyof HnEvents>(event: K, listener: (payload: HnEvents[K]) => void): () => void {
  const channel = event === 'kernel-status' ? IPC.eventKernelStatus : IPC.eventWindowState;
  const wrapped = (_event: unknown, payload: HnEvents[K]) => listener(payload);
  ipcRenderer.on(channel, wrapped);
  return () => {
    ipcRenderer.removeListener(channel, wrapped);
  };
}

const bridge: HnBridge = {
  bridgeVersion: BRIDGE_VERSION,
  platform: process.platform,
  coreBinaryPath: process.env.HN_CORE_BINARY ?? '',

  invoke,
  status: () => ipcRenderer.invoke(IPC.status) as Promise<KernelStatus>,
  restart: () => ipcRenderer.invoke(IPC.restart) as Promise<Envelope<KernelStatus>>,
  shutdownKernel: () => ipcRenderer.invoke(IPC.shutdownKernel) as Promise<void>,

  on: subscribe,

  window: {
    minimize: () => ipcRenderer.invoke(IPC.windowMinimize) as Promise<void>,
    toggleMaximize: () => ipcRenderer.invoke(IPC.windowToggleMaximize) as Promise<WindowState>,
    close: () => ipcRenderer.invoke(IPC.windowClose) as Promise<void>,
    setFullScreen: (full: boolean) => ipcRenderer.invoke(IPC.windowSetFullScreen, full) as Promise<WindowState>,
    getState: () => ipcRenderer.invoke(IPC.windowGetState) as Promise<WindowState>,
  },

  openExternal: (url: string) => ipcRenderer.invoke(IPC.openExternal, url) as Promise<boolean>,
};

contextBridge.exposeInMainWorld('hn', bridge);
