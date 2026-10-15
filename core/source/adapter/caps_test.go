// 本仓新增（M1 站源适配层测试）。
//
// §4.3 要求能力矩阵的黄金表可核对、且「初始值是待验证的假设」。本文件是那张黄金表的
// 断言副本：11 行逐行锁定，顺序即 §4.3 表格顺序；M3 逐源实测修正时，改这里与
// adapter/caps.go 的 assumedCaps 两处（黄果需按 facet 分别实测，见下）。
package adapter_test

import (
	"errors"
	"testing"

	"github.com/dawei233/hongguo-next/core/source"
	"github.com/dawei233/hongguo-next/core/source/adapter"
)

// capsGolden 是 §4.3「预计 Caps」列（按位名书写，顺序不参与比较）。
var capsGolden = []struct {
	id    source.ID
	names []string
}{
	{source.Hongguo, []string{"search", "category", "rank", "multiQuality", "encryptedPlay", "mp4Sanitize", "danmaku"}},
	{source.Hanxiaoquan, []string{"search", "multiQuality", "directPlay"}},
	{source.Guipian, []string{"search", "category", "multiQuality", "directPlay"}},
	{source.Sorani, []string{"search", "multiQuality", "directPlay"}},
	{source.Huangdou, []string{"search", "multiQuality", "encryptedPlay", "mp4Sanitize"}},
	{source.Huangju, []string{"search", "rank", "multiQuality", "directPlay"}},
	{source.Yeguo, []string{"search", "multiQuality", "directPlay"}},
	{source.DSD, []string{"search", "category", "multiQuality", "encryptedPlay"}},
	// directPlay 来自并入本 ID 的第三个 facet（cloudfront / 注册表 entryName「旧版」）：
	// 旧接口剧库返回直链，M3 实测须按 facet 逐项核对。
	{source.Huangguo, []string{"search", "category", "rank", "directPlay", "encryptedPlay", "mp4Sanitize"}},
	{source.Xifan, []string{"search", "category", "multiQuality", "directPlay"}},
	{source.Hema, []string{"search", "category", "multiQuality", "directPlay"}},
}

func sameNames(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	set := map[string]bool{}
	for _, name := range got {
		set[name] = true
	}
	for _, name := range want {
		if !set[name] {
			return false
		}
	}
	return true
}

func TestAssumedCapsGoldenTable(t *testing.T) {
	rows := adapter.AssumedCapsTable()
	if len(rows) != len(capsGolden) {
		t.Fatalf("能力矩阵应有 %d 行，得到 %d", len(capsGolden), len(rows))
	}
	for index, want := range capsGolden {
		if rows[index].ID != want.id {
			t.Fatalf("第 %d 行的站源 = %q，§4.3 顺序应为 %q", index, rows[index].ID, want.id)
		}
		if !sameNames(rows[index].Caps.Names(), want.names) {
			t.Errorf("%s 的能力位 = %v，§4.3 假设为 %v", want.id, rows[index].Caps.Names(), want.names)
		}
		caps, err := adapter.AssumedCaps(want.id)
		if err != nil {
			t.Fatalf("AssumedCaps(%s) 失败: %v", want.id, err)
		}
		if caps != rows[index].Caps {
			t.Errorf("%s: AssumedCaps 与表格不一致", want.id)
		}
	}
}

func TestAssumedCapsCoverageAndUnverifiedBits(t *testing.T) {
	unverified := source.CapHLS | source.CapRecommend | source.CapWebCommentFallback
	for _, id := range adapter.SourceIDs() {
		caps, err := adapter.AssumedCaps(id)
		if err != nil {
			t.Fatalf("AssumedCaps(%s) 失败: %v", id, err)
		}
		// §4.3 的表格没有给出这三位的假设：不得凭推测补位。
		if caps.Has(unverified) {
			t.Errorf("%s 声明了 §4.3 未假设的能力位: %s", id, caps)
		}
	}
	if len(adapter.AssumedCapsTable()) != len(adapter.SourceIDs()) {
		t.Fatal("每个站源 ID 都必须有假设能力行")
	}
}

func TestAssumedCapsRejectsUnknown(t *testing.T) {
	for _, id := range []source.ID{"", "bilibili", "hongguoai"} {
		if _, err := adapter.AssumedCaps(id); !errors.Is(err, adapter.ErrUnknownSource) {
			t.Errorf("AssumedCaps(%q) 应报 ErrUnknownSource，得到 %v", id, err)
		}
	}
}

func TestAssumedCapsSpotChecks(t *testing.T) {
	hongguo, _ := adapter.AssumedCaps(source.Hongguo)
	if !hongguo.Has(source.CapDanmaku) || !hongguo.Has(source.CapEncryptedPlay) || !hongguo.Has(source.CapMP4Sanitize) {
		t.Fatalf("红果假设能力不符: %s", hongguo)
	}
	if hongguo.Has(source.CapDirectPlay) {
		t.Fatalf("红果假设不含 directPlay: %s", hongguo)
	}
	// 黄果的三个 facet 合并后，能力是并集：旧版 facet（cloudfront）的 directPlay 必须在这一行里，
	// 否则 M1「只合并 ai + video 两个 facet」的漏项会重现。
	huangguo, _ := adapter.AssumedCaps(source.Huangguo)
	if !huangguo.Has(source.CapDirectPlay) || !huangguo.Has(source.CapEncryptedPlay) {
		t.Fatalf("黄果假设能力应含旧版 facet 的 directPlay 与 AI/video facet 的 encryptedPlay: %s", huangguo)
	}
	// facet 名不是站源 ID：按 ID 查能力必须被拒（cloudfront / legacy 都不是对外源）。
	for _, name := range []source.ID{"cloudfront", "legacy"} {
		if _, err := adapter.AssumedCaps(name); !errors.Is(err, adapter.ErrUnknownSource) {
			t.Errorf("AssumedCaps(%q) 应报 ErrUnknownSource（facet 名/回退机制名不是站源 ID），得到 %v", name, err)
		}
	}
}
