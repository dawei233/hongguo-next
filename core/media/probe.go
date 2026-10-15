// 本仓新增（M3 媒体管线）。
//
// 本文件的探针字段表与 `refs/hongguo-drama/src/native/merge-plan.js` 1-16 行的
// VIDEO_FIELDS / AUDIO_FIELDS 逐字段对应，签名算法（`signature()` / `readMergeInfo()`）
// 也照搬：多集合并的可行性判定**只**比较解码器配置（codec_tag/profile/level/尺寸/
// 像素格式/帧率/extradata 摘要/色彩/场序），不比码率或时长这类逐文件统计量。
//
// 一处必须记住的实测坑：ffprobe 默认**不输出** `extradata_hash`，需要显式加
// `-show_data_hash md5`（本文件已固定加上）。少了它，merge-plan.js 的逻辑会因
// `!info.video.extradata_hash` 判定为「必须重编码」，把整集合并从 copy 退化成 encode
// —— 这种退化不会报错，只会静默变慢变糊，因此这里把算法固化在代码里而不是交给调用方。
package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ProbeDataHashAlgorithm 是取 extradata_hash 所需的摘要算法（见文件头说明）。
const ProbeDataHashAlgorithm = "md5"

// StreamInfo 是 ffprobe 单条流的 Go 形态（字段名与 ffprobe 的 JSON 同名时以注释标注）。
type StreamInfo struct {
	Index              int     `json:"index"`
	Type               string  `json:"type"` // ffprobe: codec_type
	Codec              string  `json:"codec"`
	CodecLongName      string  `json:"codecLongName"`
	CodecTag           string  `json:"codecTag"`
	CodecTagString     string  `json:"codecTagString"`
	Profile            string  `json:"profile"`
	Level              int     `json:"level"`
	Width              int     `json:"width"`
	Height             int     `json:"height"`
	CodedWidth         int     `json:"codedWidth"`
	CodedHeight        int     `json:"codedHeight"`
	PixFmt             string  `json:"pixFmt"`
	SampleAspectRatio  string  `json:"sampleAspectRatio"`
	DisplayAspectRatio string  `json:"displayAspectRatio"`
	RFrameRate         string  `json:"rFrameRate"`
	AvgFrameRate       string  `json:"avgFrameRate"`
	TimeBase           string  `json:"timeBase"`
	StartTimeS         float64 `json:"startTimeS"`
	DurationS          float64 `json:"durationS"`
	BitRate            int64   `json:"bitRate"`
	NBFrames           int64   `json:"nbFrames"`
	HasBFrames         int     `json:"hasBFrames"`
	ExtradataSize      int     `json:"extradataSize"`
	ExtradataHash      string  `json:"extradataHash"`
	ColorRange         string  `json:"colorRange"`
	ColorSpace         string  `json:"colorSpace"`
	ColorTransfer      string  `json:"colorTransfer"`
	ColorPrimaries     string  `json:"colorPrimaries"`
	FieldOrder         string  `json:"fieldOrder"`
	SampleRate         int     `json:"sampleRate"`
	Channels           int     `json:"channels"`
	ChannelLayout      string  `json:"channelLayout"`
	MimeCodecString    string  `json:"mimeCodecString"`
	IsAVC              bool    `json:"isAvc"`
	NALLengthSize      int     `json:"nalLengthSize"`
}

// ProbeResult 是一次探测的完整结果。
type ProbeResult struct {
	Path           string       `json:"path"`
	Format         string       `json:"format"` // format_name
	FormatLongName string       `json:"formatLongName"`
	DurationS      float64      `json:"durationS"`
	SizeBytes      int64        `json:"sizeBytes"`
	BitRate        int64        `json:"bitRate"`
	StartTimeS     float64      `json:"startTimeS"`
	ProbeScore     int          `json:"probeScore"`
	Streams        []StreamInfo `json:"streams"`
}

// Video 返回首条视频流（merge-plan.js 的 `streams.find(codec_type==='video')`）。
func (r ProbeResult) Video() (StreamInfo, bool) { return r.firstOfType("video") }

// Audio 返回首条音频流。
func (r ProbeResult) Audio() (StreamInfo, bool) { return r.firstOfType("audio") }

func (r ProbeResult) firstOfType(kind string) (StreamInfo, bool) {
	for _, stream := range r.Streams {
		if stream.Type == kind {
			return stream, true
		}
	}
	return StreamInfo{}, false
}

// VideoSummary 是给日志/阶段记录用的一行摘要（"h264 320x180 15/1"）。
// 无视频流时返回 "无视频流"。
func (r ProbeResult) VideoSummary() string {
	video, ok := r.Video()
	if !ok {
		return "无视频流"
	}
	parts := []string{video.Codec, fmt.Sprintf("%dx%d", video.Width, video.Height)}
	if video.RFrameRate != "" {
		parts = append(parts, video.RFrameRate+"fps")
	}
	if video.Profile != "" {
		parts = append(parts, video.Profile)
	}
	if audio, hasAudio := r.Audio(); hasAudio {
		parts = append(parts, audio.Codec)
	}
	return strings.Join(parts, " ")
}

// 与 merge-plan.js:2-3 逐字段对应（顺序也一致 —— 签名是字段值的有序数组，
// 顺序变了签名就变，进而把可 copy 的合并判成必须 encode）。
var (
	// VideoSignatureFields 对应 merge-plan.js 的 VIDEO_FIELDS。
	VideoSignatureFields = []string{"codec_name", "codec_tag_string", "profile", "level", "width",
		"height", "pix_fmt", "sample_aspect_ratio", "r_frame_rate", "extradata_hash",
		"color_range", "color_space", "color_transfer", "color_primaries", "field_order"}
	// AudioSignatureFields 对应 merge-plan.js 的 AUDIO_FIELDS。
	AudioSignatureFields = []string{"codec_name", "profile", "sample_rate", "channels",
		"channel_layout", "extradata_hash"}
)

// Field 按 merge-plan.js 使用的 ffprobe 字段名取值（缺失时 ok=false，
// 对应 JS 的 `stream[key] ?? null` 里的 null 分支）。
func (s StreamInfo) Field(name string) (any, bool) {
	switch name {
	case "codec_name":
		return s.Codec, s.Codec != ""
	case "codec_tag_string":
		return s.CodecTagString, s.CodecTagString != ""
	case "profile":
		return s.Profile, s.Profile != ""
	case "level":
		return s.Level, true
	case "width":
		return s.Width, true
	case "height":
		return s.Height, true
	case "pix_fmt":
		return s.PixFmt, s.PixFmt != ""
	case "sample_aspect_ratio":
		return s.SampleAspectRatio, s.SampleAspectRatio != ""
	case "r_frame_rate":
		return s.RFrameRate, s.RFrameRate != ""
	case "extradata_hash":
		return s.ExtradataHash, s.ExtradataHash != ""
	case "color_range":
		return s.ColorRange, s.ColorRange != ""
	case "color_space":
		return s.ColorSpace, s.ColorSpace != ""
	case "color_transfer":
		return s.ColorTransfer, s.ColorTransfer != ""
	case "color_primaries":
		return s.ColorPrimaries, s.ColorPrimaries != ""
	case "field_order":
		return s.FieldOrder, s.FieldOrder != ""
	case "sample_rate":
		return s.SampleRate, s.SampleRate != 0
	case "channels":
		return s.Channels, s.Channels != 0
	case "channel_layout":
		return s.ChannelLayout, s.ChannelLayout != ""
	case "time_base":
		return s.TimeBase, s.TimeBase != ""
	case "codec_type":
		return s.Type, s.Type != ""
	case "bit_rate":
		return s.BitRate, s.BitRate != 0
	case "duration":
		return s.DurationS, s.DurationS != 0
	default:
		return nil, false
	}
}

// Signature 复刻 merge-plan.js 的 `signature(stream, fields)`：
// JSON.stringify(fields.map(key => stream[key] ?? null))，无字段时返回 ""。
func Signature(stream *StreamInfo, fields []string) string {
	if stream == nil {
		return ""
	}
	values := make([]any, 0, len(fields))
	for _, name := range fields {
		value, ok := stream.Field(name)
		if !ok {
			values = append(values, nil)
			continue
		}
		values = append(values, value)
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// VideoSignature 对应 merge-plan.js 的 info.videoSignature。
func (r ProbeResult) VideoSignature() string {
	video, ok := r.Video()
	if !ok {
		return ""
	}
	return Signature(&video, VideoSignatureFields)
}

// AudioSignature 对应 merge-plan.js 的 info.audioSignature（无音轨时为 ""）。
func (r ProbeResult) AudioSignature() string {
	audio, ok := r.Audio()
	if !ok {
		return ""
	}
	return Signature(&audio, AudioSignatureFields)
}

// CompatibilitySignature 对应 merge-plan.js 的 info.signature：
// JSON.stringify([videoSig, video.time_base, audioSig, audio?.time_base])。
func (r ProbeResult) CompatibilitySignature() string {
	video, hasVideo := r.Video()
	audio, hasAudio := r.Audio()
	videoPart := any(nil)
	if hasVideo {
		value := Signature(&video, VideoSignatureFields)
		videoPart = value
	}
	audioPart := any(nil)
	if hasAudio {
		value := Signature(&audio, AudioSignatureFields)
		audioPart = value
	}
	payload := []any{videoPart, video.TimeBase, audioPart, audio.TimeBase}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// ProbeOptions 控制一次 ffprobe。
type ProbeOptions struct {
	// Timeout 为 0 时取 DefaultProbeTimeout。
	Timeout time.Duration
	// Headers 是取流身份头（Referer 等）；远端 URL 探测时有效。
	Headers map[string]string
	// UserAgent 为空时取 FFmpeg.UserAgent。
	UserAgent string
}

// Probe 探测本地文件或远端地址，返回归一后的结果。
//
// 与 server.py:1073-1077 的差别只在参数面：A 那里的目的是打印一行
// `codec_name,width,height`（`-select_streams v:0`），本仓需要的是 merge-plan.js
// 那套完整字段（含 extradata_hash），因此固定 `-show_streams -show_format`。
func (f FFmpeg) Probe(ctx context.Context, path string, options ProbeOptions) (ProbeResult, error) {
	if strings.TrimSpace(path) == "" {
		return ProbeResult{}, errors.New("缺少探测目标")
	}
	resolved, err := f.Resolve()
	if err != nil {
		return ProbeResult{}, err
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = DefaultProbeTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{"-v", "error", "-print_format", "json", "-show_streams", "-show_format",
		"-show_data_hash", ProbeDataHashAlgorithm}
	if header := headerBlock(options.Headers); header != "" {
		args = append(args, "-headers", header)
	}
	if agent := firstNonEmptyString(options.UserAgent, f.UserAgent); agent != "" {
		args = append(args, "-user_agent", agent)
	}
	args = append(args, path)

	process := exec.CommandContext(runCtx, resolved.ProbePath, args...)
	var stdout, stderr bytes.Buffer
	process.Stdout = &stdout
	process.Stderr = &stderr
	if err := process.Run(); err != nil {
		tail := tailString(stderr.String(), 1024)
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return ProbeResult{}, fmt.Errorf("%w: ffprobe 超时（%s）: %s", ErrCommandFailed, timeout, tail)
		}
		return ProbeResult{}, fmt.Errorf("%w: ffprobe 失败: %s", ErrCommandFailed, tail)
	}
	result, err := parseProbeJSON(stdout.Bytes())
	if err != nil {
		return ProbeResult{}, err
	}
	if result.Path == "" {
		result.Path = path
	}
	return result, nil
}

// parseProbeJSON 解析 ffprobe 的 json 输出（字段名到 StreamInfo 的映射集中在此）。
func parseProbeJSON(body []byte) (ProbeResult, error) {
	var raw struct {
		Streams []map[string]any `json:"streams"`
		Format  map[string]any   `json:"format"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return ProbeResult{}, fmt.Errorf("ffprobe 输出不是合法 JSON: %w", err)
	}
	result := ProbeResult{
		Format:         probeText(raw.Format["format_name"]),
		FormatLongName: probeText(raw.Format["format_long_name"]),
		DurationS:      probeFloat(raw.Format["duration"]),
		SizeBytes:      probeInt(raw.Format["size"]),
		BitRate:        probeInt(raw.Format["bit_rate"]),
		StartTimeS:     probeFloat(raw.Format["start_time"]),
		ProbeScore:     int(probeInt(raw.Format["probe_score"])),
	}
	for _, item := range raw.Streams {
		result.Streams = append(result.Streams, StreamInfo{
			Index:              int(probeInt(item["index"])),
			Type:               probeText(item["codec_type"]),
			Codec:              probeText(item["codec_name"]),
			CodecLongName:      probeText(item["codec_long_name"]),
			CodecTag:           probeText(item["codec_tag"]),
			CodecTagString:     probeText(item["codec_tag_string"]),
			Profile:            probeText(item["profile"]),
			Level:              int(probeInt(item["level"])),
			Width:              int(probeInt(item["width"])),
			Height:             int(probeInt(item["height"])),
			CodedWidth:         int(probeInt(item["coded_width"])),
			CodedHeight:        int(probeInt(item["coded_height"])),
			PixFmt:             probeText(item["pix_fmt"]),
			SampleAspectRatio:  probeText(item["sample_aspect_ratio"]),
			DisplayAspectRatio: probeText(item["display_aspect_ratio"]),
			RFrameRate:         probeText(item["r_frame_rate"]),
			AvgFrameRate:       probeText(item["avg_frame_rate"]),
			TimeBase:           probeText(item["time_base"]),
			StartTimeS:         probeFloat(item["start_time"]),
			DurationS:          probeFloat(item["duration"]),
			BitRate:            probeInt(item["bit_rate"]),
			NBFrames:           probeInt(item["nb_frames"]),
			HasBFrames:         int(probeInt(item["has_b_frames"])),
			ExtradataSize:      int(probeInt(item["extradata_size"])),
			ExtradataHash:      probeText(item["extradata_hash"]),
			ColorRange:         probeText(item["color_range"]),
			ColorSpace:         probeText(item["color_space"]),
			ColorTransfer:      probeText(item["color_transfer"]),
			ColorPrimaries:     probeText(item["color_primaries"]),
			FieldOrder:         probeText(item["field_order"]),
			SampleRate:         int(probeInt(item["sample_rate"])),
			Channels:           int(probeInt(item["channels"])),
			ChannelLayout:      probeText(item["channel_layout"]),
			MimeCodecString:    probeText(item["mime_codec_string"]),
			IsAVC:              probeBool(item["is_avc"]),
			NALLengthSize:      int(probeInt(item["nal_length_size"])),
		})
	}
	return result, nil
}

// headerBlock 把请求头拼成 ffmpeg/ffprobe 接受的 CRLF 块（"Name: value\r\n"）。
func headerBlock(headers map[string]string) string {
	if len(headers) == 0 {
		return ""
	}
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	// 稳定顺序，便于单测断言 argv。
	sortStrings(names)
	var builder strings.Builder
	for _, name := range names {
		value := strings.TrimSpace(headers[name])
		if value == "" {
			continue
		}
		builder.WriteString(name)
		builder.WriteString(": ")
		builder.WriteString(value)
		builder.WriteString("\r\n")
	}
	return builder.String()
}

// ProbePlayable 是「产物可用」的轻量判据：能解析出视频流且尺寸有效。
//
// 与 server.py:1071/1105/1125 的「文件存在且 >= 1024 字节」相比，这里加了
// 「探测得到视频流」这一条 —— 一个截断的 MP4 同样可能大于 1024 字节，
// 但探测得不到视频流，用它判定「解密/净化成功」才站得住。
func (f FFmpeg) ProbePlayable(ctx context.Context, path string, options ProbeOptions) (ProbeResult, error) {
	result, err := f.Probe(ctx, path, options)
	if err != nil {
		return result, err
	}
	video, ok := result.Video()
	if !ok {
		return result, errors.New("探测结果里没有视频流（产物不可播）")
	}
	if video.Width <= 0 || video.Height <= 0 {
		return result, fmt.Errorf("视频流尺寸无效（%dx%d）", video.Width, video.Height)
	}
	return result, nil
}

func probeText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}

func probeFloat(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case string:
		parsed, err := strconv.ParseFloat(typed, 64)
		if err != nil {
			return 0
		}
		return parsed
	default:
		return 0
	}
}

func probeInt(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case string:
		parsed, err := strconv.ParseInt(typed, 10, 64)
		if err != nil {
			return 0
		}
		return parsed
	case bool:
		if typed {
			return 1
		}
		return 0
	default:
		return 0
	}
}

func probeBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return typed == "true" || typed == "1"
	case float64:
		return typed != 0
	default:
		return false
	}
}

func sortStrings(values []string) {
	for index := 1; index < len(values); index++ {
		for inner := index; inner > 0 && values[inner] < values[inner-1]; inner-- {
			values[inner], values[inner-1] = values[inner-1], values[inner]
		}
	}
}

// StatPlayable 是「文件存在且达到下限」的本地判据（不拉起 ffmpeg）。
// 供 pipeline 在做昂贵探测前快速挡掉空产物（server.py:1071 的 1024 字节判据）。
func StatPlayable(path string, minBytes int64) (int64, error) {
	if minBytes <= 0 {
		minBytes = MinPlayableBytes
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if info.Size() < minBytes {
		return info.Size(), fmt.Errorf("%w: %s 仅 %d 字节 < %d", ErrEmptyOutput, path, info.Size(), minBytes)
	}
	return info.Size(), nil
}
