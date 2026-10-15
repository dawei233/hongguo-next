#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""生成 core/media 的 MP4 净化 golden fixture（离线工具，不参与 go test）。

权威实现是 A `hongguo-monorepo/nas-backend/mp4_sanitize.py`（PC 端实测可用，ARCHITECTURE §3.1）。
本脚本**逐字节**记录它的输出，作为 Go 转写（core/media/sanitize.go）的唯一基准真值：
Go 侧 `sanitize_test.go` 只读 testdata/ 下已落盘的 fixture，不在测试期调用 Python。

用法（默认路径即为本机布局）：

    GOMODCACHE=… GOCACHE=… python3 core/media/testdata/gen_golden.py

环境变量：
    HONGGUO_A_BACKEND  A 仓 nas-backend 目录（默认 /workspace/红果/hongguo-monorepo/nas-backend）
    HONGGUO_FFMPEG     ffmpeg 可执行文件（默认 PATH 上的 ffmpeg；缺失时跳过 real_* 组）
    HONGGUO_KEEP_TMP   非空则保留 real_* 的中间产物，便于人工复核

产物（每组合一个 .meta.json 记录期望行为与实测事实）：
    <name>.in.mp4    输入
    <name>.out.mp4   A 的 Python 实现输出（期望字节；no_moov 组没有该文件）
    <name>.meta.json {"name","note","python_ok","in_bytes","out_bytes"}
"""

import importlib.util
import json
import os
import shutil
import struct
import subprocess
import sys
import tempfile
from pathlib import Path

TESTDATA = Path(__file__).resolve().parent
A_BACKEND = Path(os.environ.get("HONGGUO_A_BACKEND", "/workspace/红果/hongguo-monorepo/nas-backend"))
FFMPEG = os.environ.get("HONGGUO_FFMPEG", "ffmpeg")
KEEP_TMP = bool(os.environ.get("HONGGUO_KEEP_TMP"))

CENC_KEY = "000102030405060708090a0b0c0d0e0f"
CENC_KID = "101112131415161718191a1b1c1d1e1f"


def load_python_impl():
    """按文件路径载入 A 的 mp4_sanitize.py（不安装、不复制）。"""
    source = A_BACKEND / "mp4_sanitize.py"
    if not source.is_file():
        sys.exit(f"找不到 A 的权威实现: {source}（用 HONGGUO_A_BACKEND 指定 nas-backend 目录）")
    spec = importlib.util.spec_from_file_location("a_mp4_sanitize", source)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module, source


# ── 极简 box 构造器（只造结构，不追求语义合法） ────────────────────────────

def btype(name):
    assert len(name) == 4
    return name.encode("latin-1")


def box(name, payload=b""):
    return struct.pack(">I", 8 + len(payload)) + btype(name) + payload


def box64(name, payload=b""):
    """64 位尺寸头：size=1 + 8 字节真实尺寸（含 16 字节头）。"""
    return struct.pack(">I", 1) + btype(name) + struct.pack(">Q", 16 + len(payload)) + payload


def box_zero(name, payload=b""):
    """size=0：表示「直到文件末尾」，按 A 的实现即 len(buf)-offset。"""
    return struct.pack(">I", 0) + btype(name) + payload


def full(payload=b""):
    return b"\x00\x00\x00\x00" + payload


def stco(offsets):
    return box("stco", full(struct.pack(">I", len(offsets)) + b"".join(struct.pack(">I", o) for o in offsets)))


def co64(offsets):
    return box("co64", full(struct.pack(">I", len(offsets)) + b"".join(struct.pack(">Q", o) for o in offsets)))


def raw_payload(n, seed=0x41):
    return bytes(((seed + i) % 251) for i in range(n))


def ftyp():
    return box("ftyp", b"isom" + struct.pack(">I", 512) + b"isomiso2avc1mp41")


# A 的 SKIP_IN_STBL 全集（9 个），fixture 必须逐个覆盖。
SKIP_TYPES = ["saio", "saiz", "senc", "sgpd", "sbgp", "stsl", "sdpd", "subs", "stdp"]

SKIPPED_ALL = b"".join(box(name, raw_payload(8 + 3 * i, 0x60 + i)) for i, name in enumerate(SKIP_TYPES))


def moov_of(stbls, extra_boxes=b""):
    """把若干 stbl 装进 trak>mdia>minf>moov，并附一个假的 mvhd。"""
    tracks = b""
    for stbl in stbls:
        minf = box("minf", box("vmhd", full(struct.pack(">HHHH", 0, 0, 0, 0))) + box("stbl", stbl))
        mdia = box("mdia", box("mdhd", raw_payload(24, 0x10)) + box("hdlr", raw_payload(17, 0x20)) + minf)
        tracks += box("trak", box("tkhd", raw_payload(84, 0x30)) + mdia)
    return box("moov", box("mvhd", raw_payload(100, 0x01)) + extra_boxes + tracks)


def stbl_common(stco_box):
    return box("stsd", full(struct.pack(">I", 1) + box("avc1", raw_payload(60, 0x70)))) + \
        box("stts", full(struct.pack(">I", 0))) + \
        box("stsc", full(struct.pack(">I", 0))) + \
        box("stsz", full(struct.pack(">II", 0, 0))) + \
        stco_box


def synthetic_cases():
    """返回 [(name, note, data|None)]；data=None 表示该用例不写 fixture（由脚本内联断言）。"""
    cases = []

    # 1) 全量 skip + 负 delta + 偏移 clamp 到 0（视频 trak 与音频 trak 各一个 stbl）
    stbl_video = stbl_common(stco([0, 10, 4096, 8192])) + SKIPPED_ALL
    stbl_audio = stbl_common(co64([12, 2048, 4096])) + box("sgpd", raw_payload(26)) + box("sbgp", raw_payload(28))
    data = ftyp() + moov_of([stbl_video, stbl_audio]) + box("free") + box("mdat", raw_payload(96, 0x5a))
    cases.append(("synthetic_skip_all", "剥 9 种 box + stco/co64 负 delta + 偏移 clamp 到 0", data))

    # 2) 两个 mdat + moov 前的顶层 free（输出顺序：ftyp → 其他顶层 → 新 moov → 全部 mdat）
    stbl = stbl_common(stco([0, 64, 128])) + box("senc", raw_payload(24))
    data = (ftyp() + box("free", raw_payload(16, 0x44)) + moov_of([stbl]) +
            box("mdat", raw_payload(80, 0x11)) + box("udta", raw_payload(12, 0x22)) +
            box("mdat", raw_payload(48, 0x33)))
    cases.append(("synthetic_two_mdat", "两个 mdat + moov 前顶层 free + 非 moov/mdat 顶层原样保留", data))

    # 3) 64 位尺寸头的 stco（size=1 分支）：走 has_64 重打包路径
    data = ftyp() + moov_of([stbl_common(box64("stco", full(struct.pack(">I", 3) + struct.pack(">III", 4, 40, 400))))]) + box("mdat", raw_payload(64, 0x19))
    cases.append(("synthetic_stco_64bit", "stco 用 64 位尺寸头（has_64 重打包路径）", data))

    # 4) count 大于 payload 可容纳的条目数 → 提前 break → 首轮估算与实际不符 → 触发重算分支
    truncated = full(struct.pack(">I", 9) + struct.pack(">I", 8) + b"".join(struct.pack(">I", 100 + i) for i in range(3)))
    data = ftyp() + moov_of([stbl_common(box("stco", truncated)) + box("senc", raw_payload(40))]) + box("mdat", raw_payload(64, 0x21))
    cases.append(("synthetic_stco_truncated", "stco count 超出 payload → 提前 break → 触发 delta 重算", data))

    # 5) delta 恰为 0：A 的实现此时给 stco 多算一层 8 字节头（非 delta 重写路径），
    #    长度变化刚好抵消被剔除的 8 字节 stsl —— 逐字节复现该行为是刻意的（见 sanitize.go 注释）。
    data = ftyp() + moov_of([stbl_common(stco([16, 256, 65536])) + box("stsl")]) + box("mdat", raw_payload(64, 0x31))
    cases.append(("synthetic_delta_zero", "delta 恰为 0 时的 stco 头重复计数行为（字节级复现）", data))

    # 6) stbl 内嵌一个 size=0 的 box（按 A 的实现等价于「到文件末尾」）
    data = ftyp() + moov_of([stbl_common(stco([32, 1024])) + box_zero("senc", raw_payload(20))]) + box("mdat", raw_payload(64, 0x41))
    cases.append(("synthetic_nested_size0", "stbl 内 size=0 box：按 len(buf)-offset 解释", data))

    # 7) 无 ftyp（ftyp 缺失时输出不以 ftyp 开头）
    stbl = stbl_common(stco([8, 512])) + box("saiz", raw_payload(20))
    data = moov_of([stbl]) + box("mdat", raw_payload(72, 0x51))
    cases.append(("synthetic_no_ftyp", "无 ftyp：输出直接从其他顶层 box 开始", data))

    # 8) 顶层 64 位盒（既非 moov/mdat/ftyp）：原样透传
    stbl = stbl_common(co64([16, 4096]))
    data = ftyp() + box64("uuid", raw_payload(20, 0x61)) + moov_of([stbl]) + box("mdat", raw_payload(64, 0x71))
    cases.append(("synthetic_top_uuid_64", "顶层 64 位 uuid 盒原样透传 + co64 重写", data))

    # 9) 无 moov：A 的实现返回 False 且不写输出
    data = ftyp() + box("mdat", raw_payload(32, 0x81)) + box("free")
    cases.append(("synthetic_no_moov", "无 moov：A 返回 False（不产出输出文件）", data))

    return cases


def run_python(module, data, name):
    """用 A 的实现净化 data，返回 (ok, out_bytes|None)。"""
    with tempfile.TemporaryDirectory() as tmp:
        src = Path(tmp) / f"{name}.in.mp4"
        dst = Path(tmp) / f"{name}.out.mp4"
        src.write_bytes(data)
        ok = bool(module.sanitize_mp4(str(src), str(dst)))
        out = dst.read_bytes() if dst.is_file() else None
    return ok, out


def write_case(name, note, data, module):
    ok, out = run_python(module, data, name)
    (TESTDATA / f"{name}.in.mp4").write_bytes(data)
    if out is not None:
        (TESTDATA / f"{name}.out.mp4").write_bytes(out)
    meta = {"name": name, "note": note, "python_ok": ok, "in_bytes": len(data),
            "out_bytes": len(out) if out is not None else None}
    (TESTDATA / f"{name}.meta.json").write_text(json.dumps(meta, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"  {name}: in={len(data)} out={None if out is None else len(out)} ok={ok}")
    return meta


def run(cmd):
    return subprocess.run(cmd, capture_output=True, check=False)


def real_cases(module):
    """真实样本：ffmpeg CENC 加密 → -decryption_key 解密 + faststart → A 的净化。

    这一组是 ADR-0004 的实测复核：faststart remux 后 senc/saio/saiz/sinf/encv/tenc
    已被 ffmpeg 去掉，但 sgpd/sbgp 会回来，必须靠 sanitize 二次净化。
    """
    if not shutil.which(FFMPEG) and not Path(FFMPEG).is_file():
        print("  ! 跳过 real_* 组：找不到 ffmpeg")
        return []
    tmp = Path(tempfile.mkdtemp(prefix="hongguo-golden-"))
    plain, enc, dec = tmp / "plain.mp4", tmp / "enc.mp4", tmp / "dec.mp4"
    steps = [
        [FFMPEG, "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=15:duration=3",
         "-f", "lavfi", "-i", "sine=frequency=440:duration=3", "-c:v", "libx264", "-profile:v", "main",
         "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", str(plain)],
        [FFMPEG, "-v", "error", "-y", "-i", str(plain), "-c", "copy", "-encryption_scheme", "cenc-aes-ctr",
         "-encryption_key", CENC_KEY, "-encryption_kid", CENC_KID, str(enc)],
        # 与 server.py 1089-1097 完全同参：解密 + 不重编码 + faststart（保留 .mp4 后缀）
        [FFMPEG, "-v", "error", "-y", "-decryption_key", CENC_KEY, "-i", str(enc), "-c", "copy",
         "-movflags", "+faststart", str(dec)],
    ]
    for step in steps:
        done = run(step)
        if done.returncode != 0:
            print(f"  ! 跳过 real_* 组：ffmpeg 失败 rc={done.returncode} {done.stderr[-200:]!r}")
            return []
    results = []
    for name, note, path in [
        ("real_cenc_decrypted", "CENC 加密样本经 -decryption_key 解密 + faststart 后净化（ADR-0004 实测复核）", enc),
        ("real_faststart_remux", "解密产物（faststart remux 让 sgpd/sbgp 回来）净化", dec),
    ]:
        data = path.read_bytes()
        results.append(write_case(name, note, data, module))
    if not KEEP_TMP:
        shutil.rmtree(tmp, ignore_errors=True)
    else:
        print(f"  中间产物保留于 {tmp}")
    return results


def main():
    module, source = load_python_impl()
    print(f"权威实现: {source}")
    metas = []
    print("合成用例：")
    for name, note, data in synthetic_cases():
        metas.append(write_case(name, note, data, module))
    print("真实用例：")
    metas.extend(real_cases(module))
    index = {"source": str(source), "cases": metas}
    (TESTDATA / "golden_index.json").write_text(json.dumps(index, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"完成：{len(metas)} 组 fixture")


if __name__ == "__main__":
    main()
