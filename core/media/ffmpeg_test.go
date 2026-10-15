// 本仓新增（M3 媒体管线测试）。
//
// 这里断言的是**参数面**：每条命令行与 A / C 的权威实现逐字对应。
// 这些 argv 是实测结论的载体（少一个 -bsf:a aac_adtstoasc 就是 Malformed AAC bitstream，
// 少一个 -movflags +faststart 就不能 Range seek），因此用断言钉住而不是靠注释提醒。
package media_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dawei233/hongguo-next/core/media"
)

const (
	testCENCKey = "000102030405060708090a0b0c0d0e0f"
	testIn      = "https://cdn.example.test/movie.mp4"
	testOut     = "/tmp/out.mp4"
)

func TestDecryptFaststartArgsMatchesServerPy(t *testing.T) {
	ffmpeg := media.FFmpeg{}
	command, err := ffmpeg.DecryptFaststartArgs(testIn, testOut, testCENCKey)
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	// server.py:1089-1097 的逐字 argv。
	want := []string{"-y", "-decryption_key", testCENCKey, "-i", testIn,
		"-c", "copy", "-movflags", "+faststart", testOut}
	assertArgs(t, command.Args, want)
	if len(command.Secrets) != 1 || command.Secrets[0] != testCENCKey {
		t.Fatalf("密钥必须登记进 Secrets（用于脱敏），实际 %v", command.Secrets)
	}
}

func TestDecryptStreamArgsMatchesProgressiveMediaJs(t *testing.T) {
	ffmpeg := media.FFmpeg{}
	command, err := ffmpeg.DecryptStreamArgs(testIn, testOut, testCENCKey)
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	// progressive-media.js:54-58 的逐字 argv（含 -map / 分片 movflags / -f mp4）。
	want := []string{"-y", "-hide_banner", "-loglevel", "error", "-decryption_key", testCENCKey,
		"-i", testIn, "-map", "0:v:0", "-map", "0:a?", "-c", "copy",
		"-movflags", media.FragmentedMovFlags, "-f", "mp4", testOut}
	assertArgs(t, command.Args, want)
	if media.FragmentedMovFlags != "frag_keyframe+empty_moov+default_base_moof" {
		t.Fatalf("分片 movflags 被改动：%s（progressive-media.js:38 的实测取值）", media.FragmentedMovFlags)
	}
}

func TestDecryptArgsAllowEmptyKey(t *testing.T) {
	ffmpeg := media.FFmpeg{}
	command, err := ffmpeg.DecryptStreamArgs(testIn, testOut, "")
	if err != nil {
		t.Fatalf("未加密片源也应能走同一构造器: %v", err)
	}
	if strings.Contains(strings.Join(command.Args, " "), "-decryption_key") {
		t.Fatal("空密钥时不得出现 -decryption_key")
	}
	if len(command.Secrets) != 0 {
		t.Fatal("空密钥时不应有 Secrets")
	}
}

func TestDecryptArgsRejectInvalidKey(t *testing.T) {
	ffmpeg := media.FFmpeg{}
	// progressive-media.js:51 的校验：必须 32 位十六进制。
	for _, key := range []string{"short", strings.Repeat("z", 32), testCENCKey + "0", strings.Repeat("a", 31)} {
		if _, err := ffmpeg.DecryptFaststartArgs(testIn, testOut, key); err == nil {
			t.Errorf("密钥 %q 应被拒绝", key)
		}
	}
}

func TestFaststartRemuxArgsMatchesServerPy(t *testing.T) {
	ffmpeg := media.FFmpeg{}
	command := ffmpeg.FaststartRemuxArgs("part.mp4", "final.mp4")
	// server.py:502-503 的逐字 argv（-f mp4 与 .mp4 后缀都在）。
	assertArgs(t, command.Args, []string{"-y", "-i", "part.mp4", "-c", "copy",
		"-movflags", "+faststart", "-f", "mp4", "final.mp4"})
}

func TestHLSRemuxArgsMatchesMediaPy(t *testing.T) {
	ffmpeg := media.FFmpeg{}
	extra := []string{"-allowed_extensions", "ALL", "-protocol_whitelist", "file,http,https,tcp,tls,crypto"}
	command := ffmpeg.HLSRemuxArgs("/tmp/hls_x.m3u8", extra, "out.mp4")
	want := append([]string{"-y", "-loglevel", "error"}, extra...)
	want = append(want, "-i", "/tmp/hls_x.m3u8", "-c", "copy", "-bsf:a", "aac_adtstoasc",
		"-movflags", "+frag_keyframe+empty_moov", "out.mp4")
	assertArgs(t, command.Args, want)

	// output 为空 → 流式吐到 stdout（media.py:169 的 `-f mp4 -`）。
	streaming := ffmpeg.HLSRemuxArgs(testIn, nil, "")
	if got := streaming.Args[len(streaming.Args)-3:]; got[0] != "-f" || got[1] != "mp4" || got[2] != "-" {
		t.Fatalf("流式形态应以 -f mp4 - 收尾，实际 %v", streaming.Args)
	}
}

func TestLocalHLSRemuxArgsMatchesHlsMediaJs(t *testing.T) {
	ffmpeg := media.FFmpeg{}
	command := ffmpeg.LocalHLSRemuxArgs("/tmp/staged/index.m3u8", "out.mp4")
	// hls-media.js:108-110 的逐字 argv。
	assertArgs(t, command.Args, []string{"-v", "error", "-nostdin", "-y",
		"-protocol_whitelist", "file,crypto", "-allowed_extensions", "ALL",
		"-i", "/tmp/staged/index.m3u8", "-map", "0:v:0", "-map", "0:a?", "-c", "copy",
		"-movflags", "+faststart", "-f", "mp4", "out.mp4"})
}

func TestHeaderArgsUsesCRLF(t *testing.T) {
	ffmpeg := media.FFmpeg{}
	args := ffmpeg.HeaderArgs("https://site.example.test/watch", "")
	joined := strings.Join(args, "|")
	// media.py:109：`-headers "Referer: %s\r\n"`，行尾必须是 CRLF。
	if !strings.Contains(joined, "-headers|Referer: https://site.example.test/watch\r\n") {
		t.Fatalf("Referer 头形态不对: %q", joined)
	}
	if !strings.Contains(joined, "-user_agent|"+media.DefaultUserAgent) {
		t.Fatalf("缺少 -user_agent: %q", joined)
	}
	// 空 Referer → 只有 UA（media.py:107-110 的分支）。
	if got := ffmpeg.HeaderArgs("", ""); len(got) != 2 {
		t.Fatalf("空 Referer 时只应有 -user_agent <ua>，实际 %v", got)
	}
}

func TestCommandRedactedHidesKey(t *testing.T) {
	command, err := media.FFmpeg{}.DecryptFaststartArgs(testIn, testOut, testCENCKey)
	if err != nil {
		t.Fatalf("%v", err)
	}
	redacted := command.Redacted()
	if strings.Contains(redacted, testCENCKey) {
		t.Fatalf("脱敏后仍含密钥: %s", redacted)
	}
	if !strings.Contains(redacted, "<redacted>") {
		t.Fatalf("脱敏后应标出 <redacted>: %s", redacted)
	}
	if !strings.Contains(redacted, "-decryption_key") {
		t.Fatalf("脱敏只替换密钥值，不应连选项一起去掉: %s", redacted)
	}
}

// TestRunRedactsKeyFromStderr 用一个假 ffmpeg 验证：即使外部程序把密钥回显到 stderr，
// 进入错误信息与 RunResult 的都必须是脱敏文本（progressive-media.js:55 的硬要求）。
func TestRunRedactsKeyFromStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("用 POSIX shell 脚本模拟外部程序，Windows 跳过")
	}
	script := writeScript(t, `#!/bin/sh
echo "ffmpeg version fake" >&2
for arg in "$@"; do
  if [ "$arg" = "-decryption_key" ]; then echo "invalid key $2" >&2; fi
done
echo "decryption key $3 rejected" >&2
exit 1
`)
	ffmpeg := media.FFmpeg{Path: script, ProbePath: script}
	command, err := ffmpeg.DecryptFaststartArgs(testIn, testOut, testCENCKey)
	if err != nil {
		t.Fatalf("%v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, runErr := ffmpeg.Run(ctx, command, media.RunOptions{})
	if runErr == nil {
		t.Fatal("假 ffmpeg 退出码非 0，应报错")
	}
	for label, text := range map[string]string{"error": runErr.Error(), "stderr": result.StderrTail} {
		if strings.Contains(text, testCENCKey) {
			t.Fatalf("%s 里泄漏了密钥: %s", label, text)
		}
	}
	if result.ExitCode != 1 {
		t.Fatalf("退出码应为 1，实际 %d", result.ExitCode)
	}
}

// TestRunExpectOutputEnforcesMinBytes 覆盖产物验收（server.py:1071 的 1024 字节判据）。
func TestRunExpectOutputEnforcesMinBytes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell 脚本，Windows 跳过")
	}
	script := writeScript(t, `#!/bin/sh
out=""
for arg in "$@"; do out="$arg"; done
printf 'tiny' > "$out"
exit 0
`)
	ffmpeg := media.FFmpeg{Path: script, ProbePath: script}
	ctx := context.Background()
	target := filepath.Join(t.TempDir(), "out.mp4")
	command := media.Command{Label: "fake", Args: []string{target}}
	if _, err := ffmpeg.Run(ctx, command, media.RunOptions{ExpectOutput: target}); err == nil {
		t.Fatal("产物只有 4 字节，应被判为空产物")
	}
	// 放宽下限后同一条命令应通过。
	if _, err := ffmpeg.Run(ctx, command, media.RunOptions{ExpectOutput: target, MinBytes: 1}); err != nil {
		t.Fatalf("放宽下限后应通过: %v", err)
	}
	// 未要求产物时，即使文件不达标也不报错（探测场景）。
	if _, err := ffmpeg.Run(ctx, command, media.RunOptions{}); err != nil {
		t.Fatalf("未要求产物时不应报错: %v", err)
	}
}

func TestResolveRejectsMissingTool(t *testing.T) {
	ffmpeg := media.FFmpeg{Path: filepath.Join(t.TempDir(), "no-such-ffmpeg"), ProbePath: "/bin/sh"}
	if _, err := ffmpeg.Resolve(); err == nil {
		t.Fatal("指向不存在的绝对路径应报 ErrFFmpegMissing")
	} else if !strings.Contains(err.Error(), "ffmpeg") {
		t.Fatalf("错误信息应提到 ffmpeg: %v", err)
	}
}

func TestTempOutputKeepsMP4Suffix(t *testing.T) {
	dir := t.TempDir()
	first, err := media.TempOutput(dir, "decrypted")
	if err != nil {
		t.Fatalf("%v", err)
	}
	second, err := media.TempOutput(dir, "decrypted")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if first == second {
		t.Fatal("两次申请应得到不同路径")
	}
	for _, path := range []string{first, second} {
		// ADR-0004 / server.py:500：中间文件必须保留 .mp4 后缀。
		if !strings.HasSuffix(path, ".mp4") {
			t.Fatalf("临时输出必须保留 .mp4 后缀，实际 %s", path)
		}
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			t.Fatalf("%s 不是已创建的文件: %v", path, err)
		}
	}
	// 前缀里的路径分隔符与空格被归一（沿用 server.py:494-495 的思路）：
	// 产物必须仍落在 dir 下，不能因为前缀带 ../ 就跑到别处。
	weird, err := media.TempOutput(dir, "../../etc/pa ss:wd")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if filepath.Dir(weird) != dir {
		t.Fatalf("归一失败：产物跑到 %s（期望留在 %s）", filepath.Dir(weird), dir)
	}
	if base := filepath.Base(weird); strings.ContainsAny(base, " /\\:") {
		t.Fatalf("文件名里仍含分隔符或空格: %q", base)
	}
}

func assertArgs(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("参数个数不同：got=%v\nwant=%v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("第 %d 个参数不同：got=%q want=%q\n完整: got=%v\nwant=%v",
				index, got[index], want[index], got, want)
		}
	}
}

// writeScript 落一个可执行的 shell 脚本（模拟外部程序）。
func writeScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-tool.sh")
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatalf("%v", err)
	}
	return path
}
