// Command hnbridge 导出 Android Flutter 端使用的 FFI 符号。
//
// 来源：vendor 自 dawei233/guoapp-android 的 native/bridge/main.go，
// 仅改动一处 —— import 路径由 duanjuapp/native/core 改为本仓 module。
// 符号形状保持不变（仍是 2 个 C 符号，JSON 进 / JSON 出），
// 以便 apps/android 的 lib/core_bridge.dart 无需改动 ABI。
//
// 平台说明：guoapp 的 Dart 绑定当前只支持 Android
// （core_bridge.dart 对非 Android 抛 UnsupportedError）。
// 本文件本身不限定平台，Windows/macOS 亦可编译为 .dll/.dylib，
// 但 PC 端走的是 cmd/hncore-server 的 stdio JSON-RPC，不经这里。
package main

/*
#include <stdlib.h>
*/
import "C"

import (
	"unsafe"

	"github.com/dawei233/hongguo-next/core"
)

//export DuanjuRequest
func DuanjuRequest(input *C.char) *C.char {
	if input == nil {
		return C.CString(`{"ok":false,"error":"请求为空"}`)
	}
	return C.CString(core.NativeRequest(C.GoString(input)))
}

//export DuanjuFree
func DuanjuFree(value *C.char) { C.free(unsafe.Pointer(value)) }

func main() {}
