package sign

import (
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math/bits"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/dawei233/hongguo-next/core/danmaku/sign/sm3"
)

// 本文件是**离线**回归：签名算法没有官方文档，唯一能证明移植正确的办法是拿已验证
// 可用的参考实现（nas-backend/danmaku.py）在固定输入下的输出逐字节比对。
//
// 向量文件 core/danmaku/testdata/vectors.json 是从 C 的
// scripts/fixtures/danmaku-sign-vectors.json **原样搬入**的（sha256 一致），
// 其 source 字段记录来源。**不要自造向量**：自己算出来的期望值只能证明"实现与
// 自己一致"，证明不了"与参考实现一致"。
//
// 这个测试失败时不要去联网碰运气 —— 先修这里。

type vectorFixture struct {
	Source  string   `json:"source"`
	Vectors []vector `json:"vectors"`
}

type vector struct {
	Aid          string `json:"aid"`
	DeviceID     string `json:"device_id"`
	InstallID    string `json:"install_id"`
	NonceHex     string `json:"nonce_hex"`
	RawQuery     string `json:"raw_query"`
	Stamp        int64  `json:"stamp"`
	StampMS      int64  `json:"stamp_ms"`
	XArgus       string `json:"x_argus"`
	XGorgon      string `json:"x_gorgon"`
	XKhronos     string `json:"x_khronos"`
	XLadon       string `json:"x_ladon"`
	XSSReqTicket string `json:"x_ss_req_ticket"`
	XSSStub      string `json:"x_ss_stub"`
}

func loadVectors(t *testing.T) vectorFixture {
	t.Helper()
	path := filepath.Join("..", "testdata", "vectors.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取黄金向量 %s 失败: %v", path, err)
	}
	var fixture vectorFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("解析黄金向量失败: %v", err)
	}
	return fixture
}

func (v vector) nonce(t *testing.T) [NonceBytes]byte {
	t.Helper()
	decoded, err := hex.DecodeString(v.NonceHex)
	if err != nil {
		t.Fatalf("向量 nonce_hex %q 不是十六进制: %v", v.NonceHex, err)
	}
	if len(decoded) != NonceBytes {
		t.Fatalf("向量 nonce 长度 %d != %d", len(decoded), NonceBytes)
	}
	var nonce [NonceBytes]byte
	copy(nonce[:], decoded)
	return nonce
}

func (v vector) label() string {
	return "stamp=" + strconv.FormatInt(v.Stamp, 10) + " nonce=" + v.NonceHex
}

// TestVectorFixtureRecordsSource 保证我们用的是有出处的期望值。
func TestVectorFixtureRecordsSource(t *testing.T) {
	fixture := loadVectors(t)
	if fixture.Source != "nas-backend/danmaku.py" {
		t.Fatalf("向量来源应为 nas-backend/danmaku.py，实际 %q —— 来路不明的期望值不能用", fixture.Source)
	}
	if len(fixture.Vectors) < 3 {
		t.Fatalf("向量至少要有 3 组，实际 %d 组", len(fixture.Vectors))
	}
}

// TestBuildQueryMatchesReference 锁住参数集合、排序与转义规则。
//
// 签名是对这个字符串逐字节算的：少一个参数、顺序变了、或者 resolution 里的 '*'
// 没有被转义成 %2A，都会算出服务端拒绝的签名。
func TestBuildQueryMatchesReference(t *testing.T) {
	for _, item := range loadVectors(t).Vectors {
		actual := BuildQuery(item.StampMS, item.DeviceID, item.InstallID)
		if actual != item.RawQuery {
			t.Fatalf("%s 的 raw query 不一致\n实际: %s\n期望: %s", item.label(), actual, item.RawQuery)
		}
	}
}

// TestXGorgonMatchesReference 逐字节回归 X-Gorgon。
func TestXGorgonMatchesReference(t *testing.T) {
	for _, item := range loadVectors(t).Vectors {
		actual := XGorgon(item.RawQuery, item.Stamp, item.nonce(t))
		if actual != item.XGorgon {
			t.Fatalf("%s 的 X-Gorgon 不一致\n实际: %s\n期望: %s", item.label(), actual, item.XGorgon)
		}
	}
}

// TestLadonMatchesReference 逐字节回归 X-Ladon（注意它拿的是 nonce[2:6]）。
func TestLadonMatchesReference(t *testing.T) {
	for _, item := range loadVectors(t).Vectors {
		nonce := item.nonce(t)
		actual := Ladon(item.Aid, item.Stamp, nonce[2:6])
		if actual != item.XLadon {
			t.Fatalf("%s 的 X-Ladon 不一致\n实际: %s\n期望: %s", item.label(), actual, item.XLadon)
		}
	}
}

// TestArgusMatchesReference 逐字节回归 X-Argus（SM3 + protobuf + AES 全在这条路上）。
func TestArgusMatchesReference(t *testing.T) {
	for _, item := range loadVectors(t).Vectors {
		actual, err := Argus(item.RawQuery, item.Stamp, item.nonce(t))
		if err != nil {
			t.Fatalf("%s 的 X-Argus 计算失败: %v", item.label(), err)
		}
		if actual != item.XArgus {
			t.Fatalf("%s 的 X-Argus 不一致\n实际: %s\n期望: %s", item.label(), actual, item.XArgus)
		}
	}
}

// TestSignCommentMatchesReference 回归整组头部，并锁定头部集合本身。
//
// 头部集合"没有多也没有少"要显式断言：多一个头会被上游风控判定为异常客户端，
// 少一个头则直接 4xx。
func TestSignCommentMatchesReference(t *testing.T) {
	for _, item := range loadVectors(t).Vectors {
		headers, err := SignComment(Params{
			RawQuery: item.RawQuery,
			StampS:   item.Stamp,
			StampMS:  item.StampMS,
			Aid:      item.Aid,
			Nonce:    item.nonce(t),
		})
		if err != nil {
			t.Fatalf("%s 签名失败: %v", item.label(), err)
		}
		if headers.XGorgon != item.XGorgon {
			t.Fatalf("%s 的 X-Gorgon 不一致", item.label())
		}
		if headers.XLadon != item.XLadon {
			t.Fatalf("%s 的 X-Ladon 不一致", item.label())
		}
		if headers.XArgus != item.XArgus {
			t.Fatalf("%s 的 X-Argus 不一致", item.label())
		}
		if headers.XKhronos != item.XKhronos {
			t.Fatalf("%s 的 X-Khronos = %q，期望 %q", item.label(), headers.XKhronos, item.XKhronos)
		}
		if headers.XSSReqTicket != item.XSSReqTicket {
			t.Fatalf("%s 的 X-SS-Req-Ticket = %q，期望 %q", item.label(), headers.XSSReqTicket, item.XSSReqTicket)
		}
		if headers.XSSStub != item.XSSStub {
			t.Fatalf("%s 的 X-SS-STUB = %q，期望 %q", item.label(), headers.XSSStub, item.XSSStub)
		}
		names := make([]string, 0, len(headers.Map()))
		for name := range headers.Map() {
			names = append(names, name)
		}
		sort.Strings(names)
		if strings.Join(names, ",") != strings.Join(HeaderNames, ",") {
			t.Fatalf("%s 的头部集合不符: %v，期望 %v", item.label(), names, HeaderNames)
		}
	}
}

// TestXSSReqTicketFollowsReferenceRule 锁住"/秒 × 1000/ 而不是毫秒原值"这条规则。
//
// 向量第 3 组刻意让两者不同：stamp_ms = 1234567890123（query 里的 _rticket），
// 而 x_ss_req_ticket = 1234567890000（秒 × 1000）。传毫秒原值就会不一致。
//
// 顺带记录一处**参考实现之间的差异**：core 扁平包里既有的
// signHongguoCommentRequest 把 X-SS-Req-Ticket 写成 now.UnixMilli()（带亚秒位），
// 而向量来源 nas-backend/danmaku.py 的 _sign_comment_headers 写的是
// str(int(stamp) * 1000)。本包以黄金向量为准；M3 把内核切到生产链路前应当先做一次
// 联网验证，确认服务端对亚秒位不敏感（否则要在 core 侧一并改）。
func TestXSSReqTicketFollowsReferenceRule(t *testing.T) {
	const stamp = int64(1234567890)
	const stampMS = int64(1234567890123)
	if stampMS == stamp*1000 {
		t.Fatal("这条测试需要一个非整秒的毫秒时间戳")
	}
	query := BuildQuery(stampMS, "1000000000000000001", "1000000000000000002")
	headers, err := SignComment(Params{RawQuery: query, StampS: stamp, StampMS: stampMS, Nonce: [NonceBytes]byte{}})
	if err != nil {
		t.Fatalf("签名失败: %v", err)
	}
	if headers.XSSReqTicket != strconv.FormatInt(stamp*1000, 10) {
		t.Fatalf("X-SS-Req-Ticket = %q，期望秒 × 1000 = %d", headers.XSSReqTicket, stamp*1000)
	}
	if headers.XKhronos != strconv.FormatInt(stamp, 10) {
		t.Fatalf("X-Khronos = %q，期望秒级 %d", headers.XKhronos, stamp)
	}
	if !strings.Contains(query, "_rticket="+strconv.FormatInt(stampMS, 10)) {
		t.Fatalf("query 里的 _rticket 必须是毫秒原值：%s", query)
	}
	if _, err := SignComment(Params{RawQuery: query, StampS: 1, StampMS: stampMS, Nonce: [NonceBytes]byte{}}); err == nil {
		t.Fatal("秒级与毫秒级时间戳不自洽时必须报错，而不是算出一个服务端会拒绝的签名")
	}
}

// TestXGorgonKSAMustNotSwap 是硬约束 1 的护栏（§5.1 明确要求本仓把它写成断言）。
//
// 参考实现的 RC4 类密钥调度是 `box[i] = box[j]` **赋值**而不是标准 RC4 的交换。
// 这看着像 bug，但服务端接受它；改成"正确"的交换会让签名失效。
// 因此这里同时断言两件事：
//   - 本实现与黄金向量一致（说明非交换那版才是对的）；
//   - 把 KSA 换成标准交换后结果会变（说明"顺手修好"确实会破坏签名）。
func TestXGorgonKSAMustNotSwap(t *testing.T) {
	fixture := loadVectors(t)
	item := fixture.Vectors[0]
	nonce := item.nonce(t)

	actual := XGorgon(item.RawQuery, item.Stamp, nonce)
	if actual != item.XGorgon {
		t.Fatalf("非交换版与黄金向量不一致：%s", actual)
	}
	swapped := xGorgonWithStandardKSA(item.RawQuery, item.Stamp, nonce)
	if swapped == actual {
		t.Fatal("把 KSA 改成标准交换后签名没有变化 —— 说明这段逻辑被改写了，或者测试本身失去了鉴别力")
	}
}

// xGorgonWithStandardKSA 是"顺手修好版"：除 KSA 用标准交换外与 XGorgon 完全相同。
// 它只存在于测试里，用来证明两条路径确实不同。
func xGorgonWithStandardKSA(rawQuery string, stampS int64, nonce [NonceBytes]byte) string {
	head2 := (nonce[0]%7 + 1) << 5
	head3 := 0xe0 | (nonce[1] & 0x0f)
	var seed [20]byte
	digest := md5.Sum([]byte(rawQuery))
	copy(seed[0:4], digest[0:4])
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
		box[index], box[j] = box[j], box[index] // ← 标准交换（参考实现不是这样）
	}
	j = 0
	for offset := range seed {
		index := byte(offset + 1)
		j += box[index]
		box[index], box[j] = box[j], box[index]
		seed[offset] ^= box[box[index]+box[j]]
	}
	for index := range seed {
		seed[index] = bits.Reverse8(bits.RotateLeft8(seed[index], 4)^seed[(index+1)%len(seed)]) ^ 0xeb
	}
	out := make([]byte, 0, 26)
	out = append(out, 0x84, 0x04, head2, head3, 0x00, 0x00)
	out = append(out, seed[:]...)
	return hex.EncodeToString(out)
}

// Test64BitTruncation 是硬约束 2 的护栏（§5.1 明确要求本仓把它写成断言）。
//
// 位运算的宽度与截断位置是协议的一部分：
//   - X-Gorgon 把时间戳按**大端 32 位**写进 seed，因此只差第 33 位及以上的两个
//     时间戳必须得到完全相同的签名（多出的高位被丢弃，不是被忽略）；
//   - 但只差 1 秒必须改变签名（否则签名的时效性就没了）；
//   - X-Ladon / X-Argus 走 64 位路径（Ladon 是十进制字符串、Argus 是 ×2 的
//     uint64），所以第 33 位对它们是有效的 —— 这正是"截断位置"必须锁死的证据。
func Test64BitTruncation(t *testing.T) {
	const base = int64(1760000000)
	nonce := [NonceBytes]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	query := BuildQuery(base*1000, "1000000000000000001", "1000000000000000002")

	reference := XGorgon(query, base, nonce)
	if XGorgon(query, base+(1<<32), nonce) != reference {
		t.Fatal("X-Gorgon 的时间戳未被截断到 32 位 —— 位宽被改宽了")
	}
	if XGorgon(query, base+1, nonce) == reference {
		t.Fatal("时间戳变化必须改变 X-Gorgon")
	}

	ladonReference := Ladon(AidDefault, base, nonce[2:6])
	if Ladon(AidDefault, base+(1<<32), nonce[2:6]) == ladonReference {
		t.Fatal("X-Ladon 应走 64 位路径（时间戳以十进制字符串进明文），第 33 位必须有效")
	}
	argusReference, err := Argus(query, base, nonce)
	if err != nil {
		t.Fatalf("X-Argus 计算失败: %v", err)
	}
	shifted, err := Argus(query, base+(1<<32), nonce)
	if err != nil {
		t.Fatalf("X-Argus 计算失败: %v", err)
	}
	if shifted == argusReference {
		t.Fatal("X-Argus 走 uint64（时间戳 ×2），第 33 位必须有效")
	}
}

// TestNonceContractIsElevenBytes 文档化 nonce 契约。
//
// 各段分别喂给不同算法，长度不对**不会**报错，只会静默产生服务端拒绝的签名；
// 本包用定长数组把这条契约交给类型系统，这里补一条"零值 nonce 也能算出结果而不是
// panic"的断言，以及长度常量的断言。
func TestNonceContractIsElevenBytes(t *testing.T) {
	if NonceBytes != 11 {
		t.Fatalf("nonce 契约是 11 字节，实际 %d", NonceBytes)
	}
	if got := XGorgon("a=1", 1, [NonceBytes]byte{}); got == "" {
		t.Fatal("零值 nonce 也必须能算出签名")
	}
	generated, err := NewNonce()
	if err != nil {
		t.Fatalf("生成 nonce 失败: %v", err)
	}
	if len(generated) != NonceBytes {
		t.Fatalf("生成的 nonce 长度 %d != %d", len(generated), NonceBytes)
	}
}

// TestNewDeviceIDIsNineteenDigits 锁住设备指纹形态（19 位数字，两次不同）。
func TestNewDeviceIDIsNineteenDigits(t *testing.T) {
	first, err := NewDeviceID()
	if err != nil {
		t.Fatalf("生成设备指纹失败: %v", err)
	}
	second, err := NewDeviceID()
	if err != nil {
		t.Fatalf("生成设备指纹失败: %v", err)
	}
	for _, value := range []string{first, second} {
		if len(value) != 19 {
			t.Fatalf("设备指纹应为 19 位数字串，实际 %q", value)
		}
		if _, err := strconv.ParseUint(value, 10, 64); err != nil {
			t.Fatalf("设备指纹应为纯数字，实际 %q", value)
		}
	}
	if first == second {
		t.Fatal("两次生成的设备指纹不应相同")
	}
}

// TestSignatureVariesWithQueryTimestampAndNonceSegments 防退化。
//
// 如果哪天签名变成常量（例如摘要被短路、或 nonce 段被整体替换），黄金向量之外的
// 输入就会静默全错。这里同时锁住 nonce 的**分段**使用：
// nonce[0] 只影响 Gorgon，不影响 Ladon；nonce[10] 只影响 Argus。
func TestSignatureVariesWithQueryTimestampAndNonceSegments(t *testing.T) {
	nonce := [NonceBytes]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	base := XGorgon("a=1", 1760000000, nonce)
	if base == XGorgon("a=2", 1760000000, nonce) {
		t.Fatal("query 变化必须改变 X-Gorgon")
	}
	if base == XGorgon("a=1", 1760000001, nonce) {
		t.Fatal("时间戳变化必须改变 X-Gorgon")
	}

	flippedHead := nonce
	flippedHead[0] ^= 0x01
	if base == XGorgon("a=1", 1760000000, flippedHead) {
		t.Fatal("nonce[0] 参与 X-Gorgon")
	}
	if Ladon(AidDefault, 1760000000, nonce[2:6]) != Ladon(AidDefault, 1760000000, flippedHead[2:6]) {
		t.Fatal("nonce[0] 不应影响 X-Ladon")
	}
	argusBase, err := Argus("a=1", 1760000000, nonce)
	if err != nil {
		t.Fatalf("X-Argus 计算失败: %v", err)
	}
	argusHead, err := Argus("a=1", 1760000000, flippedHead)
	if err != nil {
		t.Fatalf("X-Argus 计算失败: %v", err)
	}
	if argusBase != argusHead {
		t.Fatal("nonce[0] 不应影响 X-Argus（它只用 [6:11]）")
	}

	flippedTail := nonce
	flippedTail[10] ^= 0x01
	if base != XGorgon("a=1", 1760000000, flippedTail) {
		t.Fatal("nonce[10] 不参与 X-Gorgon")
	}
	if Ladon(AidDefault, 1760000000, nonce[2:6]) != Ladon(AidDefault, 1760000000, flippedTail[2:6]) {
		t.Fatal("nonce[10] 不应影响 X-Ladon")
	}
	argusTail, err := Argus("a=1", 1760000000, flippedTail)
	if err != nil {
		t.Fatalf("X-Argus 计算失败: %v", err)
	}
	if argusBase == argusTail {
		t.Fatal("nonce[10] 参与 X-Argus")
	}
}

// TestSM3MatchesStandardVectors 用标准向量锁住 SM3（含跨多分组的 10000 字节输入）。
//
// 长输入是必须的：只有它能抓出填充与消息扩展的边界错误。C 的测试在这里还与
// 运行时的 native SM3 双向比对；Go 侧没有可依赖的 SM3（标准库不提供，x/crypto 也
// 没有），因此"第二个独立实现"的交叉回归放在 parity_test.go 里与 core 扁平包的
// hongguoSM3 对比。
func TestSM3MatchesStandardVectors(t *testing.T) {
	vectors := []struct{ input, expected string }{
		{"abc", "66c7f0f462eeedd9d1f2d46bdc10e4e24167c4875cf2f7a2297da02b8f4ba8e0"},
		{strings.Repeat("abcd", 16), "debe9ff92275b8a138604889c18e5a4d6fdb70e5387e5765293dcba39c0c5732"},
		{strings.Repeat("x", 10000), "335de0cbf893f7af9a3e548067c72f47630b9fa9293d93959c79c2085fab7e5e"},
		{"", "1ab21d8355cfa17f8e61194831e81a8f22bec8c728fefb747ed035eb5082aa2b"},
	}
	for _, item := range vectors {
		actual := sm3.SumHex([]byte(item.input))
		if actual != item.expected {
			t.Fatalf("SM3(%d 字节) = %s，期望 %s", len(item.input), actual, item.expected)
		}
	}
}

// TestSM3ChunkingInvariance 证明流式 API 与一次性 API 等价。
//
// 长度取 55/56/63/64/65 是因为它们正好压着"填充后是否多出一个分组"的分界，
// 是这类实现最容易写错的地方；再配上不对齐的分片（1/7/63 字节）喂数据，
// 能覆盖"写满一块再续一块"的路径。
func TestSM3ChunkingInvariance(t *testing.T) {
	for _, length := range []int{0, 1, 55, 56, 57, 63, 64, 65, 127, 128, 1000} {
		data := make([]byte, length)
		for index := range data {
			data[index] = byte((index*37 + 11) & 0xff)
		}
		expected := sm3.Sum(data)
		for _, chunk := range []int{1, 7, 63, 64, 65} {
			streaming := sm3.New()
			for offset := 0; offset < len(data); offset += chunk {
				end := offset + chunk
				if end > len(data) {
					end = len(data)
				}
				if _, err := streaming.Write(data[offset:end]); err != nil {
					t.Fatalf("写入失败: %v", err)
				}
			}
			if actual := hex.EncodeToString(streaming.Sum(nil)); actual != hex.EncodeToString(expected[:]) {
				t.Fatalf("长度 %d 按 %d 字节分片时 SM3 不一致: %s", length, chunk, actual)
			}
		}
	}
}
