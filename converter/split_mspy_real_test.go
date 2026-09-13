package converter

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/nopdan/rose/format"
	"github.com/nopdan/rose/model"
)

// TestSplitMspyRimeFrost 复现真实场景：rime-frost 多词库合并 → mspy_udl split=20000
// 词库目录不存在时跳过（CI 环境无此数据）
func TestSplitMspyRimeFrost(t *testing.T) {
	dictDir := `E:\build\rime-frost-master\dict`
	if _, err := os.Stat(dictDir); err != nil {
		t.Skip("rime-frost dict dir not available")
	}

	files := []string{
		"4jian_no_conflict.dict.yaml", // 形码词库：编码非拼音，必须重新生成拼音
		"base.dict.yaml",              // 常规拼音词库
		"shulihua.dict.yaml",          // 数字化词库：可能含特殊编码
		"en.dict.yaml",                // 英文词库：导出时应被整体过滤
	}
	inputs := make([]*InputSpec, 0, len(files))
	for _, name := range files {
		p := filepath.Join(dictDir, name)
		if _, err := os.Stat(p); err != nil {
			continue
		}
		inputs = append(inputs, &InputSpec{Path: p, Format: "rime_pinyin"})
	}
	if len(inputs) < 2 {
		t.Skip("not enough rime-frost dicts found")
	}

	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.dat")

	conv := NewConverter()
	result, err := conv.Convert(&Job{
		Inputs: inputs,
		Output: &OutputSpec{
			Path:      outPath,
			Format:    "mspy_udl",
			SplitSize: 20000,
		},
	})
	if err != nil {
		t.Fatalf("convert failed: %v", err)
	}
	t.Logf("inputEntries=%d outputEntries=%d duplicates=%d files=%d",
		result.Stats.InputEntries, result.Stats.OutputEntries, result.Stats.Duplicates, len(result.OutputFiles))

	outFmt, _ := format.GlobalRegistry.Get("mspy_udl")
	importer := outFmt.(model.Importer)

	emptyFiles := 0
	shortRecords := 0
	var totalReimported int
	for idx, file := range result.OutputFiles {
		fi, _ := os.Stat(file)
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		count := int(binary.LittleEndian.Uint32(data[0xC:0x10]))
		if count == 0 {
			emptyFiles++
			t.Errorf("file %d is EMPTY (%s)", idx+1, filepath.Base(file))
			continue
		}
		if int64(len(data)) != fi.Size() || len(data)%0x400 != 0 {
			t.Errorf("file %d size %d not 0x400-aligned", idx+1, len(data))
		}

		entries, err := importer.Import(model.NewFileSource(file))
		if err != nil {
			t.Errorf("file %d import error: %v", idx+1, err)
			continue
		}
		if len(entries) != count {
			t.Errorf("file %d headerCount=%d reimported=%d", idx+1, count, len(entries))
		}
		// 每条记录的音节数必须等于字数（即不存在索引缺失的短记录）
		for _, e := range entries {
			if e.Code != nil && len(e.Code.Strings()) != len([]rune(e.Word)) {
				shortRecords++
			}
		}
		totalReimported += len(entries)

		if idx < 3 || idx == len(result.OutputFiles)-1 {
			t.Logf("file %d: size=%dB count=%d reimported=%d", idx+1, len(data), count, len(entries))
		}
	}
	t.Logf("summary: files=%d empty=%d shortRecords=%d totalReimported=%d (expect %d)",
		len(result.OutputFiles), emptyFiles, shortRecords, totalReimported, result.Stats.OutputEntries)

	if emptyFiles > 0 {
		t.Errorf("got %d empty files", emptyFiles)
	}
	if shortRecords > 0 {
		t.Errorf("got %d records with missing pinyin indices", shortRecords)
	}
	if totalReimported != result.Stats.OutputEntries {
		t.Errorf("reimported total %d != outputEntries %d", totalReimported, result.Stats.OutputEntries)
	}
}
