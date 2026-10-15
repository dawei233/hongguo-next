import { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { ChevronLeft, ListVideo, RefreshCw, ServerOff, TriangleAlert } from 'lucide-react';
import { Container } from '@videojs/react';
// HLS 适配器不在包根导出：v10 把它放在 ./media/* 子路径（见 package.json 的 exports）。
import { HlsJsVideo } from '@videojs/react/media/hlsjs-video';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet';
import { DanmakuLayer } from '@/components/hongguo/DanmakuLayer';
import { EpisodeGrid } from '@/components/hongguo/EpisodeGrid';
import { StateMessage } from '@/components/hongguo/StateMessage';
import { fetchDanmaku, resolveStream } from '@/lib/api';
import { sortQualities, toStream, type EpisodeView } from '@/lib/adapt';
import { useKernelQuery } from '@/lib/hooks';
import { useSession } from '@/lib/session';
import { episodeLabel } from '@/lib/utils';
import { HnPlayer, useHnPlayer } from '@/player/hn-player';
import { PlayerControls } from '@/player/PlayerControls';

/**
 * 播放页（DESIGN-SYSTEM.md §5.3）。
 *
 * 三层结构，各自解决一件事：
 * 1. 数据层：`resolve` 拿 plan（URL/画质/会话），`danmaku` 拿弹幕页。都是内核数据，
 *    没有第二条网络路径 —— 页面自己不发任何 provider 请求。
 * 2. 播放层：`<HnPlayer>`（createPlayer 产物）提供 store，`<HlsJsVideo>` 挂源，
 *    控制条由 `@videojs/react` 零件组装（见 PlayerControls.tsx 的取舍说明）。
 * 3. 覆盖层：弹幕层（rAF 手写位移）、选集 Sheet、离线/错误空态。
 *
 * 关于切换画质：**不是**本地改 hls.js rendition，而是把新的像素高度回传内核重新
 * `resolve`。源侧的多档 URL 由内核决定，本地切档只影响同一 manifest 的 ABR 选择，
 * 两者语义不同（见 api.ts 的 resolveStream 注释）。
 */
const DANMAKU_WINDOW_MS = 30_000;

export function PlayerPage() {
  const navigate = useNavigate();
  const { selection, setSelection, visited, markVisited } = useSession();
  const [quality, setQuality] = useState(0);
  const [danmakuEnabled, setDanmakuEnabled] = useState(true);
  const [episodesOpen, setEpisodesOpen] = useState(false);

  const series = selection?.series ?? null;
  const episodes = selection?.episodes ?? [];
  const current = useMemo(
    () => episodes.find((episode) => episode.index === selection?.index) ?? episodes[0] ?? null,
    [episodes, selection?.index],
  );

  // 进页即记为已看（会话内），不落盘。
  useEffect(() => {
    if (current?.id) markVisited(current.id);
  }, [current?.id, markVisited]);

  const plan = useKernelQuery(
    () =>
      resolveStream({
        drama: (series?.raw ?? {}) as unknown as Record<string, unknown>,
        chapter: (current?.raw ?? {}) as unknown as Record<string, unknown>,
        quality,
      }),
    [series?.id ?? '', current?.id ?? '', quality],
    { enabled: Boolean(series && current) },
  );

  const stream = useMemo(() => (plan.data ? toStream(plan.data) : null), [plan.data]);
  const qualities = useMemo(() => sortQualities(stream?.qualities ?? []), [stream]);

  // 弹幕：按窗口取页。`session` 来自 plan，是内核给这次播放的句柄。
  const [danmakuStart, setDanmakuStart] = useState(0);
  useEffect(() => {
    setDanmakuStart(0);
  }, [stream?.session]);

  const danmaku = useKernelQuery(
    () =>
      fetchDanmaku({
        playbackSession: stream?.session ?? '',
        startMs: danmakuStart,
        durationMs: DANMAKU_WINDOW_MS,
      }),
    [stream?.session ?? '', danmakuStart],
    { enabled: Boolean(stream?.session) },
  );

  const danmakuItems = useMemo(() => danmaku.data?.items ?? [], [danmaku.data]);

  const selectEpisode = useCallback(
    (episode: EpisodeView) => {
      if (!selection) return;
      markVisited(episode.id);
      setSelection({ ...selection, index: episode.index });
      setEpisodesOpen(false);
      setQuality(0);
    },
    [markVisited, selection, setSelection],
  );

  if (!series || !current) {
    return (
      <StateMessage
        icon={ListVideo}
        title="没有可播放的剧集"
        description={
          selection
            ? '会话里只有剧集信息、没有分集（详情还没加载完，或这部剧的 chapters 是空的）。先回详情页取一次分集。'
            : '播放页需要从详情页进入；直接打开 /play 不携带数据时不会伪造播放源。'
        }
        actions={
          <>
            <Button variant="outline" size="sm" onClick={() => navigate('/detail')}>
              回详情页
            </Button>
            <Button variant="ghost" size="sm" onClick={() => navigate('/')}>
              去发现页
            </Button>
          </>
        }
      />
    );
  }

  const offline = plan.error?.offline === true;

  return (
    <div data-always-dark className="flex min-h-full flex-col bg-background">
      {/* 标题条：56px，鼠标静止后与控制条一起淡出由 Controls 的 data-visible 驱动。 */}
      <header className="flex h-[56px] shrink-0 items-center gap-3 border-b border-border-subtle bg-background/80 px-4 backdrop-blur-md">
        <Button variant="ghost" size="icon-sm" onClick={() => navigate('/detail')} aria-label="返回详情">
          <ChevronLeft />
        </Button>
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate text-base font-semibold">{series.title}</span>
          <span className="text-foreground-subtle">·</span>
          <span className="truncate text-sm text-foreground-muted">
            {episodeLabel(current.index, current.title)}
          </span>
        </div>
        <div className="ml-auto flex items-center gap-2">
          {stream?.local ? <Badge variant="secondary">本地文件</Badge> : null}
          {stream?.encrypted ? <Badge variant="outline">已解密源</Badge> : null}
          <Button
            variant="outline"
            size="sm"
            className="tabular-nums"
            onClick={() => setEpisodesOpen(true)}
            disabled={episodes.length === 0}
          >
            选集 {episodes.length > 0 ? `${current.index}/${episodes.length}` : '—'}
          </Button>
        </div>
      </header>

      <div className="flex min-h-0 flex-1 flex-col items-center bg-stage px-4 py-4">
        <div className="relative aspect-video w-full max-w-[1080px] overflow-hidden rounded-lg border border-border-subtle bg-stage">
          <HnPlayer title={series.title} poster={series.cover}>
            <Container className="absolute inset-0">
              {stream ? (
                <HlsJsVideo
                  src={stream.url}
                  source={{ src: stream.url, preferPlayback: 'mse' }}
                  className="size-full"
                  playsInline
                />
              ) : null}
              <DanmakuOverlay enabled={danmakuEnabled} items={danmakuItems} />
              <PlayerControls
                danmakuEnabled={danmakuEnabled}
                onDanmakuChange={setDanmakuEnabled}
                onOpenEpisodes={() => setEpisodesOpen(true)}
                qualities={qualities}
                quality={quality}
                onQualityChange={setQuality}
                disabled={!stream}
              />
            </Container>
          </HnPlayer>

          {!stream ? (
            <div className="absolute inset-0 flex items-center justify-center bg-stage">
              {plan.loading ? (
                <span className="text-sm text-foreground-muted">正在向内核请求播放地址…</span>
              ) : offline ? (
                <StateMessage
                  icon={ServerOff}
                  title="内核未运行，无法取得播放地址"
                  description="播放地址由本地内核 resolve 产生（含必要的请求头与解密密钥），内核不可用时不会退化成任何直连尝试。"
                  detail={plan.error?.bridge ? `bridge=${plan.error.bridge}` : undefined}
                  actions={
                    <Button variant="outline" size="sm" onClick={() => plan.reload()}>
                      <RefreshCw />
                      重试
                    </Button>
                  }
                />
              ) : (
                <StateMessage
                  icon={TriangleAlert}
                  title="这一集没能解析出播放地址"
                  description={plan.error?.error ?? '内核没有返回 plan。'}
                  detail={plan.error?.code ? `code=${plan.error.code}` : undefined}
                  actions={
                    <Button variant="outline" size="sm" onClick={() => plan.reload()}>
                      <RefreshCw />
                      重试
                    </Button>
                  }
                />
              )}
            </div>
          ) : null}
        </div>

        <PlayerMeta
          stream={stream}
          danmakuCount={danmaku.data?.total ?? danmakuItems.length}
          routeIndex={stream?.routeIndex ?? 0}
          routeCount={stream?.routeCount ?? 0}
          expiresAt={stream?.expiresAt ?? 0}
          onNextWindow={() => setDanmakuStart((value) => value + DANMAKU_WINDOW_MS)}
        />
      </div>

      <Sheet open={episodesOpen} onOpenChange={setEpisodesOpen}>
        <SheetContent side="right" className="w-[380px] p-0 sm:max-w-[380px]">
          <SheetHeader className="border-b border-border-subtle">
            <SheetTitle className="text-base">{series.title}</SheetTitle>
            <SheetDescription className="text-xs">
              共 {episodes.length} 集 · 当前 {episodeLabel(current.index, current.title)}
            </SheetDescription>
          </SheetHeader>
          <div className="min-h-0 flex-1 overflow-y-auto px-4 py-3">
            <EpisodeGrid
              episodes={episodes}
              currentIndex={current.index}
              visited={visited}
              onSelect={selectEpisode}
            />
          </div>
        </SheetContent>
      </Sheet>
    </div>
  );
}

/**
 * 弹幕覆盖层：把播放器 store 的 `currentTime` 交给 `DanmakuLayer`。
 *
 * 必须放在 `<HnPlayer>` 内部 —— 时间源只有一个（store），不接受第二份「我自己算的时间」。
 */
function DanmakuOverlay({
  enabled,
  items,
}: {
  enabled: boolean;
  items: { id: string; text: string; timeMs: number }[];
}) {
  const currentTime = useHnPlayer((state) => state.currentTime);
  const paused = useHnPlayer((state) => state.paused);
  return (
    <DanmakuLayer
      enabled={enabled}
      items={items}
      currentTimeMs={Math.max(0, currentTime) * 1000}
      playing={!paused}
    />
  );
}

interface PlayerMetaProps {
  stream: ReturnType<typeof toStream> | null;
  danmakuCount: number;
  routeIndex: number;
  routeCount: number;
  expiresAt: number;
  onNextWindow: () => void;
}

/** 播放台下方的诊断条：把「内核告诉了我们什么」如实摊开，方便排障。 */
function PlayerMeta({
  stream,
  danmakuCount,
  routeIndex,
  routeCount,
  expiresAt,
  onNextWindow,
}: PlayerMetaProps) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 30_000);
    return () => clearInterval(timer);
  }, []);

  const remaining = expiresAt > 0 ? Math.max(0, expiresAt - now) : 0;

  return (
    <div className="mt-3 flex w-full max-w-[1080px] flex-wrap items-center gap-3 text-2xs text-foreground-subtle">
      <span className="tabular-nums">弹幕 {danmakuCount} 条</span>
      <Button variant="ghost" size="xs" onClick={onNextWindow}>
        取下一段弹幕
      </Button>
      {routeCount > 1 ? (
        <span className="tabular-nums">
          线路 {routeIndex + 1}/{routeCount}
        </span>
      ) : null}
      {expiresAt > 0 ? (
        <span className="tabular-nums">地址剩余有效期 {Math.round(remaining / 1000)}s</span>
      ) : null}
      {stream?.quality ? <span className="tabular-nums">源画质 {stream.quality}P</span> : null}
      <span>画质切换会回内核重新 resolve（不是本地 ABR 切档）</span>
    </div>
  );
}
