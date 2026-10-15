// 本文件是**跨包**交叉回归：把 M2 的新签名实现与 core 扁平包里既有的实现
// （core/provider_hongguo_comment_sign.go，B 的移植）逐字节比对。
//
// 为什么测试包是 sign_test（外部测试包）而不是 sign：core 通过
// core/danmaku_bridge.go import 了本包，因此"本包的测试"要 import core 就只能走
// 外部测试包 —— Go 明确允许外部测试包 import 依赖被测包的包，这是它的用途之一。
//
// 为什么需要这一层：黄金向量只覆盖 3 组固定输入（而且来自 A 的 Python），
// 它证明不了"B 的 Go 实现与本仓新实现在所有输入上都一致"。两者同源但各自独立
// 编写，漂移是真实风险（例如某一侧把 32 位截断写成 64 位）。
package sign_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/dawei233/hongguo-next/core"
	"github.com/dawei233/hongguo-next/core/danmaku/sign"
	"github.com/dawei233/hongguo-next/core/danmaku/sign/sm3"
)

// TestSignHeadersParityWithFlatCore 随机输入下与扁平包的既有实现逐字节比对。
func TestSignHeadersParityWithFlatCore(t *testing.T) {
	random := rand.New(rand.NewSource(20261015))
	for round := 0; round < 64; round++ {
		stampS := int64(1_600_000_000 + random.Intn(600_000_000))
		var nonce [sign.NonceBytes]byte
		random.Read(nonce[:])
		deviceID := "100000000000000000" + string(rune('0'+round%10))
		installID := "100000000000000000" + string(rune('0'+(round+1)%10))
		rawQuery := sign.BuildQuery(stampS*1000, deviceID, installID)

		headers, err := sign.SignComment(sign.Params{
			RawQuery: rawQuery,
			StampS:   stampS,
			StampMS:  stampS * 1000,
			Nonce:    nonce,
		})
		if err != nil {
			t.Fatalf("第 %d 轮新实现签名失败: %v", round, err)
		}
		legacy := core.DanmakuLegacySignHeaders(rawQuery, stampS, nonce)
		if legacy == nil {
			t.Fatalf("第 %d 轮扁平包实现返回空", round)
		}
		for _, name := range sign.HeaderNames {
			if headers.Map()[name] != legacy[name] {
				t.Fatalf("第 %d 轮 %s 不一致\n新实现: %s\n扁平包: %s", round, name, headers.Map()[name], legacy[name])
			}
		}
	}
}

// TestSignHeadersParityOnVectors 让扁平包实现也过一遍黄金向量。
//
// 这样一来，if 哪天有人只改了一侧，"向量回归"与"交叉回归"会同时报警。
func TestSignHeadersParityOnVectors(t *testing.T) {
	fixture := loadVectorsForParity(t)
	for _, item := range fixture {
		var nonce [sign.NonceBytes]byte
		decoded, err := hex.DecodeString(item.NonceHex)
		if err != nil || len(decoded) != sign.NonceBytes {
			t.Fatalf("向量 nonce 非法: %q", item.NonceHex)
		}
		copy(nonce[:], decoded)
		legacy := core.DanmakuLegacySignHeaders(item.RawQuery, item.Stamp, nonce)
		if legacy["X-Gorgon"] != item.XGorgon {
			t.Fatalf("扁平包的 X-Gorgon 与黄金向量不一致: %s != %s", legacy["X-Gorgon"], item.XGorgon)
		}
		if legacy["X-Ladon"] != item.XLadon {
			t.Fatalf("扁平包的 X-Ladon 与黄金向量不一致: %s != %s", legacy["X-Ladon"], item.XLadon)
		}
		if legacy["X-Argus"] != item.XArgus {
			t.Fatalf("扁平包的 X-Argus 与黄金向量不一致: %s != %s", legacy["X-Argus"], item.XArgus)
		}
		if legacy["X-SS-Req-Ticket"] != item.XSSReqTicket {
			t.Fatalf("扁平包的 X-SS-Req-Ticket 与黄金向量不一致: %s != %s", legacy["X-SS-Req-Ticket"], item.XSSReqTicket)
		}
	}
}

// TestSM3ParityWithFlatCore 在多种长度上与扁平包的 SM3 交叉回归。
//
// 这正是 C 的 danmaku-sign.test.cjs 里"与运行时 native SM3 双向比对"那条护栏的
// Go 版本：长度取 55/56/63/64/65 是因为它们压着填充分界，而 1000 字节覆盖多分组。
func TestSM3ParityWithFlatCore(t *testing.T) {
	for _, length := range []int{0, 1, 55, 56, 57, 63, 64, 65, 127, 128, 1000} {
		data := make([]byte, length)
		for index := range data {
			data[index] = byte((index*37 + 11) & 0xff)
		}
		legacy := core.DanmakuLegacySM3(data)
		if actual := sm3.Sum(data); !bytes.Equal(actual[:], legacy[:]) {
			t.Fatalf("长度 %d 的 SM3 与扁平包不一致: %x != %x", length, actual, legacy)
		}
	}
}

// TestDeviceIDParityWithFlatCore 两种设备指纹生成规则都必须是 19 位数字串。
func TestDeviceIDParityWithFlatCore(t *testing.T) {
	for _, value := range []string{core.DanmakuLegacyDeviceID(), core.DanmakuLegacyDeviceID()} {
		if len(value) != 19 {
			t.Fatalf("扁平包设备指纹应为 19 位数字串，实际 %q", value)
		}
	}
	newValue, err := sign.NewDeviceID()
	if err != nil {
		t.Fatalf("新实现生成设备指纹失败: %v", err)
	}
	if len(newValue) != 19 {
		t.Fatalf("新实现设备指纹应为 19 位数字串，实际 %q", newValue)
	}
}

// TestCommentBodyParityWithFlatCore 请求体必须与扁平包既有实现逐字段一致。
//
// 上游对 body 里的 group_id / book_id / start_offset_time 都敏感，两边分叉会出现
// "新链路拿不到弹幕、旧链路能拿到"这种最难查的故障。
func TestCommentBodyParityWithFlatCore(t *testing.T) {
	body := core.DanmakuCommentBody("7123456789012345678", "7123456789012345679", 30000, 3600000)
	want := map[string]any{
		"comment_source": 601, "server_channel": 1000,
		"group_id": "7123456789012345678", "group_type": 30, "comment_type": 20,
		"sort": 1, "count": 90, "cursor": "", "aid": 8662, "compliance_status": 0,
	}
	for key, expected := range want {
		if body[key] != expected {
			t.Fatalf("请求体字段 %s = %v，期望 %v", key, body[key], expected)
		}
	}
	business, ok := body["business_param"].(map[string]any)
	if !ok {
		t.Fatalf("business_param 缺失或形态不对: %T", body["business_param"])
	}
	businessWant := map[string]any{
		"book_id": "7123456789012345679", "start_offset_time": int64(30000),
		"playlet_item_duration": int64(3600000),
	}
	for key, expected := range businessWant {
		if business[key] != expected {
			t.Fatalf("business_param.%s = %v，期望 %v", key, business[key], expected)
		}
	}
	guide, ok := business["need_danmaku_guide_type"].([]int)
	if !ok || len(guide) != 4 || guide[0] != 1 || guide[1] != 3 || guide[2] != 4 || guide[3] != 2 {
		t.Fatalf("need_danmaku_guide_type 不符: %v", business["need_danmaku_guide_type"])
	}
}

// TestCommentPathAndUserAgentParity 路径与 UA 与扁平包既有实现一致。
func TestCommentPathAndUserAgentParity(t *testing.T) {
	if got := core.DanmakuCommentPath("7123456789012345678"); got != "/novel/commentapi/comment/list/7123456789012345678/v1/" {
		t.Fatalf("弹幕路径不符: %s", got)
	}
	if core.DanmakuUserAgent() == "" {
		t.Fatal("UA 不应为空")
	}
	if !bytes.Contains([]byte(core.DanmakuUserAgent()), []byte("com.phoenix.read")) {
		t.Fatalf("UA 必须是官方客户端形态: %s", core.DanmakuUserAgent())
	}
}

type parityVector struct {
	NonceHex     string `json:"nonce_hex"`
	RawQuery     string `json:"raw_query"`
	Stamp        int64  `json:"stamp"`
	XArgus       string `json:"x_argus"`
	XGorgon      string `json:"x_gorgon"`
	XLadon       string `json:"x_ladon"`
	XSSReqTicket string `json:"x_ss_req_ticket"`
}

func loadVectorsForParity(t *testing.T) []parityVector {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "testdata", "vectors.json"))
	if err != nil {
		t.Fatalf("读取黄金向量失败: %v", err)
	}
	var fixture struct {
		Vectors []parityVector `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("解析黄金向量失败: %v", err)
	}
	if len(fixture.Vectors) == 0 {
		t.Fatal("黄金向量为空")
	}
	return fixture.Vectors
}
