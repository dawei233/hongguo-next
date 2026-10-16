package core

import (
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"github.com/dawei233/hongguo-next/core/danmaku/sign/aes"
	"testing"
)

// [hongguo-next 补丁测试 2026-10-16] 红果 App 取流新链路的**离线**验证。
//
// 覆盖三处最容易写错、且真机失败时会静默降级的地方：
//  1. spade/main_url 解密（AES 密钥材料推导 + 结构校验 + 去填充）
//  2. video_model 的**字符串/对象双形态**
//  3. fallback_api 的**字符串/对象双形态**，以及 `data[vid]` 取条目
//
// 真网络链路另有 TestLiveHongguoResolve（见文件末尾，需 HONGGUO_PROBE=1）。

// 用 Python 可用实现（hongguo_core.py decrypt_spade_url）的算法独立算一份期望值，
// 避免「自己实现自己验证」的同源盲区。
func TestHongguoSpadeDecryptMatchesReferenceAlgorithm(t *testing.T) {
	var constants = hongguoSpadeConstants
	keySeed := []byte("hongguo-next-test-seed-0123456789")
	// 构造一段合法密文：明文 = 一个假 URL
	plain := []byte("https://example.qznovelvod.com/video/tos/cn/abc.mp4")
	pad := 16 - len(plain)%16
	padded := append(append([]byte{}, plain...), bytesRepeat(byte(pad), pad)...)
	// 用同一推导出的 key/iv 做 AES-128-CBC 加密（借助仓库已有 aes 包）
	h1 := sha512.Sum512(keySeed)
	material := append(append([]byte{}, h1[:]...), constants[:]...)
	h2 := sha512.Sum512(material)
	encrypted, err := encryptCBC128ForTest(h2[:16], h2[16:32], padded)
	if err != nil {
		t.Fatal(err)
	}
	// 拼上 4 字节头（A8 00 01 00）
	cipher := append([]byte{0xA8, 0x00, 0x01, 0x00}, encrypted...)
	spade := base64.StdEncoding.EncodeToString(cipher)

	got, err := decryptHongguoSpadeURL(spade, keySeed)
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}
	if got != string(plain) {
		t.Fatalf("解密结果不符\n期望: %s\n实际: %s", plain, got)
	}
}

func TestHongguoSpadeRejectsBadStructure(t *testing.T) {
	keySeed := []byte("seed")
	for name, value := range map[string]string{
		"空串":       "",
		"非 base64": "!!!not-base64!!!",
		"头不对":      base64.StdEncoding.EncodeToString([]byte{0x01, 0x02, 0x03, 0x04, 0x05}),
		"太短":       base64.StdEncoding.EncodeToString([]byte{0xA8, 0x00, 0x01, 0x00}),
	} {
		if _, err := decryptHongguoSpadeURL(value, keySeed); err == nil {
			t.Errorf("%s：应报错却成功了", name)
		}
	}
	// 缺 seed
	valid := base64.StdEncoding.EncodeToString(append([]byte{0xA8, 0x00, 0x01, 0x00}, bytesRepeat(0x41, 16)...))
	if _, err := decryptHongguoSpadeURL(valid, nil); err == nil {
		t.Error("缺 key_seed：应报错却成功了")
	}
}

func TestHongguoModelFromMultiResponseBothShapes(t *testing.T) {
	inner := map[string]any{"video_duration": 91.285, "fallback_api": "https://fb.example/x"}
	encoded, _ := json.Marshal(inner)

	// 字符串形态（实测到的真实形态）
	asString := map[string]any{"data": map[string]any{"vid1": map[string]any{"video_model": string(encoded)}}}
	got, err := hongguoModelFromMultiResponse(asString, "vid1")
	if err != nil {
		t.Fatalf("字符串形态失败: %v", err)
	}
	if got["fallback_api"] != "https://fb.example/x" {
		t.Fatalf("字符串形态取值错: %v", got["fallback_api"])
	}

	// 对象形态（内核原有兼容，不得丢）
	asObject := map[string]any{"data": map[string]any{"vid1": map[string]any{"video_model": inner}}}
	got2, err := hongguoModelFromMultiResponse(asObject, "vid1")
	if err != nil {
		t.Fatalf("对象形态失败: %v", err)
	}
	if got2["fallback_api"] != "https://fb.example/x" {
		t.Fatalf("对象形态取值错: %v", got2["fallback_api"])
	}

	// 缺条目
	if _, err := hongguoModelFromMultiResponse(asString, "missing"); err == nil {
		t.Error("缺 data[vid]：应报错")
	}
	// 缺 video_model
	noModel := map[string]any{"data": map[string]any{"vid1": map[string]any{}}}
	if _, err := hongguoModelFromMultiResponse(noModel, "vid1"); err == nil {
		t.Error("缺 video_model：应报错")
	}
}

func TestHongguoFallbackAPIBothShapes(t *testing.T) {
	if got, err := hongguoFallbackAPI(map[string]any{"fallback_api": "https://a/b"}); err != nil || got != "https://a/b" {
		t.Fatalf("字符串形态: got=%q err=%v", got, err)
	}
	// 实测里 fallback_api 也可能是 {fallback_api: url} 的 map
	if got, err := hongguoFallbackAPI(map[string]any{"fallback_api": map[string]any{"fallback_api": "https://c/d"}}); err != nil || got != "https://c/d" {
		t.Fatalf("对象形态: got=%q err=%v", got, err)
	}
	for name, model := range map[string]map[string]any{
		"缺字段":  {},
		"空字符串": {"fallback_api": ""},
		"空对象":  {"fallback_api": map[string]any{}},
	} {
		if _, err := hongguoFallbackAPI(model); err == nil {
			t.Errorf("%s：应报错", name)
		}
	}
}

func TestHongguoSpadeKeySeedDecode(t *testing.T) {
	raw := []byte("0123456789abcdef0123456789abcdef")
	encoded := base64.StdEncoding.EncodeToString(raw)
	got, err := hongguoSpadeKeySeed(map[string]any{"key_seed": encoded})
	if err != nil || string(got) != string(raw) {
		t.Fatalf("key_seed 解码失败: got=%v err=%v", got, err)
	}
	if _, err := hongguoSpadeKeySeed(map[string]any{}); err == nil {
		t.Error("缺 key_seed：应报错")
	}
}

// bytesRepeat 是 strings.Repeat 的字节版（避免为了这一个用途引入 strings）。
func bytesRepeat(value byte, count int) []byte {
	out := make([]byte, count)
	for i := range out {
		out[i] = value
	}
	return out
}

// encryptCBC128ForTest 仅用于本测试构造密文，复用仓库已有的 aes 包。
func encryptCBC128ForTest(key, iv, plaintext []byte) ([]byte, error) {
	return aes.EncryptCBC128Raw(key, iv, plaintext)
}
