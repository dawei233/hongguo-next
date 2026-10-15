// 本仓新增（M3 媒体管线桥）。
//
// # 为什么需要这个文件
//
// vendor 进来的 core 是**扁平单包**且绝大多数标识符未导出（全仓只有 1 个导出函数
// NativeRequest）。M3 的 core/media 是独立子包，取不到 core 的 resolveProviderMedia /
// nativeResolve 等内部能力。M1 的先例是「走 JSON」（见 core/source/adapter/series.go 的
// SeriesFromNativeDramaValue）：core 内部把值序列化成线格式，子包再解码成契约类型。
// 本文件按同一先例，只新增**导出标识符**，不改动 core 的任何既有文件、导出符号与行为
// （bridge 的 2 个 C 符号 ABI 不变）。
//
// # 出口面（每个都是「入参 JSON → 出参 JSON」）
//
//	MediaBridgeInfoJSON()                 → {"version","ready","allSources"}
//	InitializeMediaBridge(directory)      → 初始化引擎（等价 NativeRequest 的 initialize）
//	EngineReady()                         → 引擎是否就绪
//	ProviderMediaJSON(nativeInputJSON)    → 上游媒体（providerMedia）的线格式
//	PlaybackPlanJSON(nativeInputJSON)     → 播放计划（nativePlan）的线格式
//
// ProviderMediaJSON 是 core/media 的入口：它的输出与 core/source/adapter.ProviderMedia
// 逐字段一致，把它交给 adapter.StreamFromProviderMediaValue 即得契约 Stream
// （URL / 是否加密 / 密钥 / 请求头 / 封装 / 是否需净化），再喂给 media.Pipeline。
// PlaybackPlanJSON 是「交给 core 自己的本机代理播放」那条路的入口：输出与
// adapter.NativePlan 逐字段一致（含过期时刻、线路序号）。
//
// 两个入口都要求先 InitializeMediaBridge（或调用过 NativeRequest 的 initialize），
// 否则返回 ErrMediaBridgeNotReady —— 与 core 自身在未初始化时的行为一致
// （app_runtime.go:361「应用核心尚未就绪，请重新打开应用」）。
package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"
)

// MediaBridgeVersion 是媒体桥的接口版本。新增/改变出口字段时必须递增，
// 便于 bridge/ts 与调用方定位不兼容。
const MediaBridgeVersion = 1

// ErrMediaBridgeNotReady 表示 core 引擎尚未初始化。
var ErrMediaBridgeNotReady = errors.New("应用核心尚未就绪：请先调用 InitializeMediaBridge 或 NativeRequest 的 initialize")

// mediaBridgeInputLimit 与 NativeRequest 的入参上限一致（1MB）。
const mediaBridgeInputLimit = 1 << 20

// InitializeMediaBridge 初始化 core 引擎（幂等）。
//
// directory 必须是绝对路径（与 core 自身的校验一致，见 newNativeEngine）。
func InitializeMediaBridge(directory string) error {
	defer bridgeRecover()
	if !filepath.IsAbs(directory) {
		return errors.New("应用数据目录无效")
	}
	nativeState.Lock()
	defer nativeState.Unlock()
	if nativeState.engine != nil {
		return nil
	}
	engine, err := newNativeEngine(directory)
	if err != nil {
		return err
	}
	nativeState.engine = engine
	return nil
}

// EngineReady 报告 core 引擎是否已初始化。
func EngineReady() bool {
	nativeState.Lock()
	defer nativeState.Unlock()
	return nativeState.engine != nil
}

// MediaBridgeInfoJSON 返回桥的自述信息（版本 / 就绪状态 / 是否全量开放站源）。
//
// allSources 恒为 true：M1 已按 §3.2 #15 删除 buildAllSources 门控（§4.5）。
func MediaBridgeInfoJSON() (result string, err error) {
	defer bridgeRecover(&err)
	ready := EngineReady()
	payload := map[string]any{"version": MediaBridgeVersion, "ready": ready, "allSources": true}
	encoded, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return "", marshalErr
	}
	return string(encoded), nil
}

// ProviderMediaJSON 把「一集」解析为**上游媒体**的线格式（core.providerMedia）。
//
// 入参与 NativeRequest 同形（core 的 nativeInput）：至少给出
// `{"drama":{"id":"<source>:<id>","title":"…"},"chapter":{…},"index":1}`；
// 另可给 `"directory":"/绝对路径"` —— 引擎未初始化时用它就地初始化。
//
// 为什么需要 chapter：core 的部分源把取流标记放在 chapter.VideoURL 里
// （如 `hongguo-cenc://<vid>`，见 provider_media.go:145），缺了它就解析不出真实地址。
// 调用方应把 detail 接口返回的 chapters[i] 原样透传。
//
// action 由本函数固定为 resolve，调用方不必提供（提供了也会被覆盖）。
func ProviderMediaJSON(requestJSON string) (result string, err error) {
	defer bridgeRecover(&err)
	input, engine, err := bridgePrepare(requestJSON)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	task := Task{DramaID: input.Drama.ID, DramaTitle: input.Drama.Title,
		Chapter: input.Chapter, Index: input.Index}
	media, err := engine.downloader.resolveProviderMedia(ctx, task)
	if err != nil {
		return "", publicError(err)
	}
	encoded, err := json.Marshal(media)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// PlaybackPlanJSON 把「一集」解析为**播放计划**的线格式（core.nativePlan）。
//
// 与 ProviderMediaJSON 的差别：这条走 core 自己的播放会话（含本机流代理地址、
// 过期时刻、线路序号），是「把计划交给 core 的代理去播」那条路；
// ProviderMediaJSON 给的是上游原始地址，是「由 core/media 自己取流/解密/净化」那条路。
func PlaybackPlanJSON(requestJSON string) (result string, err error) {
	defer bridgeRecover(&err)
	input, engine, err := bridgePrepare(requestJSON)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	input.Action = "resolve"
	plan, err := engine.nativeResolve(ctx, input)
	if err != nil {
		return "", publicError(err)
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// bridgePrepare 解析入参、必要时就地初始化引擎，并校验剧集编号。
func bridgePrepare(requestJSON string) (nativeInput, *nativeEngine, error) {
	var input nativeInput
	if len(requestJSON) == 0 || len(requestJSON) > mediaBridgeInputLimit {
		return input, nil, errors.New("请求格式无效")
	}
	if err := json.Unmarshal([]byte(requestJSON), &input); err != nil {
		return input, nil, errors.New("请求格式无效")
	}
	if _, _, valid := splitProviderDramaID(input.Drama.ID); !valid {
		return input, nil, errors.New("剧集信息无效")
	}
	nativeState.Lock()
	engine := nativeState.engine
	nativeState.Unlock()
	if engine == nil {
		if input.Directory == "" {
			return input, nil, ErrMediaBridgeNotReady
		}
		if err := InitializeMediaBridge(input.Directory); err != nil {
			return input, nil, err
		}
		nativeState.Lock()
		engine = nativeState.engine
		nativeState.Unlock()
		if engine == nil {
			return input, nil, ErrMediaBridgeNotReady
		}
	}
	return input, engine, nil
}

// bridgeRecover 把 panic 收敛为错误（与 NativeRequest 的 recover 同一考虑：
// 桥是从 FFI/子进程被调用的，panic 会直接终结调用方进程）。
//
// 形态说明：无参调用用于「不需要改返回值」的入口（InitializeMediaBridge），
// 带 *error 的调用用于「需要把 panic 变成错误返回」的入口。
func bridgeRecover(errs ...*error) {
	if recovered := recover(); recovered != nil {
		if len(errs) > 0 && errs[0] != nil {
			*errs[0] = fmt.Errorf("本地核心处理失败，请重试（panic: %v）", recovered)
		}
	}
}
