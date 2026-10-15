package aes

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// TestPKCS7PadAlwaysAddsPadding 锁住"长度已是整数倍时再补满一整块"这条语义。
//
// 参考实现的 hongguoCommentPad 是 `padding = 16 - len%16`：len%16 == 0 时补 16 字节
// 0x10。如果这里"顺手"改成"已经对齐就不补"，X-Argus 的最后一块会少一段，
// 解密端（以及上游）看到的就是错的明文 —— 而且只在恰好对齐的输入上出错。
func TestPKCS7PadAlwaysAddsPadding(t *testing.T) {
	for _, length := range []int{0, 1, 15, 16, 17, 31, 32} {
		padded := PadPKCS7(make([]byte, length), BlockSize)
		if len(padded)%BlockSize != 0 {
			t.Fatalf("长度 %d 补齐后不是 16 的倍数: %d", length, len(padded))
		}
		if len(padded) <= length {
			t.Fatalf("长度 %d 补齐后没有变长: %d", length, len(padded))
		}
		padding := int(padded[len(padded)-1])
		if padding != len(padded)-length {
			t.Fatalf("长度 %d 的补齐量应为 %d，实际 %d", length, len(padded)-length, padding)
		}
	}
	if got := len(PadPKCS7(make([]byte, 16), BlockSize)); got != 32 {
		t.Fatalf("已对齐的 16 字节应补成 32 字节，实际 %d", got)
	}
}

// TestRawCBCNeverAddsPadding 锁住 X-Argus 依赖的"不再追加填充"语义。
func TestRawCBCNeverAddsPadding(t *testing.T) {
	key := bytes.Repeat([]byte{0x11}, KeySize128)
	iv := bytes.Repeat([]byte{0x22}, BlockSize)
	plaintext := bytes.Repeat([]byte{0x33}, 48)
	encrypted, err := EncryptCBC128Raw(key, iv, plaintext)
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	if len(encrypted) != len(plaintext) {
		t.Fatalf("Raw 模式密文长度应等于明文长度（%d），实际 %d —— 多出一块说明加了填充", len(plaintext), len(encrypted))
	}
	if _, err := EncryptCBC128Raw(key, iv, make([]byte, 17)); err == nil {
		t.Fatal("Raw 模式对非 16 倍数的明文必须报错，而不是悄悄补齐")
	}
}

// TestCBCRoundTrip 校验带 PKCS#7 的通用路径可逆，且密文与标准向量一致。
func TestCBCRoundTrip(t *testing.T) {
	key := []byte("0123456789abcdef")
	iv := []byte("abcdef0123456789")
	for _, plaintext := range []string{"", "a", "0123456789abcdef", "0123456789abcdefg", "中文弹幕一条"} {
		encrypted, err := EncryptCBC128(key, iv, []byte(plaintext))
		if err != nil {
			t.Fatalf("加密 %q 失败: %v", plaintext, err)
		}
		if len(encrypted)%BlockSize != 0 {
			t.Fatalf("密文长度不是 16 的倍数: %d", len(encrypted))
		}
		decrypted, err := DecryptCBC128(key, iv, encrypted)
		if err != nil {
			t.Fatalf("解密 %q 失败: %v", plaintext, err)
		}
		if string(decrypted) != plaintext {
			t.Fatalf("往返不一致: %q != %q", decrypted, plaintext)
		}
	}
}

// TestCBCMatchesNISTVector 用 NIST SP 800-38A 的 AES-128-CBC 向量校验原始 CBC。
func TestCBCMatchesNISTVector(t *testing.T) {
	key, _ := hex.DecodeString("2b7e151628aed2a6abf7158809cf4f3c")
	iv, _ := hex.DecodeString("000102030405060708090a0b0c0d0e0f")
	plaintext, _ := hex.DecodeString("6bc1bee22e409f96e93d7e117393172a")
	expected, _ := hex.DecodeString("7649abac8119b246cee98e9b12e9197d")
	encrypted, err := EncryptCBC128Raw(key, iv, plaintext)
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	if !bytes.Equal(encrypted, expected) {
		t.Fatalf("AES-128-CBC 与标准向量不一致: %x", encrypted)
	}
}

// TestUnpadRejectsIllegalPadding 校验去填充会拒绝非法输入（防止把垃圾当明文）。
func TestUnpadRejectsIllegalPadding(t *testing.T) {
	cases := [][]byte{
		nil,
		{0x01, 0x02, 0x03},
		append(bytes.Repeat([]byte{0x00}, 15), 0x00),
		append(bytes.Repeat([]byte{0x00}, 15), 0x11),
		append(bytes.Repeat([]byte{0x00}, 15), 0x03),
	}
	for _, item := range cases {
		if _, err := UnpadPKCS7(item, BlockSize); err == nil {
			t.Fatalf("非法填充应当报错: %x", item)
		}
	}
}

// TestInvalidKeyOrIV 校验密钥/IV 长度与算法要求不符时报错而不是给出错误结果。
func TestInvalidKeyOrIV(t *testing.T) {
	if _, err := EncryptCBC128Raw(make([]byte, 24), make([]byte, BlockSize), make([]byte, 16)); err == nil {
		t.Fatal("非 16 字节密钥必须报错（本包只做 AES-128）")
	}
	if _, err := EncryptCBC128Raw(make([]byte, KeySize128), make([]byte, 8), make([]byte, 16)); err == nil {
		t.Fatal("IV 长度不等于分组长度必须报错")
	}
}
