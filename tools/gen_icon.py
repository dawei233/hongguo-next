#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""红果 Next（hongguo-next）应用图标与品牌视觉生成器。

可重复运行：python3 tools/gen_icon.py
依赖：numpy（必须）、ImageMagick `magick`（可选，仅用于 TV banner 中文文字层；
      缺失时自动退回几何字标，脚本仍完整产出全部图标）。

设计令牌唯一来源：docs/design-tokens.json / docs/DESIGN-SYSTEM.md §3.1
（brand 色阶 = 浅色 primary #c83933 / hover #af2a27，
  深色 primary #f96a5d / hover #ff8370，不引入任何新主色）。

记号（Seed Mark）：圆角超椭圆「果实」底 + 圆角「果核」播放三角。
  - 浅色变体：底 #c83933→#af2a27 垂直渐变，记号 #fdf9f8（primary-foreground）
  - 深色变体：底 #f96a5d→#ff8370 垂直渐变，记号 #1b0806（dark on-primary）

产出（相对仓库根）：
  安卓  res/mipmap-*/ic_launcher.png            48/72/96/144/192
        res/drawable/app_icon.xml               API<26 全出血矢量（重写上游 guoapp 图）
        res/drawable/app_icon_foreground.xml    自适应前景矢量（66dp 安全区）
        res/drawable-v26/app_icon.xml           自适应图标（背景色+前景，含 monochrome）
        res/values/ic_launcher_background.xml   自适应背景色 #C83933
        res/drawable-xhdpi/tv_banner*.png       640x360 Android TV 横幅（两份同图）
  桌面  apps/desktop/resources/icon.png         512x512（浅色变体，随包分发）
        apps/desktop/resources/icon-dark.png    512x512（深色变体，备用/文档）
        apps/desktop/build/icon.png|ico|icns    electron-builder buildResources
  文档  tools/icon_preview.png                  全尺寸目检拼板（QA 用）

约束：不修改任何 Dart/TS 代码；所有像素由本脚本确定性生成（无随机、无外部图）。
"""

from __future__ import annotations

import os
import struct
import subprocess
import zlib
from pathlib import Path

import numpy as np

REPO = Path(__file__).resolve().parent.parent

# ---------------------------------------------------------------- 令牌（§3.1）
# 全部取自 docs/design-tokens.json 的权威 HEX；禁止引入表外主色。
TOKENS = {
    "primary_light": "#C83933",   # 浅色 primary（brand-600 档）
    "primary_hover_light": "#AF2A27",
    "primary_dark": "#F96A5D",    # 深色 primary（brand-500 档）
    "primary_hover_dark": "#FF8370",
    "on_primary_light": "#FDF9F8",  # 浅色 primary-foreground（记号用白）
    "on_primary_dark": "#1B0806",   # 深色 on-primary（记号翻深）
    "background_dark": "#111213",
    "sidebar_dark": "#17181B",
    "fg_dark": "#EFEDE8",
    "fg_muted_dark": "#AAA59D",
    "primary_subtle_dark": "#512824",
    "stage": "#090A0C",
}


def hex_rgb(h: str) -> tuple[float, float, float]:
    h = h.lstrip("#")
    return tuple(int(h[i : i + 2], 16) / 255.0 for i in (0, 2, 4))  # type: ignore


# ---------------------------------------------------------------- 几何工具
def supersample_coords(size: int, k: int):
    """返回 k 倍超采样归一化坐标 x,y ∈ [-1,1]（画布中心为原点）。"""
    g = (np.arange(size * k, dtype=np.float64) + 0.5) / k - size / 2.0
    X, Y = np.meshgrid(g, g)  # 行=Y
    return X / (size / 2.0), Y / (size / 2.0), X, Y


def squircle_mask(X, Y, n: float = 5.0, a: float = 0.98) -> np.ndarray:
    """超椭圆 |x/a|^n + |y/a|^n ≤ 1（iOS 连续圆角观感）。"""
    return (np.abs(X / a) ** n + np.abs(Y / a) ** n) <= 1.0


def rect_mask(Xn, Yn, x0, y0, x1, y1, Xabs_scale, Yabs_scale) -> np.ndarray:
    x0n, x1n = x0 / Xabs_scale, x1 / Xabs_scale
    y0n, y1n = y0 / Yabs_scale, y1 / Yabs_scale
    return (Xn >= x0n) & (Xn <= x1n) & (Yn >= y0n) & (Yn <= y1n)


def convex_poly_sd_field(X, Y, pts) -> np.ndarray:
    """精确凸多边形 SDF（逐边线段距离 + 内外判定，绕向无关）。
    负值在内；用 sd <= -r 内缩即可得到真正的圆角。"""
    pts = [np.asarray(p, dtype=np.float64) for p in pts]
    area2 = sum(
        pts[i][0] * pts[(i + 1) % len(pts)][1] - pts[(i + 1) % len(pts)][0] * pts[i][1]
        for i in range(len(pts))
    )
    if area2 < 0:
        pts = pts[::-1]
    n = len(pts)
    d = np.full(X.shape, np.inf)
    inside = np.ones(X.shape, dtype=bool)
    for i in range(n):
        p0, p1 = pts[i], pts[(i + 1) % n]
        ex, ey = p1[0] - p0[0], p1[1] - p0[1]
        wx, wy = X - p0[0], Y - p0[1]
        t = np.clip((wx * ex + wy * ey) / max(ex * ex + ey * ey, 1e-18), 0.0, 1.0)
        d = np.minimum(d, np.hypot(wx - t * ex, wy - t * ey))
        cross = ex * wy - ey * wx
        inside &= cross >= 0
    return np.where(inside, -d, d)


def rounded_seed_mask(X, Y, tri, r: float) -> np.ndarray:
    """圆角「果核」播放三角：凸多边形 SDF 内缩 r。tri 为像素坐标三点（逆时针）。"""
    return convex_poly_sd_field(X, Y, tri) <= -r


def downsample(mask: np.ndarray, k: int) -> np.ndarray:
    s = mask.shape[0] // k
    return mask.reshape(s, k, s, k).mean(axis=(1, 3))


def paint(base: np.ndarray, mask: np.ndarray, color, full_alpha=None):
    """straight-alpha 叠加一层（base 为 HxWx4 float RGBA）。"""
    a = downsample(mask, K) if mask.shape[0] != base.shape[0] else mask
    if full_alpha is not None:
        a = np.clip(full_alpha, 0, 1)
    a = a[..., None]
    src_rgb = np.array(color, dtype=np.float64)[None, None, :]
    src_a = a
    dst_a = base[..., 3:4]
    out_a = src_a + dst_a * (1 - src_a)
    out_rgb = (src_rgb * src_a + base[..., :3] * dst_a * (1 - src_a)) / np.maximum(out_a, 1e-9)
    base[..., :3] = out_rgb
    base[..., 3:4] = out_a


def gradient_field(H, W, c_top, c_bottom):
    t = (np.arange(H, dtype=np.float64) + 0.5) / H
    lut = np.array(c_top)[None, :] * (1 - t)[:, None] + np.array(c_bottom)[None, :] * t[:, None]
    return lut[:, None, :]  # Hx1x3


K = 4  # 全局超采样倍率（1024 时自动降为 2）


def render_tile(size: int, dark: bool) -> np.ndarray:
    """Seed Mark 方形图标：squircle 品牌渐变底 + 果核播放三角。返回 HxWx4 uint8。"""
    global K
    K = 2 if size >= 800 else (3 if size >= 256 else 4)
    X, Y, Xa, Ya = supersample_coords(size, K)
    scale = size / 2.0

    bg = squircle_mask(X, Y)
    base = np.zeros((size * K, size * K, 4))
    base[..., :3] = gradient_field(size * K, size * K,
                                   hex_rgb(TOKENS["primary_dark" if dark else "primary_light"]),
                                   hex_rgb(TOKENS["primary_hover_dark" if dark else "primary_hover_light"]))
    base[..., 3] = bg

    # 果核三角：参数 = 视觉尺寸（内缩 r 之后的落点），名义顶点按侵蚀量补偿
    r = size * 0.052          # 圆角半径（果核感的关键）
    hv, wv, av = 0.225, 0.185, 0.215          # 视觉半高 / 视觉半宽 / 视觉顶点距中心
    h, w = hv + r, wv + r                     # 左右竖边与上下边各内缩 r
    apex = av + r / np.sin(np.arctan(hv / (av + wv))) - r  # 顶点沿角平分线侵蚀补偿
    cx, cy = size / 2.0 + size * 0.025, size / 2.0
    tri = [(cx - w * size, cy - h * size), (cx - w * size, cy + h * size), (cx + apex * size, cy)]
    tri_n = [((px - size / 2) / scale, (py - size / 2) / scale) for px, py in tri]
    fg = rounded_seed_mask(X, Y, tri_n, r / scale)
    paint(base, fg, hex_rgb(TOKENS["on_primary_dark" if dark else "on_primary_light"]))

    rgba = base.reshape(size, K, size, K, 4).mean(axis=(1, 3))
    return np.clip(rgba * 255 + 0.5, 0, 255).astype(np.uint8)


def render_tile_mac(size: int, dark: bool, content_ratio: float = 0.824) -> np.ndarray:
    """macOS Big Sur 规格：形状占 82.4%，四周留白。"""
    tile = render_tile(int(size * content_ratio), dark)
    canvas = np.zeros((size, size, 4), dtype=np.float64)
    off = (size - tile.shape[0]) // 2
    canvas[off : off + tile.shape[0], off : off + tile.shape[0]] = tile.astype(np.float64)
    return np.clip(canvas + 0.5, 0, 255).astype(np.uint8)


# ---------------------------------------------------------------- PNG/ICO/ICNS
def png_write(path: Path, rgba: np.ndarray):
    h, w = rgba.shape[:2]
    raw = b"".join(b"\x00" + rgba[y].tobytes() for y in range(h))

    def chunk(tag: bytes, data: bytes) -> bytes:
        c = tag + data
        return struct.pack(">I", len(data)) + c + struct.pack(">I", zlib.crc32(c))

    png = (b"\x89PNG\r\n\x1a\n"
           + chunk(b"IHDR", struct.pack(">IIBBBBB", w, h, 8, 6, 0, 0, 0))
           + chunk(b"IDAT", zlib.compress(raw, 9))
           + chunk(b"IEND", b""))
    path.write_bytes(png)


def ico_write(path: Path, sizes_and_imgs):
    """sizes_and_imgs: [(size, rgba)]。≤64 走 BMP DIB，256 走 PNG 内嵌。"""
    blobs, entries = [], []
    for s, img in sorted(sizes_and_imgs, key=lambda t: t[0]):
        if s <= 64:
            bgra = img[..., ::-1].astype(np.uint8)
            rows = b"".join(bgra[::-1][y].tobytes() for y in range(s))  # 自下而上
            mask_stride = ((s + 31) // 32) * 4
            and_mask = b"\x00" * (mask_stride * s)
            dib = struct.pack("<IiiHHIIiiII", 40, s, s * 2, 1, 32, 0, len(rows) + len(and_mask),
                              0, 0, 0, 0) + rows + and_mask
            entries.append((s, 0, len(dib)))
            blobs.append(dib)
        else:
            import io as _io
            tmp = Path("/tmp/_hg_icon_256.png")
            png_write(tmp, img)
            data = tmp.read_bytes()
            entries.append((s, 1, len(data)))
            blobs.append(data)
    out = struct.pack("<HHH", 0, 1, len(entries))
    offset = 6 + 16 * len(entries)
    for (s, isp, ln) in entries:
        out += struct.pack("<BBBBHHII", s % 256, s % 256, 0, 0, 1, 32, ln, offset)
        offset += ln
    path.write_bytes(out + b"".join(blobs))


def icns_write(path: Path, img_map: dict):
    types = [("icp4", 16), ("icp5", 32), ("ic11", 32), ("ic12", 64),
             ("ic07", 128), ("ic13", 256), ("ic08", 256), ("ic14", 512),
             ("ic09", 512), ("ic10", 1024)]
    body = b""
    seen = set()
    for t, s in types:
        if s in img_map and t not in seen:
            seen.add(t)
            tmp = Path(f"/tmp/_hg_icns_{t}.png")
            png_write(tmp, img_map[s])
            data = tmp.read_bytes()
            body += t.encode() + struct.pack(">I", len(data) + 8) + data
    path.write_bytes(b"icns" + struct.pack(">I", len(body) + 8) + body)


# ---------------------------------------------------------------- 文字层
def magick_text(text: str, pointsize: int, color: str, bold=False):
    """用 ImageMagick + 文泉驿渲染文字，返回 (rgba uint8 HxWx4) 或 None。"""
    font = "/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc"
    if not Path(font).exists():
        return None
    try:
        info = subprocess.run(
            ["magick", "-background", "none", "-font", font, "-pointsize", str(pointsize),
             "-fill", color, "label:" + text, "-format", "%w %h", "info:"],
            capture_output=True, check=True, text=True).stdout.split()
        w, h = int(info[0]), int(info[1])
        raw = subprocess.run(
            ["magick", "-background", "none", "-font", font, "-pointsize", str(pointsize),
             "-fill", color, "label:" + text, "-depth", "8", "rgba:-"],
            capture_output=True, check=True).stdout
        img = np.frombuffer(raw, dtype=np.uint8).reshape(h, w, 4).astype(np.float64) / 255.0
        if bold:  # 双描伪粗
            b = np.zeros_like(img)
            b[1:, :] = img[:-1, :]
            m = np.maximum(img[..., 3], b[..., 3])[..., None]
            rgb = np.where(m > 0, (img[..., :3] + b[..., :3]) / np.maximum(m, 1e-9), 0)
            img = np.concatenate([rgb, m], axis=-1)
        return img
    except Exception:
        return None


def paste(dst: np.ndarray, layer: np.ndarray, x: int, y: int):
    h, w = layer.shape[:2]
    region = dst[y : y + h, x : x + w]
    sa = layer[..., 3:4]
    da = region[..., 3:4]
    oa = sa + da * (1 - sa)
    rgb = (layer[..., :3] * sa + region[..., :3] * da * (1 - sa)) / np.maximum(oa, 1e-9)
    region[..., :3] = rgb
    region[..., 3:4] = oa


GEOM_FONT = {"N", "E", "X", "T", " "}


def geom_word(word: str, height: float, color) -> np.ndarray:
    """无 magick 时的几何字标 N E X T（凸多边形笔画的朴素实现）。"""
    stroke = height * 0.18
    adv = height * 0.78
    W = int(adv * len(word) + 8)
    H = int(height + 8)
    canvas = np.zeros((H, W, 4))
    ox = 4.0
    for ch in word:
        if ch == " ":
            ox += adv * 0.6
            continue
        x0, x1 = ox, ox + adv - stroke
        y0, y1 = 4.0, 4.0 + height
        m = height * 0.05
        polys = {
            "N": [[(x0, y1), (x0 + stroke, y1), (x1, y0 + stroke), (x1, y0), (x1 - stroke, y0), (x0, y1 - stroke)]],
            "E": [[(x0, y0), (x1, y0), (x1, y0 + stroke), (x0 + stroke, y0 + stroke),
                   (x0 + stroke, (y0 + y1) / 2 - stroke / 2), (x0 + stroke * 1.6, (y0 + y1) / 2 - stroke / 2),
                   (x0 + stroke * 1.6, (y0 + y1) / 2 + stroke / 2), (x0 + stroke, (y0 + y1) / 2 + stroke / 2),
                   (x0 + stroke, y1 - stroke), (x1, y1 - stroke), (x1, y1), (x0, y1)]],
            "X": [[(x0, y0), (x0 + stroke, y0), (x1, y1), (x1 - stroke, y1)],
                  [(x1, y0), (x1, y0 + stroke), (x0 + stroke, y1), (x0, y1)]],
            "T": [[(x0, y0), (x1, y0), (x1, y0 + stroke), (x0 + (x1 - x0 + stroke) / 2 + stroke / 2, y0 + stroke),
                   (x0 + (x1 - x0 + stroke) / 2 + stroke / 2, y1), (x0 + (x1 - x0 + stroke) / 2 - stroke / 2, y1),
                   (x0 + (x1 - x0 + stroke) / 2 - stroke / 2, y0 + stroke), (x0, y0 + stroke)]],
        }.get(ch, [])
        for poly in polys:
            pts = [(p[0] * 2, p[1] * 2) for p in poly]
            g = (np.arange(H * 2, dtype=np.float64) + 0.5) / 2.0
            GX, GY = np.meshgrid(g, g)
            mask = (convex_poly_sd_field(GX, GY, pts) <= 0).astype(np.float64)
            small = downsample(mask, 2)
            paint(canvas, small, color)
        ox += adv
    return np.clip(canvas * 255 + 0.5, 0, 255).astype(np.uint8)


# ---------------------------------------------------------------- 安卓矢量
def seed_path(tri, r, prec=2):
    """圆角三角的 SVG pathData（每角用二次贝塞尔）。逆时针 tri。"""
    import math
    pts = [np.array(p, dtype=np.float64) for p in tri]
    d = []
    # 从第一条边的内缩点出发
    entry = None
    for i in range(3):
        p0, p1, p2 = pts[i], pts[(i + 1) % 3], pts[(i + 2) % 3]
        v1 = p1 - p0
        v1 /= np.linalg.norm(v1)
        v2 = p2 - p1
        v2 /= np.linalg.norm(v2)
        t1 = p1 - v1 * r
        t2 = p1 + v2 * r
        if entry is None:
            entry = t1
            d.append(f"M{t1[0]:.{prec}f},{t1[1]:.{prec}f}")
        else:
            d.append(f"L{t1[0]:.{prec}f},{t1[1]:.{prec}f}")
        d.append(f"Q{p1[0]:.{prec}f},{p1[1]:.{prec}f} {t2[0]:.{prec}f},{t2[1]:.{prec}f}")
    d.append("Z")
    return " ".join(d)


def android_vectors():
    V = REPO / "apps/android/android/app/src/main/res"
    # 自适应前景：108dp 视口，记号落在 66dp 安全区内
    fg_tri = [(37, 35.5), (37, 72.5), (79, 54)]
    fg_r = 5.5
    fg_xml = (
        '<!-- 红果 Next 自适应图标前景（果核 Seed Mark），由 tools/gen_icon.py 生成 -->\n'
        '<vector xmlns:android="http://schemas.android.com/apk/res/android"\n'
        '    android:width="108dp" android:height="108dp"\n'
        '    android:viewportWidth="108" android:viewportHeight="108">\n'
        f'    <path android:fillColor="{TOKENS["on_primary_light"]}"\n'
        f'        android:pathData="{seed_path(fg_tri, fg_r)}"/>\n'
        "</vector>\n"
    )
    (V / "drawable/app_icon_foreground.xml").write_text(fg_xml, encoding="utf-8")

    # API<26 全出血：品牌红底 + 白果核（替换上游 guoapp 深底图形）
    legacy_tri = [(30, 28), (30, 80), (82, 54)]
    legacy_xml = (
        '<!-- 红果 Next 启动图标（API<26 全出血回退），由 tools/gen_icon.py 生成 -->\n'
        '<vector xmlns:android="http://schemas.android.com/apk/res/android"\n'
        '    android:width="108dp" android:height="108dp"\n'
        '    android:viewportWidth="108" android:viewportHeight="108">\n'
        f'    <path android:fillColor="{TOKENS["primary_light"]}" android:pathData="M0,0h108v108h-108z"/>\n'
        f'    <path android:fillColor="{TOKENS["on_primary_light"]}" android:pathData="{seed_path(legacy_tri, 6)}"/>\n'
        "</vector>\n"
    )
    (V / "drawable/app_icon.xml").write_text(legacy_xml, encoding="utf-8")

    (V / "drawable-v26").mkdir(exist_ok=True)
    (V / "drawable-v26/app_icon.xml").write_text(
        '<!-- 红果 Next 自适应图标：背景色 + 前景记号 + 单色（Android 13 主题图标），由 tools/gen_icon.py 生成 -->\n'
        '<adaptive-icon xmlns:android="http://schemas.android.com/apk/res/android">\n'
        '    <background android:drawable="@color/ic_launcher_background"/>\n'
        '    <foreground android:drawable="@drawable/app_icon_foreground"/>\n'
        '    <monochrome android:drawable="@drawable/app_icon_foreground"/>\n'
        "</adaptive-icon>\n",
        encoding="utf-8",
    )
    (V / "values/ic_launcher_background.xml").write_text(
        '<?xml version="1.0" encoding="utf-8"?>\n'
        '<!-- 自适应图标背景：设计系统 §3.1 浅色 primary，由 tools/gen_icon.py 生成 -->\n'
        f'<resources>\n    <color name="ic_launcher_background">{TOKENS["primary_light"]}</color>\n</resources>\n',
        encoding="utf-8",
    )


# ---------------------------------------------------------------- TV banner
def render_banner(size=(640, 360)) -> np.ndarray:
    W, H = size
    img = np.zeros((H, W, 4))
    # 背景垂直渐变：background_dark → sidebar_dark（§3.1 深色令牌）
    img[..., :3] = gradient_field(H, W, hex_rgb(TOKENS["background_dark"]), hex_rgb(TOKENS["sidebar_dark"]))
    img[..., 3] = 1.0

    # 左侧：Seed Mark 大徽标（浅色变体，红底白核，128px，圆角贴入）
    tile = render_tile(216, dark=False).astype(np.float64) / 255.0
    ty = (H - 216) // 2
    paste(img, tile, 48, ty)

    # 右侧文字块
    tx = 320
    word = magick_text("红果 Next", 64, TOKENS["fg_dark"], bold=True)
    if word is None:
        word = geom_word("NEXT", 56, hex_rgb(TOKENS["fg_dark"]))
        word = word.astype(np.float64) / 255.0
    paste(img, word, tx, 108)

    tag = magick_text("短剧聚合 · 全源可用", 30, TOKENS["fg_muted_dark"])
    if tag is None:
        tag = geom_word("N E X T", 20, hex_rgb(TOKENS["fg_muted_dark"]))
        tag = tag.astype(np.float64) / 255.0
    paste(img, tag, tx + 4, 196)

    # 聚合动机：三张圆角「剧集卡」（primary-subtle 深色 + 白色迷你果核）
    K_ = 2
    for i in range(3):
        cw, ch = 54, 68
        cx0 = tx + i * (cw + 14)
        gx = (np.arange(cw * K_, dtype=np.float64) + 0.5) / K_
        gy = (np.arange(ch * K_, dtype=np.float64) + 0.5) / K_
        GX, GY = np.meshgrid(gx, gy)  # 形状 (ch*K_, cw*K_)，已是卡片自身像素坐标
        Xa, Ya = GX, GY
        r = 10.0
        # 圆角矩形 SDF（像素坐标）
        qx = np.maximum(np.abs(Xa - cw / 2) - (cw / 2 - r), 0)
        qy = np.maximum(np.abs(Ya - ch / 2) - (ch / 2 - r), 0)
        sd = np.sqrt(qx**2 + qy**2) + np.minimum(np.maximum(qx, qy), 0) - r
        card = sd <= 0
        base = np.zeros((ch * K_, cw * K_, 4))
        base[..., :3] = hex_rgb(TOKENS["primary_subtle_dark"])
        base[..., 3] = card
        t2 = [(cw * 0.30, ch * 0.32), (cw * 0.30, ch * 0.68), (cw * 0.74, ch * 0.5)]
        fg = convex_poly_sd_field(Xa, Ya, t2) <= -3.0
        paint(base, fg, hex_rgb(TOKENS["on_primary_light"]))
        small = base.reshape(ch, K_, cw, K_, 4).mean(axis=(1, 3))
        paste(img, np.clip(small * 255 + 0.5, 0, 255).astype(np.uint8).astype(np.float64) / 255.0,
              cx0, H - 68 - 36)
    return np.clip(img * 255 + 0.5, 0, 255).astype(np.uint8)


# ---------------------------------------------------------------- 主流程
def main():
    out = []

    # 1) 安卓 mipmap（legacy 全出血 squircle）
    android_res = REPO / "apps/android/android/app/src/main/res"
    for dpi, size in [("mdpi", 48), ("hdpi", 72), ("xhdpi", 96), ("xxhdpi", 144), ("xxxhdpi", 192)]:
        p = android_res / f"mipmap-{dpi}/ic_launcher.png"
        p.parent.mkdir(parents=True, exist_ok=True)
        png_write(p, render_tile(size, dark=False))
        out.append((p, size, size))

    # 2) TV banner 640x360（两份同图：allSources 恒 true，身份合一）
    banner = render_banner()
    for name in ("tv_banner.png", "tv_banner_all_sources.png"):
        p = android_res / "drawable-xhdpi" / name
        png_write(p, banner)
        out.append((p, 640, 360))

    # 3) 安卓矢量/自适应 XML
    android_vectors()

    # 4) 桌面端
    desktop = REPO / "apps/desktop"
    (desktop / "resources").mkdir(exist_ok=True)
    (desktop / "build").mkdir(exist_ok=True)
    icon512 = render_tile(512, dark=False)
    png_write(desktop / "resources/icon.png", icon512)
    out.append((desktop / "resources/icon.png", 512, 512))
    icon512d = render_tile(512, dark=True)
    png_write(desktop / "resources/icon-dark.png", icon512d)
    out.append((desktop / "resources/icon-dark.png", 512, 512))

    png_write(desktop / "build/icon.png", render_tile(512, dark=False))
    out.append((desktop / "build/icon.png", 512, 512))

    ico_write(desktop / "build/icon.ico",
              [(s, render_tile(s, dark=False)) for s in (16, 24, 32, 48, 64)] + [(256, render_tile(256, dark=False))])
    out.append((desktop / "build/icon.ico", "16–64+256", "ICO"))

    icns = {s: render_tile_mac(s, dark=False) for s in (16, 32, 64, 128, 256, 512, 1024)}
    icns_write(desktop / "build/icon.icns", icns)
    out.append((desktop / "build/icon.icns", "16–1024", "ICNS"))

    # 5) QA 预览拼板（浅/深两行 + banner）
    row_items = [48, 72, 96, 144, 192]
    pad = 12
    pw = max(sum(s + pad for s in row_items) + pad, 640 + pad * 2)
    banner_y = 192 + 192 + pad * 3
    ph = banner_y + 360 + pad
    sheet = np.zeros((ph, pw * 1, 4))
    sheet[..., :3] = 1.0  # 白底
    sheet[..., 3] = 1.0
    x = pad
    for s in row_items:
        t = render_tile(s, dark=False).astype(np.float64) / 255.0
        paste(sheet, t, x, pad)
        x += s + pad
    x = pad
    for s in row_items:
        t = render_tile(s, dark=True).astype(np.float64) / 255.0
        paste(sheet, t, x, 192 + pad * 2)
        x += s + pad
    paste(sheet, banner.astype(np.float64) / 255.0, pad, banner_y)
    png_write(REPO / "tools/icon_preview.png", np.clip(sheet * 255 + 0.5, 0, 255).astype(np.uint8))
    out.append((REPO / "tools/icon_preview.png", pw, ph))

    print("生成完成：")
    for p, w, h in out:
        print(f"  {p.relative_to(REPO)}  {w}x{h}")


if __name__ == "__main__":
    main()
