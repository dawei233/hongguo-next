// 本仓新增（M3 媒体管线）。
//
// 本文件把 §2.1 的媒体管线固化成五个**顺序固定**的阶段：
//
//	Resolve → Fetch → Decrypt → Sanitize → Probe
//
// 每个阶段在 Result.Stages 里都有一条记录（被跳过时 Skipped=true + Reason），
// 因此「这一集为什么走了 ffmpeg / 为什么没净化」是可审计的，而不是隐藏在分支里。
//
// # 权威依据
//
//   - 直链优先：站源产物若是未加密直链，**不走 ffmpeg**，直接交给播放器
//     （server.py:981「CDN 地址浏览器直接播，绕开 ffmpeg 单连接下载瓶颈」）；
//   - 加密 MP4：`ffmpeg -decryption_key K -i URL -c copy -movflags +faststart`
//     → **必须**接一次 sanitize（ADR-0004；产物仍带 sgpd/sbgp，见
//     testdata/real_faststart_remux.meta.json 的实测字节）；
//   - 中间文件保留 `.mp4` 后缀（server.py:500）；
//   - HLS：Chromium 内核的 <video> 播不了 m3u8（media.py:4-6），必须转封装成 MP4；
//   - 播放计划的过期时刻：core 侧有 `播放凭证已过期，请重新解析播放`
//     （app_playback_routes.go:84），本管线在 Resolve 之后立刻用 Stream.ExpiresAt 复刻。
//
// # 与 A 的一处刻意分歧（安全相关，不可"优化"回去）
//
// server.py:1104 在 ffmpeg 解密失败时会拿**加密原文件**直接跑 sanitize 兜底。
// 实测证明那只会产出「没有 CENC 信令、但 mdat 仍是密文」的文件：
// 解码时吐出成片的 SEI/AAC 诊断（见 sanitize_test.go 的 TestSanitizeDoesNotDecrypt）。
// 本管线**不做这个兜底**：解密失败就是失败，报错让调用方重解析播放计划，
// 绝不把一个静默损坏的流交给用户。
package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/dawei233/hongguo-next/core/source"
)

// ErrResolveFailed / ErrExpiredPlan / ErrDecryptFailed / ErrSanitizeFailed
// 是管线各阶段的稳定错误分类，便于上层区分「重试有用」与「重试没用」。
var (
	ErrResolveFailed  = errors.New("解析播放地址失败")
	ErrExpiredPlan    = errors.New("播放凭证已过期，需要重新解析播放")
	ErrDecryptFailed  = errors.New("解密失败")
	ErrSanitizeFailed = errors.New("MP4 净化失败")
	ErrInvalidStream  = errors.New("流数据无效")
)

// Resolver 把「一集」解析为契约 Stream（§4.1 的 Resolve）。
//
// 本包不 import core 的私有实现：适配层（core/source/adapter）已经能把 core 的
// providerMedia / nativePlan 映射成 source.Stream，调用方把它包成 Resolver 即可。
type Resolver interface {
	Resolve(ctx context.Context, reference string, quality source.Quality) (source.Stream, error)
}

// ResolverFunc 让普通函数满足 Resolver。
type ResolverFunc func(ctx context.Context, reference string, quality source.Quality) (source.Stream, error)

// Resolve 实现 Resolver。
func (f ResolverFunc) Resolve(ctx context.Context, reference string, quality source.Quality) (source.Stream, error) {
	return f(ctx, reference, quality)
}

// StageNames 是管线阶段名（顺序即执行顺序），供测试与日志断言。
var StageNames = []string{"resolve", "fetch", "decrypt", "sanitize", "probe"}

// Stage 是一个阶段的执行记录。
type Stage struct {
	Name    string
	Skipped bool
	// Reason 说明跳过原因（Skipped 时必有内容）。
	Reason string
	// Detail 是该阶段实际做了什么（含已脱敏的命令行）。
	Detail  string
	Elapsed time.Duration
}

// Result 是一次管线运行的产物与证据。
type Result struct {
	// Stream 是 Resolve 阶段拿到的契约流。
	Stream source.Stream
	// Playback 是最终应交给播放器的地址（远端 URL 或本地文件路径）。
	Playback string
	// Local 表示 Playback 是本地产物（需要由调用方负责清理）。
	Local bool
	// Probe 是最终产物的探测结果。
	Probe ProbeResult
	// Sanitized 非空表示做过净化，含逐字节相关的统计。
	Sanitized *SanitizeResult
	// Decrypted / Remuxed 分别表示是否真的跑过 ffmpeg 解密、转封装。
	Decrypted bool
	Remuxed   bool
	// Cleanup 是中间产物路径（Close 会删除；Playback 本地时也在其中）。
	Cleanup []string
	// Warnings 是软降级留痕（例如 HLS 密钥取不到而退回直连）。
	Warnings []string
	// Stages 恒为 len(StageNames) 条，顺序与 StageNames 一致。
	Stages []Stage
}

// Close 删除本地产物（幂等）。远端直连时无事可做。
func (r Result) Close() {
	for _, path := range r.Cleanup {
		if path == "" {
			continue
		}
		_ = os.Remove(path)
	}
}

// Stage 按名字取阶段记录。
func (r Result) Stage(name string) (Stage, bool) {
	for _, stage := range r.Stages {
		if stage.Name == name {
			return stage, true
		}
	}
	return Stage{}, false
}

// Pipeline 是媒体管线的配置。
type Pipeline struct {
	FFmpeg  FFmpeg
	Fetcher Fetcher
	// TempDir 是中间产物目录（空则用 os.TempDir）。
	TempDir string
	// KeepIntermediate 为 true 时保留中间产物（诊断用；此时仍会写进 Cleanup）。
	KeepIntermediate bool
	// AllowRemoteHLS 为 true 时，未加密 HLS 直连交给播放器（安卓 ExoPlayer 可以）。
	// 默认 false —— PC 端 Chromium 内核播不了 m3u8，必须转封装（media.py:4-6）。
	AllowRemoteHLS bool
	// DecryptTimeout / RemuxTimeout / ProbeTimeout 为 0 时取各自的默认值。
	DecryptTimeout time.Duration
	RemuxTimeout   time.Duration
	ProbeTimeout   time.Duration
	// DecodeVerifyFrames 是**本地产物**的解码验收帧数：0 取 DefaultVerifyFrames，
	// 负数表示关闭（关闭会把「内容密钥失效」这类故障放行到客户端，慎用）。
	DecodeVerifyFrames int
	// AllowDecodeWarnings 为 true 时解码验收只看退出码（见 VerifyOptions.AllowDiagnostics）。
	AllowDecodeWarnings bool
	// Logf 接收**已脱敏**的诊断行；nil 表示静默。
	Logf func(format string, args ...any)
}

// Run 执行 Resolve → Fetch → Decrypt → Sanitize → Probe。
func (p Pipeline) Run(ctx context.Context, reference string, quality source.Quality, resolver Resolver) (Result, error) {
	result := Result{Stages: make([]Stage, 0, len(StageNames))}
	clock := newStageClock()

	// ── Resolve ────────────────────────────────────────────────────────────
	if resolver == nil {
		result.Stages = append(result.Stages, clock.finish("resolve", false, "", "未提供 Resolver"))
		return result, fmt.Errorf("%w: 未提供 Resolver", ErrResolveFailed)
	}
	stream, err := resolver.Resolve(ctx, reference, quality)
	stage := clock.finish("resolve", false, "", "已解析出取流地址")
	if err != nil {
		result.Stages = append(result.Stages, stage)
		return result, fmt.Errorf("%w: %v", ErrResolveFailed, err)
	}
	result.Stream = stream
	if err := validateStream(stream); err != nil {
		result.Stages = append(result.Stages, stage)
		return result, err
	}
	if !stream.ExpiresAt.IsZero() && time.Now().After(stream.ExpiresAt) {
		result.Stages = append(result.Stages, stage)
		return result, fmt.Errorf("%w（%s）", ErrExpiredPlan, stream.ExpiresAt.Format(time.RFC3339))
	}
	result.Stages = append(result.Stages, stage)

	// HLS 与 MP4 走两条分支，但阶段序列不变（跳过者留 Reason）。
	if stream.Container == source.ContainerHLS {
		if err := p.runHLS(ctx, &result, stream, clock); err != nil {
			return result, err
		}
	} else {
		if err := p.runProgressiveMP4(ctx, &result, stream, clock); err != nil {
			return result, err
		}
	}

	// ── Probe ──────────────────────────────────────────────────────────────
	target := result.Playback
	fetcher := p.FFmpeg
	fetcher.Logf = p.Logf
	probeOptions := ProbeOptions{Timeout: p.ProbeTimeout, Headers: stream.Headers}
	probe, err := fetcher.ProbePlayable(ctx, target, probeOptions)
	stage = clock.finish("probe", false, "", fmt.Sprintf("%s %s", probe.VideoSummary(), target))
	if err != nil {
		result.Stages = append(result.Stages, stage)
		return result, fmt.Errorf("探测产物失败: %w", err)
	}
	result.Probe = probe

	// 本地产物额外做「真解码几帧」的验收：ffprobe 全绿也可能 mdat 全是垃圾
	// （错密钥/过期密钥的典型形态，见 FFmpeg.VerifyDecode 的实测说明）。
	// 远端直连不做这一步 —— 那属于播放器的职责，且不该为了验收把整段视频拉下来。
	frames := p.DecodeVerifyFrames
	if frames == 0 {
		frames = DefaultVerifyFrames
	}
	if result.Local && frames > 0 {
		verify, verifyErr := fetcher.VerifyDecode(ctx, target, VerifyOptions{
			Frames:           frames,
			Timeout:          p.DecryptTimeout,
			AllowDiagnostics: p.AllowDecodeWarnings,
		})
		if verifyErr != nil {
			stage.Detail += fmt.Sprintf("；解码验收失败（%d 帧）", frames)
			result.Stages = append(result.Stages, stage)
			return result, fmt.Errorf("%w: %v", ErrDecryptFailed, verifyErr)
		}
		stage.Detail += fmt.Sprintf("；解码验收 %d 帧通过", verify.Frames)
	}
	result.Stages = append(result.Stages, stage)
	return result, nil
}

// runProgressiveMP4 处理 mp4 直链：未加密直连；加密则 ffmpeg 解密（+faststart），
// 再按 NeedsSanitize 决定是否净化。
func (p Pipeline) runProgressiveMP4(ctx context.Context, result *Result, stream source.Stream, clock *stageClock) error {
	// ── Fetch ──：直链优先，不做本地化（server.py:981 的实测结论）。
	result.Stages = append(result.Stages, clock.finish("fetch", true,
		"直链优先：CDN 地址直接交给播放器，绕开 ffmpeg 单连接下载瓶颈", ""))

	if !stream.Encrypted {
		result.Playback = stream.URL
		result.Stages = append(result.Stages, clock.finish("decrypt", true,
			"未加密 MP4：无需解密", ""))
		result.Stages = append(result.Stages, clock.finish("sanitize", true,
			"未加密：无 CENC 信令需要剔除", ""))
		return nil
	}

	output, err := TempOutput(p.TempDir, "decrypted")
	if err != nil {
		return err
	}
	ffmpeg := p.FFmpeg
	ffmpeg.Logf = p.Logf
	resolved, err := ffmpeg.Resolve()
	if err != nil {
		return err
	}
	command, err := resolved.DecryptFaststartArgs(stream.URL, output, stream.DecryptionKey)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDecryptFailed, err)
	}
	run, err := resolved.Run(ctx, command, RunOptions{
		Timeout:      p.DecryptTimeout,
		ExpectOutput: output,
		MinBytes:     MinPlayableBytes,
	})
	if err != nil {
		// 刻意不沿用 server.py:1104 的「净化加密原文件」兜底（见文件头分歧说明）。
		os.Remove(output)
		result.Stages = append(result.Stages, clock.finish("decrypt", false, "", command.Redacted()))
		return fmt.Errorf("%w: %v", ErrDecryptFailed, err)
	}
	result.Decrypted = true
	result.Cleanup = append(result.Cleanup, output)
	result.Stages = append(result.Stages, clock.finish("decrypt", false, "",
		fmt.Sprintf("%s → %d 字节", command.Redacted(), run.OutputBytes)))

	// ── Sanitize ──：只有「加密 MP4」需要（§4.1 Stream.NeedsSanitize 由 Resolve 带出）。
	if !stream.NeedsSanitize {
		result.Playback = output
		result.Local = true
		result.Stages = append(result.Stages, clock.finish("sanitize", true,
			"该源的 Resolve 声明无需净化（NeedsSanitize=false）", ""))
		return nil
	}
	sanitizedPath, sanitizeInfo, err := p.sanitizeFile(ctx, output)
	if err != nil {
		result.Stages = append(result.Stages, clock.finish("sanitize", false, "", err.Error()))
		return err
	}
	result.Sanitized = &sanitizeInfo
	result.Cleanup = append(result.Cleanup, sanitizedPath)
	result.Playback = sanitizedPath
	result.Local = true
	result.Stages = append(result.Stages, clock.finish("sanitize", false, "",
		fmt.Sprintf("剔除 %v，mdat 位移 %d，moov %d 字节，%d 遍",
			sanitizeInfo.RemovedStblBoxes, sanitizeInfo.Delta, sanitizeInfo.MoovSize, sanitizeInfo.Passes)))
	return nil
}

// runHLS 处理 m3u8：本地化（取密钥）→ 转封装成 MP4（Chromium 播不了 m3u8）。
func (p Pipeline) runHLS(ctx context.Context, result *Result, stream source.Stream, clock *stageClock) error {
	localized, err := LocalizeHLS(ctx, stream.URL, LocalizeOptions{
		WorkDir:   p.TempDir,
		Referer:   stream.Headers["Referer"],
		UserAgent: p.FFmpeg.UserAgent,
		Fetcher:   p.Fetcher,
		Logf:      p.Logf,
	})
	if err != nil {
		result.Stages = append(result.Stages, clock.finish("fetch", false, "", err.Error()))
		return fmt.Errorf("HLS 本地化失败: %w", err)
	}
	if localized.Warning != "" {
		result.Warnings = append(result.Warnings, localized.Warning)
	}
	result.Cleanup = append(result.Cleanup, localized.Cleanup...)
	detail := fmt.Sprintf("清单 %s", localized.Variant)
	if localized.Encrypted {
		detail += "，AES-128 密钥已本地化"
	}
	result.Stages = append(result.Stages, clock.finish("fetch", false, "", detail))

	if p.AllowRemoteHLS && !localized.Encrypted {
		result.Playback = localized.Variant
		result.Stages = append(result.Stages, clock.finish("decrypt", true,
			"AllowRemoteHLS：未加密 HLS 交给播放器自行解封装", ""))
		result.Stages = append(result.Stages, clock.finish("sanitize", true,
			"HLS 是分片级解密，不经 MP4 净化", ""))
		return nil
	}

	output, err := TempOutput(p.TempDir, "hls")
	if err != nil {
		return err
	}
	ffmpeg := p.FFmpeg
	ffmpeg.Logf = p.Logf
	resolved, err := ffmpeg.Resolve()
	if err != nil {
		return err
	}
	command := resolved.HLSRemuxArgs(localized.Input, localized.ExtraArgs, output)
	timeout := p.RemuxTimeout
	if timeout <= 0 {
		timeout = DefaultRemuxTimeout
	}
	run, err := resolved.Run(ctx, command, RunOptions{
		Timeout:      timeout,
		ExpectOutput: output,
		MinBytes:     MinPlayableBytes,
	})
	if err != nil {
		os.Remove(output)
		result.Stages = append(result.Stages, clock.finish("decrypt", false, "", command.Redacted()))
		return fmt.Errorf("%w: HLS 转封装失败: %v", ErrDecryptFailed, err)
	}
	result.Remuxed = true
	result.Cleanup = append(result.Cleanup, output)
	result.Playback = output
	result.Local = true
	result.Stages = append(result.Stages, clock.finish("decrypt", false, "",
		fmt.Sprintf("转封装（Chromium 不支持 m3u8）→ %d 字节", run.OutputBytes)))
	// HLS 的 AES-128 是分片级解密，产物是普通 MP4，不做 CENC 净化。
	result.Stages = append(result.Stages, clock.finish("sanitize", true,
		"HLS 是分片级解密，不经 MP4 净化", ""))
	return nil
}

// sanitizeFile 净化并**验证**：净化产物必须能探测出视频流，否则报错。
//
// 这里多做一步探测（A 只检查文件大小）的原因：净化会重写 stco/co64 偏移，
// 偏移写错时文件仍能 >1024 字节、仍能被 ffprobe 认出封装，但解码会失败。
// 探测通过 + sanitize_test.go 的解码帧数断言共同构成「偏移写对了」的证据。
func (p Pipeline) sanitizeFile(ctx context.Context, input string) (string, SanitizeResult, error) {
	output, err := TempOutput(p.TempDir, "sanitized")
	if err != nil {
		return "", SanitizeResult{}, err
	}
	logf := p.Logf
	info, err := SanitizeFile(input, output, SanitizeOptions{Logf: logf})
	if err != nil {
		os.Remove(output)
		if errors.Is(err, ErrNoMoov) {
			return "", SanitizeResult{}, fmt.Errorf("%w: %v", ErrSanitizeFailed, err)
		}
		return "", SanitizeResult{}, fmt.Errorf("%w: %v", ErrSanitizeFailed, err)
	}
	if _, err := StatPlayable(output, MinPlayableBytes); err != nil {
		os.Remove(output)
		return "", SanitizeResult{}, fmt.Errorf("%w: %v", ErrSanitizeFailed, err)
	}
	ffmpeg := p.FFmpeg
	ffmpeg.Logf = p.Logf
	if _, err := ffmpeg.ProbePlayable(ctx, output, ProbeOptions{Timeout: p.ProbeTimeout}); err != nil {
		os.Remove(output)
		return "", SanitizeResult{}, fmt.Errorf("%w: 净化产物无法探测出视频流: %v", ErrSanitizeFailed, err)
	}
	return output, info, nil
}

// validateStream 做与 core/source/adapter 同一套判据的入口校验（§4.1 的 Stream 契约）。
func validateStream(stream source.Stream) error {
	if stream.URL == "" {
		return fmt.Errorf("%w: 取流地址为空", ErrInvalidStream)
	}
	if stream.Encrypted {
		if stream.DecryptionKey == "" {
			return fmt.Errorf("%w: 声明加密但未给出解密密钥（%s）", ErrInvalidStream, stream.URL)
		}
		if !contentKeyPattern.MatchString(stream.DecryptionKey) {
			return fmt.Errorf("%w: 解密密钥不是 32 位十六进制", ErrInvalidStream)
		}
		if len(stream.DecryptionKey) != 32 {
			return fmt.Errorf("%w: 解密密钥长度不是 16 字节", ErrInvalidStream)
		}
	}
	switch stream.Container {
	case source.ContainerMP4, source.ContainerHLS:
	case "":
		return fmt.Errorf("%w: 封装形态为空（应为 mp4 或 hls）", ErrInvalidStream)
	default:
		return fmt.Errorf("%w: 未知封装形态 %q", ErrInvalidStream, stream.Container)
	}
	return nil
}

// stageClock 负责给每个阶段打耗时（阶段之间的时间差）。
type stageClock struct {
	last time.Time
}

func newStageClock() *stageClock { return &stageClock{last: time.Now()} }

func (c *stageClock) finish(name string, skipped bool, reason, detail string) Stage {
	now := time.Now()
	stage := Stage{Name: name, Skipped: skipped, Reason: reason, Detail: detail, Elapsed: now.Sub(c.last)}
	c.last = now
	return stage
}
