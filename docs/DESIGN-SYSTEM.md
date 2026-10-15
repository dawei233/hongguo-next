# hongguo-next 设计系统

> 状态：草案 v1.1 · 日期：2026-10-15
> v1.1 修订（对齐已重写的 `ARCHITECTURE.md`）：① 全仓统一 GPL-3.0，删除对已废除的 `license-guard` 指纹门禁与 `legacy.lock.json` 的引用（§0 / §1.4 / §7.2 / §7.3）；② animeko 措辞改为"仅借鉴思路、不搬代码（AGPL-3.0 与本仓 GPL-3.0 相容）"；③ 「安卓端 = Flutter」由待决事项升级为**已定架构**（§8.1），据此补齐 §6 的 Flutter 令牌镜像：修正 `HnSpacing` 圆角值、新增 `HnTypo`/`HnMotion`、补 reduced motion 与 z 序说明，并把 `check:tokens` 守卫扩展到非颜色令牌。
> 适用仓库：`github.com/dawei233/hongguo-next`
> 上游文档：`docs/ARCHITECTURE.md`（M0–M4 落地顺序，本文定义 M4 播放器与双端 UI 的视觉契约）
> 诊断基线：`hongguo-drama/src/index.css` + `src/components/*.css`（2284 行）、`guoapp-fork/lib/app_theme.dart`（108 行）、`hongguo-monorepo/nas-backend/static/index.html`（3389 行单文件）

---

## 0. 一句话结论

**PC 端 = Electron + React 19 + Tailwind v4（CSS-first `@theme inline`）+ shadcn/ui `new-york` 风格 + video.js 10 的 React 播放器（`@videojs/react`，Tailwind/官方 registry 皮肤）；安卓端 = Flutter + `ThemeExtension<HongguoTokens>` 逐项镜像同一张令牌表；暗色优先。** 三条关键约束：

1. **零新写 CSS 文件**。所有视觉差异落在 `theme.css` 一张令牌表 + shadcn 组件的 `cva` 变体扩展上；CSS 只允许出现在播放器皮肤覆写与 `@keyframes` 两处。
2. **不引入第二个设计系统**。Mantine / Chakra / MUI 都不选（理由见 §1.2）。
3. **许可现状**：本仓与三个老仓已统一为 **GPL-3.0 并整体开源**（`ARCHITECTURE.md` §0 第 2 条 / §3.4）。老仓的 CSS/TSX/Dart 早已是**可直接搬运**的，本文**不引用任何许可隔离机制** —— `license-guard` 指纹门禁与 `legacy.lock.json` 均已删除，构建期外部 checkout、"零代码复制"之类约束不再存在。因此本文对老仓 UI 的"只读"是**产品决策**（本仓要自己的双端 UI，见 §7.2 第 6 条），不是许可约束。第三方依赖许可不变：shadcn/ui = MIT、video.js = Apache-2.0、hls.js = BSD-3-Clause。唯一仍然只读的是 animeko（AGPL-3.0，仅借鉴思路、不搬代码）。

---

## 1. 选型结论

### 1.1 为什么是 shadcn/ui + Tailwind v4

以下均来自**实读源码**，不是印象：

| 证据 | 出处（真实文件） | 对本项目的意义 |
|---|---|---|
| `@theme inline` 是官方推荐路径，且 `@theme inline { --color-*: var(--*) }` 是 1:1 别名写法 | `shadcn-ui/apps/v4/app/globals.css` 第 44–97 行 | 我们的 `:root`/`.dark` 令牌与 Tailwind 工具类可以零损耗对接，**不需要 `tailwind.config.js`** |
| 圆角从单一 `--radius` 派生 6 档 | 同上 49–55 行 + `content/docs/(root)/theming.mdx` | 一处改 `--radius`，控件/卡片/弹层/对话框全部联动 |
| 每个原语都带 `data-slot` 属性 | `registry/new-york-v4/ui/*.tsx`（如 `button.tsx` 导出 `data-slot="button"`） | 我们可以在**不 fork 组件源码**的前提下用 `@apply`/选择器做红果化改造，`shadcn add` 后重新生成也不会丢 |
| 颜色已全量迁到 OKLCH | `theming.mdx`、`globals.css` | 暗色模式下调同一色相的明度时，`oklch(L C H)` 只需改 `L`，不会出现 hex 手调偏色（这正是现役 `#c8383d` / `#b83640` 两个红不一致的根因） |
| 依赖版本可钉 | `apps/v4/package.json`：`tailwindcss ^4.3.0`、`tw-animate-css ^1.4.0`、`radix-ui ^1.4.3`、`cva ^0.7.1`、`react 19.2.3` | 与 `ARCHITECTURE.md` 的 Electron+React+Vite 栈直接兼容，无需升 React 18 |

**Tailwind v4 相对 v3 的关键变化（影响本项目）**：

- 没有 `tailwind.config.js`，配置全部在 CSS 里：入口第一行 `@import "tailwindcss";`。
- 没有 `darkMode: 'class'` 字段，改为 `@custom-variant dark (&:is(.dark *));`——这就是**class 策略的落地位置**。
- `@apply` 在 `@layer` 内仍可用，但推荐用扁平 `@utility`（`globals.css` 里的 `no-scrollbar` / `theme-container` 就是范例）。

### 1.2 为什么不选其他方案

| 被否方案 | 否决理由 |
|---|---|
| **Mantine** | 组件完整但**样式是 JS 对象 + PostCSS preset**，令牌改动要动 `mantine.config.ts` 并重新编译 CSS；与 Tailwind v4 的 CSS-first 方向相反。且其默认 `Button`/`Paper` 的圆角/阴影体系与我们要"macOS 影音资料库 + 珊瑚红只用于选择"的方向冲突，改造成本高于 shadcn。 |
| **Chakra UI v3** | v3 已转 CSS-in-JS（`@emotion`）+ token 语义层（`recipes`）。语义层的抽象在多端镜像时是负担：Flutter 侧无法直接映射 `recipes`，只能映射平铺令牌。 |
| **MUI** | Material Design 语言本身是"移动优先卡片流"，与 PC 端高密度媒体库（jellyfin 式 150px 海报网格 + 214px 侧栏）气质相反；且 `sx` 运行时开销在 Electron 渲染进程没必要。 |
| **纯手写 CSS（延续现役做法）** | 现役诊断已证实失败：`index.css` 178 行内定义了 **90+ 个变量、其中 12 个是同一含义的别名**（`--bg`/`--bg-canvas`/`--bg-primary`、`--text`/`--text-primary`、`--accent`/`--accent` …），暗色版再抄一遍；`Player.css` 单文件 716 行、组件类名 `.ep-chip` / `.browse-cover` 等散落 12 个 CSS 文件，改一处圆角要全局搜索。旧 Web 单文件 3389 行里 `style=` 内联与 `<style>` 混用，是同一个病的晚期形态。**根因不是"手写 CSS 不好"，而是"没有单一令牌源 + 没有受控的组件原语"。** |
| **video-dev/hls.js 单独直接用** | hls.js 是**传输层**库，不提供 UI。用它自己写控制条 = 重造 video.js 已有的进度条缓冲/缩略图预取/字幕轨/全量快捷键。正确姿势是把它作为 video.js 的一个 adapter 引入（见 §1.3）。 |

### 1.3 播放器：video.js 10 的真实形态（重要更正）

读 `videojs/video.js` 仓库 `main`（最新提交 `4233ed1`/`9401`，2026-10）后的实情：

- **`video.js` 这个 npm 包在 v10 已经是一个"墓碑包"**。`packages/video.js/package.json` 的描述是：
  > "Points Video.js installs to `@videojs/html` and `@videojs/react`, and throws coded errors for Video.js 8 patterns"
  
  `packages/video.js/src/videojs.ts` 里 `videojs()`、`registerPlugin`、`registerComponent` 全部 `throwLegacyError('VJS8_LEGACY_*')`。**照着 v8 教程写 `videojs('my-video')` 会直接抛错。**

- **真正的播放器在 `@videojs/react`（React 19）/ `@videojs/html`（Vue/Svelte/原生）**，API 是**组合式**而非 v8 的命令式：
  - `createPlayer({ features: [...] })` → 返回 `{ Player, usePlayer, useMedia }`（`packages/react/src/player/create-player.tsx`）
  - UI 是零件：`<Controls.Root> <Controls.Backdrop> <Controls.Content> <Controls.Group> <PlayButton/> <VolumePopover/> <TimeSlider/> <VideoSettingsMenu/> </Controls.Root>`
  - 皮肤是一棵组件树：`packages/skins/src/skins/default/video/skin.tsx`

- **HLS：v10 自带引擎，hls.js 是可选 adapter。**
  - 内置：`@videojs/spf`（`packages/spf/README.md`：HLS + ABR + WebVTT + MSE），React 包**硬依赖**它。
  - hls.js：`@videojs/hlsjs-video` adapter，`packages/adapters/hlsjs-video/package.json` 里 `"hls.js": "1.6.19"` 是直接依赖；`@videojs/react` 里它是 **optional peerDependency**。
  - Safari 原生 HLS：`@videojs/native-hls-video`（`<NativeHlsVideo/>`）。
  - **对本项目的结论**：Electron Chromium 走 `<HlsJsVideo/>`（MSE + hls.js），Safari/macOS 走 `<NativeHlsVideo/>`，`resolveAdapterType()` 已封装这个判断。**不再自己 import hls.js 建播放器实例。**

- **v10 的皮肤本身就是 Tailwind + shadcn 体系**（这是选它的决定性理由）：
  - `packages/skins/package.json` 的 devDeps 里有 `tailwindcss`、`@videojs/cli`、`shadcn`、`shadcn@^4.21.4`
  - 皮肤源码全是 Tailwind 类：`src/styles/layout/controls.styles.ts` 里写 `'media-lg:bg-media-popover media-lg:surface-media'`
  - 令牌与 Tailwind 的桥接在 `src/styles/tailwind.css`：`@theme inline { --color-media-primary: var(--media-primary); --radius-media-control: var(--media-control-radius); ... }` ——**和我们 §2 要做的桥接是同一套手法**
  - 官方提供 **shadcn registry**：`packages/skins/build/registry/targets.ts` 定义 `registryPaths = { install: '@components/videojs', import: '@/components/videojs' }`，并且 build 脚本里有 `build:shadcn` + `create:skin`
  - **结论：播放器 UI 与应用 UI 用同一套 Tailwind/shadcn 令牌，不存在"播放器是外来物"的割裂。**

- 备选 **muxinc/elements**：README 显示 `mux-player` 状态为 "Public Beta"（`mux-video` 亦是），且产品定位围绕 Mux 自己的托管视频（playback-id/数据遥测）。我们是**自托管多源拉流**，不需要 Mux 云端 ID；成熟度与生态也不如 video.js。**留作降级备选，不作首选。**

- **弹幕与选集不进 skin 内部**：skin 的 `VideoSettingsMenu`（`skins/shared/video/menus/settings-menu.tsx`）已经组合了 `QualityMenu / AudioTrackMenu / PlaybackRateSubmenu / CaptionsSubmenu`。我们**复制这个 skin 文件到自己的 registry 目录**（`npx shadcn add` 生成的本地文件），在其中追加 `DanmakuMenu` 与 `EpisodeMenu`，而不是 hack 上游。

### 1.4 视觉参考的取与舍

| 参考 | 取什么 | 不取什么 | 许可处理 |
|---|---|---|---|
| `hongguo-drama/DESIGN.md` | 整个方向：macOS 影音资料库、浅暖灰侧栏、珊瑚红只用于"选择与主要动作"、3:4 海报直接呈现真实内容、"边框或投影，不同时用" | 不复制任何 CSS/JS 行 | GPL-3.0（与本仓同许可）→ **不复制行**，属产品决策而非许可约束 |
| `jellyfin-web` `src/styles/librarybrowser.scss` | 头图与内容区的比例混排（`header-poster-padding` 的 37.5%→11.1% 递进）、海报网格密度 | 不抄 SCSS | GPL-2.0 → **只读** |
| `jellyfin-web` `src/styles/videoosd.scss` | OSD 结构：`padding-top: 7.5em` 的上/下渐变遮罩、`.videoOsdBottom` `pointer-events:none` + 内部子元素 `pointer-events:all` | 不抄数值 | GPL-2.0 → **只读** |
| `animeko` `app/shared/app-platform/.../DefaultSeedColor.kt`、`ThemeSettings.kt` | 单一 seed 色 + `AUTO/LIGHT/DARK` 三态 + "追番页强制暗色"开关的做法 | 不抄其 Compose 代码 | AGPL-3.0 → **只读**（仅借鉴思路，不搬代码；AGPL-3.0 与本仓 GPL-3.0 相容） |

---

## 2. Tailwind v4 令牌落地方式

### 2.1 文件布局

```
apps/desktop/src/renderer/
├── styles/
│   ├── theme.css          # ★ 唯一令牌源：@theme inline + :root + .dark
│   ├── globals.css        # 入口：@import "tailwindcss" + 三行 @import
│   └── player.css         # 唯一允许写选择器的文件：--media-* 覆写 + 弹幕层
├── components/ui/         # shadcn 组件（npx shadcn add 生成，我们在其上扩展）
├── components/hongguo/    # 业务组件：PosterCard / EpisodeGrid / DanmakuLayer / ...
└── theme-provider.tsx     # class 策略的 .dark 挂载点
```

`globals.css` 全文（照抄 shadcn v4 的形状，只减不增）：

```css
@import "tailwindcss";
@import "tw-animate-css";
@import "shadcn/tailwind.css";
@import "./theme.css";
@import "./player.css";

@source "./**/*.{ts,tsx}";

@custom-variant dark (&:is(.dark *));
```

> `@source` 是 v4 取代 v3 `content` 的写法。本仓不依赖内容扫描（Electron 打包路径固定），仍显式声明以防 tailwind 漏扫 `components/ui` 里的类名。

### 2.2 与现役 CSS 变量的对应关系

现役 `hongguo-drama/src/index.css` 有大量同义别名。**本次不做别名兼容**（新仓无历史包袱），但下表给出"老变量 → 新令牌"的迁移对照，供实现时逐条 grep：

| 老变量（现役） | 老值 | 新令牌 | 说明 |
|---|---|---|---|
| `--bg` / `--bg-canvas` / `--bg-primary` | `#faf9f7` | `--background` | 三个别名合一 |
| `--card-bg` / `--bg-subtle` / `--bg-elevated` | `#fff` | `--card` | 三合一 |
| `--bg-inset` | `#f0eeeb` | `--muted` | —— |
| `--sidebar-bg` | `#f0eeeb` | `--sidebar` | —— |
| `--bg-hover` | `#f1efec` | `--accent` | —— |
| `--text` / `--text-primary` | `#252523` | `--foreground` | 二合一 |
| `--text-secondary` / `--text-sub` | `#686661` | `--muted-foreground` | 二合一 |
| `--text-tertiary` | `#77736c` | `--foreground-subtle`（**新令牌**） | 现役 muted 只剩两级，播放页信息密度不够 |
| `--border` / `--border-default` / `--border-color` | `#e4e1dc` | `--border` | 三合一 |
| `--border-muted` | `#efede9` | `--border-subtle`（**新令牌**） | 分隔线需要比卡片边框更弱 |
| `--border-strong` | `#c8c3bd` | `--input` | 输入框边框 |
| `--primary` / `--accent` | `#c8383d` | `--primary` | 品牌红 |
| `--accent-soft` / `--sidebar-active` | `#fae9e7` / `#e8ded9` | `--primary-subtle` | 选中底 |
| `--accent-text` | `#a5292e` | `--primary-foreground-soft`（**新令牌**） | 底色上的文字色 |
| `--focus-ring` / `--focus-soft` | `#c8383d80` | `--ring` | —— |
| `--scrim` | `#29231b40` | `--scrim` | —— |
| `--skeleton` | `#eeebe6` | `--skeleton` | —— |
| `--radius-sm/radius/radius-lg` | `6/9/14px` | `--radius` 单一基准派生 | 见 §3.4 |
| `--shadow-sm/md/lg` | 三档 | `--shadow-*` + 新增 `--shadow-glow` | —— |

**暗色分支同理**：`#222220 / #2b2b28 / #2f2f2b` 三层合并为 `--background / --card / --popover`。

### 2.3 品牌色怎么定（红果系红 + 暗色优先）

约束三条：

1. 与**红果官方 seed `#FF664F`**（现役 `app_theme.dart` 的 `ColorScheme.fromSeed`）同族，不能跑到玫红/酒红。
2. **暗色优先**：本项目核心场景是"看片"，播放页、追番页默认暗色；品牌红在暗底上需要**提亮**，否则 `#c8383d` 在 `#111213` 上对比度不足 3:1。
3. 浅色下品牌红要做"按钮填充色"，需要压暗到可承载白字（对比度 ≥ 4.5:1）。

结论——**同一色相、两套明度**，用 OKLCH 的 `L` 单独控制，`C/H` 几乎不变：

| | OKLCH | ≈ HEX | 用途 |
|---|---|---|---|
| 浅色 `primary` | `oklch(0.558 0.181 27)` | `#c83933` | 浅底上的填充按钮、选中态 |
| 浅色 `primary-hover` | `oklch(0.498 0.170 27)` | `#af2a27` | —— |
| 深色 `primary` | `oklch(0.700 0.178 28)` | `#f96a5d` | 暗底上的强调（进度条、弹幕开关） |
| 深色 `primary-hover` | `oklch(0.756 0.162 30)` | `#ff8370` | —— |
| 深色 `on-primary` | `oklch(0.165 0.035 27)` | `#1b0806` | **深色下品牌红变亮，配色必须翻成深色字** |

> 这是与现役最大的行为差异：现役暗色把 `--accent` 从 `#c8383d` 换成 `#ed9295`（色相从 23° 漂到 18°），导致"同一个红"在两个主题里不是同一个颜色。本方案锁定 `H≈27°`，只动 `L`。

### 2.4 暗色模式的 class 策略

完全照 shadcn 官方 Vite 方案（`content/docs/dark-mode/vite.mdx`），只把 storage key 换成 `hn-theme`：

```tsx
// theme-provider.tsx
type Theme = "dark" | "light" | "system";   // 与 animeko 的 AUTO/LIGHT/DARK 同构

export function ThemeProvider({ children, defaultTheme = "dark", storageKey = "hn-theme" }) {
  const [theme, setTheme] = useState<Theme>(
    () => (localStorage.getItem(storageKey) as Theme) || defaultTheme
  );
  useEffect(() => {
    const root = window.document.documentElement;
    root.classList.remove("light", "dark");
    const resolved =
      theme === "system"
        ? window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light"
        : theme;
    root.classList.add(resolved);
    root.style.colorScheme = resolved;      // 让原生滚动条/表单控件跟随
  }, [theme]);
  ...
}
```

- **默认 `dark`**（暗色优先），`system` 可选。播放器页额外加 `data-always-dark` 到根，覆盖全局偏好——对应 animeko 的 `alwaysDarkInEpisodePage`。
- 与 `guoapp-fork` 的差异：现役用 `localStorage["hongguo-experience-preview:theme"]` + `document.documentElement.dataset.theme`。**改用 class**，因为 Tailwind v4 的 `@custom-variant dark (&:is(.dark *))` 只认 class；`data-theme` 需要额外写 `@custom-variant dark (&:is([data-theme=dark] *))`，收益为零。

---

## 3. 完整令牌表

> 全部写在 `theme.css`。OKLCH 为权威值，HEX 为 Flutter 侧与文档对照用（Flutter 侧 `Color(int)` 不支持 OKLCH 字面量，用 HEX；两者由本文件单一来源，改一处必须同步另一处，并由 §7 的 `pnpm check:tokens` 校验）。

### 3.1 颜色

| CSS 变量 | `@theme inline` 别名 | 浅色 | 深色 | 用途 |
|---|---|---|---|---|
| `--background` | `--color-background` | `oklch(0.985 0.003 85)` `#fbfaf8` | `oklch(0.180 0.004 265)` `#111213` | 应用画布 |
| `--card` | `--color-card` | `oklch(1 0 0)` `#ffffff` | `oklch(0.232 0.005 265)` `#1c1d20` | 卡片/面板 |
| `--popover` | `--color-popover` | `oklch(1 0 0)` `#ffffff` | `oklch(0.232 0.005 265)` `#1c1d20` | 浮层 |
| `--surface` | `--color-surface` | `oklch(0.950 0.005 78)` `#f0eeeb` | `oklch(0.272 0.005 265)` `#262729` | 次级面（侧栏、inset） |
| `--surface-hover` | `--color-surface-hover` | `oklch(0.938 0.006 80)` `#eae7e2` | `oklch(0.320 0.006 265)` `#33353a` | 行 hover |
| `--foreground` | `--color-foreground` | `oklch(0.264 0.004 107)` `#252523` | `oklch(0.946 0.007 89)` `#efede8` | 主文字 |
| `--foreground-muted` | `--color-foreground-muted` | `oklch(0.510 0.008 89)` `#686661` | `oklch(0.724 0.013 80)` `#aaa59d` | 次要文字 |
| `--foreground-subtle` | `--color-foreground-subtle` | `oklch(0.557 0.012 82)` `#77736c` | `oklch(0.810 0.012 80)` `#c5c0b8` | 三级文字/占位 |
| `--primary` | `--color-primary` | `oklch(0.558 0.181 27)` `#c83933` | `oklch(0.700 0.178 28)` `#f96a5d` | 品牌红/主要动作 |
| `--primary-hover` | `--color-primary-hover` | `oklch(0.498 0.170 27)` `#af2a27` | `oklch(0.756 0.162 30)` `#ff8370` | —— |
| `--primary-foreground` | `--color-primary-foreground` | `oklch(0.985 0.005 27)` `#fdf9f8` | `oklch(0.165 0.035 27)` `#1b0806` | 品牌底上的字 |
| `--primary-subtle` | `--color-primary-subtle` | `oklch(0.947 0.019 26)` `#fae9e7` | `oklch(0.330 0.062 27)` `#512824` | 选中底/弱强调面 |
| `--primary-foreground-soft` | — | `oklch(0.470 0.170 27)` `#a12b27` | `oklch(0.800 0.110 25)` `#f5a49c` | 弱强调面上的字 |
| `--secondary` | `--color-secondary` | `oklch(0.950 0.005 78)` `#f0eeeb` | `oklch(0.272 0.005 265)` `#262729` | 次级填充按钮 |
| `--secondary-foreground` | `--color-secondary-foreground` | `oklch(0.264 0.004 107)` `#252523` | `oklch(0.946 0.007 89)` `#efede8` | —— |
| `--muted` | `--color-muted` | `oklch(0.950 0.005 78)` `#f0eeeb` | `oklch(0.272 0.005 265)` `#262729` | 弱面 |
| `--muted-foreground` | `--color-muted-foreground` | `oklch(0.510 0.008 89)` `#686661` | `oklch(0.724 0.013 80)` `#aaa59d` | —— |
| `--accent` | `--color-accent` | `oklch(0.938 0.006 80)` `#eae7e2` | `oklch(0.320 0.006 265)` `#33353a` | hover/焦点面 |
| `--accent-foreground` | `--color-accent-foreground` | `oklch(0.264 0.004 107)` `#252523` | `oklch(0.946 0.007 89)` `#efede8` | —— |
| `--destructive` | `--color-destructive` | `oklch(0.530 0.171 24)` `#ba3437` | `oklch(0.785 0.096 19)` `#f0a0a1` | 危险/错误 |
| `--destructive-foreground` | `--color-destructive-foreground` | `oklch(0.985 0.005 27)` `#fdf9f8` | `oklch(0.165 0.035 27)` `#1b0806` | —— |
| `--warning` | `--color-warning` | `oklch(0.558 0.107 72)` `#9a681e` | `oklch(0.807 0.095 79)` `#e1b978` | 警告 |
| `--warning-foreground` | `--color-warning-foreground` | `oklch(0.985 0.010 79)` | `oklch(0.240 0.060 79)` | —— |
| `--success` | `--color-success` | `oklch(0.509 0.078 167)` `#32745c` | `oklch(0.787 0.069 162)` `#92c8ac` | 已下载/已完成 |
| `--success-foreground` | `--color-success-foreground` | `oklch(0.985 0.005 167)` | `oklch(0.180 0.040 167)` | —— |
| `--gold` | `--color-gold` | `oklch(0.760 0.130 75)` `#e1a447` | `oklch(0.855 0.123 83)` `#f6c86b` | VIP/会员（承接 guoapp tertiary） |
| `--border` | `--color-border` | `oklch(0.911 0.008 81)` `#e4e1dc` | `oklch(0.384 0.011 85)` `#46433d` | 卡片边框 |
| `--border-subtle` | `--color-border-subtle` | `oklch(0.945 0.006 81)` `#eeebe6` | `oklch(0.310 0.010 85)` `#3a3833` | 分隔线 |
| `--input` | `--color-input` | `oklch(0.850 0.008 81)` `#ccc8c1` | `oklch(0.470 0.012 85)` `#6d675f` | 输入框边框 |
| `--ring` | `--color-ring` | `oklch(0.558 0.181 27)` `#c83933` | `oklch(0.700 0.178 28)` `#f96a5d` | 焦点环 |
| `--sidebar` | `--color-sidebar` | `oklch(0.950 0.005 78)` `#f0eeeb` | `oklch(0.210 0.005 265)` `#17181b` | 侧栏 |
| `--sidebar-foreground` | `--color-sidebar-foreground` | `oklch(0.510 0.008 89)` | `oklch(0.810 0.012 80)` | 侧栏文字 |
| `--sidebar-primary` | `--color-sidebar-primary` | `oklch(0.947 0.019 26)` `#fae9e7` | `oklch(0.330 0.062 27)` `#512824` | 侧栏选中底 |
| `--sidebar-primary-foreground` | — | `oklch(0.470 0.170 27)` `#a12b27` | `oklch(0.800 0.110 25)` `#f5a49c` | 侧栏选中字 |
| `--sidebar-border` | `--color-sidebar-border` | `oklch(0.911 0.008 81)` | `oklch(0.384 0.011 85)` | —— |
| `--skeleton` | `--color-skeleton` | `oklch(0.930 0.006 81)` `#e8e5e0` | `oklch(0.280 0.008 265)` `#2d2f33` | 骨架屏 |
| `--scrim` | `--color-scrim` | `oklch(0.300 0.020 60 / 0.45)` `#362c24` | `oklch(0 0 0 / 0.66)` `#000000` | 模态遮罩 |
| `--stage` | `--color-stage` | `oklch(0.145 0.004 265)` `#090a0c` | `oklch(0.145 0.004 265)` `#090a0c` | **播放台恒定暗色**，双主题同值 |
| `--danmaku` | `--color-danmaku` | `oklch(0.980 0.010 90 / 0.92)` `#fbf8f1` | 同左 | 弹幕文字（播放台上恒定） |
| `--scroll-thumb` | `--color-scroll-thumb` | `oklch(0.780 0.008 81)` `#c6c2bb` | `oklch(0.470 0.012 85)` `#6d675f` | 滚动条 |

### 3.2 排版

字体栈（Windows 优先，Electron 目标平台）+ 数字全部等宽，避免进度条抖动：

```css
--font-sans: "PingFang SC", "HarmonyOS Sans SC", "Microsoft YaHei UI", "Microsoft YaHei",
             -apple-system, BlinkMacSystemFont, "Segoe UI", system-ui, sans-serif;
--font-numeric: "SF Mono", "JetBrains Mono", ui-monospace, Menlo, Consolas, monospace;
```

| token | 字号 | 行高 | 字距 | 字重 | 用途 |
|---|---|---|---|---|---|
| `--text-2xs` | 10px | 1.4 | 0.02em | 600 | 角标、热度数字（等宽） |
| `--text-xs` | 11px | 1.5 | 0.01em | 500 | 集数、时长、标签 |
| `--text-sm` | 12px | 1.5 | 0 | 500 | 辅助说明、侧栏 |
| `--text-base` | 13px | 1.55 | 0 | 400 | **正文基准**（承接现役 13px） |
| `--text-md` | 15px | 1.5 | -0.01em | 600 | 卡片标题、剧名 |
| `--text-lg` | 17px | 1.4 | -0.015em | 600 | 区块标题 |
| `--text-xl` | 20px | 1.35 | -0.02em | 700 | 页面副标题 |
| `--text-2xl` | 26px | 1.25 | -0.025em | 700 | 页面主标题 |
| `--text-3xl` | 32px | 1.2 | -0.03em | 700 | 详情页剧名（Android 端 24px） |
| `--text-display` | 44px | 1.1 | -0.035em | 800 | 播放器标题、启动页 |

> 与 shadcn 默认的 `--text-*`（14/16/18px 档）不同：我们**整体下移一档**，因为现役 `index.css` 的 `body { font-size: 13px }` 已经是这个密度，下移会破坏阅读连续性。

### 3.3 间距

`--spacing: 0.25rem`（Tailwind v4 默认，即 4px 基线），只用整数档：

| 档 | px | 用途 |
|---|---|---|
| `0.5` | 2 | 徽标内边距微调 |
| `1` | 4 | 图标与文字间隙 |
| `1.5` | 6 | chip 间隙 |
| `2` | 8 | 紧凑控件内边距 |
| `2.5` | 10 | 按钮纵向 |
| `3` | 12 | 卡片内边距（密集）、chip 高度 |
| `4` | 16 | 卡片内边距（标准） |
| `5` | 20 | 区块内边距 |
| `6` | 24 | 区块之间 |
| `8` | 32 | 大区块分隔 |
| `10` | 40 | 页面顶部留白 |
| `12` | 48 | 空状态上下留白 |

**布局常量**（不放在 spacing 阶梯里，因为它们是断点相关的）：

| token | 值 | 含义 |
|---|---|---|
| `--sidebar-w` | 214px / 展开； 64px / 图标态 | 侧栏 |
| `--toolbar-h` | 48px | 顶部工具条（macOS 左上预留 78px 给红绿灯） |
| `--content-max` | 1180px | 内容最大宽（承接现役 `.browse-container`） |
| `--settings-max` | 840px | 设置页最大宽 |
| `--poster-min` | 150px | 海报网格最小列宽（jellyfin 密度） |
| `--stage-max` | 1080px | 播放器画面最大宽 |

### 3.4 圆角

照 shadcn 的"单一基准派生"模式，但基准值按 DESIGN.md 定为 **10px**（现役 `--radius: 9px`）：

```css
--radius: 0.625rem;                      /* 10px，基准 */
--radius-xs: calc(var(--radius) * 0.5);  /*  5px 徽标/进度点 */
--radius-sm: calc(var(--radius) * 0.7);  /*  7px 按钮/输入/集数格 */
--radius-md: calc(var(--radius) * 0.9);  /*  9px 卡片内容块 */
--radius-lg: var(--radius);              /* 10px 卡片 */
--radius-xl: calc(var(--radius) * 1.3);  /* 13px 海报 */
--radius-2xl: calc(var(--radius) * 1.6); /* 16px Sheet/弹层 */
--radius-3xl: calc(var(--radius) * 2.0); /* 20px Dialog */
--radius-full: 9999px;                   /* 药丸（播放悬浮按钮、弹幕开关） */
```

### 3.5 阴影

原则（承接 DESIGN.md）：**边框或投影，不同时用**。

| token | 浅色 | 深色 | 用途 |
|---|---|---|---|
| `--shadow-none` | — | — | 默认 |
| `--shadow-xs` | `0 1px 2px oklch(0.16 0.01 60 / 0.06)` | `0 1px 2px oklch(0 0 0 / 0.32)` | 卡片 hover |
| `--shadow-sm` | `0 2px 5px oklch(0.16 0.01 60 / 0.08)` | `0 2px 5px oklch(0 0 0 / 0.40)` | 悬浮卡片、下拉 |
| `--shadow-md` | `0 8px 24px oklch(0.16 0.01 60 / 0.12)` | `0 8px 24px oklch(0 0 0 / 0.52)` | Dialog、Sheet |
| `--shadow-lg` | `0 24px 64px oklch(0.16 0.01 60 / 0.20)` | `0 24px 64px oklch(0 0 0 / 0.62)` | 模态、Toast |
| `--shadow-glow` | — | `0 0 0 3px oklch(0.700 0.178 28 / 0.28)` | 播放页强调控件焦点 |

### 3.6 动效

| token | 值 | 缓动 | 用途 |
|---|---|---|---|
| `--duration-instant` | 50ms | `ease-out` | 按下态 |
| `--duration-fast` | 100ms | `ease-out` | hover、chip |
| `--duration-base` | 150ms | `cubic-bezier(0.2, 0, 0, 1)` | Sheet、Tooltip |
| `--duration-slow` | 250ms | `cubic-bezier(0.2, 0, 0, 1)` | Dialog |
| `--duration-player` | 200ms | `ease-out` | 播放器控制条显隐 |
| `--ease-out-expo` | `cubic-bezier(0.16, 1, 0.3, 1)` | —— | 弹幕入场 |
| `--ease-spring` | `cubic-bezier(0.34, 1.56, 0.64, 1)` | —— | 播放按钮出现 |

**必须全局尊重 reduced motion**（承接现役 `index.css` 的 `@media (prefers-reduced-motion: reduce)`）：

```css
@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after {
    animation-duration: 0.01ms !important;
    animation-iteration-count: 1 !important;
    transition-duration: 0.01ms !important;
    scroll-behavior: auto !important;
  }
}
```

弹幕层额外：reduced-motion 下改为**静态堆叠**（不滚动），由 `danmaku_overlay` 的配置读取同一媒体查询。

### 3.7 z-index

单一来源，禁止魔法数字：

| token | 值 | 占用者 |
|---|---|---|
| `--z-base` | 0 | 常规流 |
| `--z-raised` | 10 | 悬浮按钮、hover 预览 |
| `--z-sticky` | 20 | 粘性表头、播放页标题条 |
| `--z-sidebar` | 30 | 侧栏、折叠抽屉 |
| `--z-overlay` | 40 | 遮罩 |
| `--z-dialog` | 50 | Dialog、Sheet、Dropdown、Tooltip（Radix 默认 50，一致） |
| `--z-toast` | 60 | Sonner |
| `--z-player-ctl` | 30 | 播放器控制条（**在 `--stage` 内部**，不与全局叠加） |
| `--z-danmaku` | 20 | 弹幕层（**在控制条之下、画面之上**） |
| `--z-drag` | 9999 | Electron `-webkit-app-region: drag` 提示层 |

---

## 4. shadcn 组件用法规划

### 4.1 引入清单

```bash
npx shadcn@latest init   # style=new-york, baseColor=neutral, cssVariables=true
npx shadcn@latest add button card dialog alert-dialog sheet tabs tooltip skeleton \
  progress slider select badge empty separator scroll-area sonner input \
  toggle-group dropdown-menu sidebar carousel pagination aspect-ratio switch
```

### 4.2 改造清单（红果化）

shadcn 的价值就是"源码进你的仓库、随便改"。以下改造全部**直接改 `components/ui/*.tsx` 的 `cva` 块**，保留 `data-slot` 属性（`shadcn add` 可重复覆盖，我们靠 git 记录差异）。

| 组件 | 现役问题 | 改造 |
|---|---|---|
| **Button** | 现役 `.btn` 用 12px 字号 + 35px 高，偏小 | `size` 全部 +2：`sm: h-8`(32) / `default: h-9`(36) / `lg: h-11`(44)。新增 `size="hero"`：`h-11 rounded-full px-6 text-base` —— 详情页"开始播放"药丸按钮。`rounded-md` → `rounded-sm`（7px，贴 DESIGN.md） |
| **Button** | — | 新增 `variant="brand-soft"`：`bg-primary-subtle text-primary-foreground-soft hover:bg-primary-subtle/80` —— 用于"已追剧/已下载"这类**次级但需要被看见**的动作 |
| **Card** | 现役海报卡是自定义 div | `Card` 只用于**面板**（设置分组、搜索页统计）。`rounded-xl`(13px) → `rounded-lg`(10px)，`py-6 px-6` → `p-5` |
| **Dialog** | 现役 `.player-confirm` 26px padding | `DialogContent` 保持 `rounded-2xl`（16px），覆写为 `max-w-[500px] p-0` + 自绘头/体/脚三段（承接现役 head/body/foot 结构） |
| **Sheet** | 现役 `.browse-drawer` 是 `max-width:740px` 的居中遮罩层，语义上其实是 Dialog | **选集侧栏改用 `Sheet side="right"`**：`sm:max-w-[380px]` + `p-0`。播放页的"剧集面板"也是 Sheet，见 §5.3 |
| **Tabs** | 现役 `.browse-cats` 手写下划线 | 直接用 `Tabs variant="line"`（源码已有 line 变体，激活态是 `after:h-0.5` 下划线）。覆写：`text-sm font-medium`、激活态 `after:bg-primary` |
| **Tooltip** | 现役无 tooltip | 沿用官方 `delayDuration={0}`；覆写配色为 `bg-foreground text-background rounded-sm px-2 py-1 text-xs` |
| **Skeleton** | 现役 `.poster-skeleton` 自绘 | `Skeleton` 覆写为 `bg-skeleton rounded-sm`；海报骨架给 `style={{ aspectRatio: "3/4" }}` |
| **Progress** | 现役 `.player-wait-bar` 三处重复实现 | 统一 `Progress`，覆写 `h-1.5 bg-primary/20`，`ProgressIndicator` 用 `bg-primary`。**下载进度、缓冲进度、等待进度三个场景共用同一个组件** |
| **Slider** | 现役播放器进度条自制 | **播放器内不用 shadcn Slider**——用 video.js 的 `TimeSlider`（它带 buffer 区与缩略图预览）。shadcn `Slider` 只用于**设置页**（弹幕透明度、音量、字幕字号） |
| **Select** | 现役用原生 `<select>`（Electron 下原生弹窗会遮挡播放器工具条，见 `index.css` 注释） | 全量换 `Select`（Radix portal，无原生弹窗）。**触发器高度 32px、字号 12px** |
| **Badge** | 现役 `.browse-ep-badge` / `.browse-dl-badge` / `.browse-rank-badge` 三套 | `Badge variant="secondary" | "default" | "outline"`，`rounded-sm` 替代 `rounded-md`，新增 `className="tabular-nums"` |
| **Empty** | 现役 `.search-welcome` / `.player-empty` / `.dm-empty` 三套 | 统一 `Empty`（源码有 `EmptyHeader/EmptyMedia/EmptyTitle/EmptyDescription/EmptyAction`）。`EmptyMedia variant="icon"` 用 `size-10 rounded-lg bg-muted` |
| **Sidebar** | 现役 `.sidebar` 214px 自绘，含 macOS 58px 拖拽区 | 用 `Sidebar` 骨架，但 `--sidebar-width` 覆盖为 `214px`，并在 `SidebarHeader` 内保留 24px/58px 的 `-webkit-app-region: drag` 空隙。`SidebarMenuButton` 激活态改 `bg-sidebar-primary text-sidebar-primary-foreground rounded-sm`（现役是 `.sidebar-item.active` 的 `bg` + `color` + `font-weight` 三写） |
| **Sonner** | 现役 `.dm-toast` 自绘 | 用官方 Sonner，`toastOptions.classNames` 覆写为 `rounded-lg bg-card border` |

### 4.3 明确**不**用 shadcn 的部分

- **播放器控制条**：用 `@videojs/react` 的 `Controls.*` 零件 + 本地 skin，不用 shadcn 重建（重建就等于放弃 video.js 的 buffer/缩略图/字幕轨）。
- **弹幕层**：自写（`DanmakuLayer`），需要 Go 内核 `core/danmaku/layout.go` 与两端一致的轨道布局算法（ARCHITECTURE.md §5.2），这是数据同步问题不是样式问题。
- **海报网格**：自写 `PosterCard`，因为要叠加集数徽标、热度、下载态、hover 播放按钮四层。

---

## 5. 三个关键页面高保真设计

### 5.1 发现页 `DiscoverPage`

**布局骨架**（PC ≥ 1440px，浅色/深色共用，仅换令牌）：

```
┌─ Sidebar 214 ─┬─ Toolbar 48 ─────────────────────────────────────────────────────────┐
│ ┌─drag 24/58─┐│ ←  ‹  发现           [ 搜索短剧 / 剧名 / 演员            ] 🔍    ☾  ⚙  │
│ ◉ 红果        │├────────────────────────────────────────────────────────────────────┤
│               ││ ╔══════════════════════════════════════════════════════════════════╗ │
│  发现      ◀  ││ ║  ┌──────────┐   逆光而行                          第 12 位        ║ │
│  搜索         ││ ║  │          │   都市 · 悬疑 · 78 集 · 更新至 32 集               ║ │
│  剧库         ││ ║  │  3:4     │   ─────────────────────────────────────────────    ║ │
│  下载         ││ ║  │  海报    │   婚礼当夜她消失了，他用三年追查她留下的每一条      ║ │
│  追剧         ││ ║  │          │   线索，直到在拆迁工地的旧照片上，看见了自己的     ║ │
│  设置         ││ ║  └──────────┘   脸……                                            ║ │
│               ││ ║   192px                                                      ║ │
│  ─────────    ││ ║                [▶ 开始播放 24集]  [＋ 追剧]  [⇩ 批量下载]  [⋮]   ║ │
│  本地 12 部   ││ ╚══════════════════════════════════════════════════════════════════╝ │
│  已下载 48 集 ││                                                                            │
│               ││  热播剧  架空  甜宠  逆袭  悬疑  都市  古装   ← Tabs variant=line 14px │
│               ││  ──────────────────────────────────────────────────────────────────  │
│               ││                                                                            │
│               ││  ┌────┐ ┌────┐ ┌────┐ ┌────┐ ┌────┐ ┌────┐ ┌────┐ ┌────┐         │
│               ││  │ 3:4│ │ 3:4│ │ 3:4│ │ 3:4│ │ 3:4│ │ 3:4│ │ 3:4│ │ 3:4│  ...    │
│               ││  │    │ │    │ │    │ │    │ │    │ │    │ │    │ │    │         │
│               ││  │ #1 │ │    │ │ ↓  │ │    │ │ ▶  │ │    │ │ ✓  │ │    │         │
│               ││  └────┘ └────┘ └────┘ └────┘ └────┘ └────┘ └────┘ └────┘         │
│               ││  剧名两行   剧名两行  剧名两行 ...                              │
│               ││  1.2亿  │  876万 │  342万 │                                       │
│               ││  ┌────┐ ┌────┐ ...                                                 │
└───────────────┴────────────────────────────────────────────────────────────────────┘
```

**详细规范**

| 元素 | 规范 |
|---|---|
| Hero 卡 | `max-w-[1180px]`，`rounded-lg bg-card border border-border p-6 flex gap-8`（**边框，不用 shadow**）。海报 `w-[192px] aspect-[3/4] rounded-xl`。标题 `--text-3xl/700`。简介 `max-w-[65ch] --text-base text-foreground-muted line-[1.8]`。 |
| Hero 操作区 | `flex gap-2 mt-6`。主按钮 `<Button size="hero" variant="default">`；次操作 `variant="brand-soft"`；批量下载/更多 `<Button size="icon">` + `DropdownMenu`。**主按钮永远第一个、永远唯一实心红**。 |
| 题材 Tabs | 用 `Tabs variant="line"`，`TabsList` 高度 36px 无底色，`TabsTrigger` `--text-sm`，激活 `text-primary after:bg-primary`。**不放在 Hero 里**，独立成行 `mt-8 mb-5`。 |
| 海报网格 | `grid gap-x-5 gap-y-6 grid-cols-[repeat(auto-fill,minmax(150px,1fr))]`。列宽由 `--poster-min` 驱动，宽屏自动 7–8 列。 |
| 海报卡 | `<button>` 整块可点（承接现役 `.browse-card{display:block}`）。`rounded-xl overflow-hidden`；`img aspect-[3/4] object-cover transition-transform duration-base hover:scale-[1.035]`。 |
| 角标层 | 排名（左上，`Badge variant="default"` 圆角 `rounded-sm`）；集数（左下，`bg-black/75 text-white` 半透明，不跟随主题）；下载态（右上，`variant="success"` + `<Download/>`）；hover 播放（居中，`bg-black/45` 遮罩 + 白色药丸 `▶ 立即播放`）。 |
| 标题 | `--text-md/600`，`line-clamp-2`，`mt-3`。热度 `--text-xs text-foreground-muted tabular-nums`，格式 `1.2亿` / `876万`。 |
| 加载态 | `Skeleton`：`aspect-[3/4] rounded-xl bg-skeleton` + 下方两条 `h-2.5 w-3/4` / `h-2 w-1/2`（比例与真实卡片一致，**不做虚构片名**）。 |
| 空态 | `<Empty>`：`EmptyMedia variant="icon"` 用 `<Film/>`；标题"没找到相关短剧"；描述一行；`EmptyAction` 放 `<Button variant="outline">重载</Button>` + `<Button variant="ghost">换个关键词</Button>`。 |
| 断点 | `<960px`：Hero 竖排（海报 158px 置顶，简介 `line-clamp-3`）；网格 `minmax(135px,1fr)`。<760px：进入竖屏布局（见 §6.2），网格固定 3 列。 |

### 5.2 剧集详情 + 选集 `DetailPage`

```
┌──────────────────────────────────────────────────────────────────────────────────────┐
│ ← 发现 / 悬疑                          [＋ 追剧]  [⇩ 批量下载]  [⋮]                  │
├──────────────────────────────────────────────────────────────────────────────────────┤
│ ┌────────┐  逆光而行                                              全 78 集 · 2026   │
│ │        │  都市 · 悬疑 · 78集 · 更新至第32集 · 豆瓣 7.4                        │
│ │  3:4   │  ─────────────────────────────────────────────────────────────────     │
│ │        │  婚礼当夜她消失了，他用三年追查她留下的每一条线索，直到在拆迁工地的   │
│ │ 216px  │  旧照片上，看见了自己的脸。他以为自己是在复仇，其实是在找回被自己   │
│ └────────┘  删掉的那个人。                                     [展开 ▾]           │
│                                                                                      │
│  ──────────────────────────────────────────────────────────────────────────────────  │
│  选集  32/78 已看   [ 正序 ▾ ]  [ 只看未看 ○ ]  [ 全选 ] [ 批量下载 ]               │
│  ┌──────────────────────────────────────────────────────────────────────────────┐    │
│  │  01  │  02  │  03  │  04  │  05  │  06  │  07  │  08  │  ...    │  75~78 │    │
│  │  ⬤   │  ✓   │  ✓   │  ▶   │  ⬤   │  ⬤   │  ⬤   │  ⬤   │         │        │    │
│  └──────────────────────────────────────────────────────────────────────────────┘    │
│                                                                                      │
│  ── Tabs: 简介 │ 选集 78 │ 弹幕 12.4万 │ 相似推荐 ─────────────────────────────────  │
│  简介正文…                                                                             │
└──────────────────────────────────────────────────────────────────────────────────────┘
```

**详细规范**

| 元素 | 规范 |
|---|---|
| 头图 | 不用 Hero 大卡。改为 **320px 高横幅**（`h-[200px] md:h-[320px] rounded-lg overflow-hidden`）+ 底部渐变 `bg-gradient-to-b from-transparent to-background`，剧名压在横幅下沿 `--text-3xl`。借鉴 jellyfin 的"头图 + 内容比例"手法。 |
| 信息区 | `grid md:grid-cols-[216px_1fr] gap-6`；海报 `rounded-xl`；元信息一行 `Badge variant="secondary"` 串（题材/集数/年份/评分），`gap-1.5`。简介 `max-w-[68ch] line-clamp-3`，`展开` 为 `Button variant="link" size="xs"`。 |
| 选集网格 | `grid gap-2 grid-cols-[repeat(auto-fill,minmax(64px,1fr))]`。每格 `<Button size="default" className="h-11 tabular-nums">`。**固定高度 44px**（触达下限）。 |
| 集数状态 | 已看：`variant="brand-soft"` + 右上角 `✓`；当前：`ring-2 ring-primary bg-primary-subtle`；未下载：`variant="outline"`；已下载：右上角 `↓` 图标；下载中：`Progress h-1` 绝对定位在格子底部 + 半透明遮罩；锁定/缺失：`text-foreground-subtle` + `cursor-not-allowed`（**不隐藏**，要告诉用户"这里有但拿不到"）。 |
| 选集工具条 | `flex items-center gap-2 mb-4`：左侧"选集 + 进度"，右侧 `Switch`（只看未看）+ `Select`（正序/倒序）+ `Button variant="outline" size="sm"`（全选/全不选）+ `Button variant="brand-soft" size="sm"`（批量下载）。 |
| Tab 内容 | `Tabs variant="line"`：简介 / 选集 78 / 弹幕 12.4万 / 相似推荐。弹幕 Tab 内嵌 `DanmakuTimeline`（按时间轴聚合的可点击列表，**非播放器内**）。 |
| 相似推荐 | 复用 `PosterCard`，网格 `minmax(132px,1fr)`（比发现页小一档）。 |
| 断点 | `<1024px`：头图 200px，海报 160px，工具条换行两行。<640px（平板竖/手机）：海报与信息上下堆叠，选集网格 `minmax(56px,1fr)`。 |

### 5.3 播放器页 `PlayerPage`

**PC ≥ 1280px 横屏**

```
┌───────────────────────────────────────────────────────────────────────────────────────┐
│ ▚ 头像  逆光而行 · 第 05 集                                    [弹幕 开] [选集 32▾]    │ ← 标题条 56px
├───────────────────────────────────────────────────────────────────────────────────────┤
│ ┌ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ┐    │
│ │  这条弹幕会从右侧飞过来 →  这条也是  →  而这条是置顶的                        │    │
│ │                                                                               │    │
│ │                              ▶                                                │    │ ← 中央大播放键
│ │                         （15s 后淡出）                                        │    │
│ │                                                                               │    │
│ │ ┌─ 缓冲中 ──┐                                                                   │    │
│ └───────────────────────────────────────────────────────────────────────────────┘    │
│  ████████████████████░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░   │ ← TimeSlider(6px)
│  04:12 / 78:30                        [自动 1080P ▾] [倍速 1.0x ▾] [倍速] [画中画]  │
│  [▶] [🔊▁▁▁]   ─────────────────────────────────────────────────    [弹幕] [⛶ 全屏]   │ ← 控制条
└───────────────────────────────────────────────────────────────────────────────────────┘
│ ┌ 选集侧栏 Sheet (380px，从右滑入) ──┐                                             │
│ │ 逆光而行 · 共 78 集        [正序 ▾]│                                             │
│ │ ──────────────────────────────────│                                             │
│ │ 01✓  02✓  03✓  04✓  05◀  06  07  │  ← 5 列网格，44px 高                          │
│ │ 08   09   10   11   12   13   14  │                                             │
│ │ …                                │                                             │
│ │ ──────────────────────────────── │                                             │
│ │ [⇩ 下载本集]  [⇩ 下载全部未看 43 集] │  ← SheetFooter sticky                          │
│ └──────────────────────────────────┘                                               │
└───────────────────────────────────────────────────────────────────────────────────────┘
```

**详细规范**

| 层 | 规范 |
|---|---|
| 舞台 | `bg-stage`（**恒定暗色**，`--stage` 双主题同值）。`aspect-video max-w-[1080px] mx-auto rounded-lg overflow-hidden`。边框模式下 `border border-border-subtle`；无边框模式下 `rounded-none`（对应现役 `.layout-borderless`）。 |
| 标题条 | 高 56px，`bg-background/80 backdrop-blur-md border-b border-border-subtle`。左：竖版海报 32×42 + 剧名 `--text-base/600` + `·` + 集名 `--text-sm text-foreground-muted`。右：`Switch`（弹幕）+ `Button variant="outline" size="sm"`（选集 `32 ▾`）。**鼠标静止 3s 后与控制条一起淡出**（复用 video.js 的 `--media-duration-player`）。 |
| 中央播放键 | 72px 圆形 `bg-black/45 backdrop-blur-sm`，`hover:scale-105 transition-transform duration-player`，`--ease-spring`。仅在暂停时显示。 |
| 弹幕层 | `position:absolute inset-0 pointer-events-none z-[--z-danmaku]`。轨道：`rows = clamp(1, floor(height/32), 12)`，行高 32px，字体 `--text-base/500`，颜色 `--danmaku`，`text-shadow: 0 1px 2px rgb(0 0 0 / 0.8)`。飞行时长 8s（线性，`--ease-out-expo` 仅用于入场）。**防重叠规则直接复用 `core/danmaku/layout.go` 的输出**，两端一致（承接 ARCHITECTURE.md §5.2）。弹幕开关、透明度（0–100%）、字号（12/16/20）、区域（1/2/3/全屏）、速度（0.5–2x）全部走设置页与控制条同一份 `playback_preferences`。 |
| 进度条 | **用 `<TimeSlider/>`，不自己画。** 覆写 `--media-*` 令牌（§3.1 表末）：轨道 `h-1.5`，已播 `bg-primary`，缓冲 `bg-white/25`，thumb `size-3.5 rounded-full bg-white shadow-md hover:scale-110`，hover 时放大 1.6× 并显示缩略图预览（`renderThumbnail`）。 |
| 控制条 | 复用 skin 的 `Controls.Root/Backdrop/Content/Group`。结构：左组（`PlayButton` + `VolumePopover`）、中组（`Time.Value current` + `TimeSlider` + `Time.Value remaining`）、右组（`弹幕 Switch` + `SettingsMenu` + `PiPButton` + `FullscreenButton`）。背景 `bg-media-popover` + `backdrop-blur-md`，高度 44px。 |
| 画质菜单 | 复用 `<VideoSettingsMenu>` 的 `<QualityMenu/>`（`useQualityOptions` 会自动带上"自动"项），在其下追加 `<DanmakuMenu/>` 与 `<EpisodeMenu/>`（本地新增，走 `Sheet`）。菜单项标签中文：`自动 / 1080P / 720P / 480P`，当前项 `✓` 靠右。 |
| 选集侧栏 | `Sheet side="right"` 覆写 `sm:max-w-[380px] p-0`，内容 `flex flex-col`：头（剧名 + 集数 + 排序 `Select`）、体（5 列 × 44px 网格，`overflow-y-auto`，sticky 头尾）、尾（两个下载按钮，`sticky bottom-0 bg-card border-t`）。ESC 关闭、`Tab` 焦点循环、关闭后焦点归还触发按钮（Radix 默认行为，不需额外代码）。 |
| 等待/缓冲 | 用 `Progress`（h-1.5，`bg-primary/20` + `bg-primary`）替代现役三处自定义 `.player-wait-bar`。错误态用 `Empty`（`variant="icon"`，`Film` 图标 + 一行原因 + `[重试] [复制诊断]`）。 |
| 全屏 | 走 **Electron 原生全屏**（`win.setFullScreen`），不依赖 CSS `:fullscreen` —— 这是 hongguo-monorepo 已验证的做法（见 CortexDB 记录的"全屏丢失控制条"教训）。控制条显隐由 `<Controls.Root>` 的 `data-visible` 驱动，不用 CSS 全屏事件。 |

---

## 6. 跨端一致性

### 6.1 Flutter 侧 `ThemeExtension` 镜像

现役 `app_theme.dart` 的诊断：**只有 `ColorScheme.fromSeed`（seed `#FF664F`），零 `ThemeExtension`**，导致播放页、弹幕层、下载卡片等自定义组件只能直接写 `Color(0xFF...)` 字面量——这就是安卓端视觉漂移的根因。

新写法（**`ThemeExtension` 承载本文件 §3 的全部令牌**）：

```dart
// lib/ui/tokens.dart
@immutable
class HongguoTokens extends ThemeExtension<HongguoTokens> {
  const HongguoTokens({
    required this.canvas,      required this.surface,   required this.surfaceHover,
    required this.fg,          required this.fgMuted,    required this.fgSubtle,
    required this.primary,     required this.primaryHover,
    required this.onPrimary,   required this.primarySubtle,
    required this.onPrimarySubtle,
    required this.danger,      required this.warning,    required this.success,
    required this.gold,        required this.border,     required this.borderSubtle,
    required this.input,       required this.ring,       required this.skeleton,
    required this.scrim,       required this.stage,      required this.danmaku,
    required this.scrollThumb,
  });

  final Color canvas, surface, /* … */ scrollThumb;

  /// 播放台恒定暗色，弹幕色恒定 —— 不随 brightness 变。
  static const Color stage = Color(0xFF090A0C);
  static const Color danmaku = Color(0xFFFDF8F0);

  static const HongguoTokens light = HongguoTokens(
    canvas: Color(0xFFFBFAF8), surface: Color(0xFFFFFFFF), surfaceHover: Color(0xFFEAE7E2),
    fg: Color(0xFF252523), fgMuted: Color(0xFF686661), fgSubtle: Color(0xFF77736C),
    primary: Color(0xFFC83933), primaryHover: Color(0xFFAF2A27),
    onPrimary: Color(0xFFFDF9F8), primarySubtle: Color(0xFFFAE9E7),
    onPrimarySubtle: Color(0xFFA12B27),
    danger: Color(0xFFBA3437), warning: Color(0xFF9A681E), success: Color(0xFF32745C),
    gold: Color(0xFFE1A447),
    border: Color(0xFFE4E1DC), borderSubtle: Color(0xFFEEEBE6), input: Color(0xFFCCC8C1),
    ring: Color(0xFFC83933), skeleton: Color(0xFFE8E5E0), scrim: Color(0x73362C24),
    stage: stage, danmaku: danmaku, scrollThumb: Color(0xFFC6C2BB),
  );

  static const HongguoTokens dark = HongguoTokens(
    canvas: Color(0xFF111213), surface: Color(0xFF1C1D20), surfaceHover: Color(0xFF33353A),
    fg: Color(0xFFEFEDE8), fgMuted: Color(0xFFAAA59D), fgSubtle: Color(0xFFC5C0B8),
    primary: Color(0xFFF96A5D), primaryHover: Color(0xFFFF8370),
    onPrimary: Color(0xFF1B0806), primarySubtle: Color(0xFF512824),
    onPrimarySubtle: Color(0xFFF5A49C),
    danger: Color(0xFFF0A0A1), warning: Color(0xFFE1B978), success: Color(0xFF92C8AC),
    gold: Color(0xFFF6C86B),
    border: Color(0xFF46433D), borderSubtle: Color(0xFF3A3833), input: Color(0xFF6D675F),
    ring: Color(0xFFF96A5D), skeleton: Color(0xFF2D2F33), scrim: Color(0xA8000000),
    stage: stage, danmaku: danmaku, scrollThumb: Color(0xFF6D675F),
  );

  @override HongguoTokens copyWith({/* … */}) => /* … */;
  @override HongguoTokens lerp(ThemeExtension<HongguoTokens>? other, double t) {
    if (other is! HongguoTokens) return this;
    return HongguoTokens(
      canvas: Color.lerp(canvas, other.canvas, t)!, /* … 其余同上 */
    );
  }
}

// 用法：扩展名 `tokens` + 静态访问器
extension HongguoTokensX on BuildContext {
  HongguoTokens get tokens => Theme.of(this).extension<HongguoTokens>()!;
  Color get primary => tokens.primary;
}
```

**配套组件**（替代现役直接写 `Colors.xxx`）：

```dart
class HnButton extends StatelessWidget { /* size: sm/md/lg/hero, variant: primary/secondary/outline/ghost/brandSoft */ }
class HnCard extends StatelessWidget  { /* rounded-lg, border, p-5 */ }
class HnChip extends StatelessWidget  { /* rounded-sm, h-6/h-7, tabular figures */ }
class HnEpisodeGrid extends StatelessWidget { /* 44px 高，与 PC 同尺寸 */ }
class HnSkeleton extends StatelessWidget { /* aspect 3/4, colors.tokens.skeleton */ }
class HnEmpty extends StatelessWidget  { /* 统一空态 */ }
class HnPlayerControls extends StatelessWidget { /* Flutter 播放器控件层 + tokens（Android 底层解码仍是 ExoPlayer/Media3，UI 由 Flutter 绘制，与 PC 的 video.js skin 共用同一张令牌表） */ }
```

**暗色模式**：`ThemeData(brightness:, colorScheme: ColorScheme.fromSeed(seedColor: tokens.primary, brightness:), extensions: [tokens])`。**保留 M3**（不换骨架），但**所有色值走 `tokens` 而非 `colorScheme.*`**——这样两端真正共享的是 `HongguoTokens`，M3 只负责 ripple/elevation 等平台原生行为。

**断点/间距镜像**（Dart 侧只定义尺度，不定义断点）：

```dart
abstract final class HnSpacing {
  // 与 §3.3 的 Tailwind 数字档一一对应（0.25rem 基线）：1=4, 2=8, 3=12, 4=16,
  // 5=20, 6=24, 8=32, 10=40, 12=48 —— 名称不同但数值必须逐档相等
  static const double step1 = 4, step2 = 8, step3 = 12, step4 = 16, step5 = 20,
                      step6 = 24, step8 = 32, step10 = 40, step12 = 48;
  // 与 §3.4 同序同值（此前误写成 7/10/13/16，已按 §3.4 修正为 5/7/9/10/13/16/20）
  static const double radiusXs = 5, radiusSm = 7, radiusMd = 9, radiusLg = 10,
                      radiusXl = 13, radius2xl = 16, radius3xl = 20;
}

/// §3.2 的 10 档字号必须逐档镜像，否则两端标题/角标密度会漂移
abstract final class HnTypo {
  static const double t2xs = 10, tXs = 11, tSm = 12, tBase = 13, tMd = 15,
                      tLg = 17, tXl = 20, t2xl = 26, t3xl = 32, tDisplay = 44;
  /// 数字一律等宽（对应 CSS 的 `--font-numeric` / `tabular-nums`），避免进度与热度数字抖动
  static const List<FontFeature> tabular = [FontFeature.tabularFigures()];
}

/// §3.6 的动效令牌同值镜像（Flutter 无 CSS 过渡，用 Duration + Curve 表达）
abstract final class HnMotion {
  static const Duration instant = Duration(milliseconds: 50);
  static const Duration fast = Duration(milliseconds: 100);
  static const Duration base = Duration(milliseconds: 150);
  static const Duration slow = Duration(milliseconds: 250);
  static const Duration player = Duration(milliseconds: 200);
  static const Cubic easeOutExpo = Cubic(0.16, 1, 0.3, 1);
  static const Cubic easeSpring = Cubic(0.34, 1.56, 0.64, 1);
}
```

**镜像完整性清单**（Android = Flutter 是**正式端侧之一**，不是可选项，因此镜像必须覆盖 §3 全部令牌组，不能只有颜色）：

| §3 令牌组 | Flutter 承载 | 缺失后果 |
|---|---|---|
| §3.1 颜色（含 `--stage` / `--danmaku` / `--scrim` / `--scroll-thumb`） | `HongguoTokens`（上方） | 现役视觉漂移的根因 |
| §3.4 圆角 | `HnSpacing.radius*` | 卡片/按钮圆角两端不一致 |
| §3.3 间距 | `HnSpacing.step*` | 密度差一档，媒体库观感变化 |
| §3.2 排版 | `HnTypo` + `tabular` | 标题与角标字号在两端差 1–2px |
| §3.6 动效 | `HnMotion` | 播放器控件显隐节奏不同 |
| §3.7 z-index | Flutter 无 z-index：由 `Stack` 子序与 `Overlay` entry 入队序表达 | 弹幕层与控制条前后关系颠倒 |
| reduced motion | `MediaQuery.disableAnimationsOf(context)` / `AccessibilityFeatures.disableAnimations` | Android 无障碍开关被忽略（CSS 侧的 `@media` 在 Flutter 无效） |

### 6.2 三形态断点与栅格

| 形态 | 宽度 | PC 布局 | 栅格 | 播放器 |
|---|---|---|---|---|
| **PC** | ≥ 1280px | 侧栏 214px（可折叠 64px）+ 工具条 48px | 海报 `minmax(150px,1fr)` 自动填充；选集 `minmax(64px,1fr)`；选集侧栏 380px | 全控制条 + 弹幕 + 缩略图预览 + 原生全屏 |
| **平板 / Electron 竖屏窗口** | 768–1279px | 侧栏折叠为 64px 图标态；<1024px 隐藏 Hero 里的批量操作进 `⋯` 菜单 | 海报 `minmax(135px,1fr)`（5–6 列） | 控制条两行；选集 Sheet 全宽；弹幕行数上限 6 |
| **手机**（Android 为主，PC 窗口 < 640px 仅作调试） | < 768px | 无侧栏，底部 5 项导航（发现/搜索/追剧/下载/我的）；工具条退化为 48px 顶栏 | 发现页**固定 2 列**（`gap-x-3`），详情相关推荐 **3 列**；选集**固定 4 列**（56px） | 竖屏满宽、控制条贴合底部安全区、`aspect-[9/16]`；横屏时全屏 + 弹幕密度自动降一档 |

Flutter 侧断点用 `LayoutBuilder` + `MediaQuery.sizeOf(context).width`，阈值与 CSS 一致（768 / 1024 / 1280），**不使用 flutter_screenutil 的等比缩放**（会破坏"同一像素密度"的一致性）。

### 6.3 一致性守卫

| 守卫 | 做法 |
|---|---|
| 令牌同步 | `tools/check-tokens.mjs`：解析 `theme.css` 的 `:root`/`.dark`，比对 `tokens.dart` 的 `HongguoTokens.light/dark` 字段，缺一个/多一个/值不等即失败（CI 必跑）。**覆盖范围必须包含非颜色令牌**：`--text-*` ↔ `HnTypo`、`--radius-*` ↔ `HnSpacing.radius*`、`--spacing` 档位 ↔ `HnSpacing.step*`、`--duration-*`/`--ease-*` ↔ `HnMotion`（否则守卫只保护了颜色，圆角/字号/动效仍会静默漂移） |
| 暗色常亮区 | `--stage` 与 `--danmaku` 在两端都是**常量**，禁止参与主题切换（与 animeko 的 `alwaysDarkInEpisodePage` 同理） |
| 减少动效 | CSS 侧走 `@media (prefers-reduced-motion: reduce)`（§3.6）；Flutter 侧走 `MediaQuery.disableAnimationsOf(context)`，两端都必须在**播放器控件显隐**与**弹幕滚动**两处生效（弹幕降级为静态堆叠） |
| 弹幕布局 | 轨道数与防重叠由 `core/danmaku/layout.go` 单点计算，两端只做渲染（ARCHITECTURE.md §5.2 已定） |
| 字体 | 两端都用系统中文栈，不内嵌 webfont（Electron 与 Android 都能命中系统中文字体，避免 3MB 字体包） |

---

## 7. 落地路径

### 7.1 分步（每步可独立验证、可独立回滚）

| 步 | 内容 | 产出 | 验证判据 |
|---|---|---|---|
| **D0 · 令牌骨架** | `apps/desktop` 初始化 Vite+React+Tailwind v4；写 `theme.css` 全文（§3）；`shadcn init`；`ThemeProvider`；`globals.css` | `theme.css` / `globals.css` / `theme-provider.tsx` | `pnpm dev` 起来能看到底色随 `.dark` 切换；`tokens.dart` 已写好且 `check:tokens` 通过 |
| **D1 · 令牌守卫 + 文档站** | `tools/check-tokens.mjs`；`/design` 开发路由（令牌色卡页 + 三页面静态壳） | 校验脚本 + 令牌卡页 | 故意改一个色值 → CI 红 |
| **D2 · 播放器外壳（不动业务）** | 引入 `@videojs/react` + skin；`PlayerShell` 组件；`<HlsJsVideo/>`/`<NativeHlsVideo/>` 切换；`player.css` 覆写 `--media-*` | 播放壳可用 | 用**任意 mp4** 能播、有控制条、能全屏（验证 hongguo-monorepo 的全屏教训已规避） |
| **D3 · 播放器接内核** | `bridge/ts` 接 `core/media` 播放链；弹幕层 `DanmakuLayer`；选集 `Sheet` | 真实一集可播 + 弹幕 | 真实剧集播完一集，弹幕与 `core/danmaku` 输出一致 |
| **D4 · 发现页** | `PosterCard` / 网格 / Hero / Tabs / Empty / Skeleton 接真实聚合搜索 | 发现页 | 搜一次真实关键词，卡片数与搜索结果一致；空态不虚构内容 |
| **D5 · 详情页** | 头图 + 信息区 + 选集网格 + Tabs + 追剧 | 详情页 | 点任意集能跳到 D3 播放器并起播对应集 |
| **D6 · 侧栏 / 设置 / 下载** | `Sidebar` 组件化 + 设置页 + 下载管理（`Progress` 复用） | 剩余页面 | 下载任务能建、能看进度、失败可重试 |
| **D7 · Flutter 侧镜像** | `HongguoTokens` + `Hn*` 组件 + 三个页面对齐 | 安卓 UI | `check:tokens` 通过；安卓端深色下截图与 PC 端同页面色值一致 |
| **D8 · 三形态适配** | 断点栅格 / 竖屏布局 / 手机 2 列 | 响应式 | 1024 / 768 / 390 三个宽度截图归档到 `docs/screens/` |

### 7.2 如何避免 UI 重写期间功能回归

**铁律：UI 重写不与业务逻辑同 PR。**

1. **契约先行**。所有页面组件只接受 `contracts/*.schema.json` 生成的 TS 类型（ARCHITECTURE.md §2.1），UI 层不做网络调用、不做状态持久化。数据获取全部在 `src/features/*/api.ts`，UI 重写只换 `*.tsx`。
2. **Feature flag 双轨**。`src/pages/v1/`（现役逻辑 + 现役 DOM 标记）与 `src/pages/v2/`（新 UI）并存，`VITE_ROUTE=v1|v2` 切换，默认 v1。v2 稳定后删 v1。**任何时候都能一键回到旧 UI**。
3. **行为测试先行**。每个页面重写前，先把该页的**关键交互写成 Playwright 测试**（发现页：搜索/切题材/进详情；详情页：选集/追剧/批量下载；播放器：起播/切集/切画质/弹幕开关/全屏）。测试跑在 v1 上先绿，再在 v2 上跑绿。**UI 变绿不算完成，行为测试绿才算。**
4. **播放器单点替换**。D2/D3 只换 `PlayerShell`，不改任何其他页面的调用签名（`onEpisodeChange`、`onQualityChange` 等 props 契约不变），播放器内部随便重写。
5. **逐页合并，不做全量切换**。每个 D 步骤一个 PR，只触碰该页面的文件 + `theme.css`。禁止跨页 PR。
6. **UI 层的"不搬运"是产品决策，不再有许可门禁**。本仓已统一 GPL-3.0，`license-guard` 指纹门禁与 `legacy.lock.json` **均已删除**（`ARCHITECTURE.md` §6.1），从老仓搬 CSS/TSX/Dart 不再受限。UI 相关的老仓文件（`hongguo-drama/src/components/*`、`main.js`、guoapp 的 Dart screen）**仍不搬**，理由是产品决策：本仓要自己的双端 UI，一套 DOM 组件无法同时服务 Electron 与 Flutter。约束方式随之从 CI 门禁改为**评审约定 + 来源记录**：若某步确实搬入了老仓文件，必须在 `legacy/SOURCES.md`（`{repo, ref, license, 搬入路径}`）登记，并保持 `NOTICE` 里的第三方许可清单与实际依赖一致（GPL-3.0 第 4/5 条的署名与来源义务）。

### 7.3 每步的验证命令

```bash
# 令牌守卫
node tools/check-tokens.mjs

# 类型 + 构建
pnpm -C apps/desktop typecheck && pnpm -C apps/desktop build

# 行为回归（v1 与 v2 各跑一次）
pnpm -C apps/desktop test:e2e -- --project=v1
pnpm -C apps/desktop test:e2e -- --project=v2

# 许可门禁已删除（本仓即 GPL-3.0，见 ARCHITECTURE.md §6.1）：
# 来源记录与第三方许可清单改为发布前核对 legacy/SOURCES.md + NOTICE
```

---

## 8. 已定架构与待决事项

### 8.1 已定架构

| # | 决策 | 结论 | 对本文的约束 |
|---|---|---|---|
| A1 | **安卓端技术栈：Flutter（不切 Compose）** —— 原「待决事项 #4」已升级为已定架构，与 `ARCHITECTURE.md` 的目标架构一致 | **保持 Flutter**。UI 由 Flutter 绘制，Android 底层解码仍走 ExoPlayer/Media3；令牌由 `ThemeExtension<HongguoTokens>` 承载。不再评估 Compose。 | Android 是**正式端侧之一**，因此 §6 的 Flutter 令牌镜像（颜色/圆角/间距/排版/动效 + reduced motion，见 §6.1 完整性清单）是**强制性交付**，不是可选优化；§6.3 的 `check:tokens` 守卫必须覆盖非颜色令牌。同时 Android 侧播放器控件是 Flutter 自绘层，与 PC 的 video.js skin 共用同一张令牌表（§5.3）。 |
| A2 | 端侧划分 | PC = Electron + React 19 + video.js 10（§1.3）；Android = Flutter（§6.1）；两端共用 Go 内核与 `core/danmaku/layout.go` | 本文只定义视觉契约，两端各自渲染 |

### 8.2 待决事项

| # | 问题 | 建议 | 决策人 |
|---|---|---|---|
| 1 | 播放器皮肤是否 fork 上游 default skin 到本地 registry | **是**。`npx shadcn add @components/videojs/...` 生成到 `components/videojs/`，追加 `DanmakuMenu`/`EpisodeMenu`。理由：弹幕是本项目核心能力，不能靠 hack 上游。 | 主控 |
| 2 | 是否引入 `@videojs/hlsjs-video`（hls.js 1.6.19）还是只用内置 `@videojs/spf` | 先只用内置 SPF（已在 `@videojs/react` 硬依赖里），遇到 CDN 加密/特殊 playlist 再加 hls.js adapter。 | 主控 |
| 3 | 浅色主题是否保留 | 保留但**非默认**。浅色是"资料库/管理"场景，深色是"看片"场景。 | 主控 |

---

## 附：本文引用的真实源码位置

| 来源 | 路径 |
|---|---|
| shadcn/ui（`main` @ `4233ed1`, 2026-10-14） | `apps/v4/app/globals.css`、`apps/v4/components.json`、`apps/v4/registry/new-york-v4/ui/*.tsx`、`apps/v4/content/docs/(root)/theming.mdx`、`apps/v4/content/docs/(root)/tailwind-v4.mdx`、`apps/v4/content/docs/dark-mode/vite.mdx` |
| video.js（`main` @ `9401`, 2026-10-15，tag `video.js@10.1.0`） | `packages/video.js/src/videojs.ts`、`packages/react/src/index.ts`、`packages/react/src/player/create-player.tsx`、`packages/react/src/media/{hlsjs-video,native-hls-video,hls-video}/adapter.tsx`、`packages/adapters/hlsjs-video/package.json`、`packages/spf/README.md`、`packages/spf/docs/hls-engine.md`、`packages/skins/src/styles/{theme.css,tailwind.css,vars.ts}`、`packages/skins/src/skins/default/video/{skin.tsx,layout/controls.tsx}`、`packages/skins/src/skins/shared/video/menus/settings-menu.tsx`、`packages/skins/build/registry/{targets.ts,items/*.ts}` |
| jellyfin-web（GPL-2.0，只读） | `src/styles/{librarybrowser.scss,videoosd.scss}` |
| animeko（AGPL-3.0，与本仓 GPL-3.0 相容；**仅借鉴思路、不搬代码**，故仍标只读） | `app/shared/app-platform/src/commonMain/kotlin/ui/theme/DefaultSeedColor.kt`、`app/shared/app-data/src/commonMain/kotlin/data/models/preference/ThemeSettings.kt` |
| 本仓基线 | `refs/hongguo-drama/src/index.css`、`refs/hongguo-drama/src/components/*.css`、`refs/hongguo-drama/DESIGN.md`、`refs/guoapp-fork/lib/app_theme.dart`、`hongguo-monorepo/nas-backend/static/index.html` |
