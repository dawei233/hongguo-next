// 本仓新增（M1 契约层测试）。
//
// 理由：§4.3 明确要求能力矩阵的修正结果写进 core/source/source_caps_test.go 的
// 黄金表。本文件的黄金表是「Go 常量 ⇄ contracts/*.schema.json」的对齐断言：
// 一旦有人改了位序、位名或站源 ID 枚举而没有同步契约，这里立即失败。
package source_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dawei233/hongguo-next/core/source"
)

// contractPath 指向仓库根下的 contracts/；本测试文件在 core/source/，
// 故向上两级。
func contractPath(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "..", "contracts", name)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("契约文件不可读 %s: %v", path, err)
	}
	return path
}

func loadSchema(t *testing.T, name string) map[string]any {
	t.Helper()
	body, err := os.ReadFile(contractPath(t, name))
	if err != nil {
		t.Fatalf("读取契约失败: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(body, &schema); err != nil {
		t.Fatalf("契约不是合法 JSON: %v", err)
	}
	return schema
}

func defs(t *testing.T, schema map[string]any) map[string]any {
	t.Helper()
	value, ok := schema["$defs"].(map[string]any)
	if !ok {
		t.Fatal("契约缺少 $defs")
	}
	return value
}

func enumOf(t *testing.T, defs map[string]any, name string) []string {
	t.Helper()
	def, ok := defs[name].(map[string]any)
	if !ok {
		t.Fatalf("契约缺少 $defs.%s", name)
	}
	raw, ok := def["enum"].([]any)
	if !ok {
		t.Fatalf("$defs.%s 缺少 enum", name)
	}
	values := make([]string, 0, len(raw))
	for _, item := range raw {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("$defs.%s 的 enum 含非字符串项: %v", name, item)
		}
		values = append(values, text)
	}
	return values
}

// TestCapBitOrderAndNamesMatchContract 锁定 §4.2 的位序号与线格式名称。
//
// 断言方向：schema 的 capBit.enum 顺序 == capabilityCatalog 的位序 == Go 的位序。
func TestCapBitOrderAndNamesMatchContract(t *testing.T) {
	schema := loadSchema(t, "source-caps.schema.json")
	defs := defs(t, schema)

	enum := enumOf(t, defs, "capBit")
	catalog, ok := defs["capabilityCatalog"].(map[string]any)
	if !ok {
		t.Fatal("契约缺少 $defs.capabilityCatalog")
	}
	rows, ok := catalog["default"].([]any)
	if !ok || len(rows) != len(enum) {
		t.Fatalf("catalog.default 形态不符: %T len=%d", catalog["default"], len(rows))
	}

	bits := []source.Caps{
		source.CapSearch,
		source.CapCategory,
		source.CapRank,
		source.CapRecommend,
		source.CapMultiQuality,
		source.CapDirectPlay,
		source.CapEncryptedPlay,
		source.CapMP4Sanitize,
		source.CapHLS,
		source.CapDanmaku,
		source.CapWebCommentFallback,
	}
	if len(bits) != len(enum) {
		t.Fatalf("Go 能力位 %d 个，契约 %d 个", len(bits), len(enum))
	}
	for index, bit := range bits {
		if bit != source.Caps(1)<<uint(index) {
			t.Errorf("位 %d 的 Go 常量不是 1<<%d（位序号被改动）", index, index)
		}
		if names := bit.Names(); len(names) != 1 || names[0] != enum[index] {
			t.Errorf("位 %d: Go 名称 %v，契约名称 %q", index, names, enum[index])
		}
		row, _ := rows[index].(map[string]any)
		if got, want := row["bit"], float64(index); got != want {
			t.Errorf("位 %d: 契约 bit=%v", index, got)
		}
		if got := row["name"]; got != enum[index] {
			t.Errorf("位 %d: catalog 名称 %v 与 enum %q 不一致", index, got, enum[index])
		}
	}
}

// TestSourceIDEnumMatchesContract 锁定 §4.3 的 11 个对外站源 ID：Go 常量顺序 == 契约枚举顺序。
//
// 反向也断言：facet 名（cloudfront）与回退机制名（legacy）都不是站源，不得出现在枚举里。
func TestSourceIDEnumMatchesContract(t *testing.T) {
	defs := defs(t, loadSchema(t, "source-caps.schema.json"))
	enum := enumOf(t, defs, "sourceId")

	// 逐项写死：常量被误删/改名/加源时这里先失败，而不是静默跟随 source.IDs()。
	goIDs := []source.ID{
		source.Hongguo, source.Hanxiaoquan, source.Guipian, source.Sorani,
		source.Huangdou, source.Huangju, source.Yeguo, source.DSD, source.Huangguo,
		source.Xifan, source.Hema,
	}
	if len(goIDs) != 11 {
		t.Fatalf("§4.3 应有 11 个对外站源 ID，得到 %d", len(goIDs))
	}
	if ids := source.IDs(); len(ids) != len(goIDs) {
		t.Fatalf("source.IDs() 有 %d 项，常量表 %d 项", len(ids), len(goIDs))
	} else {
		for index, id := range ids {
			if id != goIDs[index] {
				t.Errorf("source.IDs()[%d] = %q，常量表为 %q", index, id, goIDs[index])
			}
		}
	}
	if len(goIDs) != len(enum) {
		t.Fatalf("Go 站源 ID %d 个，契约 %d 个", len(goIDs), len(enum))
	}
	seen := map[string]bool{}
	for _, id := range goIDs {
		seen[string(id)] = true
	}
	for _, name := range enum {
		if !seen[name] {
			t.Errorf("契约站源 ID %q 在 Go 侧没有对应常量", name)
		}
	}
	for index, id := range goIDs {
		if string(id) != enum[index] {
			t.Errorf("站源 ID 顺序不一致: Go[%d]=%q 契约[%d]=%q", index, id, index, enum[index])
		}
	}
	// 非站源名不得占对外 ID：cloudfront 是黄果的第三个 facet，legacy 是回退机制。
	for _, name := range enum {
		if name == "cloudfront" || name == "legacy" {
			t.Errorf("契约站源枚举不应含 %q（facet 名 / 回退机制名，按注册表都不是站源）", name)
		}
	}
}

// TestSourceDisplayNamesAndVisibility 锁定注册表里两个 UI 直显事实：
// 每个对外源都有中文显示名；默认可见（无需解锁）的恰好是 primary 的 4 个。
func TestSourceDisplayNamesAndVisibility(t *testing.T) {
	want := map[source.ID]string{
		source.Hongguo:     "红果",
		source.Hanxiaoquan: "韩小圈",
		source.Guipian:     "鬼片",
		source.Sorani:      "青空",
		source.Huangdou:    "黄豆",
		source.Huangju:     "剧果",
		source.Yeguo:       "野果",
		source.DSD:         "帝果",
		source.Huangguo:    "黄果",
		source.Xifan:       "西饭",
		source.Hema:        "河马",
	}
	ids := source.IDs()
	if len(want) != len(ids) {
		t.Fatalf("注册表中文名 %d 个，对外站源 %d 个", len(want), len(ids))
	}
	primary := 0
	for _, id := range ids {
		if name := source.DisplayName(id); name != want[id] {
			t.Errorf("DisplayName(%q) = %q，注册表为 %q", id, name, want[id])
		}
		if source.RequiresUnlock(id) {
			continue
		}
		primary++
	}
	if primary != 4 {
		t.Errorf("默认可见（无需解锁）的站源应为 4 个，得到 %d", primary)
	}
	for _, id := range []source.ID{source.Hongguo, source.Hanxiaoquan, source.Guipian, source.Sorani} {
		if source.RequiresUnlock(id) {
			t.Errorf("%q 属注册表 primaryValues，不应要求解锁", id)
		}
	}
	// 需解锁的 7 个 = B 注册表 restrictedValues 的 5 个（黄果按组算一个）+ C 侧独有 2 个。
	for _, id := range []source.ID{
		source.Huangdou, source.Huangju, source.Yeguo, source.DSD, source.Huangguo,
		source.Xifan, source.Hema,
	} {
		if !source.RequiresUnlock(id) {
			t.Errorf("%q 不在 primaryValues 内，应要求解锁", id)
		}
		if source.IsPrimary(id) {
			t.Errorf("%q 不应被判为 primary", id)
		}
	}
	for _, id := range ids {
		if source.IsPrimary(id) != (source.DisplayName(id) != "" && !source.RequiresUnlock(id)) {
			t.Errorf("%q 的 primary/解锁判定自相矛盾", id)
		}
	}
	// 未知/复合 ID 没有显示名，也不是「已知的需解锁源」。
	if got := source.DisplayName("bilibili"); got != "" {
		t.Errorf("未知 ID 的显示名应为空串，得到 %q", got)
	}
	if source.IsKnown("bilibili") || source.IsKnown("hongguo:12345") {
		t.Error("未知/复合 ID 不应被判为已知站源")
	}
	if source.RequiresUnlock("bilibili") || source.RequiresUnlock("hongguo:12345") {
		t.Error("未知/复合 ID 不应被判为需解锁")
	}
}

// TestDanmakuModeEnumMatchesContract 锁定 §4.1 的弹幕模式取值。
func TestDanmakuModeEnumMatchesContract(t *testing.T) {
	defs := defs(t, loadSchema(t, "danmaku.schema.json"))
	item, ok := defs["danmakuItem"].(map[string]any)
	if !ok {
		t.Fatal("契约缺少 $defs.danmakuItem")
	}
	properties, _ := item["properties"].(map[string]any)
	mode, _ := properties["mode"].(map[string]any)
	raw, ok := mode["enum"].([]any)
	if !ok {
		t.Fatal("danmakuItem.properties.mode 缺少 enum")
	}
	want := []int{source.DanmakuModeScroll, source.DanmakuModeTop, source.DanmakuModeBottom}
	if len(raw) != len(want) {
		t.Fatalf("契约弹幕模式 %d 个，Go %d 个", len(raw), len(want))
	}
	for index, value := range raw {
		number, ok := value.(float64)
		if !ok || int(number) != want[index] {
			t.Errorf("弹幕模式[%d]: 契约 %v，Go %d", index, value, want[index])
		}
	}
}

// TestCapsJSONRoundTrip 锁定线格式：名称数组 ⇄ 位域，且拒绝未知名称。
func TestCapsJSONRoundTrip(t *testing.T) {
	original := source.CapSearch | source.CapDirectPlay | source.CapDanmaku
	body, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if string(body) != `["search","directPlay","danmaku"]` {
		t.Fatalf("线格式不符（须按位序升序）: %s", body)
	}
	var parsed source.Caps
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if parsed != original {
		t.Fatalf("往返后位域变化: %s → %s", original, parsed)
	}
	empty, err := json.Marshal(source.Caps(0))
	if err != nil || string(empty) != `[]` {
		t.Fatalf("空能力位必须序列化为 []，得到 %s (err=%v)", empty, err)
	}
	var nullCaps source.Caps
	if err := json.Unmarshal([]byte("null"), &nullCaps); err != nil || nullCaps != 0 {
		t.Fatalf("null 必须视为空集合: %s err=%v", nullCaps, err)
	}
	if err := json.Unmarshal([]byte(`["search","teleport"]`), &nullCaps); err == nil {
		t.Fatal("未知能力位必须被拒绝")
	}
	if err := json.Unmarshal([]byte(`"search"`), &nullCaps); err == nil {
		t.Fatal("非数组形态必须被拒绝")
	}
}

// TestCapsHas 覆盖 §4.1 接口注释依赖的 Has 语义。
func TestCapsHas(t *testing.T) {
	caps := source.CapSearch | source.CapDanmaku
	if !caps.Has(source.CapDanmaku) || !caps.Has(source.CapSearch) || !caps.Has(source.CapSearch|source.CapDanmaku) {
		t.Fatal("Has 对已声明位应返回 true")
	}
	if caps.Has(source.CapRank) {
		t.Fatal("Has 对未声明位必须返回 false")
	}
	if caps.Has(source.CapSearch | source.CapRank) {
		t.Fatal("Has 要求包含全部位")
	}
}
