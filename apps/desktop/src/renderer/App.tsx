import { Component, useState, type ErrorInfo, type ReactNode } from 'react';
import { Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom';
import {
  AlertTriangle,
  ChevronLeft,
  ChevronRight,
  Compass,
  Download,
  Film,
  Library,
  RefreshCw,
  Search,
  Settings,
  Star,
  Video,
} from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Separator } from '@/components/ui/separator';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip';
import { cn } from '@/lib/utils';
import { useKernelStatus } from '@/lib/hooks';
import { useSession } from '@/lib/session';
import { DiscoverPage } from '@/pages/DiscoverPage';
import { DetailPage } from '@/pages/DetailPage';
import { PlayerPage } from '@/pages/PlayerPage';

/**
 * 应用外壳：左侧栏 214px + 顶栏 48px + 内容区（DESIGN-SYSTEM.md §5 / §3.3 的布局常量）。
 *
 * 侧栏只放**真的能用**的入口。下载 / 追剧 / 设置三页不在 M4c 范围内，因此它们渲染为
 * 禁用态并注明「未接入」，而不是画一个点了没反应的按钮 —— 空壳导航比缺导航更误导。
 */
const NAV = [
  { to: '/', label: '发现', icon: Compass, enabled: true },
  { to: '/search', label: '搜索', icon: Search, enabled: false },
  { to: '/library', label: '剧库', icon: Library, enabled: false },
  { to: '/downloads', label: '下载', icon: Download, enabled: false },
  { to: '/following', label: '追剧', icon: Star, enabled: false },
  { to: '/settings', label: '设置', icon: Settings, enabled: false },
] as const;

export function App() {
  return (
    <TooltipProvider delayDuration={300}>
      <AppShell>
        <AppRoutes />
      </AppShell>
    </TooltipProvider>
  );
}

function AppRoutes() {
  return (
    <Routes>
      <Route path="/" element={<DiscoverPage />} />
      <Route path="/detail" element={<DetailPage />} />
      <Route path="/play" element={<PlayerPage />} />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}

function AppShell({ children }: { children: ReactNode }) {
  const navigate = useNavigate();
  const location = useLocation();
  const { query, setQuery } = useSession();
  const kernel = useKernelStatus();

  return (
    <div className="flex h-screen w-screen overflow-hidden bg-background text-foreground">
      <aside className="flex w-[214px] shrink-0 flex-col border-r border-sidebar-border bg-sidebar">
        {/* macOS 红绿灯预留：拖拽区高度按平台留位，Windows 上就是普通留白。 */}
        <div className="h-[58px] w-full shrink-0 [-webkit-app-region:drag]" />
        <div className="flex items-center gap-2 px-4 pb-4">
          <span className="flex size-6 items-center justify-center rounded-sm bg-primary text-primary-foreground">
            <Film className="size-3.5" />
          </span>
          <span className="text-md font-semibold tracking-tight">红果</span>
        </div>

        <nav className="flex flex-col gap-0.5 px-2">
          {NAV.map((item) => {
            const active = item.enabled && item.to === location.pathname;
            const Icon = item.icon;
            if (!item.enabled) {
              return (
                <Tooltip key={item.to}>
                  <TooltipTrigger asChild>
                    <span
                      aria-disabled
                      className="flex h-8 cursor-not-allowed items-center gap-2 rounded-sm px-2 text-sm text-foreground-subtle"
                    >
                      <Icon className="size-4" />
                      {item.label}
                    </span>
                  </TooltipTrigger>
                  <TooltipContent side="right">M4c 未接入</TooltipContent>
                </Tooltip>
              );
            }
            return (
              <button
                key={item.to}
                type="button"
                onClick={() => navigate(item.to)}
                aria-current={active ? 'page' : undefined}
                className={cn(
                  'flex h-8 items-center gap-2 rounded-sm px-2 text-sm transition-colors',
                  active
                    ? 'bg-sidebar-primary text-sidebar-primary-foreground'
                    : 'text-sidebar-foreground hover:bg-accent hover:text-foreground',
                )}
              >
                <Icon className="size-4" />
                {item.label}
              </button>
            );
          })}
        </nav>

        <Separator className="my-3" />
        <div className="px-4">
          <KernelBadge />
        </div>
        <div className="mt-auto px-4 py-4 text-2xs text-foreground-subtle">
          桥接 {kernel.coreVersion ?? '未知版本'}
        </div>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-[48px] shrink-0 items-center gap-2 border-b border-border-subtle bg-background/80 px-3 backdrop-blur-md [-webkit-app-region:drag]">
          <div className="flex items-center gap-1 [-webkit-app-region:no-drag]">
            <Button variant="ghost" size="icon-sm" onClick={() => navigate(-1)} aria-label="后退">
              <ChevronLeft />
            </Button>
            <Button variant="ghost" size="icon-sm" onClick={() => navigate(1)} aria-label="前进">
              <ChevronRight />
            </Button>
          </div>
          <div className="relative ml-2 w-[320px] [-webkit-app-region:no-drag]">
            <Search className="absolute top-1/2 left-2 size-3.5 -translate-y-1/2 text-foreground-subtle" />
            <Input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder="筛选已加载的短剧（剧名 / 题材）"
              className="h-8 pl-7 text-sm"
            />
          </div>
          <div className="ml-auto flex items-center gap-1 [-webkit-app-region:no-drag]">
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => navigate('/')}
              aria-label="回到发现"
            >
              <Video />
            </Button>
          </div>
        </header>

        <main className="min-h-0 flex-1 overflow-y-auto">
          <AppErrorBoundary>
            <KernelNotice />
            {children}
          </AppErrorBoundary>
        </main>
      </div>
    </div>
  );
}

/** 侧栏底部的内核状态：state 用中性词描述，不把「没装内核」说成「崩溃」。 */
function KernelBadge() {
  const kernel = useKernelStatus();
  const tone =
    kernel.state === 'ready'
      ? 'bg-success'
      : kernel.state === 'starting' || kernel.state === 'restarting'
        ? 'bg-warning'
        : 'bg-destructive';
  const label =
    kernel.state === 'ready'
      ? '内核就绪'
      : kernel.state === 'starting'
        ? '内核启动中'
        : kernel.state === 'restarting'
          ? '内核重启中'
          : kernel.binaryAvailable
            ? '内核未运行'
            : '内核未安装';
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center gap-2">
        <span className={cn('size-1.5 rounded-full', tone)} />
        <span className="text-xs text-sidebar-foreground">{label}</span>
      </div>
      {kernel.inflight > 0 && (
        <span className="text-2xs text-foreground-subtle">在途请求 {kernel.inflight}</span>
      )}
    </div>
  );
}

/**
 * 内核不可用时的全局提示条。
 *
 * 关键区分：**二进制缺失**是「这台机器没编译内核」，属于可解释的部署状态；
 * 只有 `failed` 且二进制存在才是真的异常。两种情况都只提示、不阻塞浏览（页面各自有空态）。
 */
function KernelNotice() {
  const kernel = useKernelStatus();
  const [dismissed, setDismissed] = useState(false);
  if (kernel.state === 'ready' || dismissed) return null;
  const missingBinary = !kernel.binaryAvailable;

  return (
    <div className="flex items-center gap-3 border-b border-border-subtle bg-secondary px-5 py-2">
      <AlertTriangle className={cn('size-3.5', missingBinary ? 'text-warning' : 'text-destructive')} />
      <div className="min-w-0 flex-1 text-xs text-foreground-muted">
        {missingBinary ? (
          <>
            未找到内核程序 <code className="text-foreground">{kernel.binaryPath || 'hncore-server'}</code>
            ，当前为空态模式。请先执行 <code className="text-foreground">pnpm core:build</code> 编译内核。
          </>
        ) : (
          <>{kernel.lastError ?? '内核未就绪'}</>
        )}
      </div>
      <Button variant="ghost" size="xs" onClick={() => setDismissed(true)}>
        知道了
      </Button>
    </div>
  );
}

interface BoundaryState {
  error: Error | null;
}

/** 渲染层崩溃不应留下黑窗口：捕获后给出可读原因与「重试」入口。 */
class AppErrorBoundary extends Component<{ children: ReactNode }, BoundaryState> {
  override state: BoundaryState = { error: null };

  static getDerivedStateFromError(error: Error): BoundaryState {
    return { error };
  }

  override componentDidCatch(error: Error, info: ErrorInfo): void {
    console.error('[renderer] 组件树抛出异常', error, info.componentStack);
  }

  override render(): ReactNode {
    const { error } = this.state;
    if (!error) return this.props.children;
    return (
      <div className="flex h-full flex-col items-start gap-3 p-10">
        <Badge variant="destructive">渲染失败</Badge>
        <h1 className="text-xl font-semibold">这个页面没能画出来</h1>
        <p className="max-w-[60ch] text-sm text-foreground-muted">{error.message}</p>
        <Button variant="outline" size="sm" onClick={() => this.setState({ error: null })}>
          <RefreshCw />
          重试
        </Button>
      </div>
    );
  }
}
