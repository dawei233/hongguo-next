import {
  BufferingIndicator,
  Controls as ControlsParts,
  FullscreenButton,
  MuteButton,
  PiPButton,
  PlayButton,
  PlaybackRateButton,
  SeekButton,
  Time,
  TimeSlider,
} from '@videojs/react';
import type { ReactNode } from 'react';
import { Check, ListVideo, MessageSquare, MessageSquareOff, Settings2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Switch } from '@/components/ui/switch';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { qualityLabel } from '@/lib/adapt';
import { cn } from '@/lib/utils';

/**
 * 控制条（DESIGN-SYSTEM.md §5.3）。
 *
 * 用 `@videojs/react` 的零件组装而不是自己画进度条：`TimeSlider` 自带 buffer 区、键盘
 * 步进、指针捕获与无障碍语义，重画等于把这四样都丢掉（§2.3 的结论）。
 *
 * 显隐：由 `<Controls.Root>` 产生的 `data-visible` / `data-user-active` 驱动（来自
 * core 的 `ControlsDataAttrs`），不用 CSS `:fullscreen`、不监听全屏事件 —— 全屏走
 * Electron 原生全屏（main/index.ts 的 bindNativeFullscreen），控制条与标题条一起淡出。
 *
 * 与规范的**有意差异**（交付说明里列出）：
 * - 音量只有静音开关（`MuteButton`）；`VolumePopover` 在 v10 是需要自行拼 Popup +
 *   VolumeSlider 的复合零件，M4c 不做半成品，音量滑杆放设置页；
 * - 倍速用 `PlaybackRateButton`（循环档位）而不是下拉菜单，避免自己拼 Menu 复合件；
 * - 画质菜单读的是**内核 resolve 出来的画质档**，不是 hls.js 的 rendition 列表：
 *   源侧画质由内核决定，本地 rendition 只反映同一 manifest 内的 ABR 档位，混在一起
 *   会让用户以为换了源。
 */
export interface PlayerControlsProps {
  danmakuEnabled: boolean;
  onDanmakuChange: (next: boolean) => void;
  onOpenEpisodes: () => void;
  /** 已解析出来的画质档（像素高度），来自内核 plan.qualities。 */
  qualities: number[];
  /** 当前档位；0 表示「自动」。 */
  quality: number;
  onQualityChange: (height: number) => void;
  disabled?: boolean;
}

export function PlayerControls({
  danmakuEnabled,
  onDanmakuChange,
  onOpenEpisodes,
  qualities,
  quality,
  onQualityChange,
  disabled = false,
}: PlayerControlsProps) {
  return (
    <ControlsParts.Root>
      <ControlsParts.Backdrop className="pointer-events-none absolute inset-0 bg-linear-to-t from-scrim to-transparent opacity-0 transition-opacity duration-200 data-[visible]:opacity-100" />

      <BufferingIndicator
        className="absolute inset-0 flex items-center justify-center"
        render={(props, state) => (
          <div {...props} data-active={state.visible ? '' : undefined}>
            {state.visible ? (
              <span className="rounded-full bg-scrim px-3 py-1 text-xs text-danmaku">
                缓冲中…
              </span>
            ) : null}
          </div>
        )}
      />

      <ControlsParts.Content className="absolute inset-x-0 bottom-0 flex flex-col gap-1 bg-scrim px-3 pt-6 pb-2 opacity-0 backdrop-blur-md transition-opacity duration-200 data-[visible]:opacity-100">
        <TimeSlider.Root className="group/slider relative h-4 w-full" aria-label="播放进度">
          <TimeSlider.Track className="absolute inset-x-0 top-1/2 h-1.5 -translate-y-1/2 rounded-full bg-danmaku/20">
            <TimeSlider.Buffer className="absolute inset-y-0 rounded-full bg-danmaku/30" />
            <TimeSlider.Fill className="absolute inset-y-0 rounded-full bg-primary" />
          </TimeSlider.Track>
          <TimeSlider.Thumb className="absolute top-1/2 size-3.5 -translate-x-1/2 -translate-y-1/2 rounded-full bg-danmaku opacity-0 transition-opacity group-hover/slider:opacity-100 focus-visible:opacity-100" />
        </TimeSlider.Root>

        <div className="flex items-center gap-2">
          <ControlsParts.Group className="flex items-center gap-1">
            <ControlButton label="播放 / 暂停">
              <PlayButton className="flex size-9 items-center justify-center rounded-sm text-danmaku hover:bg-accent/40" />
            </ControlButton>
            <ControlButton label="后退 10 秒">
              <SeekButton
                seconds={-10}
                className="flex size-8 items-center justify-center rounded-sm text-xs text-danmaku hover:bg-accent/40"
              />
            </ControlButton>
            <ControlButton label="前进 10 秒">
              <SeekButton
                seconds={10}
                className="flex size-8 items-center justify-center rounded-sm text-xs text-danmaku hover:bg-accent/40"
              />
            </ControlButton>
            <ControlButton label="静音">
              <MuteButton className="flex size-8 items-center justify-center rounded-sm text-danmaku hover:bg-accent/40" />
            </ControlButton>
          </ControlsParts.Group>

          <ControlsParts.Group className="flex items-center gap-1 font-numeric text-xs text-danmaku tabular-nums">
            <Time.Value type="current" />
            <span className="text-danmaku/60">/</span>
            <Time.Value type="duration" />
          </ControlsParts.Group>

          <ControlsParts.Group className="ml-auto flex items-center gap-1">
            <Button
              variant="ghost"
              size="icon-sm"
              className="text-danmaku hover:bg-accent/40"
              disabled={disabled}
              onClick={onOpenEpisodes}
              aria-label="选集"
            >
              <ListVideo />
            </Button>

            <Button
              variant="ghost"
              size="icon-sm"
              className="text-danmaku hover:bg-accent/40"
              onClick={() => onDanmakuChange(!danmakuEnabled)}
              aria-pressed={danmakuEnabled}
              aria-label={danmakuEnabled ? '关闭弹幕' : '打开弹幕'}
            >
              {danmakuEnabled ? <MessageSquare /> : <MessageSquareOff />}
            </Button>

            <label className="flex h-8 cursor-pointer items-center gap-1.5 rounded-sm px-2 text-xs text-danmaku hover:bg-accent/40">
              <Switch
                checked={danmakuEnabled}
                onCheckedChange={onDanmakuChange}
                aria-label="弹幕开关"
              />
              弹幕
            </label>

            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={disabled}
                  className="text-danmaku hover:bg-accent/40"
                >
                  <Settings2 />
                  {qualityLabel(quality)}
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-40">
                <DropdownMenuLabel>画质</DropdownMenuLabel>
                <DropdownMenuSeparator />
                <DropdownMenuItem onSelect={() => onQualityChange(0)}>
                  <Check className={cn('size-3.5', quality === 0 ? 'opacity-100' : 'opacity-0')} />
                  自动
                </DropdownMenuItem>
                {qualities.map((height) => (
                  <DropdownMenuItem key={height} onSelect={() => onQualityChange(height)}>
                    <Check
                      className={cn('size-3.5', quality === height ? 'opacity-100' : 'opacity-0')}
                    />
                    {qualityLabel(height)}
                  </DropdownMenuItem>
                ))}
                {qualities.length === 0 ? (
                  <DropdownMenuItem disabled>源未提供多档画质</DropdownMenuItem>
                ) : null}
              </DropdownMenuContent>
            </DropdownMenu>

            <ControlButton label="倍速">
              <PlaybackRateButton className="flex h-8 items-center justify-center rounded-sm px-2 text-xs text-danmaku hover:bg-accent/40" />
            </ControlButton>
            <ControlButton label="画中画">
              <PiPButton className="flex size-8 items-center justify-center rounded-sm text-danmaku hover:bg-accent/40" />
            </ControlButton>
            <ControlButton label="全屏">
              <FullscreenButton className="flex size-8 items-center justify-center rounded-sm text-danmaku hover:bg-accent/40" />
            </ControlButton>
          </ControlsParts.Group>
        </div>
      </ControlsParts.Content>
    </ControlsParts.Root>
  );
}

function ControlButton({ label, children }: { label: string; children: ReactNode }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="inline-flex">{children}</span>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}
