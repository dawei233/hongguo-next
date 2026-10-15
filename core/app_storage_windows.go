//go:build windows

package core

// hongguo-next 新增文件（非 vendor 自上游）。
//
// 上游 guoapp-android 的 Go 内核只有 `//go:build !windows` 一份实现
// （见 app_storage_unix.go），即内核**只能编到 Android/Linux**；
// 其 Windows 端是 Flutter 桌面应用，走 Dart 侧媒体栈，不经这个内核。
//
// 本仓的目标架构要求同一个内核交叉编译出 Windows 可执行文件供
// PC 端（apps/desktop 经 cmd/hncore-server）调用，因此在 M1 vendor
// 时补齐 Windows 实现。语义与 unix 版一致：取「可用空间」，
// 失败返回 -1（调用方 app_storage.go 会据此降级）。

import "golang.org/x/sys/windows"

func nativeFreeSpace(path string) int64 {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return -1
	}
	var availableToCaller, totalBytes, totalFreeBytes uint64
	if err := windows.GetDiskFreeSpaceEx(name, &availableToCaller, &totalBytes, &totalFreeBytes); err != nil {
		return -1
	}
	return int64(availableToCaller)
}
