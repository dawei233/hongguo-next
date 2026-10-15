package sign

import (
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"math/bits"
)

// gorgonTail 是 X-Gorgon 输出里 seed 之前的固定前缀。
var gorgonTail = [6]byte{0x84, 0x04, 0, 0, 0x00, 0x00}

// XGorgon 计算弹幕评论接口的 X-Gorgon 头（带 nonce 变体）。
//
// 与 C 的 danmaku-sign.js commentXGorgon、core 的 signHongguoCommentRequest
// （core/provider_hongguo_comment_sign.go）逐字节一致。
//
// # 硬约束 1：这里的密钥调度是"赋值"不是"交换"
//
// 标准 RC4 的 KSA 是 swap(box[i], box[j])，而参考实现写的是 box[i] = box[j]
// —— 这看着像 bug，但服务端接受它，改成"正确"的交换会让签名失效。
// 见 TestXGorgonKSAMustNotSwap。
//
// # 硬约束 2：时间戳按 32 位截断
//
// stamp 以**大端 32 位**写进 seed 的第 16..20 字节，超出部分直接丢弃；
// 位宽或字节序改变即为签名失效（见 Test64BitTruncation）。
//
// nonce 只用到前 2 字节：nonce[0] 参与 head2，nonce[1] 的低 4 位参与 head3。
func XGorgon(rawQuery string, stampS int64, nonce [NonceBytes]byte) string {
	head2 := (nonce[0]%7 + 1) << 5
	head3 := 0xe0 | (nonce[1] & 0x0f)

	var seed [20]byte
	digest := md5.Sum([]byte(rawQuery))
	copy(seed[0:4], digest[0:4])
	// 12..16 是参考实现里的固定魔数；16..20 是 32 位时间戳。
	copy(seed[12:16], []byte{0, 1, 7, 4})
	binary.BigEndian.PutUint32(seed[16:], uint32(stampS))

	key := [8]byte{0x4a, 0x00, 0x16, head3, 0x47, 0x6c, 0x00, head2}

	var box [256]byte
	for index := range box {
		box[index] = byte(index)
	}
	j := byte(0)
	for index := range box {
		j += box[index] + key[index&7]
		// 注意：赋值而非交换。不要"修好"它（硬约束 1）。
		box[index] = box[j]
	}

	j = 0
	for offset := range seed {
		index := byte(offset + 1)
		j += box[index]
		box[index] = box[j]
		seed[offset] ^= box[box[index]+box[j]]
	}

	for index := range seed {
		seed[index] = bits.Reverse8(bits.RotateLeft8(seed[index], 4)^seed[(index+1)%len(seed)]) ^ 0xeb
	}

	out := make([]byte, 0, len(gorgonTail)+len(seed))
	out = append(out, gorgonTail[:]...)
	out[2], out[3] = head2, head3
	out = append(out, seed[:]...)
	return hex.EncodeToString(out)
}
