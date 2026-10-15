import { Play } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Skeleton } from '@/components/ui/skeleton';
import { cn, formatCount } from '@/lib/utils';
import type { SeriesView } from '@/lib/adapt';

/**
 * 海报卡（DESIGN-SYSTEM.md §5.1）。
 *
 * 整块是一个 `<button>`：现役 `.browse-card{display:block}` 的可用性不能被 "把标题也做成
 * 链接" 破坏。封面缺图时**不留白框**，用 poster 底色兜住，避免网格高度在加载中抖动。
 */
export interface PosterCardProps {
  series: SeriesView;
  rank?: number;
  onOpen: (series: SeriesView) => void;
}

export function PosterCard({ series, rank, onOpen }: PosterCardProps) {
  return (
    <button
      type="button"
      onClick={() => onOpen(series)}
      className="group block w-full text-left"
      aria-label={`打开《${series.title}》`}
    >
      <span className="relative block aspect-[3/4] overflow-hidden rounded-xl bg-secondary">
        {series.cover ? (
          <img
            src={series.cover}
            alt=""
            loading="lazy"
            decoding="async"
            className="size-full object-cover transition-transform duration-300 group-hover:scale-[1.035]"
          />
        ) : (
          <span className="flex size-full items-center justify-center text-2xs text-foreground-subtle">
            无封面
          </span>
        )}

        {rank ? (
          <Badge className="absolute top-2 left-2 rounded-sm tabular-nums">{rank}</Badge>
        ) : null}

        {series.total > 0 ? (
          <span className="absolute bottom-2 left-2 rounded-xs bg-scrim px-1.5 py-0.5 text-2xs text-danmaku tabular-nums">
            {series.total} 集
          </span>
        ) : null}

        {series.vip ? (
          <Badge variant="outline" className="absolute top-2 right-2 rounded-sm text-gold">
            VIP
          </Badge>
        ) : null}

        <span className="pointer-events-none absolute inset-0 flex items-center justify-center opacity-0 transition-opacity duration-300 group-hover:opacity-100">
          <span className="flex size-9 items-center justify-center rounded-full bg-scrim text-danmaku backdrop-blur-sm">
            <Play className="size-4" />
          </span>
        </span>
      </span>

      <span className="mt-3 line-clamp-2 block text-md font-semibold">{series.title}</span>
      <span className="mt-1 block text-xs text-foreground-muted tabular-nums">
        {[series.tag || null, formatCount(series.heat) || null].filter(Boolean).join(' · ') ||
          '暂无热度数据'}
      </span>
    </button>
  );
}

/** 加载骨架：比例与真实卡片一致（3:4 + 两行文字），不做虚构片名。 */
export function PosterCardSkeleton({ className }: { className?: string }) {
  return (
    <div className={cn('w-full', className)}>
      <Skeleton className="aspect-[3/4] rounded-xl bg-skeleton" />
      <Skeleton className="mt-3 h-2.5 w-3/4 rounded-sm bg-skeleton" />
      <Skeleton className="mt-2 h-2 w-1/2 rounded-sm bg-skeleton" />
    </div>
  );
}
