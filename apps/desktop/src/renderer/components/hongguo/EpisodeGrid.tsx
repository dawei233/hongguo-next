import { Check, Download, Lock } from 'lucide-react';
import { Progress } from '@/components/ui/progress';
import { cn, episodeNumber } from '@/lib/utils';
import type { EpisodeView } from '@/lib/adapt';

/**
 * 选集网格（详情页与播放器侧栏共用同一份）。
 *
 * 三条硬规则：
 * 1. 集号固定两位等宽排版（`episodeNumber`），否则 78 集的网格会左右跳动；
 * 2. **缺失/锁定的集不隐藏**，画出来并标明原因 —— 用户要看到「这里有但拿不到」，
 *    隐藏只会让他以为源里没有这一集；
 * 3. 状态用 `data-state` 暴露，视觉留给调用方的 className（详情页与侧栏尺寸不同）。
 */
export type EpisodeState = 'default' | 'current' | 'visited' | 'locked' | 'downloading';

export interface EpisodeGridProps {
  episodes: EpisodeView[];
  currentIndex?: number;
  visited?: ReadonlySet<string>;
  /** 已下载完成的集 id（M4c 只用于展示，下载链路由 M5 接入）。 */
  downloaded?: ReadonlySet<string>;
  /** 0–1，仅用于 `downloading` 状态。 */
  progressById?: ReadonlyMap<string, number>;
  onSelect: (episode: EpisodeView) => void;
  className?: string;
}

export function episodeState(
  episode: EpisodeView,
  options: {
    currentIndex?: number | undefined;
    visited?: ReadonlySet<string> | undefined;
    downloaded?: ReadonlySet<string> | undefined;
    progressById?: ReadonlyMap<string, number> | undefined;
  },
): EpisodeState {
  // 判据只用「有没有 id」：内核的 Chapter.id 是 resolve 的必需入参，缺它就一定拿不到流。
  // 刻意**不**看 `chapter.videoUrl` —— 该字段在部分源的 detail 响应里根本不填，等 resolve
  // 才产生；拿它判锁定会把整部剧标成不可播（假锁定比不标更糟）。
  if (!episode.id) return 'locked';
  if (options.progressById?.has(episode.id)) return 'downloading';
  if (options.currentIndex === episode.index) return 'current';
  if (options.visited?.has(episode.id)) return 'visited';
  return 'default';
}

export function EpisodeGrid({
  episodes,
  currentIndex,
  visited,
  downloaded,
  progressById,
  onSelect,
  className,
}: EpisodeGridProps) {
  return (
    <div
      className={cn('grid grid-cols-[repeat(auto-fill,minmax(64px,1fr))] gap-2', className)}
      role="list"
    >
      {episodes.map((episode) => {
        const state = episodeState(episode, { currentIndex, visited, downloaded, progressById });
        const locked = state === 'locked';
        const progress = progressById?.get(episode.id);
        return (
          <button
            key={episode.id || `${episode.index}`}
            type="button"
            role="listitem"
            data-state={state}
            disabled={locked}
            title={episode.title || undefined}
            onClick={() => onSelect(episode)}
            className={cn(
              'relative flex h-11 items-center justify-center overflow-hidden rounded-sm border text-sm tabular-nums transition-colors',
              'border-border-subtle bg-secondary text-foreground hover:bg-accent',
              state === 'current' && 'border-primary bg-primary-subtle text-primary ring-2 ring-primary',
              state === 'visited' && 'text-foreground-muted',
              locked && 'cursor-not-allowed border-transparent bg-transparent text-foreground-subtle',
            )}
          >
            {episodeNumber(episode.index)}
            {episode.vip ? (
              <span className="absolute top-0.5 left-1 text-2xs text-gold">VIP</span>
            ) : null}
            {state === 'visited' ? (
              <Check className="absolute top-0.5 right-0.5 size-3 text-success" />
            ) : null}
            {downloaded?.has(episode.id) ? (
              <Download className="absolute top-0.5 right-0.5 size-3 text-success" />
            ) : null}
            {locked ? <Lock className="absolute right-1 bottom-1 size-3" /> : null}
            {progress !== undefined ? (
              <Progress
                value={Math.round(progress * 100)}
                className="absolute inset-x-0 bottom-0 h-1 rounded-none"
              />
            ) : null}
          </button>
        );
      })}
    </div>
  );
}
