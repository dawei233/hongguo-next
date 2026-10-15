// 本仓新增（M1 站源适配层）。
//
// 理由：§4.4 规定集 ID 形如 "hongguo:12345:2"（ResolveEpisodeID 负责拆出站源），
// §4.1 规定 Episode.EpisodeID 是「源内不透明 ID，格式 <source>:<...>」。vendor 的 core
// 用 Chapter{ID: providerChapterID(source, sourceID, chapterKey)} 表达同一件事，
// 但其前缀是 facet 名（huangguoai / huangguo-video / cloudfront），与契约层的 11 个统一 ID 不同。
// 本文件承担「core 章节 ⇄ 契约 Episode」与复合 ID 的拆装。
package adapter

import (
	"errors"
	"fmt"
	"strings"

	"github.com/dawei233/hongguo-next/core"
	"github.com/dawei233/hongguo-next/core/source"
)

// ErrInvalidEpisode：集数据（章节 ID、父剧 ID、集序号）不合法。
var ErrInvalidEpisode = errors.New("集数据无效")

// SeriesID 组装契约层的全局剧 ID：ID + ":" + sourceKey。
//
// 该形态即 §4.4 别名链的键（"hongguo:12345"）。
func SeriesID(id source.ID, sourceKey string) (source.ID, error) {
	if _, err := CoreSources(id); err != nil {
		return "", err
	}
	key := strings.TrimSpace(sourceKey)
	if key == "" {
		return "", fmt.Errorf("%w: 源内剧 ID 为空（source=%s）", ErrInvalidID, id)
	}
	return source.ID(string(id) + ":" + key), nil
}

// SplitSeriesID 拆契约层的全局剧 ID：返回 (站源 ID, 源内剧 ID)。
//
// 只按首个冒号切分：冒号之后的内容（含其中的冒号）都属于源内剧 ID，因为
// core 的章节键本身就可能是含冒号的 URL（如 guipian 的 chapterKey 是播放页 URL）。
func SplitSeriesID(seriesID string) (source.ID, string, error) {
	trimmed := strings.TrimSpace(seriesID)
	id, key, found := strings.Cut(trimmed, ":")
	if !found {
		return "", "", fmt.Errorf("%w: %q 缺少 \"<sourceId>:<sourceKey>\" 形态的冒号", ErrInvalidID, seriesID)
	}
	normalized, err := SourceID(id)
	if err != nil {
		return "", "", err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", fmt.Errorf("%w: %q 的源内剧 ID 为空", ErrInvalidID, seriesID)
	}
	return normalized, key, nil
}

// SplitEpisodeID 拆集 ID：返回 (站源 ID, 源内剧 ID, 源内集键)。
//
// 与 core 的章节 ID 形态 "<sourceId>:<seriesKey>:<chapterKey>" 对应，且有两条
// 刻意保留的宽容规则，都与 core 的既有数据相符：
//   - seriesKey 允许为空（guipian / hanxiaoquan 在拿到详情前会先发 "guipian::<playURL>"，
//     详情返回后再回填，见 core/provider_guipian.go）；
//   - chapterKey 允许为空、且可含冒号（章节键是源内不透明值）。
func SplitEpisodeID(episodeID string) (source.ID, string, string, error) {
	trimmed := strings.TrimSpace(episodeID)
	id, rest, found := strings.Cut(trimmed, ":")
	if !found {
		return "", "", "", fmt.Errorf("%w: %q 缺少 \"<sourceId>:<...>\" 形态的冒号", ErrInvalidID, episodeID)
	}
	normalized, err := SourceID(id)
	if err != nil {
		return "", "", "", err
	}
	seriesKey, episodeKey, _ := strings.Cut(rest, ":")
	return normalized, seriesKey, episodeKey, nil
}

// ResolveEpisodeID 是 §4.4 约定的入口：从集 ID 里解出站源 ID。
//
// 形如 "hongguo:12345:2" → source.Hongguo。无法识别或形态不合法时返回错误
// （core 侧的对应实现返回空串，本包刻意改为显式错误）。
func ResolveEpisodeID(episodeID string) (source.ID, error) {
	id, _, _, err := SplitEpisodeID(episodeID)
	if err != nil {
		return "", err
	}
	return id, nil
}

// SeriesIDFromCoreDramaID 把 core 的剧 ID（"<facet>:<sourceKey>"）转成契约层的全局剧 ID。
//
// 例："huangguo-video:77" → "huangguo:77"（facet 归一为 §4.3 的统一 ID）。
func SeriesIDFromCoreDramaID(coreDramaID string) (source.ID, error) {
	id, key, err := SplitSeriesID(coreDramaID)
	if err != nil {
		return "", err
	}
	return SeriesID(id, key)
}

// CoreDramaIDFromSeriesID 把契约层的全局剧 ID 转回 core 的剧 ID，取主 facet。
//
// 多 facet 源（huangguo）只能取主 facet："huangguo:77" → "huangguoai:77"。
// 若原数据属于非主 facet，必须先经 CoreSources 展开后逐个尝试——
// 契约层不保留 facet 信息，这是 §4.3 合并 facet 的必然代价，调用方必须知情。
func CoreDramaIDFromSeriesID(seriesID string) (string, error) {
	id, key, err := SplitSeriesID(seriesID)
	if err != nil {
		return "", err
	}
	primary, err := PrimaryCoreSource(id)
	if err != nil {
		return "", err
	}
	return primary + ":" + key, nil
}

// EpisodeIDFromCoreChapterID 把 core 的章节 ID 归一为契约层的集 ID（前缀换成统一站源 ID）。
//
// 例："huangguo-video:77:abc" → "huangguo:77:abc"。
func EpisodeIDFromCoreChapterID(coreChapterID string) (string, error) {
	id, seriesKey, episodeKey, err := SplitEpisodeID(coreChapterID)
	if err != nil {
		return "", err
	}
	return composeEpisodeID(id, seriesKey, episodeKey), nil
}

// CoreChapterIDFromEpisodeID 把契约层集 ID 转回 core 的章节 ID，取主 facet。
// facet 的歧义与 CoreDramaIDFromSeriesID 相同。
func CoreChapterIDFromEpisodeID(episodeID string) (string, error) {
	id, seriesKey, episodeKey, err := SplitEpisodeID(episodeID)
	if err != nil {
		return "", err
	}
	primary, err := PrimaryCoreSource(id)
	if err != nil {
		return "", err
	}
	return composeEpisodeID(source.ID(primary), seriesKey, episodeKey), nil
}

func composeEpisodeID(id source.ID, seriesKey, episodeKey string) string {
	if episodeKey == "" {
		return string(id) + ":" + seriesKey
	}
	return string(id) + ":" + seriesKey + ":" + episodeKey
}

// EpisodeFromChapter 把 core 的章节映射为契约 Episode。
//
// seriesID 为 "" 时按章节 ID 的前两段自行推导；显式传入时必须与章节 ID 的站源前缀一致
// ——这与 core 的 nativeChapterAvailable 跨源校验同义，防止把 A 源的集挂到 B 源的剧下。
//
// DurationS 恒为 0：core 的 Chapter 不携带时长，只有媒体层解析后才能得知，
// 契约也不允许在此处猜。
func EpisodeFromChapter(chapter core.Chapter, seriesID source.ID, index int) (source.Episode, error) {
	if index < 0 {
		return source.Episode{}, fmt.Errorf("%w: 集序号为负 %d", ErrInvalidEpisode, index)
	}
	raw := strings.TrimSpace(chapter.ID)
	if raw == "" {
		return source.Episode{}, fmt.Errorf("%w: 章节 ID 为空（core 需 providerChapterID 生成的 <source>:<seriesKey>:<chapterKey>）", ErrInvalidEpisode)
	}
	episodeID, err := EpisodeIDFromCoreChapterID(raw)
	if err != nil {
		return source.Episode{}, fmt.Errorf("%w: 章节 ID %q: %v", ErrInvalidEpisode, raw, err)
	}
	chapterSource, seriesKey, _, err := SplitEpisodeID(raw)
	if err != nil {
		return source.Episode{}, fmt.Errorf("%w: 章节 ID %q: %v", ErrInvalidEpisode, raw, err)
	}
	parent := seriesID
	if parent == "" {
		parent, err = SeriesID(chapterSource, seriesKey)
		if err != nil {
			return source.Episode{}, fmt.Errorf("%w: 章节 ID %q: %v", ErrInvalidEpisode, raw, err)
		}
	}
	if got, _, splitErr := SplitSeriesID(string(parent)); splitErr != nil {
		return source.Episode{}, fmt.Errorf("%w: 父剧 ID %q: %v", ErrInvalidEpisode, parent, splitErr)
	} else if got != chapterSource {
		return source.Episode{}, fmt.Errorf("%w: 章节属于 %s，父剧 ID %q 属于 %s", ErrInvalidEpisode, chapterSource, parent, got)
	}
	return source.Episode{
		SeriesID:  parent,
		EpisodeID: episodeID,
		Index:     index,
		Title:     strings.TrimSpace(chapter.Title),
		DurationS: 0,
	}, nil
}

// EpisodesFromChapters 按 core 的 1 起始约定批量映射剧集（core 生成标题时用 i+1）。
//
// 任一章节目录非法即整批失败：契约要求「不支持即报错，不得假装成功」，
// 静默丢集会让 UI 出现缺集却无告警。
func EpisodesFromChapters(chapters []core.Chapter, seriesID source.ID) ([]source.Episode, error) {
	episodes := make([]source.Episode, 0, len(chapters))
	for offset, chapter := range chapters {
		episode, err := EpisodeFromChapter(chapter, seriesID, offset+1)
		if err != nil {
			return nil, fmt.Errorf("第 %d 章: %w", offset+1, err)
		}
		episodes = append(episodes, episode)
	}
	return episodes, nil
}
