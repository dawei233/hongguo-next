/**
 * 内核**实际**返回的载荷形态（nativeInput.action 面）。
 *
 * 为什么单独一层、而不是直接用 `contracts/bridge-api.schema.json` 的 `$defs/Series|Episode|Stream`：
 * hncore-server README §7.3 已明确记录 —— PC 服务端**原样透传内核载荷**、不做字段翻译
 * （`Stream` 的 `encrypted/container/needsSanitize` 在内核载荷里根本不存在，
 * `expiresAt` 内核给的是 Unix 毫秒而契约要求 RFC3339，`quality` 内核是像素高度而契约是字符串）。
 * 翻译属于 M3/M4 的站源适配层，不属于桥接层。
 *
 * 因此本文件如实描述「今天真的会收到什么」，字段名逐字来自 Go 结构体：
 * - `nativeDrama`     → core/app_runtime.go
 * - `Chapter`         → core/models.go
 * - `nativePlan`      → core/app_runtime.go
 * - `hongguoDanmaku*` → core/ui_playback_danmaku.go
 * - `nativeCatalogResult` / `nativeCategory` → core/app_runtime.go / app_categories.go
 *
 * 契约面（Series/Episode/Stream）由 `adapt.ts` 做一次性映射，UI 只消费适配后的视图模型。
 */

/** core/app_runtime.go 的 nativeDrama。 */
export interface NativeDrama {
  metadataSchema?: number;
  id: string;
  source?: string;
  sourceId?: string;
  title?: string;
  description?: string;
  cover?: string;
  episodes?: number;
  category?: string;
  vip?: boolean | null;
  heat?: string;
  views?: string;
  onlineDate?: string;
  tags?: string[];
  releaseStatus?: string;
}

/** core/models.go 的 Chapter。 */
export interface NativeChapter {
  vip?: boolean;
  id: string;
  source?: string;
  title?: string;
  videoUrl?: string;
  /** 源给的集号，可能是字符串或数字（json.RawMessage）。 */
  currentEpisode?: string | number | null;
  mediaSize?: number;
  pageUrl?: string;
  referer?: string;
}

/** `detail` 动作的 data。 */
export interface NativeDetailData {
  drama: NativeDrama;
  chapters: NativeChapter[];
  warning?: string;
}

/** `catalog` / `recommendations` / `cached` 动作的 data。 */
export interface NativeCatalogData {
  items: NativeDrama[];
  hasMore?: boolean;
  page?: number;
  warning?: string;
  localSearch?: boolean;
  fresh?: boolean;
}

/** `categories` 动作的 data。 */
export interface NativeCategoriesData {
  items: NativeCategory[];
}

export interface NativeCategory {
  id: string;
  name: string;
}

/** core/app_runtime.go 的 nativePlan（`resolve` 动作的 data）。 */
export interface NativePlan {
  /** Unix 毫秒（不是契约要求的 RFC3339）。 */
  expiresAt?: number;
  prefetchedBytes?: number;
  danmakuId?: string;
  local?: boolean;
  url: string;
  headers?: Record<string, string> | null;
  /** AES-128 密钥的十六进制形态；仅加密源存在。 */
  decryptionKey?: string;
  /** 像素高度（如 1080），不是 "1080p" 字符串。 */
  quality?: number;
  qualities?: number[];
  session?: string;
  routeIndex?: number;
  routeCount?: number;
}

/** core/ui_playback_danmaku.go 的 hongguoDanmakuItem / hongguoDanmakuPage。 */
export interface NativeDanmakuItem {
  id: string;
  text: string;
  timeMs: number;
}

export interface NativeDanmakuPage {
  episodeId?: string;
  items: NativeDanmakuItem[];
  startMs?: number;
  nextMs?: number;
  total?: number;
}

/** `initialize` 动作的 data。 */
export interface NativeInitializeData {
  version?: string;
  standalone?: boolean;
  allSources?: boolean;
}

/** `server:ping` 控制帧的 data（hncore-server README §3.3）。 */
export interface NativePingData {
  pong?: boolean;
  serverVersion?: string;
  protocolVersion?: string;
  pid?: number;
  uptimeMs?: number;
  workers?: number;
  maxFrameBytes?: number;
  requestTimeout?: string;
  inflight?: number;
  stopping?: boolean;
}
