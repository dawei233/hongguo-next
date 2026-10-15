import { getHnBridge, type Envelope, type KernelErrorCode, type KernelStatus } from '@shared/ipc';
import type {
  NativeCatalogData,
  NativeCategoriesData,
  NativeDanmakuPage,
  NativeDetailData,
  NativePlan,
} from './core-types';

/**
 * 渲染层的唯一内核入口。
 *
 * 两个硬约定：
 * 1. **永不抛异常**：任何失败都收敛成 `CallResult`，页面只需要处理 `ok` 与 `error` 两个分支。
 * 2. **优雅降级**：没有桥（纯浏览器 / vitest / preload 未注入）或内核未就绪时，
 *    返回 `offline: true` 的结果，页面据此渲染空态，而不是白屏或假的假数据。
 */

export type CallResult<T> =
  | { ok: true; data: T }
  | {
      ok: false;
      error: string;
      code?: KernelErrorCode;
      retryAt?: string | null;
      /** true = 内核根本没在跑（降级到空态）；false = 内核回了业务错误。 */
      offline: boolean;
      /** 桥接层诊断（仅 offline 时有意义）。 */
      bridge?: string;
    };

/** 内核不在跑时合成的状态；页面用它决定是否显示「内核未运行」提示。 */
export const OFFLINE_STATUS: KernelStatus = {
  state: 'failed',
  pid: null,
  restarts: 0,
  initialized: false,
  coreVersion: null,
  binaryPath: '',
  binaryAvailable: false,
  lastError: '未连接到内核（浏览器环境或 preload 未注入）',
  inflight: 0,
};

export function hasBridge(): boolean {
  return getHnBridge() !== null;
}

export async function kernelStatus(): Promise<KernelStatus> {
  const bridge = getHnBridge();
  if (!bridge) return OFFLINE_STATUS;
  try {
    return await bridge.status();
  } catch (error) {
    return { ...OFFLINE_STATUS, lastError: describe(error) };
  }
}

/** 发一条内核请求。 */
export async function call<T = unknown>(
  action: string,
  payload: Record<string, unknown> = {},
  options: { timeoutMs?: number } = {},
): Promise<CallResult<T>> {
  const bridge = getHnBridge();
  if (!bridge) {
    return {
      ok: false,
      error: '内核未连接：请在桌面应用内运行（浏览器预览没有 window.hn）',
      offline: true,
      bridge: 'no-bridge',
    };
  }
  let envelope: Envelope<T>;
  try {
    envelope = await bridge.invoke<T>(action, { ...options, payload });
  } catch (error) {
    return { ok: false, error: describe(error), offline: true, bridge: 'ipc-threw' };
  }
  if (envelope.ok) return { ok: true, data: envelope.data };
  const code = envelope.code;
  const retryAt = typeof envelope.retryAt === 'string' || envelope.retryAt === null ? envelope.retryAt : undefined;
  return {
    ok: false,
    error: envelope.error,
    ...(code ? { code } : {}),
    ...(retryAt !== undefined ? { retryAt } : {}),
    offline: envelope.ok === false && typeof envelope.bridge === 'string',
    ...(typeof envelope.bridge === 'string' ? { bridge: envelope.bridge } : {}),
  };
}

/* ── 业务动作封装（action 名逐字来自 core 的 nativeInput.action 面） ───────────── */

export function fetchCatalog(
  params: { source: string; category: string; page?: number; force?: boolean },
  options?: { timeoutMs?: number },
): Promise<CallResult<NativeCatalogData>> {
  return call<NativeCatalogData>(
    'catalog',
    {
      source: params.source,
      category: params.category,
      page: params.page ?? 1,
      ...(params.force ? { force: true } : {}),
    },
    options,
  );
}

export function fetchRecommendations(
  category: string,
  options?: { timeoutMs?: number },
): Promise<CallResult<NativeCatalogData>> {
  return call<NativeCatalogData>('recommendations', { category }, options);
}

export function fetchCategories(
  source: string,
  options?: { timeoutMs?: number },
): Promise<CallResult<NativeCategoriesData>> {
  return call<NativeCategoriesData>('categories', { source }, options);
}

/** `detail` 需要完整 drama 对象（内核用它的 id/source 定位源内剧）。 */
export function fetchDetail(
  drama: Record<string, unknown>,
  options?: { timeoutMs?: number },
): Promise<CallResult<NativeDetailData>> {
  return call<NativeDetailData>('detail', { drama }, options);
}

/** `resolve` 需要 drama + chapter + quality（像素高度）。 */
export function resolveStream(
  params: {
    drama: Record<string, unknown>;
    chapter: Record<string, unknown>;
    quality?: number;
    sequence?: number;
  },
  options?: { timeoutMs?: number },
): Promise<CallResult<NativePlan>> {
  return call<NativePlan>(
    'resolve',
    {
      drama: params.drama,
      chapter: params.chapter,
      quality: params.quality ?? 0,
      sequence: params.sequence ?? Date.now(),
    },
    options,
  );
}

/** 弹幕必须带播放会话（内核用它取 danmakuSeries/danmakuVideo）。 */
export function fetchDanmaku(
  params: { playbackSession: string; startMs: number; durationMs: number },
  options?: { timeoutMs?: number },
): Promise<CallResult<NativeDanmakuPage>> {
  return call<NativeDanmakuPage>(
    'danmaku',
    { playbackSession: params.playbackSession, startMs: params.startMs, durationMs: params.durationMs },
    options,
  );
}

function describe(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
