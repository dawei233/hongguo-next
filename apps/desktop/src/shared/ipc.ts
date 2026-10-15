/**
 * 跨进程线上契约：内核信封 + 桥接层（main）对渲染进程暴露的面。
 *
 * 内核信封的形状直接来自 `contracts/bridge-api.schema.json` 的 `$defs/okEnvelope`
 * 与 `$defs/errorEnvelope`（两个信封都允许契约未列的扩展字段，`id` 就是其中之一，
 * 见 core/cmd/hncore-server/README.md §3.2）。本文件只做类型描述，不做翻译：
 * 桥接层不得改写 `ok/data/error/code` 的任何语义。
 */

/** 契约 `$defs/errorEnvelope.properties.code` 的枚举。 */
export const KERNEL_ERROR_CODES = [
  'invalid_request',
  'not_ready',
  'source_unsupported',
  'source_blocked',
  'local_media',
  'expired',
  'upstream_error',
] as const;

export type KernelErrorCode = (typeof KERNEL_ERROR_CODES)[number];

/**
 * 桥接层自己判定、并在契约枚举之外新增的码。
 *
 * 依据 hncore-server/README.md §7.2 的建议（错误码枚举缺 bridge 层三档）与本仓
 * 「两端一致」的原则，这里**不**发明新字符串，而是复用契约里语义最近的兜底码：
 * - 请求超时、服务繁忙 → `upstream_error`（服务端自己也是这么借用的）
 * - 子进程未就绪 / 已退出 → `not_ready`（语义即「内核尚未初始化」）
 * 其余桥接层错误（帧畸形、二进制缺失）一律 `invalid_request`。
 * 真正的三档枚举扩展需要 ADR + core 侧同步，见 README §7.2 的「建议」。
 */
export type BridgeFailureReason =
  | 'kernel-not-running'
  | 'kernel-starting'
  | 'kernel-crashed'
  | 'request-timeout'
  | 'spawn-failed'
  | 'binary-missing'
  | 'malformed-response';

export interface OkEnvelope<T = unknown> {
  ok: true;
  data: T;
  id?: string | number;
  /** 内核允许扩展字段（向前兼容），原样透传。 */
  [extension: string]: unknown;
}

export interface ErrorEnvelope {
  ok: false;
  /** 面向用户的中文文案，仅供展示；机读判据一律用 code。 */
  error: string;
  code?: KernelErrorCode;
  /** 仅 code=source_blocked 时出现，RFC3339（UTC）。 */
  retryAt?: string | null;
  id?: string | number;
  /** 桥接层诊断字段（内核不会产出）；渲染层只用于日志展示。 */
  bridge?: BridgeFailureReason;
  [extension: string]: unknown;
}

export type Envelope<T = unknown> = OkEnvelope<T> | ErrorEnvelope;

/** 主进程持有的内核子进程状态。 */
export type KernelState = 'stopped' | 'starting' | 'ready' | 'restarting' | 'failed';

export interface KernelStatus {
  state: KernelState;
  /** 当前子进程 pid；未运行时为 null。 */
  pid: number | null;
  /** 已发生的意外退出次数（崩溃重启计数）。 */
  restarts: number;
  /** 内核 `initialize` 是否已成功返回。 */
  initialized: boolean;
  /** 内核版本（来自 initialize 响应）；未知为 null。 */
  coreVersion: string | null;
  /** 解析后的子进程可执行文件路径。 */
  binaryPath: string;
  /** 二进制是否存在。缺失时渲染层必须走空态，不得假装有数据。 */
  binaryAvailable: boolean;
  /** 最近一次失败文案（面向用户，中文）。 */
  lastError: string | null;
  /** 在途请求数（桥接层视角）。 */
  inflight: number;
}

/** 渲染进程 `window.hn.invoke` 的入参。 */
export interface InvokeOptions {
  /** 本请求的等待上限（毫秒）。省略则用主进程默认值。 */
  timeoutMs?: number;
  /** 透传给内核的额外字段（如 danmaku 的 playbackSession/startMs/durationMs）。 */
  payload?: Record<string, unknown>;
}

/** 主进程 → 渲染进程的推送事件。 */
export interface HnEvents {
  'kernel-status': KernelStatus;
  'window-state': WindowState;
}

export interface WindowState {
  maximized: boolean;
  fullScreen: boolean;
  focused: boolean;
}

/** preload 通过 contextBridge 暴露的完整面（`window.hn`）。 */
export interface HnBridge {
  /** 桥接层版本，用于渲染层做兼容判断。 */
  readonly bridgeVersion: string;
  readonly platform: NodeJS.Platform;
  /** 内核二进制路径（诊断用，只读）。 */
  readonly coreBinaryPath: string;

  /** 发一条内核请求。永不 reject：失败也以 ErrorEnvelope 返回。 */
  invoke<T = unknown>(action: string, options?: InvokeOptions): Promise<Envelope<T>>;
  /** 读当前内核状态（不触发启动）。 */
  status(): Promise<KernelStatus>;
  /** 手动重启内核子进程。 */
  restart(): Promise<Envelope<KernelStatus>>;
  /** 应用退出前优雅停掉内核（主进程也会在 before-quit 里做一次）。 */
  shutdownKernel(): Promise<void>;

  /** 订阅推送事件，返回取消订阅函数。 */
  on<K extends keyof HnEvents>(event: K, listener: (payload: HnEvents[K]) => void): () => void;

  window: {
    minimize(): Promise<void>;
    toggleMaximize(): Promise<WindowState>;
    close(): Promise<void>;
    setFullScreen(full: boolean): Promise<WindowState>;
    getState(): Promise<WindowState>;
  };

  /** 用系统浏览器打开外链（http/https only）。 */
  openExternal(url: string): Promise<boolean>;
}

/** 渲染进程读取 window.hn 的唯一入口，非 Electron 环境（vitest/jsdom）返回 null。 */
export function getHnBridge(): HnBridge | null {
  const candidate = (globalThis as { hn?: HnBridge }).hn;
  return candidate ?? null;
}

/** IPC 通道名集中在此，避免主/预加载两侧写错字符串。 */
export const IPC = {
  invoke: 'hn:invoke',
  status: 'hn:status',
  restart: 'hn:restart',
  shutdownKernel: 'hn:shutdown-kernel',
  windowMinimize: 'hn:window:minimize',
  windowToggleMaximize: 'hn:window:toggle-maximize',
  windowClose: 'hn:window:close',
  windowSetFullScreen: 'hn:window:set-full-screen',
  windowGetState: 'hn:window:get-state',
  openExternal: 'hn:open-external',
  eventKernelStatus: 'hn:event:kernel-status',
  eventWindowState: 'hn:event:window-state',
} as const;

export type IpcChannel = (typeof IPC)[keyof typeof IPC];
