import {
  bufferFeature,
  controlsFeature,
  createPlayer,
  errorFeature,
  fullscreenFeature,
  metadataFeature,
  playbackFeature,
  playbackRateFeature,
  pipFeature,
  qualityFeature,
  sourceFeature,
  timeFeature,
  volumeFeature,
} from '@videojs/react';

/**
 * 播放器工厂 —— **本仓 PC 端播放器的唯一入口**。
 *
 * 为什么是 `createPlayer({ features })`（组合式）而不是 v8 的命令式全局实例：
 * npm 包 `video.js` 在 v10 已是**墓碑包**——v8 的初始化函数、插件注册、组件注册
 * 三件套全部改成 `throwLegacyError('VJS8_LEGACY_*')`（结论见
 * docs/ARCHITECTURE.md §0 第 7 条、docs/DESIGN-SYSTEM.md §1.3）。
 * 本文件被 scripts/guard-videojs8.mjs 守着，写回 v8 写法会直接让 `lint:player` 失败；
 * 连注释里都不写出那几个函数名，是为了让门禁保持「grep 命中即失败」的严格形态。
 *
 * features 的选择（不是照抄 `videoFeatures` 预设，逐条给出理由）：
 * - playback / time / volume / buffer   —— 控制条与弹幕层的时间源，必需；
 * - fullscreen                          —— 走 FullscreenButton，主进程再把它转成
 *                                          Electron 原生全屏（见 main/index.ts
 *                                          bindNativeFullscreen，规避「全屏丢失控制条」）；
 * - pip                                 —— Electron/Chromium 支持；
 * - quality                             —— 只用于**读取** hls.js 的 renditions 做展示；
 *                                          真正的切档是回内核重新 resolve（源侧画质由内核
 *                                          决定，本地切档只是同一 manifest 内的 ABR 选择）；
 * - error                               —— 播放失败要能呈现，不能黑屏；
 * - metadata                            —— 标题 / 海报由我们注入（title/poster props）；
 * - source / controls / playbackRate    —— 源的挂载、控制条自动隐藏、倍速。
 *
 * **刻意不含** `liveFeature`/`streamTypeFeature`：短剧是按需点播，把播放器标成 live
 * 会让时间轴语义错位（直播没有 duration）。
 */
export const {
  Player: HnPlayer,
  usePlayer: useHnPlayer,
  useMedia: useHnMedia,
} = createPlayer({
  displayName: 'HnPlayer',
  features: [
    playbackFeature,
    timeFeature,
    volumeFeature,
    bufferFeature,
    fullscreenFeature,
    pipFeature,
    qualityFeature,
    errorFeature,
    metadataFeature,
    sourceFeature,
    controlsFeature,
    playbackRateFeature,
  ],
});

/** 播放器 store 状态字段的集中出口（组件里只读这几个，避免各页各读一套）。 */
export interface HnPlayerSnapshot {
  paused: boolean;
  waiting: boolean;
  currentTime: number;
  duration: number;
  buffered: [number, number][];
}
