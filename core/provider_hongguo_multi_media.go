// hongguo-next 补丁：红果 App 取流的**正确调用链**（2026-10-16 真机返工）。
//
// # 为什么需要
//
// vendored 内核用 `/novel/player/video_model/v1/` + body
// `{video_id, content_type:1, biz_param:{need_all_video_definition, video_platform:3}}`，
// 实测服务端返回 **HTTP 200 且 body 长度 0** —— 即 `provider_hongguo_app.go` 里
// 「红果 App 接口未返回有效数据」（`len(content)==0`）的来源，红果因此取不到流。
//
// 可用实现（`refs/hongguo-drama/src/native/hongguo.js`，PC 端实测可用）用的是：
//
//	路径：`/novel/player/multi_video_model/preload/v1`
//	body：{ biz_param: MODEL_BIZ_PARAM(8 字段), dr_scene:"preload",
//	        mixed_video_id_map:{"1004":[vid]} }
//
// 两条差异（端点后的 `preload` 段、`dr_scene`）任一缺失都会得到空 body；
// 另有 7 个 query 参数（host_abi 等）与可用实现对齐。
//
// # 全链路（三步，全部实测通过）
//
//	① POST multi_video_model/preload/v1
//	     → data[vid].video_model（**字符串**形态）里含 video_list 与 fallback_api
//	② GET  fallback_api（**绝不带 Referer** —— 带 Referer 会 403，已实测）
//	     → video_info.data.{key_seed, video_list[档位]}
//	③ key_seed + main_url → decryptHongguoSpadeURL → 明文 CDN 地址
//	   spade_a → hongguoContentKey → 16 字节 CENC 内容密钥
//
// 与旧实现的关系：旧 `resolveHongguoAppMedia` 保留为回退（老端点若某天可用，
// 或返回单档数组形态时仍能命中）。

package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// hongguoModelBizParam 对齐可用实现的 MODEL_BIZ_PARAM（hongguo.js:75-84，8 字段）。
//
// 注意 `video_platform` 是 **1024**（旧实现用 3，两者不通用）；缺 `dr_scene` 或
// 用旧路径都会拿到空 body（实测）。
func hongguoModelBizParam() map[string]any {
	return map[string]any{
		"detail_page_version":       0,
		"device_level":              3,
		"disable_digg_stat":         false,
		"need_all_video_definition": true,
		"need_mp4_align":            false,
		"use_os_player":             false,
		"use_server_dns":            false,
		"video_platform":            1024,
	}
}

// hongguoModelExtraQuery 是可用实现 URL 模板里多出的 7 个参数
// （hongguo_core.py VIDEO_MODEL_URL_TEMPLATE）。实测对成功非必需，但对齐更稳。
func hongguoModelExtraQuery() url.Values {
	return url.Values{
		"host_abi":                {"arm64-v8a"},
		"dragon_device_type":      {"phone"},
		"pv_player":               {"71332"},
		"compliance_status":       {"0"},
		"need_personal_recommend": {"1"},
		"player_so_load":          {"1"},
		"is_android_pad_screen":   {"0"},
	}
}

const hongguoMultiVideoModelPath = "/novel/player/multi_video_model/preload/v1/"

// resolveHongguoMultiVideoMedia 走完整三步链路取流（多档位）。
func (downloader *Downloader) resolveHongguoMultiVideoMedia(ctx context.Context, videoID string) (providerMedia, error) {
	if !hongguoNumericID.MatchString(videoID) {
		return providerMedia{}, errors.New("红果视频 ID 无效")
	}
	// ① 取 video_model
	payload := map[string]any{
		"biz_param":          hongguoModelBizParam(),
		"dr_scene":           "preload",
		"mixed_video_id_map": map[string]any{"1004": []string{videoID}},
	}
	result, err := downloader.hongguoAppRequest(ctx, http.MethodPost, hongguoMultiVideoModelPath, hongguoModelExtraQuery(), payload)
	if err != nil {
		return providerMedia{}, err
	}
	model, err := hongguoModelFromMultiResponse(result, videoID)
	if err != nil {
		return providerMedia{}, err
	}
	// ② 走 fallback_api 拿 key_seed + 明文可解的档位表
	fallback, err := hongguoFallbackAPI(model)
	if err != nil {
		return providerMedia{}, err
	}
	videoData, err := downloader.fetchHongguoFallbackVideoData(ctx, fallback)
	if err != nil {
		return providerMedia{}, err
	}
	// ③ 解密地址 + 派生内容密钥
	return hongguoMediaFromFallbackData(model, videoData)
}

// hongguoModelFromMultiResponse 从 multi_video_model 响应里取出 video_model。
//
// 可用实现（hongguo.js:575、hongguo_core.py:1255）：
//
//	entry = data[vid]; vm_raw = entry.video_model
//	vm = json.loads(vm_raw) if isinstance(vm_raw, str) else vm_raw
//
// **保留对象/字符串双形态**（内核原有兼容不得丢）。
func hongguoModelFromMultiResponse(result map[string]any, videoID string) (map[string]any, error) {
	data := nestedMap(result, "data")
	entry, _ := data[videoID].(map[string]any)
	if entry == nil {
		return nil, errors.New("红果取流响应缺少该集的条目")
	}
	raw := entry["video_model"]
	switch value := raw.(type) {
	case map[string]any:
		return value, nil
	case string:
		decoder := json.NewDecoder(strings.NewReader(value))
		decoder.UseNumber()
		var model map[string]any
		if err := decoder.Decode(&model); err != nil || model == nil {
			return nil, errors.New("红果 App 播放信息格式异常")
		}
		return model, nil
	default:
		return nil, errors.New("红果取流响应缺少 video_model")
	}
}

// hongguoFallbackAPI 取 video_model 里的 fallback_api（字符串或 {fallback_api:...} 双形态）。
func hongguoFallbackAPI(model map[string]any) (string, error) {
	raw := model["fallback_api"]
	switch value := raw.(type) {
	case string:
		if strings.TrimSpace(value) == "" {
			return "", errors.New("红果取流响应缺少 fallback_api")
		}
		return value, nil
	case map[string]any:
		address := mapString(value, "fallback_api")
		if strings.TrimSpace(address) == "" {
			return "", errors.New("红果取流响应缺少 fallback_api")
		}
		return address, nil
	default:
		return "", errors.New("红果取流响应缺少 fallback_api")
	}
}

// fetchHongguoFallbackVideoData 走第二跳。
//
// 两条硬约束（均有实测依据）：
//   - **不带 Referer**：该片源属 qznovelvod 系，带 Referer 会被 403
//     （见 cortexdb 记忆「红果短剧 PC 版播放 403 的根因」与 hongguo.js:576 注释）。
//   - 用 App UA（与可用实现 SESSION.get 的 User-Agent 一致）。
func (downloader *Downloader) fetchHongguoFallbackVideoData(ctx context.Context, address string) (map[string]any, error) {
	parsed, err := url.Parse(address)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return nil, errors.New("红果备用播放地址无效")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", hongguoAppUserAgent)
	request.Header.Set("Accept", "application/json")
	// 刻意不设 Referer。
	response, err := downloader.doCatalogRequestWithTimeout(request, 30*time.Second)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	content, readErr := io.ReadAll(io.LimitReader(response.Body, providerMaxBodyBytes+1))
	if readErr != nil {
		return nil, readErr
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("红果备用播放接口 HTTP %d", response.StatusCode)
	}
	if len(content) == 0 || len(content) > providerMaxBodyBytes {
		return nil, errors.New("红果备用播放接口未返回有效数据")
	}
	var result map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil || result == nil {
		return nil, errors.New("红果备用播放接口返回格式异常")
	}
	videoData := nestedMap(nestedMap(result, "video_info"), "data")
	if len(videoData) == 0 {
		return nil, errors.New("红果备用播放接口缺少 video_info")
	}
	return videoData, nil
}

// hongguoMediaFromFallbackData 把 fallback 的 video_data 转成 providerMedia。
//
// 对应可用实现 hongguo_core.py:1263-1319：
//
//	key_seed_raw = b64_decode_padded(video_data.key_seed)
//	vl = video_data.video_list        # map: 档位key -> {vheight,size,main_url,spade_a}
//	real_url = decrypt_spade_url(best.main_url, key_seed_raw)
//	content_key = derive_content_key(best.spade_a)
//
// 多档位（用户要求）：按 vheight 降序排全部档位，H.264 优先作为实际选档
// （与旧 selectHongguoAppMedia 的 codec 打分口径一致），并把其余档位填进
// Variants 供画质切换。
func hongguoMediaFromFallbackData(model, videoData map[string]any) (providerMedia, error) {
	keySeed, err := hongguoSpadeKeySeed(videoData)
	if err != nil {
		return providerMedia{}, err
	}
	rows := nestedMap(videoData, "video_list")
	if len(rows) == 0 {
		return providerMedia{}, errors.New("红果备用播放接口无可播档位")
	}
	duration, _ := strconv.ParseFloat(mapString(model, "video_duration", "duration"), 64)

	type candidate struct {
		media providerMedia
		score int
		order int
	}
	var candidates []candidate
	var keyErr error
	order := 0
	for key, raw := range rows {
		item, _ := raw.(map[string]any)
		if item == nil {
			continue
		}
		order++
		encrypted := mapString(item, "main_url")
		if encrypted == "" {
			continue
		}
		address, decErr := decryptHongguoSpadeURL(encrypted, keySeed)
		if decErr != nil || !isProviderHTTPMediaURL(address) {
			continue
		}
		meta := nestedMap(item, "video_meta")
		codec := strings.ToLower(mapString(meta, "codec_type"))
		if codec == "" {
			codec = strings.ToLower(mapString(item, "codec_type"))
		}
		if codec == "bytevc2" {
			continue // 与旧实现一致：bytevc2 播放器支持面窄，跳过
		}
		height, _ := strconv.Atoi(mapString(item, "vheight"))
		if height == 0 {
			height, _ = strconv.Atoi(mapString(meta, "vheight"))
		}
		media := providerMedia{
			URL:      address,
			Referer:  "", // 刻意留空：该片源带 Referer 会 403
			Duration: time.Duration(duration * float64(time.Second)),
			Quality:  height,
		}
		if spade := mapString(item, "spade_a"); spade != "" {
			key, keyDecodeErr := hongguoContentKey(spade)
			if keyDecodeErr != nil {
				keyErr = keyDecodeErr
				continue
			}
			media.CENCKey = key
		}
		score := height * 10
		if codec == "h264" || codec == "avc" || codec == "avc1" {
			score++
		}
		candidates = append(candidates, candidate{media: media, score: score, order: order})
		_ = key
	}
	if len(candidates) == 0 {
		if keyErr != nil {
			return providerMedia{}, fmt.Errorf("红果 App 媒体密钥不可用: %w", keyErr)
		}
		return providerMedia{}, errors.New("红果 App 未返回兼容的媒体，已跳过不支持的编码")
	}
	// 稳定排序：先按打分降序，同分按出现顺序（与旧 SliceStable 口径一致）。
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].order < candidates[j].order
	})
	selected := candidates[0].media
	for _, item := range candidates {
		selected.Variants = append(selected.Variants, item.media)
	}
	return selected, nil
}
