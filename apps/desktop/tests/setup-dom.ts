/**
 * jsdom 环境补齐。
 *
 * 只补「组件真的会调用、而 jsdom 没实现」的三样：matchMedia（Radix/我们的
 * prefers-reduced-motion 分支）、ResizeObserver（弹幕层量轨道高度）、
 * scrollIntoView（Radix Select 打开时会把选中项滚进视野）。
 * 刻意不引入 jest-dom：断言用原生 DOM API，少一层隐式全局。
 */
import { cleanup } from '@testing-library/react';
import { afterEach, vi } from 'vitest';

afterEach(() => {
  cleanup();
});

if (!window.matchMedia) {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: (query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    }),
  });
}

if (!('ResizeObserver' in globalThis)) {
  class FakeResizeObserver {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  }
  Object.defineProperty(globalThis, 'ResizeObserver', {
    writable: true,
    value: FakeResizeObserver,
  });
}

if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = function scrollIntoView() {};
}
