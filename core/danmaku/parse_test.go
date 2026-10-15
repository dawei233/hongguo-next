package danmaku

import (
	"encoding/json"
	"strings"
	"testing"
)

const testVideoID = "7123456789012345678"

// commentRow 拼一行响应数据（字段形态与实测一致：id 是字符串、offset_time 在
// expand 里、状态是字符串 "1"）。
func commentRow(id, groupID, status, content, offset string) string {
	return `{"comment":{"comment_id":"` + id + `","common":{"group_id":"` + groupID +
		`","status":"` + status + `","content":{"text":"` + content + `"}},"expand":{"offset_time":"` + offset + `"}}}`
}

func pageBody(extra, info string, rows ...string) []byte {
	body := `{"code":0,"data":{"data_list":[` + strings.Join(rows, ",") + `]`
	if extra != "" {
		body += `,"extra":` + extra
	}
	if info != "" {
		body += `,"common_list_info":` + info
	}
	return []byte(body + `}}`)
}

// TestParsePageReadsBothPaginationSignals 锁住"分页信号有两个来源"这条教训。
//
// 实测响应里 extra 常常是**空对象**，真正的"还有没有"写在 common_list_info。
// 只认 extra 就会把"还有更多"误判成"没有了"，于是整集只拿到首页 90 条。
func TestParsePageReadsBothPaginationSignals(t *testing.T) {
	onlyExtra, err := ParsePage(pageBody(`{"next_query_danmaku_list_time":"90000"}`, "",
		commentRow("c1", testVideoID, "1", "前排", "1200")), testVideoID)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if onlyExtra.NextMS != 90000 || len(onlyExtra.Items) != 1 {
		t.Fatalf("只读 extra 的路径不符: %+v", onlyExtra)
	}

	onlyInfo, err := ParsePage(pageBody("", `{"has_more":true,"cursor":"{\"end_offset_time\":\"120000\",\"danmaku_count\":321}"}`,
		commentRow("c1", testVideoID, "1", "前排", "1200")), testVideoID)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if onlyInfo.NextMS != 120000 {
		t.Fatalf("extra 为空时必须用 cursor.end_offset_time 续拉，实际 nextMs=%d", onlyInfo.NextMS)
	}
	if !onlyInfo.HasMoreKnown || !onlyInfo.HasMore {
		t.Fatalf("has_more 应为显式 true: %+v", onlyInfo)
	}
	if onlyInfo.Total != 321 {
		t.Fatalf("游标里的总数应被读出，实际 %d", onlyInfo.Total)
	}

	bothSources, err := ParsePage(pageBody(`{"next_query_danmaku_list_time":"90000"}`,
		`{"has_more":true,"cursor":{"next_offset_time":"150000"}}`,
		commentRow("c1", testVideoID, "1", "前排", "1200")), testVideoID)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if bothSources.NextMS != 150000 {
		t.Fatalf("两个来源都给时应取更靠后的那个，实际 %d", bothSources.NextMS)
	}

	// 显式的 has_more:false 优先：此时即便有 cursor 也不该继续翻。
	stopped, err := ParsePage(pageBody("", `{"has_more":false,"cursor":{"end_offset_time":"150000"}}`,
		commentRow("c1", testVideoID, "1", "前排", "1200")), testVideoID)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if stopped.NextMS != 0 {
		t.Fatalf("has_more 为 false 时不应再给下一页游标，实际 %d", stopped.NextMS)
	}
	if !stopped.HasMoreKnown || stopped.HasMore {
		t.Fatalf("has_more 应为显式 false: %+v", stopped)
	}
}

// TestParsePageFiltersOtherVideosAndHiddenStatus 校验脏数据过滤。
func TestParsePageFiltersOtherVideosAndHiddenStatus(t *testing.T) {
	page, err := ParsePage(pageBody("", "",
		commentRow("c1", testVideoID, "1", "正常", "1000"),
		commentRow("c2", "7999999999999999999", "1", "别的视频", "1100"),
		commentRow("c3", testVideoID, "0", "待审核", "1200"),
		commentRow("c4", "", "1", "上游没回 group_id", "1300"),
	), testVideoID)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	texts := make([]string, 0, len(page.Items))
	for _, item := range page.Items {
		texts = append(texts, item.Text)
	}
	want := []string{"正常", "上游没回 group_id"}
	if strings.Join(texts, "|") != strings.Join(want, "|") {
		t.Fatalf("过滤结果不符: %v", texts)
	}
}

// TestParsePageNormalizesColorAndMode 校验颜色/模式的归一（含多 holder 兜底）。
func TestParsePageNormalizesColorAndMode(t *testing.T) {
	body := []byte(`{"code":0,"data":{"data_list":[
		{"comment":{"common":{"group_id":"` + testVideoID + `","status":"1","content":{"text":"数字色","color":16711680}},"expand":{"offset_time":"1000"}}},
		{"comment":{"common":{"group_id":"` + testVideoID + `","status":"1","content":{"text":"井号短色","color":"#abc"}},"expand":{"offset_time":"2000"}}},
		{"comment":{"common":{"group_id":"` + testVideoID + `","status":"1","content":{"text":"十六进制串","color":"0x00FF00"}},"expand":{"offset_time":"3000"}}},
		{"comment":{"common":{"group_id":"` + testVideoID + `","status":"1","content":{"text":"十进制造串","color":"255"}},"expand":{"offset_time":"4000"}}},
		{"comment":{"common":{"group_id":"` + testVideoID + `","status":"1","content":{"text":"顶部","color":-1,"mode":"2"}},"expand":{"offset_time":"5000"}}},
		{"comment":{"common":{"group_id":"` + testVideoID + `","status":"1","content":{"text":"底部"}},"expand":{"offset_time":"6000","fix_type":"底部"}}},
		{"comment":{"common":{"group_id":"` + testVideoID + `","status":"1","content":{"text":"默认"}},"expand":{"offset_time":"7000"}}}
	]}}`)
	page, err := ParsePage(body, testVideoID)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	wantColors := []string{"#FF0000", "#ABC", "#00FF00", "#0000FF", ColorDefault, ColorDefault, ColorDefault}
	wantModes := []int{ModeScroll, ModeScroll, ModeScroll, ModeScroll, ModeTop, ModeBottom, ModeScroll}
	if len(page.Items) != len(wantColors) {
		t.Fatalf("条目数不符: %d", len(page.Items))
	}
	for index, item := range page.Items {
		if item.Color != wantColors[index] {
			t.Fatalf("第 %d 条颜色为 %s，期望 %s", index, item.Color, wantColors[index])
		}
		if item.Mode != wantModes[index] {
			t.Fatalf("第 %d 条模式为 %d，期望 %d", index, item.Mode, wantModes[index])
		}
	}
}

// TestParsePageRejectsStructuralAnomalies 校验"结构异常必须报错"。
//
// C 的 parsePage 对畸形响应静默返回空页；空页会让上层把"拿不到"说成本集暂无弹幕，
// 这正是 §5.2 反复强调不能发生的事。因此这里返回错误。
func TestParsePageRejectsStructuralAnomalies(t *testing.T) {
	cases := map[string][]byte{
		"非 JSON":         []byte("<html>403</html>"),
		"业务错误码":          []byte(`{"code":10051,"data":{"data_list":[]}}`),
		"缺少 data":        []byte(`{"code":0}`),
		"缺少 data_list":   []byte(`{"code":0,"data":{}}`),
		"data_list 不是数组": []byte(`{"code":0,"data":{"data_list":{}}}`),
	}
	for name, payload := range cases {
		if _, err := ParsePage(payload, testVideoID); err == nil {
			t.Fatalf("%s 必须报错，而不是返回空页", name)
		}
	}
	// 空列表本身是合法响应（确实没有弹幕）。
	empty, err := ParsePage(pageBody("", ""), testVideoID)
	if err != nil {
		t.Fatalf("空列表不应报错: %v", err)
	}
	if len(empty.Items) != 0 || empty.NextMS != 0 {
		t.Fatalf("空列表应解析为空页: %+v", empty)
	}
}

// TestParsePageSkipsInvalidTimeline 校验时间字段的边界。
func TestParsePageSkipsInvalidTimeline(t *testing.T) {
	body := []byte(`{"code":0,"data":{"data_list":[
		{"comment":{"common":{"group_id":"` + testVideoID + `","status":"1","content":{"text":"布尔时间"}},"expand":{"offset_time":true}}},
		{"comment":{"common":{"group_id":"` + testVideoID + `","status":"1","content":{"text":"空时间"}},"expand":{"offset_time":""}}},
		{"comment":{"common":{"group_id":"` + testVideoID + `","status":"1","content":{"text":"非数字"}},"expand":{"offset_time":"abc"}}},
		{"comment":{"common":{"group_id":"` + testVideoID + `","status":"1","content":{"text":"负数"}},"expand":{"offset_time":"-5"}}},
		{"comment":{"common":{"group_id":"` + testVideoID + `","status":"1","content":{"text":"超过七天"}},"expand":{"offset_time":"604800001"}}},
		{"comment":{"common":{"group_id":"` + testVideoID + `","status":"1","content":{"text":"没有时间"}},"expand":{}}},
		{"comment":{"common":{"group_id":"` + testVideoID + `","status":"1","content":{"text":"顶层时间兜底"}},"offset_time":900}}
	]}}`)
	page, err := ParsePage(body, testVideoID)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Text != "顶层时间兜底" || page.Items[0].TimeMS != 900 {
		t.Fatalf("非法时间应被跳过、comment.offset_time 应作为兜底: %+v", page.Items)
	}
}

// TestTruncateAndCleanText 校验文本清洗与截断（契约上限 181 字符）。
func TestTruncateAndCleanText(t *testing.T) {
	if got := CleanText("前\u0000排\n\n\n后\u2028排"); got != "前 排 后 排" {
		t.Fatalf("控制字符应换成空格并折叠空白，实际 %q", got)
	}
	if got := CleanText("  前后  "); got != "前后" {
		t.Fatalf("首尾空白应去掉，实际 %q", got)
	}
	short := strings.Repeat("字", 180)
	if got := TruncateText(short); got != short {
		t.Fatal("恰好 180 字符不应截断")
	}
	long := strings.Repeat("字", 500)
	truncated := TruncateText(long)
	if runes := []rune(truncated); len(runes) != 181 || runes[180] != '…' {
		t.Fatalf("超长文本应截成 180 字符 + 「…」，实际 %d 个字符", len(runes))
	}
	// 中文不能被截成半个字（按字节截断就会出现乱码）。
	if !strings.HasPrefix(truncated, "字字") {
		t.Fatalf("截断必须按字符: %q", truncated[:6])
	}
}

// TestBuildBodyMatchesProtocol 校验请求体的协议字段（填错会被上游拒）。
func TestBuildBodyMatchesProtocol(t *testing.T) {
	body := BuildBody("7123456789012345678", "7123456789012345679", 30000, 3600000)
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	for _, token := range []string{
		`"comment_source":601`, `"server_channel":1000`, `"group_id":"7123456789012345678"`,
		`"group_type":30`, `"comment_type":20`, `"aid":8662`, `"book_id":"7123456789012345679"`,
		`"start_offset_time":30000`, `"playlet_item_duration":3600000`,
		`"need_danmaku_guide_type":[1,3,4,2]`,
	} {
		if !strings.Contains(string(encoded), token) {
			t.Fatalf("请求体缺少 %s: %s", token, encoded)
		}
	}
}
