// 本仓新增（M1 契约层）。
//
// 理由：§4.2 定义了 11 个能力位，§4.1 的接口注释要求以 Caps().Has(CapDanmaku)
// 判能力；而 vendor 的 core 用散落的字符串常量与开关表达同类信息（且构建期门控已在
// §4.5 中删除）。本文件把 §4.2 固化为位域类型，并让它的线格式与
// contracts/source-caps.schema.json 的 $defs.capBit.enum 完全一致。
//
// 本文件只含类型与位运算，不含 IO、不含源实现；Marshal/Unmarshal 属该类型的线格式
// 契约（名称数组 ⇄ 位域），是「契约层」的最小必要方法。
package source

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Caps 是能力位集合（§4.2）。零值表示未声明任何能力。
type Caps uint32

// §4.2 全部 11 个能力位；位序号即常量声明顺序（CapSearch 为第 0 位）。
//
// 新增位只能追加在末尾，禁止重排/复用已有位序号：位序号已进入
// contracts/source-caps.schema.json 的 capabilityCatalog，重排会静默改变旧数据的含义。
const (
	CapSearch             Caps = 1 << iota // 0：支持关键字搜索
	CapCategory                            // 1：支持分类浏览
	CapRank                                // 2：支持榜单
	CapRecommend                           // 3：支持推荐流
	CapMultiQuality                        // 4：支持多清晰度选择
	CapDirectPlay                          // 5：返回可直接播放的直链（无需 ffmpeg）
	CapEncryptedPlay                       // 6：需要 decryption_key 解密
	CapMP4Sanitize                         // 7：解密产物需 core/media/sanitize 二次净化
	CapHLS                                 // 8：返回 m3u8 而非 mp4
	CapDanmaku                             // 9：提供真弹幕（带时间轴）
	CapWebCommentFallback                  // 10：可提供无签名评论作为降级弹幕（无时间轴）
)

// capNames 按位序号升序排列，与 §4.2 表格顺序一致。
//
// 该切片是 Go 侧位序与线格式名称的唯一映射源；其内容由
// core/source/source_caps_test.go 对 contracts/source-caps.schema.json 断言，
// 因此改这里会被契约测试拦住。
var capNames = [...]string{
	"search",
	"category",
	"rank",
	"recommend",
	"multiQuality",
	"directPlay",
	"encryptedPlay",
	"mp4Sanitize",
	"hls",
	"danmaku",
	"webCommentFallback",
}

var capByName = func() map[string]Caps {
	table := make(map[string]Caps, len(capNames))
	for index, name := range capNames {
		table[name] = Caps(1) << uint(index)
	}
	return table
}()

// Has 报告 c 是否包含 bit 声明的全部位。
func (c Caps) Has(bit Caps) bool { return c&bit == bit }

// Names 返回 c 中已置位的能力名，按位序号升序（线格式顺序）。
func (c Caps) Names() []string {
	names := make([]string, 0, len(capNames))
	for index, name := range capNames {
		if c&(Caps(1)<<uint(index)) != 0 {
			names = append(names, name)
		}
	}
	return names
}

// String 便于日志与错误信息；形态与线格式一致（"search|rank"），零值为 "none"。
func (c Caps) String() string {
	names := c.Names()
	if len(names) == 0 {
		return "none"
	}
	out := names[0]
	for _, name := range names[1:] {
		out += "|" + name
	}
	return out
}

// MarshalJSON 输出名称数组（按位序号升序）。
//
// 永远输出数组而非 null：契约里 caps 是必填的数组字段，空集合必须是 []。
func (c Caps) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.Names())
}

// UnmarshalJSON 接受名称数组（null 视为空集合），并拒绝任何未知名称。
//
// 严格拒绝未知名称是刻意的：静默忽略会让「新位名拼错」表现为「能力消失」，
// 属于跨语言契约里最难查的一类故障。
func (c *Caps) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		*c = 0
		return nil
	}
	var names []string
	if err := json.Unmarshal(trimmed, &names); err != nil {
		return fmt.Errorf("能力位必须是名称数组: %w", err)
	}
	var parsed Caps
	for _, name := range names {
		bit, ok := capByName[name]
		if !ok {
			return fmt.Errorf("未知能力位 %q", name)
		}
		parsed |= bit
	}
	*c = parsed
	return nil
}
