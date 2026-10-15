// 本仓新增（M3 媒体管线）。
//
// 本文件是 A `hongguo-monorepo/nas-backend/mp4_sanitize.py` 第 7–34 行
// （parse_box_header / iter_boxes）的 Go 逐函数转写（ARCHITECTURE §3.1 #1）。
// 转写原则：**语义逐条对应、不求重构**。Python 的怪癖在这里一律保留，因为
// sanitize.go 的输出必须与 Python 实现逐字节一致（golden fixture 断言，见
// testdata/gen_golden.py 与 sanitize_test.go）。
//
// 逐条对应关系（Python 行号 → 本文件）：
//
//	parse_box_header (7–24) → ParseBoxHeader
//	iter_boxes       (27–34) → Boxes
//
// 三处刻意的实现细节，均在下方各自标注：
//   - 类型名按 latin-1 解码。latin-1 是单射，故 Go 侧直接以原始 4 字节字符串表示，
//     写回时即原始字节，与 Python 的 decode('latin-1')/encode('latin-1') 往返等价。
//   - size==0 的「到文件末尾」按 **整个缓冲区长度** 计算（len(buf)-offset），
//     而不是父容器的 end。这是 Python 的实际行为，嵌套 size==0 时会产生跨容器读取。
//   - 边界检查用 len(buf) 而非容器的 end：size==1 的 64 位尺寸头只要落在文件内即可，
//     允许越过父容器边界。同样照搬。
package media

import "math"

// BoxHeader 是 Python parse_box_header 返回字典的 Go 形态。
//
// 字段对应：
//
//	'type'        → Type   （latin-1 解码后的 4 字节）
//	'offset'      → Offset （box 起点）
//	'size'        → Size   （box 总长度，含头）
//	'data_offset' → Payload（负载起点：offset+8 / offset+16）
//	'has_64'      → Has64  （头部为 size==1 的 64 位形态）
type BoxHeader struct {
	Type    string
	Offset  int64
	Size    int64
	Payload int64
	Has64   bool
}

// HeaderSize 返回 box 头长度（8 或 16 字节）。
func (b BoxHeader) HeaderSize() int64 {
	if b.Has64 {
		return 16
	}
	return 8
}

// End 返回 box 声明范围的右端点，溢出时饱和到 MaxInt64
// （Python 的 int 无上界；Go 侧饱和后所有切片都会按文件长度裁剪，结果一致）。
func (b BoxHeader) End() int64 { return saturateAdd(b.Offset, b.Size) }

// ParseBoxHeader 对应 Python parse_box_header。
//
// 返回 ok=false 的两种情形：
//   - offset+8 > len(buf)：Python 返回 None（iter_boxes 随即 break）；
//   - size==1 但 offset+16 > len(buf)：Python 的 struct.unpack 会抛 struct.error
//     （进程内调用方按失败处理）。Go 侧统一收敛为 ok=false —— 差异只发生在畸形输入上，
//     此时 Python 不产出任何输出字节，不存在「字节级不一致」的可能。
func ParseBoxHeader(buf []byte, offset int64) (BoxHeader, bool) {
	limit := int64(len(buf))
	if offset < 0 || offset+8 > limit {
		return BoxHeader{}, false
	}
	size := int64(bigEndianUint32(buf[offset : offset+4]))
	// 类型名不透明：保留原始 4 字节，等价于 Python 的 decode('latin-1')。
	boxType := string(buf[offset+4 : offset+8])
	switch size {
	case 1:
		if offset+16 > limit {
			return BoxHeader{}, false
		}
		size = saturateUint64(bigEndianUint64(buf[offset+8 : offset+16]))
		return BoxHeader{Type: boxType, Offset: offset, Size: size, Payload: offset + 16, Has64: true}, true
	case 0:
		// 注意：按整个文件长度计算，不是父容器的 end。
		return BoxHeader{Type: boxType, Offset: offset, Size: limit - offset, Payload: offset + 8}, true
	default:
		return BoxHeader{Type: boxType, Offset: offset, Size: size, Payload: offset + 8}, true
	}
}

// Boxes 对应 Python iter_boxes：从 start 起逐个解析，直到越过 end 或遇到无效长度。
//
// Python 的生成器在消费方每次 `break` 时提前结束；本仓的消费方（clean_stbl /
// clean_container / sanitize 本体）都只在「box 无效」时 break，而无效 box 本身就是
// 迭代终止条件，故一次性返回切片与生成器语义等价。
func Boxes(buf []byte, start, end int64) []BoxHeader {
	var out []BoxHeader
	for pos := start; pos+8 <= end; {
		box, ok := ParseBoxHeader(buf, pos)
		if !ok || box.Size <= 0 {
			break
		}
		out = append(out, box)
		next := box.End()
		if next <= pos {
			break // 理论上不可达（Size>0 时必前进），仅防御溢出
		}
		pos = next
	}
	return out
}

// Slice 等价于 Python 的 buf[from:to]：越界自动裁剪，从不 panic。
//
// 这是 Python 与 Go 之间最容易出错的一处「隐式」行为：Python 切片对越界是静默裁剪，
// 于是「声明的 box 长度超出文件末尾」会产出被截断的内容，而不是报错。
// 净化器整体依赖该语义（例如 64 位尺寸头声明超大长度的预置头）。
func Slice(buf []byte, from, to int64) []byte {
	length := int64(len(buf))
	if from < 0 {
		from = 0
	}
	if from > length {
		from = length
	}
	if to < from {
		to = from
	}
	if to > length {
		to = length
	}
	return buf[from:to]
}

// ByteRange 返回 box 在缓冲区中的原始字节（声明的完整范围，Python 的
// bytes(buf[b['offset']:b['offset'] + b['size']])）。
func ByteRange(buf []byte, box BoxHeader) []byte {
	return Slice(buf, box.Offset, box.End())
}

// BoxPayload 返回 box 的负载字节（Python 的 buf[b['data_offset']:b['offset']+b['size']]）。
func BoxPayload(buf []byte, box BoxHeader) []byte {
	return Slice(buf, box.Payload, box.End())
}

func bigEndianUint32(raw []byte) uint32 {
	if len(raw) < 4 {
		return 0
	}
	return uint32(raw[0])<<24 | uint32(raw[1])<<16 | uint32(raw[2])<<8 | uint32(raw[3])
}

func bigEndianUint64(raw []byte) uint64 {
	if len(raw) < 8 {
		return 0
	}
	var value uint64
	for _, b := range raw[:8] {
		value = value<<8 | uint64(b)
	}
	return value
}

func putBigEndianUint32(raw []byte, value uint32) {
	raw[0] = byte(value >> 24)
	raw[1] = byte(value >> 16)
	raw[2] = byte(value >> 8)
	raw[3] = byte(value)
}

func putBigEndianUint64(raw []byte, value uint64) {
	for index := 7; index >= 0; index-- {
		raw[index] = byte(value)
		value >>= 8
	}
}

func appendBigEndianUint32(dst []byte, value uint32) []byte {
	return append(dst, byte(value>>24), byte(value>>16), byte(value>>8), byte(value))
}

func appendBigEndianUint64(dst []byte, value uint64) []byte {
	return append(dst, byte(value>>56), byte(value>>48), byte(value>>40), byte(value>>32),
		byte(value>>24), byte(value>>16), byte(value>>8), byte(value))
}

// saturateAdd 是加法的饱和版本（Python 整数无上界，Go 侧饱和后由 Slice 裁剪）。
func saturateAdd(left, right int64) int64 {
	if right <= 0 {
		return left
	}
	if left > math.MaxInt64-right {
		return math.MaxInt64
	}
	return left + right
}

// saturateUint64 把 u64 收敛到 int64（仅 64 位尺寸头可能超出）。
func saturateUint64(value uint64) int64 {
	if value > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(value)
}

// WriteBoxHeader 写出 box 头，对应 Python 里三处重复的头装箱分支：
//
//	struct.pack('>I', 1); type; struct.pack('>Q', declared)   // 64 位形态
//	struct.pack('>I', size); type                             // 32 位形态
//
// declared 由调用方给出 —— Python 各处给出的 declared 与「实际写入字节数」并不总相等
// （stco/co64 的 delta==0 路径就是这样），因此不在这里推导，必须原样传入。
func WriteBoxHeader(dst []byte, boxType string, declared uint64, wide bool) []byte {
	if wide {
		dst = appendBigEndianUint32(dst, 1)
		dst = append(dst, boxType...)
		return appendBigEndianUint64(dst, declared)
	}
	dst = appendBigEndianUint32(dst, uint32(declared))
	return append(dst, boxType...)
}
