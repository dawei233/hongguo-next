// Package sm3 实现国密 SM3 杂凑算法（GB/T 32905-2016）。
//
// # 为什么要自带一份
//
// X-Argus 的两处派生（protobuf 消息体摘要与轮密钥种子）用 SM3，而 Go 标准库
// 没有 SM3。C 的 Electron 版本踩过同一个坑：Electron 内置的是 BoringSSL
// （process.versions.openssl === '0.0.0'），createHash('sm3') 会直接抛
// "Digest method not supported"；在系统 Node 上跑单测看不出来，只有真正调接口
// 才会暴露。因此在 Go 侧同样不能依赖"运行时有就有"——它必须在任何平台上都可用。
//
// 本文件是 C 的 src/native/sm3.js（36 行）的 Go 移植，与 core 扁平包里既有的
// hongguoSM3（core/provider_hongguo_comment_sign.go）互为独立实现，两者在
// 多种长度上的逐字节一致性由 core/danmaku/sign/parity_test.go 交叉回归
// ——这正是 C 的 danmaku-sign.test.cjs 里"与 native 实现双向比对"那条护栏。
//
// 正确性由标准向量（含跨块长输入）与黄金向量（core/danmaku/testdata/vectors.json
// 的 X-Argus）双重锁定：SM3 出错时 X-Argus 必然对不上。
package sm3

import (
	"encoding/binary"
	"encoding/hex"
	"hash"
	"math/bits"
)

// Size 是 SM3 摘要字节数。
const Size = 32

// BlockSize 是 SM3 分组字节数。
const BlockSize = 64

// 初始状态（IV），与标准一致。
var iv = [8]uint32{
	0x7380166f, 0x4914b2b9, 0x172442d7, 0xda8a0600,
	0xa96f30bc, 0x163138aa, 0xe38dee4d, 0xb0fb0e4e,
}

// Sum 返回 data 的 SM3 摘要。
func Sum(data []byte) [Size]byte {
	digest := New()
	digest.Write(data)
	var out [Size]byte
	copy(out[:], digest.Sum(nil))
	return out
}

// SumHex 返回 data 的 SM3 摘要的小写十六进制形态（便于与标准向量比对）。
func SumHex(data []byte) string {
	out := Sum(data)
	return hex.EncodeToString(out[:])
}

// digest 是流式 SM3 状态。它满足 hash.Hash，因为 X-Argus 之外的调用方
// （例如将来的分片校验）可能希望分段喂数据。
type digest struct {
	state    [8]uint32
	buffer   [BlockSize]byte
	buffered int
	length   uint64
}

// New 返回一个新的 SM3 hash.Hash。
func New() hash.Hash {
	return &digest{state: iv}
}

func (d *digest) Size() int      { return Size }
func (d *digest) BlockSize() int { return BlockSize }

// Reset 复位到初始状态。
func (d *digest) Reset() {
	d.state = iv
	d.buffered = 0
	d.length = 0
}

func (d *digest) Write(p []byte) (int, error) {
	written := len(p)
	d.length += uint64(len(p))
	if d.buffered > 0 {
		copied := copy(d.buffer[d.buffered:], p)
		d.buffered += copied
		p = p[copied:]
		if d.buffered == BlockSize {
			d.compress(d.buffer[:])
			d.buffered = 0
		}
	}
	for len(p) >= BlockSize {
		d.compress(p[:BlockSize])
		p = p[BlockSize:]
	}
	if len(p) > 0 {
		d.buffered = copy(d.buffer[:], p)
	}
	return written, nil
}

// Sum 追加当前摘要到 b；它不改变状态（照 hash.Hash 的约定在副本上做填充）。
func (d *digest) Sum(b []byte) []byte {
	clone := *d
	// 填充：先补 0x80，再补 0 到 len%64 == 56，最后放 64 位的比特长度。
	// 这里走 clone.Write 而不是直接拼缓冲，是为了复用同一条压缩路径 ——
	// 边界（55/56/63/64/65 字节）恰好是这类实现最容易写错的地方。
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], clone.length*8)
	clone.Write([]byte{0x80})
	for clone.buffered != BlockSize-8 {
		clone.Write([]byte{0})
	}
	clone.Write(length[:])
	var out [Size]byte
	for index, value := range clone.state {
		binary.BigEndian.PutUint32(out[index*4:], value)
	}
	return append(b, out[:]...)
}

// compress 处理一个 64 字节分组。
//
// 与 C 的 sm3.js 逐行对应：消息扩展 68 个字，然后 64 轮压缩。
func (d *digest) compress(block []byte) {
	var words [68]uint32
	for index := 0; index < 16; index++ {
		words[index] = binary.BigEndian.Uint32(block[index*4:])
	}
	for index := 16; index < len(words); index++ {
		mixed := words[index-16] ^ words[index-9] ^ bits.RotateLeft32(words[index-3], 15)
		words[index] = mixed ^ bits.RotateLeft32(mixed, 15) ^ bits.RotateLeft32(mixed, 23) ^
			bits.RotateLeft32(words[index-13], 7) ^ words[index-6]
	}
	a, b, c, e, f, g, h := d.state[0], d.state[1], d.state[2], d.state[4], d.state[5], d.state[6], d.state[7]
	dd := d.state[3]
	for index := 0; index < 64; index++ {
		// T 常量：前 16 轮与后 48 轮不同；FF/GG 也是前 16 轮用异或、之后用多数/选择。
		t, ff, gg := uint32(0x79cc4519), a^b^c, e^f^g
		if index >= 16 {
			t, ff, gg = 0x7a879d8a, (a&b)|(a&c)|(b&c), (e&f)|(^e&g)
		}
		a12 := bits.RotateLeft32(a, 12)
		ss1 := bits.RotateLeft32(a12+e+bits.RotateLeft32(t, index), 7)
		tt1 := ff + dd + (ss1 ^ a12) + (words[index] ^ words[index+4])
		tt2 := gg + h + ss1 + words[index]
		dd, c, b, a = c, bits.RotateLeft32(b, 9), a, tt1
		h, g, f = g, bits.RotateLeft32(f, 19), e
		e = tt2 ^ bits.RotateLeft32(tt2, 9) ^ bits.RotateLeft32(tt2, 17)
	}
	for index, value := range [8]uint32{a, b, c, dd, e, f, g, h} {
		d.state[index] ^= value
	}
}
