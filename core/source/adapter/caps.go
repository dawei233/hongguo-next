// 本仓新增（M1 站源适配层）。
//
// 理由：§4.3 给出 13 个源「预计 Caps」的初始矩阵，并明确要求「初始值是待验证的假设，
// 必须在 M3 阶段逐源实测后修正，修正结果写进 core/source/source_caps_test.go 的黄金表」。
// 本文件把该矩阵固化为适配层的默认能力集，供 M2 接线时先跑起来；caps_test.go 里的黄金表
// 是它的断言副本，M3 实测修正时两处一起改。
package adapter

import (
	"fmt"

	"github.com/dawei233/hongguo-next/core/source"
)

// assumedCaps 是 §4.3 表格「预计 Caps」列的机器可读形态。
//
// 未出现在 §4.3 表格里的三个位——CapHLS / CapRecommend / CapWebCommentFallback——一律不置位：
// 表格没有给出假设，而 CapHLS 事实上与「返回 m3u8」相关，需在 M3 逐源实测后再声明，
// 不在这里凭推测补齐。
var assumedCaps = map[source.ID]source.Caps{
	source.Hongguo: source.CapSearch | source.CapCategory | source.CapRank |
		source.CapMultiQuality | source.CapEncryptedPlay | source.CapMP4Sanitize | source.CapDanmaku,
	source.Huangguo: source.CapSearch | source.CapCategory | source.CapRank |
		source.CapEncryptedPlay | source.CapMP4Sanitize,
	source.Guipian:     source.CapSearch | source.CapCategory | source.CapMultiQuality | source.CapDirectPlay,
	source.Hanxiaoquan: source.CapSearch | source.CapMultiQuality | source.CapDirectPlay,
	source.Huangdou:    source.CapSearch | source.CapMultiQuality | source.CapEncryptedPlay | source.CapMP4Sanitize,
	source.Huangju:     source.CapSearch | source.CapRank | source.CapMultiQuality | source.CapDirectPlay,
	source.Yeguo:       source.CapSearch | source.CapMultiQuality | source.CapDirectPlay,
	source.DSD:         source.CapSearch | source.CapCategory | source.CapMultiQuality | source.CapEncryptedPlay,
	source.CloudFront:  source.CapDirectPlay,
	source.Sorani:      source.CapSearch | source.CapMultiQuality | source.CapDirectPlay,
	source.Legacy:      source.CapSearch,
	source.Xifan:       source.CapSearch | source.CapCategory | source.CapMultiQuality | source.CapDirectPlay,
	source.Hema:        source.CapSearch | source.CapCategory | source.CapMultiQuality | source.CapDirectPlay,
}

// AssumedCaps 返回该站源按 §4.3 假设的能力集。
//
// 未知 ID 返回包装了 ErrUnknownSource 的错误：能力位是路由与降级决策的输入，
// 「未知源返回 0」会让它被当成「不支持任何能力」的合法声明。
func AssumedCaps(id source.ID) (source.Caps, error) {
	if caps, ok := assumedCaps[id]; ok {
		return caps, nil
	}
	return 0, fmt.Errorf("%w: %q", ErrUnknownSource, id)
}

// AssumedCapsTable 按 §4.3 表格顺序返回 (站源 ID, 假设能力集) 列表，便于 M3 逐源核对。
func AssumedCapsTable() []AssumedCapsRow {
	rows := make([]AssumedCapsRow, 0, len(assumedCaps))
	for _, id := range SourceIDs() {
		caps, ok := assumedCaps[id]
		if !ok {
			continue
		}
		rows = append(rows, AssumedCapsRow{ID: id, Caps: caps})
	}
	return rows
}

// AssumedCapsRow 是 AssumedCapsTable 的一行。
type AssumedCapsRow struct {
	ID   source.ID
	Caps source.Caps
}
