import { describe, expect, it } from 'vitest';
import {
  FrameDecoder,
  MAX_FRAME_BYTES,
  encodeFrame,
  idKey,
  isEnvelope,
  readFrameId,
} from '../../src/main/kernel/frames.js';

/**
 * 帧编解码测试：逐条对齐 core/cmd/hncore-server/README.md §1 的定界表。
 * 这里断言的是**协议行为**，不是实现细节 —— 任何一条改动都意味着两端会不一致。
 */
describe('encodeFrame', () => {
  it('序列化后追加单个换行', () => {
    expect(encodeFrame({ id: '1', action: 'ping' })).toBe('{"id":"1","action":"ping"}\n');
  });

  it('载荷内的换行被转义，不会破坏行定界', () => {
    const encoded = encodeFrame({ text: '第一行\n第二行' });
    expect(encoded.endsWith('\n')).toBe(true);
    expect(encoded.slice(0, -1)).not.toContain('\n');
    expect(encoded).toContain('\\n');
  });
});

describe('FrameDecoder', () => {
  it('一次 push 多帧全部解析', () => {
    const decoder = new FrameDecoder();
    const frames = decoder.push('{"ok":true,"id":"1"}\n{"ok":true,"id":"2"}\n');
    expect(frames).toEqual([
      { ok: true, id: '1' },
      { ok: true, id: '2' },
    ]);
  });

  it('跨 chunk 的半帧被缓冲，拼齐后才产出', () => {
    const decoder = new FrameDecoder();
    expect(decoder.push('{"ok":tr')).toEqual([]);
    expect(decoder.push('ue,"id":"7"}\n')).toEqual([{ ok: true, id: '7' }]);
  });

  it('CRLF 的行尾 \\r 被剥除', () => {
    const decoder = new FrameDecoder();
    expect(decoder.push('{"ok":true}\r\n')).toEqual([{ ok: true }]);
  });

  it('行首 BOM 被剥除', () => {
    const decoder = new FrameDecoder();
    expect(decoder.push('\uFEFF{"ok":true}\n')).toEqual([{ ok: true }]);
  });

  it('空行与纯空白行被跳过并计数，不产生帧', () => {
    const decoder = new FrameDecoder();
    expect(decoder.push('\n   \n\t\n')).toEqual([]);
    expect(decoder.stats.blank).toBe(3);
  });

  it('JSON 语法错误丢弃该行并计数，后续行不受影响', () => {
    const decoder = new FrameDecoder();
    const frames = decoder.push('{不是 JSON}\n{"ok":false,"error":"x"}\n');
    expect(frames).toEqual([{ ok: false, error: 'x' }]);
    expect(decoder.stats.malformed).toBe(1);
  });

  it('合法但非对象的 JSON（数组/标量）同样按畸形丢弃', () => {
    const decoder = new FrameDecoder();
    expect(decoder.push('[1,2,3]\n42\n"text"\nnull\n')).toEqual([]);
    expect(decoder.stats.malformed).toBe(4);
  });

  it('超过单帧上限的行被丢掉并计数，且不残留缓冲', () => {
    const decoder = new FrameDecoder(64);
    const oversized = `{"ok":true,"pad":"${'x'.repeat(200)}"}\n`;
    expect(decoder.push(oversized)).toEqual([]);
    expect(decoder.stats.oversized).toBeGreaterThan(0);
    // 缓冲被清空，下一帧正常解析（否则一个坏进程能靠长行吃光内存）。
    expect(decoder.push('{"ok":true}\n')).toEqual([{ ok: true }]);
  });

  it('flush 把无结尾换行的残留按整帧处理', () => {
    const decoder = new FrameDecoder();
    expect(decoder.push('{"ok":true}')).toEqual([]);
    expect(decoder.flush()).toEqual([{ ok: true }]);
    expect(decoder.flush()).toEqual([]);
  });

  it('多字节 UTF-8 跨 chunk 切开时不会乱码', () => {
    const decoder = new FrameDecoder();
    const bytes = Buffer.from('{"ok":true,"error":"弹幕"}\n', 'utf8');
    const head = bytes.subarray(0, 20);
    const tail = bytes.subarray(20);
    expect(decoder.push(head)).toEqual([]);
    expect(decoder.push(tail)).toEqual([{ ok: true, error: '弹幕' }]);
  });

  it('默认上限与内核的 1<<20 一致', () => {
    expect(MAX_FRAME_BYTES).toBe(1 << 20);
  });
});

describe('帧配对辅助', () => {
  it('readFrameId 接受字符串与数字，其它一律 undefined', () => {
    expect(readFrameId({ id: '1' })).toBe('1');
    expect(readFrameId({ id: 1 })).toBe(1);
    expect(readFrameId({ id: null })).toBeUndefined();
    expect(readFrameId(null)).toBeUndefined();
    expect(readFrameId('x')).toBeUndefined();
  });

  it('idKey 保留类型：字符串 "1" 与数字 1 不冲突', () => {
    expect(idKey('1')).not.toBe(idKey(1));
  });

  it('isEnvelope 只认 ok 为布尔的帧', () => {
    expect(isEnvelope({ ok: true })).toBe(true);
    expect(isEnvelope({ ok: false })).toBe(true);
    expect(isEnvelope({ ok: 'yes' })).toBe(false);
    expect(isEnvelope({ id: '1' })).toBe(false);
  });
});
