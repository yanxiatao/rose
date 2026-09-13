package encoder

import (
	"fmt"
	"testing"
	"unicode/utf8"

	"github.com/nopdan/rose/model"
)

func TestPinyinEncode_Default(t *testing.T) {
	enc := NewPinyinEncoder()
	entry := &model.Entry{Word: "你好", CodeType: model.CodeTypeNone}

	enc.Encode(entry)
	fmt.Printf("%s", entry.Code.String())

	if entry.CodeType != model.CodeTypePinyin {
		t.Fatalf("expected CodeTypePinyin, got %v", entry.CodeType)
	}
	if entry.Code == nil || entry.Code.IsEmpty() {
		t.Fatalf("expected Code to be set")
	}
	if got, want := len(entry.Code.Strings()), utf8.RuneCountInString(entry.Word); got != want {
		t.Fatalf("expected %d codes, got %d", want, got)
	}
}

func TestPinyinEncode_IncompleteAppend(t *testing.T) {
	enc := NewPinyinEncoder()
	entry := &model.Entry{
		Word:     "你好",
		Code:     model.NewMultiCode("ni"),
		CodeType: model.CodeTypeIncompletePinyin,
	}

	enc.Encode(entry)
	fmt.Printf("%s", entry.Code.String())

	if entry.CodeType != model.CodeTypePinyin {
		t.Fatalf("expected CodeTypePinyin, got %v", entry.CodeType)
	}
	codes := entry.Code.Strings()
	if got, want := len(codes), utf8.RuneCountInString(entry.Word); got != want {
		t.Fatalf("expected %d codes, got %d", want, got)
	}
	if codes[0] != "ni" {
		t.Fatalf("expected first code to be preserved, got %q", codes[0])
	}
}

func TestPinyinEncode_PinyinString(t *testing.T) {
	entry := &model.Entry{
		Word:     "会计",
		Code:     model.NewSimpleCode("kuaiji"),
		CodeType: model.CodeTypePinyinString,
	}

	enc := NewPinyinEncoder()
	enc.Encode(entry)
	fmt.Printf("%s", entry.Code.String())
}

func TestPinyinEncodeBatch(t *testing.T) {
	enc := NewPinyinEncoder()
	entries := []*model.Entry{
		{Word: "你好", CodeType: model.CodeTypeNone},
		{Word: "世界", CodeType: model.CodeTypeNone},
	}

	enc.EncodeBatch(entries)
	for _, entry := range entries {
		fmt.Printf("%v\n", entry)
	}

	for i, entry := range entries {
		if entry.CodeType != model.CodeTypePinyin {
			t.Fatalf("entry %d expected CodeTypePinyin, got %v", i, entry.CodeType)
		}
		if entry.Code == nil || entry.Code.IsEmpty() {
			t.Fatalf("entry %d expected Code to be set", i)
		}
	}
}

func TestPinyinEncode_ShapeCodeReencode(t *testing.T) {
	// 形码词条（如四键 aaqd）被标为拼音类型时，音节数与字数不符，应重新生成拼音
	enc := NewPinyinEncoder()
	entry := &model.Entry{
		Word:     "暗暗祈祷",
		Code:     model.NewMultiCode("aaqd"),
		CodeType: model.CodeTypePinyin,
	}

	enc.Encode(entry)

	codes := entry.Code.Strings()
	if got, want := len(codes), 4; got != want {
		t.Fatalf("expected %d re-encoded syllables, got %d: %v", want, got, codes)
	}
	for _, c := range codes {
		if c == "aaqd" {
			t.Fatalf("shape code should be replaced, got %v", codes)
		}
	}

	// 正常逐字拼音不应被改写
	keep := &model.Entry{
		Word:     "你好",
		Code:     model.NewMultiCode("ni", "hao"),
		CodeType: model.CodeTypePinyin,
	}
	enc.Encode(keep)
	if got := keep.Code.Strings(); got[0] != "ni" || got[1] != "hao" {
		t.Fatalf("valid pinyin should be preserved, got %v", got)
	}
}
