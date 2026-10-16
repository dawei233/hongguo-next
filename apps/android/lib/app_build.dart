// hongguo-next 的应用身份与构建常量。
//
// 与 vendored 上游（guoapp-android）的三处有意差异：
//
// 1. `allSourcesEnabled` 恒为 true。上游用 `bool.fromEnvironment('ALL_SOURCES')`
//    在编译期分「红果鉴（单源）/ 真果鉴（多源）」两个版本，Go 内核侧还有
//    `buildAllSources` 门控与之配对。hongguo-next 删除了内核侧门控
//    （core/app_build.go，全部 11 个站源默认可用），本仓因此也**删掉 Dart 侧的
//    编译期分版**——两版合一，否则 `core_bridge.initialize()` 里
//    `build['allSources'] != allSourcesEnabled` 会因两边口径不同而
//    秒抛「应用与原生核心的站源版本不一致」，应用打不开（2026-10-16 真机实测）。
//
// 2/3. 应用名与 slug 不再用「红果鉴 / 真果鉴」——那是上游的两个版本名。
//      本仓是**短剧聚合应用（以红果为主）**，统一为「红果 Next」。
const allSourcesEnabled = true;
const appName = '红果 Next';
const appSlug = 'hongguo-next';
