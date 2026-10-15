// Package aes 提供 X-Argus 需要的 AES-128-CBC 与 PKCS#7 填充。
//
// # 为什么不直接用 crypto/cipher 的默认路径
//
// 参考实现（C 的 danmaku-sign.js / A 的 danmaku.py / core 的
// provider_hongguo_comment_sign.go）里的 AES-CBC **要求明文长度已是 16 的整数倍
// 且不再追加填充**：Node 侧是显式 setAutoPadding(false)，Go 侧是
// cipher.NewCBCEncrypter(...).CryptBlocks 直接对已补齐的明文加密。
// 如果这里"顺手"用标准库的自动填充，密文会多出一个整块，签名立即失效。
//
// 因此本包把两种语义分开命名：
//   - EncryptCBC128 / DecryptCBC128：带 PKCS#7 填充（通用用途）；
//   - EncryptCBC128Raw / DecryptCBC128Raw：不做任何填充，长度必须是 16 的倍数
//     （X-Argus 走这条）。
//
// PKCS#7 的补齐量在 len%16 == 0 时是 16（而不是 0）——与参考实现
// hongguoCommentPad 的 `padding = 16 - len%16` 一致；这条边界有单测钉住。
package aes

import (
	cryptoaes "crypto/aes"
	"crypto/cipher"
	"errors"
	"fmt"
)

// BlockSize 是 AES 分组字节数。
const BlockSize = 16

// KeySize128 是 AES-128 的密钥字节数。
const KeySize128 = 16

// ErrInvalidKeySize 在密钥长度不是 16 字节时返回。
var ErrInvalidKeySize = errors.New("AES-128 密钥必须是 16 字节")

// ErrInvalidIVSize 在初始向量长度不等于分组长度时返回。
var ErrInvalidIVSize = errors.New("AES-CBC 的 IV 必须等于分组长度")

// ErrInvalidPadding 在解密结果的 PKCS#7 填充非法时返回。
var ErrInvalidPadding = errors.New("PKCS#7 填充非法")

// PadPKCS7 按 PKCS#7 补齐到 blockSize 的整数倍。
//
// 长度已经是整数倍时**补满一整块**（补齐量 = blockSize），这是 PKCS#7 的要求，
// 也是参考实现的行为；不补的话解密端无法判断原文末尾有没有填充字节。
func PadPKCS7(data []byte, blockSize int) []byte {
	if blockSize <= 0 {
		blockSize = BlockSize
	}
	padding := blockSize - len(data)%blockSize
	out := make([]byte, 0, len(data)+padding)
	out = append(out, data...)
	for index := 0; index < padding; index++ {
		out = append(out, byte(padding))
	}
	return out
}

// UnpadPKCS7 去掉 PKCS#7 填充。
func UnpadPKCS7(data []byte, blockSize int) ([]byte, error) {
	if blockSize <= 0 {
		blockSize = BlockSize
	}
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, ErrInvalidPadding
	}
	padding := int(data[len(data)-1])
	if padding == 0 || padding > blockSize || padding > len(data) {
		return nil, ErrInvalidPadding
	}
	for _, value := range data[len(data)-padding:] {
		if int(value) != padding {
			return nil, ErrInvalidPadding
		}
	}
	return data[:len(data)-padding], nil
}

// EncryptCBC128Raw 用 AES-128-CBC 加密 plaintext，**不做任何填充**。
//
// plaintext 长度必须是 16 的整数倍（X-Argus 的容器在调用前已用 PadPKCS7 补齐）。
func EncryptCBC128Raw(key, iv, plaintext []byte) ([]byte, error) {
	block, err := newCBC(key, iv)
	if err != nil {
		return nil, err
	}
	if len(plaintext)%BlockSize != 0 {
		return nil, fmt.Errorf("AES-CBC 明文长度必须是 %d 的整数倍（当前 %d）", BlockSize, len(plaintext))
	}
	out := make([]byte, len(plaintext))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, plaintext)
	return out, nil
}

// EncryptCBC128 用 AES-128-CBC 加密 plaintext，先按 PKCS#7 补齐。
func EncryptCBC128(key, iv, plaintext []byte) ([]byte, error) {
	return EncryptCBC128Raw(key, iv, PadPKCS7(plaintext, BlockSize))
}

// DecryptCBC128Raw 用 AES-128-CBC 解密 ciphertext，**不做去填充**。
func DecryptCBC128Raw(key, iv, ciphertext []byte) ([]byte, error) {
	block, err := newCBC(key, iv)
	if err != nil {
		return nil, err
	}
	if len(ciphertext)%BlockSize != 0 {
		return nil, fmt.Errorf("AES-CBC 密文长度必须是 %d 的整数倍（当前 %d）", BlockSize, len(ciphertext))
	}
	out := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, ciphertext)
	return out, nil
}

// DecryptCBC128 用 AES-128-CBC 解密并去掉 PKCS#7 填充。
func DecryptCBC128(key, iv, ciphertext []byte) ([]byte, error) {
	out, err := DecryptCBC128Raw(key, iv, ciphertext)
	if err != nil {
		return nil, err
	}
	return UnpadPKCS7(out, BlockSize)
}

func newCBC(key, iv []byte) (cipher.Block, error) {
	if len(key) != KeySize128 {
		return nil, ErrInvalidKeySize
	}
	if len(iv) != BlockSize {
		return nil, ErrInvalidIVSize
	}
	return cryptoaes.NewCipher(key)
}
