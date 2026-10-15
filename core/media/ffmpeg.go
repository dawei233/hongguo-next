// 本仓新增（M3 媒体管线）。
//
// 本文件承接 A 的两处实测能力（ARCHITECTURE §3.1 #2）：
//
//  1. `hongguo-monorepo/nas-backend/server.py` 1087-1117（解密 + faststart + 二次净化）
//  2. `refs/hongguo-drama/src/native/progressive-media.js` 48-106（边解密边播，分片 MP4）
//
// 以及 HLS 侧的两处转封装形态：
//
//  3. `nas-backend/sources/media.py` 160-206（服务端把 m3u8 转成可播 MP4）
//  4. `refs/hongguo-drama/src/native/hls-media.js` 104-117（本地暂存 HLS → MP4）
//
// # 固化下来的实测结论（改这几条前请先看对应 fixture 与注释里的证据）
//
//   - `ffmpeg -decryption_key K -i URL -c copy` 的产物**仍会残留 sgpd/sbgp**
//     （faststart remux 让它们回来），因此「解密」之后**必须**接一次
//     core/media/sanitize；实测字节见 testdata/real_faststart_remux.meta.json
//     （输入 91178 → 净化 91124，剔掉 sgpd 26B + sbgp 28B）。ADR-0004。
//   - 中间文件**必须保留 .mp4 后缀**：ffmpeg 靠后缀推断封装，`.part`/`.tmp` 会失败
//     （server.py:500 `_src_finalize` 的注释、hls-media.js 同样把产物写成 .mp4）。
//   - HLS 的音轨多为 ADTS AAC，`-c copy` 直出会 "Malformed AAC bitstream"，
//     必须带 `-bsf:a aac_adtstoasc`（media.py:13-14）。
//   - 输入是**本地 playlist** 时 ffmpeg 不接受 `-headers`/`-user_agent`
//     （报 `Option not found`），且要放开 `-allowed_extensions ALL` 与
//     `-protocol_whitelist`（media.py:17-18）。
//   - **密钥只经命令行参数传递，绝不进日志**（progressive-media.js:55）。
//     本文件用 Command.Secrets + Redacted 强制这一点。
//   - 净化**不是**解密：加密原文件上跑 sanitize 只会得到「没有 CENC 信令、但 mdat
//     仍是密文」的文件。A 的 server.py:1104 在 ffmpeg 失败时正是这么兜底的；
//     本仓**不沿用**该兜底（见 pipeline.go 的说明与 sanitize_test.go 的实测断言）。
package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// 关键常量，均来自 A 的实测取值，不得随意改动。
const (
	// FragmentedMovFlags 是 progressive-media.js:38 的分片 MP4 movflags：
	// 不把 moov 留到结尾，写到哪就能播到哪（约 21MB / 2 秒即可起播）。
	FragmentedMovFlags = "frag_keyframe+empty_moov+default_base_moof"
	// FaststartMovFlags 是终态化用的一遍（moov 前置，换取可精确 seek 与持久缓存）。
	FaststartMovFlags = "+faststart"
	// HLSFragmentedMovFlags 是 media.py:165 用于「流式响应给 <video>」的分片形态。
	HLSFragmentedMovFlags = "+frag_keyframe+empty_moov"
	// ADTSAACBitstreamFilter 修 ADTS AAC 被 -c copy 直出时的 Malformed AAC bitstream。
	ADTSAACBitstreamFilter = "aac_adtstoasc"
	// DefaultUserAgent 与 media.py/JS 侧一致（移动端 UA）。
	DefaultUserAgent = "Mozilla/5.0"
	// MinPlayableBytes 是「产物非空且可解析」的下限（server.py:1071 用 1024）。
	MinPlayableBytes = 1024
	// MinFinalizedBytes 是 A 对站源转封装产物的验收下限（server.py:505/536 用 100000）。
	MinFinalizedBytes = 100000
	// DefaultDecryptTimeout / DefaultRemuxTimeout 取自 server.py:1099（600s）
	// 与 hls-media.js:33（10 分钟）。
	DefaultDecryptTimeout = 10 * time.Minute
	DefaultRemuxTimeout   = 15 * time.Minute
	// DefaultProbeTimeout 是 ffprobe 的超时（server.py:1077 用 60s）。
	DefaultProbeTimeout = 60 * time.Second
	// DefaultVerifyFrames 是解密后的解码验收帧数（见 VerifyDecode 的说明）。
	DefaultVerifyFrames = 30
)

// ErrDecodeDiagnostics 表示产物能探测出封装与编码参数，但真解码时吐诊断 —— 实测中最常见
// 的成因是**内容密钥已失效/给错**（ffmpeg 不校验 -decryption_key，见 VerifyDecode 注释）。
var ErrDecodeDiagnostics = errors.New("产物解码有诊断输出")

// VerifyOptions 控制一次解码验收。
type VerifyOptions struct {
	// Frames 为 0 时取 DefaultVerifyFrames。
	Frames int
	// Timeout 为 0 时取 DefaultDecryptTimeout。
	Timeout time.Duration
	// AllowDiagnostics 为 true 时只看退出码，不要求 stderr 为空。
	// 少数直播/老片源会打印无害的解码告警（如 "co located POCs unavailable"），
	// 此时把它放开而不是让整条链路失败。
	AllowDiagnostics bool
}

// VerifyResult 是一次解码验收的证据。
type VerifyResult struct {
	// Frames 是请求验收的帧数（实际解码帧数可能更少，短视频或坏流会提前结束）。
	Frames int
	// Diagnostics 是**已脱敏**的 ffmpeg 诊断（-v error 级别）。
	Diagnostics string
	Elapsed     time.Duration
}

// VerifyDecode 真解码若干帧做验收，而不是只看容器。
//
// # 为什么必须做这一步（实测结论，2026-10-15）
//
// ffmpeg **不校验** `-decryption_key`：用错密钥解 CENC-AES-CTR 的样本时，
// 它同样退出 0，产物大小与正确密钥**完全相同**（样例都是 91178 字节，因为只有 mdat
// 的样本字节被解密，moov/stsd/stco 未加密），ffprobe 也全绿（h264 320x180、
// nb_frames=45、extradata_hash 一致）—— 唯一的差别是真解码时吐出成片的
// `SEI … truncated` / `slice type … too large` / `no frame!`，并让 ffmpeg 以非零码退出。
//
// 也就是说，只做 ffprobe 的验收**无法**发现「内容密钥失效」这类故障，
// 而它恰好是站源最常见的故障形态（播放计划过期）。A 的 server.py 在这里只做 ffprobe，
// 靠客户端 ExoPlayer 报 ERROR_CODE_PARSING_CONTAINER_MALFORMED 兜底；
// 本仓把这一步提前到服务端，宁可在这里失败，也不把一个静默损坏的流交给用户。
func (f FFmpeg) VerifyDecode(ctx context.Context, path string, options VerifyOptions) (VerifyResult, error) {
	if strings.TrimSpace(path) == "" {
		return VerifyResult{}, errors.New("缺少解码验收目标")
	}
	resolved, err := f.Resolve()
	if err != nil {
		return VerifyResult{}, err
	}
	frames := options.Frames
	if frames <= 0 {
		frames = DefaultVerifyFrames
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = DefaultDecryptTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// -v error 只保留错误级诊断；-frames:v 让 ffmpeg 解够就停，不必读完整文件。
	command := Command{Label: "verify-decode", Args: []string{"-v", "error", "-nostdin",
		"-i", path, "-frames:v", strconv.Itoa(frames), "-f", "null", "-"}}
	started := time.Now()
	process := exec.CommandContext(runCtx, resolved.Path, command.Args...)
	var stderr bytes.Buffer
	process.Stderr = &stderr
	runErr := process.Run()
	result := VerifyResult{Frames: frames, Diagnostics: command.redact(tailString(stderr.String(), 2048)),
		Elapsed: time.Since(started)}
	if runErr != nil {
		return result, fmt.Errorf("%w: 解码 %d 帧失败: %s", ErrDecodeDiagnostics, frames, result.Diagnostics)
	}
	if !options.AllowDiagnostics && strings.TrimSpace(result.Diagnostics) != "" {
		return result, fmt.Errorf("%w: 解码 %d 帧有诊断（最常见成因是内容密钥已失效）: %s",
			ErrDecodeDiagnostics, frames, result.Diagnostics)
	}
	f.logf("verify-decode 通过：%d 帧，用时 %s", frames, result.Elapsed.Round(time.Millisecond))
	return result, nil
}

// contentKeyPattern 对应 progressive-media.js:51 的校验：32 位十六进制。
var contentKeyPattern = regexp.MustCompile(`^[a-fA-F0-9]{32}$`)

// ErrFFmpegMissing 表示环境里找不到 ffmpeg/ffprobe。
var ErrFFmpegMissing = errors.New("找不到 ffmpeg/ffprobe 可执行文件")

// ErrEmptyOutput 表示 ffmpeg 退出码为 0 但产物不存在或小于验收下限。
var ErrEmptyOutput = errors.New("ffmpeg 产物为空或过小")

// ErrCommandFailed 表示 ffmpeg/ffprobe 非零退出。
var ErrCommandFailed = errors.New("命令执行失败")

// FFmpeg 是外部工具的位置与调用参数来源。
//
// Path/ProbePath 为空时按环境变量 HONGGUO_FFMPEG / HONGGUO_FFPROBE，再退到 PATH。
// UserAgent 为空时取 DefaultUserAgent。
type FFmpeg struct {
	Path      string
	ProbePath string
	UserAgent string
	// Logf 接收**已脱敏**的诊断行；nil 表示静默。
	Logf func(format string, args ...any)
}

// Resolve 补齐空字段并确认可执行文件存在。
func (f FFmpeg) Resolve() (FFmpeg, error) {
	if f.Path == "" {
		f.Path = firstNonEmptyString(os.Getenv("HONGGUO_FFMPEG"), "ffmpeg")
	}
	if f.ProbePath == "" {
		f.ProbePath = firstNonEmptyString(os.Getenv("HONGGUO_FFPROBE"), "ffprobe")
	}
	if f.UserAgent == "" {
		f.UserAgent = DefaultUserAgent
	}
	for _, path := range []string{f.Path, f.ProbePath} {
		if strings.ContainsRune(path, os.PathSeparator) {
			if info, err := os.Stat(path); err != nil || info.IsDir() {
				return FFmpeg{}, fmt.Errorf("%w: %s", ErrFFmpegMissing, path)
			}
			continue
		}
		if _, err := exec.LookPath(path); err != nil {
			return FFmpeg{}, fmt.Errorf("%w: %s", ErrFFmpegMissing, path)
		}
	}
	return f, nil
}

// Command 是一条待执行的外部命令 + 需要脱敏的片段。
//
// Secrets 里的每个片段在日志与错误信息中都会被替换成 "<redacted>"，
// 这是 §3.1 的硬要求（密钥不得落日志）。
type Command struct {
	// Label 是给日志用的可读名（如 "decrypt"、"sanitize-remux"）。
	Label string
	// Args 是**不含**可执行文件名的参数表（便于单测断言 argv）。
	Args []string
	// Secrets 必须与 Args 中出现的字面量逐字一致（通常是密钥）。
	Secrets []string
}

// Redacted 返回可安全写日志的命令行。
func (c Command) Redacted() string {
	return c.Label + ": " + c.redact(strings.Join(c.Args, " "))
}

func (c Command) redact(text string) string {
	for _, secret := range c.Secrets {
		if secret == "" {
			continue
		}
		text = strings.ReplaceAll(text, secret, "<redacted>")
	}
	return text
}

// RunOptions 控制一次外部命令执行。
type RunOptions struct {
	// Timeout 为 0 时取 DefaultDecryptTimeout。
	Timeout time.Duration
	// ExpectOutput 非空时要求该路径存在且 >= MinBytes 才算成功。
	ExpectOutput string
	// MinBytes 为 0 时取 MinPlayableBytes；只有 ExpectOutput 非空时生效。
	MinBytes int64
	// StderrLimit 是保留的 stderr 尾部字节数（0 取 4096）。
	StderrLimit int
}

// RunResult 是一次执行的证据（供诊断与测试断言）。
type RunResult struct {
	ExitCode int
	Elapsed  time.Duration
	// StderrTail 已脱敏，且已按 StderrLimit 截断。
	StderrTail string
	// OutputBytes 是 ExpectOutput 的实测大小（未要求产物时为 0）。
	OutputBytes int64
}

// Run 执行命令并做产物验收。返回的 RunResult 无论成败都可用（含退出码与 stderr 尾部）。
func (f FFmpeg) Run(ctx context.Context, command Command, options RunOptions) (RunResult, error) {
	resolved, err := f.Resolve()
	if err != nil {
		return RunResult{}, err
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = DefaultDecryptTimeout
	}
	limit := options.StderrLimit
	if limit <= 0 {
		limit = 4096
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	started := time.Now()
	process := exec.CommandContext(runCtx, resolved.Path, command.Args...)
	var stderr bytes.Buffer
	process.Stderr = &stderr
	process.Stdout = nil
	runErr := process.Run()
	result := RunResult{Elapsed: time.Since(started)}
	if exitErr := (&exec.ExitError{}); errors.As(runErr, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
	} else if runErr != nil {
		result.ExitCode = -1
	}
	result.StderrTail = command.redact(tailString(stderr.String(), limit))
	f.logf("%s 完成 exit=%d 用时=%s", command.Label, result.ExitCode, result.Elapsed.Round(time.Millisecond))

	if runErr != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return result, fmt.Errorf("%w: %s 超时（%s）: %s", ErrCommandFailed, command.Label, timeout, result.StderrTail)
		}
		return result, fmt.Errorf("%w: %s exit=%d: %s", ErrCommandFailed, command.Label, result.ExitCode, result.StderrTail)
	}
	if options.ExpectOutput != "" {
		minBytes := options.MinBytes
		if minBytes <= 0 {
			minBytes = MinPlayableBytes
		}
		info, err := os.Stat(options.ExpectOutput)
		if err != nil {
			return result, fmt.Errorf("%w: %s 未产出 %s: %v", ErrEmptyOutput, command.Label, options.ExpectOutput, err)
		}
		result.OutputBytes = info.Size()
		if info.Size() < minBytes {
			return result, fmt.Errorf("%w: %s 产出 %d 字节 < %d（%s）",
				ErrEmptyOutput, command.Label, info.Size(), minBytes, options.ExpectOutput)
		}
	}
	return result, nil
}

func (f FFmpeg) logf(format string, args ...any) {
	if f.Logf != nil {
		f.Logf(format, args...)
	}
}

// DecryptStreamArgs 是 progressive-media.js:54-58 的逐字 argv：边解密边写分片 MP4。
//
// 用途：起播优先（写到哪播到哪，约 2 秒 / 21MB 即可起播），代价是 duration=Infinity、
// 不能拖动 —— 终态化（+faststart + 净化）由调用方在写完后另行处理。
//
// 与旧实现的差别（progressive-media.js:44）：不再先把整集下载到临时文件，
// 直接让 ffmpeg 读远端 URL，少一遍整集 IO。
func (f FFmpeg) DecryptStreamArgs(input, output, contentKey string) (Command, error) {
	if strings.TrimSpace(input) == "" {
		return Command{}, errors.New("缺少播放地址")
	}
	if output == "" {
		return Command{}, errors.New("缺少输出路径")
	}
	// key 可省略（未加密片源走同一条管线）。
	args := []string{"-y", "-hide_banner", "-loglevel", "error"}
	secrets := []string(nil)
	if contentKey != "" {
		if !contentKeyPattern.MatchString(contentKey) {
			return Command{}, fmt.Errorf("片源解密密钥格式无效（需 32 位十六进制）")
		}
		args = append(args, "-decryption_key", contentKey)
		secrets = append(secrets, contentKey)
	}
	args = append(args, "-i", input, "-map", "0:v:0", "-map", "0:a?", "-c", "copy",
		"-movflags", FragmentedMovFlags, "-f", "mp4", output)
	return Command{Label: "decrypt-stream", Args: args, Secrets: secrets}, nil
}

// DecryptFaststartArgs 是 server.py:1089-1097 的逐字 argv：解密 + 不重编码 + faststart。
//
// 产物是「普通 MP4、moov 前置」，可 Range seek，但**仍带 sgpd/sbgp**，必须接 sanitize。
func (f FFmpeg) DecryptFaststartArgs(input, output, contentKey string) (Command, error) {
	if strings.TrimSpace(input) == "" {
		return Command{}, errors.New("缺少播放地址")
	}
	if output == "" {
		return Command{}, errors.New("缺少输出路径")
	}
	args := []string{"-y"}
	secrets := []string(nil)
	if contentKey != "" {
		if !contentKeyPattern.MatchString(contentKey) {
			return Command{}, fmt.Errorf("片源解密密钥格式无效（需 32 位十六进制）")
		}
		args = append(args, "-decryption_key", contentKey)
		secrets = append(secrets, contentKey)
	}
	args = append(args, "-i", input, "-c", "copy", "-movflags", FaststartMovFlags, output)
	return Command{Label: "decrypt", Args: args, Secrets: secrets}, nil
}

// FaststartRemuxArgs 是 server.py:502-503 的逐字 argv：分片 MP4 → faststart 普通 MP4。
//
// `-f mp4` 与 `.mp4` 后缀都在（server.py:500 注释：必须保留 .mp4 后缀）。
func (f FFmpeg) FaststartRemuxArgs(input, output string) Command {
	return Command{Label: "faststart", Args: []string{"-y", "-i", input, "-c", "copy",
		"-movflags", FaststartMovFlags, "-f", "mp4", output}}
}

// HLSRemuxArgs 是 media.py:160-170 的 argv 形态（远端 m3u8 → 可直接播的 MP4）。
//
// stream=true 时输出到 stdout（`-f mp4 -`），用于边转边吐给 <video>；
// 否则写 output 文件。extraArgs 由 HLS 本地化决定（见 hls.go 的 Localize）：
// 未加密 → -user_agent/-headers；加密 → -allowed_extensions ALL -protocol_whitelist …。
func (f FFmpeg) HLSRemuxArgs(input string, extraArgs []string, output string) Command {
	args := []string{"-y", "-loglevel", "error"}
	args = append(args, extraArgs...)
	args = append(args, "-i", input, "-c", "copy", "-bsf:a", ADTSAACBitstreamFilter,
		"-movflags", HLSFragmentedMovFlags)
	if output == "" {
		args = append(args, "-f", "mp4", "-")
	} else {
		args = append(args, output)
	}
	return Command{Label: "hls-remux", Args: args}
}

// LocalHLSRemuxArgs 是 hls-media.js:108-110 的 argv 形态（本地暂存 HLS → MP4）。
//
// 与 HLSRemuxArgs 的区别是刻意的：本地 HLS 场景下 ffmpeg 不带 HTTP 传输，
// 因此把协议白名单收窄到 `file,crypto`（远端 URL 会被拒绝），并按 hls-media.js
// 只映射一条视频 + 可选音频、输出 +faststart（该场景不需要 ADTS 位流过滤器，
// 因为分片已由上游封装为 MP4/TS，实测两者都可用）。
func (f FFmpeg) LocalHLSRemuxArgs(input, output string) Command {
	return Command{Label: "hls-local-remux", Args: []string{"-v", "error", "-nostdin", "-y",
		"-protocol_whitelist", "file,crypto", "-allowed_extensions", "ALL",
		"-i", input, "-map", "0:v:0", "-map", "0:a?", "-c", "copy",
		"-movflags", FaststartMovFlags, "-f", "mp4", output}}
}

// HeaderArgs 生成 ffmpeg 的取流身份头参数（远端 URL 输入时可用）。
//
// 注意：输入改为本地 playlist 后**不能**再带这两个参数（media.py:17 实测
// ffmpeg 报 `Option not found`），因此 hls.go 在本地化成功时会丢弃它们。
func (f FFmpeg) HeaderArgs(referer string, userAgent string) []string {
	agent := firstNonEmptyString(userAgent, f.UserAgent, DefaultUserAgent)
	args := []string{"-user_agent", agent}
	if strings.TrimSpace(referer) != "" {
		// 行尾必须是 CRLF，ffmpeg 自己解析头块（media.py:109 同）。
		args = append(args, "-headers", "Referer: "+referer+"\r\n")
	}
	return args
}

// TempOutput 在目录下申请一个**保留 .mp4 后缀**的临时输出路径。
//
// 后缀不是风格问题：ffmpeg 靠后缀推断封装（ADR-0004 / server.py:500）。
// 调用方负责在成功后 os.Rename 到目标名、失败时删除。
func TempOutput(dir string, prefix string) (string, error) {
	if dir == "" {
		dir = os.TempDir()
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(dir, sanitizeFileComponent(prefix)+"-*.mp4")
	if err != nil {
		return "", err
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		return "", err
	}
	// ffmpeg 要求输出文件可写；保留 0600 的临时文件本体、直接覆盖写。
	return path, nil
}

var fileComponentPattern = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

// sanitizeFileComponent 把任意前缀收敛为安全文件名片段（沿用 server.py:494-495 的思路）。
func sanitizeFileComponent(raw string) string {
	cleaned := fileComponentPattern.ReplaceAllString(strings.TrimSpace(raw), "_")
	cleaned = strings.Trim(cleaned, "_")
	if cleaned == "" {
		return "media"
	}
	if len(cleaned) > 60 {
		cleaned = cleaned[len(cleaned)-60:]
	}
	return cleaned
}

// tailString 取字符串尾部至多 limit 字节（不切断 UTF-8 头部的近似处理：
// 直接从 limit 处截断后由调用方展示，诊断用途足够）。
func tailString(raw string, limit int) string {
	if limit <= 0 || len(raw) <= limit {
		return strings.TrimSpace(raw)
	}
	return "…" + strings.TrimSpace(raw[len(raw)-limit:])
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
