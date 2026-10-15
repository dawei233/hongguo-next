import type { ReactNode } from 'react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '@/lib/utils';

/**
 * 统一的状态块：空态 / 离线 / 错误。
 *
 * 为什么不用一个 `Empty` 组件包打天下：内核不可用（`offline`）与「查到了但结果是 0 条」
 * 是两种完全不同的用户处境，前者要教用户去编译内核，后者只需要换个词。把差异塞进
 * 一个组件的 variant 里，调用方很快就不再读文档了 —— 这里只统一**视觉**，文案与动作
 * 由调用方给出。
 */
export interface StateMessageProps {
  icon: LucideIcon;
  title: string;
  description?: ReactNode;
  actions?: ReactNode;
  /** 次要说明（诊断路径、内核错误码一类），走等宽小字。 */
  detail?: ReactNode;
  className?: string;
}

export function StateMessage({
  icon: Icon,
  title,
  description,
  actions,
  detail,
  className,
}: StateMessageProps) {
  return (
    <div
      className={cn(
        'flex flex-col items-center justify-center gap-3 px-6 py-12 text-center',
        className,
      )}
    >
      <span className="flex size-11 items-center justify-center rounded-full bg-secondary text-foreground-subtle">
        <Icon className="size-5" />
      </span>
      <h2 className="text-md font-semibold">{title}</h2>
      {description ? (
        <p className="max-w-[56ch] text-sm text-foreground-muted">{description}</p>
      ) : null}
      {detail ? (
        <p className="max-w-[64ch] font-numeric text-2xs break-all text-foreground-subtle">
          {detail}
        </p>
      ) : null}
      {actions ? <div className="mt-1 flex items-center gap-2">{actions}</div> : null}
    </div>
  );
}
