import { clsx, type ClassValue } from 'clsx';
import { twMerge } from 'tailwind-merge';

/** shadcn 约定：clsx 合并条件类名，tailwind-merge 消解冲突的 Tailwind 类。 */
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}

/** 集数、时长、热度等数字统一走等宽字形，避免进度与计数抖动（DESIGN-SYSTEM.md §3.2）。 */
export function formatCount(value: string | number | undefined | null): string {
  if (value === undefined || value === null || value === '') return '';
  const numeric = typeof value === 'number' ? value : Number(value);
  if (!Number.isFinite(numeric)) return String(value);
  if (numeric >= 100_000_000) return `${(numeric / 100_000_000).toFixed(1)}亿`;
  if (numeric >= 10_000) return `${(numeric / 10_000).toFixed(0)}万`;
  return String(numeric);
}

/** 秒 → `mm:ss` / `h:mm:ss`。 */
export function formatDuration(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) return '--:--';
  const total = Math.floor(seconds);
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const secs = total % 60;
  const pad = (n: number) => String(n).padStart(2, '0');
  return hours > 0 ? `${hours}:${pad(minutes)}:${pad(secs)}` : `${pad(minutes)}:${pad(secs)}`;
}

/** 取集标题：源未给标题时用「第 N 集」兜底，不编造片名。 */
export function episodeLabel(index: number, title: string | undefined): string {
  const trimmed = title?.trim();
  if (trimmed) return trimmed;
  return index > 0 ? `第 ${index} 集` : '未知集';
}

/** 两字中文序号：`01`…`78`，用于选集格（tabular-nums 对齐）。 */
export function episodeNumber(index: number): string {
  return index > 0 ? String(index).padStart(2, '0') : '--';
}
