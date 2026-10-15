#!/usr/bin/env python3
"""hongguo-next 设计令牌导出器（真源：docs/design-tokens.json）。

它做两件事，且只有两件事：

1. 生成 `apps/desktop/src/styles/tokens.css`（唯一产物，幂等）：
   - `:root` / `.dark` / `[data-always-dark]` 里的**令牌真身**（CSS 变量）
   - `@theme`（字体/字号/间距/缓动：Tailwind v4 的命名空间，值即令牌）
   - `@theme inline`（shadcn 语义名 → 令牌真身：`--color-*` / `--radius-*` / `--shadow-*`）
   - `@utility`（z-index / 时长的令牌化工具类）
   - reduced motion 全局降级（§3.6）

2. 生成一份 **Dart 对照产物**并与 `apps/android/lib/ui/tokens.dart` 逐字段比对。
   **绝不覆盖那个文件**：只报告差异（`--dart-out` 可把对照产物写到别处人工 diff）。

用法：
    python3 tools/gen_tokens.py                # 生成 tokens.css + 报告 Dart 差异
    python3 tools/gen_tokens.py --check        # 不写文件；tokens.css 与真源不一致时退出 1
    python3 tools/gen_tokens.py --report       # 只报告 Dart 差异，完全不碰 CSS
    python3 tools/gen_tokens.py --print-dart   # 把 Dart 对照产物打到 stdout
    python3 tools/gen_tokens.py --dart-out /tmp/tokens_mirror.dart
    python3 tools/gen_tokens.py --out <path>   # 改 CSS 输出路径（例如渲染层入口在别处时）

退出码：0 = 一致 / 1 = CSS 过期或 Dart 与真源不一致 / 2 = 真源不可解析。
"""
from __future__ import annotations

import argparse
import json
import os
import re
import sys
import tempfile
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
JSON_PATH = REPO / 'docs' / 'design-tokens.json'
CSS_PATH = REPO / 'apps' / 'desktop' / 'src' / 'styles' / 'tokens.css'
DART_PATH = REPO / 'apps' / 'android' / 'lib' / 'ui' / 'tokens.dart'

SECTION_KEYS = ('colors', 'colorAliases', 'typography', 'spacing', 'radius',
                'shadows', 'motion', 'zIndex', 'contrast')


# --------------------------------------------------------------------- 真源
def load_tokens(path: Path = JSON_PATH) -> dict:
    try:
        with path.open(encoding='utf-8') as fh:
            tokens = json.load(fh)
    except FileNotFoundError:
        raise SystemExit(f'[gen_tokens] 找不到真源：{path}')
    except json.JSONDecodeError as exc:
        raise SystemExit(f'[gen_tokens] 真源不是合法 JSON：{path}\n  {exc}')
    missing = [k for k in SECTION_KEYS if k not in tokens]
    if missing:
        raise SystemExit(f'[gen_tokens] 真源缺少段落：{", ".join(missing)}')
    problems = validate_tokens(tokens)
    if problems:
        raise SystemExit('[gen_tokens] 真源结构有问题：\n  - ' + '\n  - '.join(problems))
    return tokens


def validate_tokens(tokens: dict) -> list[str]:
    """结构自检：返回问题列表（空 = 结构可用）。生成器与守卫共用同一套判断。

    守卫必须先于渲染跑这一关 —— 否则真源结构坏了会变成 KeyError 回溯，
    而不是一条可读的失败信息。
    """
    problems: list[str] = []
    colors = tokens.get('colors') or {}
    order = tokens.get('colorOrder') or []
    if not colors:
        problems.append('colors 为空')
    if set(order) != set(colors):
        only_order = sorted(set(order) - set(colors))
        only_colors = sorted(set(colors) - set(order))
        problems.append(f'colorOrder 与 colors 不一致（只在 colorOrder：{only_order}；'
                        f'只在 colors：{only_colors}）')
    for name, spec in colors.items():
        for theme in ('light', 'dark'):
            entry = spec.get(theme)
            if not isinstance(entry, dict):
                problems.append(f'colors.{name}.{theme} 缺失')
                continue
            for field in ('oklch', 'hex'):
                if not entry.get(field):
                    problems.append(f'colors.{name}.{theme}.{field} 为空')
            if entry.get('hex') and not re.fullmatch(r'#[0-9a-fA-F]{6}', entry['hex']):
                problems.append(f'colors.{name}.{theme}.hex 不是 6 位 HEX：{entry["hex"]}')
        if not spec.get('dart'):
            problems.append(f'colors.{name} 缺 dart 镜像字段名')
    for name, spec in (tokens.get('colorAliases') or {}).items():
        if spec.get('of') not in colors and spec.get('of') not in (tokens.get('colorAliases') or {}):
            problems.append(f'colorAliases.{name} 指向不存在的令牌：{spec.get("of")}')
    for path, condition, message in (
            ('typography.scale', tokens.get('typography', {}).get('scale'), '为空'),
            ('typography.families', tokens.get('typography', {}).get('families'), '为空'),
            ('spacing.scale', tokens.get('spacing', {}).get('scale'), '为空'),
            ('radius.scale', tokens.get('radius', {}).get('scale'), '为空'),
            ('shadows.scale', tokens.get('shadows', {}).get('scale'), '为空'),
            ('motion.durations', tokens.get('motion', {}).get('durations'), '为空'),
            ('zIndex.scale', tokens.get('zIndex', {}).get('scale'), '为空'),
            ('contrast.pairs', tokens.get('contrast', {}).get('pairs'), '为空'),
    ):
        if not condition:
            problems.append(f'{path} {message}')
    if not (tokens.get('motion', {}).get('reducedMotion') or {}).get('mediaQuery'):
        problems.append('motion.reducedMotion.mediaQuery 缺失')
    if not isinstance(tokens.get('guard', {}).get('componentColorNames'), list):
        problems.append('guard.componentColorNames 缺失')
    return problems


def rel(path: Path) -> str:
    try:
        return str(path.relative_to(REPO))
    except ValueError:
        return str(path)


def unknowable_extra(tokens: dict) -> list[str]:
    """真源里声明了、但两端都不存在的颜色别名（防止拼错 token 名）。"""
    known = set(tokens['colors']) | set(tokens['colorAliases'])
    return [n for n, a in tokens['colorAliases'].items() if a['of'] not in known]


# ------------------------------------------------------------------ CSS 侧
PAD = '\x00'  # 注释列的对齐占位符：由 align() 在知道整块最长声明后统一补齐


def _decl(name: str, value: str, comment: str | None = None) -> str:
    line = f'  {name}: {value};'
    return line + PAD + f'/* {comment} */' if comment else line


def align(lines: list[str]) -> list[str]:
    """按「连续声明段」对齐注释列（值长度不一：修正过的 oklch 比 §3.1 的写法长）。"""
    out: list[str] = list(lines)
    run: list[int] = []

    def flush() -> None:
        if not run:
            return
        column = max(len(out[i].split(PAD)[0]) for i in run) + 2
        for i in run:
            head, tail = out[i].split(PAD)
            out[i] = head + ' ' * max(1, column - len(head)) + tail
        run.clear()

    for index, line in enumerate(lines):
        if PAD in line:
            run.append(index)
        else:
            flush()
    flush()
    return out


def _color_comment(spec: dict, extra: str = '') -> str:
    bits = [spec['hex']]
    if spec.get('alpha') is not None:
        bits.append(f"alpha {spec['alpha']}")
    if extra:
        bits.append(extra)
    return ' · '.join(bits)


def _rule(selector: str, body: list[str]) -> list[str]:
    return [f'{selector} {{'] + align(body) + ['}', '']


def render_css(tokens: dict) -> str:
    meta = tokens['meta']
    out: list[str] = []
    add = out.append

    add('/* ' + '=' * 76)
    add(' * hongguo-next 设计令牌 —— 由 tools/gen_tokens.py 从 docs/design-tokens.json 生成。')
    add(' * 请勿手改本文件：改真源后运行 `python3 tools/gen_tokens.py`。')
    add(' * 守卫：`python3 tools/check_tokens.py`（CI 必跑）；设计说明见 docs/DESIGN-SYSTEM.md §2/§3。')
    add(' *')
    add(' * 结构：')
    add(' *   1. :root / .dark / [data-always-dark]  令牌真身（CSS 变量，浅色为默认）')
    add(' *   2. @theme         字体栈 / --text-* 字号 / --spacing / --ease-*（Tailwind 命名空间）')
    add(' *   3. @theme inline  shadcn 语义名 → 令牌真身（--color-* / --radius-* / --shadow-*）')
    add(' *   4. @utility       z-index 与时长的令牌化工具类（避免 z-[9999] 这类任意值）')
    add(' *   5. reduced motion 全局降级（§3.6）')
    add(' *')
    add(' * 归属边界：globals.css（渲染层入口）负责 `@import "tailwindcss"`、')
    add(' * `@custom-variant dark (&:is(.dark *))` 与 base 层（body 用 var(--font-sans)/var(--text-base)）。')
    add(f' * 颜色权威值：{meta["colorAuthority"].upper()} —— 跨端无损（Flutter 侧 Color(int) 无法表达 oklch）；')
    add(' * oklch 是本文件的 CSS 表达，守卫会断言它能逐值往返到同一 HEX。')
    add(' * ' + '=' * 76 + ' */')
    add('')

    # ---------------------------------------------------------- 1. :root
    body: list[str] = []
    body.append('  /* color-scheme 让原生滚动条/表单控件跟随主题（§2.4） */')
    body.append('  color-scheme: light;')
    body.append('')
    body.append('  /* ── §3.1 颜色 · 浅色 ───────────────────────────────────────────── */')
    for name in tokens['colorOrder']:
        spec = tokens['colors'][name]
        if spec.get('constant'):
            continue
        body.append(_decl(f'--{name}', spec['light']['oklch'], _color_comment(spec['light'], spec['role'])))
    body.append('')
    body.append('  /* ── §3.1 恒定暗色区：双主题同值，不参与 .dark（§6.3） ──────────── */')
    for name in tokens['colorOrder']:
        spec = tokens['colors'][name]
        if not spec.get('constant'):
            continue
        body.append(_decl(f'--{name}', spec['light']['oklch'],
                          _color_comment(spec['light'], '恒定（播放台上）')))
    body.append('')
    body.append('  /* ── §3.3 布局常量（断点相关，故不进 spacing 阶梯） ─────────────── */')
    for _key, spec in tokens['spacing']['layout'].items():
        body.append(_decl(spec['css'], f"{spec['px']}px", spec['role']))
    body.append('')
    body.append('  /* ── §3.4 圆角基准（派生档见 @theme inline，单一基准派生） ──────── */')
    radius = tokens['radius']
    body.append(_decl('--radius', f"{radius['base']['rem']}rem", radius['base']['role']))
    body.append('')
    body.append('  /* ── §3.5 阴影真身（值随主题切换；--shadow-* 是同值的 Tailwind 映射名） ── */')
    for name, spec in tokens['shadows']['scale'].items():
        body.append(_decl(spec['css'], spec['light'], spec['role']))
    body.append('')
    body.append('  /* ── §3.6 动效时长（缓动见 @theme；background: 见 §3.6 的 reduced motion） ── */')
    for name, spec in tokens['motion']['durations'].items():
        body.append(_decl(spec['css'], f"{spec['ms']}ms", f"{spec['role']}（{spec['easing']}）"))
    body.append('')
    body.append('  /* ── §3.7 z-index：单一来源，禁止魔法数字；与 Tailwind 裸数字档同值 ── */')
    for name, spec in tokens['zIndex']['scale'].items():
        body.append(_decl(spec['css'], str(spec['value']), spec['role']))
    out += _rule(':root', body)

    # ------------------------------------------------- .dark / 强制暗色
    dark_body: list[str] = [
        '  /* class 策略（§2.4）：storage key `hn-theme`，默认 dark。',
        '   * [data-always-dark] 是播放器页覆盖全局偏好的挂点（§2.4），与 .dark 同值、同一次推导。 */',
        '  color-scheme: dark;', '']
    dark_body.append('  /* ── §3.1 颜色 · 深色 ───────────────────────────────────────────── */')
    for name in tokens['colorOrder']:
        spec = tokens['colors'][name]
        if spec.get('constant'):
            continue
        dark_body.append(_decl(f'--{name}', spec['dark']['oklch'], _color_comment(spec['dark'], spec['role'])))
    dark_body.append('')
    dark_body.append('  /* ── §3.5 阴影 · 深色 ───────────────────────────────────────────── */')
    for name, spec in tokens['shadows']['scale'].items():
        dark_body.append(_decl(spec['css'], spec['dark'], spec['role']))
    out += _rule('.dark,\n[data-always-dark]', dark_body)

    # ---------------------------------------------------------- @theme
    theme: list[str] = []
    typo = tokens['typography']
    theme.append('  /* ── §3.2 字体栈：系统中文栈，不内嵌 webfont（§6.3） ─────────────── */')
    theme.append(_decl(typo['families']['sans']['tailwind'], typo['families']['sans']['css'],
                       '全部 UI 文字'))
    theme.append(_decl(typo['families']['numeric']['tailwind'], typo['families']['numeric']['css'],
                       '数字（配 tabular-nums）'))
    theme.append('')
    theme.append('  /* ── §3.3 间距基线：Tailwind 数字档 = 基线 × 档位（p-4 = 16px） ──── */')
    theme.append(_decl(tokens['spacing']['tailwind'], f"{tokens['spacing']['basePx'] / 16}rem",
                       f"{tokens['spacing']['basePx']}px 基线"))
    theme.append('')
    theme.append('  /* ── §3.2 字号：Tailwind 的 --text-* 命名空间（字号+行高+字距+字重） ── */')
    for name, spec in typo['scale'].items():
        theme.append(_decl(spec['tailwind'], f"{spec['sizePx']}px", spec['role']))
        theme.append(_decl(f"{spec['tailwind']}--line-height", str(spec['lineHeight'])))
        theme.append(_decl(f"{spec['tailwind']}--letter-spacing", spec['letterSpacing']))
        theme.append(_decl(f"{spec['tailwind']}--font-weight", str(spec['weight'])))
    theme.append('')
    theme.append('  /* ── §3.6 缓动：Tailwind 的 --ease-* 命名空间（ease-out 用内置） ── */')
    for name, spec in tokens['motion']['easings'].items():
        if not spec.get('tailwind'):
            continue
        theme.append(_decl(spec['tailwind'], spec['value'], spec['role']))
    out += _rule('@theme', theme)

    # --------------------------------------------------- @theme inline
    inline: list[str] = []
    inline.append('  /* ── §3.1 颜色：shadcn 语义名 → 令牌真身（浅/深由 :root/.dark 决定） ──')
    inline.append('   * inline 让工具类直接内联 `var(--token)`（bg-background → var(--background)），')
    inline.append('   * 因此主题切换只需覆盖 :root/.dark 的令牌真身，无需重新生成工具类。 */')
    for name in tokens['colorOrder']:
        spec = tokens['colors'][name]
        if not spec.get('tailwind'):
            continue
        inline.append(_decl(spec['tailwind'], f'var(--{name})', spec['role']))
    inline.append('')
    inline.append('  /* §3.1 语义别名：shadcn 组件用名（卡片/浮层的字、sidebar 扩展名） */')
    for name, spec in tokens['colorAliases'].items():
        inline.append(_decl(f'--color-{name}', f"var(--{spec['of']})", spec['role']))
    inline.append('')
    inline.append('  /* ── §3.4 圆角：单一基准派生（shadcn 模式） ─────────────────────── */')
    for name, spec in tokens['radius']['scale'].items():
        if name == 'full':
            inline.append(_decl(spec['tailwind'], spec['css'], spec['role']))
            continue
        mult = spec['multiplier']
        value = 'var(--radius)' if mult == 1 else f'calc(var(--radius) * {mult})'
        inline.append(_decl(spec['tailwind'], value, f"{spec['role']} · {spec['px']}px"))
    inline.append('')
    inline.append('  /* ── §3.5 阴影：真身是 --elevation-*（值随主题切换，故不能自引用同名） ── */')
    for name, spec in tokens['shadows']['scale'].items():
        if not spec.get('tailwind'):
            continue
        inline.append(_decl(spec['tailwind'], f"var(--elevation-{name})", spec['role']))
    out += _rule('@theme inline', inline)

    # ------------------------------------------------------- @utility
    util: list[str] = ['/* ── §3.7 z-index 令牌化工具类（与 z-10/20/… 裸数字档同值） ────────── */']
    for name, spec in tokens['zIndex']['scale'].items():
        util.append(f'@utility z-{name} {{')
        util.append(f'  z-index: var(--z-{name});')
        util.append('}')
    util.append('')
    util.append('/* ── §3.6 时长令牌化工具类（duration-<ms> 裸数字档亦同值） ─────────── */')
    for name, spec in tokens['motion']['durations'].items():
        util.append(f'@utility duration-{name} {{')
        util.append(f'  --tw-duration: var(--duration-{name});')
        util.append(f'  transition-duration: var(--duration-{name});')
        util.append('}')
    util.append('')
    out += util

    # --------------------------------------------------- reduced motion
    rm = tokens['motion']['reducedMotion']
    out.append(f'/* ── §3.6 reduced motion：全局降级（播放器控件显隐与弹幕滚动同样受此约束） ── */')
    out.append(f'@media {rm["mediaQuery"]} {{')
    out.append('  *,')
    out.append('  *::before,')
    out.append('  *::after {')
    out.append(f'    animation-duration: {rm["duration"]} !important;')
    out.append(f'    animation-iteration-count: {rm["iterationCount"]} !important;')
    out.append(f'    transition-duration: {rm["duration"]} !important;')
    out.append(f'    scroll-behavior: {rm["scrollBehavior"]} !important;')
    out.append('  }')
    out.append('}')
    out.append('')

    return '\n'.join(out).rstrip('\n') + '\n'


def write_if_changed(path: Path, content: str) -> bool:
    """原子写入；内容一致则不触碰文件（保证幂等：重复运行 mtime 不变）。"""
    if path.exists() and path.read_text(encoding='utf-8') == content:
        return False
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, tmp = tempfile.mkstemp(dir=str(path.parent), prefix='.tokens-', suffix='.tmp')
    try:
        with os.fdopen(fd, 'w', encoding='utf-8') as fh:
            fh.write(content)
        os.replace(tmp, path)
    except BaseException:
        if os.path.exists(tmp):
            os.unlink(tmp)
        raise
    return True


# ----------------------------------------------------------------- Dart 侧
def dart_alpha(spec: dict) -> int:
    if spec.get('alpha') is None:
        return 255
    return round(spec['alpha'] * 255)


def dart_expected(tokens: dict) -> dict[str, tuple]:
    """真源 → Dart 期望值。key 形如 HongguoTokens.light.canvas / HnTypo.tBase。"""
    exp: dict[str, tuple] = {}
    for name in tokens['colorOrder']:
        spec = tokens['colors'][name]
        field = spec['dart']
        for theme in ('light', 'dark'):
            rgb = int(spec[theme]['hex'].lstrip('#'), 16)
            exp[f'HongguoTokens.{theme}.{field}'] = ('color', rgb, dart_alpha(spec[theme]))
    for name, spec in tokens['typography']['scale'].items():
        if spec.get('dart'):
            exp[f'HnTypo.{spec["dart"]}'] = ('double', float(spec['sizePx']))
    for key, spec in tokens['spacing']['scale'].items():
        if spec.get('dart'):
            exp[f'HnSpacing.{spec["dart"]}'] = ('double', float(spec['px']))
    for name, spec in tokens['radius']['scale'].items():
        if spec.get('dart') and name != 'full':
            exp[f'HnSpacing.{spec["dart"]}'] = ('double', float(spec['px']))
    for name, spec in tokens['motion']['durations'].items():
        if spec.get('dart'):
            exp[f'HnMotion.{spec["dart"]}'] = ('ms', int(spec['ms']))
    for name, spec in tokens['motion']['easings'].items():
        if spec.get('dart') and spec['value'].startswith('cubic-bezier'):
            nums = tuple(float(x) for x in re.findall(r'-?[\d.]+', spec['value']))
            exp[f'HnMotion.{spec["dart"]}'] = ('cubic', nums)
    return exp


def dart_unmirrored(tokens: dict) -> list[str]:
    """真源里 dart = null 的条目：不算失败，但要报出来（§6.1 的镜像完整性由人评估）。"""
    items: list[str] = []
    for key, spec in tokens['spacing']['scale'].items():
        if not spec.get('dart'):
            items.append(f'spacing.scale.{key}（{spec["px"]}px）')
    for name, spec in tokens['radius']['scale'].items():
        if not spec.get('dart'):
            items.append(f'radius.scale.{name}')
    for name, spec in tokens['motion']['easings'].items():
        if not spec.get('dart'):
            items.append(f'motion.easings.{name}（{spec["value"]}）')
    items.append('spacing.layout.*（§6.1：布局常量是 PC 形态专属）')
    items.append('zIndex.*（§3.7：Flutter 由 Stack 子序 / Overlay 入队序表达）')
    items.append('shadows.*（§6.1 未要求：Flutter 用 M3 elevation 承载投影）')
    items.append('colorAliases.*（shadcn 用名，Flutter 无对应语义名）')
    return items


def parse_dart(src: str) -> dict[str, tuple]:
    """从 Dart 源码里抽出可与真源对表的常量（tokens.dart 与 --print-dart 产物同一语法）。"""
    found: dict[str, tuple] = {}

    consts = {m.group(1): int(m.group(2), 16) for m in
              re.finditer(r'static const Color (\w+) = Color\(0x([0-9A-Fa-f]{8})\);', src)}

    def color(expr: str) -> int | None:
        m = re.fullmatch(r'Color\(0x([0-9A-Fa-f]{8})\)', expr.strip())
        if m:
            return int(m.group(1), 16)
        m = re.fullmatch(r'(\w+)', expr.strip())
        if m and m.group(1) in consts:
            return consts[m.group(1)]
        return None

    for block in re.finditer(r'static const HongguoTokens (\w+) = HongguoTokens\((.*?)\n  \);',
                             src, re.S):
        theme, body = block.group(1), block.group(2)
        for entry in re.finditer(r'(\w+):\s*([^,\n]+),', body):
            field, expr = entry.group(1), entry.group(2)
            value = color(expr)
            if value is None:
                continue
            found[f'HongguoTokens.{theme}.{field}'] = ('color', value & 0xFFFFFF, value >> 24)

    for cls, segment in class_segments(src).items():
        if cls in ('HnSpacing', 'HnTypo'):
            for m in re.finditer(r'static const double (\w+) = ([\d.]+);', segment):
                found[f'{cls}.{m.group(1)}'] = ('double', float(m.group(2)))
        elif cls == 'HnMotion':
            for m in re.finditer(r'static const Duration (\w+) = Duration\(milliseconds: (\d+)\);',
                                 segment):
                found[f'HnMotion.{m.group(1)}'] = ('ms', int(m.group(2)))
            for m in re.finditer(r'static const Cubic (\w+) = Cubic\(([^)]*)\);', segment):
                nums = tuple(float(x.strip()) for x in m.group(2).split(','))
                found[f'HnMotion.{m.group(1)}'] = ('cubic', nums)
    return found


def class_segments(src: str) -> dict[str, str]:
    """把 Dart 源码按 `class X` 切成段落，便于按类名精确取常量（不猜 class 归属）。"""
    marks = [(m.start(), m.group(1)) for m in
             re.finditer(r'^(?:abstract final )?class (\w+)', src, re.M)]
    segments: dict[str, str] = {}
    for i, (pos, name) in enumerate(marks):
        end = marks[i + 1][0] if i + 1 < len(marks) else len(src)
        segments.setdefault(name, '')
        segments[name] += src[pos:end]
    return segments


def diff_dart(tokens: dict, src: str) -> list[dict]:
    expected = dart_expected(tokens)
    actual = parse_dart(src)
    rows: list[dict] = []

    def close(kind: str, exp, got) -> bool:
        # exp/got 是 dart_expected / parse_dart 里去掉 kind 之后的载荷元组
        if kind == 'color':
            # (rgb, alphaByte)
            return exp[0] == got[0] and abs(exp[1] - got[1]) <= 1
        if kind == 'cubic':
            e, g = exp[0], got[0]
            return len(e) == len(g) and all(abs(a - b) < 1e-6 for a, b in zip(e, g))
        return abs(float(exp[0]) - float(got[0])) < 1e-6

    for key, exp in expected.items():
        kind = exp[0]
        if key not in actual:
            rows.append({'key': key, 'status': 'missing', 'expected': exp[1:], 'actual': None})
        elif actual[key][0] != kind or not close(kind, exp[1:], actual[key][1:]):
            rows.append({'key': key, 'status': 'mismatch',
                         'expected': exp[1:], 'actual': actual[key][1:]})
    for key, got in actual.items():
        if key not in expected:
            rows.append({'key': key, 'status': 'extra', 'expected': None, 'actual': got[1:]})
    rows.sort(key=lambda r: (r['status'], r['key']))
    return rows


def render_dart(tokens: dict, source: str = str(DART_PATH)) -> str:
    """Dart 对照产物：与 apps/android/lib/ui/tokens.dart 同语法，供逐字段 diff。

    它不是 tokens.dart 的替代品（没有 copyWith / lerp / 构造函数），
    因为它存在的唯一目的是「机器可比对」。
    """
    lines: list[str] = []
    add = lines.append
    add('// 由 tools/gen_tokens.py 从 docs/design-tokens.json 生成的 Dart 对照产物。')
    add('// 只用于与 ' + source + ' 逐字段比对（--print-dart / --dart-out）。')
    add('// 不覆盖、也不替代该文件：这里没有 copyWith / lerp / 构造函数。')
    add('')
    add('class HongguoTokens {')
    for theme, label in (('light', '浅色'), ('dark', '深色')):
        add(f'  /// 真源 colors.*.{theme}（{label}）。')
        add(f'  static const HongguoTokens {theme} = HongguoTokens(')
        for name in tokens['colorOrder']:
            spec = tokens['colors'][name]
            argb = (dart_alpha(spec[theme]) << 24) | int(spec[theme]['hex'].lstrip('#'), 16)
            add(f'    {spec["dart"]}: Color(0x{argb:08X}),')
        add('  );')
        add('')
    add('}')
    add('')
    add('abstract final class HnSpacing {')
    for key, spec in tokens['spacing']['scale'].items():
        if spec.get('dart'):
            add(f'  static const double {spec["dart"]} = {spec["px"]};')
    for name, spec in tokens['radius']['scale'].items():
        if spec.get('dart') and name != 'full':
            add(f'  static const double {spec["dart"]} = {spec["px"]};')
    add('}')
    add('')
    add('abstract final class HnTypo {')
    for _name, spec in tokens['typography']['scale'].items():
        add(f'  static const double {spec["dart"]} = {spec["sizePx"]};')
    add('}')
    add('')
    add('abstract final class HnMotion {')
    for _name, spec in tokens['motion']['durations'].items():
        add(f'  static const Duration {spec["dart"]} = Duration(milliseconds: {spec["ms"]});')
    for _name, spec in tokens['motion']['easings'].items():
        if not spec.get('dart') or not spec['value'].startswith('cubic-bezier'):
            continue
        nums = ', '.join(re.findall(r'-?[\d.]+', spec['value']))
        add(f'  static const Cubic {spec["dart"]} = Cubic({nums});')
    add('}')
    add('')
    return '\n'.join(lines)


# ------------------------------------------------------------------- 报告
STATUS_LABEL = {'mismatch': '值不一致', 'missing': 'Dart 缺字段', 'extra': 'Dart 多字段'}


def format_diff(rows: list[dict], dart_path: Path) -> tuple[list[str], int]:
    errors = [r for r in rows if r['status'] != 'ok']
    lines: list[str] = []
    if not errors:
        lines.append(f'  ✓ 颜色/圆角/间距/字号/动效逐字段一致（{rel(dart_path)}）')
        return lines, 0
    by_status: dict[str, list[dict]] = {}
    for row in errors:
        by_status.setdefault(row['status'], []).append(row)
    for status in ('mismatch', 'missing', 'extra'):
        rows_s = by_status.get(status, [])
        if not rows_s:
            continue
        lines.append(f'  ✗ {STATUS_LABEL[status]}：{len(rows_s)} 项')
        for row in rows_s[:12]:
            lines.append(f'      {row["key"]}  真源={row["expected"]}  Dart={row["actual"]}')
        if len(rows_s) > 12:
            lines.append(f'      …另有 {len(rows_s) - 12} 项（--print-dart 可看全量）')
    return lines, len(errors)


def report_dart(tokens: dict, dart_path: Path = DART_PATH) -> tuple[bool, list[str]]:
    lines: list[str] = ['[gen_tokens] Flutter 侧镜像校验（只读，不覆盖 ' + rel(dart_path) + '）']
    if not dart_path.exists():
        lines.append(f'  ! 未找到 {rel(dart_path)}，跳过镜像校验')
        return True, lines
    src = dart_path.read_text(encoding='utf-8')
    rows = diff_dart(tokens, src)
    diff_lines, error_count = format_diff(rows, dart_path)
    lines += diff_lines
    unmirrored = dart_unmirrored(tokens)
    lines.append(f'  · 真源中声明为「不镜像」的条目（{len(unmirrored)} 类）：' + '；'.join(unmirrored[:3]) + ' …')
    return error_count == 0, lines


# -------------------------------------------------------------------- CLI
def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description='从 docs/design-tokens.json 生成 tokens.css 并校验 Dart 镜像（不覆盖 Dart）。')
    parser.add_argument('--json', type=Path, default=JSON_PATH, help='真源路径')
    parser.add_argument('--out', type=Path, default=CSS_PATH, help='tokens.css 输出路径')
    parser.add_argument('--dart', type=Path, default=DART_PATH, help='Flutter 镜像路径（只读）')
    parser.add_argument('--check', action='store_true', help='不写文件，只检查 tokens.css 是否与真源一致')
    parser.add_argument('--report', action='store_true', help='只报告 Dart 差异，完全不碰 CSS')
    parser.add_argument('--print-dart', action='store_true', help='把 Dart 对照产物打到 stdout')
    parser.add_argument('--dart-out', type=Path, default=None, help='把 Dart 对照产物写到指定路径（供人工 diff）')
    args = parser.parse_args(argv)

    tokens = load_tokens(args.json)
    bad = unknowable_extra(tokens)
    if bad:
        raise SystemExit(f'[gen_tokens] colorAliases 指向不存在的令牌：{", ".join(bad)}')

    css = render_css(tokens)
    exit_code = 0

    if args.print_dart or args.dart_out:
        mirror = render_dart(tokens, rel(args.dart))
        if args.print_dart:
            sys.stdout.write(mirror)
        if args.dart_out:
            write_if_changed(args.dart_out, mirror)
            print(f'[gen_tokens] Dart 对照产物已写入 {args.dart_out}（未触碰 {rel(args.dart)}）')

    if not args.report:
        if args.check:
            current = args.out.read_text(encoding='utf-8') if args.out.exists() else None
            if current != css:
                where = '不存在' if current is None else '与真源不一致（可能被手改）'
                print(f'[gen_tokens] ✗ {rel(args.out)} {where}；请运行 `python3 tools/gen_tokens.py`')
                exit_code = 1
            else:
                print(f'[gen_tokens] ✓ {rel(args.out)} 与真源一致（{len(css.splitlines())} 行）')
        else:
            changed = write_if_changed(args.out, css)
            print(f'[gen_tokens] {"写入" if changed else "未变（内容已一致）"}：{rel(args.out)}'
                  f'（{len(css.splitlines())} 行）')

    ok, lines = report_dart(tokens, args.dart)
    print('\n'.join(lines))
    if not ok:
        print('  → 真源与 Dart 不一致。真源是唯一权威：改动请落在 docs/design-tokens.json，'
              'Dart 侧由 M4b 负责人同步（本工具不会覆盖它）')
        exit_code = 1
    return exit_code


if __name__ == '__main__':
    sys.exit(main())
