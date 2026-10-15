// 本仓新增（M3 媒体管线）。
//
// 本文件是 A `hongguo-monorepo/nas-backend/mp4_sanitize.py` 的 Go 逐函数转写
// （ARCHITECTURE §3.1 #1）。它解决的问题只有一个：CDN 产出的「CENC 加密 MP4」
// 经 ffmpeg `-decryption_key` 解密 + `+faststart` remux 之后，仍会残留
// `sgpd`/`sbgp` 等非标 box（实测，见 testdata/real_faststart_remux.meta.json），
// 严格 demuxer（ExoPlayer / Android WebView）因此报
// PARSING_CONTAINER_MALFORMED。净化器把这些 box 剔掉，并把 moov 前置导致的
// mdat 位移量写回 `stco`/`co64`。
//
// 逐条对应关系（Python 行号 → 本文件）：
//
//	SKIP_IN_STBL      (41)     → defaultSkipInStbl
//	clean_stbl        (44–118) → (*sanitizer).cleanStbl
//	clean_container   (121–210) → (*sanitizer).cleanContainer
//	sanitize_mp4      (213–293) → SanitizeBytes / SanitizeFile
//
// # 刻意保留的 Python 怪癖（改动任何一处都会让 golden fixture 逐字节断言失败）
//
//  1. **delta==0 时 stco/co64 会被多算一层 8 字节头**：`clean_stbl` 的
//     `preserved.append((b, raw))` 里 `raw` 含原 box 头，而重打包分支仍按
//     `new_size = 8 + len(raw)` 再补一个头，于是该 box 变成「头套头」且长度 +8。
//     这不是笔误而是可观测行为：`synthetic_delta_zero.*` 与 `synthetic_top_uuid_64.*`
//     两组 fixture 都依赖它（后者的 delta 正是被这 +8 顶成 +8 的）。
//  2. **首轮估算用 delta=0，且其长度会被当真**：Python 先以 delta=0 走一遍只取长度，
//     再按 `新 mdat 起点 - 旧 mdat 起点` 求 delta。由于 (1)，首轮长度往往不准，
//     于是有第三遍重算（最多重算一次，不迭代到收敛）。实测样例
//     `real_faststart_remux` 就走满了三遍（delta -38 → -54）。
//  3. **stco 条目数超载时提前 break**：`count` 声明值与 payload 实际可容纳条目数
//     不一致时，按 payload 边界截断（声明 count 保持不变），长度随之变化 ——
//     这也是触发 (2) 重算的常见原因（fixture `synthetic_stco_truncated`）。
//  4. **stsd 不递归**：`stsd` 内的 sample entry（avc1/hvc1/encv…）是编码器配置，
//     Python 明确按不透明字节原样保留。因此 §3.1 表格里提到的
//     `sinf`/`encv`/`tenc` 三个 box **不在**净化范围内（它们只可能出现在 stsd 内部），
//     真正的剔除集是 SKIP_IN_STBL 的 9 个类型。实测也如此：ffmpeg 解密产物里
//     senc/saio/saiz/sinf/encv/tenc 都已消失，只剩 sgpd/sbgp 需要净化。
//     若将来确需剔除 sinf/encv/tenc，必须改 cleanContainer 对 stsd 的处理，
//     而不是往 skip 集合里加名字（现在的实现不会遍历到 stsd 内部）。
//  5. **顶层顺序被重排**：输出固定为 ftyp → 其他顶层（原序）→ 新 moov → 全部 mdat。
//     因此 moov 原在 mdat 之后的文件（server.py 的样例就是）会被前移，
//     delta 为正。
//
// 与 Python 的唯一有意的差异：畸形输入下的失败方式。Python 在以下情形抛
// struct.error（进程内调用方按「净化失败」处理、不产出任何字节），Go 侧返回 error：
// 64 位尺寸头越界、stco/co64 负载不足 8 字节、重写后的偏移超出
// uint32/uint64 表示范围。成功路径上两者逐字节一致（golden fixture 覆盖 11 组）。
package media

import (
	"errors"
	"fmt"
	"math"
	"os"
)

// ErrNoMoov 表示输入没有 moov box。Python 的 sanitize_mp4 此时打印
// "[sanitize] 无 moov，跳过" 并返回 False（不写输出文件）。
var ErrNoMoov = errors.New("输入没有 moov box，无法净化")

const maxUint32Value = int64(0xFFFFFFFF)

// defaultSkipInStbl 是 Python SKIP_IN_STBL 的全集（9 个类型），注释照搬原文：
//
//	saio/saiz/senc —— CDN 字节伪加密标记
//	sgpd/sbgp      —— Sample Group（ExoPlayer 会因此报 PARSING_CONTAINER_MALFORMED）
//	stsl/sdpd/subs/stdp —— Sub-sample/Partial Sample 等辅助 box（兼容性差）
//
// 注意这些类型**只在 stbl 内**被剔除（cleanContainer 不会遍历 stbl 之外的同名 box，
// 也不遍历 stsd 内部）。
var defaultSkipInStbl = map[string]bool{
	"saio": true, "saiz": true, "senc": true,
	"sgpd": true, "sbgp": true,
	"stsl": true, "sdpd": true, "subs": true, "stdp": true,
}

// SanitizeOptions 是可选的调整项。零值 = 与 Python 实现行为完全一致。
type SanitizeOptions struct {
	// ExtraStblSkip 追加「在 stbl 内一并剔除」的 box 类型（例如 "cslg"）。
	// 为空时剔除集就是 defaultSkipInStbl，逐字节对齐 Python。
	// 该选项只影响 cleanStbl 的遍历范围（stbl 直接子 box），不会进入 stsd。
	ExtraStblSkip []string
	// Logf 接收与 Python 同形的诊断行；nil 表示静默（默认）。
	Logf func(format string, args ...any)
}

// SanitizeResult 是净化的产物与统计。
type SanitizeResult struct {
	// Data 是净化后的完整文件字节。
	Data []byte
	// RemovedStblBoxes 是**最后一遍**在 stbl 内剔除的 box 类型（按出现顺序，含重复），
	// 对应 Python 的 "[sanitize]   stbl 剔除: [...]"。
	RemovedStblBoxes []string
	// MoovSize 是写出的 moov box 总长度（含头）。
	MoovSize int64
	// Delta 是最终采用的 mdat 位移量（stco/co64 全部加上它）。
	// 注意 Python 打印的 delta 是重算前那个值，本字段是最终值。
	Delta int64
	// Passes 是 moov 被清理的遍数：2 = 估算 + 定稿，3 = 额外触发了一次 delta 重算。
	Passes int
}

type sanitizer struct {
	buf      []byte
	skip     map[string]bool
	logf     func(string, ...any)
	removed  []string // 当前这一遍剔除的 box 类型
	anyExtra bool
}

type preservedBox struct {
	box BoxHeader
	raw []byte // 已按需重写过偏移的负载（stco/co64）或原样字节（含头）
}

// SanitizeBytes 是 Python sanitize_mp4 的 Go 形态：输入完整文件字节，输出净化后的字节。
//
// 不修改入参 data（Python 也只在局部 bytearray 上读）。
func SanitizeBytes(data []byte, options SanitizeOptions) (SanitizeResult, error) {
	skip := make(map[string]bool, len(defaultSkipInStbl)+len(options.ExtraStblSkip))
	for name := range defaultSkipInStbl {
		skip[name] = true
	}
	for _, name := range options.ExtraStblSkip {
		skip[name] = true
	}
	s := &sanitizer{buf: data, skip: skip, logf: options.Logf}
	if s.logf == nil {
		s.logf = func(string, ...any) {}
	}

	top := Boxes(data, 0, int64(len(data)))
	moov, hasMoov := findBox(top, "moov")
	mdats := filterBoxes(top, "mdat")
	if !hasMoov {
		s.logf("[sanitize] 无 moov，跳过")
		return SanitizeResult{}, ErrNoMoov
	}
	s.logf("[sanitize] 原文件 %d 字节，顶层 box: %v", len(data), boxTypes(top))

	// 步骤 1：先按 delta=0 清理一遍，只为拿到 moov 尺寸（Python 213 行注释里的
	// 1:1 估算不准问题，就来自上面「怪癖 1」）。
	s.beginPass()
	content, err := s.cleanContainer(moov, 0)
	if err != nil {
		return SanitizeResult{}, err
	}
	moovSize := int64(8 + len(content))

	// 步骤 2：新 mdat 起点 = (moov 之前的顶层总长度) + 新 moov 尺寸。
	moved := int64(0)
	for _, box := range top {
		if box.Type == "moov" || box.Type == "mdat" {
			continue
		}
		moved = saturateAdd(moved, box.Size)
	}
	oldMdatStart := int64(0)
	if len(mdats) > 0 {
		oldMdatStart = mdats[0].Offset
	}
	delta := saturateAdd(moved, moovSize) - oldMdatStart
	s.logf("[sanitize] 新 moov 大小: %d delta: %d", moovSize, delta)

	// 步骤 3：带上 delta 重新清理（stco/co64 在这一遍才会被重写）。
	s.beginPass()
	content, err = s.cleanContainer(moov, delta)
	if err != nil {
		return SanitizeResult{}, err
	}
	passes := 2
	actual := int64(8 + len(content))
	if actual != moovSize {
		// 长度与估算不符（stco 被截断或重打包所致）→ 按实际尺寸重算一次。
		// Python 只重算一次，不迭代到收敛，这里保持一致。
		delta = saturateAdd(moved, actual) - oldMdatStart
		s.beginPass()
		content, err = s.cleanContainer(moov, delta)
		if err != nil {
			return SanitizeResult{}, err
		}
		actual = int64(8 + len(content))
		passes = 3
	}

	// 写输出：ftyp → 其他顶层（原序）→ 新 moov → 全部 mdat。
	out := make([]byte, 0, len(data))
	if ftyp, ok := findBox(top, "ftyp"); ok {
		out = append(out, ByteRange(data, ftyp)...)
	}
	for _, box := range top {
		switch box.Type {
		case "ftyp", "moov", "mdat":
			continue
		}
		out = append(out, ByteRange(data, box)...)
	}
	if actual > maxUint32Value {
		out = WriteBoxHeader(out, "moov", uint64(actual+8), true)
	} else {
		out = WriteBoxHeader(out, "moov", uint64(actual), false)
	}
	out = append(out, content...)
	for _, box := range mdats {
		out = append(out, ByteRange(data, box)...)
	}

	s.logf("[sanitize] 输出 %d 字节 (%d 字节 moov，%d 遍)", len(out), actual, passes)
	return SanitizeResult{Data: out, RemovedStblBoxes: append([]string(nil), s.removed...),
		MoovSize: actual, Delta: delta, Passes: passes}, nil
}

// SanitizeFile 读 inputPath、净化、写 outputPath（对应 Python 的 sanitize_mp4 文件形态）。
//
// 无 moov 时返回 ErrNoMoov 且**不创建** outputPath（与 Python 返回 False 一致）。
func SanitizeFile(inputPath, outputPath string, options SanitizeOptions) (SanitizeResult, error) {
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return SanitizeResult{}, err
	}
	result, err := SanitizeBytes(data, options)
	if err != nil {
		return SanitizeResult{}, err
	}
	if err := os.WriteFile(outputPath, result.Data, 0o600); err != nil {
		return SanitizeResult{}, err
	}
	return result, nil
}

func (s *sanitizer) beginPass() { s.removed = nil }

// cleanStbl 对应 Python clean_stbl：剔除 skip 类型、按 delta 重写 stco/co64 偏移。
func (s *sanitizer) cleanStbl(stbl BoxHeader, delta int64) ([]byte, error) {
	preserved := make([]preservedBox, 0, 8)
	for _, box := range Boxes(s.buf, stbl.Payload, stbl.End()) {
		if s.skip[box.Type] {
			s.removed = append(s.removed, box.Type)
			continue
		}
		if box.Type == "stco" && delta != 0 {
			payload, err := rewriteOffsets(BoxPayload(s.buf, box), delta, false)
			if err != nil {
				return nil, err
			}
			preserved = append(preserved, preservedBox{box: box, raw: payload})
			continue
		}
		if box.Type == "co64" && delta != 0 {
			payload, err := rewriteOffsets(BoxPayload(s.buf, box), delta, true)
			if err != nil {
				return nil, err
			}
			preserved = append(preserved, preservedBox{box: box, raw: payload})
			continue
		}
		preserved = append(preserved, preservedBox{box: box, raw: ByteRange(s.buf, box)})
	}
	if len(s.removed) > 0 {
		s.logf("[sanitize]   stbl 剔除: %v", s.removed)
	}

	result := make([]byte, 0, stbl.Size)
	for _, item := range preserved {
		if item.box.Type != "stco" && item.box.Type != "co64" {
			// 原样输出（保持原 size，因为内容不变）。
			result = append(result, item.raw...)
			continue
		}
		// 用新的 size 重新打包头（见文件头「怪癖 1」：delta==0 的路径里 item.raw
		// 含原头，这里的 8+len(raw) 会多算一层 —— 保留）。
		if item.box.Has64 {
			result = WriteBoxHeader(result, item.box.Type, uint64(8+len(item.raw)), true)
			result = append(result, item.raw...)
			continue
		}
		newSize := int64(8 + len(item.raw))
		if newSize > maxUint32Value {
			result = WriteBoxHeader(result, item.box.Type, uint64(newSize+8), true)
		} else {
			result = WriteBoxHeader(result, item.box.Type, uint64(newSize), false)
		}
		result = append(result, item.raw...)
	}
	return result, nil
}

// rewriteOffsets 重写 stco（32 位）或 co64（64 位）中的偏移表。
//
// 对应 Python clean_stbl 的两个 `if b['type'] == ... and delta != 0` 分支：
// 负载前 8 字节（version/flags + entry_count）原样保留，逐条 old+delta 并 clamp 到 0，
// 条目数超过负载可容纳数量时按负载边界提前结束（声明 count 不变 —— 见「怪癖 3」）。
func rewriteOffsets(payload []byte, delta int64, wide bool) ([]byte, error) {
	if len(payload) < 8 {
		// Python: struct.unpack('>I', payload[4:8]) → struct.error
		return nil, fmt.Errorf("stco/co64 负载不足 8 字节（%d 字节）", len(payload))
	}
	count := bigEndianUint32(payload[4:8])
	width := int64(4)
	if wide {
		width = 8
	}
	out := make([]byte, 0, len(payload))
	out = append(out, payload[:8]...)
	for index := uint64(0); index < uint64(count); index++ {
		// Python: if 8 + width * (i + 1) > len(payload): break
		if int64(8)+width*(int64(index)+1) > int64(len(payload)) {
			break
		}
		start := 8 + width*int64(index)
		if wide {
			value, err := shiftUnsigned(bigEndianUint64(payload[start:start+8]), delta)
			if err != nil {
				return nil, err
			}
			out = appendBigEndianUint64(out, value)
			continue
		}
		value, err := shiftUnsigned(uint64(bigEndianUint32(payload[start:start+4])), delta)
		if err != nil {
			return nil, err
		}
		if value > uint64(maxUint32Value) {
			// Python: struct.pack('>I', new_off) → struct.error
			return nil, fmt.Errorf("stco 偏移 %d 超出 32 位表示范围（delta=%d）", value, delta)
		}
		out = appendBigEndianUint32(out, uint32(value))
	}
	return out, nil
}

// shiftUnsigned 计算 Python 的 max(0, old + delta)；超过 uint64 上限时报错
// （Python 在 struct.pack('>Q', …) 处抛 struct.error）。
func shiftUnsigned(old uint64, delta int64) (uint64, error) {
	if delta == 0 {
		return old, nil
	}
	if delta < 0 {
		if delta == math.MinInt64 {
			return 0, nil // 减法下溢：clamp 到 0，与 max(0, …) 同效
		}
		drop := uint64(-delta)
		if old < drop {
			return 0, nil
		}
		return old - drop, nil
	}
	sum := old + uint64(delta)
	if sum < old {
		return 0, fmt.Errorf("偏移重写溢出 64 位（old=%d delta=%d）", old, delta)
	}
	return sum, nil
}

// cleanContainer 对应 Python clean_container：递归处理 moov/trak/mdia/minf，
// 在 stbl 处做真活，stsd 一律原样保留（不递归 —— 见文件头「怪癖 4」）。
func (s *sanitizer) cleanContainer(container BoxHeader, delta int64) ([]byte, error) {
	result := make([]byte, 0, container.Size)
	for _, box := range Boxes(s.buf, container.Payload, container.End()) {
		switch box.Type {
		case "stbl":
			content, err := s.cleanStbl(box, delta)
			if err != nil {
				return nil, err
			}
			result = wrapContainer(result, "stbl", content)
		case "stsd":
			result = append(result, ByteRange(s.buf, box)...)
		case "minf", "mdia", "trak", "moov":
			content, err := s.cleanContainer(box, delta)
			if err != nil {
				return nil, err
			}
			result = wrapContainer(result, box.Type, content)
		default:
			result = append(result, ByteRange(s.buf, box)...)
		}
	}
	return result, nil
}

// wrapContainer 给容器负载补上头（Python 各处重复的 `new_size = 8 + len(content)` +
// 32/64 位分支）。
func wrapContainer(dst []byte, boxType string, content []byte) []byte {
	size := int64(8 + len(content))
	if size > maxUint32Value {
		dst = WriteBoxHeader(dst, boxType, uint64(size+8), true)
	} else {
		dst = WriteBoxHeader(dst, boxType, uint64(size), false)
	}
	return append(dst, content...)
}

func findBox(boxes []BoxHeader, name string) (BoxHeader, bool) {
	for _, box := range boxes {
		if box.Type == name {
			return box, true
		}
	}
	return BoxHeader{}, false
}

func filterBoxes(boxes []BoxHeader, name string) []BoxHeader {
	var out []BoxHeader
	for _, box := range boxes {
		if box.Type == name {
			out = append(out, box)
		}
	}
	return out
}

func boxTypes(boxes []BoxHeader) []string {
	out := make([]string, 0, len(boxes))
	for _, box := range boxes {
		out = append(out, box.Type)
	}
	return out
}
