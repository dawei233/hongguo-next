import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { AlertTriangle, Play, Plus, RefreshCw, ServerOff, SlidersHorizontal } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { PosterCard, PosterCardSkeleton } from '@/components/hongguo/PosterCard';
import { StateMessage } from '@/components/hongguo/StateMessage';
import { fetchCatalog, fetchCategories, fetchRecommendations, hasBridge } from '@/lib/api';
import { toSeries, type SeriesView } from '@/lib/adapt';
import { useKernelQuery } from '@/lib/hooks';
import { useSession } from '@/lib/session';
import { formatCount } from '@/lib/utils';
import { getHnBridge } from '@shared/ipc';

/**
 * 发现页（DESIGN-SYSTEM.md §5.1）。
 *
 * 数据来源只有内核，没有第二条路径：
 * - 题材列表 ← `categories`；
 * - 网格 ← `catalog {source, category, page}`；
 * - Hero ← `recommendations`（取不到就退回网格首条，绝不编造“编辑推荐”）。
 *
 * 「推荐」是内核 `provider_huangguo.go` 里真实存在的分类名（与 `catalog` 的 category
 * 同域），不是我们发明的兜底文案；但取不到时仍然如实显示空态。
 */
const DEFAULT_SOURCE = 'hongguo';
const DEFAULT_CATEGORY = '推荐';

export function DiscoverPage() {
  const navigate = useNavigate();
  const { query, setSelection } = useSession();
  const [category, setCategory] = useState<string | null>(null);

  const categories = useKernelQuery(() => fetchCategories(DEFAULT_SOURCE), [DEFAULT_SOURCE]);
  const categoryNames = useMemo(
    () => (categories.data?.items ?? []).map((item) => item.name).filter(Boolean),
    [categories.data],
  );

  // 题材列表到货后落到第一个真实分类上；用户点过之后不再自动覆盖。
  useEffect(() => {
    if (category !== null) return;
    if (categoryNames.length > 0) setCategory(categoryNames[0] ?? null);
  }, [category, categoryNames]);

  const activeCategory = category ?? DEFAULT_CATEGORY;
  const catalog = useKernelQuery(
    () => fetchCatalog({ source: DEFAULT_SOURCE, category: activeCategory, page: 1 }),
    [activeCategory],
  );
  const recommendations = useKernelQuery(() => fetchRecommendations(DEFAULT_CATEGORY), []);

  const items = useMemo(() => {
    const raw = catalog.data?.items ?? [];
    return raw.map(toSeries);
  }, [catalog.data]);

  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase();
    if (!needle) return items;
    return items.filter((series) =>
      [series.title, series.tag, series.category, ...series.tags]
        .join(' ')
        .toLowerCase()
        .includes(needle),
    );
  }, [items, query]);

  const hero = useMemo<SeriesView | null>(() => {
    const fromRecommendation = recommendations.data?.items?.[0];
    if (fromRecommendation) return toSeries(fromRecommendation);
    return items[0] ?? null;
  }, [recommendations.data, items]);

  const open = (series: SeriesView) => {
    // 详情页负责补全 chapters；这里先落剧本身，播放页在 chapters 为空时不会假装能播。
    setSelection({ series, episodes: [], index: 1 });
    navigate('/detail');
  };

  const offline = catalog.error?.offline === true;
  const failed = catalog.error !== null;

  return (
    <div className="mx-auto w-full max-w-[1180px] px-5 pt-6 pb-12">
      {hero ? (
        <HeroCard
          series={hero}
          rank={1}
          onOpen={() => open(hero)}
          onRefresh={() => {
            catalog.reload();
            recommendations.reload();
          }}
        />
      ) : null}

      <div className="mt-8 mb-5 flex items-center gap-2 overflow-x-auto">
        {categoryNames.length > 0 ? (
          <Tabs value={activeCategory} onValueChange={setCategory}>
            <TabsList variant="line" className="h-9">
              {categoryNames.map((name) => (
                <TabsTrigger key={name} value={name} className="text-sm">
                  {name}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
        ) : (
          <span className="text-sm text-foreground-subtle">
            {categories.loading
              ? '正在读取题材…'
              : categories.error
                ? // 内核不可用时不要说“源未提供题材列表”：那是把桥接故障说成源侧事实。
                  categories.error.offline
                  ? '内核未运行，题材列表不可用'
                  : `题材列表读取失败：${categories.error.error}`
                : '源未提供题材列表'}
          </span>
        )}
        <Button
          variant="ghost"
          size="icon-sm"
          className="ml-auto shrink-0"
          onClick={() => catalog.reload()}
          aria-label="刷新"
        >
          <RefreshCw />
        </Button>
      </div>

      {catalog.loading && items.length === 0 ? (
        <div className="grid grid-cols-[repeat(auto-fill,minmax(150px,1fr))] gap-x-5 gap-y-6">
          {Array.from({ length: 12 }, (_, index) => (
            <PosterCardSkeleton key={index} />
          ))}
        </div>
      ) : offline ? (
        <StateMessage
          icon={ServerOff}
          title="内核未运行，暂时没有内容"
          description={
            <>
              桌面端的所有数据都来自本地内核进程 hncore-server。当前内核不可用，因此这个页面
              <strong className="text-foreground">不会有任何占位数据</strong>。
            </>
          }
          detail={catalog.error?.bridge ? `bridge=${catalog.error.bridge}` : undefined}
          actions={
            <>
              <Button variant="outline" size="sm" onClick={() => catalog.reload()}>
                <RefreshCw />
                重试
              </Button>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => {
                  void getHnBridge()?.restart();
                }}
                disabled={!hasBridge()}
              >
                重启内核
              </Button>
            </>
          }
        />
      ) : failed ? (
        <StateMessage
          icon={AlertTriangle}
          title="取内容失败"
          description={catalog.error?.error ?? '内核返回了错误，但没有给出原因'}
          detail={catalog.error?.code ? `code=${catalog.error.code}` : undefined}
          actions={
            <Button variant="outline" size="sm" onClick={() => catalog.reload()}>
              <RefreshCw />
              重试
            </Button>
          }
        />
      ) : filtered.length === 0 ? (
        <StateMessage
          icon={SlidersHorizontal}
          title={query.trim() ? '没有匹配合适的短剧' : '这个分类下还没有内容'}
          description={
            query.trim()
              ? `筛选条件是「${query.trim()}」，只作用于已加载的 ${items.length} 部（本地筛选，未走网络）。`
              : `${activeCategory} 分类返回了 0 条。可以换一个题材，或稍后重试。`
          }
          actions={
            <Button variant="outline" size="sm" onClick={() => catalog.reload()}>
              <RefreshCw />
              重新加载
            </Button>
          }
        />
      ) : (
        <div className="grid grid-cols-[repeat(auto-fill,minmax(150px,1fr))] gap-x-5 gap-y-6">
          {filtered.map((series, index) => (
            <PosterCard key={series.id} series={series} rank={index + 1} onOpen={open} />
          ))}
        </div>
      )}

      {catalog.data?.hasMore ? (
        <p className="mt-8 text-center text-xs text-foreground-subtle">
          源报告还有更多结果；分页交互（page 递增 / 无限滚动）不在 M4c 范围内。
        </p>
      ) : null}
    </div>
  );
}

interface HeroCardProps {
  series: SeriesView;
  rank: number;
  onOpen: () => void;
  onRefresh: () => void;
}

function HeroCard({ series, rank, onOpen, onRefresh }: HeroCardProps) {
  return (
    <section className="flex gap-8 rounded-lg border border-border bg-card p-6">
      <div className="hidden w-[192px] shrink-0 overflow-hidden rounded-xl bg-secondary sm:block">
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

      <div className="flex min-w-0 flex-1 flex-col">
        <div className="flex items-start gap-3">
          <h1 className="text-3xl font-bold tracking-tight">{series.title}</h1>
          <Badge variant="secondary" className="mt-1 shrink-0 tabular-nums">
            第 {rank} 位
          </Badge>
        </div>

        <p className="mt-2 flex flex-wrap items-center gap-2 text-xs text-foreground-muted">
          {series.tag ? <span>{series.tag}</span> : null}
          {series.total > 0 ? <span>{series.total} 集</span> : null}
          {series.heat ? <span className="tabular-nums">{formatCount(series.heat)} 热度</span> : null}
        </p>

        <p className="mt-4 line-clamp-3 max-w-[65ch] text-base leading-[1.8] text-foreground-muted">
          {series.description || '源未提供简介。'}
        </p>

        <div className="mt-6 flex items-center gap-2">
          <Button size="lg" onClick={onOpen} disabled={series.total === 0 && !series.id}>
            <Play />
            开始播放
          </Button>
          <Button variant="secondary" size="lg" onClick={onOpen}>
            <Plus />
            查看详情
          </Button>
          <Button variant="ghost" size="icon" onClick={onRefresh} aria-label="刷新推荐">
            <RefreshCw />
          </Button>
        </div>
      </div>
    </section>
  );
}
