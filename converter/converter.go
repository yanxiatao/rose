package converter

import (
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	convEncoder "github.com/nopdan/rose/encoder"
	convFilter "github.com/nopdan/rose/filter"
	"github.com/nopdan/rose/format"
	"github.com/nopdan/rose/format/custom_text"
	"github.com/nopdan/rose/model"
	"github.com/nopdan/rose/util"
)

// Converter 核心转换器
type Converter struct {
	registry *format.Registry
	filters  []convFilter.Filter
	encoder  convEncoder.Encoder
}

// NewConverter 创建新的转换器
func NewConverter() *Converter {
	return &Converter{
		registry: format.GlobalRegistry,
		filters:  make([]convFilter.Filter, 0),
	}
}

// NewDefaultConverter 创建默认转换器
func NewDefaultConverter() *Converter {
	return NewConverter()
}

// Job 转换请求
type Job struct {
	Input   *InputSpec   // 单输入（与 Inputs 二选一）
	Inputs  []*InputSpec // 多输入，导出时合并词条
	Output  *OutputSpec
	Encoder *EncodingSpec
}

// CustomFieldConfig 自定义格式字段配置
type CustomFieldConfig struct {
	Type            string `json:"type"`
	PinyinSeparator string `json:"pinyinSeparator"`
	PinyinPrefix    string `json:"pinyinPrefix"`
	PinyinSuffix    string `json:"pinyinSuffix"`
	Literal         string `json:"literal"`
}

// CustomFormatConfig 自定义格式配置
type CustomFormatConfig struct {
	Kind          string              `json:"kind"`
	Encoding      string              `json:"encoding"`
	Fields        []CustomFieldConfig `json:"fields"`
	SortByCode    bool                `json:"sortByCode"`
	CommentPrefix string              `json:"commentPrefix"`
	StartMarker   string              `json:"startMarker"`
}

// InputSpec 输入配置
type InputSpec struct {
	Source model.Source
	Path   string
	Format string
	Custom *CustomFormatConfig // 当 Format 为自定义时，携带格式配置
}

// OutputSpec 输出配置
type OutputSpec struct {
	Writer    io.Writer
	Path      string
	Format    string
	Custom    *CustomFormatConfig // 当 Format 为自定义时，携带格式配置
	Overwrite bool
	SplitSize int // 按条目数分割成多个文件，0 表示不分割
}

// Result 转换结果
type Result struct {
	OutputFile  string
	OutputFiles []string // 分割导出时的所有输出文件
	Stats       *Stats
	Entries     []*model.Entry
}

// Stats 转换统计信息
type Stats struct {
	InputEntries  int
	OutputEntries int
	FilteredOut   int
	Duplicates    int // 合并多文件时去除的重复词条数
	ProcessTime   time.Duration
}

// EncodingSpec 编码器配置
type EncodingSpec struct {
	ID     string         `json:"id"`
	Params map[string]any `json:"params,omitempty"`
}

// Convert 执行转换
func (c *Converter) Convert(req *Job) (*Result, error) {
	start := time.Now()

	// 1. 收集输入配置（兼容单个 Input 与多个 Inputs）
	inputs := req.Inputs
	if len(inputs) == 0 && req.Input != nil {
		inputs = []*InputSpec{req.Input}
	}
	if len(inputs) == 0 {
		return nil, fmt.Errorf("input config is required")
	}
	if req.Output == nil {
		return nil, fmt.Errorf("output config is required")
	}

	// 2. 获取输出格式处理器
	outputFormat, ok := c.getFormat(req.Output.Format, req.Output.Custom)
	if !ok {
		return nil, fmt.Errorf("unsupported output format: %s", req.Output.Format)
	}
	exporter, ok := outputFormat.(model.Exporter)
	if !ok {
		return nil, fmt.Errorf("output format does not support export: %s", req.Output.Format)
	}

	// 3. 逐个解析输入文件，合并词条
	entries := make([]*model.Entry, 0, 1024)
	var firstInputFormat model.Format
	for i, in := range inputs {
		inputFormat, ok := c.getFormat(in.Format, in.Custom)
		if !ok {
			return nil, fmt.Errorf("unsupported input format: %s", in.Format)
		}
		importer, ok := inputFormat.(model.Importer)
		if !ok {
			return nil, fmt.Errorf("input format does not support import: %s", in.Format)
		}
		if i == 0 {
			firstInputFormat = inputFormat
		}
		src := in.Source
		if src == nil {
			if in.Path == "" {
				return nil, fmt.Errorf("input source is required")
			}
			src = model.NewFileSource(in.Path)
		}
		parsed, err := importer.Import(src)
		if err != nil {
			return nil, fmt.Errorf("failed to parse input file %s: %w", in.Path, err)
		}
		entries = append(entries, parsed...)
	}
	originalCount := len(entries)

	// 4. 应用编码器转换
	var encoder convEncoder.Encoder = &convEncoder.NullEncoder{}
	if req.Encoder != nil {
		if e := c.createEncoder(req.Encoder); e != nil {
			encoder = e
		}
	} else {
		if c.encoder != nil {
			encoder = c.encoder
		} else {
			encoder = c.autoEncoder(firstInputFormat, outputFormat)
		}
	}

	// 如果需要编码转换，应用到entries
	if encoder != nil {
		encoder.EncodeBatch(entries)
	}

	// 5. 多文件合并时去除词与编码完全相同的重复词条
	duplicates := 0
	if len(inputs) > 1 {
		entries, duplicates = dedupeEntries(entries)
	}

	// 6. 应用过滤器（编码后过滤）
	if len(c.filters) > 0 {
		entries = c.applyFilters(entries)
	}

	// 7. 导出到文件（SplitSize > 0 时按条目数分割成多个文件）
	var outputFiles []string
	if req.Output.SplitSize > 0 && req.Output.Writer == nil && len(entries) > req.Output.SplitSize {
		files, err := c.exportSplit(exporter, entries, req.Output)
		if err != nil {
			return nil, err
		}
		outputFiles = files
	} else {
		writer := req.Output.Writer
		outputFile := req.Output.Path
		if writer == nil {
			if outputFile == "" {
				return nil, fmt.Errorf("output writer or file path is required")
			}
			f, err := os.Create(outputFile)
			if err != nil {
				return nil, fmt.Errorf("failed to create output file: %w", err)
			}
			defer f.Close()
			writer = f
		}

		if err := exporter.Export(entries, writer); err != nil {
			return nil, fmt.Errorf("failed to export to file: %w", err)
		}
		outputFiles = []string{outputFile}
	}

	// 8. 计算统计信息
	stats := &Stats{
		InputEntries:  originalCount,
		OutputEntries: len(entries),
		FilteredOut:   originalCount - duplicates - len(entries),
		Duplicates:    duplicates,
		ProcessTime:   time.Since(start),
	}

	return &Result{
		OutputFile:  outputFiles[0],
		OutputFiles: outputFiles,
		Stats:       stats,
		Entries:     entries,
	}, nil
}

// exportSplit 按条目数将词条分割导出为多个文件，返回输出文件列表
func (c *Converter) exportSplit(exporter model.Exporter, entries []*model.Entry, output *OutputSpec) ([]string, error) {
	if output.Path == "" {
		return nil, fmt.Errorf("output file path is required")
	}
	ext := filepath.Ext(output.Path)
	stem := strings.TrimSuffix(output.Path, ext)

	files := make([]string, 0, len(entries)/output.SplitSize+1)
	for start, i := 0, 1; start < len(entries); start, i = start+output.SplitSize, i+1 {
		end := min(start+output.SplitSize, len(entries))
		path := fmt.Sprintf("%s_%d%s", stem, i, ext)
		f, err := os.Create(path)
		if err != nil {
			return files, fmt.Errorf("failed to create output file: %w", err)
		}
		err = exporter.Export(entries[start:end], f)
		f.Close()
		if err != nil {
			return files, fmt.Errorf("failed to export to file: %w", err)
		}
		files = append(files, path)
	}
	return files, nil
}

// dedupeEntries 去除词与编码完全相同的重复词条，保留首次出现的
func dedupeEntries(entries []*model.Entry) ([]*model.Entry, int) {
	type entryKey struct {
		word     string
		code     string
		codeType model.CodeType
	}
	seen := make(map[entryKey]struct{}, len(entries))
	kept := make([]*model.Entry, 0, len(entries))
	duplicates := 0
	for _, e := range entries {
		key := entryKey{word: e.Word, codeType: e.CodeType}
		if e.Code != nil {
			key.code = e.Code.String()
		}
		if _, ok := seen[key]; ok {
			duplicates++
			continue
		}
		seen[key] = struct{}{}
		kept = append(kept, e)
	}
	return kept, duplicates
}

// GetSupportedFormats 获取支持的格式列表
func (c *Converter) GetSupportedFormats() []*model.BaseFormat {
	return c.registry.List()
}

// GetFormatsByType 根据类型获取格式列表
func (c *Converter) GetFormatsByType(formatType model.FormatType) []*model.BaseFormat {
	return c.registry.ListByType(formatType)
}

// AddFilter 添加过滤器
func (c *Converter) AddFilter(filter convFilter.Filter) {
	c.filters = append(c.filters, filter)
}

// RegisterEncoder 注册编码器（每个转换器仅支持一个）
func (c *Converter) RegisterEncoder(encoder convEncoder.Encoder) {
	c.encoder = encoder
}

// getFormat 获取格式处理器，支持通过 custom config 创建自定义格式
func (c *Converter) getFormat(id string, custom *CustomFormatConfig) (model.Format, bool) {
	if custom != nil {
		f, err := buildCustomFormat(id, custom)
		if err != nil {
			return nil, false
		}
		return f, true
	}
	return c.registry.Get(id)
}

// buildCustomFormat 根据配置创建自定义纯文本格式
func buildCustomFormat(id string, cfg *CustomFormatConfig) (*custom_text.CustomText, error) {
	encLabel := strings.TrimSpace(cfg.Encoding)
	if encLabel == "" {
		encLabel = "UTF-8"
	}
	enc := util.NewEncoding(encLabel)

	kind := strings.ToLower(strings.TrimSpace(cfg.Kind))
	formatType := model.FormatTypeWords
	switch kind {
	case "pinyin":
		formatType = model.FormatTypePinyin
	case "wubi":
		formatType = model.FormatTypeWubi
	case "words":
		formatType = model.FormatTypeWords
	default:
		return nil, fmt.Errorf("unsupported custom format kind: %s", kind)
	}

	fields, err := parseCustomFields(cfg.Fields, formatType)
	if err != nil {
		return nil, err
	}
	return custom_text.NewCustom(
		id, "自定义格式", formatType, enc, fields,
	).WithSortByCode(cfg.SortByCode).WithCommentPrefix(cfg.CommentPrefix).WithStartMarker(cfg.StartMarker), nil
}

// parseCustomFields 解析自定义格式字段配置
func parseCustomFields(fieldConfigs []CustomFieldConfig, formatType model.FormatType) ([]custom_text.FieldConfig, error) {
	if len(fieldConfigs) == 0 {
		switch formatType {
		case model.FormatTypePinyin:
			return []custom_text.FieldConfig{
				{Type: custom_text.FieldTypeWord},
				{Type: custom_text.FieldTypeTab},
				{Type: custom_text.FieldTypePinyin, PinyinSeparator: "'"},
			}, nil
		case model.FormatTypeWubi:
			return []custom_text.FieldConfig{
				{Type: custom_text.FieldTypeWord},
				{Type: custom_text.FieldTypeTab},
				{Type: custom_text.FieldTypeCode},
			}, nil
		default:
			return []custom_text.FieldConfig{
				{Type: custom_text.FieldTypeWord},
			}, nil
		}
	}
	fields := make([]custom_text.FieldConfig, 0, len(fieldConfigs))
	for _, cfg := range fieldConfigs {
		var fieldType custom_text.FieldType
		switch strings.ToLower(strings.TrimSpace(cfg.Type)) {
		case "word":
			fieldType = custom_text.FieldTypeWord
		case "pinyin":
			fieldType = custom_text.FieldTypePinyin
		case "code":
			fieldType = custom_text.FieldTypeCode
		case "frequency":
			fieldType = custom_text.FieldTypeFrequency
		case "rank":
			fieldType = custom_text.FieldTypeRank
		case "tab":
			fieldType = custom_text.FieldTypeTab
		case "space":
			fieldType = custom_text.FieldTypeSpace
		case "literal":
			fieldType = custom_text.FieldTypeLiteral
		default:
			return nil, fmt.Errorf("unknown field type: %s", cfg.Type)
		}
		fields = append(fields, custom_text.FieldConfig{
			Type:            fieldType,
			PinyinSeparator: cfg.PinyinSeparator,
			PinyinPrefix:    cfg.PinyinPrefix,
			PinyinSuffix:    cfg.PinyinSuffix,
			Literal:         cfg.Literal,
		})
	}
	return fields, nil
}

// applyFilters 应用过滤器
func (c *Converter) applyFilters(entries []*model.Entry) []*model.Entry {
	filtered := make([]*model.Entry, 0, len(entries))
	for _, entry := range entries {
		filteredOut := false
		for _, filter := range c.filters {
			if filter.Filter(entry) {
				filteredOut = true
				break
			}
		}
		if !filteredOut {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

// createEncoder 根据配置创建或获取编码器
func (c *Converter) createEncoder(cfg *EncodingSpec) convEncoder.Encoder {
	if cfg == nil {
		return nil
	}

	// 1) 先看是否在运行时注册
	if c.encoder != nil {
		return c.encoder
	}

	// 2) 使用工厂函数创建编码器
	params := make(map[string]any)
	if cfg.Params != nil {
		maps.Copy(params, cfg.Params)
	}

	return convEncoder.NewEncoder(cfg.ID, params)
}

func (c *Converter) autoEncoder(inputFormat, outputFormat model.Format) convEncoder.Encoder {
	if inputFormat == nil || outputFormat == nil {
		return &convEncoder.NullEncoder{}
	}

	outputType := outputFormat.Info().Type
	switch outputType {
	case model.FormatTypePinyin:
		return convEncoder.NewEncoder("pinyin", nil)
	case model.FormatTypeWubi:
		return convEncoder.NewEncoder("wubi", map[string]any{"schema": "86", "useAABC": true})
	default:
		return &convEncoder.NullEncoder{}
	}
}
