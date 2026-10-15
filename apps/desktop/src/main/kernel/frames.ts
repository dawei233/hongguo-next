/**
 * 帧编解码：与 core/cmd/hncore-server 的 NDJSON 约定逐条对齐。
 *
 * 依据 core/cmd/hncore-server/README.md §1（帧定界）与 §3（请求/响应）：
 *
 * | 情形 | 行为 |
 * |---|---|
 * | 载荷内含未转义裸换行 | 会被当成帧边界（两侧各自判 invalid_request）；转义 \n 不受影响 |
 * | CRLF | 行尾 \r 剥除 |
 * | UTF-8 BOM | 行首 BOM 剥除 |
 * | 空行/纯空白行 | 跳过，不产生响应 |
 * | 首尾空白、行首缩进 | 剥除后交给内核 |
 * | 最后一帧无结尾换行 | EOF 时按整帧处理 |
 *
 * 编码侧只做一件事：JSON 序列化后追加单个 `\n`。`JSON.stringify` 与 Go 的
 * `encoding/json` 一样会把 U+000A 转义成 `\n`，所以载荷内不会出现裸换行，
 * 行分隔天然无歧义（这正是服务端选 NDJSON 的理由 1）。
 */

const BOM = '\uFEFF';

/** 单帧上限，与内核 `NativeRequest` 的 1<<20 及服务端 `-max-bytes` 默认值一致。 */
export const MAX_FRAME_BYTES = 1 << 20;

export interface FrameCodecStats {
  /** 因为超过 MAX_FRAME_BYTES 被整帧丢弃的次数。 */
  oversized: number;
  /** 因为 JSON 语法错误 / 非对象被丢弃的次数。 */
  malformed: number;
  /** 被跳过的空行数。 */
  blank: number;
}

/**
 * 增量行切分器。字节级缓冲，按 `\n` 切；`\r` 与 BOM 在行内剥除。
 *
 * 注意：这里刻意**不**尝试「智能修复」裸换行造成的半帧——服务端已经把它当帧边界，
 * 两侧行为必须一致，否则错误会在两端呈现不同形态。半帧 JSON 解析失败即丢弃并计数。
 */
export class FrameDecoder {
  /** 未消费字节。类型宽化为 `Buffer`（`Buffer.concat` 返回 ArrayBufferLike 版本）。 */
  private buffer: Buffer = Buffer.alloc(0);
  private readonly maxFrameBytes: number;
  readonly stats: FrameCodecStats = { oversized: 0, malformed: 0, blank: 0 };

  constructor(maxFrameBytes: number = MAX_FRAME_BYTES) {
    this.maxFrameBytes = maxFrameBytes;
  }

  /** 喂入一块 stdout 数据，返回本次能完整解析出的帧。 */
  push(chunk: Buffer | string): unknown[] {
    const bytes = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk, 'utf8');
    this.buffer = this.buffer.length === 0 ? bytes : Buffer.concat([this.buffer, bytes]);

    const frames: unknown[] = [];
    let newlineAt: number;
    while ((newlineAt = this.buffer.indexOf(0x0a)) !== -1) {
      const line = this.buffer.subarray(0, newlineAt);
      this.buffer = this.buffer.subarray(newlineAt + 1);
      const frame = this.decodeLine(line);
      if (frame !== undefined) frames.push(frame);
    }

    // 未结束的超长行：服务端在超限时会把剩余字节丢到行尾为止，我们同样不再累积，
    // 否则一个坏进程能靠永不换行把主进程内存吃光。
    if (this.buffer.length > this.maxFrameBytes) {
      this.stats.oversized += 1;
      this.buffer = Buffer.alloc(0);
    }
    return frames;
  }

  /** 流结束时调用：把残留缓冲按整帧处理（对应 README §1「最后一帧无结尾换行」）。 */
  flush(): unknown[] {
    if (this.buffer.length === 0) return [];
    const line = this.buffer;
    this.buffer = Buffer.alloc(0);
    const frame = this.decodeLine(line);
    return frame === undefined ? [] : [frame];
  }

  private decodeLine(line: Buffer): unknown {
    let text = line.toString('utf8');
    if (text.endsWith('\r')) text = text.slice(0, -1);
    if (text.startsWith(BOM)) text = text.slice(1);
    text = text.trim();
    if (text === '') {
      this.stats.blank += 1;
      return undefined;
    }
    if (Buffer.byteLength(text, 'utf8') > this.maxFrameBytes) {
      this.stats.oversized += 1;
      return undefined;
    }
    try {
      const parsed: unknown = JSON.parse(text);
      if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
        this.stats.malformed += 1;
        return undefined;
      }
      return parsed;
    } catch {
      this.stats.malformed += 1;
      return undefined;
    }
  }
}

/** 把一帧编码成一行（含结尾 `\n`）。 */
export function encodeFrame(frame: unknown): string {
  return `${JSON.stringify(frame)}\n`;
}

/**
 * 从响应帧里取 `id`。服务端原样回显字符串或数字 id，因此两种都要接受。
 * 返回 undefined 表示这条帧没有可配对的 id（例如服务端在畸形帧上的兜底响应）。
 */
export function readFrameId(frame: unknown): string | number | undefined {
  if (typeof frame !== 'object' || frame === null) return undefined;
  const id = (frame as { id?: unknown }).id;
  if (typeof id === 'string' || typeof id === 'number') return id;
  return undefined;
}

/** 配对键：字符串 "1" 与数字 1 是**不同**的 id，键里必须保留类型信息。 */
export function idKey(id: string | number): string {
  return typeof id === 'number' ? `n:${id}` : `s:${id}`;
}

/** 判断一帧是否是可用的信封（ok 为布尔）。 */
export function isEnvelope(frame: unknown): boolean {
  return (
    typeof frame === 'object' &&
    frame !== null &&
    typeof (frame as { ok?: unknown }).ok === 'boolean'
  );
}
