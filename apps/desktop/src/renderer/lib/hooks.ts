import { useCallback, useEffect, useRef, useState } from 'react';
import type { KernelStatus } from '@shared/ipc';
import { getHnBridge } from '@shared/ipc';
import { kernelStatus, OFFLINE_STATUS, type CallResult } from './api';

/** 订阅内核状态（主进程推送 + 初次拉取）。 */
export function useKernelStatus(): KernelStatus {
  const [status, setStatus] = useState<KernelStatus>(OFFLINE_STATUS);

  useEffect(() => {
    let alive = true;
    void kernelStatus().then((value) => {
      if (alive) setStatus(value);
    });
    const bridge = getHnBridge();
    if (!bridge) return () => {
      alive = false;
    };
    const off = bridge.on('kernel-status', (next) => setStatus(next));
    return () => {
      alive = false;
      off();
    };
  }, []);

  return status;
}

/**
 * 失败态的**窄化**类型。
 *
 * `CallResult<never>` 是 `{ok:true;data:never} | {ok:false;…}` 的联合，直接取
 * `.error` / `.offline` 会报「属性不存在」。这里预先抽出失败分支，页面侧因此不必
 * 再写 `!error.ok` 这类守卫。
 */
export type CallFailure = Extract<CallResult<never>, { ok: false }>;

export interface AsyncState<T> {
  data: T | null;
  error: CallFailure | null;
  loading: boolean;
}

/**
 * 跑一次内核调用并管理 loading/error。
 *
 * `deps` 变化会重新取数；`run` 用 ref 保存，避免调用方每次渲染换函数导致死循环。
 */
export function useKernelQuery<T>(
  run: () => Promise<CallResult<T>>,
  deps: readonly unknown[],
  options: { enabled?: boolean } = {},
): AsyncState<T> & { reload: () => void } {
  const enabled = options.enabled ?? true;
  const [state, setState] = useState<AsyncState<T>>({ data: null, error: null, loading: enabled });
  const [nonce, setNonce] = useState(0);
  const runRef = useRef(run);
  runRef.current = run;

  useEffect(() => {
    if (!enabled) {
      setState({ data: null, error: null, loading: false });
      return;
    }
    let alive = true;
    setState((previous) => ({ ...previous, loading: true, error: null }));
    void runRef.current().then((result) => {
      if (!alive) return;
      if (result.ok) setState({ data: result.data, error: null, loading: false });
      else setState({ data: null, error: result, loading: false });
    });
    return () => {
      alive = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, enabled, nonce]);

  const reload = useCallback(() => setNonce((value) => value + 1), []);
  return { ...state, reload };
}
