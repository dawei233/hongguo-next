package danmaku

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dawei233/hongguo-next/core/danmaku/sign"
)

// capturedRequest 是假传输层记下的一次请求。
type capturedRequest struct {
	videoID  string
	rawQuery string
	headers  map[string]string
	body     []byte
}

// fakeTransport 是离线假传输层：记录请求并按脚本回放响应体。
type fakeTransport struct {
	requests []capturedRequest
	response []byte
	err      error
}

func (t *fakeTransport) PostComment(ctx context.Context, videoID, rawQuery string, headers map[string]string, body []byte) ([]byte, error) {
	t.requests = append(t.requests, capturedRequest{videoID: videoID, rawQuery: rawQuery, headers: headers, body: body})
	if t.err != nil {
		return nil, t.err
	}
	return t.response, nil
}

// officialFixture 用固定时钟与固定 nonce 造一个可复现的官方源。
func officialFixture(transport Transport) *OfficialSource {
	source, err := NewOfficialSource(OfficialConfig{
		EpisodeID: "hongguo:" + testSeriesID + ":1",
		SeriesID:  testSeriesID,
		DeviceID:  "1000000000000000001",
		InstallID: "1000000000000000002",
		Transport: transport,
		// 与黄金向量第 1 组完全同一组输入：秒 1760000000（毫秒 1760000000000）、
		// device_id / iid 用向量里的那两个 19 位数字、nonce 用向量的 11 字节。
		Now:   func() time.Time { return time.Unix(1760000000, 0) },
		Nonce: func() ([sign.NonceBytes]byte, error) { return vectorNonce(), nil },
	})
	if err != nil {
		panic(err)
	}
	return source
}

// TestOfficialSourceSignsRequestAgainstGoldenVector 校验"发出去的请求就是向量里的请求"。
//
// 这是把黄金向量与生产链路接起来的那一步：向量只证明算法对，这里证明**用它算签名的
// 输入**（query 字符串、时间戳、nonce、aid）与向量一致，并且请求体、路径、协议头都按
// 参考实现来。少了这一层，算法再对也可能因为 query 里少一个参数而 4xx。
func TestOfficialSourceSignsRequestAgainstGoldenVector(t *testing.T) {
	transport := &fakeTransport{response: pageBody("", `{"has_more":false}`)}
	source := officialFixture(transport)
	if _, err := source.FetchChunk(context.Background(), ChunkRequest{VideoID: testVideoID, StartMS: 0, DurationMS: EffectiveDurationMS}); err != nil {
		t.Fatalf("抓取失败: %v", err)
	}
	if len(transport.requests) != 1 {
		t.Fatalf("应发出 1 次请求，实际 %d", len(transport.requests))
	}
	sent := transport.requests[0]

	golden := goldenVector(t, 0)
	expectedQuery := sign.BuildQuery(1760000000000, "1000000000000000001", "1000000000000000002")
	if sent.rawQuery != expectedQuery {
		t.Fatalf("raw query 不符\n实际: %s\n期望: %s", sent.rawQuery, expectedQuery)
	}
	// 最强的一条断言：生产链路发出去的 query 与黄金向量的 raw_query 逐字节一致。
	if sent.rawQuery != golden.RawQuery {
		t.Fatalf("raw query 与黄金向量不一致\n实际: %s\n向量: %s", sent.rawQuery, golden.RawQuery)
	}
	// 向量的 raw_query 用的是 device_id=...001 / iid=...002，除 _rticket 外应与之一致。
	if strings.Contains(expectedQuery, "resolution=1280*2772") {
		t.Fatalf("resolution 里的 * 必须被转义成 %%2A: %s", expectedQuery)
	}
	if !strings.Contains(expectedQuery, "resolution=1280%2A2772") {
		t.Fatalf("resolution 转义不符: %s", expectedQuery)
	}

	expected, err := sign.SignComment(sign.Params{
		RawQuery: expectedQuery,
		StampS:   1760000000,
		StampMS:  1760000000000,
		Nonce:    vectorNonce(),
	})
	if err != nil {
		t.Fatalf("签名失败: %v", err)
	}
	for name, value := range expected.Map() {
		if sent.headers[name] != value {
			t.Fatalf("%s 与直接调用签名的结果不一致: %s != %s", name, sent.headers[name], value)
		}
	}
	if sent.headers["Comment-Source"] != "601" || sent.headers["Server-Channel"] != "1000" {
		t.Fatalf("协议头缺失: %+v", sent.headers)
	}
	for name, value := range map[string]string{
		"X-Gorgon": golden.XGorgon, "X-Ladon": golden.XLadon, "X-Argus": golden.XArgus,
		"X-Khronos": golden.XKhronos, "X-SS-Req-Ticket": golden.XSSReqTicket, "X-SS-STUB": golden.XSSStub,
	} {
		if sent.headers[name] != value {
			t.Fatalf("%s 与黄金向量不一致\n实际: %s\n向量: %s", name, sent.headers[name], value)
		}
	}

	var body map[string]any
	if err := json.Unmarshal(sent.body, &body); err != nil {
		t.Fatalf("请求体不是 JSON: %v", err)
	}
	if body["group_id"] != testVideoID {
		t.Fatalf("请求体的 group_id 必须是视频 ID: %v", body["group_id"])
	}
	business, _ := body["business_param"].(map[string]any)
	if business == nil || business["book_id"] != testSeriesID {
		t.Fatalf("请求体的 book_id 必须是剧 ID: %v", business)
	}
	if business["start_offset_time"] != float64(0) || business["playlet_item_duration"] != float64(EffectiveDurationMS) {
		t.Fatalf("窗口字段不符: %v", business)
	}
}

// TestOfficialSourceEndToEndThroughCollect 用假传输层跑完整条链路（签名 → 请求 →
// 解析 → 合并 → 报告），全程离线。
func TestOfficialSourceEndToEndThroughCollect(t *testing.T) {
	transport := &fakeTransport{response: pageBody("", `{"has_more":false}`,
		commentRow("1", testVideoID, "1", "第一条", "1200"),
		commentRow("2", testVideoID, "1", "第二条", "3400"))}
	source := officialFixture(transport)
	track, report, err := Collect(context.Background(), CollectOptions{
		SeriesID:   testSeriesID,
		VideoID:    testVideoID,
		StartMS:    0,
		DurationMS: EffectiveDurationMS,
		Fetcher:    source,
		Sleep:      func(context.Context, time.Duration) error { return nil },
	})
	if err != nil {
		t.Fatalf("抓取失败: %v", err)
	}
	if len(track.Items) != 2 || track.Items[0].Text != "第一条" || track.Items[1].TimeMS != 3400 {
		t.Fatalf("端到端结果不符: %+v", track.Items)
	}
	if report.Failure != "" || report.Pages != 1 {
		t.Fatalf("报告不符: %+v", report)
	}
	if _, err := json.Marshal(track.ToSourceTrack()); err != nil {
		t.Fatalf("契约映射失败: %v", err)
	}
}

// TestOfficialSourceKeepsDeviceIdentityAcrossRequests 校验设备指纹在进程内复用。
//
// 官方客户端的行为是"安装一次生成一次"：同一进程内每次请求换一个 device_id 会像
// 一个不断重装的客户端，最容易触发风控。
func TestOfficialSourceKeepsDeviceIdentityAcrossRequests(t *testing.T) {
	transport := &fakeTransport{response: pageBody("", `{"has_more":false}`)}
	source, err := NewOfficialSource(OfficialConfig{SeriesID: testSeriesID, Transport: transport})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	for index := 0; index < 3; index++ {
		if _, err := source.FetchChunk(context.Background(), ChunkRequest{VideoID: testVideoID, StartMS: 0, DurationMS: EffectiveDurationMS}); err != nil {
			t.Fatalf("抓取失败: %v", err)
		}
	}
	if len(transport.requests) != 3 {
		t.Fatalf("应发出 3 次请求，实际 %d", len(transport.requests))
	}
	first := transport.requests[0].rawQuery
	for _, request := range transport.requests[1:] {
		if deviceIDOf(t, request.rawQuery) != deviceIDOf(t, first) {
			t.Fatalf("同一进程内的 device_id 必须复用:\n%s\n%s", first, request.rawQuery)
		}
	}
	if len(deviceIDOf(t, first)) != 19 {
		t.Fatalf("device_id 应为 19 位数字串: %s", deviceIDOf(t, first))
	}
}

// TestOfficialSourceRejectsInvalidConfig 校验构造期校验。
func TestOfficialSourceRejectsInvalidConfig(t *testing.T) {
	if _, err := NewOfficialSource(OfficialConfig{SeriesID: "hongguo:1", Transport: &fakeTransport{}}); err == nil {
		t.Fatal("带前缀的剧 ID 必须被拒（否则会算出被上游拒绝的签名）")
	}
	if _, err := NewOfficialSource(OfficialConfig{SeriesID: testSeriesID}); err == nil {
		t.Fatal("缺少传输层必须被拒")
	}
}

// TestOfficialSourcePropagatesTransportFailure 校验传输层失败被原样带出（由 Collect
// 负责翻译成用户能看懂的文案）。
func TestOfficialSourcePropagatesTransportFailure(t *testing.T) {
	transport := &fakeTransport{err: ErrTimeout}
	source := officialFixture(transport)
	if _, err := source.FetchChunk(context.Background(), ChunkRequest{VideoID: testVideoID, DurationMS: EffectiveDurationMS}); err == nil {
		t.Fatal("传输层失败必须带出")
	}
}

// goldenVector 读取 core/danmaku/testdata/vectors.json 的第 index 组向量
// （原样搬自 C 的 scripts/fixtures/danmaku-sign-vectors.json）。
func goldenVector(t *testing.T, index int) officialVector {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "vectors.json"))
	if err != nil {
		t.Fatalf("读取黄金向量失败: %v", err)
	}
	var fixture struct {
		Vectors []officialVector `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("解析黄金向量失败: %v", err)
	}
	if index >= len(fixture.Vectors) {
		t.Fatalf("黄金向量只有 %d 组，取不到第 %d 组", len(fixture.Vectors), index)
	}
	return fixture.Vectors[index]
}

// officialVector 是黄金向量的线格式（字段名与 C 的文件一致）。
type officialVector struct {
	NonceHex     string `json:"nonce_hex"`
	RawQuery     string `json:"raw_query"`
	XArgus       string `json:"x_argus"`
	XGorgon      string `json:"x_gorgon"`
	XKhronos     string `json:"x_khronos"`
	XLadon       string `json:"x_ladon"`
	XSSReqTicket string `json:"x_ss_req_ticket"`
	XSSStub      string `json:"x_ss_stub"`
}

// vectorNonce 返回黄金向量第 1 组的 nonce（000102…0a）。
func vectorNonce() [sign.NonceBytes]byte {
	var nonce [sign.NonceBytes]byte
	for index := range nonce {
		nonce[index] = byte(index)
	}
	return nonce
}

func deviceIDOf(t *testing.T, rawQuery string) string {
	t.Helper()
	for _, pair := range strings.Split(rawQuery, "&") {
		if strings.HasPrefix(pair, "device_id=") {
			return strings.TrimPrefix(pair, "device_id=")
		}
	}
	t.Fatalf("query 里没有 device_id: %s", rawQuery)
	return ""
}
