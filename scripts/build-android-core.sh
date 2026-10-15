#!/usr/bin/env bash
#
# scripts/build-android-core.sh —— 用 NDK 交叉编译 bridge/go 为 Android arm64 内核
#
# 产物：apps/android/android/app/src/main/jniLibs/arm64-v8a/libhncore_android.so
#   · buildmode=c-shared（dlopen 可加载，导出符号进 .dynsym）
#   · 2 个 C 符号 DuanjuRequest / DuanjuFree（apps/android/lib/core_bridge.dart 经 dart:ffi 查找）
#   · ELF 校验：AArch64 + DYN（shared object），缺一即失败
#
# 来源：bridge/go/main.go（cgo，import "C"）→ import 本仓 core/（纯 Go）。
# 交叉编译 cgo 到 Android 必须有 NDK（无 NDK 报 gcc_arm64.S: no such instruction）。
#
# 环境变量（均可独立覆盖）：
#   ANDROID_NDK_HOME  NDK 根目录（必填；亦接受 ANDROID_NDK_ROOT / NDK_HOME）
#   HN_NDK_VERSION    期望的 NDK 版本号，默认 28.2.13676358（= r28c，
#                     与 apps/android/android/app/build.gradle.kts 的 ndkVersion 完全一致）
#   HN_NDK_HOST       NDK prebuilt 主机目录，默认按 uname 推断（linux-x86_64 等）
#   HN_ANDROID_API    目标 API level，默认 26（= build.gradle.kts 的 minSdk 26）
#   HN_ABI_DIR        jniLibs ABI 子目录，默认 arm64-v8a
#   HN_SO_NAME        输出文件名，默认 libhncore_android.so（与 core_bridge.dart 一致）
#
# 本地复现（有 NDK 的机器）：
#   export ANDROID_NDK_HOME=~/Android/Sdk/ndk/28.2.13676358
#   bash scripts/build-android-core.sh
#
# GitHub Actions：.github/workflows/build-android.yml 的 ① 步调用本脚本。
set -euo pipefail

# ── 路径解析 ──────────────────────────────────────────────────────────────
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BRIDGE_DIR="$ROOT/bridge/go"
ABI_DIR="${HN_ABI_DIR:-arm64-v8a}"
SO_NAME="${HN_SO_NAME:-libhncore_android.so}"
JNILIBS_DIR="$ROOT/apps/android/android/app/src/main/jniLibs/$ABI_DIR"
OUT_SO="$JNILIBS_DIR/$SO_NAME"

die() { echo "ERROR: $*" >&2; exit 1; }

# ── ①a 解析并校验 NDK（版本钉死，防止拿错工具链）────────────────────────
NDK_HOME="${ANDROID_NDK_HOME:-${ANDROID_NDK_ROOT:-${NDK_HOME:-}}}"
[ -n "$NDK_HOME" ] || die "未设置 ANDROID_NDK_HOME（例：~/Android/Sdk/ndk/28.2.13676358）"
[ -d "$NDK_HOME" ] || die "NDK 目录不存在：$NDK_HOME"

HN_NDK_VERSION="${HN_NDK_VERSION:-28.2.13676358}"
ACTUAL_NDK_REVISION="$(grep -E '^Pkg.Revision\s*=\s*' "$NDK_HOME/source.properties" | head -1 | sed -E 's/^Pkg.Revision\s*=\s*//' | tr -d '[:space:]')"
[ "$ACTUAL_NDK_REVISION" = "$HN_NDK_VERSION" ] \
  || die "NDK 版本不匹配：期望 $HN_NDK_VERSION（r28c，与 build.gradle.kts 的 ndkVersion 一致），实际 $ACTUAL_NDK_REVISION（$NDK_HOME）"

case "$(uname -s)-$(uname -m)" in
  Linux-*)    DEFAULT_HOST="linux-x86_64" ;;
  Darwin-*)   DEFAULT_HOST="darwin-x86_64" ;;
  MINGW*|MSYS*|CYGWIN*) DEFAULT_HOST="windows-x86_64" ;;
  *) DEFAULT_HOST="linux-x86_64" ;;
esac
NDK_HOST="${HN_NDK_HOST:-$DEFAULT_HOST}"
PREBUILT="$NDK_HOME/toolchains/llvm/prebuilt/$NDK_HOST"
[ -d "$PREBUILT" ] || die "NDK prebuilt 目录不存在：$PREBUILT"

HN_ANDROID_API="${HN_ANDROID_API:-26}"
CC="$PREBUILT/bin/aarch64-linux-android${HN_ANDROID_API}-clang"
[ -x "$CC" ] || die "找不到 NDK clang 包装器：$CC"

# ── ①b NDK 交叉编译：cgo（CGO_ENABLED=1）→ c-shared → jniLibs ───────────
echo ">> NDK $ACTUAL_NDK_REVISION · host=$NDK_HOST · CC=$CC"
echo ">> GOOS=android GOARCH=arm64 CGO_ENABLED=1 -buildmode=c-shared"
mkdir -p "$JNILIBS_DIR"
(
  cd "$BRIDGE_DIR"
  CGO_ENABLED=1 GOOS=android GOARCH=arm64 \
    CC="$CC" \
    go build -trimpath -buildvcs=false -ldflags="-s -w" -buildmode=c-shared \
    -o "$OUT_SO" .
)

# ── ①c 产物校验：ELF 形态 + 2 个 C 符号，任一缺失即失败 ─────────────────
[ -s "$OUT_SO" ] || die "构建产物为空：$OUT_SO"
READELF="$PREBUILT/bin/llvm-readelf"
NM="$PREBUILT/bin/llvm-nm"
"$READELF" -h "$OUT_SO" | grep -E 'Machine:\s+AArch64' >/dev/null \
  || die "产物不是 AArch64 ELF（lib/core_bridge.dart 在 Android arm64 上 dlopen）"
"$READELF" -h "$OUT_SO" | grep -E 'Type:\s+DYN' >/dev/null \
  || die "产物不是 shared object（DYN）—— 需 -buildmode=c-shared"
"$NM" -D "$OUT_SO" | grep -qw DuanjuRequest \
  || die "动态符号表缺 DuanjuRequest（dart:ffi lookupFunction 查找失败）"
"$NM" -D "$OUT_SO" | grep -qw DuanjuFree \
  || die "动态符号表缺 DuanjuFree（dart:ffi lookupFunction 查找失败）"

echo ">> OK: $OUT_SO"
ls -l "$OUT_SO"
sha256sum "$OUT_SO"
