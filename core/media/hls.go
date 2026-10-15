// 本仓新增（M3 媒体管线）。
//
// 本文件承接两条 HLS 权威实现的**可移植部分**：
//
//   - `nas-backend/sources/media.py` 46-170：`is_hls` / `_pick_variant` / `localize` /
//     `build_cmd`。核心是「把 m3u8 变成 ffmpeg 能直接吃的输入」——
//     未加密走远端直连（带 UA/Referer），AES-128 则自己取密钥、把 playlist 落本地并
//     把 `URI="…"` 改写成指向本地密钥文件，并放开协议白名单。
//   - `refs/hongguo-drama/src/native/hls-media.js` 34-99：清单校验（必须是 `#EXTM3U`、
//     拒绝未结束的直播清单、拒绝 `EXT-X-DEFINE` 动态变量、拒绝非 AES-128/NONE 的加密、
//     密钥必须 16 字节、拒绝把 HTML/JSON 错误页当分片）。
//
// # 与两条实现的关系（刻意保留的差异）
//
//   - 选档：media.py 的 `_pick_variant` 在**每个** URI 前都有 `#EXT-X-STREAM-INF` 时
//     实际是「最后一个档胜出」，并非「分辨率最高」。本文件把这个行为逐字保留为
//     `LastVariant`（用于与 A 对齐），另提供 `HighestBandwidthVariant`
//     （hls-media.js:63-68 的语义：只留带宽最高的一档）作为管线的默认选择。
//   - 密钥长度：media.py 取不到或长度不是 16/32 时**软降级**为远端直连（只打印一行）；
//     hls-media.js 则硬失败。本文件沿用 media.py 的软降级，但把降级原因写进
//     `LocalizedHLS.Warning`，避免「静默降级成未加密」这种最坏情况。
//   - 未移植（留给 M4）：hls-media.js 把**每个分片**都下载到本地再交给 ffmpeg
//     （那套做法是为了绕开「捆绑的 FFmpeg 没有 HTTP 传输」）。本仓的 ffmpeg 是
//     BtbN GPL 构建、带 HTTP 传输，可直接读远端 playlist 自行取分片，
//     因此这里只本地化「密钥」这一件 ffmpeg 取不到的东西（密钥 URI 每次请求都变且要
//     Referer —— media.py:15-16 的实测结论）。
package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ErrInvalidPlaylist 表示 m3u8 内容不合法或含不支持的特性。
var ErrInvalidPlaylist = errors.New("HLS 清单无效或不支持")

// errNotPlaylist 是内部哨兵：取回来的内容不是 m3u8（只在根地址上容忍，见 LocalizeHLS）。
var errNotPlaylist = errors.New("上游返回的不是 m3u8")

const (
	// DefaultMaxPlaylistBytes 是清单体积上限（hls-media.js:52 用 2MB）。
	DefaultMaxPlaylistBytes = 2 << 20
	// hlsKeySizes 与 media.py:35 一致：AES-128 密钥常见长度。
	hlsKeySize = 16
	// hlsFetchTimeout 与 media.py:151 的单次取数超时一致（15s）。
	hlsFetchTimeout = 15 * time.Second
)

var (
	extM3UPattern     = regexp.MustCompile(`#EXTM3U`)
	streamInfPattern  = regexp.MustCompile(`#EXT-X-STREAM-INF`)
	keyURIPattern     = regexp.MustCompile(`URI="([^"]+)"`)
	anyURIPattern     = regexp.MustCompile(`\bURI="([^"]*)"`)
	resolutionPattern = regexp.MustCompile(`RESOLUTION=(\d+)x(\d+)`)
	bandwidthPattern  = regexp.MustCompile(`(?:^|,)BANDWIDTH=(\d+)`)
	extinfPattern     = regexp.MustCompile(`#EXTINF:`)
	endListPattern    = regexp.MustCompile(`#EXT-X-ENDLIST`)
	definePattern     = regexp.MustCompile(`#EXT-X-DEFINE:`)
	keyTagPattern     = regexp.MustCompile(`^#EXT-X-(?:SESSION-)?KEY:`)
	keyMethodPattern  = regexp.MustCompile(`METHOD=(?:AES-128|NONE)(?:,|$)`)
	htmlOrJSONType    = regexp.MustCompile(`(?i)text/html|application/json`)
)

// IsHLS 对应 media.py:46-48：忽略查询串后以 .m3u8 结尾即视为 HLS。
func IsHLS(address string) bool {
	lowered := strings.ToLower(strings.SplitN(address, "?", 2)[0])
	return strings.HasSuffix(lowered, ".m3u8")
}

// Variant 是 master playlist 里的一档码率。
type Variant struct {
	URI        string
	Bandwidth  int64
	Resolution string
	Width      int
	Height     int
	Line       int
}

// Key 是 `#EXT-X-KEY` / `#EXT-X-SESSION-KEY` 标签。
type Key struct {
	Method  string
	URI     string
	IV      string
	Session bool
	Line    int
}

// Segment 是媒体清单里的一行分片地址（仅用于统计与校验，不下载）。
type Segment struct {
	URI       string
	DurationS float64
	Line      int
}

// Playlist 是一份解析后的 m3u8。
type Playlist struct {
	Body     string
	Variants []Variant
	Keys     []Key
	Segments []Segment
	// EndList 表示含 `#EXT-X-ENDLIST`（点播已完结）。
	EndList bool
	// Live 表示有 `#EXTINF` 但没有 `#EXT-X-ENDLIST`（hls-media.js:57 拒绝这种）。
	Live bool
}

// Encrypted 报告清单是否声明了 AES-128 加密。
func (p Playlist) Encrypted() bool {
	for _, key := range p.Keys {
		if strings.EqualFold(key.Method, "AES-128") {
			return true
		}
	}
	return false
}

// KeyURI 返回首个 AES-128 密钥地址（未加密时为空）。
func (p Playlist) KeyURI() string {
	for _, key := range p.Keys {
		if strings.EqualFold(key.Method, "AES-128") && key.URI != "" {
			return key.URI
		}
	}
	return ""
}

// ParsePlaylist 解析并校验 m3u8 文本。
//
// 校验项与 hls-media.js 对齐（错误信息保留原因，便于区分「源站坏了」与「我们没实现」）：
//   - 必须含 `#EXTM3U`；
//   - 拒绝 `#EXT-X-DEFINE`（动态地址变量，hls-media.js:77）；
//   - 加密方式只接受 AES-128 / NONE（hls-media.js:78）；
//   - 同一个标签里 `URI=` 的出现次数必须与能解析出的 `URI="…"` 数一致（hls-media.js:80）；
//   - 有 `#EXTINF` 且无 `#EXT-X-ENDLIST` → 视为未结束的直播清单（hls-media.js:57）。
func ParsePlaylist(body string) (Playlist, error) {
	playlist := Playlist{Body: body}
	if !extM3UPattern.MatchString(body) {
		return Playlist{}, fmt.Errorf("%w: 缺少 #EXTM3U 头", ErrInvalidPlaylist)
	}
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	pendingVariant := -1 // 上一行是 #EXT-X-STREAM-INF 时指向它
	pendingDuration := 0.0
	for index, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "#") {
			target := line
			if pendingVariant >= 0 {
				playlist.Variants[pendingVariant].URI = target
				playlist.Variants[pendingVariant].Line = index + 1
				pendingVariant = -1
				continue
			}
			playlist.Segments = append(playlist.Segments, Segment{URI: target, DurationS: pendingDuration, Line: index + 1})
			pendingDuration = 0
			continue
		}
		switch {
		case strings.HasPrefix(line, "#EXT-X-STREAM-INF"):
			payload := line[strings.Index(line, ":")+1:]
			variant := Variant{Line: index + 1}
			if match := bandwidthPattern.FindStringSubmatch(payload); match != nil {
				variant.Bandwidth, _ = strconv.ParseInt(match[1], 10, 64)
			}
			if match := resolutionPattern.FindStringSubmatch(payload); match != nil {
				variant.Resolution = match[1] + "x" + match[2]
				variant.Width, _ = strconv.Atoi(match[1])
				variant.Height, _ = strconv.Atoi(match[2])
			}
			playlist.Variants = append(playlist.Variants, variant)
			pendingVariant = len(playlist.Variants) - 1
		case strings.HasPrefix(line, "#EXTINF:"):
			value := strings.TrimSuffix(line[len("#EXTINF:"):], ",")
			pendingDuration, _ = strconv.ParseFloat(strings.TrimSpace(value), 64)
		case definePattern.MatchString(line):
			return Playlist{}, fmt.Errorf("%w: 不支持含动态地址变量的 HLS 清单（#EXT-X-DEFINE）", ErrInvalidPlaylist)
		case keyTagPattern.MatchString(line):
			if !keyMethodPattern.MatchString(line) {
				return Playlist{}, fmt.Errorf("%w: 不支持该 HLS 加密方式（第 %d 行）", ErrInvalidPlaylist, index+1)
			}
			key := Key{Session: strings.HasPrefix(line, "#EXT-X-SESSION-KEY"), Line: index + 1}
			if match := regexp.MustCompile(`METHOD=([A-Za-z0-9-]+)`).FindStringSubmatch(line); match != nil {
				key.Method = match[1]
			}
			if match := keyURIPattern.FindStringSubmatch(line); match != nil {
				key.URI = match[1]
			}
			if match := regexp.MustCompile(`IV=([0-9A-Fa-fx]+)`).FindStringSubmatch(line); match != nil {
				key.IV = match[1]
			}
			playlist.Keys = append(playlist.Keys, key)
		}
		if strings.Contains(line, "URI=") {
			if declared := strings.Count(line, "URI="); declared != len(anyURIPattern.FindAllString(line, -1)) {
				return Playlist{}, fmt.Errorf("%w: HLS 地址格式无效（第 %d 行）", ErrInvalidPlaylist, index+1)
			}
		}
	}
	playlist.EndList = endListPattern.MatchString(body)
	playlist.Live = extinfPattern.MatchString(body) && !playlist.EndList
	if playlist.Live {
		return Playlist{}, fmt.Errorf("%w: 暂不支持未结束的直播清单", ErrInvalidPlaylist)
	}
	return playlist, nil
}

// LastVariant 是 media.py `_pick_variant`（71-85）的逐字行为：
// 每个 URI 前都有 `#EXT-X-STREAM-INF` 时，最后一个档胜出。
func (p Playlist) LastVariant() (Variant, bool) {
	chosen := -1
	pendingResolution := -1
	lines := strings.Split(strings.ReplaceAll(p.Body, "\r\n", "\n"), "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF") {
			resolution := resolutionPattern.FindStringSubmatch(line)
			if resolution != nil {
				pendingResolution, _ = strconv.Atoi(resolution[2])
			} else {
				pendingResolution = 0
			}
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if chosen < 0 || pendingResolution >= 0 {
			chosen = indexOfVariantURI(p.Variants, line)
		}
		pendingResolution = -1
	}
	if chosen < 0 {
		return Variant{}, false
	}
	return p.Variants[chosen], true
}

// HighestBandwidthVariant 对应 hls-media.js:63-68：只留带宽最高的一档
// （相同带宽时取更靠后的那个，与 JS 的 `b.bandwidth > a.bandwidth` 折叠一致）。
func (p Playlist) HighestBandwidthVariant() (Variant, bool) {
	if len(p.Variants) == 0 {
		return Variant{}, false
	}
	best := p.Variants[0]
	for _, variant := range p.Variants[1:] {
		if variant.Bandwidth > best.Bandwidth {
			best = variant
		}
	}
	return best, true
}

// HighestResolutionVariant 按像素高度选档（RESOLUTION 缺失时退回带宽）。
//
// 这是本仓管线的默认策略：客户端能播的清晰度上限由播放器决定，媒体层只负责
// 「不要把高码率档选丢」；§4.2 的 CapMultiQuality 由 provider 声明可选清晰度，
// 具体档位仍以 provider 给出的地址为准。
func (p Playlist) HighestResolutionVariant() (Variant, bool) {
	if len(p.Variants) == 0 {
		return Variant{}, false
	}
	best := p.Variants[0]
	for _, variant := range p.Variants[1:] {
		if variant.Height > best.Height ||
			(variant.Height == best.Height && variant.Bandwidth > best.Bandwidth) {
			best = variant
		}
	}
	return best, true
}

func indexOfVariantURI(variants []Variant, uri string) int {
	for index := len(variants) - 1; index >= 0; index-- {
		if variants[index].URI == uri {
			return index
		}
	}
	return -1
}

// FetchResult 是一次 HTTP 取数。
type FetchResult struct {
	Status   int
	Header   http.Header
	Body     []byte
	FinalURL string
}

// Fetcher 抽象「取一个 HLS 资源」，便于单测注入 httptest 与复用 core/net（M4）。
type Fetcher interface {
	Get(ctx context.Context, address string, headers map[string]string, maxBytes int64) (FetchResult, error)
}

// HTTPFetcher 是基于 net/http 的默认实现。
//
// 不依赖 core 的 TLS 指纹客户端（core 是私有包、且 media 层只允许依赖
// core/source 的接口）：HLS 取清单/取密钥不需要指纹伪装，站点只校验 Referer/UA
// （media.py 的实测结论）。
type HTTPFetcher struct {
	Client    *http.Client
	UserAgent string
	// Logf 接收诊断行；nil 表示静默。
	Logf func(format string, args ...any)
}

// Get 实现 Fetcher。
func (f HTTPFetcher) Get(ctx context.Context, address string, headers map[string]string, maxBytes int64) (FetchResult, error) {
	client := f.Client
	if client == nil {
		client = &http.Client{Timeout: hlsFetchTimeout}
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxPlaylistBytes
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return FetchResult{}, err
	}
	request.Header.Set("User-Agent", firstNonEmptyString(f.UserAgent, DefaultUserAgent))
	request.Header.Set("Accept-Encoding", "identity")
	for name, value := range headers {
		if strings.EqualFold(name, "Referer") || strings.EqualFold(name, "User-Agent") {
			request.Header.Set(name, value)
			continue
		}
		request.Header.Set(name, value)
	}
	response, err := client.Do(request)
	if err != nil {
		return FetchResult{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return FetchResult{}, err
	}
	if int64(len(body)) > maxBytes {
		return FetchResult{}, fmt.Errorf("%w: HLS 资源超过 %d 字节上限", ErrInvalidPlaylist, maxBytes)
	}
	final := address
	if response.Request != nil && response.Request.URL != nil {
		final = response.Request.URL.String()
	}
	return FetchResult{Status: response.StatusCode, Header: response.Header, Body: body, FinalURL: final}, nil
}

// LocalizeOptions 是 LocalizeHLS 的入参。
type LocalizeOptions struct {
	// WorkDir 是本地 playlist / 密钥文件的落地目录（空则用 os.TempDir）。
	WorkDir string
	// Referer 是取流身份头（站点防盗链要求）。
	Referer string
	// UserAgent 为空时取 DefaultUserAgent。
	UserAgent string
	// Fetcher 为空时用 HTTPFetcher。
	Fetcher Fetcher
	// PreferHighestResolution 为 true 时按分辨率选档（默认），false 时用
	// HighestBandwidthVariant；两者都为「避免选丢高码率档」的取舍，见各自注释。
	PreferBandwidth bool
	// Logf 接收诊断行；nil 表示静默。
	Logf func(format string, args ...any)
}

// LocalizedHLS 是「ffmpeg 能直接吃的 HLS 输入」及其附带信息。
//
// 与 media.py localize() 的三元组 (input_arg, extra, cleanup) 对应：
// Input / ExtraArgs / Cleanup；其余字段是本仓补充的元数据。
type LocalizedHLS struct {
	// Input 是 ffmpeg 的 -i 参数。
	Input string
	// ExtraArgs 是必须追加在 -i 之前的参数。
	ExtraArgs []string
	// Cleanup 是调用方用完需要删除的临时文件（密钥与改写后的 playlist）。
	Cleanup []string
	// Variant 是实际选中的地址（未做档位选择时等于清单地址本身）。
	Variant string
	// Encrypted 表示已本地化 AES-128 密钥。
	Encrypted bool
	// KeyPath 是本地密钥文件路径（未加密时为空）。
	KeyPath string
	// Playlist 是选中档位的清单（已校验）。
	Playlist Playlist
	// Warning 记录软降级原因（A 的 media.py 只打印一行、不改变返回值）。
	Warning string
}

// LocalizeHLS 对应 media.py:88-144：把 m3u8 变成 ffmpeg 能直接吃的输入。
//
// 流程：
//  1. 取远端清单（带 UA/Referer）；
//  2. 如果是 master playlist（含 #EXT-X-STREAM-INF）→ 选档并再取一次媒体清单；
//  3. 未加密（或不像 playlist）→ 远端直连 + `-user_agent`/`-headers`；
//  4. AES-128 → 自己取密钥、把 playlist 落本地并把 `URI="…"` 改写成本地密钥路径，
//     附带 `-allowed_extensions ALL` + `-protocol_whitelist file,http,https,tcp,tls,crypto`
//     （此时**不能**带 `-headers`，见 media.py:17 的实测）。
func LocalizeHLS(ctx context.Context, address string, options LocalizeOptions) (LocalizedHLS, error) {
	if strings.TrimSpace(address) == "" {
		return LocalizedHLS{}, errors.New("缺少 HLS 地址")
	}
	fetcher := options.Fetcher
	if fetcher == nil {
		fetcher = HTTPFetcher{UserAgent: options.UserAgent, Logf: options.Logf}
	}
	headers := map[string]string{}
	if strings.TrimSpace(options.Referer) != "" {
		headers["Referer"] = options.Referer
	}
	result := LocalizedHLS{Input: address, Variant: address}

	body, finalAddress, err := fetchPlaylist(ctx, fetcher, address, headers, false)
	if errors.Is(err, errNotPlaylist) {
		// media.py:105-110 的根地址分支：不像 playlist 就远端直连（带 UA/Referer）。
		result.Input = address
		result.Variant = address
		result.ExtraArgs = headerArgsFor(options.Referer, options.UserAgent)
		result.Warning = "上游返回的不是 m3u8，已按普通地址交给 ffmpeg"
		return result, nil
	}
	if err != nil {
		return result, err
	}
	targetAddress, targetBody := finalAddress, body
	if streamInfPattern.MatchString(body) {
		parsed, parseErr := ParsePlaylist(body)
		if parseErr != nil {
			return result, parseErr
		}
		variant, ok := chooseVariant(parsed, options.PreferBandwidth)
		if ok && variant.URI != "" {
			resolved, resolveErr := resolveReference(finalAddress, variant.URI)
			if resolveErr != nil {
				return result, resolveErr
			}
			variantBody, variantAddress, fetchErr := fetchPlaylist(ctx, fetcher, resolved, headers, true)
			if fetchErr != nil {
				return result, fetchErr
			}
			targetAddress, targetBody = variantAddress, variantBody
			result.Variant = variantAddress
		}
	}

	// 选中档位后一律以它为输入（media.py 返回的是 target_url）。
	result.Input = targetAddress
	result.Variant = targetAddress
	parsed, err := ParsePlaylist(targetBody)
	if err != nil {
		return result, err
	}
	result.Playlist = parsed
	if !parsed.Encrypted() {
		result.ExtraArgs = headerArgsFor(options.Referer, options.UserAgent)
		return result, nil
	}

	keyURI := parsed.KeyURI()
	if keyURI == "" {
		// 有 #EXT-X-KEY 但没有 URI（同一清单内复用上一个密钥）：交给 ffmpeg 自行处理。
		result.ExtraArgs = headerArgsFor(options.Referer, options.UserAgent)
		result.Warning = "清单声明了加密但未给出密钥地址，已交给 ffmpeg 处理"
		return result, nil
	}
	resolvedKey, err := resolveReference(targetAddress, keyURI)
	if err != nil {
		return result, err
	}
	keyResult, err := fetcher.Get(ctx, resolvedKey, headers, DefaultMaxPlaylistBytes)
	if err != nil || keyResult.Status != http.StatusOK || len(keyResult.Body) != hlsKeySize {
		// media.py:121-126 的软降级：取不到密钥就退回远端直连（并留痕）。
		reason := "密钥取回失败"
		if err == nil {
			reason = fmt.Sprintf("密钥长度 %d 字节（期望 %d）", len(keyResult.Body), hlsKeySize)
		}
		result.ExtraArgs = headerArgsFor(options.Referer, options.UserAgent)
		result.Warning = fmt.Sprintf("%s，已降级为远端直连（%s）", reason, resolvedKey)
		return result, nil
	}

	workDir := options.WorkDir
	if workDir == "" {
		workDir = os.TempDir()
	}
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return result, err
	}
	tag := hashTag(targetAddress)
	keyPath := filepath.Join(workDir, "hlskey_"+tag+".bin")
	listPath := filepath.Join(workDir, "hls_"+tag+".m3u8")
	if err := os.WriteFile(keyPath, keyResult.Body, 0o600); err != nil {
		return result, err
	}
	// media.py:135 用 re.sub 全局替换 URI="…"；本仓只改密钥那一处以外的所有 URI
	// （清单里的分片 URI 保持相对形式，由 ffmpeg 依据本地 playlist 的基准地址解析 ——
	// 本地 playlist 会让相对分片路径失效，因此这里把分片 URI 提升为绝对地址，
	// 这是与 media.py 唯一的行为差异，用于修掉「本地化后分片 404」的老问题）。
	rewritten, err := rewriteKeyURI(targetBody, targetAddress, keyPath)
	if err != nil {
		return result, err
	}
	if err := os.WriteFile(listPath, []byte(rewritten), 0o600); err != nil {
		return result, err
	}
	result.Input = listPath
	result.Encrypted = true
	result.KeyPath = keyPath
	result.Cleanup = []string{keyPath, listPath}
	result.ExtraArgs = []string{"-allowed_extensions", "ALL",
		"-protocol_whitelist", "file,http,https,tcp,tls,crypto"}
	return result, nil
}

// Close 删除本地化产物（幂等；对应 media.py 的 _cleanup）。
func (l LocalizedHLS) Close() {
	for _, path := range l.Cleanup {
		if path == "" {
			continue
		}
		_ = os.Remove(path)
	}
}

func chooseVariant(playlist Playlist, preferBandwidth bool) (Variant, bool) {
	if preferBandwidth {
		return playlist.HighestBandwidthVariant()
	}
	return playlist.HighestResolutionVariant()
}

// fetchPlaylist 取一份清单。requirePlaylist 为 false 时容忍「取回来的不是 m3u8」
// （media.py:105 的根地址分支：不像 playlist 就原样交给 ffmpeg），
// 为 true 时严格拒绝（hls-media.js:56 对选中档位/密钥的校验）。
func fetchPlaylist(ctx context.Context, fetcher Fetcher, address string, headers map[string]string, requirePlaylist bool) (string, string, error) {
	result, err := fetcher.Get(ctx, address, headers, DefaultMaxPlaylistBytes)
	if err != nil {
		return "", "", fmt.Errorf("取 HLS 清单失败（%s）: %w", address, err)
	}
	if result.Status != http.StatusOK {
		return "", "", fmt.Errorf("取 HLS 清单失败（%s）: HTTP %d", address, result.Status)
	}
	contentType := ""
	if result.Header != nil {
		contentType = result.Header.Get("Content-Type")
	}
	if htmlOrJSONType.MatchString(contentType) {
		return "", "", fmt.Errorf("%w: 上游返回错误页（Content-Type=%s）", ErrInvalidPlaylist, contentType)
	}
	body := string(result.Body)
	if !extM3UPattern.MatchString(body) && !streamInfPattern.MatchString(body) {
		if requirePlaylist {
			return "", "", fmt.Errorf("%w: %s 不是 m3u8", ErrInvalidPlaylist, address)
		}
		return "", "", errNotPlaylist
	}
	return body, result.FinalURL, nil
}

// rewriteKeyURI 把 `URI="…"` 里的密钥地址指向本地文件，其余 URI 提升为绝对地址。
func rewriteKeyURI(body, baseAddress, keyPath string) (string, error) {
	keyURI := ""
	if parsed, err := ParsePlaylist(body); err == nil {
		keyURI = parsed.KeyURI()
	}
	if keyURI == "" {
		return body, nil
	}
	out := strings.Builder{}
	out.Grow(len(body) + 64)
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			out.WriteString(line)
		case strings.HasPrefix(trimmed, "#"):
			if keyTagPattern.MatchString(trimmed) {
				out.WriteString(strings.ReplaceAll(line, `URI="`+keyURI+`"`, `URI="`+keyPath+`"`))
			} else {
				out.WriteString(line)
			}
		default:
			// 分片行：本地 playlist 没有原始基准地址，必须提升为绝对地址。
			resolved, err := resolveReference(baseAddress, trimmed)
			if err != nil {
				return "", err
			}
			out.WriteString(resolved)
		}
		out.WriteString("\n")
	}
	return out.String(), nil
}

// resolveReference 等价于 JS 的 `new URL(value, base).href` 与 Python 的 urljoin。
func resolveReference(baseAddress, reference string) (string, error) {
	base, err := url.Parse(baseAddress)
	if err != nil {
		return "", err
	}
	target, err := url.Parse(reference)
	if err != nil {
		return "", err
	}
	return base.ResolveReference(target).String(), nil
}

// hashTag 复刻 media.py:128 的 tag：把地址的非字母数字压成下划线，取末 40 字符。
func hashTag(address string) string {
	cleaned := nonWordPattern.ReplaceAllString(address, "_")
	if len(cleaned) > 40 {
		cleaned = cleaned[len(cleaned)-40:]
	}
	if cleaned == "" {
		return "hls"
	}
	return cleaned
}

var nonWordPattern = regexp.MustCompile(`\W+`)

func headerArgsFor(referer, userAgent string) []string {
	agent := firstNonEmptyString(userAgent, DefaultUserAgent)
	args := []string{"-user_agent", agent}
	if strings.TrimSpace(referer) != "" {
		args = append(args, "-headers", "Referer: "+referer+"\r\n")
	}
	return args
}
