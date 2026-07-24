package document

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/william-lbn/DecisionProof/internal/domain"
	"github.com/william-lbn/DecisionProof/internal/integrity"
	"github.com/william-lbn/DecisionProof/internal/product"
)

const (
	maxSourceFileBytes = 50 << 20 // 原始文件最大 50 MiB。
	maxOOXMLPartBytes  = 64 << 20 // 单个 XML 部件解压后最大 64 MiB，防止 ZIP Bomb。
	maxPPTXSlides      = 5000     // 防止异常文件创建海量 Slide。
	maxExtractedBlocks = 50000
)

// ExtractFile 从常见办公文档中提取可追踪文本块。
// 当前实现原生支持 DOCX、PPTX、Markdown 和纯文本；PDF 需要接入独立解析服务。
func ExtractFile(path string) ([]product.SourceBlock, string, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", "", fmt.Errorf("读取文档失败: %w", err)
	}
	if len(data) > maxSourceFileBytes {
		return nil, "", "", fmt.Errorf("原始文档超过 %d MiB 限制", maxSourceFileBytes>>20)
	}
	digest := integrity.SHA256Bytes(data)
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".docx":
		blocks, err := extractDOCX(data)
		return blocks, digest, "application/vnd.openxmlformats-officedocument.wordprocessingml.document", err
	case ".pptx":
		blocks, err := extractPPTX(data)
		return blocks, digest, "application/vnd.openxmlformats-officedocument.presentationml.presentation", err
	case ".md", ".markdown":
		blocks, err := extractLines(data, "markdown")
		return blocks, digest, "text/markdown", err
	case ".txt":
		blocks, err := extractLines(data, "text")
		return blocks, digest, "text/plain", err
	default:
		return nil, digest, "application/octet-stream", fmt.Errorf("暂不支持文件类型 %s；PDF 应通过外部解析适配器处理", ext)
	}
}

func extractLines(data []byte, kind string) ([]product.SourceBlock, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	// 产品文档可能存在较长单行，主动提高限制。
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	blocks := make([]product.SourceBlock, 0)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		blocks = append(blocks, newBlock(kind, fmt.Sprintf("%s:line:%d", kind, lineNo), text, len(blocks)+1))
		if len(blocks) > maxExtractedBlocks {
			return nil, fmt.Errorf("提取文本块超过 %d 限制", maxExtractedBlocks)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("解析文本行失败: %w", err)
	}
	return blocks, nil
}

func extractDOCX(data []byte) ([]product.SourceBlock, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("解析 DOCX ZIP 失败: %w", err)
	}
	var target *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			target = f
			break
		}
	}
	if target == nil {
		return nil, errors.New("DOCX 缺少 word/document.xml")
	}
	if target.UncompressedSize64 > maxOOXMLPartBytes {
		return nil, errors.New("DOCX document.xml 解压后超过安全限制")
	}
	r, err := target.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	paragraphs, err := extractOOXMLParagraphs(io.LimitReader(r, maxOOXMLPartBytes+1), "docx")
	if err != nil {
		return nil, err
	}
	blocks := make([]product.SourceBlock, 0, len(paragraphs))
	for i, text := range paragraphs {
		blocks = append(blocks, newBlock("paragraph", fmt.Sprintf("docx:paragraph:%d", i+1), text, i+1))
	}
	return blocks, nil
}

var slideRE = regexp.MustCompile(`^ppt/slides/slide([0-9]+)\.xml$`)

func extractPPTX(data []byte) ([]product.SourceBlock, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("解析 PPTX ZIP 失败: %w", err)
	}
	type slide struct {
		n int
		f *zip.File
	}
	var slides []slide
	for _, f := range zr.File {
		m := slideRE.FindStringSubmatch(f.Name)
		if len(m) != 2 {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		slides = append(slides, slide{n: n, f: f})
	}
	if len(slides) > maxPPTXSlides {
		return nil, fmt.Errorf("PPTX 幻灯片超过 %d 限制", maxPPTXSlides)
	}
	sort.Slice(slides, func(i, j int) bool { return slides[i].n < slides[j].n })
	blocks := make([]product.SourceBlock, 0, len(slides))
	for _, s := range slides {
		if s.f.UncompressedSize64 > maxOOXMLPartBytes {
			return nil, fmt.Errorf("PPTX slide%d.xml 解压后超过安全限制", s.n)
		}
		r, err := s.f.Open()
		if err != nil {
			return nil, err
		}
		paragraphs, err := extractOOXMLParagraphs(io.LimitReader(r, maxOOXMLPartBytes+1), "pptx")
		r.Close()
		if err != nil {
			return nil, err
		}
		text := strings.Join(paragraphs, "\n")
		text = strings.TrimSpace(text)
		if text != "" {
			blocks = append(blocks, newBlock("slide", fmt.Sprintf("pptx:slide:%d", s.n), text, len(blocks)+1))
			if len(blocks) > maxExtractedBlocks {
				return nil, fmt.Errorf("提取文本块超过 %d 限制", maxExtractedBlocks)
			}
		}
	}
	return blocks, nil
}

func extractOOXMLParagraphs(r io.Reader, format string) ([]string, error) {
	decoder := xml.NewDecoder(r)
	var paragraphs []string
	var current []string
	inParagraph := false
	for {
		tok, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("解析 %s XML 失败: %w", format, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "p" {
				if inParagraph && len(current) > 0 {
					paragraphs = appendText(paragraphs, current)
				}
				inParagraph = true
				current = nil
			}
			if t.Name.Local == "t" {
				var value string
				if err := decoder.DecodeElement(&value, &t); err != nil {
					return nil, err
				}
				value = strings.TrimSpace(value)
				if value != "" {
					current = append(current, value)
				}
			}
		case xml.EndElement:
			if t.Name.Local == "p" && inParagraph {
				paragraphs = appendText(paragraphs, current)
				current = nil
				inParagraph = false
			}
		}
	}
	if len(current) > 0 {
		paragraphs = appendText(paragraphs, current)
	}
	return paragraphs, nil
}

func appendText(dst []string, current []string) []string {
	text := strings.TrimSpace(strings.Join(current, " "))
	if text != "" {
		return append(dst, text)
	}
	return dst
}

func newBlock(kind, locator, text string, sequence int) product.SourceBlock {
	id, _ := domain.NewID("blk")
	return product.SourceBlock{ID: id, Kind: kind, Locator: locator, Text: text, Digest: integrity.SHA256Bytes([]byte(text)), Sequence: sequence}
}
