import type { NativeChapter, NativeDrama, NativePlan } from './core-types';

/**
 * 内核载荷 → 视图模型。
 *
 * 这是 README §7.3 说的那个「适配层」在端侧的**最小**形态：只做 UI 需要的归一，
 * 不假装补齐契约字段（例如不发明 `container` / `needsSanitize`），缺失就是缺失。
 */

export interface SeriesView {
  id: string;
  sourceId: string;
  title: string;
  cover: string;
  year: number;
  total: number;
  tag: string;
  score: string;
  description: string;
  heat: string;
  views: string;
  category: string;
  tags: string[];
  vip: boolean;
  /** 原始 drama：detail / resolve 必须原样回传，内核用它的 id+source 定位源内剧。 */
  raw: NativeDrama;
}

export interface EpisodeView {
  id: string;
  index: number;
  title: string;
  durationS: number;
  vip: boolean;
  raw: NativeChapter;
}

export interface StreamView {
  url: string;
  /** 像素高度；0 表示源未给出。 */
  quality: number;
  qualities: number[];
  session: string;
  local: boolean;
  /** Unix 毫秒；0 表示未知。 */
  expiresAt: number;
  headers: Record<string, string>;
  encrypted: boolean;
  danmakuId: string;
  routeIndex: number;
  routeCount: number;
}

export function toSeries(drama: NativeDrama): SeriesView {
  const id = drama.id ?? '';
  const colon = id.indexOf(':');
  const sourceId = drama.source ?? (colon > 0 ? id.slice(0, colon) : '');
  return {
    id,
    sourceId,
    title: drama.title?.trim() || '短剧',
    cover: drama.cover ?? '',
    year: typeof drama.episodes === 'number' ? 0 : 0,
    total: typeof drama.episodes === 'number' ? drama.episodes : 0,
    tag: drama.category ?? '',
    score: '',
    description: drama.description ?? '',
    heat: drama.heat ?? '',
    views: drama.views ?? '',
    category: drama.category ?? '',
    tags: drama.tags ?? [],
    vip: drama.vip === true,
    raw: drama,
  };
}

/** `nativeDrama.onlineDate` 是源给的文案，能取到 4 位年份就取，取不到就是 0（未知）。 */
export function seriesYear(drama: NativeDrama): number {
  const match = /(19|20)\d{2}/.exec(drama.onlineDate ?? '');
  return match ? Number(match[0]) : 0;
}

export function toEpisode(chapter: NativeChapter, fallbackIndex: number): EpisodeView {
  const rawIndex = chapter.currentEpisode;
  const parsed =
    typeof rawIndex === 'number' ? rawIndex : typeof rawIndex === 'string' ? Number.parseInt(rawIndex, 10) : Number.NaN;
  const index = Number.isFinite(parsed) && parsed > 0 ? parsed : fallbackIndex;
  return {
    id: chapter.id ?? '',
    index,
    title: chapter.title?.trim() ?? '',
    durationS: 0, // Chapter 不携带时长（contracts 里 Episode.durationS=0 表示未知）
    vip: chapter.vip === true,
    raw: chapter,
  };
}

export function toStream(plan: NativePlan): StreamView {
  return {
    url: plan.url ?? '',
    quality: typeof plan.quality === 'number' ? plan.quality : 0,
    qualities: plan.qualities ?? [],
    session: plan.session ?? '',
    local: plan.local === true,
    expiresAt: typeof plan.expiresAt === 'number' ? plan.expiresAt : 0,
    headers: plan.headers ?? {},
    // 内核载荷里的 decryptionKey 存在即代表需要解密（契约的 encrypted 字段内核不产出）。
    encrypted: typeof plan.decryptionKey === 'string' && plan.decryptionKey.length > 0,
    danmakuId: plan.danmakuId ?? '',
    routeIndex: plan.routeIndex ?? 0,
    routeCount: plan.routeCount ?? 0,
  };
}

/** 像素高度 → 画质标签（契约 Quality 是 "auto"|"1080p"，内核给的是像素高度）。 */
export function qualityLabel(height: number): string {
  if (!height || height <= 0) return '自动';
  return `${height}P`;
}

/** 把内核的 qualities（像素高度数组）排序成 UI 顺序：高的在前。 */
export function sortQualities(qualities: number[]): number[] {
  return [...new Set(qualities.filter((value) => value > 0))].sort((a, b) => b - a);
}
