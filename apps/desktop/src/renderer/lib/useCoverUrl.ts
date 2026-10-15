import { useEffect, useState } from 'react';

import { getHnBridge } from '@shared/ipc';

/**
 * 把内核返回的封面（HEIC 远端 URL）解析成可直接给 <img> 的本地 URL。
 *
 * 为什么不直接用 drama.cover：那是 fqnovelpic.com 的 **HEIC**（Chromium 不支持），
 * 且直接取 403。主进程经内核 `prepareCover` + ffmpeg 转码后，经 `hncover://`
 * 协议喂回本地 JPEG。
 *
 * 失败（无桥 / 内核未运行 / 转码失败）一律返回 null，组件显示占位——
 * 不退回远端 URL（那会渲染成裂图，比占位更糟）。
 *
 * drama 用 unknown 而非 Record<string, unknown>：内核的 nativeDrama 是
 * TS 里的具体接口，没有字符串索引签名；主进程侧只读它的 JSON 键。
 */
export function useCoverUrl(drama: unknown): string | null {
  const [url, setUrl] = useState<string | null>(null);

  // 只有 id 变化才算新请求；对象引用变化不触发（避免每帧重新转码）。
  const id =
    drama !== null && typeof drama === 'object' && typeof (drama as { id?: unknown }).id === 'string'
      ? (drama as { id: string }).id
      : '';

  useEffect(() => {
    const bridge = getHnBridge();
    if (!drama || !id || !bridge) {
      setUrl(null);
      return;
    }
    let cancelled = false;
    void bridge
      .coverUrl(drama)
      .then((resolved: string | null) => {
        if (!cancelled) setUrl(resolved);
      });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id]);

  return url;
}
