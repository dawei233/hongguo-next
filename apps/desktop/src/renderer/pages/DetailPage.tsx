import { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  ArrowDownWideNarrow,
  ChevronLeft,
  Download,
  Play,
  Plus,
  RefreshCw,
  Star,
} from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Separator } from '@/components/ui/separator';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Switch } from '@/components/ui/switch';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { EpisodeGrid } from '@/components/hongguo/EpisodeGrid';
import { StateMessage } from '@/components/hongguo/StateMessage';
import { fetchDetail } from '@/lib/api';
import { toEpisode, toSeries, type EpisodeView } from '@/lib/adapt';
import { useKernelQuery } from '@/lib/hooks';
import { useSession } from '@/lib/session';
import { formatCount } from '@/lib/utils';

/**
 * 剧集详情 + 选集（DESIGN-SYSTEM.md §5.2）。
 *
 * 数据链只有一条：`detail {drama}` → chapters。注意 `detail` 的入参必须是**原始 drama**
 * 对象（内核用它的 id+source 定位源内剧），所以这里回传 `series.raw` 而不是视图模型。
 *
 * 「已看」的来源要说清楚：内核的观看进度面（workLease/storage）在 M4c 未接，因此
 * 「只看未看」用的是**本次会话浏览过的集**，不是真实进度。UI 上如实标注，避免用户
 * 以为自己的历史被丢了。
 */
export function DetailPage() {
  const navigate = useNavigate();
  const { selection, setSelection, visited, markVisited } = useSession();
  const series = selection?.series ?? null;

  const [order, setOrder] = useState<'asc' | 'desc'>('asc');
  const [onlyUnwatched, setOnlyUnwatched] = useState(false);
  const [expanded, setExpanded] = useState(false);

  const detail = useKernelQuery(
    () => fetchDetail((series?.raw ?? {}) as unknown as Record<string, unknown>),
    [series?.id ?? ''],
    { enabled: series !== null },
  );

  // 详情到货后把 chapters 写回会话：播放页据此选集，不再二次请求。
  const appliedRef = useRef<string>('');
  useEffect(() => {
    const data = detail.data;
    if (!series || !data) return;
    if (appliedRef.current === series.id) return;
    appliedRef.current = series.id;
    const episodes = (data.chapters ?? []).map((chapter, index) => toEpisode(chapter, index + 1));
    setSelection({ series: toSeries(data.drama), episodes, index: selection?.index ?? 1 });
  }, [detail.data, series, selection?.index, setSelection]);

  const episodes = selection?.episodes ?? [];

  const shown = useMemo(() => {
    const list = onlyUnwatched
      ? episodes.filter((episode) => !visited.has(episode.id))
      : episodes;
    return order === 'asc' ? list : [...list].reverse();
  }, [episodes, onlyUnwatched, order, visited]);

  const openEpisode = (episode: EpisodeView) => {
    if (!selection) return;
    markVisited(episode.id);
    setSelection({ ...selection, index: episode.index });
    navigate('/play');
  };

  if (!series) {
    return (
      <StateMessage
        icon={ChevronLeft}
        title="没有正在查看的剧集"
        description="这个页面需要从发现页进入（会话里没有剧集信息）。直接打开 /detail 不携带数据是有意的设计：不伪造一部剧给你看。"
        actions={
          <Button variant="outline" size="sm" onClick={() => navigate('/')}>
            去发现页
          </Button>
        }
      />
    );
  }

  return (
    <div className="mx-auto w-full max-w-[1180px] px-5 pt-4 pb-12">
      <div className="flex items-center gap-2 text-xs text-foreground-muted">
        <Button variant="ghost" size="xs" onClick={() => navigate('/')}>
          <ChevronLeft />
          发现
        </Button>
        <span>/</span>
        <span>{series.tag || '未分类'}</span>
      </div>

      {/* 头图：320px 横幅 + 底部渐变，剧名压在下沿（不用 Hero 大卡）。 */}
      <div className="relative mt-3 h-[200px] overflow-hidden rounded-lg bg-secondary md:h-[320px]">
        {series.cover ? (
          <img src={series.cover} alt="" className="size-full object-cover opacity-70" />
        ) : null}
        <div className="absolute inset-0 bg-linear-to-b from-transparent to-background" />
      </div>

      <div className="mt-6 grid gap-6 md:grid-cols-[216px_1fr]">
        <div className="hidden overflow-hidden rounded-xl bg-secondary md:block">
          <div className="aspect-[3/4]">
            {series.cover ? (
              <img src={series.cover} alt="" className="size-full object-cover" />
            ) : (
              <div className="flex size-full items-center justify-center text-2xs text-foreground-subtle">
                无封面
              </div>
            )}
          </div>
        </div>

        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-3">
            <h1 className="text-3xl font-bold tracking-tight">{series.title}</h1>
            {series.vip ? <Badge className="text-gold">VIP</Badge> : null}
          </div>

          <div className="mt-3 flex flex-wrap items-center gap-1.5">
            {series.tag ? <Badge variant="secondary">{series.tag}</Badge> : null}
            <Badge variant="secondary" className="tabular-nums">
              {series.total > 0 ? `全 ${series.total} 集` : '集数未知'}
            </Badge>
            {series.heat ? (
              <Badge variant="secondary" className="tabular-nums">
                {formatCount(series.heat)} 热度
              </Badge>
            ) : null}
            {detail.data?.warning ? (
              <Badge variant="outline" className="text-warning">
                {detail.data.warning}
              </Badge>
            ) : null}
          </div>

          <Separator className="my-4" />

          <p className={expanded ? 'max-w-[68ch] text-base' : 'line-clamp-3 max-w-[68ch] text-base'}>
            {series.description || '源未提供简介。'}
          </p>
          {series.description.length > 90 ? (
            <Button variant="link" size="xs" onClick={() => setExpanded((value) => !value)}>
              {expanded ? '收起' : '展开'}
            </Button>
          ) : null}

          <div className="mt-6 flex flex-wrap items-center gap-2">
            <Button size="lg" disabled={episodes.length === 0} onClick={() => openEpisode(episodes[0]!)}>
              <Play />
              开始播放
            </Button>
            <Button variant="secondary" size="lg" disabled title="追剧需要内核 storage 面，M4c 未接入">
              <Plus />
              追剧
            </Button>
            <Button
              variant="ghost"
              size="icon"
              disabled
              title="批量下载需要内核 downloads 面，M4c 未接入"
              aria-label="批量下载"
            >
              <Download />
            </Button>
            <Button
              variant="ghost"
              size="icon"
              onClick={() => detail.reload()}
              aria-label="重新加载详情"
            >
              <RefreshCw />
            </Button>
          </div>
        </div>
      </div>

      <Separator className="my-8" />

      <div className="mb-4 flex flex-wrap items-center gap-2">
        <h2 className="text-lg font-semibold">选集</h2>
        <span className="text-xs text-foreground-muted tabular-nums">
          {episodes.length > 0
            ? `${selection?.index ?? 1}/${episodes.length} 已选`
            : detail.loading
              ? '正在读取…'
              : '暂无分集'}
        </span>
        <div className="ml-auto flex items-center gap-2">
          <label className="flex items-center gap-1.5 text-xs text-foreground-muted">
            <Switch
              checked={onlyUnwatched}
              onCheckedChange={setOnlyUnwatched}
              aria-label="只看未看"
            />
            只看未看
          </label>
          <Select value={order} onValueChange={(value) => setOrder(value as 'asc' | 'desc')}>
            <SelectTrigger size="sm" className="w-[104px] text-xs">
              <ArrowDownWideNarrow className="size-3.5" />
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="asc">正序</SelectItem>
              <SelectItem value="desc">倒序</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>

      {episodes.length === 0 ? (
        detail.loading ? (
          <div className="grid grid-cols-[repeat(auto-fill,minmax(64px,1fr))] gap-2">
            {Array.from({ length: 24 }, (_, index) => (
              <div key={index} className="h-11 animate-pulse rounded-sm bg-skeleton" />
            ))}
          </div>
        ) : (
          <StateMessage
            icon={Star}
            title={detail.error ? '没能取到分集列表' : '这部剧没有分集信息'}
            description={
              detail.error?.offline
                ? '内核未运行，无法读取分集。'
                : (detail.error?.error ?? '内核返回的 chapters 是空的。')
            }
            detail={detail.error?.code ? `code=${detail.error.code}` : undefined}
            actions={
              <Button variant="outline" size="sm" onClick={() => detail.reload()}>
                <RefreshCw />
                重试
              </Button>
            }
          />
        )
      ) : (
        <EpisodeGrid
          episodes={shown}
          currentIndex={selection?.index}
          visited={visited}
          onSelect={openEpisode}
        />
      )}

      <Tabs defaultValue="intro" className="mt-10">
        <TabsList variant="line">
          <TabsTrigger value="intro">简介</TabsTrigger>
          <TabsTrigger value="episodes">选集 {episodes.length || ''}</TabsTrigger>
          <TabsTrigger value="danmaku">弹幕</TabsTrigger>
        </TabsList>
        <TabsContent value="intro" className="pt-4 text-base text-foreground-muted">
          {series.description || '源未提供简介。'}
        </TabsContent>
        <TabsContent value="episodes" className="pt-4 text-sm text-foreground-muted">
          选集网格在上方；此处作为 Tabs 的第二入口保留，便于后续挂载「批量下载进度」。
        </TabsContent>
        <TabsContent value="danmaku" className="pt-4 text-sm text-foreground-muted">
          弹幕时间轴（DanmakuTimeline）需要内核 `danmaku` 面的分页数据，M4c 只接了播放器内
          的弹幕层，此处的聚合列表未接入。
        </TabsContent>
      </Tabs>
    </div>
  );
}
