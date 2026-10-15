// 本仓新增（M1 站源适配层测试）。
//
// 覆盖：13 个已知站源 ID 的正向/反向映射、core 的 facet 名与主机名别名，
// 以及未知 ID 的全部拒绝路径（含 legacy 的「fallback 机制」与 xifan/hema 的「尚无实现」）。
package adapter_test

import (
	"errors"
	"testing"

	"github.com/dawei233/hongguo-next/core/source"
	"github.com/dawei233/hongguo-next/core/source/adapter"
)

// TestSourceIDKnownIDs 覆盖 §4.3 的全部 13 个站源 ID（每个都必须自映射）。
func TestSourceIDKnownIDs(t *testing.T) {
	ids := adapter.SourceIDs()
	if len(ids) != 13 {
		t.Fatalf("§4.3 应有 13 个站源 ID，得到 %d", len(ids))
	}
	for _, id := range ids {
		got, err := adapter.SourceID(string(id))
		if err != nil {
			t.Fatalf("已知站源 ID %q 被拒绝: %v", id, err)
		}
		if got != id {
			t.Fatalf("站源 ID %q 映射成了 %q", id, got)
		}
	}
}

// TestSourceIDCoreAliases 覆盖 core 侧的站源名 / facet 名 / 主机名。
func TestSourceIDCoreAliases(t *testing.T) {
	cases := []struct {
		raw  string
		want source.ID
	}{
		{"hongguo", source.Hongguo},
		{"HONGGUO", source.Hongguo},
		{" hongguo ", source.Hongguo},
		{"hongguoduanju.com", source.Hongguo},
		{"www.hongguoduanju.com", source.Hongguo},
		// huangguo 的两个 facet 合并为同一个 §4.1 ID（§4.3）。
		{"huangguo", source.Huangguo},
		{"huangguoai", source.Huangguo},
		{"huangguoai.com", source.Huangguo},
		{"huangguo-video", source.Huangguo},
		{"huangguo.video", source.Huangguo},
		{"huangdou", source.Huangdou},
		{"tideember.cc", source.Huangdou},
		{"xqjurgek.top", source.Huangdou},
		{"huangju", source.Huangju},
		{"api.huangju.net", source.Huangju},
		{"yeguo", source.Yeguo},
		{"ygdj7.com", source.Yeguo},
		{"delta.ygrwdsgt.cc", source.Yeguo},
		{"dsd", source.DSD},
		{"www.dsd.com.se", source.DSD},
		{"cloudfront", source.CloudFront},
		{"sorani", source.Sorani},
		{"api.sorani.cc", source.Sorani},
		{"guipian", source.Guipian},
		{"www.guipianwu.com", source.Guipian},
		{"hanxiaoquan", source.Hanxiaoquan},
		{"jennyhow.com", source.Hanxiaoquan},
		{"legacy", source.Legacy},
		{"xifan", source.Xifan},
		{"hema", source.Hema},
		// 主机后缀规则（复刻 core 的 providerSourceForURL）。
		{"ttvoij.ediayikma.cc", source.Huangguo},
		{"x.agdkczeyx.cc", source.Huangguo},
		{"analyze.buxefaex.cc", source.Yeguo},
		{"x.fzchosdi.cc", source.Yeguo},
	}
	for _, item := range cases {
		got, err := adapter.SourceID(item.raw)
		if err != nil {
			t.Errorf("SourceID(%q) 失败: %v", item.raw, err)
			continue
		}
		if got != item.want {
			t.Errorf("SourceID(%q) = %q，期望 %q", item.raw, got, item.want)
		}
	}
}

// TestSourceIDForURL 覆盖按 URL 判定站源，以及未知主机的拒绝路径。
func TestSourceIDForURL(t *testing.T) {
	cases := []struct {
		raw  string
		want source.ID
	}{
		{"https://www.hongguoduanju.com/detail/12345", source.Hongguo},
		{"https://huangguo.video/series/77", source.Huangguo},
		{"https://ttvoij.ediayikma.cc/video/9", source.Huangguo},
		{"https://yeguodj.com/play/1", source.Yeguo},
		{"http://api.huangju.net/list", source.Huangju},
	}
	for _, item := range cases {
		got, err := adapter.SourceIDForURL(item.raw)
		if err != nil {
			t.Errorf("SourceIDForURL(%q) 失败: %v", item.raw, err)
			continue
		}
		if got != item.want {
			t.Errorf("SourceIDForURL(%q) = %q，期望 %q", item.raw, got, item.want)
		}
	}
	// URL 形态直接交给 SourceID 也应自动改走 URL 判定。
	if got, err := adapter.SourceID("https://www.hongguoduanju.com/x"); err != nil || got != source.Hongguo {
		t.Errorf("SourceID(URL) = %q, %v", got, err)
	}

	// 未知主机：core 的 providerSourceForURL 对 cloudfront 域同样返回空串（它只用于
	// 拼接基址，不参与站源识别），契约层把它明确成拒绝。
	if _, err := adapter.SourceIDForURL("https://d2pypzndaqisk.cloudfront.net/a.mp4"); !errors.Is(err, adapter.ErrUnknownSource) {
		t.Errorf("未知主机应报 ErrUnknownSource，得到 %v", err)
	}
	for _, raw := range []string{"", "   ", "https:///no-host", "://bad"} {
		if _, err := adapter.SourceIDForURL(raw); err == nil {
			t.Errorf("SourceIDForURL(%q) 必须被拒绝", raw)
		}
	}
}

// TestSourceIDRejectsUnknown 覆盖未知/空/非法输入的拒绝路径。
func TestSourceIDRejectsUnknown(t *testing.T) {
	for _, raw := range []string{"", "   ", "bilibili", "hongguo-x", "unknown.example.com", "..", "a."} {
		id, err := adapter.SourceID(raw)
		if err == nil {
			t.Errorf("SourceID(%q) 必须被拒绝，却返回 %q", raw, id)
			continue
		}
		if !errors.Is(err, adapter.ErrUnknownSource) && !errors.Is(err, adapter.ErrInvalidID) {
			t.Errorf("SourceID(%q) 的错误没有包装 ErrUnknownSource/ErrInvalidID: %v", raw, err)
		}
	}
}

// TestCoreSources 覆盖反向映射：主 facet、多 facet 展开与三条拒绝路径。
func TestCoreSources(t *testing.T) {
	// 每个已落地的站源都必须能反查到 core 的 provider 源名。
	for _, id := range []source.ID{
		source.Hongguo, source.Guipian, source.Hanxiaoquan, source.Huangdou,
		source.Huangju, source.Yeguo, source.DSD, source.CloudFront, source.Sorani,
	} {
		names, err := adapter.CoreSources(id)
		if err != nil {
			t.Fatalf("CoreSources(%q) 失败: %v", id, err)
		}
		if len(names) != 1 || names[0] != string(id) {
			t.Fatalf("CoreSources(%q) = %v，期望 [%q]", id, names, id)
		}
		if primary, err := adapter.PrimaryCoreSource(id); err != nil || primary != string(id) {
			t.Fatalf("PrimaryCoreSource(%q) = %q, %v", id, primary, err)
		}
	}

	// huangguo 是 §4.3 合并的两个 facet：反向必须展开，且主 facet 与 core 的
	// canonicalProviderSource（裸 "huangguo" → huangguoai）一致。
	names, err := adapter.CoreSources(source.Huangguo)
	if err != nil {
		t.Fatalf("CoreSources(huangguo) 失败: %v", err)
	}
	if len(names) != 2 || names[0] != "huangguoai" || names[1] != "huangguo-video" {
		t.Fatalf("CoreSources(huangguo) = %v", names)
	}
	if primary, err := adapter.PrimaryCoreSource(source.Huangguo); err != nil || primary != "huangguoai" {
		t.Fatalf("PrimaryCoreSource(huangguo) = %q, %v", primary, err)
	}
	// 返回的切片必须是副本：调用方改它不能污染映射表。
	names[0] = "tampered"
	again, _ := adapter.CoreSources(source.Huangguo)
	if again[0] != "huangguoai" {
		t.Fatal("CoreSources 必须返回副本")
	}

	// legacy：§4.3 的 fallback 机制，没有独立 provider 源名。
	if _, err := adapter.CoreSources(source.Legacy); !errors.Is(err, adapter.ErrFallbackOnly) {
		t.Errorf("legacy 应报 ErrFallbackOnly，得到 %v", err)
	}
	if _, err := adapter.PrimaryCoreSource(source.Legacy); !errors.Is(err, adapter.ErrFallbackOnly) {
		t.Errorf("PrimaryCoreSource(legacy) 应报 ErrFallbackOnly，得到 %v", err)
	}
	// xifan / hema：C 侧独有源，本仓 core 尚无实现（§3.3 计划 M3/M4 移植）。
	for _, id := range []source.ID{source.Xifan, source.Hema} {
		if _, err := adapter.CoreSources(id); !errors.Is(err, adapter.ErrNotImplementedInCore) {
			t.Errorf("CoreSources(%q) 应报 ErrNotImplementedInCore，得到 %v", id, err)
		}
		if _, err := adapter.PrimaryCoreSource(id); !errors.Is(err, adapter.ErrNotImplementedInCore) {
			t.Errorf("PrimaryCoreSource(%q) 应报 ErrNotImplementedInCore，得到 %v", id, err)
		}
	}
	// 未知 ID。
	for _, id := range []source.ID{"", "bilibili", "hongguoai"} {
		if _, err := adapter.CoreSources(id); !errors.Is(err, adapter.ErrUnknownSource) {
			t.Errorf("CoreSources(%q) 应报 ErrUnknownSource，得到 %v", id, err)
		}
	}
}

// TestSeriesIDRejectsUnimplemented 锁定「不可实现的源不得生成契约 ID」。
func TestSeriesIDRejectsUnimplemented(t *testing.T) {
	if _, err := adapter.SeriesID(source.Xifan, "42"); !errors.Is(err, adapter.ErrNotImplementedInCore) {
		t.Errorf("xifan 不应生成契约剧 ID，得到 %v", err)
	}
	if _, err := adapter.SeriesID(source.Legacy, "42"); !errors.Is(err, adapter.ErrFallbackOnly) {
		t.Errorf("legacy 不应生成契约剧 ID，得到 %v", err)
	}
	if _, err := adapter.SeriesID(source.Hongguo, "  "); !errors.Is(err, adapter.ErrInvalidID) {
		t.Errorf("空源内剧 ID 应被拒绝，得到 %v", err)
	}
	id, err := adapter.SeriesID(source.Huangguo, "77")
	if err != nil || id != source.ID("huangguo:77") {
		t.Fatalf("SeriesID(huangguo,77) = %q, %v", id, err)
	}
}
