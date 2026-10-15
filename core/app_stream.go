package core

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type nativeStreamAsset struct {
	address     string
	data        []byte
	contentType string
	total       int64
	etag        string
	modified    string
}

type nativeStreamSession struct {
	credentials *providerMediaCredentials
	mu          sync.Mutex
	assets      map[string]nativeStreamAsset
	referer     string
	key         []byte
	ctx         context.Context
	cancel      context.CancelFunc
	lastUsed    time.Time
}

type nativeStreamServer struct {
	mu         sync.Mutex
	downloader *Downloader
	address    string
	sessions   map[string]*nativeStreamSession
	server     *http.Server
}

func (stream *nativeStreamServer) nativeRequest(request *http.Request) (*http.Response, error) {
	client := *stream.downloader.client
	client.Timeout = 0
	return stream.downloader.doMediaRequestWithClient(request, &client)
}

var nativePlaylistURI = regexp.MustCompile(`URI="([^"]+)"`)

func newNativeStreamServer(d *Downloader) (*nativeStreamServer, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, errors.New("无法初始化本机播放器")
	}
	stream := &nativeStreamServer{downloader: d, address: "http://" + listener.Addr().String(), sessions: map[string]*nativeStreamSession{}}
	server := &http.Server{Handler: http.HandlerFunc(stream.nativeServe), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384}
	stream.server = server
	go func() { _ = server.Serve(listener) }()
	return stream, nil
}

func (stream *nativeStreamServer) nativeOpen(media providerMedia) (string, string) {
	tokenBytes := make([]byte, 24)
	if _, err := rand.Read(tokenBytes); err != nil {
		panic(err)
	}
	token := hex.EncodeToString(tokenBytes)
	ctx, cancel := context.WithCancel(providerMediaContext(context.Background(), media.credentials))
	session := &nativeStreamSession{assets: map[string]nativeStreamAsset{}, referer: media.Referer, key: media.HLSKey, ctx: ctx, cancel: cancel, lastUsed: time.Now(), credentials: media.credentials}
	stream.mu.Lock()
	for id, old := range stream.sessions {
		if time.Since(old.lastUsed) > 10*time.Minute {
			old.cancel()
			delete(stream.sessions, id)
		}
	}
	if len(stream.sessions) >= 8 {
		oldest := ""
		for id, old := range stream.sessions {
			if oldest == "" || old.lastUsed.Before(stream.sessions[oldest].lastUsed) {
				oldest = id
			}
		}
		stream.sessions[oldest].cancel()
		delete(stream.sessions, oldest)
	}
	stream.sessions[token] = session
	stream.mu.Unlock()
	entry := nativeStreamAsset{address: media.URL, contentType: "video/mp4"}
	// [红果漫剧补丁] 严格判定 HLS：只看 path 后缀 / 清单内容 / HLS 密钥。
	// 旧版用 strings.Contains(URL, "hls") 宽松匹配 —— 实测红果 CDN 的 MP4 直链
	// URL 第 415 位恰好含 "hls" 子串（base64 参数巧合）→ MP4 被误标 mpegurl →
	// 假 .m3u8 尾 → mpv 用 HLS demuxer 解 MP4 → seek 全失败 → 播放器跳集。
	pathIsM3u8 := false
	if parsed, err := url.Parse(media.URL); err == nil {
		// [红果漫剧补丁] 只看 path 且【排除媒体扩展名】：
		// /hls/ 目录名 + .ts 分片 ≠ 清单（剧果分片就是 /hls/10362/480/seg-014.ts）。
		pathLower := strings.ToLower(parsed.Path)
		hasMediaExt := strings.HasSuffix(pathLower, ".ts") || strings.HasSuffix(pathLower, ".mp4") ||
			strings.HasSuffix(pathLower, ".m4s") || strings.HasSuffix(pathLower, ".aac") ||
			strings.HasSuffix(pathLower, ".m4a") || strings.HasSuffix(pathLower, ".mp3")
		pathIsM3u8 = !hasMediaExt &&
			(strings.Contains(pathLower, "m3u8") || strings.Contains(pathLower, "hls"))
	}
	isHLS := media.Playlist != "" || len(media.HLSKey) > 0 || pathIsM3u8
	if isHLS {
		entry.contentType = "application/vnd.apple.mpegurl"
	}
	if media.Playlist != "" {
		entry.data = []byte(media.Playlist)
		entry.contentType = "application/vnd.apple.mpegurl"
	}
	registered := stream.nativeAsset(token, session, entry)
	stream.downloader.recordDiagnostic(diagnosticEvent{Event: "stream.open", Level: "info", Message: fmt.Sprintf("token=%s… url尾=%s playlistLen=%d hlsKey=%d isHLS=%v contentType=%s dataLen=%d", token[:12], registered[len(registered)-14:], len(media.Playlist), len(media.HLSKey), isHLS, entry.contentType, len(entry.data))})
	return registered, token
}

func (stream *nativeStreamServer) nativeRelease(token string) {
	stream.mu.Lock()
	if session := stream.sessions[token]; session != nil {
		session.cancel()
		delete(stream.sessions, token)
	}
	stream.mu.Unlock()
}

func (stream *nativeStreamServer) nativeAsset(token string, session *nativeStreamSession, asset nativeStreamAsset) string {
	digest := sha256.Sum256([]byte(asset.address + "\x00" + asset.contentType))
	extension := ".ts"
	if parsed, err := url.Parse(asset.address); err == nil {
		switch candidate := strings.ToLower(path.Ext(parsed.Path)); candidate {
		case ".m3u8", ".ts", ".m4s", ".mp4", ".aac", ".m4a", ".mp3", ".vtt", ".webvtt", ".key":
			extension = candidate
		}
	}
	switch asset.contentType {
	case "application/vnd.apple.mpegurl":
		extension = ".m3u8"
	case "application/octet-stream":
		extension = ".key"
	case "video/mp4":
		extension = ".mp4"
	}
	id := hex.EncodeToString(digest[:12]) + extension
	session.mu.Lock()
	if previous, found := session.assets[id]; !found || len(previous.data) == 0 || len(asset.data) > 0 {
		session.assets[id] = asset
	}
	session.mu.Unlock()
	return stream.address + "/" + token + "/" + id
}

func (stream *nativeStreamServer) nativeRewrite(token string, session *nativeStreamSession, body, base string) (string, error) {
	var output []string
	nextPlaylist := false
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		text := strings.TrimSpace(line)
		rewrite := func(reference string, key bool, contentType string) string {
			if strings.HasPrefix(reference, "data:") {
				return reference
			}
			parsed, err := url.Parse(base)
			if err != nil {
				return ""
			}
			relative, err := url.Parse(reference)
			if err != nil {
				return ""
			}
			address := parsed.ResolveReference(relative).String()
			if !isProviderHTTPMediaURL(address) {
				return ""
			}
			asset := nativeStreamAsset{address: address, contentType: contentType}
			if key && len(session.key) == 16 {
				asset.data = append([]byte{}, session.key...)
				asset.contentType = "application/octet-stream"
			}
			return stream.nativeAsset(token, session, asset)
		}
		if text != "" && !strings.HasPrefix(text, "#") {
			contentType := ""
			if nextPlaylist {
				contentType = "application/vnd.apple.mpegurl"
			}
			line = rewrite(text, false, contentType)
			nextPlaylist = false
			if line == "" {
				return "", errors.New("播放列表中的媒体地址无效")
			}
		} else if strings.Contains(text, "URI=") {
			invalid := false
			line = nativePlaylistURI.ReplaceAllStringFunc(line, func(match string) string {
				reference := nativePlaylistURI.FindStringSubmatch(match)[1]
				key := strings.HasPrefix(text, "#EXT-X-KEY:") || strings.HasPrefix(text, "#EXT-X-SESSION-KEY:")
				contentType := ""
				switch {
				case key:
					contentType = "application/octet-stream"
				case strings.HasPrefix(text, "#EXT-X-MAP:"):
					contentType = "video/mp4"
				case strings.HasPrefix(text, "#EXT-X-MEDIA:"), strings.HasPrefix(text, "#EXT-X-I-FRAME-STREAM-INF:"), strings.HasPrefix(text, "#EXT-X-RENDITION-REPORT:"):
					contentType = "application/vnd.apple.mpegurl"
				}
				updated := rewrite(reference, key, contentType)
				if updated == "" {
					invalid = true
				}
				return "URI=\"" + updated + "\""
			})
			if invalid {
				return "", errors.New("播放列表中的附属地址无效")
			}
		}
		if strings.HasPrefix(text, "#EXT-X-STREAM-INF:") {
			nextPlaylist = true
		}
		output = append(output, line)
	}
	return strings.Join(output, "\n"), nil
}

func (stream *nativeStreamServer) nativeServe(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	if len(parts) != 2 {
		// [红果漫剧补丁] 带实际路径段数，Dart 探针可见
		http.Error(writer, fmt.Sprintf("路径段数=%d path=%s", len(parts), request.URL.Path), http.StatusNotFound)
		return
	}
	stream.mu.Lock()
	session := stream.sessions[parts[0]]
	if session != nil {
		session.lastUsed = time.Now()
	}
	stream.mu.Unlock()
	if session == nil {
		stream.mu.Lock()
		alive := len(stream.sessions)
		stream.mu.Unlock()
		stream.downloader.recordDiagnostic(diagnosticEvent{Event: "stream.serve_miss", Level: "error", Message: fmt.Sprintf("session 不存在 token=%s 存活session=%d", parts[0], alive)})
		// [红果漫剧补丁] 响应体直接带诊断（Dart 探针会原样显示）
		http.Error(writer, fmt.Sprintf("播放已结束(token=%s… 存活session=%d)", parts[0][:12], alive), http.StatusGone)
		return
	}
	session.mu.Lock()
	asset, found := session.assets[parts[1]]
	assetCount := len(session.assets)
	session.mu.Unlock()
	if !found {
		// [红果漫剧补丁] 列出 session 内全部资产 id + 各自地址（定性 404：没注册 vs id 错位 vs 换了地址）
		ids := make([]string, 0, assetCount)
		session.mu.Lock()
		for assetID, a := range session.assets {
			ids = append(ids, fmt.Sprintf("%s(%s)", assetID[:12], a.address))
		}
		session.mu.Unlock()
		sort.Strings(ids)
		stream.downloader.recordDiagnostic(diagnosticEvent{Event: "stream.serve_miss", Level: "error", Message: fmt.Sprintf("asset 不存在 id=%s session资产=%d %v", parts[1], assetCount, ids)})
		// [红果漫剧补丁] 十六进制级对比：请求键 vs 实际键（揪出隐藏字符/编码差异）
		hexKeys := make([]string, 0, assetCount)
		session.mu.Lock()
		for assetID := range session.assets {
			hexKeys = append(hexKeys, fmt.Sprintf("%x", assetID))
		}
		session.mu.Unlock()
		sort.Strings(hexKeys)
		http.Error(writer, fmt.Sprintf("asset不存在 请求id=%q len=%d hex=%x\nsession资产(%d) hex:\n%s",
			parts[1], len(parts[1]), parts[1], assetCount, strings.Join(hexKeys, "\n")), http.StatusNotFound)
		return
	}
	ctx, cancel := context.WithCancel(providerMediaContext(request.Context(), session.credentials))
	defer cancel()
	stop := context.AfterFunc(session.ctx, cancel)
	defer stop()
	writer.Header().Set("Cache-Control", "no-store")
	if len(asset.data) > 0 && asset.total > int64(len(asset.data)) {
		if stream.nativeServePrefix(ctx, writer, request, session, asset) {
			return
		}
		asset.data = nil
	}
	if len(asset.data) > 0 {
		if strings.Contains(asset.contentType, "mpegurl") {
			body, err := stream.nativeRewrite(parts[0], session, string(asset.data), asset.address)
			if err != nil {
				http.Error(writer, err.Error(), http.StatusBadGateway)
				return
			}
			writer.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			if request.Method == http.MethodGet {
				_, _ = io.WriteString(writer, body)
			}
		} else {
			writer.Header().Set("Content-Type", asset.contentType)
			if asset.etag != "" {
				writer.Header().Set("ETag", asset.etag)
			}
			http.ServeContent(writer, request, parts[1], time.Time{}, bytes.NewReader(asset.data))
		}
		return
	}
	upstream, err := http.NewRequestWithContext(ctx, request.Method, asset.address, nil)
	if err != nil {
		stream.downloader.recordDiagnostic(diagnosticEvent{Event: "stream.upstream_invalid", Level: "error", Message: err.Error(), Host: asset.address})
		http.Error(writer, "媒体地址无效: "+err.Error(), http.StatusBadGateway)
		return
	}
	upstream.Header.Set("User-Agent", userAgent)
	upstream.Header.Set("Referer", session.referer)
	for _, name := range []string{"Range", "If-Range"} {
		if value := request.Header.Get(name); value != "" {
			upstream.Header.Set(name, value)
		}
	}
	response, err := stream.nativeRequest(upstream)
	if err != nil {
		stream.downloader.recordDiagnostic(diagnosticEvent{Event: "stream.upstream_error", Level: "error", Message: err.Error(), Host: asset.address})
		http.Error(writer, "读取媒体失败: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		err := stream.downloader.catalogResponseError(upstream, response, body)
		stream.downloader.recordDiagnostic(diagnosticEvent{Event: "stream.upstream_status", Level: "error", Message: err.Error(), Host: asset.address, HTTPStatus: response.StatusCode})
		http.Error(writer, fmt.Sprintf("上游 %d: %s", response.StatusCode, err.Error()), response.StatusCode)
		return
	}
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	finalURL := upstream.URL
	if response.Request != nil && response.Request.URL != nil {
		finalURL = response.Request.URL
	}
	// [红果漫剧补丁] 同 nativeOpen：只认 path 后缀与 mpegurl content-type，
	// 不用 URL 任意位置子串（会误命中 base64 参数里的巧合子串）。
	finalPath := strings.ToLower(finalURL.Path)
	// [红果漫剧补丁] 清单判定（多轮踩坑后的最终版）：
	//  1) HLS AES-128 源 session.key 非空，但那是给清单内 #EXT-X-KEY 用的，不是清单标志；
	//  2) path 里的 "hls" 多为【目录名】（剧果分片 = /hls/10362/480/seg-014.ts），
	//     不能作为清单依据，否则 .ts 分片会被当清单校验 #EXTM3U → 502；
	//  3) 因此：以【媒体扩展名】为最高优先级排除，再看 contentType / .m3u8 后缀。
	mediaExt := strings.HasSuffix(finalPath, ".ts") || strings.HasSuffix(finalPath, ".mp4") ||
		strings.HasSuffix(finalPath, ".m4s") || strings.HasSuffix(finalPath, ".aac") ||
		strings.HasSuffix(finalPath, ".m4a") || strings.HasSuffix(finalPath, ".mp3") ||
		strings.HasSuffix(finalPath, ".vtt")
	playlist := !mediaExt && (strings.Contains(asset.contentType, "mpegurl") ||
		strings.Contains(contentType, "mpegurl") ||
		strings.HasSuffix(finalPath, ".m3u8") || strings.Contains(finalPath, "m3u8") ||
		(len(session.key) > 0 && !strings.Contains(finalPath, ".")))
	reader := bufio.NewReader(response.Body)
	if !playlist && request.Method == http.MethodGet {
		peek, _ := reader.Peek(512)
		if strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(string(peek), "\ufeff")), "#EXTM3U") {
			playlist = true
		}
	}
	if playlist && request.Method == http.MethodHead {
		writer.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		writer.WriteHeader(http.StatusOK)
		return
	}
	if playlist && request.Method == http.MethodGet {
		// [红果漫剧补丁] ftyp 预探测提前：上游 .m3u8 被 CDN 重定向成 MP4 直链时
		//（实测：探针带 Range 得 206+mp4，无 Range 时 mp4 远超 4MB 清单上限），
		// 必须在读全量前就识别，否则先撞「播放列表过大」的 502。
		peekBytes, _ := reader.Peek(12)
		if len(peekBytes) >= 12 && string(peekBytes[4:8]) == "ftyp" {
			// [红果漫剧补丁] 直接 302 到上游 CDN 直链：mpv 持有 media_kit 注入的
			// Referer/UA 头，CDN 原生处理 Range/seek。此前经代理中转 serve 的
			// mp4 别名在真机上 seek 连发失败（size -38 → demuxer fatal）——
			// 代理 stream 层的 Range 透传不可靠，不再参与 mp4 数据面。
			stream.downloader.recordDiagnostic(diagnosticEvent{Event: "stream.m3u8_downgraded_to_mp4", Level: "warning", Message: "上游以 MP4 直链响应 .m3u8 地址，302 直连 CDN", Host: asset.address})
			writer.Header().Set("Cache-Control", "no-store")
			http.Redirect(writer, request, asset.address, http.StatusFound)
			return
		}
		body, err := io.ReadAll(io.LimitReader(reader, 4<<20+1))
		if err != nil || len(body) > 4<<20 {
			stream.downloader.recordDiagnostic(diagnosticEvent{Event: "stream.playlist_read_fail", Level: "error", Host: asset.address, Message: fmt.Sprintf("err=%v len=%d", err, len(body))})
			http.Error(writer, "播放列表过大或读取失败", http.StatusBadGateway)
			return
		}
		text := strings.TrimSpace(strings.TrimPrefix(string(body), "\ufeff"))
		if !strings.HasPrefix(text, "#EXTM3U") {
			preview := text
			if len(preview) > 96 {
				preview = preview[:96]
			}
			stream.downloader.recordDiagnostic(diagnosticEvent{Event: "stream.playlist_invalid", Level: "error", Host: asset.address, Message: fmt.Sprintf("ct=%s 前96字节=%q", contentType, preview)})
			http.Error(writer, fmt.Sprintf("播放列表无效(非#EXTM3U) ct=%s 前96字节=%q", contentType, preview), http.StatusBadGateway)
			return
		}
		rewritten, err := stream.nativeRewrite(parts[0], session, text, finalURL.String())
		// [红果漫剧补丁] dump 改写后的清单头部（定位其他源播放失败时 mpv 收到了什么）
		if err == nil {
			head := rewritten
			if len(head) > 320 {
				head = head[:320]
			}
			stream.downloader.recordDiagnostic(diagnosticEvent{Event: "stream.playlist_served", Level: "info", Host: asset.address,
				Message: fmt.Sprintf("in=%d out=%d 头部=%q", len(text), len(rewritten), head)})
		}
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadGateway)
			return
		}
		writer.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = io.WriteString(writer, rewritten)
		return
	}
	for _, name := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified"} {
		if value := response.Header.Get(name); value != "" {
			writer.Header().Set(name, value)
		}
	}
	writer.WriteHeader(response.StatusCode)
	if request.Method == http.MethodGet {
		_, _ = io.Copy(writer, reader)
	}
}
