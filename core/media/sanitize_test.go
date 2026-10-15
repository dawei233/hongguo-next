// 本仓新增（M3 媒体管线测试）。
//
// 核心验收点：Go 转写（sanitize.go/boxes.go）与 A 的 Python 权威实现
// （hongguo-monorepo/nas-backend/mp4_sanitize.py）**逐字节一致**。
//
// 基准真值由 testdata/gen_golden.py 离线产出（该脚本直接 import A 的实现，
// 把它的输出落盘成 <name>.out.mp4）。本测试只读 fixture，**不调用 Python**，
// 因此可在无 Python 的 CI 上复现。
package media_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dawei233/hongguo-next/core/media"
)

const testdataDir = "testdata"

type goldenMeta struct {
	Name     string `json:"name"`
	Note     string `json:"note"`
	PythonOK bool   `json:"python_ok"`
	InBytes  int    `json:"in_bytes"`
	OutBytes *int   `json:"out_bytes"`
}

type goldenIndex struct {
	Source string       `json:"source"`
	Cases  []goldenMeta `json:"cases"`
}

func loadGoldenIndex(t *testing.T) goldenIndex {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(testdataDir, "golden_index.json"))
	if err != nil {
		t.Fatalf("读取 golden_index.json 失败（先用 testdata/gen_golden.py 生成）: %v", err)
	}
	var index goldenIndex
	if err := json.Unmarshal(body, &index); err != nil {
		t.Fatalf("golden_index.json 不是合法 JSON: %v", err)
	}
	if len(index.Cases) == 0 {
		t.Fatal("golden_index.json 里没有任何用例")
	}
	return index
}

// TestSanitizeMatchesPythonGolden 是逐字节基准断言：11 组 fixture 全部比对。
func TestSanitizeMatchesPythonGolden(t *testing.T) {
	index := loadGoldenIndex(t)
	for _, item := range index.Cases {
		item := item
		t.Run(item.Name, func(t *testing.T) {
			input, err := os.ReadFile(filepath.Join(testdataDir, item.Name+".in.mp4"))
			if err != nil {
				t.Fatalf("读取输入失败: %v", err)
			}
			if len(input) != item.InBytes {
				t.Fatalf("输入长度 %d 与 golden 记录 %d 不符（fixture 被改动过？）", len(input), item.InBytes)
			}
			result, err := media.SanitizeBytes(input, media.SanitizeOptions{})
			if !item.PythonOK {
				if err == nil {
					t.Fatal("Python 侧返回 False（无 moov），Go 侧却成功了")
				}
				if !strings.Contains(err.Error(), "moov") {
					t.Fatalf("期望 moov 相关错误，实际: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("净化失败: %v", err)
			}
			expected, err := os.ReadFile(filepath.Join(testdataDir, item.Name+".out.mp4"))
			if err != nil {
				t.Fatalf("读取期望输出失败: %v", err)
			}
			if item.OutBytes != nil && *item.OutBytes != len(expected) {
				t.Fatalf("期望输出长度 %d 与 golden 记录 %d 不符", len(expected), *item.OutBytes)
			}
			if !bytes.Equal(result.Data, expected) {
				t.Fatalf("与 Python 输出不一致：len(go)=%d len(python)=%d，首个差异 %s\n%s",
					len(result.Data), len(expected), firstDifference(result.Data, expected), describeGolden(item))
			}
		})
	}
}

// TestSanitizeGoldenIsByteIdenticalByHash 用摘要固化「逐字节一致」这一结论，
// 便于在提交信息/评审里引用不依赖 diff 的证据。
func TestSanitizeGoldenIsByteIdenticalByHash(t *testing.T) {
	index := loadGoldenIndex(t)
	lines := make([]string, 0, len(index.Cases))
	for _, item := range index.Cases {
		input, err := os.ReadFile(filepath.Join(testdataDir, item.Name+".in.mp4"))
		if err != nil {
			t.Fatalf("%s: 读取输入失败: %v", item.Name, err)
		}
		result, err := media.SanitizeBytes(input, media.SanitizeOptions{})
		if !item.PythonOK {
			lines = append(lines, fmt.Sprintf("%s ok=false err=%v", item.Name, err != nil))
			continue
		}
		if err != nil {
			t.Fatalf("%s: 净化失败: %v", item.Name, err)
		}
		expected, err := os.ReadFile(filepath.Join(testdataDir, item.Name+".out.mp4"))
		if err != nil {
			t.Fatalf("%s: 读取期望输出失败: %v", item.Name, err)
		}
		got, want := sha256.Sum256(result.Data), sha256.Sum256(expected)
		if got != want {
			t.Fatalf("%s: sha256 不一致 go=%s python=%s", item.Name, hex.EncodeToString(got[:8]), hex.EncodeToString(want[:8]))
		}
		lines = append(lines, fmt.Sprintf("%s in=%d out=%d sha256=%s", item.Name, len(input), len(result.Data), hex.EncodeToString(got[:8])))
	}
	t.Logf("Python 权威实现 (%s) 与 Go 转写逐字节一致，共 %d 组：\n%s", index.Source, len(lines), strings.Join(lines, "\n"))
}

// TestSanitizeGoldenCoversAllSkipTypes 防止 fixture 集合被悄悄缩小：
// 9 个 SKIP_IN_STBL 类型必须逐个被真实剔除过，且合成/真实两组都在。
func TestSanitizeGoldenCoversAllSkipTypes(t *testing.T) {
	index := loadGoldenIndex(t)
	wanted := []string{"saio", "saiz", "senc", "sgpd", "sbgp", "stsl", "sdpd", "subs", "stdp"}
	seen := map[string]bool{}
	sawStco, sawCo64, sawReal := false, false, false
	for _, item := range index.Cases {
		if strings.HasPrefix(item.Name, "real_") {
			sawReal = true
		}
		if !item.PythonOK {
			continue
		}
		input, err := os.ReadFile(filepath.Join(testdataDir, item.Name+".in.mp4"))
		if err != nil {
			t.Fatalf("%s: %v", item.Name, err)
		}
		result, err := media.SanitizeBytes(input, media.SanitizeOptions{})
		if err != nil {
			t.Fatalf("%s: %v", item.Name, err)
		}
		for _, name := range result.RemovedStblBoxes {
			seen[name] = true
		}
		if bytes.Contains(bytes.Join([][]byte{input}, nil), []byte("stco")) {
			// 只要输入里有 stco/co64 且 delta 非零，就说明该分支被执行过（有 golden 比对兜底）。
			if result.Delta != 0 && bytes.Contains(input, []byte("stco")) {
				sawStco = true
			}
			if result.Delta != 0 && bytes.Contains(input, []byte("co64")) {
				sawCo64 = true
			}
		}
	}
	for _, name := range wanted {
		if !seen[name] {
			t.Errorf("没有任何 fixture 真正剔除过 %q", name)
		}
	}
	if !sawStco || !sawCo64 {
		t.Errorf("stco/co64 偏移重写未在 delta≠0 下被覆盖（stco=%v co64=%v）", sawStco, sawCo64)
	}
	if !sawReal {
		t.Error("缺少真实 ffmpeg CENC 样本（real_*）")
	}
}

// TestSanitizeRealFixtureFacts 固化 ADR-0004 的实测事实，避免后人「优化」掉净化步骤。
func TestSanitizeRealFixtureFacts(t *testing.T) {
	faststart, err := os.ReadFile(filepath.Join(testdataDir, "real_faststart_remux.in.mp4"))
	if err != nil {
		t.Fatalf("读取真实样本失败: %v", err)
	}
	result, err := media.SanitizeBytes(faststart, media.SanitizeOptions{})
	if err != nil {
		t.Fatalf("净化失败: %v", err)
	}
	// 实测：faststart remux 后 senc/saio/saiz/sinf/encv/tenc 已被 ffmpeg 去掉，
	// 残留的只有 sgpd/sbgp（两处：视频/音频各一，本样本只在音频 trak）。
	if len(result.RemovedStblBoxes) != 2 || result.RemovedStblBoxes[0] != "sgpd" || result.RemovedStblBoxes[1] != "sbgp" {
		t.Fatalf("真实样本剔除集应为 [sgpd sbgp]，实际 %v", result.RemovedStblBoxes)
	}
	if result.Delta != -54 {
		t.Fatalf("真实样本最终 delta 应为 -54（54 = sgpd 26 + sbgp 28），实际 %d", result.Delta)
	}
	if result.Passes != 3 {
		t.Fatalf("真实样本应走满 3 遍（首轮 delta=0 的估算不准触发重算），实际 %d 遍", result.Passes)
	}
	if len(result.Data) != len(faststart)-54 {
		t.Fatalf("输出应比输入少 54 字节，实际 %d → %d", len(faststart), len(result.Data))
	}
	// 净化后不得再出现这两个 box 类型（作为 stbl 直接子 box）。
	for _, name := range []string{"sgpd", "sbgp"} {
		if bytes.Contains(result.Data, []byte(name)) {
			t.Errorf("净化后仍能看到 %s 的字节", name)
		}
	}
	// 但 stsd 内的编码器配置必须保留（avc1/avcC 不能被动）。
	if !bytes.Contains(result.Data, []byte("avc1")) || !bytes.Contains(result.Data, []byte("avcC")) {
		t.Error("stsd 内的 avc1/avcC 被破坏：stsd 必须原样保留")
	}
	// 加密样本（未解密）走同一入口时，剔除集扩大到 senc/saio/saiz（CENC 信令）。
	encrypted, err := os.ReadFile(filepath.Join(testdataDir, "real_cenc_decrypted.in.mp4"))
	if err != nil {
		t.Fatalf("读取加密样本失败: %v", err)
	}
	encResult, err := media.SanitizeBytes(encrypted, media.SanitizeOptions{})
	if err != nil {
		t.Fatalf("加密样本净化失败: %v", err)
	}
	if encResult.Delta <= 0 {
		t.Fatalf("加密样本 mdat 在 moov 之前，delta 应为正，实际 %d", encResult.Delta)
	}
	for _, name := range []string{"senc", "saio", "saiz"} {
		found := false
		for _, removed := range encResult.RemovedStblBoxes {
			if removed == name {
				found = true
			}
		}
		if !found {
			t.Errorf("加密样本未剔除 %s（实际 %v）", name, encResult.RemovedStblBoxes)
		}
	}
}

// TestSanitizeDeltaZeroKeepsPythonQuirk 固化「delta 恰为 0 时 stco 多一层头」这一
// 可观测怪癖：输出与输入等长但不相同，且 go 与 python 一致（由 golden 断言兜底）。
func TestSanitizeDeltaZeroKeepsPythonQuirk(t *testing.T) {
	input, err := os.ReadFile(filepath.Join(testdataDir, "synthetic_delta_zero.in.mp4"))
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	result, err := media.SanitizeBytes(input, media.SanitizeOptions{})
	if err != nil {
		t.Fatalf("净化失败: %v", err)
	}
	if result.Delta != 0 {
		t.Fatalf("该 fixture 的 delta 应恰为 0，实际 %d", result.Delta)
	}
	if result.Passes != 2 {
		t.Fatalf("delta==0 时长度变化相互抵消，不应触发重算，实际 %d 遍", result.Passes)
	}
	if len(result.Data) != len(input) {
		t.Fatalf("delta==0 且只剔除 8 字节 + stco 头 +8，输出长度应与输入相同：%d vs %d",
			len(result.Data), len(input))
	}
	if bytes.Equal(result.Data, input) {
		t.Fatal("输出不应与输入相同（被剔除的 stsl 与 stco 的额外头）")
	}
}

// TestSanitizeDoesNotMutateInput 保证净化器不修改调用方的切片
// （pipeline 会把同一个 buffer 复用于多次尝试）。
func TestSanitizeDoesNotMutateInput(t *testing.T) {
	for _, name := range []string{"synthetic_skip_all", "real_faststart_remux"} {
		input, err := os.ReadFile(filepath.Join(testdataDir, name+".in.mp4"))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		before := sha256.Sum256(input)
		if _, err := media.SanitizeBytes(input, media.SanitizeOptions{}); err != nil {
			t.Fatalf("%s: 净化失败: %v", name, err)
		}
		if after := sha256.Sum256(input); after != before {
			t.Errorf("%s: 输入被修改了", name)
		}
	}
}

// TestSanitizeExtraStblSkip 覆盖 ExtraStblSkip 逃生口：默认不动的 stsd 被显式剔除。
func TestSanitizeExtraStblSkip(t *testing.T) {
	input, err := os.ReadFile(filepath.Join(testdataDir, "synthetic_skip_all.in.mp4"))
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	base, err := media.SanitizeBytes(input, media.SanitizeOptions{})
	if err != nil {
		t.Fatalf("净化失败: %v", err)
	}
	extra, err := media.SanitizeBytes(input, media.SanitizeOptions{ExtraStblSkip: []string{"stsd"}})
	if err != nil {
		t.Fatalf("净化失败: %v", err)
	}
	if len(extra.Data) >= len(base.Data) {
		t.Fatalf("显式剔除 stsd 后应更短：%d vs %d", len(extra.Data), len(base.Data))
	}
	found := false
	for _, name := range extra.RemovedStblBoxes {
		if name == "stsd" {
			found = true
		}
	}
	if !found {
		t.Fatalf("剔除记录里应含 stsd，实际 %v", extra.RemovedStblBoxes)
	}
	if bytes.Contains(extra.Data, []byte("stsd")) {
		t.Error("剔除 stsd 后不应还能看到该类型")
	}
}

// TestSanitizeFileRoundTrip 覆盖文件形态入口与「无 moov 不写输出」。
func TestSanitizeFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.mp4")
	source := filepath.Join(testdataDir, "real_faststart_remux.in.mp4")
	result, err := media.SanitizeFile(source, out, media.SanitizeOptions{})
	if err != nil {
		t.Fatalf("SanitizeFile 失败: %v", err)
	}
	written, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("读取输出失败: %v", err)
	}
	if !bytes.Equal(written, result.Data) {
		t.Fatal("SanitizeFile 落盘内容与返回值不一致")
	}
	expected, err := os.ReadFile(filepath.Join(testdataDir, "real_faststart_remux.out.mp4"))
	if err != nil {
		t.Fatalf("%v", err)
	}
	if !bytes.Equal(written, expected) {
		t.Fatal("SanitizeFile 输出与 Python golden 不一致")
	}

	noMoovOut := filepath.Join(dir, "no-moov.mp4")
	if _, err := media.SanitizeFile(filepath.Join(testdataDir, "synthetic_no_moov.in.mp4"), noMoovOut, media.SanitizeOptions{}); err == nil {
		t.Fatal("无 moov 的输入应当报错")
	}
	if _, err := os.Stat(noMoovOut); !os.IsNotExist(err) {
		t.Fatalf("无 moov 时不应创建输出文件（stat err=%v）", err)
	}
}

// TestSanitizedDecryptedFileStillDecodes 是「净化没有破坏可解码性」的实测证据：
// 解密产物经净化后，ffmpeg 必须零诊断解码，且读到的视频帧数与原样本一致
// （45 帧 = 3 秒 × 15fps）。帧数是 stco 偏移写对了的强证据 —— 偏移若错位，
// 要么解码报错，要么读不满 45 帧。
func TestSanitizedDecryptedFileStillDecodes(t *testing.T) {
	ffmpeg := ffmpegPath(t)
	ffprobe := ffprobePath(t)
	input := sanitizedFixture(t, "real_faststart_remux")
	cmd := exec.Command(ffmpeg, "-v", "error", "-i", input, "-f", "null", "-")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("净化产物解码失败: %v\nstderr: %s", err, stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("净化产物解码有诊断输出（stco 偏移可能被写坏）:\n%s", stderr.String())
	}
	probe := exec.Command(ffprobe, "-v", "error", "-count_frames", "-select_streams", "v:0",
		"-show_entries", "stream=nb_read_frames", "-of", "csv=p=0", input)
	out, err := probe.Output()
	if err != nil {
		t.Fatalf("ffprobe 失败: %v", err)
	}
	if frames := strings.TrimSpace(string(out)); frames != "45" {
		t.Fatalf("应为 45 帧（3s × 15fps），实际 %q", frames)
	}
}

// TestSanitizeDoesNotDecrypt 固化一条容易踩的边界：**净化不是解密**。
//
// server.py 在 ffmpeg 解密失败时会拿加密原文件直接净化兜底（server.py:1104），
// 那样得到的是「CENC 信令被剔掉、mdat 仍是密文」的文件，解码只会吐垃圾诊断。
// 本测试用实测证据固定这一点：加密样本净化后解码**有**诊断输出，
// 而解密样本净化后解码**无**诊断输出。pipeline 因此必须 Decrypt → Sanitize 单向推进，
// 不允许在解密失败时退化成「净化原文件」。
func TestSanitizeDoesNotDecrypt(t *testing.T) {
	ffmpeg := ffmpegPath(t)
	input := sanitizedFixture(t, "real_cenc_decrypted")
	cmd := exec.Command(ffmpeg, "-v", "error", "-i", input, "-f", "null", "-")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	_ = cmd.Run() // 退出码在本样本上可能为 0（垃圾 NAL 仍能被“解”出帧），故只看诊断
	if strings.TrimSpace(stderr.String()) == "" {
		t.Fatal("加密样本净化后竟然解码无诊断：说明该 fixture 已不是密文，" +
			"或净化真的动了 mdat（净化器不得触碰 mdat）")
	}
}

// sanitizedFixture 把某个 fixture 净化后写到临时目录，返回路径。
func sanitizedFixture(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(testdataDir, name+".in.mp4"))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	result, err := media.SanitizeBytes(body, media.SanitizeOptions{})
	if err != nil {
		t.Fatalf("%s: 净化失败: %v", name, err)
	}
	path := filepath.Join(t.TempDir(), name+".san.mp4")
	if err := os.WriteFile(path, result.Data, 0o600); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return path
}

func ffprobePath(t *testing.T) string {
	t.Helper()
	if custom := os.Getenv("HONGGUO_FFPROBE"); custom != "" {
		return custom
	}
	path, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("环境里没有 ffprobe，跳过帧数验证；用 HONGGUO_FFPROBE 指定绝对路径可强制启用")
	}
	return path
}

// ffmpegPath 找 ffmpeg：优先 HONGGUO_FFMPEG，其次 PATH。找不到即跳过。
func ffmpegPath(t *testing.T) string {
	t.Helper()
	if custom := os.Getenv("HONGGUO_FFMPEG"); custom != "" {
		return custom
	}
	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("环境里没有 ffmpeg（PATH 上找不到），跳过解码验证；用 HONGGUO_FFMPEG 指定绝对路径可强制启用")
	}
	return path
}

// firstDifference 报出首个差异偏移与两侧上下文，让失败信息直接可诊断。
func firstDifference(got, want []byte) string {
	limit := len(got)
	if len(want) < limit {
		limit = len(want)
	}
	for index := 0; index < limit; index++ {
		if got[index] != want[index] {
			from, to := index-4, index+8
			if from < 0 {
				from = 0
			}
			gEnd, wEnd := to, to
			if gEnd > len(got) {
				gEnd = len(got)
			}
			if wEnd > len(want) {
				wEnd = len(want)
			}
			return fmt.Sprintf("offset=%d go=% x python=% x", index,
				got[from:gEnd], want[from:wEnd])
		}
	}
	return fmt.Sprintf("前缀相同但长度不同（go=%d python=%d）", len(got), len(want))
}

func describeGolden(item goldenMeta) string {
	return fmt.Sprintf("fixture %s（%s）", item.Name, item.Note)
}
