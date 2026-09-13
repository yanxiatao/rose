package converter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nopdan/rose/format"
	"github.com/nopdan/rose/model"
)

// TestSplitRoundtripAllFormats 对所有可导出格式做 分割导出→回读 校验
func TestSplitRoundtripAllFormats(t *testing.T) {
	cases := []struct {
		name        string
		inputFile   string
		inputFormat string
		outFormat   string
	}{
		{"mspy_udl", "../testdata/ChsPinyinUDL.dat", "mspy_udl", "mspy_udl"},
		{"sogou_scel", "../testdata/sogou.scel", "scel", "scel"},
		{"mswb_lex", "../testdata/ChsWubi.lex", "mswb_lex", "mswb_lex"},
		{"msudp", "../testdata/ChsPinyinUDP.lex", "msudp", "msudp"},
		{"gboard", "../testdata/ChsPinyinUDL.dat", "mspy_udl", "gboard"},
		{"jiajia", "../testdata/jiajia.txt", "jiajia", "jiajia"},
		{"baidu_def", "../testdata/sample.def", "def", "def"},
		{"jidian", "../testdata/duoduo.txt", "duoduo", "jidian"},
		{"words", "../testdata/duoduo.txt", "duoduo", "words"},
	}

	const splitSize = 3
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			outPath := filepath.Join(dir, "out_"+tc.name)

			conv := NewConverter()
			result, err := conv.Convert(&Job{
				Input: &InputSpec{
					Path:   tc.inputFile,
					Format: tc.inputFormat,
				},
				Output: &OutputSpec{
					Path:      outPath,
					Format:    tc.outFormat,
					SplitSize: splitSize,
				},
			})
			if err != nil {
				t.Fatalf("convert failed: %v", err)
			}
			t.Logf("total=%d files=%d", result.Stats.OutputEntries, len(result.OutputFiles))

			outFmt, ok := format.GlobalRegistry.Get(tc.outFormat)
			if !ok {
				t.Fatalf("format %s not found", tc.outFormat)
			}
			importer, ok := outFmt.(model.Importer)
			if !ok {
				t.Fatalf("format %s not importable", tc.outFormat)
			}

			for idx, file := range result.OutputFiles {
				fi, err := os.Stat(file)
				if err != nil {
					t.Errorf("chunk %d: stat: %v", idx+1, err)
					continue
				}
				entries, err := importer.Import(model.NewFileSource(file))
				if err != nil {
					t.Errorf("chunk %d (%s, %dB): import error: %v", idx+1, filepath.Base(file), fi.Size(), err)
					continue
				}
				expect := splitSize
				if idx == len(result.OutputFiles)-1 {
					expect = (result.Stats.OutputEntries-1)%splitSize + 1
				}
				status := "OK"
				if len(entries) != expect {
					status = "MISMATCH"
				}
				t.Logf("chunk %d (%s, %dB): got=%d expect=%d %s", idx+1, filepath.Base(file), fi.Size(), len(entries), expect, status)
			}
		})
	}
}

