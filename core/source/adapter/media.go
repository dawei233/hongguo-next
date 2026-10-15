// 本仓新增（M1 站源适配层）。
//
// 理由：§4.1 的 Stream 刻意把「怎么播」与源特有字段解耦，并显式带出解密材料与
// 「是否需二次净化」，好让 core/media 统一决定 ffmpeg / 直连 / 代理。vendor 的 core
// 用未导出的 providerMedia 表达同一件事（URL/Referer/Playlist/CENCKey/HLSKey/Quality），
// 另有未导出的 nativePlan 承担最终播放计划（含过期时刻与十六进制密钥）。
// 本文件把这两个形态映射为契约 Stream；不改动 core 的任何导出符号与行为。
package adapter

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dawei233/hongguo-next/core"
	"github.com/dawei233/hongguo-next/core/source"
)

// ErrInvalidStream：「怎么播」的数据不合法（地址为空、是内核中间态标记、
// 密钥长度不符、本地文件计划被当成网络流等）。
var ErrInvalidStream = errors.New("流数据无效")

// ProviderMedia 是 core.providerMedia 的线格式镜像（字段名与类型逐字段一致）。
//
// 只读镜像：core 的该类型未导出。JSON 形态下 time.Duration 是纳秒整数、
// []byte 是 base64 字符串——any/JSON 入口依赖这两条标准编码。
type ProviderMedia struct {
	URL      string
	Referer  string
	Duration time.Duration
	Playlist string
	HLSKey   []byte
	CENCKey  []byte
	Quality  int
	Variants []ProviderMedia
}

// NativePlan 是 core.nativePlan 的线格式镜像（JSON tag 逐字段一致）。
type NativePlan struct {
	ExpiresAt       int64             `json:"expiresAt,omitempty"`
	PrefetchedBytes int64             `json:"prefetchedBytes,omitempty"`
	DanmakuID       string            `json:"danmakuId,omitempty"`
	Local           bool              `json:"local"`
	URL             string            `json:"url"`
	Headers         map[string]string `json:"headers"`
	Key             string            `json:"decryptionKey,omitempty"`
	Quality         int               `json:"quality"`
	Qualities       []int             `json:"qualities"`
	Session         string            `json:"session,omitempty"`
	RouteIndex      int               `json:"routeIndex"`
	RouteCount      int               `json:"routeCount"`
}

// QualityLabel 把 core 的清晰度（像素高度，int）归一为契约标签。
//
// 高度落在 [100, 9999] 内即输出 "<高度>p"（1080→"1080p"、720→"720p"，与 §4.1 的例子一致）；
// 其余（0/负数/明显非法值）一律归一为 "auto"，因为契约不允许出现空串或非规范标签。
func QualityLabel(height int) string {
	if height >= 100 && height <= 9999 {
		return strconv.Itoa(height) + "p"
	}
	return string(source.QualityAuto)
}

// HeightFromQuality 是 QualityLabel 的逆换算；"auto" 与空串都返回 0（交由播放侧自选）。
func HeightFromQuality(quality source.Quality) int {
	label := strings.TrimSpace(strings.ToLower(string(quality)))
	if label == "" || label == string(source.QualityAuto) {
		return 0
	}
	digits := strings.TrimSuffix(label, "p")
	if digits == label {
		return 0
	}
	height, err := strconv.Atoi(digits)
	if err != nil || height < 100 || height > 9999 {
		return 0
	}
	return height
}

// StreamFromProviderMedia 把 core 的 providerMedia 映射为契约 Stream。
//
// 判定规则（M3 需逐源实测复核，§4.3 的注记）：
//   - 容器：Playlist 非空（core 已把 m3u8 正文取回）或地址以 .m3u8 结尾即 hls，否则 mp4；
//   - 解密材料：CENCKey 优先于 HLSKey，两者都必须是 16 字节（AES-128，与 core 的
//     app_source_health.go 判据一致）；出现非 16 字节密钥即报错，绝不静默降级成「未加密」
//     ——那会产出静默损坏的播放流；
//   - NeedsSanitize：仅「加密 MP4」为真（CENC 解密产物须二次净化，ADR-0004）。
//     HLS 的 AES-128 是分片级解密，不经 sanitize；
//   - ExpiresAt 为零值：core 的 providerMedia 不带过期信息，只有 nativePlan 才有。
func StreamFromProviderMedia(media ProviderMedia) (source.Stream, error) {
	address := strings.TrimSpace(media.URL)
	if err := validateStreamURL(address, "core providerMedia.URL"); err != nil {
		return source.Stream{}, err
	}
	container := containerFor(address, media.Playlist != "")

	key := ""
	switch {
	case len(media.CENCKey) == 16:
		key = hex.EncodeToString(media.CENCKey)
	case len(media.CENCKey) == 0 && len(media.HLSKey) == 16:
		key = hex.EncodeToString(media.HLSKey)
	case len(media.CENCKey) == 0 && len(media.HLSKey) == 0:
		key = ""
	default:
		return source.Stream{}, fmt.Errorf("%w: 密钥长度不是 16 字节（CENC=%d, HLS=%d），契约只接受 AES-128 的十六进制形态",
			ErrInvalidStream, len(media.CENCKey), len(media.HLSKey))
	}
	encrypted := key != ""
	return source.Stream{
		URL:           address,
		Encrypted:     encrypted,
		DecryptionKey: key,
		Headers:       refererHeaders(media.Referer),
		Quality:       QualityLabel(media.Quality),
		Container:     container,
		NeedsSanitize: encrypted && container == source.ContainerMP4,
		ExpiresAt:     time.Time{},
	}, nil
}

// StreamFromProviderMediaValue 是 StreamFromProviderMedia 的 any/JSON 形态入口
// （供 core 内部传入未导出的 providerMedia 值）。
func StreamFromProviderMediaValue(value any) (source.Stream, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return source.Stream{}, fmt.Errorf("%w: 无法序列化媒体数据: %v", ErrInvalidStream, err)
	}
	var media ProviderMedia
	if err := json.Unmarshal(body, &media); err != nil {
		return source.Stream{}, fmt.Errorf("%w: 媒体数据不是对象形态: %v", ErrInvalidStream, err)
	}
	return StreamFromProviderMedia(media)
}

// StreamFromChapter 把 core 的 Chapter 直链映射为契约 Stream。
//
// 仅适用于「章节里已是可取流地址」的源（§4.3 的 CapDirectPlay 一类）。
// core 对需要解析的源会在 Chapter.VideoURL 里放内核中间态标记（如 "hongguo-cenc://<vid>"），
// 本函数对非 http/https/file 的协议一律拒绝：那种地址必须先在 core 内经 providerMedia
// 解析，再交给 StreamFromProviderMedia。
//
// Encrypted 恒为 false：直链自身不带解密材料，其内部加密（如 m3u8 里的 AES-128）由
// 媒体层按播放列表自行处理。
func StreamFromChapter(chapter core.Chapter) (source.Stream, error) {
	address := strings.TrimSpace(chapter.VideoURL)
	if err := validateStreamURL(address, "core Chapter.VideoURL"); err != nil {
		return source.Stream{}, err
	}
	return source.Stream{
		URL:           address,
		Encrypted:     false,
		DecryptionKey: "",
		Headers:       refererHeaders(chapter.Referer),
		Quality:       string(source.QualityAuto),
		Container:     containerFor(address, false),
		NeedsSanitize: false,
		ExpiresAt:     time.Time{},
	}, nil
}

// StreamFromNativePlan 把 core 的播放计划映射为契约 Stream。
//
// Local 计划（URL 是本地文件路径）被拒绝：契约的 Stream 描述的是「怎么取流」，
// 本地回放属于 core 的本地计划路径，不能被伪装成一条网络流。
func StreamFromNativePlan(plan NativePlan) (source.Stream, error) {
	if plan.Local {
		return source.Stream{}, fmt.Errorf("%w: 本地文件计划不能被映射为网络流（url=%q）", ErrInvalidStream, plan.URL)
	}
	address := strings.TrimSpace(plan.URL)
	if err := validateStreamURL(address, "core nativePlan.url"); err != nil {
		return source.Stream{}, err
	}
	// core 的 nativePlan.Key 是 hex.EncodeToString 的产物（小写 32 位）；
	// 契约同样要求小写，故此处统一归一，避免上游大写形态漏进契约。
	key := strings.ToLower(strings.TrimSpace(plan.Key))
	encrypted := key != ""
	if encrypted && !isHex128(key) {
		return source.Stream{}, fmt.Errorf("%w: 播放计划密钥不是 32 位十六进制（%q）", ErrInvalidStream, plan.Key)
	}
	expires := time.Time{}
	if plan.ExpiresAt > 0 {
		expires = time.UnixMilli(plan.ExpiresAt).UTC()
	}
	container := containerFor(address, false)
	headers := map[string]string(nil)
	if len(plan.Headers) > 0 {
		headers = make(map[string]string, len(plan.Headers))
		for name, value := range plan.Headers {
			headers[name] = value
		}
	}
	return source.Stream{
		URL:           address,
		Encrypted:     encrypted,
		DecryptionKey: key,
		Headers:       headers,
		Quality:       QualityLabel(plan.Quality),
		Container:     container,
		NeedsSanitize: encrypted && container == source.ContainerMP4,
		// core 的 nativePlan.ExpiresAt 是 Unix 毫秒；契约用 RFC3339 绝对时刻，0 表示未知。
		ExpiresAt: expires,
	}, nil
}

// StreamFromNativePlanValue 是 StreamFromNativePlan 的 any/JSON 形态入口。
func StreamFromNativePlanValue(value any) (source.Stream, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return source.Stream{}, fmt.Errorf("%w: 无法序列化播放计划: %v", ErrInvalidStream, err)
	}
	var plan NativePlan
	if err := json.Unmarshal(body, &plan); err != nil {
		return source.Stream{}, fmt.Errorf("%w: 播放计划不是对象形态: %v", ErrInvalidStream, err)
	}
	return StreamFromNativePlan(plan)
}

// validateStreamURL 校验地址形态：必须是可取流的绝对地址，且不得是内核中间态标记。
func validateStreamURL(address, field string) error {
	if address == "" {
		return fmt.Errorf("%w: %s 为空", ErrInvalidStream, field)
	}
	parsed, err := url.Parse(address)
	if err != nil {
		return fmt.Errorf("%w: %s=%q 不是合法地址: %v", ErrInvalidStream, field, address, err)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "file":
		if parsed.Scheme != "file" && parsed.Host == "" {
			return fmt.Errorf("%w: %s=%q 缺少主机名", ErrInvalidStream, field, address)
		}
		return nil
	case "":
		return fmt.Errorf("%w: %s=%q 是相对地址", ErrInvalidStream, field, address)
	default:
		return fmt.Errorf("%w: %s=%q 使用内核中间态协议 %q，须先经 core 的 providerMedia 解析",
			ErrInvalidStream, field, address, parsed.Scheme)
	}
}

// containerFor 判定封装形态：core 已取回播放列表，或地址本身是 m3u8。
func containerFor(address string, hasPlaylist bool) string {
	if hasPlaylist {
		return source.ContainerHLS
	}
	if parsed, err := url.Parse(address); err == nil {
		if strings.HasSuffix(strings.ToLower(parsed.Path), ".m3u8") {
			return source.ContainerHLS
		}
	}
	return source.ContainerMP4
}

// refererHeaders 构造取流请求头：core 用 Referer 表达防盗链要求，空值即无需附加头。
func refererHeaders(referer string) map[string]string {
	value := strings.TrimSpace(referer)
	if value == "" {
		return nil
	}
	return map[string]string{"Referer": value}
}

func isHex128(key string) bool {
	if len(key) != 32 {
		return false
	}
	_, err := hex.DecodeString(key)
	return err == nil
}
