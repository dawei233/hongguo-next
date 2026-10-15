// 本仓新增（M1 站源适配层测试）。
//
// 理由：本机没有 jsonschema 库，而「契约与实现逐字段对应」是本里程碑的核心验收点。
// 这里实现一个最小校验器（只覆盖本仓契约实际用到的关键字：type / required /
// additionalProperties / properties / items / enum / pattern / minimum / maximum /
// minLength / $ref），把适配层产出的每个对象都对着 contracts/ 里的 schema 校验一遍。
// 遇到不认识的关键字就跳过而不是报错，避免把校验器的能力边界误报成契约违规。
package adapter_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/dawei233/hongguo-next/core"
	"github.com/dawei233/hongguo-next/core/source"
	"github.com/dawei233/hongguo-next/core/source/adapter"
)

const contractsDir = "../../../contracts"

type contractFile struct {
	name string
	doc  map[string]any
}

var contractCache = map[string]*contractFile{}

func contract(t *testing.T, name string) *contractFile {
	t.Helper()
	if cached, ok := contractCache[name]; ok {
		return cached
	}
	path := filepath.Join(contractsDir, name)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取契约 %s 失败: %v", path, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("契约 %s 不是合法 JSON: %v", name, err)
	}
	file := &contractFile{name: name, doc: doc}
	contractCache[name] = file
	return file
}

// pointer 解析 "#/$defs/Series" 形式的 JSON Pointer。
func (file *contractFile) pointer(t *testing.T, pointer string) map[string]any {
	t.Helper()
	current := any(file.doc)
	for _, segment := range splitPointer(pointer) {
		object, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("%s: %q 在 %q 处不是对象", file.name, pointer, segment)
		}
		next, ok := object[segment]
		if !ok {
			t.Fatalf("%s: %q 缺少 %q", file.name, pointer, segment)
		}
		current = next
	}
	definition, ok := current.(map[string]any)
	if !ok {
		t.Fatalf("%s: %q 不是对象定义", file.name, pointer)
	}
	return definition
}

func splitPointer(pointer string) []string {
	segments := []string{}
	for _, part := range regexp.MustCompile(`/`).Split(pointer, -1) {
		if part != "" && part != "#" {
			segments = append(segments, part)
		}
	}
	return segments
}

func (file *contractFile) def(t *testing.T, name string) map[string]any {
	t.Helper()
	return file.pointer(t, "/$defs/"+name)
}

// resolve 跟随 $ref（支持跨文件），返回定义所属文件与定义本身。
func resolve(t *testing.T, file *contractFile, definition map[string]any) (*contractFile, map[string]any) {
	t.Helper()
	for depth := 0; depth < 8; depth++ {
		reference, ok := definition["$ref"].(string)
		if !ok {
			return file, definition
		}
		target := file
		pointer := reference
		if index := indexOfHash(reference); index >= 0 {
			pointer = reference[index+1:]
			if name := reference[:index]; name != "" {
				target = contract(t, name)
			}
		}
		definition = target.pointer(t, pointer)
		file = target
	}
	t.Fatal("$ref 嵌套过深")
	return file, definition
}

func indexOfHash(value string) int {
	for index, char := range value {
		if char == '#' {
			return index
		}
	}
	return -1
}

// conforms 按契约校验一个 JSON 值；只对能判定的关键字失败。
func conforms(t *testing.T, label string, file *contractFile, definition map[string]any, value any) {
	t.Helper()
	file, definition = resolve(t, file, definition)
	if alternatives, ok := definition["oneOf"].([]any); ok && len(alternatives) > 0 {
		// 判别式联合：这里只要求至少命中一支，命中不了再报错。
		for _, alternative := range alternatives {
			entry, ok := alternative.(map[string]any)
			if !ok {
				continue
			}
			if matches(t, file, entry, value) {
				return
			}
		}
		t.Errorf("%s: 不满足契约的任何一个 oneOf 分支: %v", label, value)
		return
	}

	if types, ok := definition["type"]; ok && !typeMatches(types, value) {
		t.Errorf("%s: 类型不符，契约 %v，实际 %T(%v)", label, types, value, value)
		return
	}
	if enum, ok := definition["enum"].([]any); ok {
		matched := false
		for _, candidate := range enum {
			if fmt.Sprint(candidate) == fmt.Sprint(value) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("%s: 取值 %v 不在契约枚举 %v 内", label, value, enum)
		}
	}
	if text, ok := value.(string); ok {
		if pattern, ok := definition["pattern"].(string); ok {
			if !regexp.MustCompile(pattern).MatchString(text) {
				t.Errorf("%s: %q 不匹配契约 pattern %q", label, text, pattern)
			}
		}
		if minimum, ok := definition["minLength"].(float64); ok && float64(len([]rune(text))) < minimum {
			t.Errorf("%s: 长度 %d 小于契约 minLength %v", label, len([]rune(text)), minimum)
		}
		if format, ok := definition["format"].(string); ok && format == "date-time" {
			if _, err := time.Parse(time.RFC3339, text); err != nil {
				t.Errorf("%s: %q 不是 RFC3339 时刻: %v", label, text, err)
			}
		}
	}
	if number, ok := value.(float64); ok {
		if minimum, ok := definition["minimum"].(float64); ok && number < minimum {
			t.Errorf("%s: %v 小于契约 minimum %v", label, number, minimum)
		}
		if maximum, ok := definition["maximum"].(float64); ok && number > maximum {
			t.Errorf("%s: %v 大于契约 maximum %v", label, number, maximum)
		}
	}

	if properties, ok := definition["properties"].(map[string]any); ok {
		object, ok := value.(map[string]any)
		if !ok {
			return
		}
		for _, name := range stringList(definition["required"]) {
			if _, present := object[name]; !present {
				t.Errorf("%s: 缺少契约必填字段 %q", label, name)
			}
		}
		for name := range object {
			if _, known := properties[name]; !known {
				if strict, ok := definition["additionalProperties"].(bool); ok && !strict {
					t.Errorf("%s: 出现契约未声明的字段 %q", label, name)
				}
			}
		}
		for name, raw := range properties {
			entry, present := object[name]
			if !present {
				continue
			}
			property, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			conforms(t, label+"."+name, file, property, entry)
		}
		return
	}

	if items, ok := definition["items"].(map[string]any); ok {
		array, ok := value.([]any)
		if !ok {
			return
		}
		for index, entry := range array {
			conforms(t, fmt.Sprintf("%s[%d]", label, index), file, items, entry)
		}
	}
}

// matches 用于 oneOf 判定：静默校验，返回是否命中。
func matches(t *testing.T, file *contractFile, definition map[string]any, value any) bool {
	t.Helper()
	file, definition = resolve(t, file, definition)
	if types, ok := definition["type"]; ok && !typeMatches(types, value) {
		return false
	}
	if properties, ok := definition["properties"].(map[string]any); ok {
		object, ok := value.(map[string]any)
		if !ok {
			return false
		}
		for _, name := range stringList(definition["required"]) {
			if _, present := object[name]; !present {
				return false
			}
		}
		for name, raw := range properties {
			entry, present := object[name]
			if !present {
				continue
			}
			property, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if constant, ok := property["const"]; ok && fmt.Sprint(constant) != fmt.Sprint(entry) {
				return false
			}
		}
	}
	return true
}

func typeMatches(types any, value any) bool {
	names := stringList(types)
	if len(names) == 0 {
		return true
	}
	for _, name := range names {
		switch name {
		case "null":
			if value == nil {
				return true
			}
		case "object":
			if _, ok := value.(map[string]any); ok {
				return true
			}
		case "array":
			if _, ok := value.([]any); ok {
				return true
			}
		case "string":
			if _, ok := value.(string); ok {
				return true
			}
		case "boolean":
			if _, ok := value.(bool); ok {
				return true
			}
		case "integer":
			if number, ok := value.(float64); ok && number == float64(int64(number)) {
				return true
			}
		case "number":
			if _, ok := value.(float64); ok {
				return true
			}
		}
	}
	return false
}

func stringList(value any) []string {
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case []any:
		names := make([]string, 0, len(typed))
		for _, entry := range typed {
			if name, ok := entry.(string); ok {
				names = append(names, name)
			}
		}
		return names
	}
	return nil
}

// marshalObject 把值转成 map[string]any（JSON 视角），便于按契约校验。
func marshalObject(t *testing.T, value any) map[string]any {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var object map[string]any
	if err := json.Unmarshal(body, &object); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	return object
}

// TestSeriesEpisodePageConformToContract 校验适配层产出的模型对象逐字段符合契约。
func TestSeriesEpisodePageConformToContract(t *testing.T) {
	bridge := contract(t, "bridge-api.schema.json")

	series, err := adapter.SeriesFromDrama(core.Drama{
		ID: "hongguo:12345", Source: "hongguo", SourceID: "12345", Title: "都市之巅",
		Cover: "https://cdn.example.com/c.jpg", TotalEpisode: "24", CategoryName: "都市",
		Score: "9.2分", OnlineDate: "2024-05-01",
	})
	if err != nil {
		t.Fatalf("SeriesFromDrama 失败: %v", err)
	}
	conforms(t, "Series", bridge, bridge.def(t, "Series"), marshalObject(t, series))

	episode, err := adapter.EpisodeFromChapter(core.Chapter{ID: "hongguo:12345:7301", Title: "第1集"}, "", 1)
	if err != nil {
		t.Fatalf("EpisodeFromChapter 失败: %v", err)
	}
	conforms(t, "Episode", bridge, bridge.def(t, "Episode"), marshalObject(t, episode))

	page, err := adapter.PageFromNativeCatalog(adapter.NativeCatalogPage{
		Items:   []adapter.NativeDrama{{ID: "hongguo:12345", Source: "hongguo", SourceID: "12345", Title: "都市之巅", Episodes: 24}},
		HasMore: true,
	})
	if err != nil {
		t.Fatalf("PageFromNativeCatalog 失败: %v", err)
	}
	conforms(t, "Page", bridge, bridge.def(t, "Page"), marshalObject(t, page))
}

// TestStreamConformsToContract 校验三条取流路径的产物都符合 Stream 契约。
func TestStreamConformsToContract(t *testing.T) {
	bridge := contract(t, "bridge-api.schema.json")
	definition := bridge.def(t, "Stream")

	encrypted, err := adapter.StreamFromProviderMedia(adapter.ProviderMedia{
		URL: "https://cdn.example.com/a.mp4", Referer: "https://novel.snssdk.com/", CENCKey: testKeyBytes, Quality: 1080,
	})
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	conforms(t, "Stream(providerMedia/cenc)", bridge, definition, marshalObject(t, encrypted))

	hls, err := adapter.StreamFromProviderMedia(adapter.ProviderMedia{
		URL: "https://cdn.example.com/play.m3u8", Playlist: "#EXTM3U", HLSKey: testKeyBytes, Quality: 720,
	})
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	conforms(t, "Stream(providerMedia/hls)", bridge, definition, marshalObject(t, hls))

	direct, err := adapter.StreamFromChapter(core.Chapter{VideoURL: "https://cdn.example.com/a.mp4"})
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	conforms(t, "Stream(chapter)", bridge, definition, marshalObject(t, direct))

	plan, err := adapter.StreamFromNativePlan(adapter.NativePlan{
		URL: "https://cdn.example.com/a.mp4", Key: testKeyHex, Quality: 1080, ExpiresAt: time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	conforms(t, "Stream(nativePlan)", bridge, definition, marshalObject(t, plan))
}

// TestCapsAndDanmakuConformToContract 校验能力位与弹幕线格式。
func TestCapsAndDanmakuConformToContract(t *testing.T) {
	capsFile := contract(t, "source-caps.schema.json")
	caps, err := adapter.AssumedCaps(source.Hongguo)
	if err != nil {
		t.Fatalf("AssumedCaps 失败: %v", err)
	}
	body, err := json.Marshal(caps)
	if err != nil {
		t.Fatalf("序列化能力位失败: %v", err)
	}
	var names []any
	if err := json.Unmarshal(body, &names); err != nil {
		t.Fatalf("能力位不是数组形态: %v", err)
	}
	conforms(t, "Caps", capsFile, capsFile.def(t, "caps"), names)

	danmakuFile := contract(t, "danmaku.schema.json")
	track := source.DanmakuTrack{
		VideoID: "7301",
		Items: []source.Danmaku{
			{TimeMS: 1200, Text: "前排", Color: source.DanmakuColorDefault, Mode: source.DanmakuModeScroll, Weight: 0},
			{TimeMS: 3400, Text: "顶部", Color: "#FF000080", Mode: source.DanmakuModeTop, Weight: 5},
			{TimeMS: 5600, Text: "底部", Color: "#00FF00FF", Mode: source.DanmakuModeBottom, Weight: 1},
		},
	}
	conforms(t, "DanmakuTrack", danmakuFile, danmakuFile.def(t, "danmakuTrack"), marshalObject(t, track))
}

// TestQualityLabelsConformToContractPattern 用契约里的 pattern 覆盖清晰度标签的全值域。
func TestQualityLabelsConformToContractPattern(t *testing.T) {
	bridge := contract(t, "bridge-api.schema.json")
	stream := bridge.def(t, "Stream")
	properties, ok := stream["properties"].(map[string]any)
	if !ok {
		t.Fatal("Stream 缺少 properties")
	}
	quality, ok := properties["quality"].(map[string]any)
	if !ok {
		t.Fatal("Stream 缺少 quality")
	}
	pattern, ok := quality["pattern"].(string)
	if !ok {
		t.Fatal("Stream.quality 缺少 pattern")
	}
	compiled := regexp.MustCompile(pattern)
	for height := -10; height <= 12000; height++ {
		label := adapter.QualityLabel(height)
		if !compiled.MatchString(label) {
			t.Fatalf("QualityLabel(%d) = %q 不匹配契约 pattern %q", height, label, pattern)
		}
	}
	for _, declared := range []source.Quality{
		source.QualityAuto, source.Quality2160p, source.Quality1440p, source.Quality1080p,
		source.Quality720p, source.Quality480p, source.Quality360p,
	} {
		if !compiled.MatchString(string(declared)) {
			t.Errorf("source.Quality 常量 %q 不匹配契约 pattern", declared)
		}
	}
	// 容器枚举也必须与 Go 常量一致（container 通过 $ref 指向 $defs.Container）。
	container, ok := properties["container"].(map[string]any)
	if !ok {
		t.Fatal("Stream 缺少 container")
	}
	_, resolvedContainer := resolve(t, bridge, container)
	enum := stringList(resolvedContainer["enum"])
	if len(enum) != 2 || enum[0] != source.ContainerMP4 || enum[1] != source.ContainerHLS {
		t.Fatalf("Stream.container 枚举 %v 与 Go 常量不一致", enum)
	}
}
