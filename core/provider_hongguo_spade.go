// hongguo-next 补丁：红果 spade_a 加密播放地址的解密。
//
// 为什么需要（2026-10-16 真机返工）：
// 红果 App 取流链路拿到的是 **加密地址**（`video_list[档位].spade_a`）而不是明文 URL。
// 同一响应里的 `key_seed` 是解密种子。缺这一步 → 播放器拿到的是 base64 密文，
// 表现为「返不出视频流」。
//
// 算法来源（逐行移植，可用实现）：
//   - `/workspace/红果/hongguo-monorepo/nas-backend/hongguo_core.py:1156 decrypt_spade_url`
//     （PC 端实测可用；AES 密钥材料 = sha512(key_seed) 再与 64 字节常数拼接后 sha512）
//   - 常数表同该函数第 1163-1168 行（64 字节）
//
// 与 `hongguoContentKey`（provider_hongguo_playback.go）的分工：
//   - `hongguoContentKey(spade_a)`  → 16 字节 **CENC 内容密钥**（解密分片内容）
//   - `decryptHongguoSpadeURL(...)` → **明文播放 URL**（解密地址本身）
//   两者都吃 spade_a，但目的与算法不同，不可互相替代。

package core

import (
	"bytes"
	"crypto/sha512"
	"errors"
	"strings"

	"github.com/dawei233/hongguo-next/core/danmaku/sign/aes"
)

// hongguoSpadeConstants 是 decrypt_spade_url 里的 64 字节常数表。
var hongguoSpadeConstants = [64]byte{
	0x4D, 0xD4, 0xC2, 0xE6, 0xB8, 0x31, 0x62, 0x09, 0x0E, 0x52, 0xB3, 0xC7, 0xA6, 0x73, 0x3B, 0xA4,
	0x1C, 0xB2, 0x46, 0x2B, 0x82, 0x9A, 0xB5, 0x8A, 0x19, 0x6B, 0x39, 0xDB, 0x57, 0x17, 0x75, 0x24,
	0xF4, 0x9B, 0xAF, 0x7F, 0x08, 0xE8, 0xD6, 0x8D, 0x26, 0xA7, 0x2E, 0x37, 0xC1, 0xA9, 0x5A, 0x2F,
	0x1F, 0x05, 0xA5, 0x18, 0x92, 0xAE, 0xF2, 0x94, 0x97, 0x32, 0xB6, 0x2A, 0x38, 0xAA, 0xDD, 0x58,
}

// decryptHongguoSpadeURL 把 spade_a 密文解成明文播放地址。
//
// 对应 Python `decrypt_spade_url(b64_str, key_seed)`：
//
//	raw[0]==0xA8 && raw[2]==0x01 && raw[3]==0x00 才认（否则原实现返回空串）
//	h1 = sha512(key_seed); h2 = sha512(h1 || constants)
//	aes_key = h2[0:16]; iv = h2[16:32]
//	密文（raw[4:] 截到 16 的整数倍）走 AES-128-CBC，去 PKCS#7 填充，去尾部 \x00
func decryptHongguoSpadeURL(spade string, keySeed []byte) (string, error) {
	raw, err := decodeHongguoBase64(spade)
	if err != nil {
		return "", errors.New("红果加密地址编码无效")
	}
	if len(raw) < 5 || raw[0] != 0xA8 || raw[2] != 0x01 || raw[3] != 0x00 {
		return "", errors.New("红果加密地址结构无效")
	}
	cipherData := raw[4:]
	// 与 Python 一致：截到 16 的整数倍（尾部不足一块的丢弃）
	cipherData = cipherData[:(len(cipherData)/16)*16]
	if len(cipherData) == 0 {
		return "", errors.New("红果加密地址长度不足")
	}
	if len(keySeed) == 0 {
		return "", errors.New("红果加密地址缺少解密种子")
	}

	h1 := sha512.Sum512(keySeed)
	material := make([]byte, 0, len(h1)+len(hongguoSpadeConstants))
	material = append(material, h1[:]...)
	material = append(material, hongguoSpadeConstants[:]...)
	h2 := sha512.Sum512(material)
	key, iv := h2[:16], h2[16:32]

	plaintext, err := aes.DecryptCBC128Raw(key, iv, cipherData)
	if err != nil {
		return "", err
	}
	// PKCS#7 去填充（Python 同逻辑：仅当填充值合法才去）
	if length := len(plaintext); length > 0 {
		pad := int(plaintext[length-1])
		if pad >= 1 && pad <= 16 && pad <= length {
			plaintext = plaintext[:length-pad]
		}
	}
	plaintext = bytes.TrimRight(plaintext, "\x00")
	url := string(plaintext)
	if url == "" {
		return "", errors.New("红果加密地址解密结果为空")
	}
	return url, nil
}

// hongguoSpadeKeySeed 从 fallback_api 响应里取 key_seed 并解码。
//
// 可用实现（hongguo_core.py:1263）：
//
//	key_seed_raw = b64_decode_padded(video_data.get("key_seed", ""))
func hongguoSpadeKeySeed(videoData map[string]any) ([]byte, error) {
	encoded := mapString(videoData, "key_seed")
	if strings.TrimSpace(encoded) == "" {
		return nil, errors.New("红果取流响应缺少 key_seed")
	}
	raw, err := decodeHongguoBase64(encoded)
	if err != nil || len(raw) == 0 {
		return nil, errors.New("红果 key_seed 编码无效")
	}
	return raw, nil
}
