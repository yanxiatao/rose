package converter

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/nopdan/rose/encoder"
	"github.com/nopdan/rose/filter"
	"github.com/nopdan/rose/model"
)

func TestConverter(t *testing.T) {
	conv := NewConverter()

	// 测试转换器初始化
	if conv.registry == nil {
		t.Fatal("Registry should not be nil")
	}

	// 测试添加过滤器
	conv.AddFilter(filter.NewLengthFilter(1, 10))
	conv.AddFilter(filter.NewFrequencyFilter(1, 0))

	if len(conv.filters) != 2 {
		t.Errorf("Expected 2 filters, got %d", len(conv.filters))
	}

	// 测试注册编码器
	pinyinEncoder := encoder.NewPinyinEncoder()
	conv.RegisterEncoder(pinyinEncoder)

	if conv.encoder == nil {
		t.Errorf("Expected encoder to be set")
	}

	// 测试获取支持的格式
	formats := conv.GetSupportedFormats()
	if len(formats) == 0 {
		t.Error("Should have at least one supported format")
	}

	t.Logf("Supported formats: %d", len(formats))
	for _, format := range formats {
		t.Logf("- %s: %s", format.ID, format.Name)
	}
}

func TestConverterApplyFilters(t *testing.T) {
	conv := NewConverter()

	// 添加过滤器
	conv.AddFilter(filter.NewLengthFilter(2, 4))
	conv.AddFilter(filter.NewCharacterFilter(true, false))

	// 创建测试数据
	entries := []*model.Entry{
		{Word: "一", Code: model.NewSimpleCode("")},
		{Word: "你好", Code: model.NewSimpleCode("")},
		{Word: "English", Code: model.NewSimpleCode("")},
		{Word: "测试词汇", Code: model.NewSimpleCode("")},
	}

	// 应用过滤器
	filtered := conv.applyFilters(entries)

	// 应该过滤掉"一"（长度不够）和"English"（包含英文）
	if len(filtered) != 2 {
		t.Errorf("Expected 2 entries after filtering, got %d", len(filtered))
	}

	// 验证剩余的词条
	expectedWords := map[string]bool{"你好": true, "测试词汇": true}
	for _, entry := range filtered {
		if !expectedWords[entry.Word] {
			t.Errorf("Unexpected word after filtering: %s", entry.Word)
		}
	}
}

func TestConverterIntegration(t *testing.T) {
	conv := NewConverter()

	// 添加过滤器
	conv.AddFilter(filter.NewLengthFilter(1, 10))

	// 添加编码器
	pinyinEncoder := encoder.NewPinyinEncoder()
	conv.RegisterEncoder(pinyinEncoder)

	// 覆盖编码器
	wubiEncoder := encoder.NewWubiEncoder("wubi86", nil, false)
	conv.RegisterEncoder(wubiEncoder)

	// 测试编码器注册
	if conv.encoder != wubiEncoder {
		t.Error("Expected encoder to be replaced by the latest registration")
	}

	t.Log("Integration test passed - converter can handle filters and encoder")
}

func TestDedupeEntries(t *testing.T) {
	entries := []*model.Entry{
		{Word: "你好", Code: model.NewSimpleCode("nihc")},
		{Word: "你好", Code: model.NewSimpleCode("nihc")},
		{Word: "你好", Code: model.NewSimpleCode("nihao")},
		{Word: "世界", Code: model.NewMultiCode("shi", "jie")},
		{Word: "世界", Code: model.NewMultiCode("shi", "jie")},
		{Word: "世界"},
	}
	kept, duplicates := dedupeEntries(entries)
	if duplicates != 2 {
		t.Errorf("Expected 2 duplicates, got %d", duplicates)
	}
	if len(kept) != 4 {
		t.Fatalf("Expected 4 entries after dedupe, got %d", len(kept))
	}
	if kept[0].Code.String() != "nihc" || kept[1].Code.String() != "nihao" {
		t.Errorf("Unexpected entries after dedupe: %s, %s", kept[0].Code, kept[1].Code)
	}
}

func TestConvertMergeMultipleInputs(t *testing.T) {
	dir := t.TempDir()
	writeFile := func(name, content string) string {
		path := dir + "/" + name
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	pathA := writeFile("a.txt", "你好\n世界\n再见\n")
	pathB := writeFile("b.txt", "你好\n世界\n劳动\n")

	wordsFormat := func() *CustomFormatConfig {
		return &CustomFormatConfig{Kind: "words"}
	}
	outputPath := dir + "/merged.txt"

	conv := NewConverter()
	result, err := conv.Convert(&Job{
		Inputs: []*InputSpec{
			{Path: pathA, Format: "custom", Custom: wordsFormat()},
			{Path: pathB, Format: "custom", Custom: wordsFormat()},
		},
		Output: &OutputSpec{
			Path:   outputPath,
			Format: "custom",
			Custom: wordsFormat(),
		},
	})
	if err != nil {
		t.Fatalf("Convert failed: %v", err)
	}

	if result.Stats.InputEntries != 6 {
		t.Errorf("Expected 6 input entries, got %d", result.Stats.InputEntries)
	}
	if result.Stats.Duplicates != 2 {
		t.Errorf("Expected 2 duplicates, got %d", result.Stats.Duplicates)
	}
	if result.Stats.OutputEntries != 4 {
		t.Errorf("Expected 4 output entries, got %d", result.Stats.OutputEntries)
	}
	if result.Stats.FilteredOut != 0 {
		t.Errorf("Expected 0 filtered entries, got %d", result.Stats.FilteredOut)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\r\n"), "\n")
	if len(lines) != 4 {
		t.Errorf("Expected 4 lines in merged output, got %d: %q", len(lines), string(data))
	}
}

func TestConvertSplitOutput(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/words.txt"
	if err := os.WriteFile(path, []byte("甲\n乙\n丙\n丁\n戊\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	outputPath := dir + "/split.txt"
	conv := NewConverter()
	result, err := conv.Convert(&Job{
		Input: &InputSpec{
			Path:   path,
			Format: "custom",
			Custom: &CustomFormatConfig{Kind: "words"},
		},
		Output: &OutputSpec{
			Path:      outputPath,
			Format:    "custom",
			Custom:    &CustomFormatConfig{Kind: "words"},
			SplitSize: 2,
		},
	})
	if err != nil {
		t.Fatalf("Convert failed: %v", err)
	}

	if len(result.OutputFiles) != 3 {
		t.Fatalf("Expected 3 output files, got %d: %v", len(result.OutputFiles), result.OutputFiles)
	}
	expectedCounts := []int{2, 2, 1}
	for i, file := range result.OutputFiles {
		expected := fmt.Sprintf("%s/split_%d.txt", dir, i+1)
		if file != expected {
			t.Errorf("Expected file %s, got %s", expected, file)
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimRight(string(data), "\r\n"), "\n")
		if len(lines) != expectedCounts[i] {
			t.Errorf("Expected %d lines in %s, got %d: %q", expectedCounts[i], file, len(lines), string(data))
		}
	}
	if result.Stats.OutputEntries != 5 {
		t.Errorf("Expected 5 output entries, got %d", result.Stats.OutputEntries)
	}
}
