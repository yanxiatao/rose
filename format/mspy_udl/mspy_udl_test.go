package mspy_udl

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/nopdan/rose/model"
)

func TestMspyUDL(t *testing.T) {
	f := New()
	f.LogLevel = model.LogDebug
	inputPath := "../../testdata/ChsPinyinUDP.lex"
	entries, err := f.Import(model.NewFileSource(inputPath))
	if err != nil {
		t.Fatal(err)
	}

	outputPath := "test_export" + filepath.Ext(inputPath)
	out, err := os.Create(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()

	if err := f.Export(entries, out); err != nil {
		t.Fatal(err)
	}
}

// 音节无法全部映射的词条必须整条丢弃，不能写出索引缺失的记录
func TestExportDropsUnmappableSyllable(t *testing.T) {
	f := New()
	entries := []*model.Entry{
		model.NewEntry("你好").WithMultiCode("ni", "hao").WithFrequency(1),
		// "n" 不在微软拼音音节表中，无法映射
		model.NewEntry("嗯").WithMultiCode("n").WithFrequency(1),
		model.NewEntry("世界").WithMultiCode("shi", "jie").WithFrequency(1),
	}

	var buf bytes.Buffer
	if err := f.Export(entries, &buf); err != nil {
		t.Fatal(err)
	}

	data := buf.Bytes()
	count := int(binary.LittleEndian.Uint32(data[0xC:0x10]))
	if count != 2 {
		t.Fatalf("expected 2 records in file, got %d", count)
	}

	// 回读校验
	back, err := f.Import(model.NewBytesSource("test.dat", data))
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 2 {
		t.Fatalf("expected 2 entries after roundtrip, got %d", len(back))
	}
	if back[0].Word != "你好" || back[1].Word != "世界" {
		t.Fatalf("unexpected words: %s, %s", back[0].Word, back[1].Word)
	}
}
