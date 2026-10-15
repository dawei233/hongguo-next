// 本仓新增（M1 站源适配层测试）。
//
// §4.3 要求能力矩阵的黄金表可核对、且「初始值是待验证的假设」。本文件是那张黄金表的
// 断言副本：13 行逐行锁定，顺序即 §4.3 表格顺序；M3 逐源实测修正时，改这里与
// adapter/caps.go 的 assumedCaps 两处。
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
	{source.Huangguo, []string{"search", "category", "rank", "encryptedPlay", "mp4Sanitize"}},
	{source.Guipian, []string{"search", "category", "multiQuality", "directPlay"}},
	{source.Hanxiaoquan, []string{"search", "multiQuality", "directPlay"}},
	{source.Huangdou, []string{"search", "multiQuality", "encryptedPlay", "mp4Sanitize"}},
	{source.Huangju, []string{"search", "rank", "multiQuality", "directPlay"}},
	{source.Yeguo, []string{"search", "multiQuality", "directPlay"}},
	{source.DSD, []string{"search", "category", "multiQuality", "encryptedPlay"}},
	{source.CloudFront, []string{"directPlay"}},
	{source.Sorani, []string{"search", "multiQuality", "directPlay"}},
	{source.Legacy, []string{"search"}},
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
	// cloudfront 是 §4.3 里唯一只声明 directPlay 的专用源。
	cloudfront, _ := adapter.AssumedCaps(source.CloudFront)
	if cloudfront != source.CapDirectPlay {
		t.Fatalf("cloudfront 假设能力 = %s", cloudfront)
	}
	// legacy 是 fallback 机制，§4.3 假设里只声明了搜索。
	legacy, _ := adapter.AssumedCaps(source.Legacy)
	if legacy != source.CapSearch {
		t.Fatalf("legacy 假设能力 = %s", legacy)
	}
}
