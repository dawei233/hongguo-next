package core

// 本文件源自 guoapp-android 的 app_build_test.go。
//
// hongguo-next 的有意改动：上游用 `buildAllSources` 门控在「仅 hongguo 源」与
// 「全源」两种版本间切换，测试里因此遍布 `if buildAllSources == "true"` 分支。
// 本仓已删除该门控（见 app_build.go），全部已知站源默认可用，故这里的断言
// 一律按上游的「全源」语义固定下来，不再有版本分支。

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestNativeBuildAuthorization 校验跨源鉴权：
// 已注册站源一律放行，未知站源一律拒绝；同一请求内剧集来源与声明来源
// 不一致、或章节来源与剧集来源不一致时必须拒绝。
func TestNativeBuildAuthorization(t *testing.T) {
	for _, source := range []string{sourceHongguo, sourceHuangdou, sourceHuangguoVideo, sourceHuangguoAI, sourceCloudFront, sourceDSD, "unknown"} {
		// 无门控：除未知站源外全部可用。
		allowed := source != "unknown"
		for _, action := range []string{"catalog", "cached", "sourceStatus", "sourceJob", "cancelSourceJob", "detail", "cover", "resolve", "enqueueDownloads", "localPlayback"} {
			input := nativeInput{Action: action, Source: source, Drama: nativeDrama{ID: source + ":123", Source: source}}
			err := nativeAuthorizeInput(input)
			if (err == nil) != allowed {
				t.Fatalf("%s %s: allowed=%v, error=%v", action, source, allowed, err)
			}
		}
	}
	// 来源不匹配的三类请求都必须被拒。
	for _, input := range []nativeInput{
		{Action: "resolve", Drama: nativeDrama{ID: "huangdou:123", Source: sourceHongguo}},
		{Action: "resolve", Drama: nativeDrama{ID: "hongguo:123", Source: sourceHongguo}, Chapter: Chapter{Source: sourceHuangdou}},
		{Action: "resolve", Drama: nativeDrama{ID: "hongguo:123", Source: sourceHongguo}, Chapter: Chapter{ID: "huangdou:123:1"}},
	} {
		if !errors.Is(nativeAuthorizeInput(input), errNativeBuildSource) {
			t.Fatal("mismatched source passed native authorization")
		}
	}
	manager := downloadTestManager(t)
	input := downloadTestInput(1)
	input.Entries[0].Chapter.Source = sourceHuangdou
	if _, err := manager.enqueue(input); !errors.Is(err, errNativeBuildSource) {
		t.Fatalf("mismatched download source accepted: %v", err)
	}
}

// TestNativeBuildPreservesForeignDownloadRecordsAndSchedulesAllSources 校验：
// 已保存的各站源下载记录在重开后全部可见、全部可被调度，且批量操作
// （pauseAll / close）不会丢弃任何一条记录。
//
// 上游此测试名为 ...RestrictsScheduling，因为在默认版下只有 hongguo 源可调度、
// 其余记录被隐藏。本仓全源开放，故断言方向一致地改为「全部可见且可调度」。
func TestNativeBuildPreservesForeignDownloadRecordsAndSchedulesAllSources(t *testing.T) {
	manager := downloadTestManager(t)
	records := []*nativeDownloadRecord{}
	for index, source := range []string{sourceHongguo, sourceHuangdou, sourceHuangguoVideo, sourceHuangguoAI, sourceCloudFront, sourceDSD} {
		drama := nativeDrama{ID: source + ":123", Source: source, Title: "合成下载"}
		record := &nativeDownloadRecord{nativeDownloadJob: nativeDownloadJob{
			ID: nativeDownloadID(drama.ID, 1), Drama: drama, Index: 1,
			Chapter: Chapter{ID: "1", Source: source, CurrentEpisode: json.RawMessage("1")}, State: "queued", Created: int64(index),
		}}
		records = append(records, record)
	}
	data, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(manager.root, "index.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	reopened := newNativeDownloads(manager.engine)
	t.Cleanup(reopened.close)
	jobs, err := reopened.snapshot()
	// 全源开放：6 条记录全部可见。
	if err != nil || len(jobs) != 6 {
		t.Fatalf("wrong visible downloads: %v, %v", jobs, err)
	}
	if len(reopened.jobs) != len(records) {
		t.Fatal("changing edition discarded saved downloads")
	}
	started := make(chan string, 6)
	reopened.resolve = func(ctx context.Context, job nativeDownloadJob) (providerMedia, error) {
		started <- job.Drama.Source
		<-ctx.Done()
		return providerMedia{}, ctx.Err()
	}
	if err := reopened.control("", "resumeAll"); err != nil {
		t.Fatal(err)
	}
	// 2 个 worker，按 Created 顺序取前两条：hongguo、huangdou。
	seen := map[string]bool{}
	for index := 0; index < 2; index++ {
		select {
		case source := <-started:
			seen[source] = true
		case <-time.After(5 * time.Second):
			t.Fatal("allowed download did not start")
		}
	}
	if !seen[sourceHongguo] || !seen[sourceHuangdou] {
		t.Fatalf("wrong sources scheduled: %v", seen)
	}
	// 两条 worker 都在跑，不应有更多任务被并发拉起。
	reopened.mu.Lock()
	active := len(reopened.active)
	reopened.mu.Unlock()
	if active != 2 {
		t.Fatalf("worker count mismatch: %d", active)
	}
	if err := reopened.control("", "pauseAll"); err != nil {
		t.Fatal(err)
	}
	reopened.close()
	// 批量操作后所有记录仍在内存与磁盘上。
	for _, original := range records {
		if reopened.jobs[original.ID] == nil {
			t.Fatalf("download %s was removed during bulk operations", original.ID)
		}
	}
	stored, err := os.ReadFile(filepath.Join(reopened.root, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved []nativeDownloadRecord
	if err := json.Unmarshal(stored, &saved); err != nil || len(saved) != 6 {
		t.Fatalf("saved downloads lost: %v", err)
	}
	if job := reopened.jobs[nativeDownloadID(sourceDSD+":123", 1)]; job == nil {
		t.Fatal("DSD download was lost during bulk operations")
	}
}
