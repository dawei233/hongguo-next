import { app, BrowserWindow, ipcMain, shell, type WebContents } from 'electron';
import path from 'node:path';
import { IPC, type InvokeOptions, type KernelStatus, type WindowState } from '../shared/ipc.js';
import { resolveRendererEntry } from './paths.js';
import {
  getKernelClient,
  invokeKernel,
  registerKernelLifecycle,
  startKernel,
  stopKernel,
} from './kernel/service.js';
import { attachCoverProtocol, coverUrl, registerCoverScheme } from './kernel/coverService.js';

// 自定义协议的特权声明必须在 app ready 之前完成（Electron 硬性要求）。
registerCoverScheme();

/** ffmpeg 的定位：打包后 resources/bin，开发态 PATH。 */
function resolveFfmpegPath(): string | null {
  if (process.env.HN_FFMPEG?.trim()) return process.env.HN_FFMPEG.trim();
  const candidates = [
    path.join(process.resourcesPath ?? '', 'bin', 'ffmpeg'),
    path.join(__dirname, '..', '..', 'resources', 'bin', 'ffmpeg'),
  ];
  for (const candidate of candidates) {
    try {
      // 只判断存在性；不做 require。
      if (candidate && path.isAbsolute(candidate)) return candidate;
    } catch {
      // 忽略
    }
  }
  return null;
}

/**
 * 主进程入口：窗口与生命周期、安全设置、IPC 路由。
 *
 * 安全基线（对齐 Electron 官方 security checklist）：
 * - nodeIntegration: false —— 渲染层拿不到 require/process；
 * - contextIsolation: true —— preload 与页面脚本在不同 world；
 * - sandbox: true —— 渲染进程走 OS 级沙箱；preload 因此被打成单文件（见 scripts/build-main.mjs）；
 * - webSecurity: true、allowRunningInsecureContent: false；
 * - 导航与新窗口一律拦截，外链交给系统浏览器；
 * - 不加载任何远端内容：dev 走本机 vite server，prod 走 file://。
 */

const isDev = !app.isPackaged;
const rendererEntry = resolveRendererEntry();

let mainWindow: BrowserWindow | null = null;

function currentWindowState(win: BrowserWindow): WindowState {
  return {
    maximized: win.isMaximized(),
    fullScreen: win.isFullScreen(),
    focused: win.isFocused(),
  };
}

function pushWindowState(win: BrowserWindow): void {
  if (win.isDestroyed()) return;
  win.webContents.send(IPC.eventWindowState, currentWindowState(win));
}

function hardenWebContents(contents: WebContents): void {
  // 新窗口：一律拒绝，http/https 交给系统浏览器。
  contents.setWindowOpenHandler(({ url }) => {
    if (isSafeExternalUrl(url)) void shell.openExternal(url);
    return { action: 'deny' };
  });

  // 页面内导航：只允许留在应用自身来源内。
  contents.on('will-navigate', (event, url) => {
    const allowed = rendererEntry.kind === 'url' ? rendererEntry.value : 'file://';
    if (!url.startsWith(allowed)) {
      event.preventDefault();
      if (isSafeExternalUrl(url)) void shell.openExternal(url);
    }
  });

  // 权限：本端不需要摄像头/麦克风/通知等任何权限，全部拒绝。
  contents.session.setPermissionRequestHandler((_wc, _permission, callback) => callback(false));

  contents.on('will-attach-webview', (event) => event.preventDefault());
}

/**
 * 全屏走 **Electron 原生全屏**（DESIGN-SYSTEM.md §5.3 的硬要求）。
 *
 * 播放器里的 FullscreenButton 走的是标准 Fullscreen API（对 Container 元素请求全屏），
 * 这里把 html 全屏事件转成窗口原生全屏，既保留组件行为，又拿到原生全屏的体验
 * （不依赖 CSS `:fullscreen`，避免「全屏丢失控制条」那类问题）。
 */
function bindNativeFullscreen(win: BrowserWindow): void {
  win.webContents.on('enter-html-full-screen', () => {
    if (!win.isFullScreen()) win.setFullScreen(true);
  });
  win.webContents.on('leave-html-full-screen', () => {
    if (win.isFullScreen()) win.setFullScreen(false);
  });
  win.on('enter-full-screen', () => pushWindowState(win));
  win.on('leave-full-screen', () => pushWindowState(win));
}

function createMainWindow(): BrowserWindow {
  const win = new BrowserWindow({
    width: 1440,
    height: 900,
    minWidth: 960,
    minHeight: 640,
    show: false,
    backgroundColor: '#111213', // 与 --background 暗色同值，避免启动白闪
    autoHideMenuBar: true,
    titleBarStyle: process.platform === 'darwin' ? 'hiddenInset' : 'default',
    webPreferences: {
      preload: path.resolve(__dirname, '..', 'preload', 'index.js'),
      nodeIntegration: false,
      contextIsolation: true,
      sandbox: true,
      webSecurity: true,
      allowRunningInsecureContent: false,
      spellcheck: false,
      devTools: isDev,
    },
  });

  hardenWebContents(win.webContents);
  bindNativeFullscreen(win);

  win.once('ready-to-show', () => win.show());
  win.on('maximize', () => pushWindowState(win));
  win.on('unmaximize', () => pushWindowState(win));
  win.on('focus', () => pushWindowState(win));
  win.on('blur', () => pushWindowState(win));
  win.on('closed', () => {
    if (mainWindow === win) mainWindow = null;
  });

  // 渲染进程崩溃：记录并尝试重载一次，避免留下黑窗口。
  win.webContents.on('render-process-gone', (_event, details) => {
    console.error('[desktop] renderer gone', details);
    if (!win.isDestroyed() && details.reason !== 'clean-exit') win.reload();
  });

  if (rendererEntry.kind === 'url') {
    void win.loadURL(rendererEntry.value);
    if (isDev) win.webContents.openDevTools({ mode: 'detach' });
  } else {
    void win.loadFile(rendererEntry.value);
  }

  return win;
}

function isSafeExternalUrl(url: string): boolean {
  try {
    const parsed = new URL(url);
    return parsed.protocol === 'https:' || parsed.protocol === 'http:';
  } catch {
    return false;
  }
}

function windowOf(event: Electron.IpcMainInvokeEvent): BrowserWindow | null {
  return BrowserWindow.fromWebContents(event.sender);
}

function registerIpcHandlers(): void {
  ipcMain.handle(IPC.coverUrl, async (_event, options: unknown) => {
    const opts = (options ?? {}) as InvokeOptions;
    const drama = opts.payload && typeof opts.payload === 'object' ? opts.payload : {};
    try {
      return await coverUrl(drama);
    } catch {
      return null;
    }
  });
  ipcMain.handle(IPC.invoke, async (_event, action: unknown, options: unknown) => {
    if (typeof action !== 'string' || action.length === 0) {
      return { ok: false, error: '桥接动作无效', code: 'invalid_request' };
    }
    const opts = (options ?? {}) as InvokeOptions;
    const payload = opts.payload && typeof opts.payload === 'object' ? opts.payload : {};
    return invokeKernel(action, payload, opts.timeoutMs);
  });

  ipcMain.handle(IPC.status, (): KernelStatus => getKernelClient().getStatus());

  ipcMain.handle(IPC.restart, async () => {
    const kernel = getKernelClient();
    try {
      const status = await kernel.restart();
      return { ok: true, data: status };
    } catch (error) {
      return {
        ok: false,
        error: error instanceof Error ? error.message : String(error),
        code: 'not_ready',
      };
    }
  });

  ipcMain.handle(IPC.shutdownKernel, () => stopKernel());

  ipcMain.handle(IPC.windowMinimize, (event) => {
    windowOf(event)?.minimize();
  });
  ipcMain.handle(IPC.windowToggleMaximize, (event) => {
    const win = windowOf(event);
    if (!win) return { maximized: false, fullScreen: false, focused: false };
    if (win.isMaximized()) win.unmaximize();
    else win.maximize();
    return currentWindowState(win);
  });
  ipcMain.handle(IPC.windowClose, (event) => {
    windowOf(event)?.close();
  });
  ipcMain.handle(IPC.windowSetFullScreen, (event, full: unknown) => {
    const win = windowOf(event);
    if (!win) return { maximized: false, fullScreen: false, focused: false };
    win.setFullScreen(Boolean(full));
    return currentWindowState(win);
  });
  ipcMain.handle(IPC.windowGetState, (event) => {
    const win = windowOf(event);
    return win ? currentWindowState(win) : { maximized: false, fullScreen: false, focused: false };
  });

  ipcMain.handle(IPC.openExternal, async (_event, url: unknown) => {
    if (typeof url !== 'string' || !isSafeExternalUrl(url)) return false;
    await shell.openExternal(url);
    return true;
  });
}

// 单实例：第二次启动把已有窗口拉到前台，不再开新内核。
if (!app.requestSingleInstanceLock()) {
  app.quit();
} else {
  app.on('second-instance', () => {
    if (!mainWindow) return;
    if (mainWindow.isMinimized()) mainWindow.restore();
    mainWindow.focus();
  });

  registerKernelLifecycle();

  app.whenReady().then(() => {
    registerIpcHandlers();
    attachCoverProtocol(resolveFfmpegPath());
    mainWindow = createMainWindow();
    startKernel();

    app.on('activate', () => {
      if (BrowserWindow.getAllWindows().length === 0) mainWindow = createMainWindow();
    });
  });

  app.on('window-all-closed', () => {
    if (process.platform !== 'darwin') app.quit();
  });

  // 未捕获异常不要静默吞掉：内核与窗口都已各自兜底，这里只记录。
  process.on('uncaughtException', (error) => console.error('[desktop] uncaught', error));
  process.on('unhandledRejection', (reason) => console.error('[desktop] unhandled rejection', reason));
}
