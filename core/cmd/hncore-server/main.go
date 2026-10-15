// Command hncore-server 是 hongguo-next 内核在 PC 端的 stdio JSON 服务端。
//
// 定位（ARCHITECTURE.md §2.2 / §2.3 / §2.4）：PC 端（apps/desktop 的 Electron
// 主进程）**不**链接内核，而是把它作为子进程拉起，用「stdin/stdout 行分隔
// JSON」通信；安卓端继续走 bridge/go 的 2 个 C 符号。两条路径的语义面相同：
// 请求是内核的 nativeInput（JSON 进），响应是内核的 {ok,data} /
// {ok,error,code,retryAt} 信封（JSON 出）。
//
// 本程序**不含任何业务逻辑**：唯一的业务入口是 core.NativeRequest(string)
// string，服务端只负责帧定界、并发、超时/取消、panic 隔离与优雅退出。
//
// 用法:
//
//	hncore-server [-max-bytes N] [-workers N] [-max-inflight N] [-timeout D] [-grace D] [-quiet]
//
// 线协议与逐条取舍见同目录 README.md。
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	os.Exit(entry(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// entry 是 main 的可测形态：不直接调 os.Exit，也不直接摸 os.Stdin/Stdout。
func entry(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	cfg, showVersion, err := parseFlags(args, stderr)
	if err != nil {
		return 2
	}
	logger := log.New(stderr, "hncore-server ", log.LstdFlags|log.Lmicroseconds)
	if cfg.Quiet {
		logger = log.New(io.Discard, "", 0)
	}
	cfg.Logger = logger
	if showVersion {
		fmt.Fprintf(stdout, "hncore-server %s (协议 v%d)\n", serverVersion, protocolVersion)
		return 0
	}

	// 在 Unix 上忽略 SIGPIPE：调用方（Electron）先关掉读端时，写 stdout 会
	// 变成 EPIPE 错误而不是让进程被信号打死，从而能走完优雅退出路径。
	// 这是 Go 对 fd 1/2 的默认行为的一个必要修正（见 sigpipe_*.go）。
	ignoreSIGPIPE()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		received := <-signals
		logger.Printf("收到信号 %v，转入优雅退出", received)
		cancel()
		// 读循环可能正阻塞在 stdin 上；关掉它来解除阻塞（run 会把因此产生的
		// 读错误视为正常收尾）。这也是唯一可靠的跨平台解阻塞手段。
		if closer, ok := stdin.(io.Closer); ok {
			_ = closer.Close()
		}
	}()

	if err := run(ctx, stdin, stdout, cfg); err != nil {
		logger.Printf("异常退出：%v", err)
		return 1
	}
	return 0
}

func parseFlags(args []string, stderr io.Writer) (config, bool, error) {
	flags := flag.NewFlagSet("hncore-server", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var cfg config
	showVersion := flags.Bool("version", false, "打印版本后退出")
	flags.IntVar(&cfg.MaxFrameBytes, "max-bytes", defaultMaxFrameBytes,
		"单帧上限（字节）；默认与内核自身的 1 MiB 请求上限对齐")
	flags.IntVar(&cfg.Workers, "workers", defaultWorkers,
		"同时对内核发起的调用数上限；1 = 串行且响应顺序与请求顺序一致")
	flags.IntVar(&cfg.MaxInflight, "max-inflight", 0,
		"在途请求上限（0 = 按 max(32, workers*8) 推导）")
	timeout := flags.Duration("timeout", 0,
		"单请求默认等待上限（0 = 不设服务端超时，用内核自身的期限）")
	flags.DurationVar(&cfg.Grace, "grace", defaultGrace,
		"收到退出请求后等待在途请求收尾的宽限期")
	flags.BoolVar(&cfg.Quiet, "quiet", false, "不输出诊断日志（stderr 静默）")
	flags.Usage = func() {
		fmt.Fprint(stderr, "hncore-server —— hongguo-next 内核的 stdio JSON 服务端\n\n"+
			"stdin/stdout 走行分隔 JSON（一帧一行，UTF-8，容忍 CRLF）；\n"+
			"stdout 只用于协议帧，诊断日志一律走 stderr。\n\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return cfg, false, err
		}
		return cfg, false, err
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(stderr, "不接受位置参数：%v\n", flags.Args())
		return cfg, false, fmt.Errorf("unexpected args")
	}
	if cfg.MaxFrameBytes <= 0 {
		fmt.Fprintf(stderr, "-max-bytes 必须为正数\n")
		return cfg, false, fmt.Errorf("invalid max-bytes")
	}
	if cfg.Workers <= 0 {
		fmt.Fprintf(stderr, "-workers 必须为正数\n")
		return cfg, false, fmt.Errorf("invalid workers")
	}
	if cfg.MaxInflight < 0 {
		fmt.Fprintf(stderr, "-max-inflight 不能为负\n")
		return cfg, false, fmt.Errorf("invalid max-inflight")
	}
	if *timeout < 0 {
		fmt.Fprintf(stderr, "-timeout 不能为负\n")
		return cfg, false, fmt.Errorf("invalid timeout")
	}
	if cfg.Grace < 0 {
		fmt.Fprintf(stderr, "-grace 不能为负\n")
		return cfg, false, fmt.Errorf("invalid grace")
	}
	cfg.RequestTimeout = *timeout
	return cfg, *showVersion, nil
}
