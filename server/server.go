package server

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/nopdan/rose/converter"
	"github.com/nopdan/rose/encoder"
	"github.com/nopdan/rose/filter"
	"github.com/nopdan/rose/format"
	"github.com/nopdan/rose/frontend"
	"github.com/nopdan/rose/model"
)

// FormatInfo 格式信息
type FormatInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      int    `json:"kind"`
	Ext       string `json:"ext"`
	CanImport bool   `json:"canImport"`
	CanExport bool   `json:"canExport"`
}

// StoredFile 存储的文件信息
type StoredFile struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
}

// ConvertRequest 转换请求
type ConvertRequest struct {
	FileIDs      []string                      `json:"fileIds,omitempty"` // 多文件，导出时自动合并
	FileID       string                        `json:"fileId,omitempty"`  // 兼容旧版单文件
	InputFormat  string                        `json:"inputFormat"`
	OutputFormat string                        `json:"outputFormat"`
	InputCustom  *converter.CustomFormatConfig `json:"inputCustom,omitempty"`
	OutputCustom *converter.CustomFormatConfig `json:"outputCustom,omitempty"`
	Encoder      *EncoderConfig                `json:"encoder,omitempty"`
	Filter       *FilterConfig                 `json:"filter,omitempty"`
	SplitSize    int                           `json:"splitSize,omitempty"` // 按条目数分割输出，0 表示不分割
}

// EncoderConfig 编码器配置
type EncoderConfig struct {
	Type            string `json:"type"`            // "pinyin" or "wubi"
	Schema          string `json:"schema"`          // "86", "98", "06"
	CodeTableFileID string `json:"codeTableFileId"` // 自定义码表文件ID
	UseAABC         bool   `json:"useAABC"`         // 组词规则
}

// FilterConfig 过滤器配置
type FilterConfig struct {
	MinLength     int      `json:"minLength"`
	MaxLength     int      `json:"maxLength"`
	MinFrequency  int      `json:"minFrequency"`
	MaxFrequency  int      `json:"maxFrequency"`
	FilterEnglish bool     `json:"filterEnglish"`
	FilterNumber  bool     `json:"filterNumber"`
	CustomRules   []string `json:"customRules"`
}

var (
	dist        = frontend.Dist
	storedFiles = make(map[string]*StoredFile)
	fileCounter = 0
)

func Serve(port int) {
	distFS, _ := fs.Sub(dist, "dist")
	http.Handle("/", http.FileServer(http.FS(distFS)))

	http.HandleFunc("/api/formats", handleFormats)
	http.HandleFunc("/api/upload", handleUpload)
	http.HandleFunc("/api/convert", handleConvert)

	ln, actualPort, err := listenWithFallback(port, 20)
	if err != nil {
		log.Fatalf("Failed to bind port: %v", err)
	}
	addr := fmt.Sprintf("http://localhost:%d", actualPort)
	log.Println("Listening on " + addr)
	openBrowser(addr)
	if err := http.Serve(ln, nil); err != nil {
		log.Fatalf("Server stopped: %v", err)
	}
}

func listenWithFallback(port, _ int) (net.Listener, int, error) {
	// 尝试指定端口
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err == nil {
		return ln, port, nil
	}
	log.Printf("端口 %d 不可用: %v，尝试随机端口...", port, err)
	// 回退到系统分配的可用端口
	ln, err = net.Listen("tcp", ":0")
	if err != nil {
		return nil, 0, err
	}
	actualPort := ln.Addr().(*net.TCPAddr).Port
	return ln, actualPort, nil
}

// handleFormats 获取所有支持的格式列表
func handleFormats(w http.ResponseWriter, r *http.Request) {
	setupCORS(&w)
	if r.Method == "OPTIONS" {
		return
	}
	log.Println("GET /api/formats")
	writeJSON(w, getFormatList())
}

// handleUpload 处理文件上传
func handleUpload(w http.ResponseWriter, r *http.Request) {
	setupCORS(&w)
	if r.Method == "OPTIONS" {
		return
	}
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.ParseMultipartForm(1 << 32)
	file, handler, err := r.FormFile("file")
	if err != nil {
		log.Printf("POST /api/upload err: %v\n", err)
		http.Error(w, "文件上传失败", http.StatusBadRequest)
		return
	}
	defer file.Close()

	log.Printf("POST /api/upload %v", handler.Filename)
	fileData, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "文件读取失败", http.StatusInternalServerError)
		return
	}
	stored, err := saveUploadedFile(handler.Filename, fileData)
	if err != nil {
		http.Error(w, "文件保存失败", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"id":       stored.ID,
		"filename": stored.Filename,
		"size":     stored.Size,
	})
}

// handleConvert 执行词库转换，将结果保存为临时文件并返回下载 ID
func handleConvert(w http.ResponseWriter, r *http.Request) {
	setupCORS(&w)
	if r.Method == "OPTIONS" {
		return
	}
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req ConvertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求格式错误", http.StatusBadRequest)
		return
	}
	inputFormat := strings.TrimSpace(req.InputFormat)
	outputFormat := strings.TrimSpace(req.OutputFormat)
	if inputFormat == "" || outputFormat == "" {
		http.Error(w, "输入或输出格式缺失", http.StatusBadRequest)
		return
	}

	// 收集待转换文件（兼容旧的单文件字段）
	fileIDs := req.FileIDs
	if len(fileIDs) == 0 && req.FileID != "" {
		fileIDs = []string{req.FileID}
	}
	stored := make([]*StoredFile, 0, len(fileIDs))
	for _, id := range fileIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		f := findStoredFile(id)
		if f == nil {
			http.Error(w, "文件不存在，请先上传", http.StatusBadRequest)
			return
		}
		stored = append(stored, f)
	}
	if len(stored) == 0 {
		http.Error(w, "请先上传文件", http.StatusBadRequest)
		return
	}
	log.Printf("POST /api/convert files=%d input=%s output=%s", len(stored), inputFormat, outputFormat)

	conv := converter.NewConverter()

	// 构建 encoder
	if req.Encoder != nil {
		if enc := buildEncoder(req.Encoder); enc != nil {
			conv.RegisterEncoder(enc)
		}
	}

	// 构建 filter
	applyFilters(conv, req.Filter)

	// 生成输出路径
	outputFilename := buildOutputFilename(stored[0].Filename, outputFormat)
	if len(stored) > 1 {
		outputFilename = buildMergedFilename(stored[0].Filename, len(stored), outputFormat)
	}
	outputDir := "output"
	_ = os.MkdirAll(outputDir, 0o755)
	outputPath := uniquePath(filepath.Join(outputDir, outputFilename))

	// 分割条目数合法性检查
	splitSize := req.SplitSize
	if splitSize < 0 {
		splitSize = 0
	}

	// 每个文件构建一个输入配置，导出时合并词条
	inputs := make([]*converter.InputSpec, 0, len(stored))
	for _, f := range stored {
		inputs = append(inputs, &converter.InputSpec{
			Source: model.NewFileSource(f.Path),
			Path:   f.Filename,
			Format: inputFormat,
			Custom: req.InputCustom,
		})
	}

	result, err := conv.Convert(&converter.Job{
		Inputs: inputs,
		Output: &converter.OutputSpec{
			Path:      outputPath,
			Format:    outputFormat,
			Custom:    req.OutputCustom,
			Overwrite: true,
			SplitSize: splitSize,
		},
	})
	if err != nil {
		log.Printf("转换失败: %v", err)
		http.Error(w, fmt.Sprintf("转换失败: %v", err), http.StatusInternalServerError)
		return
	}

	resp := map[string]any{
		"outputPath": result.OutputFile,
		"stats": map[string]int{
			"inputEntries":  result.Stats.InputEntries,
			"outputEntries": result.Stats.OutputEntries,
			"filteredOut":   result.Stats.FilteredOut,
			"duplicates":    result.Stats.Duplicates,
		},
	}
	if len(stored) > 1 {
		resp["mergedFiles"] = len(stored)
	}
	if len(result.OutputFiles) > 1 {
		resp["outputFiles"] = result.OutputFiles
	}
	writeJSON(w, resp)
}

// --- 辅助函数 ---

func getFormatList() []FormatInfo {
	formats := format.GlobalRegistry.List()
	items := make([]FormatInfo, 0, len(formats))
	for _, f := range formats {
		canImport, canExport := false, false
		if actual, ok := format.GlobalRegistry.Get(f.ID); ok {
			if _, ok := actual.(model.Importer); ok {
				canImport = true
			}
			if _, ok := actual.(model.Exporter); ok {
				canExport = true
			}
		}
		items = append(items, FormatInfo{
			ID:        f.ID,
			Name:      f.Name,
			Kind:      int(f.Type),
			Ext:       f.Extension,
			CanImport: canImport,
			CanExport: canExport,
		})
	}
	return items
}

// uniquePath 在文件名重复时添加 (2)、(3)… 后缀
func uniquePath(p string) string {
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return p
	}
	ext := filepath.Ext(p)
	base := strings.TrimSuffix(p, ext)
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s(%d)%s", base, i, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func buildEncoder(cfg *EncoderConfig) encoder.Encoder {
	if cfg == nil {
		return nil
	}
	switch cfg.Type {
	case "pinyin":
		return encoder.NewEncoder("pinyin", nil)
	case "wubi":
		params := map[string]any{
			"schema":  cfg.Schema,
			"useAABC": cfg.UseAABC,
		}
		if cfg.CodeTableFileID != "" {
			if f := findStoredFile(cfg.CodeTableFileID); f != nil {
				data, err := os.ReadFile(f.Path)
				if err == nil {
					params["codeTableData"] = data
					params["schema"] = "custom"
				}
			}
		}
		return encoder.NewEncoder("wubi", params)
	case "none":
		return &encoder.NullEncoder{}
	default:
		return nil
	}
}

func buildOutputFilename(inputFilename, outputFormat string) string {
	base := strings.TrimSuffix(inputFilename, filepath.Ext(inputFilename))
	name := base + "_" + outputFormat
	ext := getFormatExtension(outputFormat)
	if ext == "" {
		ext = ".txt"
	}
	name += ext
	return name
}

// buildMergedFilename 多文件合并导出时的输出文件名
func buildMergedFilename(firstFilename string, count int, outputFormat string) string {
	base := strings.TrimSuffix(firstFilename, filepath.Ext(firstFilename))
	ext := getFormatExtension(outputFormat)
	if ext == "" {
		ext = ".txt"
	}
	return fmt.Sprintf("%s_等%d个合并_%s%s", base, count, outputFormat, ext)
}

func getFormatExtension(formatID string) string {
	if f, ok := format.GlobalRegistry.Get(formatID); ok {
		if info := f.Info(); info != nil {
			return info.Extension
		}
	}
	return ""
}

func saveUploadedFile(filename string, data []byte) (*StoredFile, error) {
	uploadDir := filepath.Join(os.TempDir(), "rose_uploads")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		return nil, err
	}
	fileCounter++
	fileID := fmt.Sprintf("f_%d", fileCounter)
	filePath := filepath.Join(uploadDir, fmt.Sprintf("%s_%s", fileID, filename))
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		return nil, err
	}
	stored := &StoredFile{
		ID: fileID, Filename: filename, Path: filePath, Size: int64(len(data)),
	}
	storedFiles[fileID] = stored
	return stored, nil
}

func findStoredFile(fileID string) *StoredFile {
	if fileID == "" {
		return nil
	}
	if f, ok := storedFiles[fileID]; ok {
		return f
	}
	return nil
}

func applyFilters(conv *converter.Converter, cfg *FilterConfig) {
	if cfg == nil {
		return
	}
	if cfg.MinLength > 0 || cfg.MaxLength > 0 {
		conv.AddFilter(filter.NewLengthFilter(cfg.MinLength, cfg.MaxLength))
	}
	if cfg.MinFrequency > 0 || cfg.MaxFrequency > 0 {
		conv.AddFilter(filter.NewFrequencyFilter(cfg.MinFrequency, cfg.MaxFrequency))
	}
	if cfg.FilterEnglish || cfg.FilterNumber {
		conv.AddFilter(filter.NewCharacterFilter(cfg.FilterEnglish, cfg.FilterNumber))
	}
	if len(cfg.CustomRules) > 0 {
		conv.AddFilter(filter.NewRegexFilter(cfg.CustomRules))
	}
}

func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func setupCORS(w *http.ResponseWriter) {
	(*w).Header().Set("Access-Control-Allow-Origin", "*")
	(*w).Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	(*w).Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding")
}

func openBrowser(url string) {
	var name string
	switch runtime.GOOS {
	case "windows":
		name = "explorer"
	case "linux":
		name = "xdg-open"
	default:
		name = "open"
	}
	cmd := exec.Command(name, url)
	cmd.Start()
}
