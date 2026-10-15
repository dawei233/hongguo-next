#!/usr/bin/env python3
"""hongguo-next 令牌护栏（CI 必跑）：`python3 tools/check_tokens.py`。

它断言的是「真源唯一 + 两端一致 + 不退化」，共 9 组：

  1. 真源 JSON        合法 JSON、必需段落齐全、颜色别名指向存在的令牌
  2. CSS 产物         apps/desktop/src/styles/tokens.css 与真源逐字节一致（手改即红）
  3. 语义名覆盖        shadcn 组件用到的每个 Tailwind 语义名都能从令牌拿到值（否则组件样式全空）
  4. 颜色往返         真源里每个 oklch 都能往返到它的 HEX（跨端同色的机器保证）
  5. 对比度标注       重新计算 WCAG 对比度，与真源标注一致；未标注的未达标项即失败
  6. Flutter 镜像      apps/android/lib/ui/tokens.dart 与真源逐字段一致（只读，绝不覆盖）
  7. 字面量           新增代码里不得出现写死的字号/圆角/间距/颜色（令牌层自身除外）
  8. z-index          任何地方不得出现 z-index ≥ 1000，真源 z 阶梯也不得越界
  9. 旧变量名         §2.2 的老变量名（--bg/--text2/--grad/--brand …）不得在新代码里复活
 10. 单一真源         令牌只允许在 tokens.css 里声明（player.css 等只可覆写 --media-*）

颜色数学（oklch→sRGB、WCAG 相对亮度、alpha 合成）在本文件内自带实现；
已用 CSS Color 4 的 sRGB 原色（oklch → #ff0000/#00ff00/#0000ff/#ffff00）逐个校准过。

退出码：0 = 通过（警告不阻塞）；1 = 有失败项。
`--strict-literals` 把「字面量」的旁路警告升级为失败（渲染层修完之后打开）。
"""
from __future__ import annotations

import argparse
import json
import math
import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from gen_tokens import (  # noqa: E402
    CSS_PATH, DART_PATH, JSON_PATH, REPO, dart_expected, diff_dart, rel, render_css,
    validate_tokens,
)

# ------------------------------------------------------------------ 颜色数学
def _oklch_to_rgb255(lightness: float, chroma: float, hue: float) -> tuple[int, int, int]:
    rad = math.radians(hue)
    a, b = chroma * math.cos(rad), chroma * math.sin(rad)
    l_ = lightness + 0.3963377774 * a + 0.2158037573 * b
    m_ = lightness - 0.1055613458 * a - 0.0638541728 * b
    s_ = lightness - 0.0894841775 * a - 1.2914855480 * b
    l, m, s = l_ ** 3, m_ ** 3, s_ ** 3
    lin = (4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s,
           -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s,
           -0.0041960863 * l - 0.7034186147 * m + 1.7076147010 * s)
    out = []
    for value in lin:
        value = min(1.0, max(0.0, value))
        srgb = 12.92 * value if value <= 0.0031308 else 1.055 * value ** (1 / 2.4) - 0.055
        out.append(round(srgb * 255))
    return out[0], out[1], out[2]


OKLCH_RE = re.compile(r'oklch\(\s*([\d.]+)\s+([\d.]+)\s+([\d.]+)\s*(?:/\s*([\d.]+)\s*)?\)')


def parse_oklch(text: str) -> tuple[float, float, float, float | None]:
    match = OKLCH_RE.fullmatch(text.strip())
    if not match:
        raise ValueError(f'不是合法 oklch：{text}')
    alpha = match.group(4)
    return (float(match.group(1)), float(match.group(2)), float(match.group(3)),
            float(alpha) if alpha is not None else None)


def hex_to_rgb(text: str) -> tuple[int, int, int]:
    value = text.lstrip('#')
    return tuple(int(value[i:i + 2], 16) for i in (0, 2, 4))  # type: ignore[return-value]


def luminance(rgb: tuple[int, int, int]) -> float:
    def channel(raw: int) -> float:
        c = raw / 255
        return c / 12.92 if c <= 0.04045 else ((c + 0.055) / 1.055) ** 2.4
    r, g, b = (channel(x) for x in rgb)
    return 0.2126 * r + 0.7152 * g + 0.0722 * b


def contrast_ratio(fg: tuple[int, int, int], bg: tuple[int, int, int]) -> float:
    a, b = luminance(fg), luminance(bg)
    hi, lo = max(a, b), min(a, b)
    return (hi + 0.05) / (lo + 0.05)


def composite(fg: tuple[int, int, int], alpha: float, bg: tuple[int, int, int]) -> tuple[int, int, int]:
    return tuple(round(fg[i] * alpha + bg[i] * (1 - alpha)) for i in range(3))  # type: ignore[return-value]


# --------------------------------------------------------------------- 报告
class Report:
    def __init__(self, strict_literals: bool = False) -> None:
        self.failures: list[str] = []
        self.warnings: list[str] = []
        self.passed = 0
        self.strict_literals = strict_literals
        self.section_name = ''

    def section(self, title: str) -> None:
        self.section_name = title
        print(f'\n── {title} ' + '─' * max(0, 62 - len(title) * 2))

    def ok(self, message: str) -> None:
        self.passed += 1
        print(f'  ✓ {message}')

    def warn(self, message: str) -> None:
        self.warnings.append(f'{self.section_name}：{message}')
        print(f'  ! {message}')

    def fail(self, message: str) -> None:
        self.failures.append(f'{self.section_name}：{message}')
        print(f'  ✗ {message}')

    def check(self, condition: bool, ok_message: str, fail_message: str) -> bool:
        if condition:
            self.ok(ok_message)
        else:
            self.fail(fail_message)
        return condition


# ------------------------------------------------------------- 文件扫描工具
def iter_files(patterns: list[str], skip_dirs: list[str], excludes: list[str] | None = None):
    seen: set[Path] = set()
    excludes = excludes or []
    for pattern in patterns:
        for path in REPO.glob(pattern):
            if not path.is_file() or path in seen:
                continue
            parts = set(path.relative_to(REPO).parts)
            if parts & set(skip_dirs):
                continue
            rel_path = rel(path)
            if any(Path(rel_path).match(ex) for ex in excludes):
                continue
            seen.add(path)
            yield path


ALLOW_LINE = (
    re.compile(r'^\s*(?://|\*|/\*|#)'),          # 注释
    re.compile(r'var\(--'),                        # 引用令牌
    re.compile(r'--media-'),                       # 播放器契约覆写（§2.1）
    re.compile(r'oklch\('),                        # 令牌层自身的颜色写法
    re.compile(r'hn-tokens-allow'),                # 显式豁免标记
)

LITERAL_RULES = [
    (re.compile(r'#[0-9a-fA-F]{3,8}\b'), '十六进制颜色字面量（应使用令牌）'),
    (re.compile(r'\brgba?\(\s*\d'), 'rgb()/rgba() 颜色字面量'),
    (re.compile(r'\bhsla?\(\s*[\d.]'), 'hsl() 颜色字面量'),
    (re.compile(r'font-size:\s*[\d.]'), '写死的 font-size'),
    (re.compile(r'fontSize:\s*[\d.]'), '写死的 fontSize'),
    (re.compile(r'border-radius:\s*[\d.]'), '写死的 border-radius'),
    (re.compile(r'borderRadius:\s*[\d.]'), '写死的 borderRadius'),
    (re.compile(r'BorderRadius\.circular\(\s*[\d.]'), '写死的 BorderRadius.circular'),
    (re.compile(r'Radius\.circular\(\s*[\d.]'), '写死的 Radius.circular'),
    (re.compile(r'\b(text|rounded|p|px|py|pt|pb|pl|pr|m|mx|my|mt|mb|ml|mr|gap|space-[xy])-\[\s*[\d.]+(px|rem)'),
     '任意值字号/圆角/间距（应使用令牌档）'),
    (re.compile(r'Color\(0x[0-9A-Fa-f]{8}\)'), '写死的 Color(0x…)'),
]


def scan_literals(report: Report, guard: dict, level: str) -> None:
    scope = guard['literalScope']
    own_scope = level == 'error'
    patterns = scope['error'] if own_scope else scope['warn']
    excludes = scope.get('errorExclude', []) + ([scope['error'][0]] if False else [])
    hits: list[str] = []
    for path in iter_files(patterns, guard['scanSkipDirs'], excludes):
        for number, line in enumerate(path.read_text(encoding='utf-8').splitlines(), start=1):
            if any(allowed.search(line) for allowed in ALLOW_LINE):
                continue
            for rule, message in LITERAL_RULES:
                if rule.search(line):
                    hits.append(f'{rel(path)}:{number} {message}\n      {line.strip()[:110]}')
    where = '令牌层之外的新代码（styles/ 与 lib/ui/，令牌文件本身除外）' if own_scope else '渲染层（旁路）'
    if not hits:
        report.ok(f'{where}：无写死的颜色/字号/圆角/间距')
        return
    for hit in hits[:10]:
        print(f'    - {hit}')
    if len(hits) > 10:
        print(f'    …另有 {len(hits) - 10} 处')
    message = f'发现 {len(hits)} 处设计字面量'
    if level == 'warn' and not report.strict_literals:
        report.warn(message + '（渲染层归 apps/desktop/scripts/guard-tokens.mjs 判定，本守卫先旁路报告；'
                              '--strict-literals 可升级为失败）')
    else:
        report.fail(message + '（令牌层之外的代码必须只用令牌档）')


# ------------------------------------------------------------------- 各项检查
def check_json(report: Report) -> dict:
    report.section('真源 JSON')
    if not JSON_PATH.exists():
        report.fail(f'找不到真源 {rel(JSON_PATH)}')
        return {}
    try:
        tokens = json.loads(JSON_PATH.read_text(encoding='utf-8'))
    except json.JSONDecodeError as exc:
        report.fail(f'{rel(JSON_PATH)} 不是合法 JSON：{exc}')
        return {}
    report.ok(f'{rel(JSON_PATH)} 是合法 JSON（{len(JSON_PATH.read_text(encoding="utf-8").splitlines())} 行）')

    needed = ('meta', 'colors', 'colorAliases', 'typography', 'spacing', 'radius',
              'shadows', 'motion', 'zIndex', 'contrast', 'mirror', 'guard')
    missing = [key for key in needed if key not in tokens]
    report.check(not missing, f'必需段落齐全（{len(needed)} 段）',
                 f'缺少段落：{", ".join(missing)}')

    known = set(tokens.get('colors', {})) | set(tokens.get('colorAliases', {}))
    dangling = [name for name, spec in tokens.get('colorAliases', {}).items()
                if spec.get('of') not in known]
    report.check(not dangling, f'颜色别名都指向存在的令牌（{len(tokens.get("colorAliases", {}))} 个别名）',
                 f'别名声明的目标不存在：{", ".join(dangling)}')

    no_tailwind = [name for name, spec in tokens.get('colors', {}).items() if not spec.get('tailwind')]
    report.check(not no_tailwind, '每个颜色令牌都有 Tailwind 语义名',
                 f'缺 Tailwind 语义名：{", ".join(no_tailwind)}')

    problems = validate_tokens(tokens)
    if problems:
        report.fail(f'真源结构自检未通过（{len(problems)} 项）')
        for item in problems[:6]:
            print(f'      {item}')
    else:
        report.ok('真源结构自检通过（colorOrder/颜色/别名/各令牌组/guard 契约齐全）')
    return tokens


def check_css(report: Report, tokens: dict) -> None:
    report.section('CSS 产物 ↔ 真源')
    if not CSS_PATH.exists():
        report.fail(f'{rel(CSS_PATH)} 不存在；运行 `python3 tools/gen_tokens.py` 生成')
        return
    current = CSS_PATH.read_text(encoding='utf-8')
    expected = render_css(tokens)
    report.check(current == expected,
                 f'{rel(CSS_PATH)} 与真源逐字节一致（{len(expected.splitlines())} 行，生成器幂等）',
                 f'{rel(CSS_PATH)} 与真源不一致（手改或漏跑生成器）；修复：`python3 tools/gen_tokens.py`')


def check_semantic_coverage(report: Report, tokens: dict) -> None:
    report.section('shadcn 语义名覆盖')
    if not CSS_PATH.exists():
        report.fail('tokens.css 缺失，无法校验语义名')
        return
    css = CSS_PATH.read_text(encoding='utf-8')
    # 注意：文件头注释里也出现 "@theme inline" 字样，故用带花括号的块标记切分
    marker = '\n@theme inline {'
    if marker not in css:
        report.fail('tokens.css 里没有 @theme inline 块（shadcn 语义名将无法解析）')
        return
    head, inline = css.split(marker, 1)
    declared = set(re.findall(r'(--color-[a-z0-9-]+)\s*:', inline))
    names = tokens['guard']['componentColorNames']
    missing = [name for name in names if f'--color-{name}' not in declared]
    report.check(not missing,
                 f'组件用到的 {len(names)} 个语义名在 @theme inline 都有值',
                 f'以下语义名没有值（对应工具类会失效、组件样式会空）：{", ".join(missing)}')

    body_vars = set(re.findall(r'^\s*(--[a-z0-9-]+)\s*:', head, re.M))
    unresolved = []
    for name in names:
        spec = tokens['colors'].get(name) or tokens['colorAliases'].get(name)
        target = name if name in tokens['colors'] else (spec or {}).get('of')
        if f'--{target}' not in body_vars:
            unresolved.append(name)
    report.check(not unresolved, '每个语义名都解析到 :root/.dark 里的令牌真身',
                 f'以下语义名指向了不存在的令牌真身：{", ".join(unresolved)}')


def check_color_roundtrip(report: Report, tokens: dict) -> None:
    report.section('颜色往返（oklch ↔ HEX）')
    bad: list[str] = []
    for name, spec in tokens['colors'].items():
        for theme in ('light', 'dark'):
            entry = spec[theme]
            try:
                light, chroma, hue, _alpha = parse_oklch(entry['oklch'])
            except ValueError as exc:
                bad.append(f'{name}.{theme}: {exc}')
                continue
            computed = '#%02x%02x%02x' % _oklch_to_rgb255(light, chroma, hue)
            if computed != entry['hex'].lower():
                bad.append(f'{name}.{theme}: oklch→{computed} ≠ hex {entry["hex"]}')
    report.check(not bad,
                 f'{len(tokens["colors"]) * 2} 个颜色值都能往返（CSS 与 Flutter 渲染同一颜色）',
                 '以下颜色 oklch 与 hex 不自洽：\n      ' + '\n      '.join(bad[:8]))


def check_contrast(report: Report, tokens: dict) -> None:
    report.section('对比度标注（WCAG）')
    gaps = {(gap['fg'], gap['bg'], gap['theme']) for gap in tokens['contrast'].get('knownGaps', [])}
    problems: list[str] = []
    unused_gaps = set(gaps)

    def rgb_of(token: str, theme: str) -> tuple[tuple[int, int, int], float | None]:
        spec = tokens['colors'].get(token)
        if spec is None:  # 别名：跟随它指向的令牌
            spec = tokens['colors'][tokens['colorAliases'][token]['of']]
        entry = spec[theme]
        return hex_to_rgb(entry['hex']), entry.get('alpha')

    for pair in tokens['contrast']['pairs']:
        fg, bg, theme = pair['fg'], pair['bg'], pair['theme']
        fg_rgb, fg_alpha = rgb_of(fg, theme)
        bg_rgb, bg_alpha = rgb_of(bg, theme)
        if bg_alpha is not None:
            bg_rgb = composite(bg_rgb, bg_alpha, rgb_of('background', theme)[0])
        effective = composite(fg_rgb, fg_alpha, bg_rgb) if fg_alpha is not None else fg_rgb
        ratio = round(contrast_ratio(effective, bg_rgb), 2)
        if abs(ratio - pair['ratio']) > 0.02:
            problems.append(f'{theme} {fg}/{bg}: 标注 {pair["ratio"]} ≠ 实算 {ratio}')
        if ratio < pair['min']:
            unused_gaps.discard((fg, bg, theme))
            if (fg, bg, theme) not in gaps:
                problems.append(f'{theme} {fg}/{bg}: {ratio} < {pair["min"]} 且未在 knownGaps 里登记')
    report.check(not problems,
                 f'{len(tokens["contrast"]["pairs"])} 组对比度标注与实算一致，达标或已登记缺口',
                 '对比度问题：\n      ' + '\n      '.join(problems[:8]))
    if gaps:
        report.warn(f'已知缺口 {len(gaps)} 处（已登记在真源 contrast.knownGaps，含原因）：'
                    + '；'.join(f'{g[2]} {g[0]}/{g[1]}' for g in sorted(gaps)))
    if unused_gaps:
        report.warn(f'knownGaps 里有 {len(unused_gaps)} 处已经达标，请从真源移除：'
                    + ', '.join('/'.join(g) for g in sorted(unused_gaps)))


def check_dart(report: Report, tokens: dict) -> None:
    report.section('Flutter 镜像（只读，不覆盖）')
    if not DART_PATH.exists():
        report.warn(f'未找到 {rel(DART_PATH)}，跳过镜像校验')
        return
    rows = diff_dart(tokens, DART_PATH.read_text(encoding='utf-8'))
    if not rows:
        total = len(dart_expected(tokens))
        report.ok(f'{rel(DART_PATH)} 与真源逐字段一致（{total} 个可比对常量：颜色/圆角/间距/字号/动效）')
        return
    grouped: dict[str, list[dict]] = {}
    for row in rows:
        grouped.setdefault(row['status'], []).append(row)
    labels = {'mismatch': '值不一致', 'missing': 'Dart 缺字段', 'extra': 'Dart 多字段'}
    for status, items in grouped.items():
        report.fail(f'{labels.get(status, status)}：{len(items)} 项')
        for row in items[:8]:
            print(f'      {row["key"]}  真源={row["expected"]}  Dart={row["actual"]}')
        if len(items) > 8:
            print(f'      …另有 {len(items) - 8} 项（`python3 tools/gen_tokens.py --print-dart` 看全量）')
    report.warn('修复方向：真源（docs/design-tokens.json）是唯一权威；'
                'apps/android/lib/ui/tokens.dart 由 M4b 负责人同步，本守卫不会覆盖它')


def check_literals(report: Report, tokens: dict) -> None:
    report.section('字面量（令牌层之外）')
    scan_literals(report, tokens['guard'], level='error')
    scan_literals(report, tokens['guard'], level='warn')


def check_zindex(report: Report, tokens: dict) -> None:
    report.section('z-index')
    limit = tokens['guard']['zIndexMax']
    over = [(name, spec['value']) for name, spec in tokens['zIndex']['scale'].items()
            if spec['value'] > limit]
    report.check(not over, f'真源 z 阶梯 {len(tokens["zIndex"]["scale"])} 档全部 ≤ {limit}',
                 f'真源里有越界的 z-index：{over}')

    hits: list[str] = []
    patterns = ['apps/**/*.css', 'apps/**/*.scss', 'apps/**/*.ts', 'apps/**/*.tsx',
                'apps/**/*.js', 'apps/**/*.jsx', 'apps/**/*.dart', 'bridge/**/*.ts', 'core/**/*.go']
    rules = re.compile(r'(?:z-index:\s*(\d+))|(?:z-\[(\d+)\])|(?:zIndex:\s*(\d+))|'
                       r'(?:z_index:\s*(\d+))')
    for path in iter_files(patterns, tokens['guard']['scanSkipDirs']):
        for number, line in enumerate(path.read_text(encoding='utf-8').splitlines(), start=1):
            if line.lstrip().startswith(('//', '*', '/*', '#')):
                continue
            for match in rules.finditer(line):
                value = int(next(group for group in match.groups() if group))
                if value > limit:
                    hits.append(f'{rel(path)}:{number} z-index={value}')
    report.check(not hits, f'代码里没有 z-index ≥ {limit + 1}（扫了 {len(patterns)} 类文件）',
                 '出现越界 z-index：\n      ' + '\n      '.join(hits[:8]))


def check_legacy_names(report: Report, tokens: dict) -> None:
    report.section('旧变量名（防复活）')
    guard = tokens['guard']
    allowed = tokens['legacyNames'].get('allowFiles', [])
    extensions = tuple(guard['legacyScanExtensions'])
    patterns = [f'{root}/**/*' for root in guard['legacyScanRoots']]

    hits: list[str] = []
    scanned = 0
    for path in iter_files(patterns, guard['scanSkipDirs'] + ['legacy', 'refs', 'docs', 'node_modules'],
                           excludes=allowed):
        if path.suffix not in extensions:
            continue
        scanned += 1
        text = path.read_text(encoding='utf-8', errors='replace')
        for number, line in enumerate(text.splitlines(), start=1):
            for name, replacement in tokens['legacyNames']['denied'].items():
                if re.search(rf'(?<![\w-]){re.escape(name)}(?![\w-])', line):
                    hits.append(f'{rel(path)}:{number} {name} → {replacement}')
    report.check(not hits,
                 f'{scanned} 个代码文件里没有 {len(tokens["legacyNames"]["denied"])} 个旧变量名'
                 f'（白名单 {len(allowed)} 条：兼容层/文档）',
                 '旧变量名复活（新代码必须用新令牌）：\n      ' + '\n      '.join(hits[:8]))


def check_single_source(report: Report, tokens: dict) -> None:
    report.section('单一真源')
    # 任何自定义属性声明都算「第二个真源」（不要求独占一行：手写 CSS 常写成 `:root { --x: 1 }`）；
    # --media-* 覆写与 var() 引用由 ALLOW_LINE 放行
    token_decl = re.compile(r'(?:^|[{;\s])(--[a-z][a-z0-9-]*)\s*:')
    offenders: list[str] = []
    for path in iter_files(['apps/**/*.css', 'apps/**/*.scss'], tokens['guard']['scanSkipDirs'],
                           excludes=[rel(CSS_PATH)]):
        for number, line in enumerate(path.read_text(encoding='utf-8').splitlines(), start=1):
            if any(allowed.search(line) for allowed in ALLOW_LINE):
                continue
            if token_decl.search(line) or line.strip().startswith('@theme'):
                offenders.append(f'{rel(path)}:{number} {line.strip()[:80]}')
    report.check(not offenders,
                 f'令牌只在 {rel(CSS_PATH)} 里声明（其他样式文件只可覆写 --media-*）',
                 '别的样式文件在声明令牌（会造成第二个真源）：\n      ' + '\n      '.join(offenders[:6]))

    dart_other = [rel(path) for path in iter_files(['apps/android/lib/ui/**/*.dart'],
                                                   tokens['guard']['scanSkipDirs'],
                                                   excludes=[rel(DART_PATH)])
                  if 'static const Color(0x' in path.read_text(encoding='utf-8')]
    if dart_other:
        report.warn(f'Dart 侧除 tokens.dart 外还有文件写死颜色：{", ".join(dart_other)}')
    else:
        report.ok('Dart 侧只有 tokens.dart 持有颜色真值')


def check_cross_team_paths(report: Report) -> None:
    report.section('跨端/跨人接缝')
    other_guard = REPO / 'apps/desktop/scripts/guard-tokens.mjs'
    if other_guard.exists() and 'tokens.css' in other_guard.read_text(encoding='utf-8'):
        # 接缝已在 M4c 收敛：app.css 实际引入的是 ../styles/tokens.css（src/styles/），
        # 而 guard-tokens.mjs 也已改为两处都查。此处只校验引用是否真实成立。
        referenced = REPO / 'apps/desktop/src/renderer/styles/tokens.css'
        app_css = REPO / 'apps/desktop/src/renderer/app.css'
        if not referenced.exists() and not (app_css.exists()
                                           and 'styles/tokens.css' in app_css.read_text(encoding='utf-8')):
            report.warn('apps/desktop 的渲染层入口没有引入本真源的 tokens.css。'
                        '期望 app.css 里出现 `@import "../styles/tokens.css"`。')
    # 渲染层入口：实际是 app.css（它已注明"承担 DESIGN-SYSTEM §2.1 的 globals.css 角色"）。
    # 两个名字都接受，避免只认一个名字而报永久告警。
    entry_css = list(REPO.glob('apps/desktop/src/**/globals.css')) or list(REPO.glob('apps/desktop/src/**/app.css'))
    if not entry_css:
        report.warn('尚未发现渲染层 CSS 入口（globals.css / app.css）：'
                    '它需要 `@import "tailwindcss"`、`@import` 本 tokens.css 与 '
                    '`@custom-variant dark (&:is(.dark *))`')
    else:
        report.ok(f'渲染层入口存在：{rel(entry_css[0])}')


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description='hongguo-next 令牌护栏')
    parser.add_argument('--strict-literals', action='store_true',
                        help='把渲染层的字面量旁路警告升级为失败')
    args = parser.parse_args(argv)

    report = Report(strict_literals=args.strict_literals)
    print('[check_tokens] hongguo-next 令牌护栏（真源：docs/design-tokens.json）')

    tokens = check_json(report)
    if not tokens:
        print('\n[check_tokens] 真源不可用，后续检查跳过')
        return 1

    steps = (check_css, check_semantic_coverage, check_color_roundtrip, check_contrast,
             check_dart, check_literals, check_zindex, check_legacy_names,
             check_single_source, check_cross_team_paths)
    for step in steps:
        try:
            step(report, tokens) if step is not check_cross_team_paths else step(report)
        except Exception as exc:  # 守卫自己崩了也要以「失败」而不是回溯收场
            report.fail(f'{step.__name__} 检查自身异常：{type(exc).__name__}: {exc}')

    print('\n' + '═' * 66)
    if report.failures:
        print(f'[check_tokens] 失败：{len(report.failures)} 项（通过 {report.passed}，警告 {len(report.warnings)}）')
        for item in report.failures:
            print(f'  ✗ {item}')
        return 1
    print(f'[check_tokens] 通过：{report.passed} 项断言全绿，{len(report.warnings)} 条警告（不阻塞）')
    for item in report.warnings:
        print(f'  ! {item}')
    return 0


if __name__ == '__main__':
    sys.exit(main())
