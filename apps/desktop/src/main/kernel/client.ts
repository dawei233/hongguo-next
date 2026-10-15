import { spawn, type ChildProcessWithoutNullStreams } from 'node:child_process';
import { EventEmitter } from 'node:events';
import { existsSync } from 'node:fs';
import type {
  BridgeFailureReason,
  Envelope,
  ErrorEnvelope,
  KernelState,
  KernelStatus,
  OkEnvelope,
} from '../../shared/ipc.js';
import { FrameDecoder, encodeFrame, idKey, isEnvelope, readFrameId } from './frames.js';

export interface KernelLogger {
  info(message: string, meta?: Record<string, unknown>): void;
  warn(message: string, meta?: Record<string, unknown>): void;
  error(message: string, meta?: Record<string, unknown>): void;
}

const silentLogger: KernelLogger = { info() {}, warn() {}, error() {} };

export interface KernelClientOptions {
  binaryPath: string;
  /** 内核 `initialize` 的数据目录（内核在此建引擎）。 */
  dataDir: string;
  /** 业务请求默认等待上限；内核多数动作自设 60s 期限，桥接层默认给 65s 兜底。 */
  defaultTimeoutMs?: number;
  /** `initialize` 的等待上限（内核冷启动要建目录与缓存，给得比业务请求宽）。 */
  initializeTimeoutMs?: number;
  /** 崩溃后最大自动重启次数，超过则进入 failed 并停止重试。 */
  maxRestarts?: number;
  /** 第 n 次重启前的退避（n 从 1 起）。 */
  restartBackoffMs?: (attempt: number) => number;
  /** 优雅退出等待（先 server:shutdown，再 SIGTERM，最后 SIGKILL）。 */
  graceMs?: number;
  logger?: KernelLogger;
  /** 注入用：默认 `child_process.spawn`。测试里指向假内核进程。 */
  spawnFn?: typeof spawn;
  /** 注入用：默认 `Date.now`。 */
  now?: () => number;
}

interface PendingRequest {
  key: string;
  id: string;
  action: string;
  resolve: (envelope: Envelope) => void;
  timer: NodeJS.Timeout | null;
  startedAt: number;
  timeoutMs: number;
}

export interface KernelClientEvents {
  status: (status: KernelStatus) => void;
}

const DEFAULTS = {
  defaultTimeoutMs: 65_000,
  initializeTimeoutMs: 30_000,
  maxRestarts: 5,
  graceMs: 5_000,
} as const;

/** 与 hncore-server 的退出码约定对齐（README §6）。 */
const EXIT_OK = 0;

export class KernelClient extends EventEmitter {
  private readonly options: Required<Omit<KernelClientOptions, 'spawnFn' | 'now' | 'logger' | 'restartBackoffMs'>> & {
    logger: KernelLogger;
    spawnFn: typeof spawn;
    now: () => number;
    restartBackoffMs: (attempt: number) => number;
  };

  private child: ChildProcessWithoutNullStreams | null = null;
  private decoder = new FrameDecoder();
  private pending = new Map<string, PendingRequest>();
  private state: KernelState = 'stopped';
  private restarts = 0;
  private initialized = false;
  private coreVersion: string | null = null;
  private lastError: string | null = null;
  private nextId = 1;
  private stopping = false;
  private restartTimer: NodeJS.Timeout | null = null;
  private readyWaiters: Array<() => void> = [];
  /** stderr 环形缓冲，仅用于诊断展示。 */
  private stderrTail: string[] = [];
  /** 写出串行化：一帧一次 write，避免交错。 */
  private writeChain: Promise<void> = Promise.resolve();

  /** 诊断计数：晚到的响应（超时/取消后到达，按协议必须丢弃且不补发）。 */
  lateResponses = 0;
  /** 诊断计数：无法配对、也非控制帧的帧。 */
  orphanFrames = 0;

  constructor(options: KernelClientOptions) {
    super();
    this.options = {
      binaryPath: options.binaryPath,
      dataDir: options.dataDir,
      defaultTimeoutMs: options.defaultTimeoutMs ?? DEFAULTS.defaultTimeoutMs,
      initializeTimeoutMs: options.initializeTimeoutMs ?? DEFAULTS.initializeTimeoutMs,
      maxRestarts: options.maxRestarts ?? DEFAULTS.maxRestarts,
      graceMs: options.graceMs ?? DEFAULTS.graceMs,
      logger: options.logger ?? silentLogger,
      spawnFn: options.spawnFn ?? spawn,
      now: options.now ?? Date.now,
      restartBackoffMs: options.restartBackoffMs ?? ((attempt) => Math.min(500 * 2 ** (attempt - 1), 8_000)),
    };
  }

  getStatus(): KernelStatus {
    return {
      state: this.state,
      pid: this.child?.pid ?? null,
      restarts: this.restarts,
      initialized: this.initialized,
      coreVersion: this.coreVersion,
      binaryPath: this.options.binaryPath,
      binaryAvailable: existsSync(this.options.binaryPath),
      lastError: this.lastError,
      inflight: this.pending.size,
    };
  }

  private setState(state: KernelState, error?: string | null): void {
    this.state = state;
    if (error !== undefined) this.lastError = error;
    const status = this.getStatus();
    this.emit('status', status);
  }

  /**
   * 启动子进程并完成 `initialize` 握手。
   *
   * README §8 是硬要求：第一条必须发 initialize 并**等它的响应**再发业务请求，
   * 否则晚到的业务请求会拿到「应用核心尚未就绪」。`invoke()` 因此在 starting 期间
   * 等 ready 闸门，而不是抢先发出。
   */
  async start(): Promise<KernelStatus> {
    if (this.state === 'ready') return this.getStatus();
    if (this.state === 'starting') {
      await this.waitForReady();
      return this.getStatus();
    }
    this.stopping = false;
    await this.spawnChild();
    return this.getStatus();
  }

  private waitForReady(): Promise<void> {
    if (this.state === 'ready' || this.state === 'failed') return Promise.resolve();
    return new Promise((resolve) => {
      this.readyWaiters.push(resolve);
    });
  }

  private releaseReadyWaiters(): void {
    const waiters = this.readyWaiters;
    this.readyWaiters = [];
    for (const waiter of waiters) waiter();
  }

  private async spawnChild(): Promise<void> {
    if (!existsSync(this.options.binaryPath)) {
      this.setState('failed', `未找到内核程序：${this.options.binaryPath}`);
      this.options.logger.error('kernel binary missing', { path: this.options.binaryPath });
      this.releaseReadyWaiters();
      return;
    }

    this.setState(this.restarts > 0 ? 'restarting' : 'starting');
    this.decoder = new FrameDecoder();
    this.initialized = false;
    this.coreVersion = null;

    let child: ChildProcessWithoutNullStreams;
    try {
      child = this.options.spawnFn(this.options.binaryPath, [], {
        stdio: ['pipe', 'pipe', 'pipe'],
        windowsHide: true,
      }) as ChildProcessWithoutNullStreams;
    } catch (error) {
      this.setState('failed', describeError(error));
      this.options.logger.error('kernel spawn threw', { error: describeError(error) });
      this.releaseReadyWaiters();
      return;
    }

    this.child = child;
    child.stdout.on('data', (chunk: Buffer) => this.onStdout(chunk));
    child.stderr.on('data', (chunk: Buffer) => this.onStderr(chunk));
    child.on('error', (error) => {
      this.options.logger.error('kernel process error', { error: describeError(error) });
      this.setState('failed', describeError(error));
      this.releaseReadyWaiters();
    });
    child.on('exit', (code, signal) => this.onExit(code, signal));

    const handshake = await this.initialize();
    if (handshake) {
      this.setState('ready', null);
    } else {
      this.setState(this.stopping ? 'stopped' : 'failed', this.lastError);
    }
    this.releaseReadyWaiters();
  }

  private async initialize(): Promise<boolean> {
    const envelope = await this.send('initialize', { directory: this.options.dataDir }, this.options.initializeTimeoutMs);
    if (envelope.ok) {
      this.initialized = true;
      const data = envelope.data as { version?: unknown } | null;
      this.coreVersion = typeof data?.version === 'string' ? data.version : null;
      this.options.logger.info('kernel initialized', { version: this.coreVersion });
      return true;
    }
    this.lastError = envelope.error;
    this.options.logger.warn('kernel initialize failed', { error: envelope.error, code: envelope.code });
    return false;
  }

  /**
   * 发一条内核请求。**永不 reject**：任何失败都以 ErrorEnvelope 返回，
   * 这样渲染层只需要看 `ok`，不必区分「IPC 失败」与「内核失败」。
   */
  async invoke<T = unknown>(
    action: string,
    payload: Record<string, unknown> = {},
    timeoutMs?: number,
  ): Promise<Envelope<T>> {
    if (this.stopping) return failure('内核正在退出', 'not_ready', 'kernel-not-running');
    if (this.state !== 'ready') {
      if (this.state === 'stopped') {
        // 首次调用即触发启动（懒启动），但仍要等握手完成再发业务请求。
        void this.start();
      }
      await this.waitForReady();
      // 重新显式读一次状态：`waitForReady()` 期间状态可能已被 exit 分支改写，而 TS 的
      // 窄化会跟着上面的 `!== 'ready'` 判断一直走，所以要经 `getStatus()` 取新快照。
      const settled: KernelState = this.getStatus().state;
      if (settled !== 'ready') {
        const reason: BridgeFailureReason =
          settled === 'failed' ? 'kernel-crashed' : 'kernel-starting';
        return failure(
          this.lastError ?? (reason === 'kernel-starting' ? '内核正在启动，请稍后重试' : '内核不可用'),
          'not_ready',
          reason,
        );
      }
    }
    return (await this.send(action, payload, timeoutMs)) as Envelope<T>;
  }

  private send(action: string, payload: Record<string, unknown>, timeoutMs?: number): Promise<Envelope> {
    const child = this.child;
    if (!child || child.stdin.destroyed) {
      return Promise.resolve(failure('内核未运行', 'not_ready', 'kernel-not-running'));
    }

    const id = String(this.nextId++);
    const effectiveTimeout = timeoutMs ?? this.options.defaultTimeoutMs;

    return new Promise<Envelope>((resolve) => {
      const pending: PendingRequest = {
        key: idKey(id),
        id,
        action,
        resolve,
        timer: null,
        startedAt: this.options.now(),
        timeoutMs: effectiveTimeout,
      };
      if (effectiveTimeout > 0) {
        pending.timer = setTimeout(() => this.onTimeout(pending.key), effectiveTimeout);
      }
      this.pending.set(pending.key, pending);

      // 除桥接层字段外整帧透传：内核用宽松解码，未知字段被忽略（README §3.1）。
      // **顺序要紧**：payload 来自渲染层，若其中带 `id`/`action`（例如某个动作的载荷
      // 恰好有同名字段），必须先铺开 payload 再由桥接层的 id/action 覆盖 —— 否则
      // 响应会配不上号，调用方只能等到超时。见 tests/main/client.test.ts 的回归用例。
      const frame: Record<string, unknown> = { ...payload, id, action };
      this.write(frame).catch((error: unknown) => {
        this.settle(pending.key, failure(`写入内核失败：${describeError(error)}`, 'upstream_error', 'spawn-failed'));
      });
    });
  }

  private write(frame: Record<string, unknown>): Promise<void> {
    this.writeChain = this.writeChain.then(
      () =>
        new Promise<void>((resolve, reject) => {
          const child = this.child;
          if (!child || child.stdin.destroyed) {
            reject(new Error('stdin 不可写'));
            return;
          }
          const ok = child.stdin.write(encodeFrame(frame), (error) => {
            if (error) reject(error);
            else resolve();
          });
          // 背压：等 drain 再发下一帧，避免把大帧堆在内存里。
          if (!ok) child.stdin.once('drain', () => resolve());
        }),
    );
    // 单帧失败不能让 writeChain 变成 rejected 链，否则后续写入全部立即失败。
    this.writeChain = this.writeChain.catch(() => undefined);
    return this.writeChain;
  }

  private onStdout(chunk: Buffer): void {
    for (const frame of this.decoder.push(chunk)) this.dispatch(frame);
  }

  private dispatch(frame: unknown): void {
    const id = readFrameId(frame);
    if (id === undefined) {
      this.orphanFrames += 1;
      this.options.logger.warn('kernel frame without id, dropped', { frame: truncate(frame) });
      return;
    }
    const key = idKey(id);
    const pending = this.pending.get(key);
    if (!pending) {
      // 超时/取消之后内核才回来的结果：按协议丢弃且不补发第二条响应（README §5）。
      this.lateResponses += 1;
      this.options.logger.info('late kernel response dropped', { id, action: 'unknown' });
      return;
    }
    if (!isEnvelope(frame)) {
      this.settle(key, failure('内核返回了无法识别的响应', 'upstream_error', 'malformed-response', frame));
      return;
    }
    this.settle(key, frame as Envelope);
  }

  private settle(key: string, envelope: Envelope): void {
    const pending = this.pending.get(key);
    if (!pending) return;
    this.pending.delete(key);
    if (pending.timer) clearTimeout(pending.timer);
    pending.resolve(envelope);
  }

  private onTimeout(key: string): void {
    const pending = this.pending.get(key);
    if (!pending) return;
    // 超时只让调用方不再等待；内核调用无法从外部中止，其晚到结果会被丢弃。
    this.settle(
      key,
      failure(`请求超时（${pending.timeoutMs}ms）：${pending.action}`, 'upstream_error', 'request-timeout'),
    );
  }

  private onStderr(chunk: Buffer): void {
    const text = chunk.toString('utf8');
    for (const line of text.split('\n')) {
      const trimmed = line.trim();
      if (!trimmed) continue;
      this.stderrTail.push(trimmed);
      if (this.stderrTail.length > 40) this.stderrTail.shift();
      this.options.logger.info('kernel stderr', { line: trimmed });
    }
  }

  private onExit(code: number | null, signal: NodeJS.Signals | null): void {
    const wasReady = this.state === 'ready';
    this.child = null;
    this.initialized = false;
    this.failAllPending(
      failure(
        `内核进程已退出（code=${code ?? 'null'}, signal=${signal ?? 'null'}）`,
        'not_ready',
        'kernel-crashed',
      ),
    );

    if (this.stopping || (code === EXIT_OK && signal === null && !wasReady)) {
      this.setState('stopped');
      this.releaseReadyWaiters();
      return;
    }

    this.restarts += 1;
    if (this.restarts > this.options.maxRestarts) {
      this.setState('failed', `内核连续异常退出 ${this.restarts} 次，已停止自动重启`);
      this.releaseReadyWaiters();
      return;
    }

    const delay = this.options.restartBackoffMs(this.restarts);
    this.setState('restarting', `内核异常退出，${delay}ms 后第 ${this.restarts} 次重启`);
    this.options.logger.warn('kernel restarting', { attempt: this.restarts, delay, code, signal });

    this.restartTimer = setTimeout(() => {
      this.restartTimer = null;
      if (this.stopping) return;
      void this.spawnChild();
    }, delay);
  }

  private failAllPending(envelope: Envelope): void {
    for (const key of [...this.pending.keys()]) this.settle(key, envelope);
  }

  /**
   * 优雅退出：先发 `{"server":"shutdown"}`，等它回包，再等进程自己退；
   * 超期 SIGTERM，再超期 SIGKILL（README §8 建议的接线顺序）。
   */
  async stop(): Promise<void> {
    this.stopping = true;
    if (this.restartTimer) {
      clearTimeout(this.restartTimer);
      this.restartTimer = null;
    }
    const child = this.child;
    if (!child) {
      this.failAllPending(failure('内核已停止', 'not_ready', 'kernel-not-running'));
      this.setState('stopped');
      this.releaseReadyWaiters();
      return;
    }

    const exited = new Promise<void>((resolve) => child.once('exit', () => resolve()));

    // server:shutdown 是控制帧（README §3.3），由服务端本地处理、不转发内核，
    // 因此它不能走 send() 的 action 通道，直接写原始帧。
    await this.write({ id: String(this.nextId++), server: 'shutdown' }).catch(() => undefined);

    await Promise.race([exited, delay(this.options.graceMs)]);
    if (this.child) {
      this.child.kill('SIGTERM');
      await Promise.race([exited, delay(1_000)]);
    }
    if (this.child) {
      this.child.kill('SIGKILL');
      await Promise.race([exited, delay(500)]);
    }
    this.failAllPending(failure('内核已停止', 'not_ready', 'kernel-not-running'));
    this.setState('stopped');
    this.releaseReadyWaiters();
  }

  /** 手动重启：先停再起，重置重启计数（用户显式动作，不消耗自动重启额度）。 */
  async restart(): Promise<KernelStatus> {
    await this.stop();
    this.restarts = 0;
    this.stopping = false;
    await this.start();
    return this.getStatus();
  }

  /** `server:ping` 控制帧：用于诊断面板，不走内核业务。 */
  async ping(): Promise<Envelope> {
    return this.controlFrame('ping');
  }

  private controlFrame(server: string, extra: Record<string, unknown> = {}): Promise<Envelope> {
    const child = this.child;
    if (!child || child.stdin.destroyed) {
      return Promise.resolve(failure('内核未运行', 'not_ready', 'kernel-not-running'));
    }
    const id = String(this.nextId++);
    return new Promise<Envelope>((resolve) => {
      const pending: PendingRequest = {
        key: idKey(id),
        id,
        action: `server:${server}`,
        resolve,
        timer: setTimeout(() => this.onTimeout(pending.key), 5_000),
        startedAt: this.options.now(),
        timeoutMs: 5_000,
      };
      this.pending.set(pending.key, pending);
      this.write({ ...extra, id, server }).catch((error: unknown) => {
        this.settle(pending.key, failure(`写入内核失败：${describeError(error)}`, 'upstream_error', 'spawn-failed'));
      });
    });
  }
}

function failure(
  error: string,
  code: ErrorEnvelope['code'],
  bridge: BridgeFailureReason,
  raw?: unknown,
): ErrorEnvelope {
  const envelope: ErrorEnvelope = { ok: false, error, bridge };
  if (code) envelope.code = code;
  if (raw !== undefined) envelope.raw = raw;
  return envelope;
}

export function isOk<T>(envelope: Envelope<T>): envelope is OkEnvelope<T> {
  return envelope.ok === true;
}

function describeError(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function truncate(value: unknown, limit = 200): string {
  const text = typeof value === 'string' ? value : JSON.stringify(value);
  return text.length > limit ? `${text.slice(0, limit)}…` : text;
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
