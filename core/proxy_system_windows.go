//go:build windows

package core

// hongguo-next 新增文件（非 vendor 自上游）。
//
// 上游只有 `//go:build !windows` 的空实现（proxy_system_other.go），
// 本仓补上 Windows 实现：读取 WinINET 的每用户代理设置，
// 这是 Windows 上「Internet 选项 → 连接 → 局域网设置」写入的位置，
// Chromium/Electron 与系统其它程序也读它。
//
// 读取失败（键不存在、权限不足、值形态异常）一律视为「无系统代理」，
// 与 unix 版行为一致：宁可不走代理，也不让代理配置读取失败阻断启动。
// 不做 PAC 脚本求值 —— 仅报告 PAC 已启用，由 nativeSystemProxy.resolve
// 给出明确提示（该行为与上游 proxy_system.go 的既有约定一致）。

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

const winINetSettingsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

func platformSystemProxy() nativeSystemProxy {
	key, err := registry.OpenKey(registry.CURRENT_USER, winINetSettingsKey, registry.QUERY_VALUE)
	if err != nil {
		return nativeSystemProxy{}
	}
	defer key.Close()

	proxy := nativeSystemProxy{}
	if enabled, _, err := key.GetIntegerValue("ProxyEnable"); err == nil && enabled == 0 {
		// 手动代理已关闭：仍然如实报告 PAC 状态（若启用），供上层提示。
		if pac, _, err := key.GetStringValue("AutoConfigURL"); err == nil && strings.TrimSpace(pac) != "" {
			proxy.PAC = true
		}
		return proxy
	}
	server, _, err := key.GetStringValue("ProxyServer")
	if err != nil {
		return nativeSystemProxy{}
	}
	proxy.HTTP, proxy.HTTPS = splitWinINetProxy(server)
	if override, _, err := key.GetStringValue("ProxyOverride"); err == nil {
		proxy.Bypass = splitWinINetBypass(override)
	}
	if pac, _, err := key.GetStringValue("AutoConfigURL"); err == nil && strings.TrimSpace(pac) != "" {
		proxy.PAC = true
	}
	return proxy
}

// splitWinINetProxy 解析 ProxyServer 的两种形态：
//
//	"host:port"                        → 协议无关，HTTP/HTTPS 同址
//	"http=h:p;https=h:p;ftp=h:p"       → 按协议分别给出
//
// 协议无关形态下，若未显式声明 https，则 https 复用同一地址
// （这是 WinINET 的实际语义：单值即对所有协议生效）。
// 缺失协议一律留空，交由 resolve 回退到环境变量。
func splitWinINetProxy(server string) (httpAddr, httpsAddr string) {
	server = strings.TrimSpace(server)
	if server == "" {
		return "", ""
	}
	entries := map[string]string{}
	perProtocol := false
	for _, part := range strings.Split(server, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, value, found := strings.Cut(part, "=")
		if !found {
			continue
		}
		perProtocol = true
		entries[strings.ToLower(strings.TrimSpace(name))] = strings.TrimSpace(value)
	}
	if !perProtocol {
		return normalizeProxyAddress(server), normalizeProxyAddress(server)
	}
	httpAddr = normalizeProxyAddress(entries["http"])
	httpsAddr = normalizeProxyAddress(entries["https"])
	return httpAddr, httpsAddr
}

// normalizeProxyAddress 保证地址带 scheme，url.Parse 才不会把 "127.0.0.1:7890"
// 的 "127.0.0.1" 误当成 scheme。
func normalizeProxyAddress(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.Contains(value, "://") {
		return value
	}
	return "http://" + value
}

// splitWinINetBypass 解析 ProxyOverride：以 ";" 分隔，支持 "<local>" 与通配。
func splitWinINetBypass(override string) []string {
	fields := strings.FieldsFunc(override, func(r rune) bool { return r == ';' })
	bypass := make([]string, 0, len(fields))
	for _, field := range fields {
		if field = strings.TrimSpace(field); field != "" {
			bypass = append(bypass, field)
		}
	}
	return bypass
}
