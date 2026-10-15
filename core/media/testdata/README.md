# core/media 的 golden fixture

这些文件是 **MP4 净化的逐字节基准真值**：`<name>.in.mp4` 是输入，`<name>.out.mp4` 是
A 的权威实现（`hongguo-monorepo/nas-backend/mp4_sanitize.py`）的输出。
`sanitize_test.go` 只读备份文件、不调用 Python，因此无 Python 的 CI 也能复现。

## 重新生成

```sh
python3 core/media/testdata/gen_golden.py          # 默认路径即是本机 A 仓位置
# 可选环境变量：HONGGUO_A_BACKEND / HONGGUO_FFMPEG / HONGGUO_KEEP_TMP
```

`gen_golden.py` 直接 `import` A 的实现（不安装、不复制），因此**权威实现换版本时
重新跑一遍就能看出差异**。

## 可复现性（重要）

- `synthetic_*` 全部由脚本内的 box 构造器生成，**字节完全确定**，任何环境都能复现。
- `real_*` 由容器里的 ffmpeg（BtbN GPL 构建）现场产出：
  `testsrc2 320x180@15fps 3s` + `sine` → libx264/aac → `cenc-aes-ctr` 加密
  （key `000102…0f`、kid `1011…1f`）→ `-decryption_key` 解密 + `-faststart`。
  编码器版本不同则产物字节不同（**golden 会变**）。因此：
  fixture 一旦提交就视为固定基准，**不要为了"让测试通过"重新生成**；
  确需更新时必须同时核对 `meta.json` 里记录的长度与 `sanitize_test.go` 的断言。

## 各组覆盖点

| fixture | 覆盖 |
|---|---|
| `synthetic_skip_all` | 9 种 skip box 一次性剔除 + stco/co64 负 delta + 偏移 clamp 到 0 |
| `synthetic_two_mdat` | 两个 mdat、moov 前的顶层 `free`、其它顶层盒原样保留 |
| `synthetic_stco_64bit` | `stco` 用 64 位尺寸头（has_64 重打包路径） |
| `synthetic_stco_truncated` | stco 条目数超载 → 提前 break → 触发 delta 重算（3 遍） |
| `synthetic_delta_zero` | delta 恰为 0 时 stco 头被多算一层（Python 的可观测行为，刻意复现） |
| `synthetic_nested_size0` | stbl 内嵌 `size=0` 盒（按"到文件末尾"解释，跨容器读取） |
| `synthetic_no_ftyp` | 无 ftyp 时的输出顺序 |
| `synthetic_top_uuid_64` | 顶层 64 位盒原样透传（其 delta 被 +8 怪癖顶成正数） |
| `synthetic_no_moov` | 无 moov：A 返回 False 且**不产出**输出文件（Go 报 `ErrNoMoov`） |
| `real_cenc_decrypted` | 真实 CENC 加密样本（mdat 在 moov 前，delta 为正，剔除 senc/saio/saiz/sgpd/sbgp） |
| `real_faststart_remux` | 解密 + faststart 产物（ADR-0004 现场证据：只剩 sgpd/sbgp 需要净化） |

`golden_index.json` 记录权威实现路径、每组的输入/输出长度与 A 侧是否成功；
`sanitize_test.go` 会比对它，防止 fixture 被悄悄换掉。
