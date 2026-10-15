import { randomUUID } from 'node:crypto';
import path from 'node:path';

import { net, protocol } from 'electron';

import type { Envelope } from '../../shared/ipc.js';
import { CoverDecoder } from './cover.js';
import { invokeKernel } from './service.js';

/**
 * 封面服务：把内核下载的本地封面（含 HEIC 转码后的 JPEG）经自定义协议
 * `hncover://` 喂给渲染层。
 *
 * 为什么不经 <img src="https://...">：封面 URL 是 HEIC（Chromium 不支持），
 * 且直接取 403。为什么用令牌而不是裸绝对路径：渲染层（页面脚本）只能
 * 拿到我们**发出去过的**令牌，不能用它去读文件系统里任意路径 ——
 * 协议处理器只认令牌表里登记过的文件。
 */

const SCHEME = 'hncover';

/** 令牌 → 本地文件绝对路径。只登记本服务产出的文件。 */
const tokens = new Map<string, string>();

let decoder: CoverDecoder | null = null;
/** 同一部剧只解码一次。 */
const inflight = new Map<string, Promise<string | null>>();

export function registerCoverScheme(): void {
  protocol.registerSchemesAsPrivileged([
    {
      scheme: SCHEME,
      privileges: { standard: true, secure: true, supportFetchAPI: true, stream: true },
    },
  ]);
}

/** app ready 后调用。 */
export function attachCoverProtocol(ffmpegPath?: string | null): void {
  decoder = new CoverDecoder(ffmpegPath);
  protocol.handle(SCHEME, async (request) => {
    const url = new URL(request.url);
    // 形态：hncover://t/<token>
    if (url.host !== 't') {
      return new Response('bad cover host', { status: 404 });
    }
    const token = url.pathname.replace(/^\/+/, '');
    const file = tokens.get(token);
    if (!file) return new Response('unknown cover token', { status: 404 });
    try {
      return net.fetch(pathToFileUrl(file));
    } catch {
      return new Response('cover unreadable', { status: 404 });
    }
  });
}

function pathToFileUrl(file: string): string {
  const encoded = file.split(path.sep).map(encodeURIComponent).join('/');
  return `file://${encoded.startsWith('/') ? '' : '/'}${encoded}`;
}

/** 取一部剧的可显示封面 URL；失败返回 null（渲染层显示占位）。 */
export async function coverUrl(drama: Record<string, unknown>): Promise<string | null> {
  if (!decoder) return null;
  const id = typeof drama?.id === 'string' ? drama.id : '';
  if (!id) return null;

  const existing = inflight.get(id);
  if (existing) return existing;

  const work = (async () => {
    // 先问内核拿本地缓存文件（prepareCover 对非 HEIC 直接返回 path）。
    const envelope = await invokeKernel<PreparedShape>('prepareCover', { drama });
    if (!envelope.ok) return null;
    const local = envelope.data?.path;
    if (!local) return null;
    let file: string;
    try {
      file = await decoder!.convert(local, drama);
    } catch {
      return null;
    }
    const token = randomUUID();
    tokens.set(token, file);
    if (tokens.size > 2048) {
      // 只保留最近的令牌；Map 迭代序即插入序。
      const stale = [...tokens.keys()].slice(0, tokens.size - 2048);
      for (const key of stale) tokens.delete(key);
    }
    return `${SCHEME}://t/${token}`;
  })();

  inflight.set(id, work);
  try {
    return await work;
  } finally {
    if (inflight.get(id) === work) inflight.delete(id);
  }
}

type PreparedShape = { path?: string; heic?: boolean; input?: string; filters?: string };
export type { Envelope };
