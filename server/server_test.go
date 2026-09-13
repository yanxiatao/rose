package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nopdan/rose/converter"
)

// uploadForTest 通过 /api/upload 处理器上传文件，返回文件 ID
func uploadForTest(t *testing.T, name, content string) string {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fw, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	writer.Close()

	req := httptest.NewRequest("POST", "/api/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	handleUpload(rec, req)
	if rec.Code != 200 {
		t.Fatalf("upload failed: %d %s", rec.Code, rec.Body.String())
	}
	var res struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	return res.ID
}

func TestHandleConvertMergesMultipleFiles(t *testing.T) {
	idA := uploadForTest(t, "a.txt", "你好\n世界\n再见\n")
	idB := uploadForTest(t, "b.txt", "你好\n世界\n劳动\n")

	reqBody := ConvertRequest{
		FileIDs:      []string{idA, idB},
		InputFormat:  "custom",
		OutputFormat: "custom",
		InputCustom:  &converter.CustomFormatConfig{Kind: "words"},
		OutputCustom: &converter.CustomFormatConfig{Kind: "words"},
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/convert", bytes.NewReader(data))
	rec := httptest.NewRecorder()
	handleConvert(rec, req)
	if rec.Code != 200 {
		t.Fatalf("convert failed: %d %s", rec.Code, rec.Body.String())
	}

	var res struct {
		OutputPath  string `json:"outputPath"`
		MergedFiles int    `json:"mergedFiles"`
		Stats       struct {
			InputEntries  int `json:"inputEntries"`
			OutputEntries int `json:"outputEntries"`
			FilteredOut   int `json:"filteredOut"`
			Duplicates    int `json:"duplicates"`
		} `json:"stats"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		os.Remove(res.OutputPath)
		os.Remove("output")
	})

	if res.MergedFiles != 2 {
		t.Errorf("Expected mergedFiles=2, got %d", res.MergedFiles)
	}
	if res.Stats.InputEntries != 6 {
		t.Errorf("Expected inputEntries=6, got %d", res.Stats.InputEntries)
	}
	if res.Stats.Duplicates != 2 {
		t.Errorf("Expected duplicates=2, got %d", res.Stats.Duplicates)
	}
	if res.Stats.OutputEntries != 4 {
		t.Errorf("Expected outputEntries=4, got %d", res.Stats.OutputEntries)
	}

	// 输出文件名应体现合并信息
	if !strings.Contains(filepath.Base(res.OutputPath), "等2个合并") {
		t.Errorf("Unexpected merged output filename: %s", res.OutputPath)
	}

	content, err := os.ReadFile(res.OutputPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(content), "\r\n"), "\n")
	if len(lines) != 4 {
		t.Errorf("Expected 4 lines in merged output, got %d: %q", len(lines), string(content))
	}
}
