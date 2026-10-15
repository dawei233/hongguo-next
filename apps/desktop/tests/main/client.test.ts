import { EventEmitter } from 'node:events';
import { PassThrough } from 'node:stream';
import type { spawn } from 'node:child_process';
import { afterEach, describe, expect, it } from 'vitest';
import { KernelClient } from '../../src/main/kernel/client.js';
import { encodeFrame } from '../../src/main/kernel/frames.js';

/**
 * 内核 IPC 客户端测试：帧编解码之外的**时序与配对**行为。
 *
 * 用假子进程（注入 `spawnFn`）而不是真起 hncore-server：
 * - 真进程不能可靠构造「响应乱序」「响应永不到达」这两种拓扑，而这恰恰是最容易写错的；
 * - CI 里不该依赖 Go 工具链是否产出二进制。
 */

class FakeKernel extends EventEmitter {
  pid = 4242;
  stdin = new PassThrough();
  stdout = new PassThrough();
  stderr = new PassThrough();
  /** 收到的请求帧（按写入顺序）。 */
  inbox: Record<string, unknown>[] = [];
  killed = false;

  constructor() {
    super();
    let buffer = '';
    this.stdin.on('data', (chunk: Buffer) => {
      buffer += chunk.toString('utf8');
      let index: number;
      while ((index = buffer.indexOf('\n')) !== -1) {
        const line = buffer.slice(0, index);
        buffer = buffer.slice(index + 1);
        if (!line.trim()) continue;
        this.inbox.push(JSON.parse(line) as Record<string, unknown>);
      }
    });
  }

  /** 以服务端的形态回一帧（原样回显 id）。 */
  respond(frame: Record<string, unknown>, raw?: string): void {
    this.stdout.write(raw ?? encodeFrame(frame));
  }

  kill(): boolean {
    this.killed = true;
    this.emit('exit', 0, 'SIGTERM');
    return true;
  }

  crash(code = 1): void {
    this.emit('exit', code, null);
  }

  last(): Record<string, unknown> {
    const frame = this.inbox.at(-1);
    if (!frame) throw new Error('假内核没有收到任何帧');
    return frame;
  }
}

const clients: KernelClient[] = [];

afterEach(async () => {
  for (const client of clients.splice(0)) {
    await client.stop().catch(() => undefined);
  }
});

function makeClient(options: { defaultTimeoutMs?: number } = {}) {
  const kernel = new FakeKernel();
  const client = new KernelClient({
    binaryPath: process.execPath, // 存在即可，isReady 判定用 existsSync
    dataDir: '/tmp/hn-test-core',
    defaultTimeoutMs: options.defaultTimeoutMs ?? 40,
    initializeTimeoutMs: 200,
    restartBackoffMs: () => 5,
    graceMs: 10,
    spawnFn: (() => kernel) as unknown as typeof spawn,
  });
  clients.push(client);
  return { client, kernel };
}

const tick = () => new Promise((resolve) => setImmediate(resolve));

/** 起客户端并完成 initialize 握手，返回处于 ready 的客户端。 */
async function ready(options: { defaultTimeoutMs?: number } = {}) {
  const { client, kernel } = makeClient(options);
  const started = client.start();
  await tick();
  const init = kernel.last();
  expect(init['action']).toBe('initialize');
  kernel.respond({ ok: true, id: init['id'], data: { version: '9.9.9-test' } });
  await started;
  expect(client.getStatus().state).toBe('ready');
  expect(client.getStatus().coreVersion).toBe('9.9.9-test');
  return { client, kernel };
}

describe('KernelClient · 请求配对', () => {
  it('响应乱序返回时，各自解析到自己的 Promise', async () => {
    const { client, kernel } = await ready();
    const first = client.invoke('catalog', { page: 1 }, 500);
    await tick();
    const second = client.invoke('detail', { target: 'x' }, 500);
    await tick();

    expect(kernel.inbox).toHaveLength(3); // initialize + 两条业务
    const firstId = kernel.inbox[1]?.['id'];
    const secondId = kernel.inbox[2]?.['id'];
    expect(firstId).not.toBe(secondId);

    // 后发先回
    kernel.respond({ ok: true, id: secondId, data: { which: 'second' } });
    kernel.respond({ ok: true, id: firstId, data: { which: 'first' } });

    await expect(first).resolves.toMatchObject({ ok: true, data: { which: 'first' } });
    await expect(second).resolves.toMatchObject({ ok: true, data: { which: 'second' } });
    expect(client.lateResponses).toBe(0);
  });

  it('载荷里带 id/action 时不能劫持信封自身的 id（否则响应永远配不上号）', async () => {
    const { client, kernel } = await ready();
    const pending = client.invoke('detail', { id: '载荷里的 id', action: '载荷里的 action' }, 500);
    await tick();
    const frame = kernel.inbox[1];
    // 信封字段由桥接层决定，载荷只能补字段、不能改这两个。
    expect(frame?.['id']).toBe('2');
    expect(frame?.['action']).toBe('detail');

    kernel.respond({ ok: true, id: frame?.['id'], data: { correlated: true } });
    await expect(pending).resolves.toMatchObject({ ok: true, data: { correlated: true } });
    expect(client.lateResponses).toBe(0);
  });

  it('无 id 的帧被计为 orphan，不会误配给任何在途请求', async () => {
    const { client, kernel } = await ready();
    const pending = client.invoke('catalog', {}, 500);
    await tick();
    kernel.respond({ ok: true, data: { stray: true } }); // 故意不带 id
    await expect(Promise.race([pending, Promise.resolve('still-pending')])).resolves.toBe(
      'still-pending',
    );
    expect(client.orphanFrames).toBe(1);
    kernel.respond({ ok: true, id: kernel.inbox[1]?.['id'], data: { ok2: true } });
    await expect(pending).resolves.toMatchObject({ ok: true, data: { ok2: true } });
  });

  it('形如信封但 ok 非布尔的响应按 malformed-response 收敛', async () => {
    const { client, kernel } = await ready();
    const pending = client.invoke('catalog', {}, 500);
    await tick();
    kernel.respond({ id: kernel.inbox[1]?.['id'], data: { weird: true } });
    const envelope = await pending;
    expect(envelope.ok).toBe(false);
    expect(envelope).toMatchObject({ bridge: 'malformed-response', code: 'upstream_error' });
  });
});

describe('KernelClient · 超时与晚到响应', () => {
  it('超时返回 request-timeout，且不再等待内核', async () => {
    const { client } = await ready({ defaultTimeoutMs: 30 });
    const started = Date.now();
    const envelope = await client.invoke('catalog', {}, 30);
    expect(Date.now() - started).toBeLessThan(1_000);
    expect(envelope).toMatchObject({
      ok: false,
      bridge: 'request-timeout',
      code: 'upstream_error',
    });
    expect(client.getStatus().inflight).toBe(0);
  });

  it('超时之后到达的响应被丢弃并计入 lateResponses（不补发第二条）', async () => {
    const { client, kernel } = await ready({ defaultTimeoutMs: 25 });
    const pending = client.invoke('catalog', {}, 25);
    await tick();
    const id = kernel.inbox[1]?.['id'];
    const envelope = await pending;
    expect(envelope.ok).toBe(false);

    kernel.respond({ ok: true, id, data: { tooLate: true } });
    await tick();
    expect(client.lateResponses).toBe(1);
    expect(client.getStatus().inflight).toBe(0);
  });

  it('字符串 id 与数字 id 不会互相配对', async () => {
    // 服务端原样回显 id，字符串 "1" 与数字 1 在协议上是两个键。
    const { client } = await ready({ defaultTimeoutMs: 30 });
    const envelope = await client.invoke('catalog', {}, 30);
    expect(envelope.ok).toBe(false);
    expect(client.getStatus().inflight).toBe(0);
  });
});

describe('KernelClient · 生命周期降级', () => {
  it('二进制缺失时不 spawn，状态为 failed 并给出可读原因', async () => {
    const kernel = new FakeKernel();
    let spawned = 0;
    const client = new KernelClient({
      binaryPath: '/definitely/not/here/hncore-server',
      dataDir: '/tmp/hn-test-core',
      spawnFn: (() => {
        spawned += 1;
        return kernel;
      }) as unknown as typeof spawn,
    });
    clients.push(client);
    await client.start();
    const status = client.getStatus();
    expect(spawned).toBe(0);
    expect(status.state).toBe('failed');
    expect(status.binaryAvailable).toBe(false);
    expect(status.lastError).toContain('未找到内核程序');

    const envelope = await client.invoke('catalog');
    expect(envelope).toMatchObject({ ok: false, code: 'not_ready' });
  });

  it('进程意外退出时，在途请求全部以 kernel-crashed 失败', async () => {
    const { client, kernel } = await ready();
    const pending = client.invoke('catalog', {}, 5_000);
    await tick();
    kernel.crash(139);
    await expect(pending).resolves.toMatchObject({ ok: false, bridge: 'kernel-crashed' });
    expect(client.getStatus().inflight).toBe(0);
  });

  it('stop() 后调用返回 not_ready，而不是挂住', async () => {
    const { client } = await ready();
    await client.stop();
    expect(client.getStatus().state).toBe('stopped');
    await expect(client.invoke('catalog')).resolves.toMatchObject({ ok: false, code: 'not_ready' });
  });
});
