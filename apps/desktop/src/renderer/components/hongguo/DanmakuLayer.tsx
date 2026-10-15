import { useEffect, useMemo, useRef, useState } from 'react';
import type { NativeDanmakuItem } from '@/lib/core-types';

/**
 * 弹幕飞行层。
 *
 * 为什么用 rAF 手写位移而不是 CSS 动画：
 * 1. DESIGN-SYSTEM.md §2.1 规定 `@keyframes` 只能出现在 `styles/player.css`（另一位工程师
 *    的文件），本组件不能自带关键帧；
 * 2. 弹幕必须能随时暂停/拖动定位 —— CSS 动画的进度与 `currentTime` 之间要额外做同步，
 *    反而更复杂。这里以「播放位置」为唯一时间源计算位移，暂停即静止、seek 即跳变，
 *    与播放器状态天然一致。
 *
 * 不在这里做的事情：防重叠分轨。DESIGN-SYSTEM.md §5.3 要求复用 `core/danmaku/layout.go`
 * 的输出（两端一致），因此本组件只按 `item` 自带信息行内渲染；**轨道分配是临时启发式**
 * （见 `hashRow`），等内核的 layout 输出对齐后替换。
 */

/** 单条弹幕飞完全程的时长（ms），DESIGN-SYSTEM.md §5.3 规定 8s。 */
export const FLIGHT_MS = 8_000;
/** 行高（px），与 §5.3 的 32px 行高一致。 */
const ROW_HEIGHT = 32;

export interface DanmakuLayerProps {
  items: NativeDanmakuItem[];
  /** 开关：关闭时不渲染任何节点，但**不卸载已取回的数据**。 */
  enabled: boolean;
  /** 播放位置（ms）。由播放器 store 的 `currentTime` 提供。 */
  currentTimeMs: number;
  /** 暂停时冻结位移，不做时间补偿。 */
  playing: boolean;
  /** 透明度 0–1（设置页的 `playback_preferences` 会注入）。 */
  opacity?: number;
}

interface Flying {
  item: NativeDanmakuItem;
  row: number;
}

export function DanmakuLayer({
  items,
  enabled,
  currentTimeMs,
  playing,
  opacity = 1,
}: DanmakuLayerProps) {
  const hostRef = useRef<HTMLDivElement>(null);
  const [rows, setRows] = useState(6);
  const [reducedMotion, setReducedMotion] = useState(false);
  /** 冻结的时间轴：暂停时保持最后一次位移，不被 currentTime 的抖动带着走。 */
  const frozenRef = useRef(currentTimeMs);
  /** 每帧自增以驱动重渲染：位移由 rAF 计算，React 只负责把它写到 DOM。 */
  const [, setFrame] = useState(0);

  // 轨道数按实际高度算：rows = clamp(1, floor(h/32), 12)（§5.3）。
  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    const measure = () => {
      const height = host.clientHeight || 0;
      const next = Math.max(1, Math.min(12, Math.floor(height / ROW_HEIGHT)));
      setRows(next);
    };
    measure();
    if (typeof ResizeObserver === 'undefined') return;
    const observer = new ResizeObserver(measure);
    observer.observe(host);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return;
    const query = window.matchMedia('(prefers-reduced-motion: reduce)');
    setReducedMotion(query.matches);
    const listener = (event: MediaQueryListEvent) => setReducedMotion(event.matches);
    query.addEventListener('change', listener);
    return () => query.removeEventListener('change', listener);
  }, []);

  useEffect(() => {
    if (playing) frozenRef.current = currentTimeMs;
  }, [playing, currentTimeMs]);

  const timeline = playing ? currentTimeMs : frozenRef.current;

  /** 只挂载「正在飞」的那一小段：进入前 0.2s 到飞完为止。 */
  const flying = useMemo<Flying[]>(() => {
    if (!enabled || reducedMotion) return [];
    const visible: Flying[] = [];
    for (const item of items) {
      const elapsed = timeline - item.timeMs;
      if (elapsed < -200 || elapsed > FLIGHT_MS) continue;
      visible.push({ item, row: hashRow(item.id, rows) });
    }
    // 同轨同时进入的按时间排序，保证叠加顺序稳定（避免每秒重排导致的视觉跳动）。
    visible.sort((a, b) => a.item.timeMs - b.item.timeMs || a.item.id.localeCompare(b.item.id));
    return visible;
  }, [items, enabled, reducedMotion, timeline, rows]);

  // 位移重算：静止时（暂停/减少动效）不需要每帧刷新。
  useEffect(() => {
    if (!playing || !enabled || reducedMotion) return;
    let frame = 0;
    const step = () => {
      frame = requestAnimationFrame(() => {
        setFrame((value) => value + 1);
        step();
      });
    };
    step();
    return () => cancelAnimationFrame(frame);
  }, [playing, enabled, reducedMotion]);

  if (!enabled) return null;

  if (reducedMotion) {
    // 减少动效：不飞，改为静态堆叠（DESIGN-SYSTEM.md §6.1 的降级要求）。
    return (
      <div
        ref={hostRef}
        data-hn-danmaku="static"
        className="pointer-events-none absolute inset-0 overflow-hidden px-3 py-2"
        style={{ opacity }}
      >
        <div className="flex flex-col gap-1">
          {items.slice(0, rows * 2).map((item) => (
            <span key={item.id} className="text-base text-danmaku">
              {item.text}
            </span>
          ))}
        </div>
      </div>
    );
  }

  return (
    <div
      ref={hostRef}
      data-hn-danmaku="flying"
      className="pointer-events-none absolute inset-0 overflow-hidden"
      style={{ opacity }}
    >
      {flying.map(({ item, row }) => {
        const elapsed = Math.max(0, timeline - item.timeMs);
        const progress = Math.min(1, elapsed / FLIGHT_MS);
        // 从右缘外进入，到左缘外离开；用百分比以便不依赖测量宽度。
        const translate = 100 - progress * 200;
        return (
          <span
            key={item.id}
            data-hn-danmaku-item={item.id}
            className="absolute text-base whitespace-nowrap text-danmaku will-change-transform"
            style={{
              top: row * ROW_HEIGHT,
              left: 0,
              transform: `translateX(${translate}%)`,
            }}
          >
            {item.text}
          </span>
        );
      })}
    </div>
  );
}

/** 临时轨道启发式：稳定散列到 0..rows-1（等内核 layout 输出后替换）。 */
function hashRow(id: string, rows: number): number {
  let hash = 0;
  for (let index = 0; index < id.length; index += 1) {
    hash = (hash * 31 + id.charCodeAt(index)) | 0;
  }
  return Math.abs(hash) % Math.max(1, rows);
}
