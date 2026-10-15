package main

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"unicode/utf8"
)

var (
	// errFrameTooLarge / errFrameBadUTF8 / errEmptyFrame 都是**可恢复**错误：
	// 出错的那一帧已经被完整消费掉（读到行尾），流仍然同步，读循环可以继续。
	errFrameTooLarge = errors.New("帧超出上限")
	errFrameBadUTF8  = errors.New("帧不是合法 UTF-8")
	errFrameEmpty    = errors.New("空帧")
)

// readFrame 从 r 读出一帧（行分隔，见 protocol.go 的选型说明）。
//
// 返回的 payload 已剥离行尾 \n/\r、行首 BOM 与首尾空白。
// 返回值语义：
//   - (payload, nil)                    正常帧；payload 非空
//   - (nil, errFrame* 三个可恢复错误)    该行非法，但流已同步到下一行
//   - (nil, io.EOF)                     流干净结束
//   - (nil, 其他 error)                 底层读错误，不可恢复
//
// 内存上界：单帧最多在内存里保留 max+1 字节；超限后不再累积，只把剩余字节
// 丢到行尾（丢弃过程本身不再分配），因此恶意超长行不会把服务端撑爆。
func readFrame(r *bufio.Reader, max int) ([]byte, error) {
	if max <= 0 {
		max = defaultMaxFrameBytes
	}
	var frame []byte
	oversize := false
	for {
		chunk, err := r.ReadSlice('\n')
		if !oversize {
			// +1 是给行尾的 '\n' 留位：恰好 max 字节的载荷仍算合规。
			if len(frame)+len(chunk) > max+1 {
				oversize, frame = true, nil
			} else {
				frame = append(frame, chunk...)
			}
		}
		switch {
		case err == nil:
			// 读到 '\n'，本行结束。
			if oversize {
				return nil, errFrameTooLarge
			}
			return normalizeFrame(frame)
		case errors.Is(err, bufio.ErrBufferFull):
			// 只是读缓冲满了，继续拼。
			continue
		case errors.Is(err, io.EOF):
			// 最后一帧允许没有结尾换行；若确实什么都没有，就是流结束。
			if oversize {
				return nil, errFrameTooLarge
			}
			if len(frame) == 0 {
				return nil, io.EOF
			}
			return normalizeFrame(frame)
		default:
			return nil, err
		}
	}
}

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// normalizeFrame 剥掉 CRLF、UTF-8 BOM 与首尾空白，并校验 UTF-8。
//
// CRLF 容忍：Windows 侧（PC 首发平台）的调用方有可能按文本模式写管道，
// 行尾会多一个 \r，这里统一剥除。
// BOM 容忍：某些 Windows 编辑器/脚本会写出带 BOM 的第一行。
func normalizeFrame(frame []byte) ([]byte, error) {
	frame = bytes.TrimLeft(frame, "\r\n")
	frame = bytes.TrimPrefix(frame, utf8BOM)
	frame = bytes.TrimRight(frame, "\r\n")
	frame = bytes.TrimSpace(frame)
	if len(frame) == 0 {
		return nil, errFrameEmpty
	}
	if !utf8.Valid(frame) {
		return nil, errFrameBadUTF8
	}
	return frame, nil
}
