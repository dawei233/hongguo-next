package sign

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/bits"

	aesutil "github.com/dawei233/hongguo-next/core/danmaku/sign/aes"
	"github.com/dawei233/hongguo-next/core/danmaku/sign/sm3"
)

// argusKeyHex 是参考实现里硬编码的 32 字节密钥（十六进制形态）。
const argusKeyHex = "ac1adaae95a7af94a5114ab3b3a97dd80050aa0a39314c40528caec95256c28c"

// argusRounds 是轮数：72 轮。
const argusRounds = 72

// argusMultiplier 是轮常量生成器的 64 位种子（每轮取不同的位）。
const argusMultiplier = uint64(0x3dc94c3a046d678b)

// argusPrefix 是容器前缀（9 字节魔数）。
var argusPrefix = []byte{0xa6, 0xe7, 0x83, 0xee, 0x70, 0x01, 0x10, 0x09, 0x18}

// argusXor 是加密前逐字节异或的 8 字节掩码，同时也被放到容器最前面。
var argusXor = []byte{0xd0, 0x4f, 0xfd, 0xff, 0xd0, 0x4f, 0xfd, 0xff}

// argusOutput 是输出前缀（"<|" 的字节形态）。
var argusOutput = []byte{0x3c, 0xcc}

// argusKey 是解码后的密钥；长度一定正确，解码失败只可能是编译期常量被改坏。
var argusKey = func() [32]byte {
	var key [32]byte
	decoded, err := hex.DecodeString(argusKeyHex)
	if err != nil || len(decoded) != len(key) {
		panic("danmaku/sign: ARGUS_KEY 必须是 32 字节十六进制常量")
	}
	copy(key[:], decoded)
	return key
}()

// Argus 计算 X-Argus 头。
//
// 与 C 的 danmaku-sign.js commentArgus、core 的 hongguoCommentArgus 逐字节一致。
// 流程分五段，每一段都是参考实现里踩出来的，不能简化：
//
//  1. **手写 protobuf 组包**：21 个字段（含一个嵌套消息），全用变长整数/长度前缀
//     编码，不是 protobuf 库生成的代码 —— 字段号与取值都是协议的一部分。
//  2. **摘要字段**：字段 13/14 分别是 SM3(空 16 字节) 与 SM3(rawQuery) 的**前 6 字节**。
//     rawQuery 参与签名，所以 query 与签名必须由同一次请求构造（见 SignComment）。
//  3. **密钥流派生**：SM3(key‖0x3CC C567B‖key) 当种子，前 4 轮直接取种子的小端
//     uint64，其余 68 轮由非线性递推生成，递推里那一项
//     (0x3DC94C3A046D678B >> ((i-4)%62)) & 1 是参考实现的常量表。
//  4. **72 轮块变换**：对每个 16 字节块做 left/right 交替的 Feistel 式混合。
//  5. **封装**：整体异或掩码 → 前置掩码后整体**反转** → 套 9 字节前缀与 0x56 0x7B
//     结尾 → PKCS#7 补齐 → AES-128-CBC（**不再追加填充**）→ base64。
//
// nonce 只用第 7~11 字节：nonce[6:10] 当作小端 uint32 进字段 3，
// nonce[10] 决定嵌套消息里的取值档位。
func Argus(rawQuery string, stampS int64, nonce [NonceBytes]byte) (string, error) {
	key := argusKey

	var message []byte
	varint := func(field, value uint64) {
		message = binary.AppendUvarint(message, field*8)
		message = binary.AppendUvarint(message, value)
	}
	bytesField := func(field uint64, value []byte) {
		message = binary.AppendUvarint(message, field*8+2)
		message = binary.AppendUvarint(message, uint64(len(value)))
		message = append(message, value...)
	}

	varint(1, 0x20200929*2)
	varint(2, 2)
	varint(3, uint64(binary.LittleEndian.Uint32(nonce[6:10])))
	bytesField(4, []byte("3019"))
	bytesField(6, []byte("1611921764"))
	bytesField(7, []byte("6.8.1.32"))
	bytesField(8, []byte("v04.07.01-ml-android"))
	varint(9, 135135744)
	bytesField(10, make([]byte, 8))
	varint(12, uint64(stampS)*2)

	bodyHash := sm3.Sum(make([]byte, aesutil.BlockSize))
	queryHash := sm3.Sum([]byte(rawQuery))
	bytesField(13, bodyHash[:6])
	bytesField(14, queryHash[:6])

	nested := []byte{0x08, byte(2 + (nonce[10]%49)*2)}
	for _, field := range []uint64{2, 3, 4} {
		nested = binary.AppendUvarint(nested, field*8)
		nested = binary.AppendUvarint(nested, 1388734)
	}
	bytesField(15, nested)
	bytesField(20, []byte("none"))
	varint(21, 738)

	data := aesutil.PadPKCS7(message, aesutil.BlockSize)

	seed := make([]byte, 0, len(key)*2+4)
	seed = append(seed, key[:]...)
	seed = append(seed, 0x3c, 0xcc, 0x56, 0x7b)
	seed = append(seed, key[:]...)
	derived := sm3.Sum(seed)

	var rounds [argusRounds]uint64
	for index := 0; index < 4; index++ {
		rounds[index] = binary.LittleEndian.Uint64(derived[index*8:])
	}
	for index := 4; index < len(rounds); index++ {
		mixed := bits.RotateLeft64(rounds[index-1], -3) ^ rounds[index-3]
		mixed ^= bits.RotateLeft64(mixed, -1)
		rounds[index] = ^rounds[index-4] ^ mixed ^ ((argusMultiplier >> uint((index-4)%62)) & 1) ^ 3
	}

	for offset := 0; offset < len(data); offset += aesutil.BlockSize {
		left := binary.LittleEndian.Uint64(data[offset:])
		right := binary.LittleEndian.Uint64(data[offset+8:])
		for _, round := range rounds {
			next := left ^ (bits.RotateLeft64(right, 1) & bits.RotateLeft64(right, 8)) ^
				bits.RotateLeft64(right, 2) ^ round
			left, right = right, next
		}
		binary.LittleEndian.PutUint64(data[offset:], left)
		binary.LittleEndian.PutUint64(data[offset+8:], right)
	}

	for index := range data {
		data[index] ^= argusXor[index%len(argusXor)]
	}
	reversed := append(append([]byte(nil), argusXor...), data...)
	for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
		reversed[left], reversed[right] = reversed[right], reversed[left]
	}

	container := make([]byte, 0, len(argusPrefix)+len(reversed)+2+aesutil.BlockSize)
	container = append(container, argusPrefix...)
	container = append(container, reversed...)
	container = append(container, 0x56, 0x7b)
	container = aesutil.PadPKCS7(container, aesutil.BlockSize)

	// 密钥与前 16 字节、IV 与后 16 字节分别取 md5 —— 32 字节常量当"两把 md5 原像"
	// 用，这是参考实现的做法，不要换成 AES-256 或直接截断。
	aesKey := md5.Sum(key[:16])
	iv := md5.Sum(key[16:])
	// Raw：容器已经补齐，绝不能再让 CBC 追加填充，否则密文多一个整块、签名失效。
	encrypted, err := aesutil.EncryptCBC128Raw(aesKey[:], iv[:], container)
	if err != nil {
		return "", fmt.Errorf("X-Argus 封装失败: %w", err)
	}
	return base64.StdEncoding.EncodeToString(append(append([]byte(nil), argusOutput...), encrypted...)), nil
}
