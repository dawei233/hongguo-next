import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from 'react';
import type { EpisodeView, SeriesView } from './adapt';

/**
 * 跨页面会话状态。
 *
 * 为什么不用路由 state / search params 传整份详情：
 * 内核的 `detail` 返回的 chapters 有几十上百条，把整棵对象序列化进 URL 既丑又会在
 * 刷新后失效。这里用「进程内会话」承载，规则是：
 * - 详情页取到数据后写入 `selection`；
 * - 播放页只读 `selection`，读不到（直接深链、刷新、内核未起）就渲染空态并引导回发现页；
 * - 已看集合只是**本次会话**的浏览记录，不落盘、不假装等于内核的观看进度
 *   （真实进度属于 `core` 的 storage/works` 面，M4c 不接）。
 */
export interface PlaybackSelection {
  series: SeriesView;
  episodes: EpisodeView[];
  /** 当前集号（1 基），与 `EpisodeView.index` 一致。 */
  index: number;
}

interface SessionValue {
  selection: PlaybackSelection | null;
  setSelection: (next: PlaybackSelection) => void;
  selectEpisode: (index: number) => void;
  /** 本次会话内打开过的集（详情页「只看未看」用）。 */
  visited: ReadonlySet<string>;
  markVisited: (episodeId: string) => void;
  /** 顶栏搜索框的内容，发现页据此做**本地**筛选。 */
  query: string;
  setQuery: (next: string) => void;
}

const SessionContext = createContext<SessionValue | null>(null);

export function PlaybackSessionProvider({ children }: { children: ReactNode }) {
  const [selection, setSelectionState] = useState<PlaybackSelection | null>(null);
  const [visited, setVisited] = useState<ReadonlySet<string>>(() => new Set<string>());
  const [query, setQuery] = useState('');

  const setSelection = useCallback((next: PlaybackSelection) => {
    setSelectionState(next);
  }, []);

  const selectEpisode = useCallback((index: number) => {
    setSelectionState((previous) => {
      if (!previous) return previous;
      if (!previous.episodes.some((episode) => episode.index === index)) return previous;
      return { ...previous, index };
    });
  }, []);

  const markVisited = useCallback((episodeId: string) => {
    if (!episodeId) return;
    setVisited((previous) => {
      if (previous.has(episodeId)) return previous;
      const next = new Set(previous);
      next.add(episodeId);
      return next;
    });
  }, []);

  const value = useMemo<SessionValue>(
    () => ({ selection, setSelection, selectEpisode, visited, markVisited, query, setQuery }),
    [selection, setSelection, selectEpisode, visited, markVisited, query],
  );

  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}

export function useSession(): SessionValue {
  const value = useContext(SessionContext);
  if (!value) throw new Error('useSession 必须在 PlaybackSessionProvider 内使用');
  return value;
}
