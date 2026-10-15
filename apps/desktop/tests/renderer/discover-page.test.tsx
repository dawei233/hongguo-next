import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it } from 'vitest';
import type { Envelope, HnBridge, KernelStatus } from '@shared/ipc';
import { DiscoverPage } from '@/pages/DiscoverPage';
import { PlaybackSessionProvider } from '@/lib/session';

/**
 * 发现页组件测试。
 *
 * 覆盖两件在生产里最容易坏、且**只有真渲染才能发现**的事：
 * 1. 内核数据到货后，海报网格真的按标题渲染出来了；
 * 2. 没有桥（浏览器 / preload 未注入）时走**空态**，而不是白屏或假数据。
 *
 * 不 mock `@/lib/api`：测试要穿过 api.ts 的降级分支，否则「优雅降级」这条约束等于没测。
 */

const READY_STATUS: KernelStatus = {
  state: 'ready',
  pid: 1234,
  restarts: 0,
  initialized: true,
  coreVersion: '9.9.9-test',
  binaryPath: '/tmp/hncore-server',
  binaryAvailable: true,
  lastError: null,
  inflight: 0,
};

const DRAMA = {
  id: 'hongguo:1001',
  source: 'hongguo',
  title: '逆光而行',
  cover: 'https://example.test/cover.jpg',
  episodes: 78,
  category: '都市',
  heat: '120000000',
  description: '婚礼当夜她消失了。',
};

function createBridge(overrides: Partial<HnBridge> = {}): HnBridge {
  const invoke = (async (action: string): Promise<Envelope> => {
    if (action === 'categories') {
      return { ok: true, data: { items: [{ id: 'dushi', name: '都市' }, { id: 'xuanyi', name: '悬疑' }] } };
    }
    if (action === 'recommendations') {
      return { ok: true, data: { items: [DRAMA] } };
    }
    if (action === 'catalog') {
      return { ok: true, data: { items: [DRAMA, { ...DRAMA, id: 'hongguo:1002', title: '雪落无声' }] } };
    }
    return { ok: false, error: `未预期的动作 ${action}`, code: 'invalid_request' };
  }) as HnBridge['invoke'];

  return {
    bridgeVersion: '0.1.0',
    platform: 'linux',
    coreBinaryPath: '/tmp/hncore-server',
    invoke,
    status: async () => READY_STATUS,
    restart: async () => ({ ok: true, data: READY_STATUS }),
    shutdownKernel: async () => undefined,
    on: () => () => undefined,
    window: {
      minimize: async () => undefined,
      toggleMaximize: async () => ({ maximized: false, fullScreen: false, focused: true }),
      close: async () => undefined,
      setFullScreen: async () => ({ maximized: false, fullScreen: true, focused: true }),
      getState: async () => ({ maximized: false, fullScreen: false, focused: true }),
    },
    openExternal: async () => true,
    coverUrl: async () => null,
    ...overrides,
  };
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/']}>
      <PlaybackSessionProvider>
        <DiscoverPage />
      </PlaybackSessionProvider>
    </MemoryRouter>,
  );
}

afterEach(() => {
  delete (globalThis as { hn?: HnBridge }).hn;
});

describe('DiscoverPage', () => {
  it('没有 window.hn 时渲染空态而不是白屏', async () => {
    renderPage();
    expect(await screen.findByText('内核未运行，暂时没有内容')).toBeTruthy();
    // 空态下不能出现任何伪造的剧名。
    expect(screen.queryByText('逆光而行')).toBeNull();
  });

  it('内核返回数据后按标题渲染海报网格与题材 Tabs', async () => {
    (globalThis as { hn?: HnBridge }).hn = createBridge();
    renderPage();

    expect(await screen.findByText('雪落无声')).toBeTruthy();
    expect(screen.getAllByText('逆光而行').length).toBeGreaterThan(0);
    // 题材来自内核 categories，不是硬编码。
    expect(screen.getByRole('tab', { name: '悬疑' })).toBeTruthy();
    expect(screen.getAllByRole('button', { name: /打开《/ }).length).toBe(2);
  });

  it('内核回业务错误时展示原因与 code，而不是空态', async () => {
    (globalThis as { hn?: HnBridge }).hn = createBridge({
      invoke: (async (action: string): Promise<Envelope> => {
        if (action === 'categories') return { ok: true, data: { items: [] } };
        return { ok: false, error: '上游超时', code: 'upstream_error' };
      }) as HnBridge['invoke'],
    });
    renderPage();

    expect(await screen.findByText('取内容失败')).toBeTruthy();
    expect(screen.getByText('上游超时')).toBeTruthy();
    await waitFor(() => expect(screen.getByText('code=upstream_error')).toBeTruthy());
  });
});
