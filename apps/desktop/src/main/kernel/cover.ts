import { execFile } from 'node:child_process';
import { promises as fs } from 'node:fs';
import path from 'node:path';

import { invokeKernel } from './service.js';

/**
 * PC 端的 HEIC 封面解码（对应安卓端 guoapp-android lib/cover_decoder.dart）。
 *
 * 为什么需要它：站源 CDN 返回的封面是 **HEIC**（fqnovelpic.com 的
 * `...superreso-aifit:400:0.heic`），Chromium 原生不支持 HEIC；而且该 URL
 * 直接取是 403（要 x-signature 签名参数）。所以不能把远端 URL 交给 <img>。
 *
 * 链路：内核 `prepareCover` 把封面下载到本地并解出 HEIC，返回
 * {path, heic, input, filters} —— input 是解出的 HEVC 裸流临时文件、
 * filters 是方向矫正滤镜；本模块用 ffmpeg 把它转成 JPEG。
 *
 * ffmpeg 参数与缓存键**逐字对齐**安卓端实现，保证两端产物一致：
 * - 缓存键 = `<basename>-<mtime微秒>-<size>.jpg`，落在 `compatible-v1/` 子目录
 * - 转换超时 12s，产物必须是真 JPEG（FF D8 FF 头）
 */

const FFMPEG_TIMEOUT_MS = 12_000;
const MAX_COVER_PIXELS = 800;
const MAX_ALLOC = '67108864';
const PRUNE_KEEP = 96;

type PreparedCover = {
  path?: string;
  heic?: boolean;
  input?: string;
  filters?: string;
};

export class CoverDecoder {
  /** 并发去重：同一文件只转一次。 */
  private readonly pending = new Map<string, Promise<string>>();
  /** 串行尾：避免同时拉起大量 ffmpeg（与安卓端 _tail 同语义）。 */
  private tail: Promise<void> = Promise.resolve();
  private ffmpegPath: string | null;

  constructor(ffmpegPath?: string | null) {
    this.ffmpegPath = ffmpegPath ?? null;
  }

  /** 返回可直接给 <img> 的本地文件路径；失败抛错（调用方转成空态）。 */
  async convert(original: string, drama: Record<string, unknown>): Promise<string> {
    const stat = await fs.stat(original).catch(() => null);
    if (!stat || !stat.isFile() || stat.size <= 0) {
      throw new Error('海报缓存已失效，请重试');
    }
    const directory = path.join(path.dirname(original), 'compatible-v1');
    // Node 的 path 没有 basenameWithoutExtension（那是 Dart 的 API），手动截掉扩展名。
    const base = path.basename(original).replace(/\.[^.]+$/, '');
    const key = `${base}-${Math.floor(stat.mtimeMs * 1000)}-${stat.size}`;
    const target = path.join(directory, `${key}.jpg`);

    if (await isJPEG(target)) {
      await fs.utimes(target, new Date(), new Date()).catch(() => {});
      return target;
    }
    const inflight = this.pending.get(key);
    if (inflight) return inflight;
    if (this.pending.size >= 96) throw new Error('海报正在处理，请稍后重试');

    const result = this.tail.then(() => this.convertOne(original, target, drama));
    this.tail = result.then(
      () => undefined,
      () => undefined,
    );
    this.pending.set(key, result);
    try {
      return await result;
    } finally {
      if (this.pending.get(key) === result) this.pending.delete(key);
    }
  }

  private async convertOne(
    original: string,
    target: string,
    drama: Record<string, unknown>,
  ): Promise<string> {
    const envelope = await invokeKernel<PreparedCover>('prepareCover', { drama });
    if (!envelope.ok) throw new Error(envelope.error);
    const prepared = envelope.data ?? {};

    // 非 HEIC：内核已把封面下载成本地可显示文件（JPEG/PNG），直接用。
    if (prepared.heic !== true) {
      const direct = prepared.path ?? original;
      if (await isJPEG(direct)) return direct;
      // PNG/其它 Chromium 可显示的格式也直接用。
      return direct;
    }

    const input = prepared.input ?? '';
    if (!input) throw new Error('海报格式转换失败，请重试');
    const intermediate = `${target}.part`;
    const ffmpeg = this.resolveFfmpeg();
    if (!ffmpeg) throw new Error('未找到 ffmpeg，无法解码 HEIC 海报');

    const orientation = prepared.filters ?? '';
    const filters = [
      ...(orientation ? [orientation] : []),
      `scale=w='min(${MAX_COVER_PIXELS},iw)':h='min(${MAX_COVER_PIXELS},ih)':force_original_aspect_ratio=decrease`,
    ].join(',');

    await fs.mkdir(path.dirname(target), { recursive: true });
    await runFfmpeg(ffmpeg, [
      '-max_alloc', MAX_ALLOC,
      '-threads', '1',
      '-f', 'hevc',
      '-i', input,
      '-frames:v', '1',
      '-an',
      '-sn',
      '-vf', filters,
      '-filter_threads', '1',
      '-threads', '1',
      '-c:v', 'mjpeg',
      '-pix_fmt', 'yuvj420p',
      '-q:v', '3',
      '-f', 'image2',
      intermediate,
    ], FFMPEG_TIMEOUT_MS);

    if (!(await isJPEG(intermediate))) {
      await fs.rm(intermediate, { force: true });
      throw new Error('海报转换未生成有效图片');
    }
    await fs.rm(target, { force: true });
    await fs.rename(intermediate, target);
    await prune(path.dirname(target), target);
    return target;
  }

  private resolveFfmpeg(): string | null {
    if (this.ffmpegPath) return this.ffmpegPath;
    return process.env.HN_FFMPEG?.trim() || null;
  }
}

async function isJPEG(file: string): Promise<boolean> {
  let handle;
  try {
    handle = await fs.open(file, 'r');
  } catch {
    return false;
  }
  try {
    const header = Buffer.alloc(3);
    const { bytesRead } = await handle.read(header, 0, 3, 0);
    return (
      bytesRead === 3 && header[0] === 0xff && header[1] === 0xd8 && header[2] === 0xff
    );
  } finally {
    await handle.close();
  }
}

function runFfmpeg(ffmpeg: string, args: string[], timeoutMs: number): Promise<void> {
  return new Promise((resolve, reject) => {
    const child = execFile(
      ffmpeg,
      args,
      { timeout: timeoutMs, maxBuffer: 4 * 1024 * 1024, windowsHide: true },
      (error) => {
        if (error) reject(new Error(`ffmpeg 海报转码失败：${error.message}`));
        else resolve();
      },
    );
    // 超时后 Electron 主进程也要保证子进程真的退了（execFile 的 timeout 会发 SIGTERM）。
    child.on('error', () => {});
  });
}

async function prune(directory: string, keep: string): Promise<void> {
  const entries = await fs.readdir(directory).catch(() => []);
  const files: { file: string; mtime: number }[] = [];
  for (const entry of entries) {
    if (!entry.endsWith('.jpg') || path.join(directory, entry) === keep) continue;
    const stat = await fs.stat(path.join(directory, entry)).catch(() => null);
    if (stat?.isFile()) files.push({ file: path.join(directory, entry), mtime: stat.mtimeMs });
  }
  if (files.length < PRUNE_KEEP) return;
  files.sort((a, b) => b.mtime - a.mtime);
  for (const stale of files.slice(PRUNE_KEEP)) {
    await fs.rm(stale.file, { force: true }).catch(() => {});
  }
}
