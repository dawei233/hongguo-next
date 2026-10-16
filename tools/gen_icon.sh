#!/usr/bin/env bash
# ============================================================================
# 红果 Next（hongguo-next）应用图标与品牌视觉生成器
#
# 从 tools/icon/*.svg 源产出全部 PNG 与 Android 矢量层；幂等，可反复运行
# （全部 PNG 去除时间戳元数据块，字节级可复现）。
#
#   用法：bash tools/gen_icon.sh
#   依赖：ImageMagick 7（convert / identify / composite / montage）
#         可选：wqy-zenhei 字体（TV banner 文字层；缺失时右侧按任务要求留白）
#         可选：python3（纯标准库，仅用于打包 .icns）
#
# 记号「竖屏追剧」：9:16 竖屏短剧帧 + 圆端点播放三角（底色透出，品牌红只点亮
# "播放"这条主路径）+ 右上探出的"下一集"帧（追剧链条 / Next），整体右倾 7°
# —— 刷短剧的节奏感，构图重心落在白屏与播放键上，不对称。
#
# 色令牌（DESIGN-SYSTEM-V2 §3.1.1 brand 色阶，不引入表外色）：
#   浅色变体  背景 brand-600 #D92B26 / 记号 #FFFFFF / 下一集帧 brand-700 #B31F1C
#   深色变体  背景 brand-500 #FF7A6E / 记号 #3B0E07 / 下一集帧 brand-900 #FFE0DC
#
# 产出（相对仓库根）：
#   安卓  apps/android/android/app/src/main/res/
#         mipmap-{mdpi,hdpi,xhdpi,xxhdpi,xxxhdpi}/ic_launcher.png   48..192（浅色）
#         drawable/app_icon.xml             全出血矢量（API<26 资源基座）
#         drawable/app_icon_foreground.xml  自适应前景（内容在中心 66dp 安全圆内）
#         drawable-v26/app_icon.xml         自适应图标（背景色满幅 + 前景 + 单色）
#         values/ic_launcher_background.xml 自适应背景色 #D92B26
#         drawable-xhdpi/tv_banner{,_all_sources}.png  426x135（左图右文）
#   桌面  apps/desktop/resources/icon.png       512（浅色，随包分发）
#         apps/desktop/resources/icon-dark.png  512（深色）
#         apps/desktop/build/icon.png|ico|icns  electron-builder buildResources
#   QA    tools/icon_preview.png          全尺寸目检拼板（浅/深两行 + 裁切模拟 + banner）
#         tools/icon_legibility_check.png 48px 实际尺寸 + 8 倍最近邻放大
#
# 约束：不修改任何 Dart/TS/Go 代码；所有像素由 SVG 源经 convert 确定性产出，
#       无随机、无网络、无外部图片输入。
#       注意：tools/gen_icon.py 是上一版被否决的生成器，勿再运行。
# ============================================================================
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC="$REPO/tools/icon"
RES="$REPO/apps/android/android/app/src/main/res"
DESK="$REPO/apps/desktop"
FONT="/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

for bin in convert identify composite montage; do
  command -v "$bin" >/dev/null 2>&1 || { echo "缺少 ImageMagick $bin"; exit 1; }
done

rel() { echo "${1#"$REPO"/}"; }
SUMMARY=()
# 确定性写出：去时间戳块 + 8-bit（复跑字节一致）
PNGDET=(-strip -define png:exclude-chunk=date,time -depth 8)

# 方形图标：SVG 源 → PNG，identify 自检尺寸
gen_icon() { # <svg> <size> <out>
  local svg="$1" size="$2" out="$3" got
  mkdir -p "$(dirname "$out")"
  convert "${PNGDET[@]}" -background none "$svg" -resize "${size}x${size}" "$out"
  got="$(identify -format '%w %h' "$out")"
  if [[ "$got" != "$size $size" ]]; then
    echo "✗ 尺寸不符 $(rel "$out")：${got/ /x}（期望 ${size}x${size}）" >&2
    exit 1
  fi
  SUMMARY+=("  OK $(rel "$out")  ${got/ /x}")
}

# TV 横幅：SVG 源 → 426x135，可选文字层（红果=fg / Next=brand-500），identify 自检
gen_banner() { # <out>
  local out="$1" got w1
  mkdir -p "$(dirname "$out")"
  convert "${PNGDET[@]}" -background none "$SRC/tv-banner.svg" -resize "426x135!" "$TMP/banner_base.png"
  if [[ -f "$FONT" ]]; then
    convert "${PNGDET[@]}" -background none -font "$FONT" -pointsize 40 -fill "#EFEDE8" label:"红果" "$TMP/w1.png"
    convert "${PNGDET[@]}" -background none -font "$FONT" -pointsize 40 -fill "#FF7A6E" label:"Next" "$TMP/w2.png"
    w1="$(identify -format %w "$TMP/w1.png")"
    composite "${PNGDET[@]}" -geometry +140+42 "$TMP/w1.png" "$TMP/banner_base.png" "$TMP/banner_t1.png"
    composite "${PNGDET[@]}" -geometry "+$((140 + w1 + 8))+42" "$TMP/w2.png" "$TMP/banner_t1.png" "$TMP/banner_final.png"
    cp "$TMP/banner_final.png" "$out"
  else
    cp "$TMP/banner_base.png" "$out" # 缺字体：右侧保持留白（任务要求"右留白给文字"）
  fi
  got="$(identify -format '%w %h' "$out")"
  if [[ "$got" != "426 135" ]]; then
    echo "✗ 尺寸不符 $(rel "$out")：${got/ /x}（期望 426x135）" >&2
    exit 1
  fi
  SUMMARY+=("  OK $(rel "$out")  ${got/ /x}")
}

echo "→ 1/5 安卓 mipmap 启动图标（浅色变体）"
for pair in "mdpi 48" "hdpi 72" "xhdpi 96" "xxhdpi 144" "xxxhdpi 192"; do
  set -- $pair
  gen_icon "$SRC/app-icon-light.svg" "$2" "$RES/mipmap-$1/ic_launcher.png"
done

echo "→ 2/5 Android 矢量层（自适应 + 全出血基座）"
mkdir -p "$RES/drawable-v26"
# 前景：108dp 视口；内容落在中心 66dp 安全圆内（33% 安全边距），任意裁切形状完整
cat > "$RES/drawable/app_icon_foreground.xml" <<'XML'
<?xml version="1.0" encoding="utf-8"?>
<!-- 红果 Next 自适应图标前景「竖屏追剧」记号，由 tools/gen_icon.sh 生成。
     108dp 视口；全部内容落在中心 66dp 安全圆内（33% 安全边距），
     系统裁切为圆形/圆角方形/水滴形时记号完整。
     播放三角填背景色 brand-600 #D92B26，从白色竖屏中透出。 -->
<vector xmlns:android="http://schemas.android.com/apk/res/android"
    android:width="108dp" android:height="108dp"
    android:viewportWidth="108" android:viewportHeight="108">
    <group android:rotation="7" android:pivotX="50.14" android:pivotY="55.72">
        <path
            android:fillColor="#B31F1C"
            android:pathData="M50.71,29.55 L65.01,29.55 C68.17,29.55 70.73,32.11 70.73,35.27 L70.73,69.3 C70.73,72.46 68.17,75.02 65.01,75.02 L50.71,75.02 C47.55,75.02 44.99,72.46 44.99,69.3 L44.99,35.27 C44.99,32.11 47.55,29.55 50.71,29.55 Z"/>
        <path
            android:fillColor="#FFFFFF"
            android:pathData="M42.99,32.98 L57.29,32.98 C60.45,32.98 63.01,35.54 63.01,38.7 L63.01,72.74 C63.01,75.9 60.45,78.46 57.29,78.46 L42.99,78.46 C39.83,78.46 37.27,75.9 37.27,72.74 L37.27,38.7 C37.27,35.54 39.83,32.98 42.99,32.98 Z"/>
        <path
            android:fillColor="#D92B26"
            android:pathData="M45.28,50.57 L45.28,60.87 Q45.28,62.3 46.52,61.59 L55.48,56.43 Q56.72,55.72 55.48,55.01 L46.52,49.85 Q45.28,49.14 45.28,50.57 Z"/>
    </group>
</vector>
XML
cat > "$RES/drawable-v26/app_icon.xml" <<'XML'
<?xml version="1.0" encoding="utf-8"?>
<!-- 红果 Next 自适应图标：brand-600 背景层满幅 + 前景记号 + 单色
     （Android 13 主题图标沿用同一轮廓），由 tools/gen_icon.sh 生成。 -->
<adaptive-icon xmlns:android="http://schemas.android.com/apk/res/android">
    <background android:drawable="@color/ic_launcher_background"/>
    <foreground android:drawable="@drawable/app_icon_foreground"/>
    <monochrome android:drawable="@drawable/app_icon_foreground"/>
</adaptive-icon>
XML
cat > "$RES/drawable/app_icon.xml" <<'XML'
<?xml version="1.0" encoding="utf-8"?>
<!-- 红果 Next 启动图标全出血矢量（API&lt;26 资源基座；minSdk=26 实际不命中），
     替换上游 guoapp 图形，由 tools/gen_icon.sh 生成。 -->
<vector xmlns:android="http://schemas.android.com/apk/res/android"
    android:width="108dp" android:height="108dp"
    android:viewportWidth="108" android:viewportHeight="108">
    <path android:fillColor="#D92B26" android:pathData="M0,0h108v108h-108z"/>
    <group android:rotation="7" android:pivotX="50.14" android:pivotY="55.72">
        <path
            android:fillColor="#B31F1C"
            android:pathData="M50.71,29.55 L65.01,29.55 C68.17,29.55 70.73,32.11 70.73,35.27 L70.73,69.3 C70.73,72.46 68.17,75.02 65.01,75.02 L50.71,75.02 C47.55,75.02 44.99,72.46 44.99,69.3 L44.99,35.27 C44.99,32.11 47.55,29.55 50.71,29.55 Z"/>
        <path
            android:fillColor="#FFFFFF"
            android:pathData="M42.99,32.98 L57.29,32.98 C60.45,32.98 63.01,35.54 63.01,38.7 L63.01,72.74 C63.01,75.9 60.45,78.46 57.29,78.46 L42.99,78.46 C39.83,78.46 37.27,75.9 37.27,72.74 L37.27,38.7 C37.27,35.54 39.83,32.98 42.99,32.98 Z"/>
        <path
            android:fillColor="#D92B26"
            android:pathData="M45.28,50.57 L45.28,60.87 Q45.28,62.3 46.52,61.59 L55.48,56.43 Q56.72,55.72 55.48,55.01 L46.52,49.85 Q45.28,49.14 45.28,50.57 Z"/>
    </group>
</vector>
XML
cat > "$RES/values/ic_launcher_background.xml" <<'XML'
<?xml version="1.0" encoding="utf-8"?>
<!-- 自适应图标背景层：DESIGN-SYSTEM-V2 §3.1.1 brand-600 #D92B26（浅色主色），
     由 tools/gen_icon.sh 生成。 -->
<resources>
    <color name="ic_launcher_background">#D92B26</color>
</resources>
XML
SUMMARY+=("  OK $(rel "$RES/drawable/app_icon_foreground.xml")  XML")
SUMMARY+=("  OK $(rel "$RES/drawable-v26/app_icon.xml")  XML")
SUMMARY+=("  OK $(rel "$RES/drawable/app_icon.xml")  XML")
SUMMARY+=("  OK $(rel "$RES/values/ic_launcher_background.xml")  XML")

echo "→ 3/5 TV 横幅（426x135，两份同图：allSources 与默认版身份合一）"
gen_banner "$RES/drawable-xhdpi/tv_banner.png"
gen_banner "$RES/drawable-xhdpi/tv_banner_all_sources.png"

echo "→ 4/5 桌面端（浅/深两套 + electron-builder buildResources）"
gen_icon "$SRC/app-icon-light.svg" 512 "$DESK/resources/icon.png"
gen_icon "$SRC/app-icon-dark.svg" 512 "$DESK/resources/icon-dark.png"
mkdir -p "$DESK/build"
gen_icon "$SRC/app-icon-light.svg" 512 "$DESK/build/icon.png"
convert "${PNGDET[@]}" -background none "$DESK/resources/icon.png" \
  -define icon:auto-resize=16,24,32,48,64,128,256 "$DESK/build/icon.ico"
SUMMARY+=("  OK $(rel "$DESK/build/icon.ico")  16-256 ICO")
if command -v python3 >/dev/null 2>&1; then
  for s in 32 64 128 256 512 1024; do
    convert "${PNGDET[@]}" -background none "$SRC/app-icon-light.svg" -resize "${s}x${s}" "$TMP/icns_$s.png"
  done
  python3 - "$TMP" "$DESK/build/icon.icns" <<'PY'
"""纯标准库 ICNS 打包：PNG 容器（macOS 10.7+ 支持 PNG 数据块）。"""
import struct, sys
from pathlib import Path
tmp, out = Path(sys.argv[1]), Path(sys.argv[2])
types = [("ic11", 32), ("ic12", 64), ("ic07", 128),
         ("ic08", 256), ("ic09", 512), ("ic10", 1024)]
body = b"".join(
    t.encode() + struct.pack(">I", len((tmp / f"icns_{s}.png").read_bytes()) + 8)
    + (tmp / f"icns_{s}.png").read_bytes()
    for t, s in types
)
out.write_bytes(b"icns" + struct.pack(">I", len(body) + 8) + body)
PY
  SUMMARY+=("  OK $(rel "$DESK/build/icon.icns")  32-1024 ICNS")
fi

echo "→ 5/5 QA 预览拼板与 48px 可辨识性自查"
for s in 48 72 96 144 192; do
  convert "${PNGDET[@]}" -background none "$SRC/app-icon-dark.svg" -resize "${s}x${s}" "$TMP/dark_$s.png"
done
# 48px 圆形裁切模拟（自适应图标可见区）
convert -size 48x48 xc:none -fill white -draw "circle 24,24 24,2" "$TMP/mask48.png"
convert "${PNGDET[@]}" "$RES/mipmap-mdpi/ic_launcher.png" "$TMP/mask48.png" -compose DstIn -composite "$TMP/masked48.png"
montage "${PNGDET[@]}" "$RES/mipmap-mdpi/ic_launcher.png" "$RES/mipmap-hdpi/ic_launcher.png" \
  "$RES/mipmap-xhdpi/ic_launcher.png" "$RES/mipmap-xxhdpi/ic_launcher.png" \
  "$RES/mipmap-xxxhdpi/ic_launcher.png" \
  -tile 5x1 -geometry +12+12 -background "#F1EEEA" "$TMP/row1.png"
montage "${PNGDET[@]}" "$TMP/dark_48.png" "$TMP/dark_72.png" "$TMP/dark_96.png" "$TMP/dark_144.png" "$TMP/dark_192.png" \
  -tile 5x1 -geometry +12+12 -background "#141418" "$TMP/row2.png"
montage "${PNGDET[@]}" "$TMP/masked48.png" "$RES/drawable-xhdpi/tv_banner.png" \
  -tile 2x1 -geometry +12+12 -background "#F1EEEA" "$TMP/row3.png"
maxw=0
for f in row1 row2 row3; do
  w="$(identify -format %w "$TMP/$f.png")"
  if (( w > maxw )); then maxw=$w; fi
done
for f in row1 row2 row3; do
  convert "${PNGDET[@]}" "$TMP/$f.png" -background "#F1EEEA" -gravity center -extent "${maxw}x" "$TMP/${f}_n.png"
done
convert "${PNGDET[@]}" "$TMP/row1_n.png" "$TMP/row2_n.png" "$TMP/row3_n.png" -append "$REPO/tools/icon_preview.png"
SUMMARY+=("  OK $(rel "$REPO/tools/icon_preview.png")  $(identify -format '%wx%h' "$REPO/tools/icon_preview.png")")

# 48px 实际尺寸 + 8 倍最近邻放大（像素级自查用）
convert "${PNGDET[@]}" "$RES/mipmap-mdpi/ic_launcher.png" -sample 384x384 "$TMP/l48big.png"
convert "${PNGDET[@]}" "$TMP/dark_48.png" -sample 384x384 "$TMP/d48big.png"
montage "${PNGDET[@]}" "$RES/mipmap-mdpi/ic_launcher.png" "$TMP/l48big.png" "$TMP/d48big.png" \
  -tile 3x1 -geometry +10+10 -background "#F1EEEA" "$REPO/tools/icon_legibility_check.png"
SUMMARY+=("  OK $(rel "$REPO/tools/icon_legibility_check.png")  $(identify -format '%wx%h' "$REPO/tools/icon_legibility_check.png")")

echo
echo "生成完成（identify 实测尺寸）："
for line in "${SUMMARY[@]}"; do echo "$line"; done
