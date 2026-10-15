//go:build windows

package main

// ignoreSIGPIPE 在 Windows 上是空操作：Windows 没有 SIGPIPE，写已关闭的管道
// 直接返回 ERROR_NO_DATA / ERROR_BROKEN_PIPE，天然就是普通 error。
func ignoreSIGPIPE() {}
