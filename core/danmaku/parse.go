package danmaku

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Page 是上游一页响应的解析结果（C 的 danmaku.js parsePage 的 Go 形态）。
type Page struct {
	// Items 是仍然"带原始时间与文本"的条目，按上游给出顺序，未排序、未去重。
	Items []Item
	// NextMS 是下一分片起点；<= 0 表示没有下一页。
	NextMS int64
	// HasMore 只在 HasMoreKnown 为真时有意义：上游显式说了还有没有。
	HasMore bool
	// HasMoreKnown 区分"显式 false"与"字段缺失" —— C 里 null 与 false 是两回事。
	HasMoreKnown bool
	// Total 是上游游标里给出的该集总数；0 表示未知。
	Total int64
}

// TruncateText 按契约把超长文本截断为 180 个字符并补「…」。
//
// contracts/danmaku.schema.json 的 danmakuItem.text 上限是 181 个字符，描述里写明
// "实现将超长文本截断为 180 个字符并补「…」"；core 的分页实现
// （core/ui_playback_danmaku.go parseHongguoDanmaku）也是这么做的。这里按**字符**
// （runes）而不是字节数截断，否则中文弹幕会被截成乱码。
func TruncateText(text string) string {
	runes := []rune(text)
	if len(runes) <= 180 {
		return text
	}
	return string(runes[:180]) + "…"
}

// CleanText 把控制字符与行分隔符换成空格，再折叠连续空白并去掉首尾空白。
//
// 与 C 的 cleanControl 同义（避免污染渲染层），但比 C 多覆盖 C1 控制区
// （U+0080–U+009F）：C 只判 < 0x20 / 0x7F，而 B 用的是 unicode.IsControl，
// 契约描述的是"控制字符与 U+2028/U+2029 在解析阶段被替换为空格"。取两者中更严的
// 那个，因为它只影响渲染安全，不影响签名。
func CleanText(text string) string {
	var builder strings.Builder
	builder.Grow(len(text))
	for _, char := range text {
		if unicode.IsControl(char) || char == '\u2028' || char == '\u2029' {
			builder.WriteByte(' ')
			continue
		}
		builder.WriteRune(char)
	}
	return strings.Join(strings.Fields(builder.String()), " ")
}

// NormalizeColor 把上游给的颜色归一成 "#RRGGBB"，拿不到时返回 ColorDefault。
//
// 移植 C 的 normalizeColor：数字按 24 位取色；字符串支持 "#RGB" / "#RRGGBB" /
// "0xRRGGBB" / 十进制。负数与非法值一律回落到白色（宁可白，也不要算出黑色的字）。
func NormalizeColor(value any) string {
	format := func(number int64) string {
		if number < 0 {
			return ColorDefault
		}
		return fmt.Sprintf("#%06X", number&0xFFFFFF)
	}
	switch typed := value.(type) {
	case nil:
		return ColorDefault
	case json.Number:
		number, err := typed.Int64()
		if err != nil {
			if float, floatErr := typed.Float64(); floatErr == nil {
				return format(int64(float))
			}
			return ColorDefault
		}
		return format(number)
	case float64:
		return format(int64(typed))
	case int64:
		return format(typed)
	case int:
		return format(int64(typed))
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return ColorDefault
		}
		if strings.HasPrefix(text, "#") && (len(text) == 4 || len(text) == 7) {
			return strings.ToUpper(text)
		}
		base := 10
		if strings.HasPrefix(strings.ToLower(text), "0x") {
			base = 16
			text = text[2:]
		}
		number, err := strconv.ParseInt(text, base, 64)
		if err != nil {
			return ColorDefault
		}
		return format(number)
	}
	return ColorDefault
}

// modeHolders 是模式字段的候选容器与候选键，取自 C 的 normalizeMode。
var (
	modeHolders = []string{"mode", "danmaku_type", "position", "fix_type", "danmaku_position"}
	topTokens   = []string{"top", "2", "fix_top", "顶部"}
	bottomToken = []string{"bottom", "3", "fix_bottom", "底部"}
)

// NormalizeMode 把上游给的位置字段归一成 ModeScroll / ModeTop / ModeBottom。
//
// 红果评论接口目前**不下发**位置字段，因此实测恒为滚动。保留多 holder、多键的兜底
// 识别是给上游将来补字段留的口子（C 的原注释如此），不要因为"反正总是 scroll"删掉。
//
// 与 C 的差异：C 只检查 row 与 comment 两层，ParseValue 这里把 content / common /
// expand 也一并传进来（row → comment 的优先级不变）。这是**只增不减**的扩展：上游
// 真把位置字段塞进嵌套层时也能认出来；万一某层出现同名的无关字段并命中 token，
// 最坏结果是一条弹幕被画到顶部/底部，而不是弹幕丢失或签名失效。
func NormalizeMode(holders ...map[string]any) int {
	for _, holder := range holders {
		if holder == nil {
			continue
		}
		for _, key := range modeHolders {
			value, exists := holder[key]
			if !exists || value == nil {
				continue
			}
			token := strings.ToLower(strings.TrimSpace(textValue(value)))
			for _, candidate := range topTokens {
				if token == candidate {
					return ModeTop
				}
			}
			for _, candidate := range bottomToken {
				if token == candidate {
					return ModeBottom
				}
			}
		}
	}
	return ModeScroll
}

// ParsePage 解析一页响应体。
//
// # 分页信号有两个来源（C 的原注释：实测里都出现过）
//
//  1. data.extra.next_query_danmaku_list_time —— 参考实现用的就是这个；
//  2. data.common_list_info（has_more + cursor.end_offset_time / next_offset_time /
//     offset）。
//
// 实测响应里 extra 常常是空对象，真正的"还有没有"写在 common_list_info 里。只认第
// 一个的话，extra 一空就会把"还有更多"误判成"没有了"，于是整集只拿到首页 90 条。
// 两个都读，且**显式的 has_more: false 优先**（此时即便有 cursor 也不该继续翻）。
//
// 与 C 的一处有意差异：结构异常（缺 data.data_list）在这里返回错误而不是当成空页。
// C 的 parsePage 会静默返回空页，而空页会让上层把"拿不到"说成"本集暂无弹幕"，
// 这正是 §5.2 反复强调不能发生的事。
func ParsePage(payload []byte, videoID string) (Page, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return Page{}, fmt.Errorf("弹幕接口返回了非 JSON 内容: %w", err)
	}
	return ParseValue(decoded, videoID)
}

// ParseValue 是 ParsePage 的"已解码 JSON"入口（core 的请求链路会先解成 map）。
func ParseValue(payload any, videoID string) (Page, error) {
	root, ok := payload.(map[string]any)
	if !ok || root == nil {
		return Page{}, fmt.Errorf("弹幕接口返回结构异常（根节点不是对象）")
	}
	// 业务错误码：上游用 code=0 表示成功；缺失/null 视为成功（实测有这种情况）。
	if code := textValue(root["code"]); code != "" && code != "0" {
		return Page{}, fmt.Errorf("弹幕接口返回业务错误（code=%s）", code)
	}
	data, ok := root["data"].(map[string]any)
	if !ok || data == nil {
		return Page{}, fmt.Errorf("弹幕接口返回结构异常（缺少 data）")
	}
	rows, ok := data["data_list"].([]any)
	if !ok {
		return Page{}, fmt.Errorf("弹幕接口返回结构异常（缺少 data.data_list）")
	}

	page := Page{}
	extra, _ := data["extra"].(map[string]any)
	if extra != nil {
		if raw := strings.TrimSpace(textValue(extra["next_query_danmaku_list_time"])); raw != "" {
			if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil {
				page.NextMS = parsed
			}
		}
	}
	if info, _ := data["common_list_info"].(map[string]any); info != nil {
		switch flag := info["has_more"].(type) {
		case bool:
			page.HasMore, page.HasMoreKnown = flag, true
		case string:
			token := strings.ToLower(strings.TrimSpace(flag))
			if token != "" {
				page.HasMoreKnown = true
				page.HasMore = token == "1" || token == "true" || token == "yes"
			}
		default:
			if text := textValue(flag); text != "" {
				page.HasMoreKnown = true
				page.HasMore = text != "0" && text != "false" && text != "no"
			}
		}
		cursor := info["cursor"]
		if text, isText := cursor.(string); isText && strings.HasPrefix(strings.TrimSpace(text), "{") {
			var parsed any
			decoder := json.NewDecoder(strings.NewReader(text))
			decoder.UseNumber()
			if decoder.Decode(&parsed) == nil {
				cursor = parsed
			} else {
				cursor = nil
			}
		}
		if cursorMap, isMap := cursor.(map[string]any); isMap {
			for _, key := range []string{"end_offset_time", "next_offset_time", "offset"} {
				candidate, err := strconv.ParseInt(strings.TrimSpace(textValue(cursorMap[key])), 10, 64)
				if err != nil {
					continue
				}
				if candidate > page.NextMS {
					page.NextMS = candidate
				}
				break
			}
			if parsed, err := strconv.ParseInt(strings.TrimSpace(textValue(cursorMap["danmaku_count"])), 10, 64); err == nil && parsed >= 0 {
				page.Total = parsed
			}
		}
	}
	// 明确说没有更多了，就别再翻（此时即便有 cursor 也不该继续）。
	if page.HasMoreKnown && !page.HasMore {
		page.NextMS = 0
	}
	if page.NextMS <= 0 {
		page.NextMS = 0
	}

	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row == nil {
			continue
		}
		comment, _ := row["comment"].(map[string]any)
		if comment == nil {
			continue
		}
		common, _ := comment["common"].(map[string]any)
		if common == nil {
			common = map[string]any{}
		}
		// 只收本视频、且状态正常的弹幕：别的值会把待审核/已折叠的脏数据混进来。
		// （C 的判据是"group_id 非空且不等于本视频才丢"，即缺失时放行；core 更严，
		//  要求必须相等。这里跟 C，避免上游偶尔不回 group_id 时整页被丢光。）
		if groupID := textValue(common["group_id"]); groupID != "" && groupID != videoID {
			continue
		}
		if textValue(common["status"]) != "1" {
			continue
		}
		content, _ := common["content"].(map[string]any)
		text := ""
		if content != nil {
			text = textValue(content["text"])
		}
		expand, _ := comment["expand"].(map[string]any)
		timeRaw := any(nil)
		if expand != nil {
			timeRaw = expand["offset_time"]
		}
		if timeRaw == nil {
			timeRaw = comment["offset_time"]
		}
		position, ok := timeMS(timeRaw)
		if !ok {
			continue
		}
		var colorRaw any
		for _, holder := range []map[string]any{content, common, expand, comment} {
			if holder == nil {
				continue
			}
			found := false
			for _, key := range []string{"color", "font_color", "text_color", "col"} {
				if value, exists := holder[key]; exists && value != nil {
					colorRaw, found = value, true
					break
				}
			}
			if found {
				break
			}
		}
		page.Items = append(page.Items, Item{
			TimeMS: position,
			Text:   text,
			Color:  NormalizeColor(colorRaw),
			Mode:   NormalizeMode(row, comment, content, common, expand),
		})
	}
	return page, nil
}

// BuildBody 构造评论接口的请求体（C 的 buildBody）。
//
// 这些字段是协议的一部分：seriesID 进 business_param.book_id，videoID 进 group_id
// ——两者不同，填错会被上游拒。start/duration 是毫秒，语义是"窗口起点/窗口终点"。
func BuildBody(videoID, seriesID string, startMS, durationMS int64) map[string]any {
	return map[string]any{
		"comment_source":    601,
		"server_channel":    1000,
		"group_id":          videoID,
		"group_type":        30,
		"comment_type":      20,
		"sort":              1,
		"count":             PageCount,
		"cursor":            "",
		"aid":               8662,
		"compliance_status": 0,
		"business_param": map[string]any{
			"book_id":                 seriesID,
			"start_offset_time":       startMS,
			"playlet_item_duration":   durationMS,
			"need_danmaku_guide_type": []int{1, 3, 4, 2},
		},
	}
}

// timeMS 把上游的时间字段读成毫秒，并挡掉越界值。
//
// C 的 toSeconds 在这里除以 1000 并限制 7 天；本包内部统一用毫秒，只在渲染层换秒。
// 布尔值、空串、非数字都视为无效（不是 0）——把无效值当 0 会让脏数据挤在片头。
func timeMS(value any) (int64, bool) {
	switch typed := value.(type) {
	case nil, bool:
		return 0, false
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return 0, false
		}
		parsed, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			if float, floatErr := strconv.ParseFloat(text, 64); floatErr == nil {
				return bounded(int64(float))
			}
			return 0, false
		}
		return bounded(parsed)
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return bounded(parsed)
		}
		if float, err := typed.Float64(); err == nil {
			return bounded(int64(float))
		}
		return 0, false
	case float64:
		return bounded(int64(typed))
	case int64:
		return bounded(typed)
	case int:
		return bounded(int64(typed))
	}
	return 0, false
}

func bounded(value int64) (int64, bool) {
	if value < 0 || value > MaxTimelineMS {
		return 0, false
	}
	return value, true
}

// textValue 复刻 core 的 mapString（只读复刻）：把任意标量取成文本。
//
// 布尔取 "1"/"0"，与 C 的 asText 一致；数字走 json.Number 以免大整数被 float64
// 吃掉精度（上游的 id 与时间戳都可能超 2^53）。
func textValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case bool:
		if typed {
			return "1"
		}
		return "0"
	case json.Number:
		return typed.String()
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	}
	return ""
}
