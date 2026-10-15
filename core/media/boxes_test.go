// 本仓新增（M3 媒体管线测试）。
//
// box 游标的边界用例：这些行为全部来自 Python 实现的实际语义（切片自动裁剪、
// size==0 表示「到文件末尾」、size==1 走 64 位头），净化器的逐字节一致性就建立在这里。
package media_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/dawei233/hongguo-next/core/media"
)

func boxBytes(name string, payload []byte) []byte {
	out := make([]byte, 0, 8+len(payload))
	out = binary.BigEndian.AppendUint32(out, uint32(8+len(payload)))
	out = append(out, name...)
	return append(out, payload...)
}

func TestParseBoxHeader32Bit(t *testing.T) {
	body := boxBytes("mdat", []byte("hello"))
	box, ok := media.ParseBoxHeader(body, 0)
	if !ok {
		t.Fatal("应解析成功")
	}
	if box.Type != "mdat" || box.Offset != 0 || box.Size != 13 || box.Payload != 8 || box.Has64 {
		t.Fatalf("字段不符: %+v", box)
	}
	if box.HeaderSize() != 8 || box.End() != 13 {
		t.Fatalf("HeaderSize/End 不符: %d %d", box.HeaderSize(), box.End())
	}
}

func TestParseBoxHeader64Bit(t *testing.T) {
	body := make([]byte, 0, 24)
	body = binary.BigEndian.AppendUint32(body, 1)
	body = append(body, "uuid"...)
	body = binary.BigEndian.AppendUint64(body, 24)
	body = append(body, bytes.Repeat([]byte{7}, 8)...)
	box, ok := media.ParseBoxHeader(body, 0)
	if !ok {
		t.Fatal("应解析成功")
	}
	if !box.Has64 || box.Size != 24 || box.Payload != 16 || box.HeaderSize() != 16 || box.End() != 24 {
		t.Fatalf("64 位头字段不符: %+v", box)
	}
	// 64 位尺寸头越界（不足 16 字节）→ ok=false，而不是抛异常。
	short := append(binary.BigEndian.AppendUint32(nil, 1), "uuid"...)
	if _, ok := media.ParseBoxHeader(short, 0); ok {
		t.Fatal("64 位头不足 16 字节时应返回 ok=false")
	}
}

func TestParseBoxHeaderSizeZeroMeansToEndOfBuffer(t *testing.T) {
	body := append(binary.BigEndian.AppendUint32(nil, 0), "mdat"...)
	body = append(body, bytes.Repeat([]byte{1}, 32)...)
	box, ok := media.ParseBoxHeader(body, 0)
	if !ok {
		t.Fatal("应解析成功")
	}
	if box.Size != int64(len(body)) || box.Payload != 8 {
		t.Fatalf("size==0 应按整个缓冲区长度解释: %+v", box)
	}
	// 起点落在缓冲区末尾时，Python 的 parse_box_header 先撞上 `offset + 8 > len(buf)`
	// 而返回 None（Go 侧同为 ok=false）。也就是说 iter_boxes 里那句
	// `if not b or b['size'] <= 0: break` 的后半段在文件内其实不可达
	// —— 这是 Python 的实际行为，Go 转写保持一致而不是"顺手修好"。
	if _, ok := media.ParseBoxHeader(body, int64(len(body))); ok {
		t.Fatal("起点落在末尾时应返回 ok=false（对应 Python 返回 None）")
	}
	tail := media.Boxes(body, int64(len(body)), int64(len(body))+64)
	if len(tail) != 0 {
		t.Fatalf("起点在末尾时不该解析出 box: %+v", tail)
	}
	// 但 size==0 出现在文件中间时，会吞掉「到文件末尾」的全部字节（跨容器读取）。
	nested := append(boxBytes("free", nil), body...)
	boxes := media.Boxes(nested, 8, int64(len(nested)))
	if len(boxes) != 1 || boxes[0].Size != int64(len(nested))-8 {
		t.Fatalf("嵌套 size==0 应跨容器读到文件末尾: %+v", boxes)
	}
}

func TestParseBoxHeaderShortBuffer(t *testing.T) {
	if _, ok := media.ParseBoxHeader([]byte("abc"), 0); ok {
		t.Fatal("不足 8 字节应返回 ok=false")
	}
	if _, ok := media.ParseBoxHeader([]byte("abcdefgh"), 4); ok {
		t.Fatal("offset 越界应返回 ok=false")
	}
	if _, ok := media.ParseBoxHeader([]byte("abcdefgh"), -1); ok {
		t.Fatal("负 offset 应返回 ok=false")
	}
}

func TestBoxesIteratesAndStops(t *testing.T) {
	body := append(boxBytes("ftyp", []byte("isom")), boxBytes("moov", []byte("mvhd"))...)
	body = append(body, boxBytes("mdat", []byte("data"))...)
	boxes := media.Boxes(body, 0, int64(len(body)))
	if len(boxes) != 3 {
		t.Fatalf("应解析出 3 个 box，实际 %d: %+v", len(boxes), boxes)
	}
	if boxes[0].Type != "ftyp" || boxes[1].Type != "moov" || boxes[2].Type != "mdat" {
		t.Fatalf("顺序不符: %+v", boxes)
	}
	if boxes[2].Offset != int64(len(body)-12) {
		t.Fatalf("mdat 起点不符: %+v", boxes[2])
	}
	// end 收窄 → 只解析落在范围内的 box。
	if narrowed := media.Boxes(body, 0, boxes[1].Offset); len(narrowed) != 1 {
		t.Fatalf("end 收窄后应只剩 1 个 box: %+v", narrowed)
	}
}

func TestBoxesAdvancesOnTinySize(t *testing.T) {
	// size=4 的畸形头：与 Python 一致，pos 前进 4 字节（不做任何修正）。
	body := make([]byte, 24)
	binary.BigEndian.PutUint32(body[0:4], 4)
	copy(body[4:8], "abcd")
	binary.BigEndian.PutUint32(body[4:8], 0) // 让第二个"box"落在 pos=4
	boxes := media.Boxes(body, 0, int64(len(body)))
	if len(boxes) == 0 {
		t.Fatal("畸形头也应至少解析出一个 box（Python 的 pos += size 语义）")
	}
	if boxes[0].Size != 4 {
		t.Fatalf("第一个 box 的 size 应为声明的 4，实际 %d", boxes[0].Size)
	}
}

func TestSliceClampsLikePython(t *testing.T) {
	body := []byte("0123456789")
	cases := []struct {
		from, to int64
		want     string
	}{
		{0, 4, "0123"},
		{4, 100, "456789"}, // 右越界 → 裁剪到末尾
		{100, 200, ""},     // 全越界 → 空
		{-5, 3, "012"},     // 负起点 → 0
		{5, 2, ""},         // to < from → 空
		{3, 3, ""},         // 空区间
	}
	for _, item := range cases {
		if got := string(media.Slice(body, item.from, item.to)); got != item.want {
			t.Errorf("Slice(%d,%d) = %q，期望 %q", item.from, item.to, got, item.want)
		}
	}
	// ByteRange / BoxPayload 是 Slice 的两个常用包装。
	box := media.BoxHeader{Type: "mdat", Offset: 0, Size: 100, Payload: 8}
	if got := string(media.ByteRange(body, box)); got != "0123456789" {
		t.Fatalf("ByteRange 应裁剪到文件长度，实际 %q", got)
	}
	if got := string(media.BoxPayload(body, box)); got != "89" {
		t.Fatalf("BoxPayload 应从 Payload 裁剪到末尾，实际 %q", got)
	}
}

func TestWriteBoxHeader(t *testing.T) {
	wide := media.WriteBoxHeader(nil, "stco", 24, true)
	if len(wide) != 16 || binary.BigEndian.Uint32(wide[0:4]) != 1 ||
		string(wide[4:8]) != "stco" || binary.BigEndian.Uint64(wide[8:16]) != 24 {
		t.Fatalf("64 位头不符: % x", wide)
	}
	narrow := media.WriteBoxHeader(nil, "stco", 192, false)
	if len(narrow) != 8 || binary.BigEndian.Uint32(narrow[0:4]) != 192 || string(narrow[4:8]) != "stco" {
		t.Fatalf("32 位头不符: % x", narrow)
	}
	// declared 与「实际写入字节数」可以不一致（净化器刻意依赖这一点）。
	odd := media.WriteBoxHeader(nil, "stco", 200, false)
	if binary.BigEndian.Uint32(odd[0:4]) != 200 || len(odd) != 8 {
		t.Fatalf("declared 应原样写入而不做推导: % x", odd)
	}
}

func TestParseBoxHeaderKeepsNonASCIIBytes(t *testing.T) {
	// 类型名是 4 个不透明字节（Python 的 latin-1 解码：单射，等价于原样保留）。
	body := make([]byte, 8)
	binary.BigEndian.PutUint32(body[0:4], 8)
	copy(body[4:8], []byte{0xff, 0x00, 0x80, 0x41})
	box, ok := media.ParseBoxHeader(body, 0)
	if !ok {
		t.Fatal("应解析成功")
	}
	if len(box.Type) != 4 || !bytes.Equal([]byte(box.Type), []byte{0xff, 0x00, 0x80, 0x41}) {
		t.Fatalf("类型名被改写了: % x", []byte(box.Type))
	}
}
