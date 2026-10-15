//go:build !windows

package main

import (
	"os/signal"
	"syscall"
)

// ignoreSIGPIPE 让「stdout 读端已关闭」表现为 EPIPE 写错误而不是进程被
// SIGPIPE 打死。
//
// 为什么要专门处理：Go 运行时对 fd 1/2 上的 EPIPE 有特殊约定 —— 默认直接以
// SIGPIPE 结束进程（与 shell 管道行为一致）。对 stdio 服务端来说这是个坑：
// 调用方（Electron 主进程）先关掉读端或整个进程退出后，服务端应当在
// 「写失败 → 停止读 → 宽限收尾 → 退出」这条路径上走完，而不是被信号打断，
// 否则每次父进程异常都要靠退出码去猜发生了什么。
//
// 忽略 SIGPIPE 之后，写 stdout 变成普通 error，交给 run/respond 处理。
func ignoreSIGPIPE() {
	signal.Ignore(syscall.SIGPIPE)
}
