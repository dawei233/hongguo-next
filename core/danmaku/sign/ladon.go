package sign

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"math/bits"
	"strconv"

	aesutil "github.com/dawei233/hongguo-next/core/danmaku/sign/aes"
)

// ladonSeed 是参考实现里硬编码的字符串尾段（与 aid、时间戳拼成明文）。
const ladonSeed = "-1611921764-3019"

// ladonRounds 是密钥扩展轮数：34 轮。
const ladonRounds = 34

// Ladon 计算 X-Ladon 头。
//
// 与 C 的 danmaku-sign.js commentLadon、core 的 hongguoCommentLadon 逐字节一致：
//
//  1. 取 md5(prefix‖aid) 的**十六进制 ASCII 形态**（32 字节）当密钥材料
//     ——注意不是摘要本身，是它的 hex 字符串，这一步最容易漏；
//  2. 把 32 字节切成 4 个**小端** uint64，做 34 轮密钥扩展（同时维护左右两半
//     与一个 2 元素滑动队列）；
//  3. 用扩展出的 34 个轮密钥对 "<时间戳>-1611921764-3019"（PKCS#7 补齐）逐块做
//     TEA 风格变换，明文原地改写；
//  4. 输出 base64(prefix‖密文)。
//
// prefix 是 nonce 的第 3~7 字节（nonce[2:6]）——它是**明文前缀**而不是密钥，
// 所以 base64 开头能直接看到它（黄金向量里 x_ladon 以 "AgMEBQ" 开头）。
//
// 全部运算按 uint64 回绕（Go 的无符号整数天然如此），位宽改变即失效。
func Ladon(aid string, stampS int64, prefix []byte) string {
	material := append(append([]byte(nil), prefix...), []byte(aid)...)
	digest := md5.Sum(material)
	key := []byte(hex.EncodeToString(digest[:]))

	left := binary.LittleEndian.Uint64(key[0:8])
	right := binary.LittleEndian.Uint64(key[8:16])
	queue := []uint64{binary.LittleEndian.Uint64(key[16:24]), binary.LittleEndian.Uint64(key[24:32])}

	var keys [ladonRounds]uint64
	for index := range keys {
		keys[index] = left
		mixed := (bits.RotateLeft64(right, -8) + left) ^ uint64(index)
		queue = append(queue, mixed)
		left = mixed ^ bits.RotateLeft64(left, 3)
		right = queue[0]
		queue = queue[1:]
	}

	data := aesutil.PadPKCS7([]byte(strconv.FormatInt(stampS, 10)+ladonSeed), aesutil.BlockSize)
	for offset := 0; offset < len(data); offset += aesutil.BlockSize {
		blockLeft := binary.LittleEndian.Uint64(data[offset:])
		blockRight := binary.LittleEndian.Uint64(data[offset+8:])
		for _, roundKey := range keys {
			blockRight = roundKey ^ (blockLeft + bits.RotateLeft64(blockRight, -8))
			blockLeft = blockRight ^ bits.RotateLeft64(blockLeft, 3)
		}
		binary.LittleEndian.PutUint64(data[offset:], blockLeft)
		binary.LittleEndian.PutUint64(data[offset+8:], blockRight)
	}
	return base64.StdEncoding.EncodeToString(append(append([]byte(nil), prefix...), data...))
}
