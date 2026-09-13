package msudp

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/nopdan/rose/model"
)

func TestMsUDP(t *testing.T) {
	f := New()
	f.LogLevel = model.LogDebug
	inputPath := "../../testdata/UserDefinedPhrase.dat"
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

// 示例文件回读：80 条自定义短语
func TestImportSample(t *testing.T) {
	f := New()
	entries, err := f.Import(model.NewFileSource("../../testdata/UserDefinedPhrase.dat"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 80 {
		t.Fatalf("expected 80 entries, got %d", len(entries))
	}
	if entries[0].Word == "" || entries[0].Code.String() == "" {
		t.Fatalf("unexpected first entry: %q %q", entries[0].Word, entries[0].Code)
	}
}

// 导出→回读往返，长度字段必须与记录实际字节数一致
func TestExportRoundtripVariableLength(t *testing.T) {
	f := New()
	entries := []*model.Entry{
		model.NewEntry("测试").WithSimpleCode("ceshi").WithRank(1),
		model.NewEntry("三个字").WithSimpleCode("sangezi").WithRank(1),
		model.NewEntry("五个字的词组").WithSimpleCode("wugezidecizu").WithRank(1),
	}

	var buf bytes.Buffer
	if err := f.Export(entries, &buf); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()

	// 偏移表与记录长度自洽：每条记录字节数 = 6 前缀 + 长度字段
	entryStart := int(binary.LittleEndian.Uint32(data[0x14:0x18]))
	for i := range 3 {
		offset := int(binary.LittleEndian.Uint32(data[0x40+4*i : 0x44+4*i]))
		rec := entryStart + offset
		length := int(binary.LittleEndian.Uint16(data[rec+4 : rec+6]))
		size := 6 + length
		if i < 2 {
			next := int(binary.LittleEndian.Uint32(data[0x44+4*i : 0x48+4*i]))
			if offset+size != next {
				t.Fatalf("entry %d: offset %d + size %d != next offset %d", i, offset, size, next)
			}
		}
	}

	back, err := f.Import(model.NewBytesSource("test.dat", data))
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 3 {
		t.Fatalf("expected 3 entries after roundtrip, got %d", len(back))
	}
	for i, want := range []string{"测试", "三个字", "五个字的词组"} {
		if back[i].Word != want {
			t.Fatalf("entry %d: expected %q, got %q", i, want, back[i].Word)
		}
	}
}

// 拼音形式：相同拼音码按词频降序生成候选序
func TestPinyinFormRankGeneration(t *testing.T) {
	f := NewPinyin()
	if f.ID != "mspy_eudp" || f.Type != model.FormatTypePinyin {
		t.Fatalf("unexpected format meta: %s %v", f.ID, f.Type)
	}

	entries := []*model.Entry{
		model.NewEntry("世纪").WithMultiCode("shi", "ji").WithFrequency(1),
		model.NewEntry("时机").WithMultiCode("shi", "ji").WithFrequency(5),
		model.NewEntry("世界").WithMultiCode("shi", "jie").WithFrequency(9),
	}

	var buf bytes.Buffer
	if err := f.Export(entries, &buf); err != nil {
		t.Fatal(err)
	}

	back, err := f.Import(model.NewBytesSource("test.dat", buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(back))
	}
	// 世纪/时机 同码：时机词频高排第 1
	if back[0].Word != "时机" || back[0].Rank != 1 {
		t.Fatalf("expected 时机 rank 1, got %s rank %d", back[0].Word, back[0].Rank)
	}
	if back[1].Word != "世纪" || back[1].Rank != 2 {
		t.Fatalf("expected 世纪 rank 2, got %s rank %d", back[1].Word, back[1].Rank)
	}
}
