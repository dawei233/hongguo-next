package sm3

import (
	"encoding/hex"
	"strings"
	"testing"
)

// TestStandardVectors 用公开标准向量校验 SM3（含跨多分组的 10000 字节输入）。
//
// 长输入是必须的：只有它能把"填充是否多出一个分组"与"消息扩展是否越界"这类错误
// 暴露出来，而 X-Argus 恰好对整体输出做逐字节比较，任何一处偏差都会让它全错。
func TestStandardVectors(t *testing.T) {
	vectors := []struct {
		name     string
		input    string
		expected string
	}{
		{"空输入", "", "1ab21d8355cfa17f8e61194831e81a8f22bec8c728fefb747ed035eb5082aa2b"},
		{"abc", "abc", "66c7f0f462eeedd9d1f2d46bdc10e4e24167c4875cf2f7a2297da02b8f4ba8e0"},
		{"跨块 64 字节", strings.Repeat("abcd", 16), "debe9ff92275b8a138604889c18e5a4d6fdb70e5387e5765293dcba39c0c5732"},
		{"跨多块 10000 字节", strings.Repeat("x", 10000), "335de0cbf893f7af9a3e548067c72f47630b9fa9293d93959c79c2085fab7e5e"},
	}
	for _, item := range vectors {
		if actual := SumHex([]byte(item.input)); actual != item.expected {
			t.Fatalf("%s: SM3 = %s，期望 %s", item.name, actual, item.expected)
		}
	}
}

// TestPaddingBoundaries 覆盖"填充后是否多出一个分组"的分界长度。
//
// 55/56 是 message+0x80 后能否塞进同一块的分界，63/64/65 是跨块写入的分界。
// 这里没有第二个独立实现可对照（Go 标准库与 x/crypto 都没有 SM3），因此用
// "分片写入必须与一次性等价"这条不变量来锁边界；与 core 扁平包 hongguoSM3 的
// 独立交叉回归在 core/danmaku/sign/parity_test.go。
func TestPaddingBoundaries(t *testing.T) {
	for _, length := range []int{0, 1, 55, 56, 57, 63, 64, 65, 127, 128, 1000} {
		data := make([]byte, length)
		for index := range data {
			data[index] = byte((index*37 + 11) & 0xff)
		}
		expected := SumHex(data)
		for _, chunk := range []int{1, 7, 63, 64, 65, 128} {
			streaming := New()
			for offset := 0; offset < len(data); offset += chunk {
				end := offset + chunk
				if end > len(data) {
					end = len(data)
				}
				if _, err := streaming.Write(data[offset:end]); err != nil {
					t.Fatalf("写入失败: %v", err)
				}
			}
			if actual := hex.EncodeToString(streaming.Sum(nil)); actual != expected {
				t.Fatalf("长度 %d 按 %d 分片不一致: %s != %s", length, chunk, actual, expected)
			}
		}
	}
}

// TestSumDoesNotMutateState 校验 Sum 不改变流式状态（hash.Hash 的约定）。
//
// X-Argus 一次算两个摘要（空体与 query），如果 Sum 会推进内部状态，第二次调用就算错。
func TestSumDoesNotMutateState(t *testing.T) {
	streaming := New()
	streaming.Write([]byte("abc"))
	first := hex.EncodeToString(streaming.Sum(nil))
	second := hex.EncodeToString(streaming.Sum(nil))
	if first != second {
		t.Fatalf("连续两次 Sum 结果不同: %s != %s", first, second)
	}
	streaming.Write([]byte("d"))
	if got, want := hex.EncodeToString(streaming.Sum(nil)), SumHex([]byte("abcd")); got != want {
		t.Fatalf("继续写入后的摘要不符: %s != %s", got, want)
	}
}

// TestReset 校验 Reset 回到初始状态。
func TestReset(t *testing.T) {
	streaming := New()
	streaming.Write([]byte("abcdef"))
	streaming.Reset()
	streaming.Write([]byte("abc"))
	if got, want := hex.EncodeToString(streaming.Sum(nil)), SumHex([]byte("abc")); got != want {
		t.Fatalf("Reset 后摘要不符: %s != %s", got, want)
	}
	if New().Size() != Size || New().BlockSize() != BlockSize {
		t.Fatal("Size/BlockSize 必须等于包常量")
	}
}
